"""Tests for Black-Scholes pricing and Greeks."""

import pytest
import numpy as np
from optix_engine.options.pricing import (
    call_price, put_price, delta, gamma, theta, vega, rho, all_greeks,
)
from optix_engine.options.implied_vol import implied_volatility
from optix_engine.options.max_pain import calculate_max_pain


class TestBlackScholes:
    """Test BS pricing against known values."""

    # Standard test case: S=100, K=100, T=1, r=5%, sigma=20%
    S, K, T, r, sigma = 100, 100, 1.0, 0.05, 0.20

    def test_call_price(self):
        p = call_price(self.S, self.K, self.T, self.r, self.sigma)
        # Known BS value ≈ 10.45
        assert 10.0 < p < 11.0

    def test_put_price(self):
        p = put_price(self.S, self.K, self.T, self.r, self.sigma)
        # Known BS value ≈ 5.57
        assert 5.0 < p < 6.5

    def test_put_call_parity(self):
        c = call_price(self.S, self.K, self.T, self.r, self.sigma)
        p = put_price(self.S, self.K, self.T, self.r, self.sigma)
        # C - P = S - K*e^(-rT)
        parity = self.S - self.K * np.exp(-self.r * self.T)
        assert abs((c - p) - parity) < 1e-6

    def test_expired_call_itm(self):
        assert call_price(105, 100, 0, 0.05, 0.20) == 5.0

    def test_expired_call_otm(self):
        assert call_price(95, 100, 0, 0.05, 0.20) == 0.0

    def test_expired_put_itm(self):
        assert put_price(95, 100, 0, 0.05, 0.20) == 5.0


class TestGreeks:
    S, K, T, r, sigma = 100, 100, 1.0, 0.05, 0.20

    def test_call_delta_range(self):
        d = delta(self.S, self.K, self.T, self.r, self.sigma, "call")
        assert 0 < d < 1

    def test_put_delta_range(self):
        d = delta(self.S, self.K, self.T, self.r, self.sigma, "put")
        assert -1 < d < 0

    def test_atm_delta_approximately_half(self):
        d = delta(self.S, self.K, self.T, self.r, self.sigma, "call")
        assert 0.4 < d < 0.7  # ATM call delta ≈ 0.5-0.6

    def test_gamma_positive(self):
        g = gamma(self.S, self.K, self.T, self.r, self.sigma)
        assert g > 0

    def test_theta_negative_for_long(self):
        t = theta(self.S, self.K, self.T, self.r, self.sigma, "call")
        assert t < 0  # time decay hurts long positions

    def test_vega_positive(self):
        v = vega(self.S, self.K, self.T, self.r, self.sigma)
        assert v > 0

    def test_all_greeks_keys(self):
        g = all_greeks(self.S, self.K, self.T, self.r, self.sigma, "call")
        assert set(g.keys()) == {"delta", "gamma", "theta", "vega", "rho"}


class TestImpliedVolatility:
    S, K, T, r = 100, 100, 1.0, 0.05

    def test_roundtrip_call(self):
        """Price a call, then recover IV from that price."""
        true_vol = 0.25
        market_price = call_price(self.S, self.K, self.T, self.r, true_vol)
        iv, converged = implied_volatility(market_price, self.S, self.K, self.T, self.r, "call")
        assert converged
        assert abs(iv - true_vol) < 0.001

    def test_roundtrip_put(self):
        true_vol = 0.35
        market_price = put_price(self.S, self.K, self.T, self.r, true_vol)
        iv, converged = implied_volatility(market_price, self.S, self.K, self.T, self.r, "put")
        assert converged
        assert abs(iv - true_vol) < 0.001

    def test_zero_price_returns_not_converged(self):
        iv, converged = implied_volatility(0, self.S, self.K, self.T, self.r, "call")
        assert not converged


class TestMaxPain:
    def test_simple_max_pain(self):
        strikes = [95, 100, 105, 110]
        call_oi = [100, 500, 200, 50]
        put_oi = [50, 200, 500, 100]
        mp = calculate_max_pain(strikes, call_oi, put_oi)
        assert mp in strikes  # Max pain should be one of the strikes

    def test_empty_strikes(self):
        assert calculate_max_pain([], [], []) == 0.0
