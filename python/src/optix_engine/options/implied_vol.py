"""Implied volatility solver using Newton-Raphson with Brent's method fallback."""

import numpy as np
from scipy.optimize import brentq

from optix_engine.options.pricing import price, vega as bs_vega


def implied_volatility(
    market_price: float,
    S: float,
    K: float,
    T: float,
    r: float,
    option_type: str,
    q: float = 0.0,
    tol: float = 1e-6,
    max_iter: int = 100,
) -> tuple[float, bool]:
    """Calculate implied volatility from market price.

    Uses Newton-Raphson first (fast convergence), falls back to Brent's method if NR fails.

    Returns:
        (implied_vol, converged): The IV and whether it converged.
    """
    if T <= 0 or market_price <= 0:
        return 0.0, False

    # Intrinsic value check
    if option_type.lower() == "call":
        intrinsic = max(S * np.exp(-q * T) - K * np.exp(-r * T), 0)
    else:
        intrinsic = max(K * np.exp(-r * T) - S * np.exp(-q * T), 0)

    if market_price < intrinsic:
        return 0.0, False

    # Newton-Raphson
    sigma = 0.3  # initial guess
    for _ in range(max_iter):
        p = price(S, K, T, r, sigma, option_type, q)
        v = bs_vega(S, K, T, r, sigma, q) * 100  # vega is per 1%, need actual
        if abs(v) < 1e-12:
            break
        diff = market_price - p
        if abs(diff) < tol:
            return sigma, True
        sigma += diff / v
        if sigma <= 0:
            break  # fall through to Brent's

    # Brent's method fallback
    try:
        def objective(sig):
            return price(S, K, T, r, sig, option_type, q) - market_price

        result = brentq(objective, 0.001, 5.0, xtol=tol, maxiter=max_iter)
        return result, True
    except (ValueError, RuntimeError):
        return 0.0, False
