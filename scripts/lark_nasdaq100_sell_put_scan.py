#!/usr/bin/env python3
"""Lark cron entry for the Nasdaq-100 sell-put income scan.

The script is intentionally quiet outside the valid open-after-20-minutes
window so two China-time cron entries can cover US DST without duplicate posts.
"""

import datetime as dt
import argparse
import contextlib
import io
import json
import math
import os
from pathlib import Path
import subprocess
import sys
import time
import urllib.request
import warnings
from dataclasses import dataclass
from typing import Dict, Iterable, List, Optional, Tuple
from zoneinfo import ZoneInfo


def ensure_project_runtime() -> None:
    """Re-exec with the project venv before importing market-data packages."""
    if os.environ.get("OPTIX_SCAN_RUNTIME_CHECKED") == "1":
        return
    repo_root = Path(__file__).resolve().parents[1]
    venv_python = repo_root / "python" / ".venv" / "bin" / "python"
    current = Path(sys.executable).resolve()
    if venv_python.exists() and current != venv_python.resolve():
        env = os.environ.copy()
        env["OPTIX_SCAN_RUNTIME_CHECKED"] = "1"
        env.pop("__PYVENV_LAUNCHER__", None)
        completed = subprocess.run([str(venv_python), *sys.argv], env=env, check=False)
        raise SystemExit(completed.returncode)
    if sys.version_info < (3, 11):
        print(
            "Optix Nasdaq-100 Sell Put 扫描服务异常：需要 Python >= 3.11，"
            f"当前为 {sys.version.split()[0]}，且项目虚拟环境不可用。"
        )
        raise SystemExit(2)


ensure_project_runtime()

warnings.filterwarnings("ignore", message="urllib3 v2 only supports OpenSSL")

import pandas as pd  # noqa: E402  (imported after venv re-exec by design)
import yfinance as yf  # noqa: E402


# stdlib zoneinfo（Python >= 3.11 已由 ensure_project_runtime 保证），
# 不再依赖 pytz（此前只是经 yfinance 传递可用，未声明为直接依赖）。
NY = ZoneInfo("America/New_York")
# The two Asia/Shanghai cron entries run at 21:50 and 22:50.  Depending on
# US daylight saving time, exactly one maps to 09:50 ET; the other stays quiet.
WINDOW_START = dt.time(9, 45)
WINDOW_END = dt.time(10, 10)
MIN_DTE = 7
MAX_DTE = 24
# Nasdaq-100 含双股类（GOOG/GOOGL 等）常年 >100 个 ticker,上限留余量。
MAX_SYMBOLS = 110
TOP_N = 10
DEFAULT_IBKR_TOP_N = int(os.environ.get("OPTIX_IBKR_TOP_N", "10"))
SYMBOL_SOURCE = "unknown"
SYMBOL_WARNING: Optional[str] = None
OPTIX_BIN = os.environ.get("OPTIX_BIN", "")

# 内置兜底成分表（截至 2026-07 快照,含少量已被调出的旧成分;仅在两级在线源都
# 失败时使用,输出会带「成分股提醒」警示行）。
FALLBACK_NDX = [
    "AAPL", "MSFT", "NVDA", "AMZN", "META", "AVGO", "GOOGL", "GOOG", "TSLA",
    "COST", "NFLX", "AMD", "PEP", "LIN", "ADBE", "CSCO", "QCOM", "TMUS",
    "INTU", "AMAT", "TXN", "BKNG", "ISRG", "AMGN", "HON", "VRTX", "ADP",
    "PANW", "MU", "ADI", "MELI", "LRCX", "SBUX", "GILD", "KLAC", "MDLZ",
    "CDNS", "SNPS", "CRWD", "MAR", "CEG", "ORLY", "CSX", "REGN", "PYPL",
    "ABNB", "FTNT", "MRVL", "NXPI", "ROP", "PCAR", "WDAY", "CHTR", "ADSK",
    "MNST", "ROST", "PAYX", "CPRT", "AEP", "FAST", "KDP", "KHC", "ODFL",
    "CTAS", "DDOG", "EXC", "EA", "GEHC", "VRSK", "BKR", "XEL", "CCEP",
    "FANG", "LULU", "TEAM", "CSGP", "TTWO", "IDXX", "ON", "DXCM", "ZS",
    "BIIB", "CDW", "MDB", "GFS", "ILMN", "MRNA", "WBD", "DLTR", "SIRI",
    "ENPH", "MCHP", "AZN", "ASML",
]


@dataclass
class Candidate:
    symbol: str
    spot: float
    expiry: str
    dte: int
    strike: float
    bid: float
    ask: float
    mid: float
    iv: float
    oi: int
    volume: int
    cushion_pct: float
    premium_yield_pct: float
    annualized_yield_pct: float
    delta: Optional[float]
    score: float
    ibkr_last: Optional[float] = None
    ibkr_bid: Optional[float] = None
    ibkr_ask: Optional[float] = None
    ibkr_volume: Optional[int] = None
    ibkr_session: Optional[str] = None
    ibkr_source: Optional[str] = None
    ibkr_cushion_pct: Optional[float] = None
    ibkr_error: Optional[str] = None
    ibkr_option_last: Optional[float] = None
    ibkr_option_bid: Optional[float] = None
    ibkr_option_ask: Optional[float] = None
    ibkr_option_mid: Optional[float] = None
    ibkr_option_mark: Optional[float] = None
    ibkr_option_volume: Optional[int] = None
    ibkr_option_oi: Optional[int] = None
    ibkr_option_iv: Optional[float] = None
    ibkr_option_delta: Optional[float] = None
    ibkr_option_gamma: Optional[float] = None
    ibkr_option_theta: Optional[float] = None
    ibkr_option_vega: Optional[float] = None
    ibkr_option_market_data_type: Optional[str] = None
    ibkr_option_source: Optional[str] = None
    ibkr_option_warnings: Optional[List[str]] = None
    ibkr_option_error: Optional[str] = None
    ibkr_premium_yield_pct: Optional[float] = None
    ibkr_annualized_yield_pct: Optional[float] = None


@dataclass
class ScanResult:
    candidates: List[Candidate]
    stats: Dict[str, int]
    errors: List[str]
    ibkr_errors: List[str]
    ibkr_attempted: int
    data_quality_error: Optional[str] = None


def nth_weekday(year: int, month: int, weekday: int, nth: int) -> dt.date:
    first = dt.date(year, month, 1)
    offset = (weekday - first.weekday()) % 7
    return first + dt.timedelta(days=offset + 7 * (nth - 1))


def last_weekday(year: int, month: int, weekday: int) -> dt.date:
    last = dt.date(year, month + 1, 1) - dt.timedelta(days=1) if month < 12 else dt.date(year, 12, 31)
    return last - dt.timedelta(days=(last.weekday() - weekday) % 7)


def observed(day: dt.date) -> dt.date:
    # 注意:元旦逢周六时 observed 落到上一年 12/31,不会出现在当年的
    # holidays 集合里 — 恰好正确(NYSE 那个周五照常开市,如 2027-12-31)。
    # 依赖的是「窗口判断只查当年集合」这一事实,改动时留意。
    if day.weekday() == 5:
        return day - dt.timedelta(days=1)
    if day.weekday() == 6:
        return day + dt.timedelta(days=1)
    return day


def easter(year: int) -> dt.date:
    a = year % 19
    b = year // 100
    c = year % 100
    d = b // 4
    e = b % 4
    f = (b + 8) // 25
    g = (b - f + 1) // 3
    h = (19 * a + b - d - g + 15) % 30
    i = c // 4
    k = c % 4
    ell = (32 + 2 * e + 2 * i - h - k) % 7
    m = (a + 11 * h + 22 * ell) // 451
    month = (h + ell - 7 * m + 114) // 31
    day = ((h + ell - 7 * m + 114) % 31) + 1
    return dt.date(year, month, day)


def nyse_holidays(year: int) -> set:
    holidays = {
        observed(dt.date(year, 1, 1)),
        nth_weekday(year, 1, 0, 3),
        nth_weekday(year, 2, 0, 3),
        easter(year) - dt.timedelta(days=2),
        last_weekday(year, 5, 0),
        observed(dt.date(year, 6, 19)),
        observed(dt.date(year, 7, 4)),
        nth_weekday(year, 9, 0, 1),
        nth_weekday(year, 11, 3, 4),
        observed(dt.date(year, 12, 25)),
    }
    return holidays


def is_valid_window(now: Optional[dt.datetime] = None) -> bool:
    now_ny = (now or dt.datetime.now(tz=NY)).astimezone(NY)
    today = now_ny.date()
    if today.weekday() >= 5:
        return False
    if today in nyse_holidays(today.year):
        return False
    return WINDOW_START <= now_ny.time() <= WINDOW_END


BROWSER_UA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 optix-nasdaq100-scan/1.0"


def _dedupe_upper(raw: Iterable[str]) -> List[str]:
    seen = set()
    out = []
    for item in raw:
        sym = str(item).strip().upper().replace(".", "-")
        if sym and sym != "NAN" and sym not in seen:
            seen.add(sym)
            out.append(sym)
    return out


def _symbols_from_nasdaq_api() -> List[str]:
    """Index-owner source: api.nasdaq.com list-type endpoint (JSON)."""
    request = urllib.request.Request(
        "https://api.nasdaq.com/api/quote/list-type/nasdaq100",
        headers={"User-Agent": BROWSER_UA, "Accept": "application/json"},
    )
    with urllib.request.urlopen(request, timeout=15) as response:
        data = json.loads(response.read())
    rows = (((data or {}).get("data") or {}).get("data") or {}).get("rows") or []
    return _dedupe_upper(row.get("symbol", "") for row in rows)


def _symbols_from_slickcharts() -> List[str]:
    """Backup source: slickcharts.com constituents table (HTML)."""
    request = urllib.request.Request(
        "https://www.slickcharts.com/nasdaq100",
        headers={"User-Agent": BROWSER_UA, "Accept": "text/html"},
    )
    with urllib.request.urlopen(request, timeout=15) as response:
        html = response.read().decode("utf-8", errors="replace")
    for table in pd.read_html(io.StringIO(html), flavor="lxml"):
        columns = [str(c) for c in table.columns]
        if "Symbol" in columns and len(table) >= 90:
            return _dedupe_upper(table["Symbol"].astype(str).tolist())
    raise ValueError("no table with a Symbol column of >=90 rows")


def nasdaq100_symbols() -> List[str]:
    # 2026-07 起 Wikipedia Nasdaq-100 条目不再内嵌成分股表(改为 navbox),
    # 因此在线源改为:Nasdaq 官方 API → slickcharts → 内置兜底。
    global SYMBOL_SOURCE, SYMBOL_WARNING
    failures = []
    for name, fetch in (
        ("Nasdaq official API", _symbols_from_nasdaq_api),
        ("slickcharts.com", _symbols_from_slickcharts),
    ):
        try:
            symbols = fetch()
            if len(symbols) >= 90:
                SYMBOL_SOURCE = name
                SYMBOL_WARNING = None
                return symbols[:MAX_SYMBOLS]
            failures.append(f"{name}: unexpected count {len(symbols)}")
        except Exception as exc:
            failures.append(f"{name}: {type(exc).__name__}: {exc}")
    SYMBOL_SOURCE = "built-in fallback"
    SYMBOL_WARNING = "Nasdaq-100 成分股在线抓取失败，使用内置 fallback：" + "；".join(failures)
    return FALLBACK_NDX[:MAX_SYMBOLS]


def norm_cdf(x: float) -> float:
    return 0.5 * (1.0 + math.erf(x / math.sqrt(2.0)))


def put_delta(spot: float, strike: float, dte: int, iv: float, rate: float = 0.045) -> Optional[float]:
    if spot <= 0 or strike <= 0 or dte <= 0 or iv <= 0:
        return None
    t = dte / 365.0
    d1 = (math.log(spot / strike) + (rate + 0.5 * iv * iv) * t) / (iv * math.sqrt(t))
    return norm_cdf(d1) - 1.0


def best_put_for_symbol(symbol: str, today: dt.date) -> Tuple[Optional[Candidate], str]:
    ticker = yf.Ticker(symbol)
    hist = ticker.history(period="5d", interval="1d", auto_adjust=False)
    if hist.empty:
        return None, "no_price"
    spot = float(hist["Close"].dropna().iloc[-1])
    expiries = []
    for raw in ticker.options or []:
        expiry = dt.datetime.strptime(raw, "%Y-%m-%d").date()
        dte = (expiry - today).days
        if MIN_DTE <= dte <= MAX_DTE:
            expiries.append((raw, dte))
    if not expiries:
        return None, "no_expiry"

    best = None
    reason = "no_chain"
    # 取前 3 个在带内的到期：只取 2 个会把常见的第三个到期（往往是流动性
    # 最好的月度合约）挡在外面。
    for expiry, dte in expiries[:3]:
        chain = ticker.option_chain(expiry).puts
        if chain.empty:
            continue
        reason = "no_strike_band"
        rows = chain.copy()
        rows["bid"] = pd.to_numeric(rows["bid"], errors="coerce").fillna(0.0)
        rows["ask"] = pd.to_numeric(rows["ask"], errors="coerce").fillna(0.0)
        rows["strike"] = pd.to_numeric(rows["strike"], errors="coerce").fillna(0.0)
        rows["openInterest"] = pd.to_numeric(rows.get("openInterest", 0), errors="coerce").fillna(0).astype(int)
        rows["volume"] = pd.to_numeric(rows.get("volume", 0), errors="coerce").fillna(0).astype(int)
        rows["impliedVolatility"] = pd.to_numeric(rows.get("impliedVolatility", 0), errors="coerce").fillna(0.0)
        rows = rows[(rows["strike"] >= spot * 0.82) & (rows["strike"] <= spot * 0.95)]
        if rows.empty:
            continue
        reason = "no_bid_oi"
        rows = rows[(rows["bid"] >= 0.20) & (rows["ask"] > rows["bid"]) & (rows["openInterest"] >= 50)]
        if rows.empty:
            continue
        reason = "wide_spread"
        rows["mid"] = (rows["bid"] + rows["ask"]) / 2.0
        rows["spread_pct"] = (rows["ask"] - rows["bid"]) / rows["mid"].replace(0, float("nan"))
        rows = rows[rows["spread_pct"] <= 0.35]
        if rows.empty:
            continue

        for row in rows.itertuples(index=False):
            strike = float(row.strike)
            mid = float(row.mid)
            iv = float(row.impliedVolatility)
            oi = int(row.openInterest)
            volume = int(row.volume)
            cushion = (spot - strike) / spot
            premium_yield = mid / strike
            annualized = premium_yield * 365.0 / dte
            delta = put_delta(spot, strike, dte, iv)
            # delta 未知(IV 缺失/为 0)按最大惩罚处理,不能让坏数据的行反而
            # 享受「正好在 0.24 目标」的零惩罚;delta==0.0(深 OTM/低 IV 饱和)
            # 也照常吃 |0-0.24| 的惩罚 — 不再使用会把 0.0 当 None 的 `or`。
            delta_penalty = 0.36 if delta is None else abs(abs(delta) - 0.24) * 1.5
            # 打分用 bid 口径(卖方最差成交价),展示仍是 mid 口径;年化项
            # 封顶 200%,否则 365/dte 放大会让 7d 合约永远压过 21d。
            bid_yield = float(row.bid) / strike
            bid_annualized = bid_yield * 365.0 / dte
            score = (
                bid_yield * 4.0
                + min(bid_annualized, 2.0) * 0.7
                + min(iv, 1.5) * 0.12
                + min(math.log10(max(oi, 1)) / 5.0, 0.2)
                + min(volume / 1000.0, 0.08)
                - max(0.0, 0.08 - cushion) * 2.0
                - delta_penalty
            )
            cand = Candidate(
                symbol=symbol,
                spot=spot,
                expiry=expiry,
                dte=dte,
                strike=strike,
                bid=float(row.bid),
                ask=float(row.ask),
                mid=mid,
                iv=iv,
                oi=oi,
                volume=volume,
                cushion_pct=cushion * 100.0,
                premium_yield_pct=premium_yield * 100.0,
                annualized_yield_pct=annualized * 100.0,
                delta=delta,
                score=score,
            )
            if best is None or cand.score > best.score:
                best = cand
    return (best, "candidate") if best else (None, reason)


def optix_script() -> str:
    if OPTIX_BIN:
        return OPTIX_BIN
    repo_script = Path(__file__).resolve().parents[1] / "skills" / "commands" / "optix" / "optix.sh"
    if repo_script.exists():
        return str(repo_script)
    return str(Path.home() / ".agents" / "skills" / "optix" / "bin" / "optix.sh")


def fetch_ibkr_stock_quote(symbol: str) -> Tuple[Optional[Dict], Optional[str]]:
    try:
        completed = subprocess.run(
            ["bash", optix_script(), "quote", symbol, "--format", "json"],
            check=False,
            capture_output=True,
            text=True,
            timeout=15,
        )
    except Exception as exc:
        return None, f"{symbol}: {type(exc).__name__}: {exc}"
    if completed.returncode != 0:
        err = (completed.stderr or completed.stdout).strip().splitlines()
        return None, f"{symbol}: {compact_ibkr_error('; '.join(err[-2:]))}"
    try:
        return json.loads(completed.stdout), None
    except Exception as exc:
        return None, f"{symbol}: parse optix quote JSON: {exc}"


def fetch_ibkr_option_quote(cand: Candidate) -> Tuple[Optional[Dict], Optional[str]]:
    strike = f"{cand.strike:.4f}".rstrip("0").rstrip(".")
    try:
        completed = subprocess.run(
            [
                "bash",
                optix_script(),
                "option-quote",
                cand.symbol,
                "--expiry",
                cand.expiry,
                "--right",
                "P",
                "--strike",
                strike,
                "--format",
                "json",
            ],
            check=False,
            capture_output=True,
            text=True,
            timeout=20,
        )
    except Exception as exc:
        return None, f"{cand.symbol} {cand.expiry} P {strike}: {type(exc).__name__}: {exc}"

    quote = None
    if completed.stdout.strip():
        try:
            quote = json.loads(completed.stdout)
        except Exception as exc:
            return None, f"{cand.symbol} {cand.expiry} P {strike}: parse option-quote JSON: {exc}"

    if completed.returncode != 0:
        err_lines = (completed.stderr or "").strip().splitlines()
        err = "; ".join(err_lines[-2:]) if err_lines else f"optix option-quote exit {completed.returncode}"
        return quote, f"{cand.symbol} {cand.expiry} P {strike}: {compact_ibkr_error(err)}"
    return quote, None


def compact_ibkr_error(message: str) -> str:
    if "IBKR TWS/Gateway not detected" in message or "Couldn't connect to TWS" in message:
        return "IBKR unavailable/connect failed"
    if "no usable price data" in message:
        return "IBKR option quote has no usable price data"
    if "no_price_data" in message:
        return "IBKR option quote no_price_data"
    return message[:240]


def enrich_with_ibkr_quotes(candidates: List[Candidate], limit: int) -> List[str]:
    errors = []
    for cand in candidates[:max(0, limit)]:
        quote, err = fetch_ibkr_stock_quote(cand.symbol)
        if err or not quote:
            cand.ibkr_error = err or f"{cand.symbol}: empty IBKR quote"
            if len(errors) < 5:
                errors.append(cand.ibkr_error)
            continue
        cand.ibkr_last = as_float(quote.get("last"))
        cand.ibkr_bid = as_float(quote.get("bid"))
        cand.ibkr_ask = as_float(quote.get("ask"))
        volume = quote.get("volume")
        cand.ibkr_volume = int(volume) if isinstance(volume, (int, float)) else None
        cand.ibkr_session = str(quote.get("session_label") or quote.get("market_session") or "")
        cand.ibkr_source = str(quote.get("source") or "IBKR")
        ibkr_spot = quote_spot(cand.ibkr_last, cand.ibkr_bid, cand.ibkr_ask)
        if ibkr_spot:
            cand.ibkr_cushion_pct = (ibkr_spot - cand.strike) / ibkr_spot * 100.0

        opt_quote, opt_err = fetch_ibkr_option_quote(cand)
        if opt_quote:
            apply_ibkr_option_quote(cand, opt_quote)
        if opt_err:
            cand.ibkr_option_error = opt_err
            if len(errors) < 5:
                errors.append(opt_err)
        time.sleep(0.1)
    return errors


def apply_ibkr_option_quote(cand: Candidate, quote: Dict) -> None:
    cand.ibkr_option_last = as_float(quote.get("last"))
    cand.ibkr_option_bid = as_float(quote.get("bid"))
    cand.ibkr_option_ask = as_float(quote.get("ask"))
    cand.ibkr_option_mid = as_float(quote.get("mid"))
    cand.ibkr_option_mark = as_float(quote.get("mark"))
    volume = quote.get("volume")
    cand.ibkr_option_volume = int(volume) if isinstance(volume, (int, float)) and volume >= 0 else None
    oi = quote.get("open_interest")
    cand.ibkr_option_oi = int(oi) if isinstance(oi, (int, float)) and oi >= 0 else None
    cand.ibkr_option_iv = as_float(quote.get("implied_volatility"))
    greeks = quote.get("greeks") or {}
    cand.ibkr_option_delta = as_number(greeks.get("delta"))
    cand.ibkr_option_gamma = as_number(greeks.get("gamma"))
    cand.ibkr_option_theta = as_number(greeks.get("theta"))
    cand.ibkr_option_vega = as_number(greeks.get("vega"))
    cand.ibkr_option_market_data_type = str(quote.get("market_data_type") or "")
    cand.ibkr_option_source = str(quote.get("source") or "IBKR")
    warnings_value = quote.get("warnings")
    if isinstance(warnings_value, list):
        cand.ibkr_option_warnings = [str(item) for item in warnings_value if str(item)]

    premium = cand.ibkr_option_mid or cand.ibkr_option_mark or cand.ibkr_option_last
    if premium and cand.strike > 0 and cand.dte > 0:
        premium_yield = premium / cand.strike
        cand.ibkr_premium_yield_pct = premium_yield * 100.0
        cand.ibkr_annualized_yield_pct = premium_yield * 365.0 / cand.dte * 100.0


def as_float(value) -> Optional[float]:
    if isinstance(value, (int, float)) and value > 0:
        return float(value)
    return None


def as_number(value) -> Optional[float]:
    if isinstance(value, (int, float)):
        return float(value)
    return None


def quote_spot(last: Optional[float], bid: Optional[float], ask: Optional[float]) -> Optional[float]:
    if last and last > 0:
        return last
    if bid and ask and bid > 0 and ask > 0:
        return (bid + ask) / 2.0
    return None


def scan(symbols: Iterable[str], ibkr_top_n: int = DEFAULT_IBKR_TOP_N) -> ScanResult:
    today = dt.datetime.now(tz=NY).date()
    candidates = []
    stats = {
        "symbols": 0,
        "candidate": 0,
        "no_price": 0,
        "no_expiry": 0,
        "no_chain": 0,
        "no_strike_band": 0,
        "no_bid_oi": 0,
        "wide_spread": 0,
        "error": 0,
        "ibkr_quote_ok": 0,
        "ibkr_quote_error": 0,
    }
    errors = []
    for symbol in symbols:
        stats["symbols"] += 1
        try:
            with contextlib.redirect_stderr(io.StringIO()):
                cand, reason = best_put_for_symbol(symbol, today)
            if cand:
                candidates.append(cand)
            stats[reason] = stats.get(reason, 0) + 1
        except Exception as exc:
            stats["error"] += 1
            if len(errors) < 5:
                errors.append(f"{symbol}: {type(exc).__name__}: {exc}")
        time.sleep(0.08)
    candidates.sort(key=lambda item: item.score, reverse=True)
    top = candidates[:TOP_N]
    ibkr_attempted = min(max(0, ibkr_top_n), len(top))
    ibkr_errors = enrich_with_ibkr_quotes(top, ibkr_attempted) if ibkr_attempted > 0 else []
    stats["ibkr_quote_ok"] = sum(1 for cand in top if cand.ibkr_last or (cand.ibkr_bid and cand.ibkr_ask))
    stats["ibkr_quote_error"] = sum(1 for cand in top if cand.ibkr_error)
    stats["ibkr_option_ok"] = sum(1 for cand in top if cand.ibkr_option_mid or (cand.ibkr_option_bid and cand.ibkr_option_ask) or cand.ibkr_option_mark)
    stats["ibkr_option_error"] = sum(1 for cand in top if cand.ibkr_option_error)
    stats["ibkr_option_warning"] = sum(1 for cand in top if cand.ibkr_option_warnings)
    # 熔断分子必须包含异常桶:yfinance 限流等故障走 except 分支进 error,
    # 不算进来的话「全被限流」会渲染成貌似合法的零候选 — 熔断器要防的
    # 正是这种假阴性。
    failed = stats["no_price"] + stats["error"]
    fail_ratio = failed / max(stats["symbols"], 1)
    data_quality_error = None
    if fail_ratio > 0.20:
        data_quality_error = (
            f"行情获取失败 {failed}/{stats['symbols']} "
            f"({fail_ratio:.0%}，含无行情 {stats['no_price']} + 异常 {stats['error']})，"
            f"超过 20% 熔断阈值；本次结果无效。"
        )
    return ScanResult(
        candidates=top,
        stats=stats,
        errors=errors,
        ibkr_errors=ibkr_errors,
        ibkr_attempted=ibkr_attempted,
        data_quality_error=data_quality_error,
    )


def fmt_delta(delta: Optional[float]) -> str:
    return "n/a" if delta is None else f"{delta:.2f}"


def fmt_price(value: Optional[float]) -> str:
    return "n/a" if value is None else f"{value:.2f}"


def fmt_pct(value: Optional[float]) -> str:
    return "n/a" if value is None else f"{value:.1f}%"


def fmt_ibkr_quote(c: Candidate) -> str:
    if c.ibkr_error:
        return "n/a"
    if c.ibkr_last is None and c.ibkr_bid is None and c.ibkr_ask is None:
        return "n/a"
    bid_ask = "n/a"
    if c.ibkr_bid is not None and c.ibkr_ask is not None:
        bid_ask = f"{c.ibkr_bid:.2f}/{c.ibkr_ask:.2f}"
    return f"{fmt_price(c.ibkr_last)} ({bid_ask})"


def fmt_ibkr_option_quote(c: Candidate) -> str:
    if c.ibkr_option_bid is None and c.ibkr_option_ask is None and c.ibkr_option_mid is None and c.ibkr_option_mark is None:
        return "n/a"
    bid_ask = "n/a"
    if c.ibkr_option_bid is not None and c.ibkr_option_ask is not None:
        bid_ask = f"{c.ibkr_option_bid:.2f}/{c.ibkr_option_ask:.2f}"
    mark = c.ibkr_option_mid or c.ibkr_option_mark
    return f"{bid_ask} m={fmt_price(mark)}"


def fmt_int_pair(left: Optional[int], right: Optional[int]) -> str:
    if left is None and right is None:
        return "n/a"
    return f"{left if left is not None else 'n/a'}/{right if right is not None else 'n/a'}"


def render(result: ScanResult, symbols_count: int) -> str:
    now_ny = dt.datetime.now(tz=NY).strftime("%Y-%m-%d %H:%M ET")
    candidates = result.candidates
    stats = result.stats
    stats_line = (
        f"过滤统计：候选 {stats.get('candidate', 0)} / 标的 {stats.get('symbols', symbols_count)}；"
        f"无行情 {stats.get('no_price', 0)}，无合适到期 {stats.get('no_expiry', 0)}，"
        f"无期权链 {stats.get('no_chain', 0)}，无 5%-18% OTM Put {stats.get('no_strike_band', 0)}，"
        f"未过 bid/OI {stats.get('no_bid_oi', 0)}，价差过宽 {stats.get('wide_spread', 0)}，"
        f"错误 {stats.get('error', 0)}；IBKR 正股复核 {stats.get('ibkr_quote_ok', 0)} / "
        f"{result.ibkr_attempted}，IBKR 期权复核 {stats.get('ibkr_option_ok', 0)} / {result.ibkr_attempted}。"
    )
    if result.data_quality_error:
        lines = [
            f"Optix Nasdaq-100 Sell Put 扫描（{now_ny}）",
            "",
            "⚠️ 扫描服务异常：不是‘零候选’。",
            result.data_quality_error,
            stats_line,
            f"运行环境：Python {sys.version.split()[0]} / yfinance {yf.__version__}。",
            f"成分股来源：{SYMBOL_SOURCE}。",
        ]
        if SYMBOL_WARNING:
            lines.append(f"成分股提醒：{SYMBOL_WARNING}")
        if result.errors:
            lines.append("错误样本：" + "；".join(result.errors))
        return "\n".join(lines)
    if not candidates:
        lines = [
            f"Optix Nasdaq-100 Sell Put 扫描（{now_ny}）",
            "",
            "未筛出满足流动性、价外距离和权利金条件的候选。",
            stats_line,
            f"成分股来源：{SYMBOL_SOURCE}。",
        ]
        if SYMBOL_WARNING:
            lines.append(f"成分股提醒：{SYMBOL_WARNING}")
        if result.errors:
            lines.append("错误样本：" + "；".join(result.errors))
        if result.ibkr_errors:
            lines.append("IBKR 错误样本：" + "；".join(result.ibkr_errors))
        return "\n".join(lines)
    lines = [
        f"Optix Nasdaq-100 Sell Put 扫描（{now_ny}）",
        "",
        f"范围：Nasdaq-100 {symbols_count} 个标的；筛选：7-24 DTE、约 5%-18% OTM、OI>=50、bid>=0.20、价差<=35%。",
        f"成分股来源：{SYMBOL_SOURCE}。{stats_line}",
        f"数据源：Yahoo/yfinance 做全市场初筛；Top {result.ibkr_attempted} 候选串行调用 Optix/IBKR `option-quote` 做单合约盘口复核，失败时保留 yfinance 初筛值作为 fallback。",
        "",
        "| # | 标的 | YF现价 | IBKR正股 last(bid/ask) | 到期 | Put | YF期权bid/ask | IBKR期权bid/ask mid | YF/IBKR OTM | YF年化 | IBKR年化 | YF/IBKR IV | YF/IBKR Δ | YF OI/Vol | IBKR OI/Vol |",
        "|---|---:|---:|---:|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|",
    ]
    for i, c in enumerate(candidates, 1):
        lines.append(
            f"| {i} | {c.symbol} | {c.spot:.2f} | {fmt_ibkr_quote(c)} | {c.expiry} ({c.dte}d) | "
            f"{c.strike:.2f} | {c.bid:.2f}/{c.ask:.2f} | {fmt_ibkr_option_quote(c)} | "
            f"{c.cushion_pct:.1f}%/{fmt_pct(c.ibkr_cushion_pct)} | "
            f"{c.annualized_yield_pct:.1f}% | {fmt_pct(c.ibkr_annualized_yield_pct)} | "
            f"{c.iv * 100:.0f}%/{fmt_pct(None if c.ibkr_option_iv is None else c.ibkr_option_iv * 100)} | "
            f"{fmt_delta(c.delta)}/{fmt_delta(c.ibkr_option_delta)} | {c.oi}/{c.volume} | "
            f"{fmt_int_pair(c.ibkr_option_oi, c.ibkr_option_volume)} |"
        )
    lines.extend([
        "",
        "提示：这只是候选池，不是下单指令；优先复核财报日、真实期权盘口、组合集中度和可接受接货价。",
        "口径：表内年化/OTM 为 mid 口径；排名打分用 bid 口径（卖方最差成交价），故行序可能与年化列不完全一致，按 bid 实际成交的年化会更低。",
        "IBKR 核实：`option-quote` 是 IBKR-only；没有自动 fallback 到 yfinance。脚本 fallback 逻辑是在 IBKR 失败/无数据时保留 yfinance 初筛值并标注错误样本。",
    ])
    if SYMBOL_WARNING:
        lines.append(f"成分股提醒：{SYMBOL_WARNING}")
    if result.errors:
        lines.append("错误样本：" + "；".join(result.errors))
    if result.ibkr_errors:
        lines.append("IBKR 错误样本：" + "；".join(result.ibkr_errors))
    return "\n".join(lines)


def main() -> int:
    parser = argparse.ArgumentParser(description="Nasdaq-100 sell-put income scan")
    parser.add_argument("--dry-run", action="store_true", help="Run even outside the scheduled NY time window")
    parser.add_argument("--no-ibkr", action="store_true", help="Skip IBKR stock quote verification")
    parser.add_argument("--ibkr-top", type=int, default=DEFAULT_IBKR_TOP_N, help="Number of top candidates to validate through Optix/IBKR")
    args = parser.parse_args()

    if not args.dry_run and not is_valid_window():
        return 0
    symbols = nasdaq100_symbols()
    ibkr_top_n = 0 if args.no_ibkr else args.ibkr_top
    print(render(scan(symbols, ibkr_top_n=ibkr_top_n), len(symbols)))
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception as exc:  # noqa: BLE001 — cron 包装层可能把 stderr 转发进飞书,禁止裸堆栈外泄
        print(f"Optix Nasdaq-100 Sell Put 扫描服务异常：{type(exc).__name__}: {exc}")
        sys.exit(1)
