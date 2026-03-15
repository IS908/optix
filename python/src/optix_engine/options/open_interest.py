"""Open Interest analysis: OI walls, unusual activity detection."""

import numpy as np
import pandas as pd


def find_oi_walls(
    chain_df: pd.DataFrame,
    top_n: int = 5,
) -> dict:
    """Find significant OI concentrations (walls) that may act as support/resistance.

    Args:
        chain_df: DataFrame with columns: strike, call_oi, put_oi
        top_n: Number of top OI clusters to return

    Returns:
        dict with 'put_walls' and 'call_walls', each a list of (strike, oi) tuples
    """
    # Put walls (potential support): large put OI = dealers buy stock to hedge
    put_sorted = chain_df.nlargest(top_n, "put_oi")[["strike", "put_oi"]]
    put_walls = list(zip(put_sorted["strike"], put_sorted["put_oi"]))

    # Call walls (potential resistance): large call OI = dealers sell stock to hedge
    call_sorted = chain_df.nlargest(top_n, "call_oi")[["strike", "call_oi"]]
    call_walls = list(zip(call_sorted["strike"], call_sorted["call_oi"]))

    return {
        "put_walls": put_walls,
        "call_walls": call_walls,
    }


def detect_unusual_activity(
    chain_df: pd.DataFrame,
    volume_oi_threshold: float = 2.0,
    min_volume: int = 100,
) -> list[dict]:
    """Detect unusual options activity based on volume/OI ratio.

    High volume relative to open interest suggests new positioning (potential institutional activity).

    Args:
        chain_df: DataFrame with columns: strike, expiration, option_type, volume, open_interest
        volume_oi_threshold: Minimum volume/OI ratio to flag
        min_volume: Minimum volume to consider

    Returns:
        List of dicts describing unusual activity
    """
    results = []

    for _, row in chain_df.iterrows():
        vol = row.get("volume", 0)
        oi = row.get("open_interest", 0)

        if vol < min_volume:
            continue

        if oi > 0:
            vol_oi_ratio = vol / oi
        else:
            vol_oi_ratio = float(vol)  # no OI = all new positions

        if vol_oi_ratio >= volume_oi_threshold:
            results.append({
                "strike": row["strike"],
                "expiration": row.get("expiration", ""),
                "option_type": row.get("option_type", ""),
                "volume": int(vol),
                "open_interest": int(oi),
                "volume_oi_ratio": round(vol_oi_ratio, 2),
                "description": f"Unusual volume: {vol} contracts vs {oi} OI (ratio: {vol_oi_ratio:.1f}x)",
            })

    # Sort by volume/OI ratio descending
    results.sort(key=lambda x: x["volume_oi_ratio"], reverse=True)
    return results


def put_call_ratio(chain_df: pd.DataFrame, by: str = "oi") -> float:
    """Calculate Put/Call ratio.

    Args:
        chain_df: DataFrame with columns: option_type ('C'/'P'), volume, open_interest
        by: "oi" for open interest ratio, "volume" for volume ratio

    Returns:
        PCR value (>1 = bearish sentiment, <1 = bullish)
    """
    calls = chain_df[chain_df["option_type"] == "C"]
    puts = chain_df[chain_df["option_type"] == "P"]

    if by == "oi":
        call_total = calls["open_interest"].sum()
        put_total = puts["open_interest"].sum()
    else:
        call_total = calls["volume"].sum()
        put_total = puts["volume"].sum()

    if call_total == 0:
        return float("inf") if put_total > 0 else 1.0

    return put_total / call_total
