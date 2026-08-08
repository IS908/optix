import importlib.util
import sys
import types
from pathlib import Path

import pandas as pd


def load_fetcher():
    path = Path(__file__).resolve().parents[2] / "internal" / "broker" / "yfinance" / "fetcher.py"
    spec = importlib.util.spec_from_file_location("optix_yfinance_fetcher", path)
    module = importlib.util.module_from_spec(spec)
    assert spec.loader is not None
    spec.loader.exec_module(module)
    return module


def test_fetch_bars_coerces_nan_volume_to_zero(monkeypatch):
    fetcher = load_fetcher()

    class FakeTicker:
        def history(self, period, interval):
            return pd.DataFrame(
                [
                    {
                        "Open": 100.0,
                        "High": 101.0,
                        "Low": 99.0,
                        "Close": 100.5,
                        "Volume": float("nan"),
                    }
                ],
                index=pd.DatetimeIndex(["2026-06-12T14:30:00Z"]),
            )

    fake_yfinance = types.SimpleNamespace(Ticker=lambda symbol: FakeTicker())
    monkeypatch.setitem(sys.modules, "yfinance", fake_yfinance)

    bars = fetcher.fetch_bars("NANV", "5m", 5)

    assert bars[0]["volume"] == 0


def test_fetch_bars_maps_sub_day_lookback_to_1d_period(monkeypatch):
    """#191 finding 1: a sub-day intraday lookback (days<=1) must map to the
    yfinance "1d" period, not fall through to "5d" (which combined with the
    pre-fix Go-side 365-day default caused movers/heatmap to silently come
    back empty on the live IBKR-unavailable-but-yfinance-fallback path)."""
    fetcher = load_fetcher()
    captured = {}

    class FakeTicker:
        def history(self, period, interval):
            captured["period"] = period
            captured["interval"] = interval
            return pd.DataFrame(
                [{"Open": 100.0, "High": 101.0, "Low": 99.0, "Close": 100.5, "Volume": 1000}],
                index=pd.DatetimeIndex(["2026-06-25T14:30:00Z"]),
            )

    fake_yfinance = types.SimpleNamespace(Ticker=lambda symbol: FakeTicker())
    monkeypatch.setitem(sys.modules, "yfinance", fake_yfinance)

    fetcher.fetch_bars("AAPL", "5m", 1)

    assert captured["period"] == "1d"


def test_fetch_bars_period_buckets_by_days():
    fetcher = load_fetcher()
    cases = [
        (0, "1d"),
        (1, "1d"),
        (2, "5d"),
        (5, "5d"),
        (6, "1mo"),
        (30, "1mo"),
        (31, "3mo"),
        (90, "3mo"),
        (91, "6mo"),
        (180, "6mo"),
        (181, "1y"),
        (365, "1y"),
        (366, "2y"),
    ]
    captured = []

    class RecordingTicker:
        def history(self, period, interval):
            captured.append(period)
            return pd.DataFrame(columns=["Open", "High", "Low", "Close", "Volume"])

    import sys as _sys
    import types as _types

    fake_yfinance = _types.SimpleNamespace(Ticker=lambda symbol: RecordingTicker())
    _sys.modules["yfinance"] = fake_yfinance
    try:
        for days, want in cases:
            fetcher.fetch_bars("AAPL", "5m", days)
            assert captured[-1] == want, f"days={days}: period={captured[-1]!r}, want {want!r}"
    finally:
        del _sys.modules["yfinance"]
