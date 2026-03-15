from optix.marketdata.v1 import types_pb2 as _types_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class GetQuoteRequest(_message.Message):
    __slots__ = ("symbol",)
    SYMBOL_FIELD_NUMBER: _ClassVar[int]
    symbol: str
    def __init__(self, symbol: _Optional[str] = ...) -> None: ...

class GetQuoteResponse(_message.Message):
    __slots__ = ("quote",)
    QUOTE_FIELD_NUMBER: _ClassVar[int]
    quote: _types_pb2.StockQuote
    def __init__(self, quote: _Optional[_Union[_types_pb2.StockQuote, _Mapping]] = ...) -> None: ...

class GetHistoricalBarsRequest(_message.Message):
    __slots__ = ("symbol", "timeframe", "days")
    SYMBOL_FIELD_NUMBER: _ClassVar[int]
    TIMEFRAME_FIELD_NUMBER: _ClassVar[int]
    DAYS_FIELD_NUMBER: _ClassVar[int]
    symbol: str
    timeframe: str
    days: int
    def __init__(self, symbol: _Optional[str] = ..., timeframe: _Optional[str] = ..., days: _Optional[int] = ...) -> None: ...

class GetHistoricalBarsResponse(_message.Message):
    __slots__ = ("bars",)
    BARS_FIELD_NUMBER: _ClassVar[int]
    bars: _containers.RepeatedCompositeFieldContainer[_types_pb2.OHLCV]
    def __init__(self, bars: _Optional[_Iterable[_Union[_types_pb2.OHLCV, _Mapping]]] = ...) -> None: ...
