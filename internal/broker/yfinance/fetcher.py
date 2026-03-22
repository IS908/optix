#!/usr/bin/env python3
"""Yahoo Finance data fetcher — called by Go broker via subprocess.

Usage:
    python fetcher.py quote AAPL
    python fetcher.py bars AAPL 1d 365

Outputs JSON to stdout. Errors go to stderr with non-zero exit code.
"""

import json
import sys
from datetime import datetime, timedelta, timezone


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


def main():
    if len(sys.argv) < 3:
        print("Usage: fetcher.py <quote|bars> <SYMBOL> [timeframe] [days]", file=sys.stderr)
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
        else:
            print(f"Unknown command: {command}", file=sys.stderr)
            sys.exit(1)

        json.dump(result, sys.stdout)
    except Exception as e:
        print(f"Error fetching {command} for {symbol}: {e}", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
