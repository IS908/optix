"""AnalysisService gRPC servicer implementation."""

import optix_engine.gen  # fixes sys.path for proto imports

from optix.analysis.v1 import analysis_pb2, analysis_pb2_grpc, types_pb2
from optix.marketdata.v1 import types_pb2 as md_types

from optix_engine.options import pricing as bs
from optix_engine.options.implied_vol import implied_volatility
from optix_engine.options.max_pain import calculate_max_pain
from optix_engine.options.open_interest import find_oi_walls, put_call_ratio, detect_unusual_activity
from optix_engine.technical.indicators import compute_all_indicators
from optix_engine.technical.support_resistance import find_all_levels
from optix_engine.strategy.recommender import AnalysisContext, recommend_strategies, StrategyRecommendation

import grpc
import numpy as np
import pandas as pd

# ---------------------------------------------------------------------------
# Empirical IV correction factor
# ---------------------------------------------------------------------------
# IB's structure-only option chain (reqSecDefOptParams) provides strikes and
# expirations but NOT live bid/ask prices, so we use HV20 as an IV proxy.
# Cross-verification against alphaquery.com (COIN, 2026-03-13):
#   HV20 = 93.6%  vs  30-day market IV = 70.25%  → ratio = 0.75
# This ratio is consistent with literature for volatile individual equities
# in non-event periods (typical range 0.70–0.85).  Apply as a haircut so
# premium estimates and Greek-based probabilities reflect realistic market IV.
#
# NOTE: IV rank / percentile are computed from the HV20 series itself and
# remain self-consistent (no adjustment needed there).
_IV_HV_RATIO: float = 0.75


class AnalysisServicer(analysis_pb2_grpc.AnalysisServiceServicer):
    """Implements the AnalysisService gRPC interface."""

    # ─── PriceOption ──────────────────────────────────────────────────────────

    def PriceOption(self, request, context):
        S = request.spot_price
        K = request.strike
        T = request.time_to_expiry
        r = request.risk_free_rate
        sigma = request.volatility
        q = request.dividend_yield
        opt_type = "call" if request.option_type == md_types.OPTION_TYPE_CALL else "put"

        if T <= 0 or sigma <= 0 or S <= 0 or K <= 0:
            context.set_code(grpc.StatusCode.INVALID_ARGUMENT)
            context.set_details("spot_price, strike, time_to_expiry, and volatility must be > 0")
            return analysis_pb2.PriceOptionResponse()

        price = bs.price(S, K, T, r, sigma, opt_type, q)
        greeks = bs.all_greeks(S, K, T, r, sigma, opt_type, q)

        return analysis_pb2.PriceOptionResponse(
            price=price,
            greeks=md_types.Greeks(
                delta=greeks["delta"],
                gamma=greeks["gamma"],
                theta=greeks["theta"],
                vega=greeks["vega"],
                rho=greeks["rho"],
            ),
        )

    # ─── GetMaxPain ───────────────────────────────────────────────────────────

    def GetMaxPain(self, request, context):
        if not request.chain:
            context.set_code(grpc.StatusCode.INVALID_ARGUMENT)
            context.set_details("option chain is required")
            return analysis_pb2.MaxPainResponse()

        # Use the first (nearest) expiry
        expiry = request.chain[0]
        strikes, call_oi, put_oi = [], [], []

        # Build OI lists from chain
        call_map = {c.strike: c.open_interest for c in expiry.calls}
        put_map = {p.strike: p.open_interest for p in expiry.puts}
        all_strikes = sorted(set(call_map) | set(put_map))

        for s in all_strikes:
            strikes.append(s)
            call_oi.append(call_map.get(s, 0))
            put_oi.append(put_map.get(s, 0))

        max_pain = calculate_max_pain(strikes, call_oi, put_oi)
        return analysis_pb2.MaxPainResponse(
            max_pain_price=max_pain,
            expiration=expiry.expiration,
        )

    # ─── AnalyzeStock ─────────────────────────────────────────────────────────

    def AnalyzeStock(self, request, context):
        try:
            return self._analyze_stock_impl(request)
        except Exception as e:
            import traceback
            traceback.print_exc()
            context.set_code(grpc.StatusCode.INTERNAL)
            context.set_details(f"Analysis failed: {e}")
            return analysis_pb2.AnalyzeStockResponse()

    def _analyze_stock_impl(self, request):
        symbol = request.symbol
        forecast_days = request.forecast_days or 14
        capital = request.available_capital or 50000.0
        risk_tolerance = request.risk_tolerance or "moderate"

        if not request.historical_bars:
            raise ValueError("No historical bars provided")

        # 1. Convert bars to DataFrame and compute indicators
        df = _bars_to_dataframe(request.historical_bars)
        compute_all_indicators(df)

        # 2. Current price
        current_price = (
            request.current_quote.last
            if request.current_quote and request.current_quote.last > 0
            else float(df.iloc[-1]["close"])
        )

        # 3. Historical volatility (used as IV proxy since IB chain has no live prices)
        hv20 = _compute_hv(df, 20)
        hv_series = _compute_rolling_hv(df, 20)
        iv_rank, iv_percentile = _compute_iv_rank_percentile(hv_series, hv20)

        # Empirical IV correction: for volatile stocks, market IV ≈ HV20 × 0.75.
        # Cross-verification against alphaquery.com (COIN 3/13/2026):
        #   HV20=93.6% vs market IV=70.25%  → IV/HV ratio = 0.75.
        # This corrects premium overestimation when using raw HV as IV proxy.
        # IV rank / percentile stay on the raw HV20 series (self-consistent relative measure).
        iv_for_pricing = hv20 * _IV_HV_RATIO

        # IV environment
        if iv_rank >= 50:
            iv_env = "high"
        elif iv_rank >= 30:
            iv_env = "medium"
        else:
            iv_env = "low"

        # 4. Technical indicators
        last = df.iloc[-1]
        rsi_val = _safe_float(last.get("rsi_14")) if not pd.isna(last.get("rsi_14", float("nan"))) else 50.0
        trend_score, trend = _compute_trend_score(df, current_price)
        trend_description = _build_trend_description(df, current_price, trend_score)

        # 5. Option chain analysis
        max_pain = 0.0
        max_pain_expiry = ""
        pcr_oi = 1.0
        pcr_vol = 1.0
        oi_walls = {"put_walls": [], "call_walls": []}
        oi_clusters_proto = []
        default_expiry = ""
        option_chain_list = list(request.option_chain)

        if option_chain_list:
            # OI / Max Pain: use nearest expiry (most liquid)
            expiry = option_chain_list[0]
            max_pain_expiry = expiry.expiration

            # Strategy legs: pick the expiry closest to forecast_days (covers the forecast period).
            # Prefer the first expiry whose DTE >= half the forecast horizon so the
            # strategy has meaningful time value; fall back to the nearest if none qualify.
            target_dte = max(forecast_days, 7)
            strategy_expiry = option_chain_list[0]
            for e in option_chain_list:
                if e.days_to_expiry >= target_dte // 2:
                    strategy_expiry = e
                    break
            default_expiry = strategy_expiry.expiration

            # Build OI maps
            call_map = {c.strike: c.open_interest for c in expiry.calls}
            put_map = {p.strike: p.open_interest for p in expiry.puts}
            all_strikes_sorted = sorted(set(call_map) | set(put_map))

            if all_strikes_sorted:
                call_oi_list = [call_map.get(s, 0) for s in all_strikes_sorted]
                put_oi_list = [put_map.get(s, 0) for s in all_strikes_sorted]
                total_oi = sum(call_oi_list) + sum(put_oi_list)

                if total_oi > 0:
                    max_pain = calculate_max_pain(all_strikes_sorted, call_oi_list, put_oi_list)

                # Aggregate chain DataFrame by strike for OI walls / PCR
                agg_rows = [
                    {"strike": s, "call_oi": call_map.get(s, 0), "put_oi": put_map.get(s, 0)}
                    for s in all_strikes_sorted
                ]
                agg_df = pd.DataFrame(agg_rows)

                # Flat rows for PCR computation
                pcr_rows = (
                    [{"option_type": "C", "open_interest": c.open_interest, "volume": 0}
                     for c in expiry.calls]
                    + [{"option_type": "P", "open_interest": p.open_interest, "volume": 0}
                       for p in expiry.puts]
                )
                pcr_df = pd.DataFrame(pcr_rows)

                if total_oi > 0:
                    oi_walls = find_oi_walls(agg_df)
                    pcr_oi = put_call_ratio(pcr_df, by="oi")
                    pcr_vol = pcr_oi  # volume data not available from IB structure-only chain

                    # Build OI cluster protos
                    for strike, oi in oi_walls.get("put_walls", [])[:3]:
                        oi_clusters_proto.append(types_pb2.OICluster(
                            strike=float(strike),
                            option_type=md_types.OPTION_TYPE_PUT,
                            open_interest=int(oi),
                            significance="support_wall",
                        ))
                    for strike, oi in oi_walls.get("call_walls", [])[:3]:
                        oi_clusters_proto.append(types_pb2.OICluster(
                            strike=float(strike),
                            option_type=md_types.OPTION_TYPE_CALL,
                            open_interest=int(oi),
                            significance="resistance_wall",
                        ))

        # 6. Support and resistance levels
        oi_walls_for_levels = oi_walls if (oi_walls.get("put_walls") or oi_walls.get("call_walls")) else None
        support_levels, resistance_levels = find_all_levels(
            df, current_price, oi_walls_for_levels, max_pain if max_pain > 0 else None
        )

        # 7. Price range forecast (IV-based statistical range)
        T = forecast_days / 365.0
        price_move_1s = iv_for_pricing * current_price * np.sqrt(T)
        range_low_1s = max(current_price - price_move_1s, 0.01)
        range_high_1s = current_price + price_move_1s
        range_low_2s = max(current_price - 2 * price_move_1s, 0.01)
        range_high_2s = current_price + 2 * price_move_1s

        # 8. Build AnalysisContext and get strategy recommendations
        ctx = AnalysisContext(
            symbol=symbol,
            current_price=current_price,
            available_capital=capital,
            risk_tolerance=risk_tolerance,
            forecast_days=forecast_days,
            trend=trend,
            trend_score=trend_score,
            rsi=rsi_val,
            support_levels=support_levels,
            resistance_levels=resistance_levels,
            iv_rank=iv_rank,
            iv_percentile=iv_percentile,
            iv_current=iv_for_pricing,   # corrected: HV20 × 0.75 ≈ market IV
            iv_skew=0.0,
            max_pain=max_pain,
            pcr=pcr_oi,
            oi_put_walls=oi_walls.get("put_walls", []),
            oi_call_walls=oi_walls.get("call_walls", []),
            earnings_before_expiry=False,
            next_earnings_date=None,
        )
        strategies = recommend_strategies(ctx)

        # 9. Outlook confidence and rationale
        confidence = min(abs(trend_score) * 80.0 + iv_rank * 0.2, 100.0)
        outlook_rationale = _build_outlook_rationale(
            trend, trend_score, iv_rank, iv_env, max_pain, current_price, pcr_oi
        )

        # 10. Summary fields
        hi52w = float(df["high"].max())
        lo52w = float(df["low"].min())
        avg_vol_20 = float(df["volume"].tail(20).mean())
        today_vol = int(df.iloc[-1]["volume"])

        change = 0.0
        change_pct = 0.0
        if request.current_quote:
            change = request.current_quote.change
            change_pct = request.current_quote.change_pct
        elif len(df) >= 2:
            prev_close = float(df.iloc[-2]["close"])
            if prev_close > 0:
                change = current_price - prev_close
                change_pct = change / prev_close * 100.0

        return analysis_pb2.AnalyzeStockResponse(
            summary=types_pb2.StockSummary(
                symbol=symbol,
                price=current_price,
                change=change,
                change_pct=change_pct,
                high_52w=hi52w,
                low_52w=lo52w,
                avg_volume_20d=avg_vol_20,
                today_volume=today_vol,
            ),
            technical=types_pb2.TechnicalAnalysis(
                trend=trend,
                trend_score=trend_score,
                trend_description=trend_description,
                ma_20=_safe_float(last.get("ma_20")),
                ma_50=_safe_float(last.get("ma_50")),
                ma_200=_safe_float(last.get("ma_200")),
                rsi_14=rsi_val,
                macd=_safe_float(last.get("macd")),
                macd_signal=_safe_float(last.get("macd_signal")),
                macd_histogram=_safe_float(last.get("macd_histogram")),
                bollinger_upper=_safe_float(last.get("bb_upper")),
                bollinger_lower=_safe_float(last.get("bb_lower")),
                bollinger_mid=_safe_float(last.get("bb_mid")),
                support_levels=[
                    types_pb2.PriceLevel(
                        price=sl["price"], source=sl["source"], strength=sl["strength"]
                    )
                    for sl in support_levels[:6]
                ],
                resistance_levels=[
                    types_pb2.PriceLevel(
                        price=rl["price"], source=rl["source"], strength=rl["strength"]
                    )
                    for rl in resistance_levels[:6]
                ],
            ),
            options=types_pb2.OptionsAnalysis(
                iv_current=iv_for_pricing,   # corrected: HV20 × 0.75 ≈ market IV
                iv_rank=iv_rank,
                iv_percentile=iv_percentile,
                iv_environment=iv_env,
                iv_skew=0.0,
                max_pain=max_pain,
                max_pain_expiry=max_pain_expiry,
                pcr_volume=pcr_vol,
                pcr_oi=pcr_oi,
                oi_clusters=oi_clusters_proto,
            ),
            outlook=types_pb2.MarketOutlook(
                direction=trend,
                confidence=round(confidence, 1),
                rationale=outlook_rationale,
                range_low_1s=round(range_low_1s, 2),
                range_high_1s=round(range_high_1s, 2),
                range_low_2s=round(range_low_2s, 2),
                range_high_2s=round(range_high_2s, 2),
                forecast_days=forecast_days,
            ),
            strategies=[
                _strategy_to_proto(s, default_expiry) for s in strategies
            ],
        )

    # ─── BatchQuickAnalysis ───────────────────────────────────────────────────

    def BatchQuickAnalysis(self, request, context):
        summaries = []
        for stock_data in request.stocks:
            try:
                summary = self._quick_analyze_one(
                    stock_data,
                    request.forecast_days or 14,
                    request.available_capital or 50000.0,
                )
                summaries.append(summary)
            except Exception as e:
                import traceback
                traceback.print_exc()
                summaries.append(types_pb2.StockQuickSummary(
                    symbol=stock_data.symbol,
                    recommendation=f"Error: {e}",
                ))
        return analysis_pb2.BatchQuickAnalysisResponse(summaries=summaries)

    def _quick_analyze_one(self, stock_data, forecast_days, capital):
        symbol = stock_data.symbol
        if not stock_data.historical_bars:
            return types_pb2.StockQuickSummary(symbol=symbol, recommendation="No historical data")

        df = _bars_to_dataframe(stock_data.historical_bars)
        compute_all_indicators(df)

        current_price = (
            stock_data.quote.last
            if stock_data.quote and stock_data.quote.last > 0
            else float(df.iloc[-1]["close"])
        )

        hv20 = _compute_hv(df, 20)
        hv_series = _compute_rolling_hv(df, 20)
        iv_rank, _ = _compute_iv_rank_percentile(hv_series, hv20)
        iv_for_pricing = hv20 * _IV_HV_RATIO  # market IV ≈ HV20 × 0.75

        rsi_val = _safe_float(df.iloc[-1].get("rsi_14")) if not pd.isna(df.iloc[-1].get("rsi_14", float("nan"))) else 50.0
        trend_score, trend = _compute_trend_score(df, current_price)

        max_pain = 0.0
        pcr_oi = 1.0
        option_chain_list = list(stock_data.option_chain)
        if option_chain_list:
            expiry = option_chain_list[0]
            call_map = {c.strike: c.open_interest for c in expiry.calls}
            put_map = {p.strike: p.open_interest for p in expiry.puts}
            strikes = sorted(set(call_map) | set(put_map))
            if strikes:
                call_oi_l = [call_map.get(s, 0) for s in strikes]
                put_oi_l = [put_map.get(s, 0) for s in strikes]
                if sum(call_oi_l) + sum(put_oi_l) > 0:
                    max_pain = calculate_max_pain(strikes, call_oi_l, put_oi_l)
                pcr_rows = (
                    [{"option_type": "C", "open_interest": c.open_interest} for c in expiry.calls]
                    + [{"option_type": "P", "open_interest": p.open_interest} for p in expiry.puts]
                )
                if pcr_rows:
                    pcr_df = pd.DataFrame(pcr_rows)
                    if pcr_df["open_interest"].sum() > 0:
                        pcr_oi = put_call_ratio(pcr_df, by="oi")

        T = forecast_days / 365.0
        price_move = iv_for_pricing * current_price * np.sqrt(T)
        range_low = max(current_price - price_move, 0.01)
        range_high = current_price + price_move

        # Quick recommendation based on IV + direction
        if iv_rank < 30:
            recommendation = "Wait (Low IV)"
            opportunity_score = iv_rank * 0.5
        elif trend == "bullish":
            recommendation = "★ Sell Put / Bull Put Spread"
            opportunity_score = min(iv_rank * 0.6 + abs(trend_score) * 40.0, 100.0)
        elif trend == "bearish":
            recommendation = "★ Bear Call Spread"
            opportunity_score = min(iv_rank * 0.6 + abs(trend_score) * 40.0, 100.0)
        else:
            recommendation = "★ Iron Condor"
            opportunity_score = min(iv_rank * 0.8, 100.0)

        return types_pb2.StockQuickSummary(
            symbol=symbol,
            price=current_price,
            trend=trend,
            rsi=rsi_val,
            iv_rank=iv_rank,
            max_pain=max_pain,
            pcr=pcr_oi,
            range_low_1s=round(range_low, 2),
            range_high_1s=round(range_high, 2),
            recommendation=recommendation,
            opportunity_score=round(opportunity_score, 1),
        )

    # ─── RecommendStrategies ──────────────────────────────────────────────────

    def RecommendStrategies(self, request, context):
        tech = request.technical
        opts = request.options_analysis

        support = [
            {"price": l.price, "source": l.source, "strength": l.strength}
            for l in tech.support_levels
        ]
        resistance = [
            {"price": l.price, "source": l.source, "strength": l.strength}
            for l in tech.resistance_levels
        ]

        ctx = AnalysisContext(
            symbol=request.symbol,
            current_price=request.underlying_price,
            available_capital=request.available_capital or 50000.0,
            risk_tolerance=request.risk_tolerance or "moderate",
            forecast_days=request.forecast_days or 14,
            trend=tech.trend,
            trend_score=tech.trend_score,
            rsi=tech.rsi_14,
            support_levels=support,
            resistance_levels=resistance,
            iv_rank=opts.iv_rank,
            iv_percentile=opts.iv_percentile,
            iv_current=opts.iv_current,
            iv_skew=opts.iv_skew,
            max_pain=opts.max_pain,
            pcr=opts.pcr_oi,
            oi_put_walls=[],
            oi_call_walls=[],
            earnings_before_expiry=opts.earnings_before_expiry,
            next_earnings_date=opts.next_earnings_date if opts.next_earnings_date else None,
        )

        strategies = recommend_strategies(ctx)
        default_expiry = request.option_chain[0].expiration if request.option_chain else ""
        return analysis_pb2.RecommendStrategiesResponse(
            strategies=[_strategy_to_proto(s, default_expiry) for s in strategies]
        )

    # ─── GetSupportResistance ─────────────────────────────────────────────────

    def GetSupportResistance(self, request, context):
        if not request.bars:
            context.set_code(grpc.StatusCode.INVALID_ARGUMENT)
            context.set_details("bars are required")
            return analysis_pb2.SupportResistanceResponse()

        df = _bars_to_dataframe(request.bars)
        compute_all_indicators(df)
        support, resistance = find_all_levels(df, request.current_price)

        return analysis_pb2.SupportResistanceResponse(
            support_levels=[
                types_pb2.PriceLevel(price=l["price"], source=l["source"], strength=l["strength"])
                for l in support[:10]
            ],
            resistance_levels=[
                types_pb2.PriceLevel(price=l["price"], source=l["source"], strength=l["strength"])
                for l in resistance[:10]
            ],
        )


# ─── Helper functions ─────────────────────────────────────────────────────────

def _bars_to_dataframe(bars) -> pd.DataFrame:
    """Convert proto OHLCV repeated field to pandas DataFrame."""
    rows = [
        {"open": b.open, "high": b.high, "low": b.low, "close": b.close, "volume": b.volume}
        for b in bars
    ]
    return pd.DataFrame(rows)


def _compute_hv(df: pd.DataFrame, period: int = 20) -> float:
    """Compute annualized historical volatility (HV) over the last `period` trading days."""
    closes = df["close"].values.astype(float)
    if len(closes) < 2:
        return 0.30  # default 30%
    log_returns = np.log(closes[1:] / np.where(closes[:-1] > 0, closes[:-1], 1e-9))
    n = min(period, len(log_returns))
    recent = log_returns[-n:]
    hv = float(np.std(recent, ddof=1) * np.sqrt(252))
    return max(hv, 0.05)  # floor at 5%


def _compute_rolling_hv(df: pd.DataFrame, period: int = 20) -> pd.Series:
    """Compute rolling annualized historical volatility series."""
    closes = df["close"].replace(0, np.nan)
    log_returns = np.log(closes / closes.shift(1))
    rolling_std = log_returns.rolling(window=period).std()
    return (rolling_std * np.sqrt(252)).dropna()


def _compute_iv_rank_percentile(hv_series: pd.Series, current_hv: float) -> tuple:
    """Compute IV Rank and IV Percentile from historical HV series."""
    if len(hv_series) < 5:
        return 50.0, 50.0
    hv_min = float(hv_series.min())
    hv_max = float(hv_series.max())
    if hv_max <= hv_min:
        return 50.0, 50.0
    iv_rank = (current_hv - hv_min) / (hv_max - hv_min) * 100.0
    iv_percentile = float((hv_series < current_hv).sum() / len(hv_series) * 100.0)
    return round(float(np.clip(iv_rank, 0, 100)), 1), round(iv_percentile, 1)


def _compute_trend_score(df: pd.DataFrame, current_price: float) -> tuple:
    """Compute weighted trend score (-1 to 1) and direction label."""
    last = df.iloc[-1]

    # MA signals: price vs each MA + MA crossovers
    ma20 = last.get("ma_20", float("nan"))
    ma50 = last.get("ma_50", float("nan"))
    ma200 = last.get("ma_200", float("nan"))

    ma_signals = []
    if not pd.isna(ma20):
        ma_signals.append(1.0 if current_price > float(ma20) else -1.0)
    if not pd.isna(ma50):
        ma_signals.append(1.0 if current_price > float(ma50) else -1.0)
        if not pd.isna(ma20):
            ma_signals.append(0.5 if float(ma20) > float(ma50) else -0.5)
    if not pd.isna(ma200):
        ma_signals.append(1.0 if current_price > float(ma200) else -1.0)
    ma_signal = float(np.mean(ma_signals)) if ma_signals else 0.0

    # MACD signal: histogram direction
    macd_val = last.get("macd", float("nan"))
    macd_hist = last.get("macd_histogram", float("nan"))
    if not pd.isna(macd_val) and not pd.isna(macd_hist):
        denominator = abs(float(macd_val)) + 1e-6
        macd_signal_val = float(np.clip(float(macd_hist) / denominator, -1.0, 1.0))
    else:
        macd_signal_val = 0.0

    # RSI signal: centered around 50, overbought/oversold at extremes
    rsi_val = last.get("rsi_14", float("nan"))
    if not pd.isna(rsi_val):
        r = float(rsi_val)
        if r > 70:
            rsi_signal = -0.5  # overbought → slightly bearish
        elif r < 30:
            rsi_signal = 0.5   # oversold → slightly bullish
        else:
            rsi_signal = (r - 50.0) / 50.0 * 0.5
    else:
        rsi_signal = 0.0

    # Volume signal: is volume confirming the price direction?
    vol_signal = 0.0
    if len(df) >= 10:
        recent_vol = float(df["volume"].tail(5).mean())
        avg_vol = float(df["volume"].tail(20).mean())
        if avg_vol > 0:
            vol_ratio = recent_vol / avg_vol
            price_up = float(last["close"]) > float(df.iloc[-6]["close"])
            if vol_ratio > 1.2:
                vol_signal = 0.3 if price_up else -0.3
            elif vol_ratio < 0.8:
                vol_signal = -0.1 if price_up else 0.1

    score = (
        ma_signal      * 0.35
        + macd_signal_val * 0.25
        + rsi_signal      * 0.20
        + vol_signal      * 0.20
    )
    score = float(np.clip(score, -1.0, 1.0))

    if score > 0.30:
        direction = "bullish"
    elif score < -0.30:
        direction = "bearish"
    else:
        direction = "neutral"

    return round(score, 3), direction


def _build_trend_description(df: pd.DataFrame, current_price: float, trend_score: float) -> str:
    """Build a human-readable trend summary."""
    last = df.iloc[-1]
    parts = []

    ma20 = last.get("ma_20", float("nan"))
    ma50 = last.get("ma_50", float("nan"))
    ma200 = last.get("ma_200", float("nan"))

    if not pd.isna(ma20) and not pd.isna(ma50):
        cross = "above" if float(ma20) > float(ma50) else "below"
        parts.append(f"MA20 {cross} MA50")
    if not pd.isna(ma200):
        pos = "above" if current_price > float(ma200) else "below"
        parts.append(f"price {pos} MA200")

    rsi = last.get("rsi_14", float("nan"))
    if not pd.isna(rsi):
        r = float(rsi)
        if r > 70:
            parts.append(f"RSI {r:.0f} (overbought)")
        elif r < 30:
            parts.append(f"RSI {r:.0f} (oversold)")
        else:
            parts.append(f"RSI {r:.0f}")

    macd_hist = last.get("macd_histogram", float("nan"))
    if not pd.isna(macd_hist):
        parts.append(f"MACD hist {'positive' if float(macd_hist) > 0 else 'negative'}")

    return "; ".join(parts) if parts else f"trend score {trend_score:+.2f}"


def _build_outlook_rationale(trend, trend_score, iv_rank, iv_env, max_pain, current_price, pcr) -> str:
    """Build a concise human-readable outlook rationale."""
    parts = [
        f"Trend: {trend} (score {trend_score:+.2f})",
        f"IV Rank: {iv_rank:.0f}% ({iv_env})",
    ]
    if max_pain > 0:
        mp_dir = "above" if max_pain > current_price else "below"
        parts.append(f"Max Pain ${max_pain:.2f} is {mp_dir} current price")
    if pcr > 1.3:
        parts.append(f"PCR {pcr:.2f} (elevated put buying — contrarian bullish signal)")
    elif pcr < 0.7:
        parts.append(f"PCR {pcr:.2f} (low put buying — market may be complacent)")
    else:
        parts.append(f"PCR {pcr:.2f} (neutral sentiment)")
    return ". ".join(parts) + "."


def _safe_float(val) -> float:
    """Return float, replacing NaN/None with 0.0."""
    if val is None:
        return 0.0
    try:
        f = float(val)
        return 0.0 if np.isnan(f) or np.isinf(f) else f
    except (TypeError, ValueError):
        return 0.0


def _strategy_to_proto(s: StrategyRecommendation, default_expiry: str) -> types_pb2.StrategyRecommendation:
    """Convert Python StrategyRecommendation dataclass → proto message."""
    legs = []
    for leg in s.legs:
        ot = (
            md_types.OPTION_TYPE_CALL
            if str(leg.get("option_type", "")).lower() in ("call", "c")
            else md_types.OPTION_TYPE_PUT
        )
        legs.append(types_pb2.StrategyLeg(
            option_type=ot,
            strike=float(leg.get("strike", 0.0)),
            expiration=str(leg.get("expiration", default_expiry)),
            quantity=int(leg.get("quantity", 0)),
            premium=float(leg.get("premium", 0.0)),
        ))

    return types_pb2.StrategyRecommendation(
        strategy_name=s.strategy_name,
        strategy_type=s.strategy_type,
        score=float(s.score),
        legs=legs,
        max_profit=float(s.max_profit),
        max_loss=float(s.max_loss),
        risk_reward_ratio=float(s.risk_reward_ratio),
        margin_required=float(s.margin_required),
        probability_of_profit=float(s.probability_of_profit),
        breakeven_price=float(s.breakeven_price),
        net_credit=float(s.net_credit),
        rationale=s.rationale,
        risk_warnings=list(s.risk_warnings),
    )
