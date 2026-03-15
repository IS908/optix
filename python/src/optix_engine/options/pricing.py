"""Black-Scholes option pricing and Greeks calculation."""

import numpy as np
from scipy.stats import norm


def d1(S: float, K: float, T: float, r: float, sigma: float, q: float = 0.0) -> float:
    """Calculate d1 parameter of the Black-Scholes model."""
    return (np.log(S / K) + (r - q + 0.5 * sigma**2) * T) / (sigma * np.sqrt(T))


def d2(S: float, K: float, T: float, r: float, sigma: float, q: float = 0.0) -> float:
    """Calculate d2 parameter of the Black-Scholes model."""
    return d1(S, K, T, r, sigma, q) - sigma * np.sqrt(T)


def call_price(S: float, K: float, T: float, r: float, sigma: float, q: float = 0.0) -> float:
    """Calculate Black-Scholes call option price.

    Args:
        S: Current stock price
        K: Strike price
        T: Time to expiration in years
        r: Risk-free interest rate
        sigma: Volatility (annualized)
        q: Continuous dividend yield
    """
    if T <= 0:
        return max(S - K, 0.0)
    _d1 = d1(S, K, T, r, sigma, q)
    _d2 = _d1 - sigma * np.sqrt(T)
    return S * np.exp(-q * T) * norm.cdf(_d1) - K * np.exp(-r * T) * norm.cdf(_d2)


def put_price(S: float, K: float, T: float, r: float, sigma: float, q: float = 0.0) -> float:
    """Calculate Black-Scholes put option price."""
    if T <= 0:
        return max(K - S, 0.0)
    _d1 = d1(S, K, T, r, sigma, q)
    _d2 = _d1 - sigma * np.sqrt(T)
    return K * np.exp(-r * T) * norm.cdf(-_d2) - S * np.exp(-q * T) * norm.cdf(-_d1)


def price(S: float, K: float, T: float, r: float, sigma: float, option_type: str, q: float = 0.0) -> float:
    """Calculate option price for a given type ('call' or 'put')."""
    if option_type.lower() == "call":
        return call_price(S, K, T, r, sigma, q)
    elif option_type.lower() == "put":
        return put_price(S, K, T, r, sigma, q)
    raise ValueError(f"Unknown option type: {option_type}")


# --- Greeks ---

def delta(S: float, K: float, T: float, r: float, sigma: float, option_type: str, q: float = 0.0) -> float:
    """Calculate option delta."""
    if T <= 0:
        if option_type.lower() == "call":
            return 1.0 if S > K else 0.0
        return -1.0 if S < K else 0.0
    _d1 = d1(S, K, T, r, sigma, q)
    if option_type.lower() == "call":
        return np.exp(-q * T) * norm.cdf(_d1)
    return np.exp(-q * T) * (norm.cdf(_d1) - 1)


def gamma(S: float, K: float, T: float, r: float, sigma: float, q: float = 0.0) -> float:
    """Calculate option gamma (same for calls and puts)."""
    if T <= 0:
        return 0.0
    _d1 = d1(S, K, T, r, sigma, q)
    return np.exp(-q * T) * norm.pdf(_d1) / (S * sigma * np.sqrt(T))


def theta(S: float, K: float, T: float, r: float, sigma: float, option_type: str, q: float = 0.0) -> float:
    """Calculate option theta (per day)."""
    if T <= 0:
        return 0.0
    _d1 = d1(S, K, T, r, sigma, q)
    _d2 = _d1 - sigma * np.sqrt(T)

    common = -(S * np.exp(-q * T) * norm.pdf(_d1) * sigma) / (2 * np.sqrt(T))
    if option_type.lower() == "call":
        result = common - r * K * np.exp(-r * T) * norm.cdf(_d2) + q * S * np.exp(-q * T) * norm.cdf(_d1)
    else:
        result = common + r * K * np.exp(-r * T) * norm.cdf(-_d2) - q * S * np.exp(-q * T) * norm.cdf(-_d1)
    return result / 365.0  # per day


def vega(S: float, K: float, T: float, r: float, sigma: float, q: float = 0.0) -> float:
    """Calculate option vega (per 1% change in vol)."""
    if T <= 0:
        return 0.0
    _d1 = d1(S, K, T, r, sigma, q)
    return S * np.exp(-q * T) * norm.pdf(_d1) * np.sqrt(T) / 100.0


def rho(S: float, K: float, T: float, r: float, sigma: float, option_type: str, q: float = 0.0) -> float:
    """Calculate option rho (per 1% change in rate)."""
    if T <= 0:
        return 0.0
    _d2 = d2(S, K, T, r, sigma, q)
    if option_type.lower() == "call":
        return K * T * np.exp(-r * T) * norm.cdf(_d2) / 100.0
    return -K * T * np.exp(-r * T) * norm.cdf(-_d2) / 100.0


def all_greeks(S: float, K: float, T: float, r: float, sigma: float, option_type: str, q: float = 0.0) -> dict:
    """Calculate all Greeks at once."""
    return {
        "delta": delta(S, K, T, r, sigma, option_type, q),
        "gamma": gamma(S, K, T, r, sigma, q),
        "theta": theta(S, K, T, r, sigma, option_type, q),
        "vega": vega(S, K, T, r, sigma, q),
        "rho": rho(S, K, T, r, sigma, option_type, q),
    }
