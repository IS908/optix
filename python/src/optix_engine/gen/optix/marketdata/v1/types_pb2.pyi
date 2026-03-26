import datetime

from google.protobuf import timestamp_pb2 as _timestamp_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class OptionType(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    OPTION_TYPE_UNSPECIFIED: _ClassVar[OptionType]
    OPTION_TYPE_CALL: _ClassVar[OptionType]
    OPTION_TYPE_PUT: _ClassVar[OptionType]

class MarketSession(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    MARKET_SESSION_UNSPECIFIED: _ClassVar[MarketSession]
    MARKET_SESSION_PRE_MARKET: _ClassVar[MarketSession]
    MARKET_SESSION_REGULAR: _ClassVar[MarketSession]
    MARKET_SESSION_POST_MARKET: _ClassVar[MarketSession]
    MARKET_SESSION_CLOSED: _ClassVar[MarketSession]
OPTION_TYPE_UNSPECIFIED: OptionType
OPTION_TYPE_CALL: OptionType
OPTION_TYPE_PUT: OptionType
MARKET_SESSION_UNSPECIFIED: MarketSession
MARKET_SESSION_PRE_MARKET: MarketSession
MARKET_SESSION_REGULAR: MarketSession
MARKET_SESSION_POST_MARKET: MarketSession
MARKET_SESSION_CLOSED: MarketSession

class StockQuote(_message.Message):
    __slots__ = ("symbol", "last", "bid", "ask", "volume", "change", "change_pct", "high", "low", "open", "close", "high_52w", "low_52w", "avg_volume", "timestamp", "market_session")
    SYMBOL_FIELD_NUMBER: _ClassVar[int]
    LAST_FIELD_NUMBER: _ClassVar[int]
    BID_FIELD_NUMBER: _ClassVar[int]
    ASK_FIELD_NUMBER: _ClassVar[int]
    VOLUME_FIELD_NUMBER: _ClassVar[int]
    CHANGE_FIELD_NUMBER: _ClassVar[int]
    CHANGE_PCT_FIELD_NUMBER: _ClassVar[int]
    HIGH_FIELD_NUMBER: _ClassVar[int]
    LOW_FIELD_NUMBER: _ClassVar[int]
    OPEN_FIELD_NUMBER: _ClassVar[int]
    CLOSE_FIELD_NUMBER: _ClassVar[int]
    HIGH_52W_FIELD_NUMBER: _ClassVar[int]
    LOW_52W_FIELD_NUMBER: _ClassVar[int]
    AVG_VOLUME_FIELD_NUMBER: _ClassVar[int]
    TIMESTAMP_FIELD_NUMBER: _ClassVar[int]
    MARKET_SESSION_FIELD_NUMBER: _ClassVar[int]
    symbol: str
    last: float
    bid: float
    ask: float
    volume: int
    change: float
    change_pct: float
    high: float
    low: float
    open: float
    close: float
    high_52w: float
    low_52w: float
    avg_volume: float
    timestamp: _timestamp_pb2.Timestamp
    market_session: MarketSession
    def __init__(self, symbol: _Optional[str] = ..., last: _Optional[float] = ..., bid: _Optional[float] = ..., ask: _Optional[float] = ..., volume: _Optional[int] = ..., change: _Optional[float] = ..., change_pct: _Optional[float] = ..., high: _Optional[float] = ..., low: _Optional[float] = ..., open: _Optional[float] = ..., close: _Optional[float] = ..., high_52w: _Optional[float] = ..., low_52w: _Optional[float] = ..., avg_volume: _Optional[float] = ..., timestamp: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., market_session: _Optional[_Union[MarketSession, str]] = ...) -> None: ...

class OHLCV(_message.Message):
    __slots__ = ("timestamp", "open", "high", "low", "close", "volume")
    TIMESTAMP_FIELD_NUMBER: _ClassVar[int]
    OPEN_FIELD_NUMBER: _ClassVar[int]
    HIGH_FIELD_NUMBER: _ClassVar[int]
    LOW_FIELD_NUMBER: _ClassVar[int]
    CLOSE_FIELD_NUMBER: _ClassVar[int]
    VOLUME_FIELD_NUMBER: _ClassVar[int]
    timestamp: _timestamp_pb2.Timestamp
    open: float
    high: float
    low: float
    close: float
    volume: int
    def __init__(self, timestamp: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., open: _Optional[float] = ..., high: _Optional[float] = ..., low: _Optional[float] = ..., close: _Optional[float] = ..., volume: _Optional[int] = ...) -> None: ...

class OptionQuote(_message.Message):
    __slots__ = ("underlying", "expiration", "strike", "option_type", "open_interest", "implied_volatility")
    UNDERLYING_FIELD_NUMBER: _ClassVar[int]
    EXPIRATION_FIELD_NUMBER: _ClassVar[int]
    STRIKE_FIELD_NUMBER: _ClassVar[int]
    OPTION_TYPE_FIELD_NUMBER: _ClassVar[int]
    OPEN_INTEREST_FIELD_NUMBER: _ClassVar[int]
    IMPLIED_VOLATILITY_FIELD_NUMBER: _ClassVar[int]
    underlying: str
    expiration: str
    strike: float
    option_type: OptionType
    open_interest: int
    implied_volatility: float
    def __init__(self, underlying: _Optional[str] = ..., expiration: _Optional[str] = ..., strike: _Optional[float] = ..., option_type: _Optional[_Union[OptionType, str]] = ..., open_interest: _Optional[int] = ..., implied_volatility: _Optional[float] = ...) -> None: ...

class OptionChainExpiry(_message.Message):
    __slots__ = ("expiration", "days_to_expiry", "calls", "puts")
    EXPIRATION_FIELD_NUMBER: _ClassVar[int]
    DAYS_TO_EXPIRY_FIELD_NUMBER: _ClassVar[int]
    CALLS_FIELD_NUMBER: _ClassVar[int]
    PUTS_FIELD_NUMBER: _ClassVar[int]
    expiration: str
    days_to_expiry: int
    calls: _containers.RepeatedCompositeFieldContainer[OptionQuote]
    puts: _containers.RepeatedCompositeFieldContainer[OptionQuote]
    def __init__(self, expiration: _Optional[str] = ..., days_to_expiry: _Optional[int] = ..., calls: _Optional[_Iterable[_Union[OptionQuote, _Mapping]]] = ..., puts: _Optional[_Iterable[_Union[OptionQuote, _Mapping]]] = ...) -> None: ...

class Greeks(_message.Message):
    __slots__ = ("delta", "gamma", "theta", "vega", "rho")
    DELTA_FIELD_NUMBER: _ClassVar[int]
    GAMMA_FIELD_NUMBER: _ClassVar[int]
    THETA_FIELD_NUMBER: _ClassVar[int]
    VEGA_FIELD_NUMBER: _ClassVar[int]
    RHO_FIELD_NUMBER: _ClassVar[int]
    delta: float
    gamma: float
    theta: float
    vega: float
    rho: float
    def __init__(self, delta: _Optional[float] = ..., gamma: _Optional[float] = ..., theta: _Optional[float] = ..., vega: _Optional[float] = ..., rho: _Optional[float] = ...) -> None: ...
