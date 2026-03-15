"""Max Pain calculation for options expiration."""

import numpy as np
import pandas as pd


def calculate_max_pain(
    strikes: list[float],
    call_oi: list[int],
    put_oi: list[int],
) -> float:
    """Calculate the Max Pain price for an option expiration.

    Max Pain is the strike price at which the total value of all outstanding
    options (calls + puts) would cause the maximum financial loss to option holders
    (maximum gain for option sellers/writers).

    Args:
        strikes: List of strike prices
        call_oi: Open interest for each call at corresponding strike
        put_oi: Open interest for each put at corresponding strike

    Returns:
        The Max Pain strike price
    """
    if not strikes:
        return 0.0

    strikes = np.array(strikes)
    call_oi = np.array(call_oi)
    put_oi = np.array(put_oi)

    min_pain = float("inf")
    max_pain_strike = strikes[0]

    for test_price in strikes:
        # At test_price, calculate total pain (value) for all option holders
        # Call holders lose when stock < strike: pain = 0 (expires worthless)
        # Call holders gain when stock > strike: that's not pain for them
        # But we want total $ lost by ALL holders if stock settles at test_price

        # For call holders: intrinsic value = max(test_price - strike, 0) * OI * 100
        call_pain = np.sum(np.maximum(test_price - strikes, 0) * call_oi * 100)
        # For put holders: intrinsic value = max(strike - test_price, 0) * OI * 100
        put_pain = np.sum(np.maximum(strikes - test_price, 0) * put_oi * 100)

        total_pain = call_pain + put_pain

        if total_pain < min_pain:
            min_pain = total_pain
            max_pain_strike = test_price

    return float(max_pain_strike)


def max_pain_from_chain(chain_df: pd.DataFrame) -> float:
    """Calculate Max Pain from a DataFrame with columns: strike, call_oi, put_oi."""
    return calculate_max_pain(
        chain_df["strike"].tolist(),
        chain_df["call_oi"].tolist(),
        chain_df["put_oi"].tolist(),
    )
