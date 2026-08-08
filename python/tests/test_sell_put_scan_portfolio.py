"""Portfolio-aware scan pure functions (#spec 2026-08-08)."""
import importlib.util
import os
from pathlib import Path

os.environ["OPTIX_SCAN_RUNTIME_CHECKED"] = "1"
_spec = importlib.util.spec_from_file_location(
    "sell_put_scan", Path(__file__).resolve().parents[2] / "scripts" / "lark_nasdaq100_sell_put_scan.py")
scan = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(scan)


def _pos(**kw):
    """positions --format json 单条记录（字段名对齐 internal/cli/json_output.go positionOutput）。"""
    base = dict(account="U123", symbol="AAPL", sec_type="STK", quantity=100.0,
                avg_cost=180.0, multiplier=1.0, currency="USD",
                last_price=0.0, market_value=0.0, unrealized_pnl=0.0, unrealized_pnl_pct=0.0)
    base.update(kw)
    return base


def test_normalize_portfolio_symbol():
    assert scan.normalize_portfolio_symbol(" brk b ") == "BRK-B"
    assert scan.normalize_portfolio_symbol("nvda") == "NVDA"


def test_index_stock_long_and_short():
    idx = scan.build_holdings_index([
        _pos(symbol="AAPL", quantity=100.0),
        _pos(symbol="TSLA", quantity=-50.0),
    ])
    assert idx["AAPL"].stock_qty == 100.0 and idx["AAPL"].short_puts == []
    assert idx["TSLA"].stock_qty == -50.0


def test_index_aggregates_stock_across_accounts():
    idx = scan.build_holdings_index([
        _pos(account="U1", quantity=100.0), _pos(account="U2", quantity=-100.0)])
    assert idx["AAPL"].stock_qty == 0.0


def test_index_short_put_converts_expiry():
    idx = scan.build_holdings_index([
        _pos(symbol="NVDA", sec_type="OPT", right="P", quantity=-2.0,
             expiration="20260814", strike=170.0, multiplier=100.0)])
    puts = idx["NVDA"].short_puts
    assert len(puts) == 1
    assert puts[0].strike == 170.0 and puts[0].expiry == "2026-08-14" and puts[0].qty == -2.0


def test_index_ignores_long_puts_calls_and_zero_qty():
    idx = scan.build_holdings_index([
        _pos(symbol="A", sec_type="OPT", right="P", quantity=1.0,
             expiration="20260814", strike=10.0),     # long put — 忽略
        _pos(symbol="B", sec_type="OPT", right="C", quantity=-1.0,
             expiration="20260814", strike=10.0),     # short call — 忽略
        _pos(symbol="C", quantity=0.0),                # 零仓 — 忽略
    ])
    assert "A" not in idx and "B" not in idx and "C" not in idx


def test_index_skips_malformed_row_keeps_rest():
    idx = scan.build_holdings_index([
        {"symbol": "X"},                              # 缺 quantity — 跳过
        {"symbol": None, "sec_type": "STK", "quantity": 1},  # symbol 畸形 — 跳过
        _pos(symbol="MSFT", quantity=10.0),
        _pos(symbol="AMD", sec_type="OPT", right="P", quantity=-1.0,
             expiration="bad", strike=100.0),          # expiration 畸形 — 跳过该腿
    ])
    assert list(idx.keys()) == ["MSFT"]
    assert idx["MSFT"].stock_qty == 10.0
