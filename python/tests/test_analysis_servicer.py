from optix_engine.grpc_server.analysis_servicer import AnalysisServicer
from optix_engine.gen.optix.analysis.v1 import analysis_pb2
from optix_engine.gen.optix.marketdata.v1 import types_pb2 as md_types


class _FakeCtx:
    def __init__(self):
        self.code = None
        self.details = None

    def set_code(self, c):
        self.code = c

    def set_details(self, d):
        self.details = d


def test_implied_vol_round_trips_a_known_price():
    svc = AnalysisServicer()
    # Price a known call at sigma=0.25, then invert it back.
    S, K, T, r, sigma = 100.0, 100.0, 0.5, 0.04, 0.25
    priced = svc.PriceOption(
        analysis_pb2.PriceOptionRequest(
            spot_price=S, strike=K, time_to_expiry=T, risk_free_rate=r,
            volatility=sigma, dividend_yield=0.0,
            option_type=md_types.OPTION_TYPE_CALL,
        ),
        _FakeCtx(),
    )
    resp = svc.ImpliedVol(
        analysis_pb2.ImpliedVolRequest(
            market_price=priced.price, spot_price=S, strike=K,
            time_to_expiry=T, risk_free_rate=r, dividend_yield=0.0,
            option_type=md_types.OPTION_TYPE_CALL,
        ),
        _FakeCtx(),
    )
    assert resp.converged is True
    assert abs(resp.implied_volatility - sigma) < 1e-3


def test_implied_vol_marks_non_convergence_for_garbage():
    svc = AnalysisServicer()
    ctx = _FakeCtx()
    resp = svc.ImpliedVol(
        analysis_pb2.ImpliedVolRequest(
            market_price=0.0, spot_price=100, strike=100,
            time_to_expiry=0.5, risk_free_rate=0.04, dividend_yield=0.0,
            option_type=md_types.OPTION_TYPE_CALL,
        ),
        ctx,
    )
    assert resp.converged is False
