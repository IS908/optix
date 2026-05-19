#!/usr/bin/env python3
"""Yahoo Finance data fetcher — called by Go broker via subprocess.

Usage:
    python fetcher.py quote AAPL
    python fetcher.py bars AAPL 1d 365
    python fetcher.py option_chain AAPL [YYYYMMDD]

Outputs JSON to stdout. Errors go to stderr with non-zero exit code.
"""

import json
import math
import sys
from datetime import datetime, timedelta, timezone


def _safe_float(v) -> float:
    """Coerce to finite float; treat None/NaN/inf as 0.0."""
    try:
        f = float(v) if v is not None else 0.0
    except (TypeError, ValueError):
        return 0.0
    if not math.isfinite(f):
        return 0.0
    return f


def _safe_int(v) -> int:
    """Coerce to int; treat None/NaN/inf/non-numeric as 0. yfinance sometimes
    returns NaN for volume/openInterest on illiquid contracts."""
    return int(_safe_float(v))


def _ensure_yfinance():
    try:
        import yfinance  # noqa: F401
    except ImportError:
        import subprocess
        subprocess.check_call(
            [sys.executable, "-m", "pip", "install", "--quiet", "yfinance"],
            stdout=sys.stderr,
        )


def fetch_quote(symbol: str) -> dict:
    import yfinance as yf

    ticker = yf.Ticker(symbol)
    info = ticker.info

    # yfinance info dict keys vary; use .get() with defaults
    last = info.get("currentPrice") or info.get("regularMarketPrice") or 0.0
    prev_close = info.get("previousClose") or info.get("regularMarketPreviousClose") or 0.0
    change = last - prev_close if last and prev_close else 0.0
    change_pct = (change / prev_close * 100) if prev_close else 0.0

    return {
        "symbol": symbol,
        "last": float(last),
        "bid": float(info.get("bid", 0) or 0),
        "ask": float(info.get("ask", 0) or 0),
        "volume": int(info.get("volume", 0) or 0),
        "change": round(change, 4),
        "changePct": round(change_pct, 4),
        "high": float(info.get("dayHigh", 0) or 0),
        "low": float(info.get("dayLow", 0) or 0),
        "open": float(info.get("open") or info.get("regularMarketOpen", 0) or 0),
        "close": float(prev_close),
        "high52w": float(info.get("fiftyTwoWeekHigh", 0) or 0),
        "low52w": float(info.get("fiftyTwoWeekLow", 0) or 0),
        "avgVolume": float(info.get("averageVolume", 0) or 0),
        "timestamp": datetime.now(timezone.utc).isoformat(),
    }


def fetch_bars(symbol: str, timeframe: str, days: int) -> list:
    import yfinance as yf

    # Map timeframe to yfinance interval
    interval_map = {
        "1 day": "1d",
        "1d": "1d",
        "1 hour": "1h",
        "1h": "1h",
        "5 mins": "5m",
        "5m": "5m",
        "1 min": "1m",
        "1m": "1m",
    }
    interval = interval_map.get(timeframe, "1d")

    # yfinance period based on days
    if days <= 5:
        period = "5d"
    elif days <= 30:
        period = "1mo"
    elif days <= 90:
        period = "3mo"
    elif days <= 180:
        period = "6mo"
    elif days <= 365:
        period = "1y"
    else:
        period = "2y"

    ticker = yf.Ticker(symbol)
    df = ticker.history(period=period, interval=interval)

    if df.empty:
        return []

    bars = []
    for ts, row in df.iterrows():
        bars.append({
            "timestamp": ts.isoformat(),
            "open": round(float(row["Open"]), 4),
            "high": round(float(row["High"]), 4),
            "low": round(float(row["Low"]), 4),
            "close": round(float(row["Close"]), 4),
            "volume": int(row["Volume"]),
        })
    return bars


def fetch_option_chain(symbol: str, expiration: str = "") -> dict:
    """Fetch option chain for `symbol`.

    expiration: optional YYYYMMDD. If empty, returns the nearest expiry.
    Returns a dict matching model.OptionChain shape:
      {
        "underlying": "GOOGL",
        "expirations": [
          {
            "expiration": "20260522",
            "calls": [{"strike": ..., "bid": ..., "ask": ..., "last": ..., "volume": ..., "openInterest": ..., "iv": ...}],
            "puts":  [...],
          },
          ...
        ]
      }

    When expiration is specified but not in yfinance's available list, returns
    all available expirations as empty-contract entries (Go side will detect
    the missing requested expiry and produce ErrExpiryNotAvailable).
    """
    import yfinance as yf

    ticker = yf.Ticker(symbol)
    avail = list(ticker.options or [])  # yfinance returns YYYY-MM-DD strings
    if not avail:
        return {"underlying": symbol, "expirations": []}

    if expiration:
        # Caller passes YYYYMMDD; convert to YYYY-MM-DD for yfinance lookup.
        target = f"{expiration[:4]}-{expiration[4:6]}-{expiration[6:]}"
        if target not in avail:
            # Return the full available list so Go side can build
            # ErrExpiryNotAvailable. Expirations are emitted in YYYYMMDD.
            return {
                "underlying": symbol,
                "expirations": [
                    {"expiration": e.replace("-", ""), "calls": [], "puts": []}
                    for e in avail
                ],
            }
        targets = [target]
    else:
        # Without a request, surface nearest only (parity with IBKR's default).
        targets = [avail[0]]

    def row(r):
        return {
            "strike":       _safe_float(r.get("strike")),
            "bid":          _safe_float(r.get("bid")),
            "ask":          _safe_float(r.get("ask")),
            "last":         _safe_float(r.get("lastPrice")),
            "volume":       _safe_int(r.get("volume")),
            "openInterest": _safe_int(r.get("openInterest")),
            "iv":           _safe_float(r.get("impliedVolatility")),
        }

    expirations_out = []
    for tgt in targets:
        oc = ticker.option_chain(tgt)
        expirations_out.append({
            "expiration": tgt.replace("-", ""),
            "calls": [row(c) for c in oc.calls.to_dict("records")],
            "puts":  [row(p) for p in oc.puts.to_dict("records")],
        })

    return {"underlying": symbol, "expirations": expirations_out}


def main():
    if len(sys.argv) < 3:
        print("Usage: fetcher.py <quote|bars|option_chain> <SYMBOL> [args...]", file=sys.stderr)
        sys.exit(1)

    _ensure_yfinance()

    command = sys.argv[1]
    symbol = sys.argv[2].upper()

    try:
        if command == "quote":
            result = fetch_quote(symbol)
        elif command == "bars":
            timeframe = sys.argv[3] if len(sys.argv) > 3 else "1d"
            days = int(sys.argv[4]) if len(sys.argv) > 4 else 365
            result = fetch_bars(symbol, timeframe, days)
        elif command == "option_chain":
            expiration = sys.argv[3] if len(sys.argv) > 3 else ""
            result = fetch_option_chain(symbol, expiration)
        else:
            print(f"Unknown command: {command}", file=sys.stderr)
            sys.exit(1)

        json.dump(result, sys.stdout)
    except Exception as e:
        print(f"Error fetching {command} for {symbol}: {e}", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
