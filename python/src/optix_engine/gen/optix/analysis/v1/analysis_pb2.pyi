from optix.marketdata.v1 import types_pb2 as _types_pb2
from optix.analysis.v1 import types_pb2 as _types_pb2_1
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class PriceOptionRequest(_message.Message):
    __slots__ = ("spot_price", "strike", "time_to_expiry", "risk_free_rate", "volatility", "dividend_yield", "option_type")
    SPOT_PRICE_FIELD_NUMBER: _ClassVar[int]
    STRIKE_FIELD_NUMBER: _ClassVar[int]
    TIME_TO_EXPIRY_FIELD_NUMBER: _ClassVar[int]
    RISK_FREE_RATE_FIELD_NUMBER: _ClassVar[int]
    VOLATILITY_FIELD_NUMBER: _ClassVar[int]
    DIVIDEND_YIELD_FIELD_NUMBER: _ClassVar[int]
    OPTION_TYPE_FIELD_NUMBER: _ClassVar[int]
    spot_price: float
    strike: float
    time_to_expiry: float
    risk_free_rate: float
    volatility: float
    dividend_yield: float
    option_type: _types_pb2.OptionType
    def __init__(self, spot_price: _Optional[float] = ..., strike: _Optional[float] = ..., time_to_expiry: _Optional[float] = ..., risk_free_rate: _Optional[float] = ..., volatility: _Optional[float] = ..., dividend_yield: _Optional[float] = ..., option_type: _Optional[_Union[_types_pb2.OptionType, str]] = ...) -> None: ...

class PriceOptionResponse(_message.Message):
    __slots__ = ("price", "greeks")
    PRICE_FIELD_NUMBER: _ClassVar[int]
    GREEKS_FIELD_NUMBER: _ClassVar[int]
    price: float
    greeks: _types_pb2.Greeks
    def __init__(self, price: _Optional[float] = ..., greeks: _Optional[_Union[_types_pb2.Greeks, _Mapping]] = ...) -> None: ...

class MaxPainRequest(_message.Message):
    __slots__ = ("underlying", "chain")
    UNDERLYING_FIELD_NUMBER: _ClassVar[int]
    CHAIN_FIELD_NUMBER: _ClassVar[int]
    underlying: str
    chain: _containers.RepeatedCompositeFieldContainer[_types_pb2.OptionChainExpiry]
    def __init__(self, underlying: _Optional[str] = ..., chain: _Optional[_Iterable[_Union[_types_pb2.OptionChainExpiry, _Mapping]]] = ...) -> None: ...

class MaxPainResponse(_message.Message):
    __slots__ = ("max_pain_price", "expiration")
    MAX_PAIN_PRICE_FIELD_NUMBER: _ClassVar[int]
    EXPIRATION_FIELD_NUMBER: _ClassVar[int]
    max_pain_price: float
    expiration: str
    def __init__(self, max_pain_price: _Optional[float] = ..., expiration: _Optional[str] = ...) -> None: ...

class AnalyzeStockRequest(_message.Message):
    __slots__ = ("symbol", "forecast_days", "available_capital", "risk_tolerance", "historical_bars", "option_chain", "current_quote")
    SYMBOL_FIELD_NUMBER: _ClassVar[int]
    FORECAST_DAYS_FIELD_NUMBER: _ClassVar[int]
    AVAILABLE_CAPITAL_FIELD_NUMBER: _ClassVar[int]
    RISK_TOLERANCE_FIELD_NUMBER: _ClassVar[int]
    HISTORICAL_BARS_FIELD_NUMBER: _ClassVar[int]
    OPTION_CHAIN_FIELD_NUMBER: _ClassVar[int]
    CURRENT_QUOTE_FIELD_NUMBER: _ClassVar[int]
    symbol: str
    forecast_days: int
    available_capital: float
    risk_tolerance: str
    historical_bars: _containers.RepeatedCompositeFieldContainer[_types_pb2.OHLCV]
    option_chain: _containers.RepeatedCompositeFieldContainer[_types_pb2.OptionChainExpiry]
    current_quote: _types_pb2.StockQuote
    def __init__(self, symbol: _Optional[str] = ..., forecast_days: _Optional[int] = ..., available_capital: _Optional[float] = ..., risk_tolerance: _Optional[str] = ..., historical_bars: _Optional[_Iterable[_Union[_types_pb2.OHLCV, _Mapping]]] = ..., option_chain: _Optional[_Iterable[_Union[_types_pb2.OptionChainExpiry, _Mapping]]] = ..., current_quote: _Optional[_Union[_types_pb2.StockQuote, _Mapping]] = ...) -> None: ...

class AnalyzeStockResponse(_message.Message):
    __slots__ = ("summary", "technical", "options", "outlook", "strategies")
    SUMMARY_FIELD_NUMBER: _ClassVar[int]
    TECHNICAL_FIELD_NUMBER: _ClassVar[int]
    OPTIONS_FIELD_NUMBER: _ClassVar[int]
    OUTLOOK_FIELD_NUMBER: _ClassVar[int]
    STRATEGIES_FIELD_NUMBER: _ClassVar[int]
    summary: _types_pb2_1.StockSummary
    technical: _types_pb2_1.TechnicalAnalysis
    options: _types_pb2_1.OptionsAnalysis
    outlook: _types_pb2_1.MarketOutlook
    strategies: _containers.RepeatedCompositeFieldContainer[_types_pb2_1.StrategyRecommendation]
    def __init__(self, summary: _Optional[_Union[_types_pb2_1.StockSummary, _Mapping]] = ..., technical: _Optional[_Union[_types_pb2_1.TechnicalAnalysis, _Mapping]] = ..., options: _Optional[_Union[_types_pb2_1.OptionsAnalysis, _Mapping]] = ..., outlook: _Optional[_Union[_types_pb2_1.MarketOutlook, _Mapping]] = ..., strategies: _Optional[_Iterable[_Union[_types_pb2_1.StrategyRecommendation, _Mapping]]] = ...) -> None: ...

class BatchQuickAnalysisRequest(_message.Message):
    __slots__ = ("stocks", "forecast_days", "available_capital")
    STOCKS_FIELD_NUMBER: _ClassVar[int]
    FORECAST_DAYS_FIELD_NUMBER: _ClassVar[int]
    AVAILABLE_CAPITAL_FIELD_NUMBER: _ClassVar[int]
    stocks: _containers.RepeatedCompositeFieldContainer[_types_pb2_1.SingleStockData]
    forecast_days: int
    available_capital: float
    def __init__(self, stocks: _Optional[_Iterable[_Union[_types_pb2_1.SingleStockData, _Mapping]]] = ..., forecast_days: _Optional[int] = ..., available_capital: _Optional[float] = ...) -> None: ...

class BatchQuickAnalysisResponse(_message.Message):
    __slots__ = ("summaries",)
    SUMMARIES_FIELD_NUMBER: _ClassVar[int]
    summaries: _containers.RepeatedCompositeFieldContainer[_types_pb2_1.StockQuickSummary]
    def __init__(self, summaries: _Optional[_Iterable[_Union[_types_pb2_1.StockQuickSummary, _Mapping]]] = ...) -> None: ...

class RecommendStrategiesRequest(_message.Message):
    __slots__ = ("symbol", "underlying_price", "available_capital", "risk_tolerance", "forecast_days", "technical", "options_analysis", "option_chain")
    SYMBOL_FIELD_NUMBER: _ClassVar[int]
    UNDERLYING_PRICE_FIELD_NUMBER: _ClassVar[int]
    AVAILABLE_CAPITAL_FIELD_NUMBER: _ClassVar[int]
    RISK_TOLERANCE_FIELD_NUMBER: _ClassVar[int]
    FORECAST_DAYS_FIELD_NUMBER: _ClassVar[int]
    TECHNICAL_FIELD_NUMBER: _ClassVar[int]
    OPTIONS_ANALYSIS_FIELD_NUMBER: _ClassVar[int]
    OPTION_CHAIN_FIELD_NUMBER: _ClassVar[int]
    symbol: str
    underlying_price: float
    available_capital: float
    risk_tolerance: str
    forecast_days: int
    technical: _types_pb2_1.TechnicalAnalysis
    options_analysis: _types_pb2_1.OptionsAnalysis
    option_chain: _containers.RepeatedCompositeFieldContainer[_types_pb2.OptionChainExpiry]
    def __init__(self, symbol: _Optional[str] = ..., underlying_price: _Optional[float] = ..., available_capital: _Optional[float] = ..., risk_tolerance: _Optional[str] = ..., forecast_days: _Optional[int] = ..., technical: _Optional[_Union[_types_pb2_1.TechnicalAnalysis, _Mapping]] = ..., options_analysis: _Optional[_Union[_types_pb2_1.OptionsAnalysis, _Mapping]] = ..., option_chain: _Optional[_Iterable[_Union[_types_pb2.OptionChainExpiry, _Mapping]]] = ...) -> None: ...

class RecommendStrategiesResponse(_message.Message):
    __slots__ = ("strategies",)
    STRATEGIES_FIELD_NUMBER: _ClassVar[int]
    strategies: _containers.RepeatedCompositeFieldContainer[_types_pb2_1.StrategyRecommendation]
    def __init__(self, strategies: _Optional[_Iterable[_Union[_types_pb2_1.StrategyRecommendation, _Mapping]]] = ...) -> None: ...

class SupportResistanceRequest(_message.Message):
    __slots__ = ("bars", "current_price")
    BARS_FIELD_NUMBER: _ClassVar[int]
    CURRENT_PRICE_FIELD_NUMBER: _ClassVar[int]
    bars: _containers.RepeatedCompositeFieldContainer[_types_pb2.OHLCV]
    current_price: float
    def __init__(self, bars: _Optional[_Iterable[_Union[_types_pb2.OHLCV, _Mapping]]] = ..., current_price: _Optional[float] = ...) -> None: ...

class SupportResistanceResponse(_message.Message):
    __slots__ = ("support_levels", "resistance_levels")
    SUPPORT_LEVELS_FIELD_NUMBER: _ClassVar[int]
    RESISTANCE_LEVELS_FIELD_NUMBER: _ClassVar[int]
    support_levels: _containers.RepeatedCompositeFieldContainer[_types_pb2_1.PriceLevel]
    resistance_levels: _containers.RepeatedCompositeFieldContainer[_types_pb2_1.PriceLevel]
    def __init__(self, support_levels: _Optional[_Iterable[_Union[_types_pb2_1.PriceLevel, _Mapping]]] = ..., resistance_levels: _Optional[_Iterable[_Union[_types_pb2_1.PriceLevel, _Mapping]]] = ...) -> None: ...
