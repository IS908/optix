#!/usr/bin/env python3
"""Yahoo Finance data fetcher — called by Go broker via subprocess.

Usage:
    python fetcher.py quote AAPL
    python fetcher.py bars AAPL 1d 365
    python fetcher.py option_chain AAPL [YYYYMMDD]
    python fetcher.py earnings_dates AAPL,NVDA [LIMIT]

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


def _optional_float(v):
    """Coerce finite numeric values; preserve missing/NaN as None."""
    try:
        f = float(v) if v is not None else None
    except (TypeError, ValueError):
        return None
    if f is None or not math.isfinite(f):
        return None
    return f


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


def fetch_batch_quotes(symbols: list) -> dict:
    """One process, N symbols. Failures are ABSENT from the result (not errors),
    matching the Go side's missing-not-error contract."""
    import yfinance as yf
    out = {}
    for sym in symbols:
        try:
            fi = yf.Ticker(sym).fast_info
            price = _safe_float(getattr(fi, "last_price", None))
            prev = _safe_float(getattr(fi, "previous_close", None))
            if price <= 0:
                continue
            change = price - prev if prev > 0 else 0.0
            change_pct = (change / prev * 100.0) if prev > 0 else 0.0
            out[sym] = {"price": round(price, 4), "change": round(change, 4), "change_pct": round(change_pct, 4)}
        except Exception:
            continue
    return out


def fetch_batch_bars(symbols: list, interval: str, period: str) -> dict:
    """Multi-symbol intraday bars via one yf.download call.
    Returns {sym: [{ts, open, high, low, close, volume}, ...]}; failed symbols absent."""
    import yfinance as yf
    import pandas as pd
    df = yf.download(
        " ".join(symbols), interval=interval, period=period,
        group_by="ticker", progress=False, prepost=True, threads=True,
    )
    out = {}
    for sym in symbols:
        try:
            # Branch on the actual DataFrame shape, NOT symbol count: modern
            # yfinance builds MultiIndex (ticker, field) columns even for a
            # single symbol, so an arity-based branch silently yields zero
            # rows on the one-symbol path (the Go Bars() sparkline refill).
            sub = df[sym] if isinstance(df.columns, pd.MultiIndex) else df
            rows = []
            for ts, r in sub.iterrows():
                close = _safe_float(r.get("Close"))
                if close <= 0:
                    continue
                rows.append({
                    "ts": ts.isoformat(),
                    "open": _safe_float(r.get("Open")),
                    "high": _safe_float(r.get("High")),
                    "low": _safe_float(r.get("Low")),
                    "close": close,
                    "volume": _safe_int(r.get("Volume")),
                })
            if rows:
                out[sym] = rows
        except Exception:
            continue
    return out


def fetch_earnings_dates(symbols: list, limit: int = 12) -> dict:
    """Fetch yfinance earnings-date rows for symbols.

    Returns {sym: [{symbol,event_time,timing,eps_estimate,eps_reported,
    eps_surprise_pct}, ...]}. Missing/unavailable symbols return an empty list.
    """
    import yfinance as yf

    out = {}
    for sym in symbols:
        rows = []
        try:
            ticker = yf.Ticker(sym)
            df = ticker.get_earnings_dates(limit=limit)
            if df is not None and not df.empty:
                for ts, r in df.iterrows():
                    event_time = ts.to_pydatetime().astimezone(timezone.utc).isoformat()
                    hour = ts.to_pydatetime().hour
                    timing = "postmarket" if hour >= 16 else "premarket" if hour < 9 else "unknown"
                    rows.append({
                        "symbol": sym,
                        "event_time": event_time,
                        "timing": timing,
                        "eps_estimate": _optional_float(r.get("EPS Estimate")),
                        "eps_reported": _optional_float(r.get("Reported EPS")),
                        "eps_surprise_pct": _optional_float(r.get("Surprise(%)")),
                    })
        except Exception:
            rows = []
        out[sym] = rows
    return out


def main():
    if len(sys.argv) < 3:
        print("Usage: fetcher.py <quote|bars|option_chain|batch_quotes|batch_bars|earnings_dates> <SYMBOL[,SYMBOL...]> [args...]", file=sys.stderr)
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
        elif command == "batch_quotes":
            # argv[2] = comma-joined Yahoo symbols (upper-cased by the shared path)
            result = fetch_batch_quotes([s for s in symbol.split(",") if s])
        elif command == "batch_bars":
            interval = sys.argv[3] if len(sys.argv) > 3 else "5m"
            period = sys.argv[4] if len(sys.argv) > 4 else "1d"
            result = fetch_batch_bars([s for s in symbol.split(",") if s], interval, period)
        elif command == "earnings_dates":
            limit = int(sys.argv[3]) if len(sys.argv) > 3 else 12
            result = fetch_earnings_dates([s for s in symbol.split(",") if s], limit)
        else:
            print(f"Unknown command: {command}", file=sys.stderr)
            sys.exit(1)

        json.dump(result, sys.stdout)
    except Exception as e:
        print(f"Error fetching {command} for {symbol}: {e}", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
