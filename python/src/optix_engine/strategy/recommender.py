"""Strategy recommendation engine for options sell-side strategies.

Decision flow:
1. IV environment assessment → is selling premium worthwhile?
2. Direction judgment → which strategy type?
3. Strike selection → based on support/resistance + OI walls + delta targets
4. Filtering + scoring → rank candidates
5. Output top 3
"""

import numpy as np
from dataclasses import dataclass


@dataclass
class AnalysisContext:
    """Aggregated analysis data passed to the recommender."""
    symbol: str
    current_price: float
    available_capital: float
    risk_tolerance: str  # "conservative" / "moderate" / "aggressive"
    forecast_days: int

    # Technical
    trend: str               # "bullish" / "bearish" / "neutral"
    trend_score: float       # -1.0 to 1.0
    rsi: float
    support_levels: list[dict]
    resistance_levels: list[dict]

    # Options
    iv_rank: float           # 0-100
    iv_percentile: float     # 0-100
    iv_current: float        # ATM IV (annualized, e.g. 0.30 for 30%)
    iv_skew: float
    max_pain: float
    pcr: float
    oi_put_walls: list[tuple[float, int]]   # [(strike, oi), ...]
    oi_call_walls: list[tuple[float, int]]

    # Events
    earnings_before_expiry: bool
    next_earnings_date: str | None


@dataclass
class StrategyRecommendation:
    """A single strategy recommendation."""
    strategy_name: str
    strategy_type: str
    legs: list[dict]  # [{option_type, strike, expiration, quantity, premium, greeks}]
    max_profit: float
    max_loss: float
    risk_reward_ratio: float
    margin_required: float
    probability_of_profit: float
    breakeven_price: float
    net_credit: float
    net_greeks: dict
    score: float
    rationale: str
    risk_warnings: list[str]


def recommend_strategies(ctx: AnalysisContext) -> list[StrategyRecommendation]:
    """Main entry point: generate ranked strategy recommendations."""
    recommendations = []

    # Step 1: IV environment assessment
    iv_assessment = _assess_iv_environment(ctx)
    if iv_assessment == "low":
        return [_make_observation_recommendation(ctx, "Low IV environment (IV Rank < 30%). Sell-side strategies have poor risk/reward. Consider waiting for IV expansion.")]

    # Step 2: Direction judgment
    direction = _judge_direction(ctx)

    # Step 3: Generate candidate strategies based on direction
    candidates = _generate_candidates(ctx, direction)

    # Step 4: Filter and score
    for candidate in candidates:
        if _passes_filters(candidate, ctx):
            candidate.score = _calculate_score(candidate, ctx)
            recommendations.append(candidate)

    # Step 5: Sort by score, return top 3
    recommendations.sort(key=lambda r: r.score, reverse=True)
    return recommendations[:3]


def _assess_iv_environment(ctx: AnalysisContext) -> str:
    """Step 1: Assess IV environment."""
    if ctx.iv_rank >= 50:
        return "high"
    elif ctx.iv_rank >= 30:
        return "medium"
    return "low"


def _judge_direction(ctx: AnalysisContext) -> str:
    """Step 2: Determine market direction.

    Uses weighted scoring of technical signals + options data corrections.
    """
    score = ctx.trend_score  # already a weighted technical score from -1 to 1

    # Max Pain correction
    if ctx.max_pain > 0 and ctx.current_price > 0:
        mp_distance_pct = (ctx.max_pain - ctx.current_price) / ctx.current_price
        # Max Pain > current price → bullish pull
        score += mp_distance_pct * 0.15  # small weight

    # PCR extreme correction (contrarian)
    if ctx.pcr > 1.5:
        score += 0.1   # extreme bearish sentiment → contrarian bullish
    elif ctx.pcr < 0.5:
        score -= 0.1   # extreme bullish sentiment → contrarian bearish

    if score > 0.3:
        return "bullish"
    elif score < -0.3:
        return "bearish"
    return "neutral"


def _generate_candidates(ctx: AnalysisContext, direction: str) -> list[StrategyRecommendation]:
    """Step 3: Generate candidate strategies based on direction."""
    candidates = []
    T = ctx.forecast_days / 365.0

    if direction == "bullish":
        # Sell Put (CSP)
        strike = _select_put_strike(ctx)
        if strike:
            candidates.append(_build_sell_put(ctx, strike, T))
        # Bull Put Spread
        short_strike = _select_put_strike(ctx)
        if short_strike:
            long_strike = short_strike - _spread_width(ctx)
            candidates.append(_build_bull_put_spread(ctx, short_strike, long_strike, T))

    elif direction == "bearish":
        # Bear Call Spread
        short_strike = _select_call_strike(ctx)
        if short_strike:
            long_strike = short_strike + _spread_width(ctx)
            candidates.append(_build_bear_call_spread(ctx, short_strike, long_strike, T))

    else:  # neutral
        # Iron Condor
        put_strike = _select_put_strike(ctx)
        call_strike = _select_call_strike(ctx)
        if put_strike and call_strike:
            width = _spread_width(ctx)
            candidates.append(_build_iron_condor(
                ctx, put_strike, put_strike - width, call_strike, call_strike + width, T
            ))
        # Short Strangle (higher risk)
        if ctx.risk_tolerance != "conservative" and put_strike and call_strike:
            candidates.append(_build_short_strangle(ctx, put_strike, call_strike, T))

    return [c for c in candidates if c is not None]


def _select_put_strike(ctx: AnalysisContext) -> float | None:
    """Select optimal put strike: support level + OI wall + delta target."""
    candidates = []

    # Nearest strong support
    if ctx.support_levels:
        candidates.append(ctx.support_levels[0]["price"])

    # Put OI wall
    if ctx.oi_put_walls:
        candidates.append(ctx.oi_put_walls[0][0])

    # Delta-based: ~20-30 delta (OTM)
    # Approximate using IV: strike ≈ price * (1 - delta_target * IV * sqrt(T))
    T = ctx.forecast_days / 365.0
    delta_target = 0.25 if ctx.risk_tolerance == "moderate" else (0.15 if ctx.risk_tolerance == "conservative" else 0.30)
    iv_based = ctx.current_price * (1 - delta_target * ctx.iv_current * np.sqrt(T) * 2)
    candidates.append(round(iv_based))

    if not candidates:
        return None

    # Pick the one closest to the median (balanced between all signals)
    median = np.median(candidates)
    # Round to nearest standard strike (assume $5 increments for most stocks)
    strike_increment = _guess_strike_increment(ctx.current_price)
    return round(median / strike_increment) * strike_increment


def _select_call_strike(ctx: AnalysisContext) -> float | None:
    """Select optimal call strike: resistance level + OI wall + delta target."""
    candidates = []

    if ctx.resistance_levels:
        candidates.append(ctx.resistance_levels[0]["price"])

    if ctx.oi_call_walls:
        candidates.append(ctx.oi_call_walls[0][0])

    T = ctx.forecast_days / 365.0
    delta_target = 0.25 if ctx.risk_tolerance == "moderate" else (0.15 if ctx.risk_tolerance == "conservative" else 0.30)
    iv_based = ctx.current_price * (1 + delta_target * ctx.iv_current * np.sqrt(T) * 2)
    candidates.append(round(iv_based))

    if not candidates:
        return None

    median = np.median(candidates)
    strike_increment = _guess_strike_increment(ctx.current_price)
    return round(median / strike_increment) * strike_increment


def _guess_strike_increment(price: float) -> float:
    """Guess the standard strike price increment."""
    if price < 50:
        return 1.0
    elif price < 200:
        return 2.5
    elif price < 500:
        return 5.0
    return 10.0


def _spread_width(ctx: AnalysisContext) -> float:
    """Determine spread width based on risk tolerance."""
    increment = _guess_strike_increment(ctx.current_price)
    if ctx.risk_tolerance == "conservative":
        return increment * 2
    elif ctx.risk_tolerance == "aggressive":
        return increment * 4
    return increment * 3  # moderate


def _estimate_premium(ctx: AnalysisContext, strike: float, option_type: str, T: float) -> float:
    """Rough premium estimate based on IV and moneyness (used before actual market data)."""
    from optix_engine.options.pricing import price as bs_price
    return bs_price(ctx.current_price, strike, T, 0.05, ctx.iv_current, option_type)


def _build_sell_put(ctx, strike, T) -> StrategyRecommendation:
    premium = _estimate_premium(ctx, strike, "put", T)
    max_profit = premium * 100
    max_loss = (strike - premium) * 100
    margin = strike * 100  # cash-secured
    breakeven = strike - premium

    # Prob OTM: approximate with delta
    from optix_engine.options.pricing import delta as bs_delta
    prob_otm = 1 - abs(bs_delta(ctx.current_price, strike, T, 0.05, ctx.iv_current, "put"))

    warnings = []
    if ctx.earnings_before_expiry:
        warnings.append(f"Earnings on {ctx.next_earnings_date} before expiry - expect high volatility")

    return StrategyRecommendation(
        strategy_name="Cash Secured Put",
        strategy_type="sell_put",
        legs=[{"option_type": "put", "strike": strike, "quantity": -1, "premium": premium}],
        max_profit=round(max_profit, 2),
        max_loss=round(max_loss, 2),
        risk_reward_ratio=round(max_profit / max_loss, 3) if max_loss > 0 else 0,
        margin_required=round(margin, 2),
        probability_of_profit=round(prob_otm * 100, 1),
        breakeven_price=round(breakeven, 2),
        net_credit=round(premium, 2),
        net_greeks={},
        score=0,
        rationale=f"Sell {strike} Put for ${premium:.2f} credit. Support at {ctx.support_levels[0]['price'] if ctx.support_levels else 'N/A'}.",
        risk_warnings=warnings,
    )


def _build_bull_put_spread(ctx, short_strike, long_strike, T) -> StrategyRecommendation:
    short_prem = _estimate_premium(ctx, short_strike, "put", T)
    long_prem = _estimate_premium(ctx, long_strike, "put", T)
    net_credit = short_prem - long_prem
    width = short_strike - long_strike
    max_profit = net_credit * 100
    max_loss = (width - net_credit) * 100
    margin = width * 100
    breakeven = short_strike - net_credit

    from optix_engine.options.pricing import delta as bs_delta
    prob_otm = 1 - abs(bs_delta(ctx.current_price, short_strike, T, 0.05, ctx.iv_current, "put"))

    warnings = []
    if ctx.earnings_before_expiry:
        warnings.append(f"Earnings on {ctx.next_earnings_date} before expiry")

    return StrategyRecommendation(
        strategy_name="Bull Put Spread",
        strategy_type="credit_spread",
        legs=[
            {"option_type": "put", "strike": short_strike, "quantity": -1, "premium": short_prem},
            {"option_type": "put", "strike": long_strike, "quantity": 1, "premium": long_prem},
        ],
        max_profit=round(max_profit, 2),
        max_loss=round(max_loss, 2),
        risk_reward_ratio=round(max_profit / max_loss, 3) if max_loss > 0 else 0,
        margin_required=round(margin, 2),
        probability_of_profit=round(prob_otm * 100, 1),
        breakeven_price=round(breakeven, 2),
        net_credit=round(net_credit, 2),
        net_greeks={},
        score=0,
        rationale=f"Bull Put Spread {long_strike}/{short_strike} for ${net_credit:.2f} credit.",
        risk_warnings=warnings,
    )


def _build_bear_call_spread(ctx, short_strike, long_strike, T) -> StrategyRecommendation:
    short_prem = _estimate_premium(ctx, short_strike, "call", T)
    long_prem = _estimate_premium(ctx, long_strike, "call", T)
    net_credit = short_prem - long_prem
    width = long_strike - short_strike
    max_profit = net_credit * 100
    max_loss = (width - net_credit) * 100
    margin = width * 100
    breakeven = short_strike + net_credit

    from optix_engine.options.pricing import delta as bs_delta
    prob_otm = 1 - bs_delta(ctx.current_price, short_strike, T, 0.05, ctx.iv_current, "call")

    warnings = []
    if ctx.earnings_before_expiry:
        warnings.append(f"Earnings on {ctx.next_earnings_date} before expiry")

    return StrategyRecommendation(
        strategy_name="Bear Call Spread",
        strategy_type="credit_spread",
        legs=[
            {"option_type": "call", "strike": short_strike, "quantity": -1, "premium": short_prem},
            {"option_type": "call", "strike": long_strike, "quantity": 1, "premium": long_prem},
        ],
        max_profit=round(max_profit, 2),
        max_loss=round(max_loss, 2),
        risk_reward_ratio=round(max_profit / max_loss, 3) if max_loss > 0 else 0,
        margin_required=round(margin, 2),
        probability_of_profit=round(prob_otm * 100, 1),
        breakeven_price=round(breakeven, 2),
        net_credit=round(net_credit, 2),
        net_greeks={},
        score=0,
        rationale=f"Bear Call Spread {short_strike}/{long_strike} for ${net_credit:.2f} credit.",
        risk_warnings=warnings,
    )


def _build_iron_condor(ctx, short_put, long_put, short_call, long_call, T) -> StrategyRecommendation:
    sp_prem = _estimate_premium(ctx, short_put, "put", T)
    lp_prem = _estimate_premium(ctx, long_put, "put", T)
    sc_prem = _estimate_premium(ctx, short_call, "call", T)
    lc_prem = _estimate_premium(ctx, long_call, "call", T)

    net_credit = (sp_prem - lp_prem) + (sc_prem - lc_prem)
    put_width = short_put - long_put
    call_width = long_call - short_call
    max_width = max(put_width, call_width)
    max_profit = net_credit * 100
    max_loss = (max_width - net_credit) * 100
    margin = max_width * 100

    # Breakeven prices (two-sided):
    #   lower BE = short_put  - net_credit
    #   upper BE = short_call + net_credit
    lower_be = short_put - net_credit
    upper_be = short_call + net_credit

    # Probability of profit: P(lower_be < S_T < upper_be) at expiry.
    # Approximated via BS delta (fast proxy for N(d2)):
    #   P(S_T < short_put)  ≈ |delta_put(short_put)|
    #   P(S_T > short_call) ≈  delta_call(short_call)
    #   P(profit) = 1 - P(below short_put) - P(above short_call)
    from optix_engine.options.pricing import delta as bs_delta
    prob_below = abs(bs_delta(ctx.current_price, short_put, T, 0.05, ctx.iv_current, "put"))
    prob_above = bs_delta(ctx.current_price, short_call, T, 0.05, ctx.iv_current, "call")
    prob_profit = round(max(0.0, 1.0 - prob_below - prob_above) * 100, 1)

    warnings = []
    if ctx.earnings_before_expiry:
        warnings.append(f"Earnings on {ctx.next_earnings_date} before expiry - Iron Condor is risky")

    return StrategyRecommendation(
        strategy_name="Iron Condor",
        strategy_type="iron_condor",
        legs=[
            {"option_type": "put", "strike": long_put, "quantity": 1, "premium": lp_prem},
            {"option_type": "put", "strike": short_put, "quantity": -1, "premium": sp_prem},
            {"option_type": "call", "strike": short_call, "quantity": -1, "premium": sc_prem},
            {"option_type": "call", "strike": long_call, "quantity": 1, "premium": lc_prem},
        ],
        max_profit=round(max_profit, 2),
        max_loss=round(max_loss, 2),
        risk_reward_ratio=round(max_profit / max_loss, 3) if max_loss > 0 else 0,
        margin_required=round(margin, 2),
        probability_of_profit=prob_profit,
        breakeven_price=round(lower_be, 2),  # lower BE (more conservative to display)
        net_credit=round(net_credit, 2),
        net_greeks={},
        score=0,
        rationale=(
            f"Iron Condor {long_put}/{short_put}/{short_call}/{long_call} for ${net_credit:.2f} credit. "
            f"Profitable between ${lower_be:.2f}–${upper_be:.2f}."
        ),
        risk_warnings=warnings,
    )


def _build_short_strangle(ctx, put_strike, call_strike, T) -> StrategyRecommendation:
    put_prem = _estimate_premium(ctx, put_strike, "put", T)
    call_prem = _estimate_premium(ctx, call_strike, "call", T)
    net_credit = put_prem + call_prem
    max_profit = net_credit * 100
    # Max loss is theoretically unlimited (short call side)
    max_loss = ctx.current_price * 100  # practical max loss estimate

    # Breakeven prices:  lower_be = put_strike - net_credit,  upper_be = call_strike + net_credit
    lower_be = put_strike - net_credit
    upper_be = call_strike + net_credit

    # Probability of profit: P(lower_be < S_T < upper_be) using BS delta proxy
    from optix_engine.options.pricing import delta as bs_delta
    prob_below = abs(bs_delta(ctx.current_price, put_strike, T, 0.05, ctx.iv_current, "put"))
    prob_above = bs_delta(ctx.current_price, call_strike, T, 0.05, ctx.iv_current, "call")
    prob_profit = round(max(0.0, 1.0 - prob_below - prob_above) * 100, 1)

    warnings = ["Short strangle has UNLIMITED risk on the call side"]
    if ctx.earnings_before_expiry:
        warnings.append(f"Earnings on {ctx.next_earnings_date} - extremely risky for naked strategies")

    return StrategyRecommendation(
        strategy_name="Short Strangle",
        strategy_type="short_strangle",
        legs=[
            {"option_type": "put", "strike": put_strike, "quantity": -1, "premium": put_prem},
            {"option_type": "call", "strike": call_strike, "quantity": -1, "premium": call_prem},
        ],
        max_profit=round(max_profit, 2),
        max_loss=round(max_loss, 2),
        risk_reward_ratio=round(max_profit / max_loss, 3) if max_loss > 0 else 0,
        margin_required=round(max_loss * 0.3, 2),  # rough margin estimate
        probability_of_profit=prob_profit,
        breakeven_price=round(lower_be, 2),
        net_credit=round(net_credit, 2),
        net_greeks={},
        score=0,
        rationale=f"Short Strangle {put_strike}P/{call_strike}C for ${net_credit:.2f} credit.",
        risk_warnings=warnings,
    )


def _passes_filters(rec: StrategyRecommendation, ctx: AnalysisContext) -> bool:
    """Step 4a: Filter out invalid strategies."""
    # Position size limit
    position_limit = {"conservative": 0.10, "moderate": 0.20, "aggressive": 0.30}
    max_position = ctx.available_capital * position_limit.get(ctx.risk_tolerance, 0.20)
    if rec.margin_required > max_position:
        return False

    # Minimum risk/reward
    min_rr = {"conservative": 0.25, "moderate": 0.15, "aggressive": 0.10}
    if rec.risk_reward_ratio < min_rr.get(ctx.risk_tolerance, 0.15):
        return False

    return True


def _calculate_score(rec: StrategyRecommendation, ctx: AnalysisContext) -> float:
    """Step 4b: Score a strategy recommendation (0-100)."""

    def normalize(val, min_val, max_val):
        if max_val <= min_val:
            return 0.5
        return max(0, min(1, (val - min_val) / (max_val - min_val)))

    prob_score = normalize(rec.probability_of_profit, 50, 95)  # 50-95% range
    rr_score = normalize(rec.risk_reward_ratio, 0.05, 0.5)     # 5-50% return range
    theta_score = normalize(rec.net_credit, 0, ctx.current_price * 0.03)
    iv_score = normalize(ctx.iv_rank, 30, 90)
    safety_score = normalize(
        abs(rec.breakeven_price - ctx.current_price) / ctx.current_price if rec.breakeven_price > 0 else 0,
        0, 0.10,
    )

    score = (
        prob_score * 30 +
        rr_score * 25 +
        theta_score * 20 +
        iv_score * 15 +
        safety_score * 10
    )

    return round(score, 1)


def _make_observation_recommendation(ctx: AnalysisContext, reason: str) -> StrategyRecommendation:
    """Return a 'watch/wait' recommendation when conditions aren't favorable."""
    return StrategyRecommendation(
        strategy_name="Observation - No Trade",
        strategy_type="none",
        legs=[],
        max_profit=0,
        max_loss=0,
        risk_reward_ratio=0,
        margin_required=0,
        probability_of_profit=0,
        breakeven_price=0,
        net_credit=0,
        net_greeks={},
        score=0,
        rationale=reason,
        risk_warnings=[],
    )
