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
    p = scan.build_journal_payload([_cand(), _cand(symbol="SNDK", strike=890.0)],
                                   "Nasdaq official API")
    assert p["symbol_source"] == "Nasdaq official API"
    assert [c["rank"] for c in p["candidates"]] == [1, 2]
    c0 = p["candidates"][0]
    assert c0["symbol"] == "NBIS" and c0["strike"] == 145.0 and c0["bid"] == 18.90
    assert "ibkr_bid" not in c0  # None 的 IBKR 字段不携带


def test_build_journal_payload_carries_ibkr_when_present():
    c = _cand()
    c.ibkr_bid, c.ibkr_ask = 155.0, 155.2
    c.ibkr_option_iv, c.ibkr_option_delta = 1.61, -0.36
    c0 = scan.build_journal_payload([c], "test")["candidates"][0]
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


def test_render_review_section_void_row():
    rec = {"settled": 1, "void": 1, "pending": 0,
           "results": [
               {"symbol": "NBIS", "strike": 145.0, "expiry": "2026-08-21", "outcome": "void",
                "expiry_close": 152.30, "realized_pnl": 0.0, "touched": False, "max_breach_pct": 0}],
           "hit_rate": {"hit": 0, "miss": 0, "void": 1, "rate": 0, "avg_pnl": 0, "window": "all"}}
    lines = scan.render_review_section(rec)
    text = "\n".join(lines)
    assert "⚪" in text
    assert "否" in text  # touched column renders for the void row


def _scan_result(*, candidates=None, data_quality_error=None):
    return scan.ScanResult(
        candidates=candidates if candidates is not None else [_cand()],
        stats={}, errors=[], ibkr_errors=[], ibkr_attempted=0,
        data_quality_error=data_quality_error,
    )


def test_journal_flow_register_failure_degrades_to_note(monkeypatch):
    def fake(args, stdin_json=None, timeout=30):
        return None, "boom"

    monkeypatch.setattr(scan, "run_scan_journal", fake)
    result = _scan_result()
    review_lines, journal_notes = scan.run_journal_flow(
        result, dry_run=False, no_journal=False, with_journal=False)
    assert review_lines == []
    assert journal_notes == ["复盘入库失败：boom", "复盘对账失败：boom"]


def test_journal_flow_inactive_when_dry_run_without_with_journal(monkeypatch):
    def fake(args, stdin_json=None, timeout=30):
        raise AssertionError("run_scan_journal should not be called when journal is inactive")

    monkeypatch.setattr(scan, "run_scan_journal", fake)
    result = _scan_result()

    review_lines, journal_notes = scan.run_journal_flow(
        result, dry_run=True, no_journal=False, with_journal=False)
    assert review_lines == [] and journal_notes == []

    review_lines, journal_notes = scan.run_journal_flow(
        result, dry_run=False, no_journal=True, with_journal=False)
    assert review_lines == [] and journal_notes == []


def test_journal_flow_success_renders_review(monkeypatch):
    rec = {"settled": 1, "void": 0, "pending": 0,
           "results": [
               {"symbol": "NBIS", "strike": 145.0, "expiry": "2026-08-21", "outcome": "hit",
                "expiry_close": 152.30, "realized_pnl": 18.90, "touched": False, "max_breach_pct": 0}],
           "hit_rate": {"hit": 1, "miss": 0, "void": 0, "rate": 1.0, "avg_pnl": 18.90, "window": "all"}}

    def fake(args, stdin_json=None, timeout=30):
        if args[0] == "register":
            return {"registered": 1, "skipped": 0}, None
        assert args[0] == "reconcile"
        return rec, None

    monkeypatch.setattr(scan, "run_scan_journal", fake)
    result = _scan_result()
    review_lines, journal_notes = scan.run_journal_flow(
        result, dry_run=False, no_journal=False, with_journal=False)
    assert journal_notes == []
    assert any("复盘（本次结算 1 笔）" in line for line in review_lines)


def test_journal_flow_skips_register_on_circuit_breaker(monkeypatch):
    calls = []

    def fake(args, stdin_json=None, timeout=30):
        calls.append(args[0])
        assert args[0] == "reconcile", "register must not be called when circuit breaker tripped"
        return ({"settled": 0, "void": 0, "pending": 0, "results": [],
                  "hit_rate": {"hit": 0, "miss": 0, "void": 0, "rate": 0,
                               "avg_pnl": 0, "window": "all"}}, None)

    monkeypatch.setattr(scan, "run_scan_journal", fake)
    result = _scan_result(candidates=[_cand()], data_quality_error="熔断")
    review_lines, journal_notes = scan.run_journal_flow(
        result, dry_run=False, no_journal=False, with_journal=False)
    assert calls == ["reconcile"] and review_lines == [] and journal_notes == []
