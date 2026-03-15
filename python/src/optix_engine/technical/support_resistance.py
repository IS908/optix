"""Support and resistance level identification."""

import numpy as np
import pandas as pd

from optix_engine.technical.indicators import sma


def find_pivot_points(
    df: pd.DataFrame,
    window: int = 5,
) -> tuple[list[float], list[float]]:
    """Find pivot highs and lows from OHLCV data.

    A pivot high is a high that is higher than the `window` bars on either side.
    A pivot low is a low that is lower than the `window` bars on either side.

    Returns: (pivot_highs, pivot_lows)
    """
    highs = df["high"].values
    lows = df["low"].values

    pivot_highs = []
    pivot_lows = []

    for i in range(window, len(highs) - window):
        # Pivot high
        if all(highs[i] >= highs[i - j] for j in range(1, window + 1)) and \
           all(highs[i] >= highs[i + j] for j in range(1, window + 1)):
            pivot_highs.append(highs[i])

        # Pivot low
        if all(lows[i] <= lows[i - j] for j in range(1, window + 1)) and \
           all(lows[i] <= lows[i + j] for j in range(1, window + 1)):
            pivot_lows.append(lows[i])

    return pivot_highs, pivot_lows


def find_fibonacci_levels(high: float, low: float) -> dict[str, float]:
    """Calculate Fibonacci retracement levels from a swing high/low.

    Returns dict of level_name -> price.
    """
    diff = high - low
    return {
        "fib_0.0": high,
        "fib_0.236": high - 0.236 * diff,
        "fib_0.382": high - 0.382 * diff,
        "fib_0.5": high - 0.5 * diff,
        "fib_0.618": high - 0.618 * diff,
        "fib_0.786": high - 0.786 * diff,
        "fib_1.0": low,
    }


def find_ma_levels(df: pd.DataFrame) -> list[dict]:
    """Identify MA levels as potential support/resistance.

    Returns list of {price, source, strength}.
    """
    levels = []
    last = df.iloc[-1]

    for period, col in [(20, "ma_20"), (50, "ma_50"), (200, "ma_200")]:
        if col in df.columns and not np.isnan(last[col]):
            levels.append({
                "price": round(last[col], 2),
                "source": f"MA_{period}",
                "strength": min(period / 2, 100),  # longer MA = stronger level
            })

    return levels


def classify_levels(
    current_price: float,
    levels: list[dict],
) -> tuple[list[dict], list[dict]]:
    """Split levels into support (below price) and resistance (above price).

    Returns: (support_levels, resistance_levels), each sorted by distance to current price.
    """
    support = [l for l in levels if l["price"] < current_price]
    resistance = [l for l in levels if l["price"] > current_price]

    # Sort: nearest first
    support.sort(key=lambda l: current_price - l["price"])
    resistance.sort(key=lambda l: l["price"] - current_price)

    return support, resistance


def find_all_levels(
    df: pd.DataFrame,
    current_price: float,
    oi_walls: dict | None = None,
    max_pain: float | None = None,
) -> tuple[list[dict], list[dict]]:
    """Find all support and resistance levels from multiple sources.

    Args:
        df: OHLCV DataFrame with indicator columns (ma_20, ma_50, ma_200)
        current_price: Current stock price
        oi_walls: Optional dict from open_interest.find_oi_walls()
        max_pain: Optional Max Pain price

    Returns: (support_levels, resistance_levels)
    """
    all_levels = []

    # 1. Moving average levels
    all_levels.extend(find_ma_levels(df))

    # 2. Pivot highs/lows
    pivot_highs, pivot_lows = find_pivot_points(df)
    for ph in pivot_highs[-5:]:  # recent 5
        all_levels.append({"price": round(ph, 2), "source": "pivot_high", "strength": 60})
    for pl in pivot_lows[-5:]:
        all_levels.append({"price": round(pl, 2), "source": "pivot_low", "strength": 60})

    # 3. Fibonacci levels (from recent swing)
    if len(df) >= 20:
        recent = df.tail(60)
        swing_high = recent["high"].max()
        swing_low = recent["low"].min()
        fibs = find_fibonacci_levels(swing_high, swing_low)
        for name, price_val in fibs.items():
            all_levels.append({"price": round(price_val, 2), "source": name, "strength": 50})

    # 4. Bollinger Bands
    last = df.iloc[-1]
    if "bb_upper" in df.columns:
        all_levels.append({"price": round(last["bb_upper"], 2), "source": "bb_upper", "strength": 40})
        all_levels.append({"price": round(last["bb_lower"], 2), "source": "bb_lower", "strength": 40})

    # 5. OI walls (from options data)
    if oi_walls:
        for strike, oi in oi_walls.get("put_walls", []):
            all_levels.append({"price": strike, "source": "put_oi_wall", "strength": min(oi / 1000, 90)})
        for strike, oi in oi_walls.get("call_walls", []):
            all_levels.append({"price": strike, "source": "call_oi_wall", "strength": min(oi / 1000, 90)})

    # 6. Max Pain
    if max_pain:
        all_levels.append({"price": max_pain, "source": "max_pain", "strength": 70})

    return classify_levels(current_price, all_levels)
