from optix.marketdata.v1 import types_pb2 as _types_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class StockSummary(_message.Message):
    __slots__ = ("symbol", "price", "change", "change_pct", "high_52w", "low_52w", "avg_volume_20d", "today_volume")
    SYMBOL_FIELD_NUMBER: _ClassVar[int]
    PRICE_FIELD_NUMBER: _ClassVar[int]
    CHANGE_FIELD_NUMBER: _ClassVar[int]
    CHANGE_PCT_FIELD_NUMBER: _ClassVar[int]
    HIGH_52W_FIELD_NUMBER: _ClassVar[int]
    LOW_52W_FIELD_NUMBER: _ClassVar[int]
    AVG_VOLUME_20D_FIELD_NUMBER: _ClassVar[int]
    TODAY_VOLUME_FIELD_NUMBER: _ClassVar[int]
    symbol: str
    price: float
    change: float
    change_pct: float
    high_52w: float
    low_52w: float
    avg_volume_20d: float
    today_volume: int
    def __init__(self, symbol: _Optional[str] = ..., price: _Optional[float] = ..., change: _Optional[float] = ..., change_pct: _Optional[float] = ..., high_52w: _Optional[float] = ..., low_52w: _Optional[float] = ..., avg_volume_20d: _Optional[float] = ..., today_volume: _Optional[int] = ...) -> None: ...

class PriceLevel(_message.Message):
    __slots__ = ("price", "source", "strength")
    PRICE_FIELD_NUMBER: _ClassVar[int]
    SOURCE_FIELD_NUMBER: _ClassVar[int]
    STRENGTH_FIELD_NUMBER: _ClassVar[int]
    price: float
    source: str
    strength: float
    def __init__(self, price: _Optional[float] = ..., source: _Optional[str] = ..., strength: _Optional[float] = ...) -> None: ...

class TechnicalAnalysis(_message.Message):
    __slots__ = ("trend", "trend_score", "trend_description", "ma_20", "ma_50", "ma_200", "rsi_14", "macd", "macd_signal", "macd_histogram", "bollinger_upper", "bollinger_lower", "bollinger_mid", "support_levels", "resistance_levels")
    TREND_FIELD_NUMBER: _ClassVar[int]
    TREND_SCORE_FIELD_NUMBER: _ClassVar[int]
    TREND_DESCRIPTION_FIELD_NUMBER: _ClassVar[int]
    MA_20_FIELD_NUMBER: _ClassVar[int]
    MA_50_FIELD_NUMBER: _ClassVar[int]
    MA_200_FIELD_NUMBER: _ClassVar[int]
    RSI_14_FIELD_NUMBER: _ClassVar[int]
    MACD_FIELD_NUMBER: _ClassVar[int]
    MACD_SIGNAL_FIELD_NUMBER: _ClassVar[int]
    MACD_HISTOGRAM_FIELD_NUMBER: _ClassVar[int]
    BOLLINGER_UPPER_FIELD_NUMBER: _ClassVar[int]
    BOLLINGER_LOWER_FIELD_NUMBER: _ClassVar[int]
    BOLLINGER_MID_FIELD_NUMBER: _ClassVar[int]
    SUPPORT_LEVELS_FIELD_NUMBER: _ClassVar[int]
    RESISTANCE_LEVELS_FIELD_NUMBER: _ClassVar[int]
    trend: str
    trend_score: float
    trend_description: str
    ma_20: float
    ma_50: float
    ma_200: float
    rsi_14: float
    macd: float
    macd_signal: float
    macd_histogram: float
    bollinger_upper: float
    bollinger_lower: float
    bollinger_mid: float
    support_levels: _containers.RepeatedCompositeFieldContainer[PriceLevel]
    resistance_levels: _containers.RepeatedCompositeFieldContainer[PriceLevel]
    def __init__(self, trend: _Optional[str] = ..., trend_score: _Optional[float] = ..., trend_description: _Optional[str] = ..., ma_20: _Optional[float] = ..., ma_50: _Optional[float] = ..., ma_200: _Optional[float] = ..., rsi_14: _Optional[float] = ..., macd: _Optional[float] = ..., macd_signal: _Optional[float] = ..., macd_histogram: _Optional[float] = ..., bollinger_upper: _Optional[float] = ..., bollinger_lower: _Optional[float] = ..., bollinger_mid: _Optional[float] = ..., support_levels: _Optional[_Iterable[_Union[PriceLevel, _Mapping]]] = ..., resistance_levels: _Optional[_Iterable[_Union[PriceLevel, _Mapping]]] = ...) -> None: ...

class OICluster(_message.Message):
    __slots__ = ("strike", "option_type", "open_interest", "significance")
    STRIKE_FIELD_NUMBER: _ClassVar[int]
    OPTION_TYPE_FIELD_NUMBER: _ClassVar[int]
    OPEN_INTEREST_FIELD_NUMBER: _ClassVar[int]
    SIGNIFICANCE_FIELD_NUMBER: _ClassVar[int]
    strike: float
    option_type: _types_pb2.OptionType
    open_interest: int
    significance: str
    def __init__(self, strike: _Optional[float] = ..., option_type: _Optional[_Union[_types_pb2.OptionType, str]] = ..., open_interest: _Optional[int] = ..., significance: _Optional[str] = ...) -> None: ...

class OptionsAnalysis(_message.Message):
    __slots__ = ("iv_current", "iv_rank", "iv_percentile", "iv_environment", "iv_skew", "max_pain", "max_pain_expiry", "pcr_volume", "pcr_oi", "oi_clusters", "earnings_before_expiry", "next_earnings_date")
    IV_CURRENT_FIELD_NUMBER: _ClassVar[int]
    IV_RANK_FIELD_NUMBER: _ClassVar[int]
    IV_PERCENTILE_FIELD_NUMBER: _ClassVar[int]
    IV_ENVIRONMENT_FIELD_NUMBER: _ClassVar[int]
    IV_SKEW_FIELD_NUMBER: _ClassVar[int]
    MAX_PAIN_FIELD_NUMBER: _ClassVar[int]
    MAX_PAIN_EXPIRY_FIELD_NUMBER: _ClassVar[int]
    PCR_VOLUME_FIELD_NUMBER: _ClassVar[int]
    PCR_OI_FIELD_NUMBER: _ClassVar[int]
    OI_CLUSTERS_FIELD_NUMBER: _ClassVar[int]
    EARNINGS_BEFORE_EXPIRY_FIELD_NUMBER: _ClassVar[int]
    NEXT_EARNINGS_DATE_FIELD_NUMBER: _ClassVar[int]
    iv_current: float
    iv_rank: float
    iv_percentile: float
    iv_environment: str
    iv_skew: float
    max_pain: float
    max_pain_expiry: str
    pcr_volume: float
    pcr_oi: float
    oi_clusters: _containers.RepeatedCompositeFieldContainer[OICluster]
    earnings_before_expiry: bool
    next_earnings_date: str
    def __init__(self, iv_current: _Optional[float] = ..., iv_rank: _Optional[float] = ..., iv_percentile: _Optional[float] = ..., iv_environment: _Optional[str] = ..., iv_skew: _Optional[float] = ..., max_pain: _Optional[float] = ..., max_pain_expiry: _Optional[str] = ..., pcr_volume: _Optional[float] = ..., pcr_oi: _Optional[float] = ..., oi_clusters: _Optional[_Iterable[_Union[OICluster, _Mapping]]] = ..., earnings_before_expiry: bool = ..., next_earnings_date: _Optional[str] = ...) -> None: ...

class MarketOutlook(_message.Message):
    __slots__ = ("direction", "confidence", "rationale", "range_low_1s", "range_high_1s", "range_low_2s", "range_high_2s", "forecast_days", "risk_events")
    DIRECTION_FIELD_NUMBER: _ClassVar[int]
    CONFIDENCE_FIELD_NUMBER: _ClassVar[int]
    RATIONALE_FIELD_NUMBER: _ClassVar[int]
    RANGE_LOW_1S_FIELD_NUMBER: _ClassVar[int]
    RANGE_HIGH_1S_FIELD_NUMBER: _ClassVar[int]
    RANGE_LOW_2S_FIELD_NUMBER: _ClassVar[int]
    RANGE_HIGH_2S_FIELD_NUMBER: _ClassVar[int]
    FORECAST_DAYS_FIELD_NUMBER: _ClassVar[int]
    RISK_EVENTS_FIELD_NUMBER: _ClassVar[int]
    direction: str
    confidence: float
    rationale: str
    range_low_1s: float
    range_high_1s: float
    range_low_2s: float
    range_high_2s: float
    forecast_days: int
    risk_events: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, direction: _Optional[str] = ..., confidence: _Optional[float] = ..., rationale: _Optional[str] = ..., range_low_1s: _Optional[float] = ..., range_high_1s: _Optional[float] = ..., range_low_2s: _Optional[float] = ..., range_high_2s: _Optional[float] = ..., forecast_days: _Optional[int] = ..., risk_events: _Optional[_Iterable[str]] = ...) -> None: ...

class StrategyLeg(_message.Message):
    __slots__ = ("option_type", "strike", "expiration", "quantity", "premium")
    OPTION_TYPE_FIELD_NUMBER: _ClassVar[int]
    STRIKE_FIELD_NUMBER: _ClassVar[int]
    EXPIRATION_FIELD_NUMBER: _ClassVar[int]
    QUANTITY_FIELD_NUMBER: _ClassVar[int]
    PREMIUM_FIELD_NUMBER: _ClassVar[int]
    option_type: _types_pb2.OptionType
    strike: float
    expiration: str
    quantity: int
    premium: float
    def __init__(self, option_type: _Optional[_Union[_types_pb2.OptionType, str]] = ..., strike: _Optional[float] = ..., expiration: _Optional[str] = ..., quantity: _Optional[int] = ..., premium: _Optional[float] = ...) -> None: ...

class StrategyRecommendation(_message.Message):
    __slots__ = ("strategy_name", "strategy_type", "score", "legs", "max_profit", "max_loss", "risk_reward_ratio", "margin_required", "probability_of_profit", "breakeven_price", "net_credit", "rationale", "risk_warnings")
    STRATEGY_NAME_FIELD_NUMBER: _ClassVar[int]
    STRATEGY_TYPE_FIELD_NUMBER: _ClassVar[int]
    SCORE_FIELD_NUMBER: _ClassVar[int]
    LEGS_FIELD_NUMBER: _ClassVar[int]
    MAX_PROFIT_FIELD_NUMBER: _ClassVar[int]
    MAX_LOSS_FIELD_NUMBER: _ClassVar[int]
    RISK_REWARD_RATIO_FIELD_NUMBER: _ClassVar[int]
    MARGIN_REQUIRED_FIELD_NUMBER: _ClassVar[int]
    PROBABILITY_OF_PROFIT_FIELD_NUMBER: _ClassVar[int]
    BREAKEVEN_PRICE_FIELD_NUMBER: _ClassVar[int]
    NET_CREDIT_FIELD_NUMBER: _ClassVar[int]
    RATIONALE_FIELD_NUMBER: _ClassVar[int]
    RISK_WARNINGS_FIELD_NUMBER: _ClassVar[int]
    strategy_name: str
    strategy_type: str
    score: float
    legs: _containers.RepeatedCompositeFieldContainer[StrategyLeg]
    max_profit: float
    max_loss: float
    risk_reward_ratio: float
    margin_required: float
    probability_of_profit: float
    breakeven_price: float
    net_credit: float
    rationale: str
    risk_warnings: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, strategy_name: _Optional[str] = ..., strategy_type: _Optional[str] = ..., score: _Optional[float] = ..., legs: _Optional[_Iterable[_Union[StrategyLeg, _Mapping]]] = ..., max_profit: _Optional[float] = ..., max_loss: _Optional[float] = ..., risk_reward_ratio: _Optional[float] = ..., margin_required: _Optional[float] = ..., probability_of_profit: _Optional[float] = ..., breakeven_price: _Optional[float] = ..., net_credit: _Optional[float] = ..., rationale: _Optional[str] = ..., risk_warnings: _Optional[_Iterable[str]] = ...) -> None: ...

class StockQuickSummary(_message.Message):
    __slots__ = ("symbol", "price", "trend", "rsi", "iv_rank", "max_pain", "pcr", "range_low_1s", "range_high_1s", "recommendation", "opportunity_score")
    SYMBOL_FIELD_NUMBER: _ClassVar[int]
    PRICE_FIELD_NUMBER: _ClassVar[int]
    TREND_FIELD_NUMBER: _ClassVar[int]
    RSI_FIELD_NUMBER: _ClassVar[int]
    IV_RANK_FIELD_NUMBER: _ClassVar[int]
    MAX_PAIN_FIELD_NUMBER: _ClassVar[int]
    PCR_FIELD_NUMBER: _ClassVar[int]
    RANGE_LOW_1S_FIELD_NUMBER: _ClassVar[int]
    RANGE_HIGH_1S_FIELD_NUMBER: _ClassVar[int]
    RECOMMENDATION_FIELD_NUMBER: _ClassVar[int]
    OPPORTUNITY_SCORE_FIELD_NUMBER: _ClassVar[int]
    symbol: str
    price: float
    trend: str
    rsi: float
    iv_rank: float
    max_pain: float
    pcr: float
    range_low_1s: float
    range_high_1s: float
    recommendation: str
    opportunity_score: float
    def __init__(self, symbol: _Optional[str] = ..., price: _Optional[float] = ..., trend: _Optional[str] = ..., rsi: _Optional[float] = ..., iv_rank: _Optional[float] = ..., max_pain: _Optional[float] = ..., pcr: _Optional[float] = ..., range_low_1s: _Optional[float] = ..., range_high_1s: _Optional[float] = ..., recommendation: _Optional[str] = ..., opportunity_score: _Optional[float] = ...) -> None: ...

class SingleStockData(_message.Message):
    __slots__ = ("symbol", "quote", "historical_bars", "option_chain")
    SYMBOL_FIELD_NUMBER: _ClassVar[int]
    QUOTE_FIELD_NUMBER: _ClassVar[int]
    HISTORICAL_BARS_FIELD_NUMBER: _ClassVar[int]
    OPTION_CHAIN_FIELD_NUMBER: _ClassVar[int]
    symbol: str
    quote: _types_pb2.StockQuote
    historical_bars: _containers.RepeatedCompositeFieldContainer[_types_pb2.OHLCV]
    option_chain: _containers.RepeatedCompositeFieldContainer[_types_pb2.OptionChainExpiry]
    def __init__(self, symbol: _Optional[str] = ..., quote: _Optional[_Union[_types_pb2.StockQuote, _Mapping]] = ..., historical_bars: _Optional[_Iterable[_Union[_types_pb2.OHLCV, _Mapping]]] = ..., option_chain: _Optional[_Iterable[_Union[_types_pb2.OptionChainExpiry, _Mapping]]] = ...) -> None: ...
