"""Scan-journal integration pure functions (#spec 2026-07-29)."""
import importlib.util
import os
from pathlib import Path

os.environ["OPTIX_SCAN_RUNTIME_CHECKED"] = "1"
_spec = importlib.util.spec_from_file_location(
    "sell_put_scan", Path(__file__).resolve().parents[2] / "scripts" / "lark_nasdaq100_sell_put_scan.py")
scan = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(scan)


def _cand(**kw):
    base = dict(symbol="NBIS", spot=155.01, expiry="2026-08-21", dte=23, strike=145.0,
                bid=18.90, ask=19.80, mid=19.35, iv=1.58, oi=3077, volume=28,
                cushion_pct=6.5, premium_yield_pct=13.0, annualized_yield_pct=211.8,
                delta=-0.35, score=1.2)
    base.update(kw)
    return scan.Candidate(**base)


def test_build_journal_payload_shape():
    result = scan.ScanResult(candidates=[_cand(), _cand(symbol="SNDK", strike=890.0)],
                             stats={}, errors=[], ibkr_errors=[], ibkr_attempted=0)
    p = scan.build_journal_payload(result, "Nasdaq official API")
    assert p["symbol_source"] == "Nasdaq official API"
    assert [c["rank"] for c in p["candidates"]] == [1, 2]
    c0 = p["candidates"][0]
    assert c0["symbol"] == "NBIS" and c0["strike"] == 145.0 and c0["bid"] == 18.90
    assert "ibkr_bid" not in c0  # None 的 IBKR 字段不携带


def test_build_journal_payload_carries_ibkr_when_present():
    c = _cand()
    c.ibkr_bid, c.ibkr_ask = 155.0, 155.2
    c.ibkr_option_iv, c.ibkr_option_delta = 1.61, -0.36
    result = scan.ScanResult(candidates=[c], stats={}, errors=[], ibkr_errors=[], ibkr_attempted=1)
    c0 = scan.build_journal_payload(result, "test")["candidates"][0]
    assert c0["ibkr_bid"] == 155.0 and c0["ibkr_option_delta"] == -0.36


def test_render_review_section_empty_when_nothing_settled():
    rec = {"settled": 0, "void": 0, "pending": 1, "results": [],
           "hit_rate": {"hit": 0, "miss": 0, "void": 0, "rate": 0, "avg_pnl": 0, "window": "all"}}
    assert scan.render_review_section(rec) == []


def test_render_review_section_table_and_cumulative():
    rec = {"settled": 2, "void": 0, "pending": 0,
           "results": [
               {"symbol": "NBIS", "strike": 145.0, "expiry": "2026-08-21", "outcome": "hit",
                "expiry_close": 152.30, "realized_pnl": 18.90, "touched": False, "max_breach_pct": 0},
               {"symbol": "SNDK", "strike": 890.0, "expiry": "2026-08-07", "outcome": "miss",
                "expiry_close": 861.20, "realized_pnl": 22.50, "touched": True, "max_breach_pct": 3.2}],
           "hit_rate": {"hit": 12, "miss": 3, "void": 1, "rate": 0.8, "avg_pnl": 8.42, "window": "all"}}
    lines = scan.render_review_section(rec)
    text = "\n".join(lines)
    assert "复盘（本次结算 2 笔）" in text
    assert "NBIS" in text and "✅" in text and "❌" in text
    assert "命中率 80%" in text and "+$8.42" in text
    assert "3.2%" in text
