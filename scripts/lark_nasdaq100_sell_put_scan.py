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
import signal
import subprocess
import sys
import time
import urllib.request
import warnings
from dataclasses import dataclass, field
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
    portfolio_labels: str = ""      # 组合感知标注(空串=无重叠);只进 Lark 显示,不进 journal
    portfolio_penalty: float = 0.0  # 组合感知扣分;只影响显示排序,不改 score


@dataclass
class ScanResult:
    candidates: List[Candidate]
    stats: Dict[str, int]
    errors: List[str]
    ibkr_errors: List[str]
    ibkr_attempted: int
    data_quality_error: Optional[str] = None


# ── 组合感知（spec 2026-08-08）────────────────────────────────────────────
# 契约:只改「读表顺序」,不改「进表资格」——top-N 与 journal 全维持市场面
# score 口径,penalty 只作用于 Lark 显示排序。


def normalize_portfolio_symbol(symbol: str) -> str:
    # IBKR 类股符号带空格(如 "BRK B"),yfinance 用连字符;NDX 成分股当前
    # 无点分类股,upper+空格转连字符已覆盖 —— 已知简化,写在 spec §3。
    return symbol.strip().upper().replace(" ", "-")


@dataclass
class ShortPut:
    strike: float
    expiry: str  # YYYY-MM-DD(由 positions 的 YYYYMMDD 转换)
    qty: float   # 负数为空头


@dataclass
class Holding:
    stock_qty: float = 0.0
    short_puts: List[ShortPut] = field(default_factory=list)


def build_holdings_index(rows: List[dict]) -> Dict[str, Holding]:
    """positions --format json 的记录列表 → {规范化 symbol: Holding}。
    单条畸形记录跳过(宽进);正股数量跨账户求和;只收 short put 期权腿。"""
    index: Dict[str, Holding] = {}
    for row in rows:
        try:
            symbol = normalize_portfolio_symbol(str(row["symbol"]))
            sec_type = str(row.get("sec_type", "")).upper()
            qty = float(row["quantity"])
        except (KeyError, TypeError, ValueError):
            continue
        if not symbol or symbol == "NONE" or qty == 0:
            continue
        if sec_type == "STK":
            index.setdefault(symbol, Holding()).stock_qty += qty
        elif sec_type == "OPT" and str(row.get("right", "")).upper() == "P" and qty < 0:
            expiration = str(row.get("expiration", ""))
            try:
                strike = float(row.get("strike", 0))
            except (TypeError, ValueError):
                continue
            if len(expiration) == 8 and expiration.isdigit() and strike > 0:
                expiry = f"{expiration[:4]}-{expiration[4:6]}-{expiration[6:]}"
                index.setdefault(symbol, Holding()).short_puts.append(
                    ShortPut(strike=strike, expiry=expiry, qty=qty))
    return index


PORTFOLIO_POSITIONS_TIMEOUT = 30


def fetch_portfolio_positions() -> Tuple[Optional[List[dict]], Optional[str]]:
    """调 `optix positions --format json`。成功 (rows, None);任何故障
    (None, 紧凑原因) —— 组合感知是增强项,绝不让它失败整个扫描。"""
    try:
        completed = run_optix_subprocess(
            ["bash", optix_script(), "positions", "--format", "json"],
            timeout=PORTFOLIO_POSITIONS_TIMEOUT,
        )
    except Exception as exc:
        return None, f"{type(exc).__name__}: {exc}"
    if completed.returncode != 0:
        err_lines = (completed.stderr or completed.stdout or "").strip().splitlines()
        return None, compact_ibkr_error("; ".join(err_lines[-2:]))
    try:
        data = json.loads(completed.stdout)
    except Exception as exc:
        return None, f"parse positions JSON: {exc}"
    rows = data.get("positions")
    if not isinstance(rows, list):
        return None, "positions JSON 缺 positions 列表"
    return rows, None


PORTFOLIO_PENALTY_STOCK = 0.10        # 仅持正股 —— 接货会叠加既有仓位
PORTFOLIO_PENALTY_SHORT_PUT = 0.25    # 已有同名 short put(不同到期)
PORTFOLIO_PENALTY_SAME_EXPIRY = 0.40  # 同名同到期撞车(strike 不论);score 典型
                                      # 量级 0.4-0.9,0.40 压到表尾但不消失


def classify_candidate(cand: Candidate, index: Dict[str, Holding]) -> Tuple[str, float]:
    """候选 vs 持仓索引 → (标注串, penalty)。多情形并存取最大 penalty、标注全显。"""
    holding = index.get(normalize_portfolio_symbol(cand.symbol))
    if holding is None:
        return "", 0.0
    labels: List[str] = []
    penalty = 0.0
    if holding.short_puts:
        if any(p.expiry == cand.expiry for p in holding.short_puts):
            labels.append("撞期!")
            penalty = max(penalty, PORTFOLIO_PENALTY_SAME_EXPIRY)
        else:
            penalty = max(penalty, PORTFOLIO_PENALTY_SHORT_PUT)
        nearest = min(holding.short_puts, key=lambda p: p.expiry)
        detail = f"sp {nearest.strike:g}@{nearest.expiry[5:]}"
        if len(holding.short_puts) > 1:
            detail += f"×{len(holding.short_puts)}"
        labels.append(detail)
    if holding.stock_qty > 0:
        labels.append("正股")
        penalty = max(penalty, PORTFOLIO_PENALTY_STOCK)
    elif holding.stock_qty < 0:
        labels.append("空股")  # 卖 put 对空头实为对冲方向 —— 仅标注,不扣分
    return " ".join(labels), penalty


def apply_portfolio_awareness(
    candidates: List[Candidate], index: Dict[str, Holding], fetched_at: str
) -> str:
    """就地写入每个候选的 portfolio_labels/portfolio_penalty,返回摘要行。"""
    flagged = penalized = 0
    for cand in candidates:
        labels, penalty = classify_candidate(cand, index)
        cand.portfolio_labels = labels
        cand.portfolio_penalty = penalty
        flagged += 1 if labels else 0
        penalized += 1 if penalty > 0 else 0
    if flagged == 0:
        return "组合感知: 与当前持仓无重叠"
    return f"组合感知: 持仓 {len(index)} 标的，{penalized} 项降权（positions@{fetched_at}）"


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


def run_optix_subprocess(
    cmd: List[str], timeout: int, stdin_text: Optional[str] = None
) -> "subprocess.CompletedProcess[str]":
    """subprocess.run 的优雅版:超时先 SIGTERM 进程组(给 optix 的信号清理注册表
    断开 IB Gateway 的机会,防僵尸 clientID),宽限 5s 后再 SIGKILL。
    stdin_text 非 None 时通过管道喂给子进程 stdin(scan-journal register 等需要
    从 stdin 读 JSON 的子命令用);为 None 时保持原行为,不接管 stdin。"""
    proc = subprocess.Popen(
        cmd, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True,
        stdin=subprocess.PIPE if stdin_text is not None else None,
        start_new_session=True,
    )
    try:
        out, err = proc.communicate(input=stdin_text, timeout=timeout)
    except subprocess.TimeoutExpired:
        with contextlib.suppress(ProcessLookupError):
            os.killpg(proc.pid, signal.SIGTERM)
        try:
            out, err = proc.communicate(timeout=5)
        except subprocess.TimeoutExpired:
            with contextlib.suppress(ProcessLookupError):
                os.killpg(proc.pid, signal.SIGKILL)
            out, err = proc.communicate()
        raise subprocess.TimeoutExpired(cmd, timeout, output=out, stderr=err)
    except KeyboardInterrupt:
        # start_new_session 让子进程脱离终端前台进程组,手动 Ctrl-C 不会自动
        # 送达 —— 这里补上进程组 SIGTERM,防止孤儿 optix 占住 IBKR clientID
        # (正是本修复要消灭的僵尸来源,不能自己再引入一个)。
        # register 是 Go 侧单事务原子——KI 中断产生的半写 stdin 最多导致
        # register 端 JSON 解析失败、整批拒绝,不会半批入库;KI 随后继续
        # 向上传播终止全脚本。
        with contextlib.suppress(ProcessLookupError):
            os.killpg(proc.pid, signal.SIGTERM)
        with contextlib.suppress(Exception):
            proc.communicate(timeout=5)
        raise
    return subprocess.CompletedProcess(cmd, proc.returncode, out, err)


def fetch_ibkr_stock_quote(symbol: str) -> Tuple[Optional[Dict], Optional[str]]:
    try:
        completed = run_optix_subprocess(
            ["bash", optix_script(), "quote", symbol, "--format", "json"],
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
        completed = run_optix_subprocess(
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
    if "option quote unavailable:" in message:
        # optix #193 finding 1: a bad strike/expiry/right now fails fast with
        # the real IB per-request error (e.g. errCode 200 "no security
        # definition") instead of burning the full collection window and
        # reporting the misleading "no usable price data" below. Surface
        # that real reason verbatim rather than falling through to the
        # generic 240-char truncation.
        return "IBKR option quote invalid: " + message.split("option quote unavailable:", 1)[-1].strip()
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


JOURNAL_REGISTER_TIMEOUT = 10
JOURNAL_RECONCILE_TIMEOUT = 30


def build_journal_payload(candidates: List[Candidate], symbol_source: str) -> dict:
    """市场序 Top-N 候选 → `optix scan-journal register` 的 stdin payload。
    契约(spec 2026-08-08 §6):调用方必须传 **市场面 score 降序** 列表
    (ScanResult.candidates 的原序);组合感知只重排 Lark 显示,rank 语义与
    2026-08-05 起的样本保持同口径。rank 按列表顺序 1..N,ibkr_* 仅在非
    None 时携带(与 Go 侧 CandidateInput 的 omitempty 语义对齐)。"""
    rows = []
    for i, c in enumerate(candidates, 1):
        row = {
            "rank": i, "symbol": c.symbol, "expiry": c.expiry, "dte": c.dte,
            "strike": c.strike, "spot": c.spot, "bid": c.bid, "ask": c.ask, "mid": c.mid,
            "iv": c.iv, "delta": c.delta, "oi": c.oi, "volume": c.volume,
            "cushion_pct": c.cushion_pct, "premium_yield_pct": c.premium_yield_pct,
            "annualized_yield_pct": c.annualized_yield_pct, "score": c.score,
        }
        for key, value in (("ibkr_bid", c.ibkr_bid), ("ibkr_ask", c.ibkr_ask),
                           ("ibkr_option_iv", c.ibkr_option_iv),
                           ("ibkr_option_delta", c.ibkr_option_delta)):
            if value is not None:
                row[key] = value
        rows.append(row)
    return {"symbol_source": symbol_source, "candidates": rows}


def run_scan_journal(
    args: List[str], stdin_json: Optional[str] = None, timeout: int = 30
) -> Tuple[Optional[dict], Optional[str]]:
    """调 `optix scan-journal` 子命令；返回 (解析后的 JSON | None, 错误串 | None)。
    走 run_optix_subprocess（进程组 SIGTERM→5s 宽限→SIGKILL），stdin_json 经
    stdin_text 管道喂给子进程（register 靠它接收候选 payload）。"""
    try:
        completed = run_optix_subprocess(
            ["bash", optix_script(), "scan-journal", *args],
            timeout=timeout, stdin_text=stdin_json,
        )
    except Exception as exc:
        return None, f"scan-journal {args[0]}: {type(exc).__name__}: {exc}"
    if completed.returncode != 0:
        err_lines = (completed.stderr or completed.stdout or "").strip().splitlines()
        return None, f"scan-journal {args[0]}: " + "; ".join(err_lines[-2:])[:240]
    try:
        return json.loads(completed.stdout), None
    except Exception as exc:
        return None, f"scan-journal {args[0]}: parse JSON: {exc}"


def render_review_section(rec: dict) -> list:
    if not rec or rec.get("settled", 0) <= 0:
        return []
    lines = ["", f"—— 复盘（本次结算 {rec['settled']} 笔）——",
             "| 标的 | Put | 到期 | 结果 | 到期收盘 | P&L/股 | 曾触及 | 最深击穿 |",
             "|---|---:|---|---|---:|---:|---|---:|"]
    for r in rec.get("results", []):
        mark = "✅ hit" if r["outcome"] == "hit" else ("❌ miss" if r["outcome"] == "miss" else "⚪ void")
        touched = "是" if r.get("touched") else "否"
        breach = f"{r['max_breach_pct']:.1f}%" if r.get("touched") else "–"
        lines.append(
            f"| {r['symbol']} | {r['strike']:.0f} | {r['expiry'][5:]} | {mark} | "
            f"{r['expiry_close']:.2f} | {r['realized_pnl']:+.2f} | {touched} | {breach} |")
    hr = rec.get("hit_rate", {})
    denom = hr.get("hit", 0) + hr.get("miss", 0)
    if denom > 0:
        sign = "+" if hr["avg_pnl"] >= 0 else "-"
        lines.append(
            f"累计：hit {hr['hit']} / miss {hr['miss']} / void {hr.get('void', 0)} · "
            f"命中率 {hr['rate'] * 100:.0f}% · "
            f"平均 P&L {sign}${abs(hr['avg_pnl']):.2f}/股")
    return lines


def run_journal_flow(
    result: ScanResult, *, dry_run: bool, no_journal: bool, with_journal: bool
) -> Tuple[List[str], List[str]]:
    """执行 journal 接线(register + reconcile),返回 (复盘段行, 警示行)。
    任何 journal 失败都只产生警示行 —— 扫描消息必须照发。"""
    journal_active = not no_journal and (not dry_run or with_journal)
    review_lines: List[str] = []
    journal_notes: List[str] = []
    if not journal_active:
        return review_lines, journal_notes
    if not result.data_quality_error and result.candidates:
        payload = build_journal_payload(result.candidates, SYMBOL_SOURCE)
        _, err = run_scan_journal(["register"], stdin_json=json.dumps(payload),
                                  timeout=JOURNAL_REGISTER_TIMEOUT)
        if err:
            journal_notes.append(f"复盘入库失败：{err}")
    rec, err = run_scan_journal(["reconcile"], timeout=JOURNAL_RECONCILE_TIMEOUT)
    if err:
        journal_notes.append(f"复盘对账失败：{err}")
    else:
        review_lines = render_review_section(rec)
    return review_lines, journal_notes


def render(result: ScanResult, symbols_count: int, portfolio_line: Optional[str] = None) -> str:
    now_ny = dt.datetime.now(tz=NY).strftime("%Y-%m-%d %H:%M ET")
    candidates = sorted(result.candidates,
                        key=lambda c: c.score - c.portfolio_penalty, reverse=True)
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
        if portfolio_line:
            lines.append(portfolio_line)
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
        "| # | 标的 | 持仓 | YF现价 | IBKR正股 last(bid/ask) | 到期 | Put | YF期权bid/ask | IBKR期权bid/ask mid | YF/IBKR OTM | YF年化 | IBKR年化 | YF/IBKR IV | YF/IBKR Δ | YF OI/Vol | IBKR OI/Vol |",
        "|---|---:|---|---:|---:|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|",
    ]
    for i, c in enumerate(candidates, 1):
        lines.append(
            f"| {i} | {c.symbol} | {c.portfolio_labels or '-'} | {c.spot:.2f} | {fmt_ibkr_quote(c)} | {c.expiry} ({c.dte}d) | "
            f"{c.strike:.2f} | {c.bid:.2f}/{c.ask:.2f} | {fmt_ibkr_option_quote(c)} | "
            f"{c.cushion_pct:.1f}%/{fmt_pct(c.ibkr_cushion_pct)} | "
            f"{c.annualized_yield_pct:.1f}% | {fmt_pct(c.ibkr_annualized_yield_pct)} | "
            f"{c.iv * 100:.0f}%/{fmt_pct(None if c.ibkr_option_iv is None else c.ibkr_option_iv * 100)} | "
            f"{fmt_delta(c.delta)}/{fmt_delta(c.ibkr_option_delta)} | {c.oi}/{c.volume} | "
            f"{fmt_int_pair(c.ibkr_option_oi, c.ibkr_option_volume)} |"
        )
    if portfolio_line:
        lines.extend(["", portfolio_line])
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
    parser.add_argument("--no-journal", action="store_true", help="Skip scan-journal register/reconcile")
    parser.add_argument("--with-journal", action="store_true", help="Force journal writes even with --dry-run")
    args = parser.parse_args()

    if not args.dry_run and not is_valid_window():
        return 0
    symbols = nasdaq100_symbols()
    ibkr_top_n = 0 if args.no_ibkr else args.ibkr_top
    result = scan(symbols, ibkr_top_n=ibkr_top_n)
    review_lines, journal_notes = run_journal_flow(
        result, dry_run=args.dry_run, no_journal=args.no_journal, with_journal=args.with_journal)
    body = render(result, len(symbols))
    extra = review_lines + journal_notes
    if extra:
        body = body + "\n" + "\n".join(extra)
    print(body)
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception as exc:  # noqa: BLE001 — cron 包装层可能把 stderr 转发进飞书,禁止裸堆栈外泄
        print(f"Optix Nasdaq-100 Sell Put 扫描服务异常：{type(exc).__name__}: {exc}")
        sys.exit(1)
