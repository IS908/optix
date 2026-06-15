"""Regression tests for sell-side strike selection risk-tolerance mapping (#173).

The recommender picks short-strike candidates from support/resistance, OI walls,
and an IV-distance heuristic. The IV-distance term varies by risk_tolerance; a
conservative seller should land FURTHER out-of-the-money than an aggressive one
(lower assignment probability), and the cash-secured-put / covered-call sides
should mirror each other around spot.
"""
import pytest

from optix_engine.strategy.recommender import (
    AnalysisContext,
    _select_call_strike,
    _select_put_strike,
)


def _bare_ctx(risk: str) -> AnalysisContext:
    """A context that isolates the IV-distance candidate: empty support/resistance
    and OI walls so only the IV-based strike contributes to the median."""
    return AnalysisContext(
        symbol="TST",
        current_price=100.0,
        available_capital=10_000.0,
        risk_tolerance=risk,
        forecast_days=30,
        trend="neutral",
        trend_score=0.0,
        rsi=50.0,
        support_levels=[],
        resistance_levels=[],
        iv_rank=50.0,
        iv_percentile=50.0,
        iv_current=0.40,
        iv_skew=0.0,
        max_pain=100.0,
        pcr=1.0,
        oi_put_walls=[],
        oi_call_walls=[],
        earnings_before_expiry=False,
        next_earnings_date=None,
    )


@pytest.mark.parametrize("risk", ["conservative", "moderate", "aggressive"])
def test_put_strike_below_spot(risk: str) -> None:
    ctx = _bare_ctx(risk)
    strike = _select_put_strike(ctx)
    assert strike is not None
    assert strike < ctx.current_price, f"{risk}: put strike {strike} should be below spot"


@pytest.mark.parametrize("risk", ["conservative", "moderate", "aggressive"])
def test_call_strike_above_spot(risk: str) -> None:
    ctx = _bare_ctx(risk)
    strike = _select_call_strike(ctx)
    assert strike is not None
    assert strike > ctx.current_price, f"{risk}: call strike {strike} should be above spot"


def test_conservative_put_is_furthest_otm() -> None:
    """Conservative sellers should get the LOWEST put strike (furthest below
    spot → lowest assignment probability). #173 regression."""
    cons = _select_put_strike(_bare_ctx("conservative"))
    mod = _select_put_strike(_bare_ctx("moderate"))
    aggr = _select_put_strike(_bare_ctx("aggressive"))
    assert cons is not None and mod is not None and aggr is not None
    assert cons <= mod <= aggr, (
        f"put strikes must satisfy conservative ({cons}) ≤ moderate ({mod}) ≤ "
        f"aggressive ({aggr}); conservative should be furthest OTM (lowest)"
    )
    assert cons < aggr, (
        f"conservative put ({cons}) must be strictly below aggressive ({aggr}) — "
        "if equal, the risk dial is doing nothing"
    )


def test_conservative_call_is_furthest_otm() -> None:
    """Conservative sellers should get the HIGHEST call strike (furthest above
    spot → lowest call-away probability). #173 regression."""
    cons = _select_call_strike(_bare_ctx("conservative"))
    mod = _select_call_strike(_bare_ctx("moderate"))
    aggr = _select_call_strike(_bare_ctx("aggressive"))
    assert cons is not None and mod is not None and aggr is not None
    assert cons >= mod >= aggr, (
        f"call strikes must satisfy conservative ({cons}) ≥ moderate ({mod}) ≥ "
        f"aggressive ({aggr}); conservative should be furthest OTM (highest)"
    )
    assert cons > aggr, (
        f"conservative call ({cons}) must be strictly above aggressive ({aggr})"
    )
