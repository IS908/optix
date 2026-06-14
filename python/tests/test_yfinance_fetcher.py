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
