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


import subprocess


class _FakeCompleted:
    def __init__(self, returncode=0, stdout="", stderr=""):
        self.returncode, self.stdout, self.stderr = returncode, stdout, stderr


def test_fetch_positions_ok(monkeypatch):
    payload = '{"positions": [{"symbol": "AAPL", "sec_type": "STK", "quantity": 100}], "source": "IBKR"}'
    monkeypatch.setattr(scan, "run_optix_subprocess",
                        lambda cmd, timeout, stdin_text=None: _FakeCompleted(stdout=payload))
    rows, err = scan.fetch_portfolio_positions()
    assert err is None and rows == [{"symbol": "AAPL", "sec_type": "STK", "quantity": 100}]


def test_fetch_positions_nonzero_exit_degrades(monkeypatch):
    monkeypatch.setattr(scan, "run_optix_subprocess",
                        lambda cmd, timeout, stdin_text=None: _FakeCompleted(
                            returncode=2, stderr="连接 IBKR 失败: dial tcp 127.0.0.1:4001: connection refused"))
    rows, err = scan.fetch_portfolio_positions()
    assert rows is None
    assert err is not None and "Traceback" not in err  # 紧凑原因,非裸堆栈


def test_fetch_positions_timeout_degrades(monkeypatch):
    def _boom(cmd, timeout, stdin_text=None):
        raise subprocess.TimeoutExpired(cmd, timeout)
    monkeypatch.setattr(scan, "run_optix_subprocess", _boom)
    rows, err = scan.fetch_portfolio_positions()
    assert rows is None and "TimeoutExpired" in err


def test_fetch_positions_bad_json_degrades(monkeypatch):
    monkeypatch.setattr(scan, "run_optix_subprocess",
                        lambda cmd, timeout, stdin_text=None: _FakeCompleted(stdout="not json"))
    rows, err = scan.fetch_portfolio_positions()
    assert rows is None and "parse positions JSON" in err


def test_fetch_positions_missing_list_degrades(monkeypatch):
    monkeypatch.setattr(scan, "run_optix_subprocess",
                        lambda cmd, timeout, stdin_text=None: _FakeCompleted(stdout='{"source": "IBKR"}'))
    rows, err = scan.fetch_portfolio_positions()
    assert rows is None and "positions" in err


def _cand(**kw):
    base = dict(symbol="NVDA", spot=170.0, expiry="2026-08-14", dte=6, strike=160.0,
                bid=1.20, ask=1.40, mid=1.30, iv=0.45, oi=500, volume=200,
                cushion_pct=5.9, premium_yield_pct=0.8, annualized_yield_pct=44.0,
                delta=-0.22, score=0.61)
    base.update(kw)
    return scan.Candidate(**base)


def _holding(stock_qty=0.0, puts=()):
    return scan.Holding(stock_qty=stock_qty, short_puts=list(puts))


def test_classify_no_overlap():
    labels, penalty = scan.classify_candidate(_cand(), {})
    assert labels == "" and penalty == 0.0


def test_classify_stock_long():
    idx = {"NVDA": _holding(stock_qty=100)}
    labels, penalty = scan.classify_candidate(_cand(), idx)
    assert labels == "正股" and penalty == scan.PORTFOLIO_PENALTY_STOCK


def test_classify_stock_short_annotate_only():
    idx = {"NVDA": _holding(stock_qty=-100)}
    labels, penalty = scan.classify_candidate(_cand(), idx)
    assert labels == "空股" and penalty == 0.0


def test_classify_short_put_other_expiry():
    idx = {"NVDA": _holding(puts=[scan.ShortPut(150.0, "2026-08-21", -1.0)])}
    labels, penalty = scan.classify_candidate(_cand(expiry="2026-08-14"), idx)
    assert penalty == scan.PORTFOLIO_PENALTY_SHORT_PUT
    assert labels == "sp 150@08-21"


def test_classify_same_expiry_any_strike_is_clash():
    idx = {"NVDA": _holding(puts=[scan.ShortPut(150.0, "2026-08-14", -1.0)])}
    labels, penalty = scan.classify_candidate(_cand(expiry="2026-08-14", strike=160.0), idx)
    assert penalty == scan.PORTFOLIO_PENALTY_SAME_EXPIRY
    assert labels.startswith("撞期!") and "sp 150@08-14" in labels


def test_classify_multi_puts_nearest_and_count():
    idx = {"NVDA": _holding(puts=[scan.ShortPut(150.0, "2026-09-18", -1.0),
                                  scan.ShortPut(155.0, "2026-08-21", -2.0)])}
    labels, _ = scan.classify_candidate(_cand(expiry="2026-08-14"), idx)
    assert "sp 155@08-21×2" in labels  # 取最近到期一笔,总条数 ×N


def test_classify_combined_takes_max_penalty_and_all_labels():
    idx = {"NVDA": _holding(stock_qty=100,
                            puts=[scan.ShortPut(150.0, "2026-08-14", -1.0)])}
    labels, penalty = scan.classify_candidate(_cand(expiry="2026-08-14"), idx)
    assert penalty == scan.PORTFOLIO_PENALTY_SAME_EXPIRY
    assert "撞期!" in labels and "正股" in labels


def test_apply_awareness_mutates_and_summarizes():
    cands = [_cand(symbol="NVDA"), _cand(symbol="AAPL", expiry="2026-08-21")]
    idx = {"NVDA": _holding(stock_qty=100)}
    line = scan.apply_portfolio_awareness(cands, idx, "09:50 ET")
    assert cands[0].portfolio_labels == "正股"
    assert cands[0].portfolio_penalty == scan.PORTFOLIO_PENALTY_STOCK
    assert cands[1].portfolio_labels == "" and cands[1].portfolio_penalty == 0.0
    assert line == "组合感知: 持仓 1 标的，1 项降权（positions@09:50 ET）"


def test_apply_awareness_no_overlap_line():
    cands = [_cand(symbol="AMD")]
    line = scan.apply_portfolio_awareness(cands, {"NVDA": _holding(stock_qty=1)}, "09:50 ET")
    assert line == "组合感知: 与当前持仓无重叠"


import json as _json


def test_journal_payload_immune_to_display_reorder():
    """spec §6 关键回归:组合感知重排显示不得影响 journal payload。"""
    market_order = [_cand(symbol="NVDA", score=0.9),
                    _cand(symbol="AAPL", score=0.8, expiry="2026-08-21")]
    before = _json.dumps(scan.build_journal_payload(market_order, "src"), sort_keys=True)
    # NVDA 被重罚后显示序反转 —— 但 journal 输入仍是市场序列表
    market_order[0].portfolio_penalty = scan.PORTFOLIO_PENALTY_SAME_EXPIRY
    market_order[0].portfolio_labels = "撞期!"
    display = sorted(market_order,
                     key=lambda c: c.score - c.portfolio_penalty, reverse=True)
    assert [c.symbol for c in display] == ["AAPL", "NVDA"]  # 显示序确实变了
    after = _json.dumps(scan.build_journal_payload(market_order, "src"), sort_keys=True)
    assert before == after  # 字节级不变:payload 不含 penalty/labels,顺序不受显示影响
