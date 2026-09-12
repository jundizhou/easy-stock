#!/usr/bin/env python3
"""chan.py 服务脚本：把 Vespa314/chan.py 缠论引擎接入 easy-stock。

两个子命令：

  analyze  单只股票的完整缠论结构分析（笔/线段/中枢/买卖点 + 评分）
  screen   批量选股：对一组代码计算缠论结构，按买卖点/中枢位置等条件过滤并评分

K 线统一从 easy-stock 后端 /api/v1/quotes/kline 获取（与 czsc-service/analyze.py
相同的回调模式），并发预取后交给 chan.py 串行计算。输出始终是单个 JSON：
成功 ok=true，失败 ok=false 且 exit code 非零。

用法示例：
  python chanpy_service.py analyze --symbol 600519.SH --period day --limit 500 \
      --backend http://127.0.0.1:20081
  python chanpy_service.py screen --symbols "600519.SH,000001.SZ" --limit 400 \
      --backend http://127.0.0.1:20081 --filters '{"bs_types":["3a","3b"],"side":"buy"}'
"""
from __future__ import annotations

import argparse
import datetime as _dt
import json
import os
import sys
import time
from concurrent.futures import ThreadPoolExecutor
from typing import Any, Dict, List, Optional

SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
# vendored chan.py 位于脚本同级的 chanpy/ 目录，Chan.py/ChanConfig.py 等都是
# 该目录下的顶层模块，因此把该目录整体插入 sys.path。
CHANPY_ROOT = os.path.join(SCRIPT_DIR, "chanpy")
if CHANPY_ROOT not in sys.path:
    sys.path.insert(0, CHANPY_ROOT)

from Common.ChanException import CChanException  # noqa: E402
from DataAPI import backendAPI  # noqa: E402

DEFAULT_CONF: Dict[str, Any] = {
    "bi_strict": True,
    "bi_algo": "normal",
    "bi_fx_check": "strict",
    "gap_as_kl": False,
    "bi_end_is_peak": True,
    "bi_allow_sub_peak": True,
    "seg_algo": "chan",
    "left_seg_method": "peak",
    "zs_algo": "normal",
    "zs_combine": True,
    "zs_combine_mode": "zs",
    "one_bi_zs": False,
    "trigger_step": False,
    "skip_step": 0,
    "divergence_rate": float("inf"),
    "bsp2_follow_1": False,
    "bsp3_follow_1": False,
    "bsp3_peak": False,
    "bsp2s_follow_2": True,
    "min_zs_cnt": 0,
    "bs1_peak": False,
    "macd_algo": "peak",
    "bs_type": "1,1p,2,2s,3a,3b",
    "print_warning": False,
    "print_err_time": False,
}

# 允许调用方覆盖的配置键白名单；其余键会被丢弃，避免 CChanConfig 抛 PARA_ERROR。
CONF_KEYS = set(DEFAULT_CONF) | {
    "macd", "mean_metrics", "trend_metrics", "max_bs2_rate",
    "bsp1_only_multibi_zs", "max_bsp2s_lv", "strict_bsp3", "bsp3a_max_zs_cnt",
}

PERIOD_ALIASES = {
    "day": "day", "daily": "day", "d": "day",
    "week": "week", "weekly": "week", "w": "week",
    "month": "month", "monthly": "month",
    "60min": "60min", "30min": "30min", "15min": "15min", "5min": "5min",
}

# 买卖点类型 → 基础名（不带买卖方向），标签按 is_buy 拼接成 "二买"/"二卖" 等。
BS_TYPE_BASE_NAMES = {
    "1": "一", "1p": "类一", "2": "二", "2s": "类二",
    "3a": "三a", "3b": "三b",
}
BS_TYPE_WEIGHTS = {"1": 20, "1p": 16, "2": 24, "2s": 18, "3a": 28, "3b": 26}


def bs_type_label(bs_type: str, is_buy: bool) -> str:
    return f"{BS_TYPE_BASE_NAMES.get(bs_type, bs_type)}{'买' if is_buy else '卖'}"


# ─────────────────────────── 数据预取 ───────────────────────────

def parse_conf(raw: Optional[str]) -> Dict[str, Any]:
    """解析 --conf JSON，未知键丢弃，divergence_rate 支持 "inf"。"""
    if not raw or not raw.strip():
        return dict(DEFAULT_CONF)
    user = json.loads(raw)
    if not isinstance(user, dict):
        raise ValueError("conf 必须是 JSON 对象")
    conf = dict(DEFAULT_CONF)
    for key, value in user.items():
        if key not in CONF_KEYS:
            continue
        if key == "divergence_rate" and isinstance(value, str):
            value = float("inf") if value.strip().lower() == "inf" else float(value)
        conf[key] = value
    return conf


def preload_rows(backend: str, symbols: List[str], period: str, limit: int,
                 token: str, timeout: int, workers: int,
                 names: Optional[Dict[str, str]] = None) -> Dict[str, str]:
    """并发预取全部 K 线（IO 密集），返回 {symbol: error}。"""
    errors: Dict[str, str] = {}
    lock_threads = max(1, min(workers, 12))

    def fetch(symbol: str) -> None:
        try:
            backendAPI.PRELOAD[symbol] = backendAPI.fetch_backend_kline(
                backend, symbol, period, limit, token, timeout)
            if names and names.get(symbol):
                backendAPI.STOCK_INFO[symbol] = (names[symbol], True)
        except Exception as exc:  # noqa: BLE001 - 逐只隔离
            errors[symbol] = describe_error(exc)

    with ThreadPoolExecutor(max_workers=lock_threads) as pool:
        list(pool.map(fetch, symbols))
    return errors


def describe_error(exc: Exception) -> str:
    if isinstance(exc, CChanException):
        return f"{exc.errcode.name}: {exc.msg}"
    return str(exc) or exc.__class__.__name__


# ─────────────────────────── 结构提取 ───────────────────────────

def time_text(ctime) -> str:
    if ctime.hour or ctime.minute:
        return f"{ctime.year:04}-{ctime.month:02}-{ctime.day:02} {ctime.hour:02}:{ctime.minute:02}"
    return f"{ctime.year:04}-{ctime.month:02}-{ctime.day:02}"


def bi_payload(bi) -> Dict[str, Any]:
    start_klu, end_klu = bi.get_begin_klu(), bi.get_end_klu()
    start, end = start_klu.close, end_klu.close
    change = ((end - start) / start * 100.0) if start else 0.0
    direction = "up" if str(bi.dir).endswith("UP") else "down"
    return {
        "direction": direction,
        "start_time": time_text(start_klu.time),
        "end_time": time_text(end_klu.time),
        "start_price": round(float(start), 3),
        "end_price": round(float(end), 3),
        "change_percent": round(change, 2),
        "bars": bi.get_klu_cnt(),
        "amp": round(float(bi.amp()), 3),
        "is_sure": bool(bi.is_sure),
    }


def zs_payload(zs) -> Dict[str, Any]:
    return {
        "begin": time_text(zs.begin.time),
        "end": time_text(zs.end.time),
        "zd": round(float(zs.low), 3),
        "zg": round(float(zs.high), 3),
        "gg": round(float(zs.peak_high), 3),
        "dd": round(float(zs.peak_low), 3),
        "bi_count": len(list(zs.bi_lst)),
        "is_sure": bool(zs.is_sure),
    }


def bsp_payload(bsp) -> Dict[str, Any]:
    types = [t.value for t in bsp.type]
    labels = [bs_type_label(t, bsp.is_buy) for t in types]
    relate = bsp.relate_bsp1
    return {
        "is_buy": bool(bsp.is_buy),
        "types": types,
        "labels": labels,
        "type_str": ",".join(types),
        "time": time_text(bsp.klu.time),
        "price": round(float(bsp.klu.close), 3),
        # bar_index 是该买卖点K线在区间内的下标，用于计算时效（距今多少根）。
        "bar_index": int(bsp.klu.idx),
        # CBS_Point 本身没有 is_sure 字段：买卖点挂在笔的端点上，
        # 笔确认即买卖点确认（虚笔端点的买卖点后续可能消失）。
        "is_sure": bool(bsp.bi.is_sure),
        "is_segbsp": bool(bsp.is_segbsp),
        "relate_bsp1_time": time_text(relate.klu.time) if relate else "",
        "bi_direction": "up" if str(bsp.bi.dir).endswith("UP") else "down",
    }


def zs_position_of(last_close: float, zone: Optional[Dict[str, Any]]) -> Optional[Dict[str, Any]]:
    if not zone:
        return None
    if last_close > zone["zg"]:
        state = "above"
    elif last_close < zone["zd"]:
        state = "below"
    else:
        state = "inside"
    return {"state": state, "zone": zone}


def last_zone(zones: List[Dict[str, Any]]) -> Optional[Dict[str, Any]]:
    return zones[-1] if zones else None


def extract_structure(chan, bars: int, last_close: float = 0.0) -> Dict[str, Any]:
    """把 chan[0] 的缠论元素转成可 JSON 化的字典。"""
    kl = chan[0]
    bis = [bi_payload(bi) for bi in kl.bi_list]
    zones = [zs_payload(zs) for zs in kl.zs_list]
    bsps = [bsp_payload(bsp) for bsp in kl.bs_point_lst.bsp_iter()]
    if not last_close:
        # 合并K线里最后一根KLU的收盘价。
        last_close = float(kl[-1].lst[-1].close) if len(kl) > 0 else 0.0
    if not last_close:
        last_close = float(bis[-1]["end_price"]) if bis else 0.0
    return {
        "bars": bars,
        "bi_count": len(bis),
        "zs_count": len(zones),
        "seg_count": len(list(kl.seg_list)),
        "bsp_count": len(bsps),
        "last_close": round(last_close, 3),
        "bi": bis,
        "zs": zones,
        "bsp": bsps,
        "last_bi": bis[-1] if bis else None,
        "zs_position": zs_position_of(last_close, last_zone(zones)),
    }


# ─────────────────────────── 评分与选股 ───────────────────────────

def score_structure(structure: Dict[str, Any], recent_bars: int) -> Dict[str, Any]:
    """确定性打分：买卖点类型 + 中枢位置 + 当前笔方向 + 新鲜度衰减。"""
    score = 50.0
    reasons: List[str] = []
    bsp = structure.get("bsp") or []
    last_bsp: Optional[Dict[str, Any]] = None
    if bsp:
        last_bsp = bsp[-1]
        bars_since = max(0, structure["bars"] - 1 - last_bsp.get("bar_index", 0))
        if bars_since <= recent_bars:
            types = last_bsp["types"]
            weight = max(BS_TYPE_WEIGHTS.get(t, 0) for t in types) if types else 0
            if last_bsp["is_buy"]:
                score += weight
                reasons.append(f"最近出现{'+'.join(last_bsp['labels'])}（{bars_since}根K线前）")
            else:
                score -= weight
                reasons.append(f"最近出现{'+'.join(last_bsp['labels'])}（{bars_since}根K线前），注意风险")
        else:
            reasons.append(f"最近买卖点在 {recent_bars} 根K线之外，时效性弱")

    zs_pos = structure.get("zs_position")
    if zs_pos:
        zone = zs_pos["zone"]
        if zs_pos["state"] == "above":
            if not (last_bsp and last_bsp["is_buy"]):
                score += 10
            reasons.append(f"现价站上中枢 {zone['zd']:.2f}~{zone['zg']:.2f}")
        elif zs_pos["state"] == "below":
            if not (last_bsp and not last_bsp["is_buy"]):
                score -= 10
            reasons.append(f"现价跌破中枢 {zone['zd']:.2f}~{zone['zg']:.2f}")
        else:
            reasons.append(f"现价处于中枢 {zone['zd']:.2f}~{zone['zg']:.2f} 内部")

    last_bi = structure.get("last_bi")
    if last_bi:
        if last_bi["direction"] == "up":
            if not (last_bsp and not last_bsp["is_buy"]):
                score += 5
            reasons.append(f"当前笔向上（{last_bi['start_price']:.2f}→{last_bi['end_price']:.2f}）")
        else:
            if not (last_bsp and last_bsp["is_buy"]):
                score -= 5
            reasons.append(f"当前笔向下（{last_bi['start_price']:.2f}→{last_bi['end_price']:.2f}）")

    score = max(0.0, min(100.0, score))
    stance = "偏多" if score >= 60 else ("偏空" if score <= 40 else "中性")
    tone = "up" if score >= 60 else ("down" if score <= 40 else "flat")
    return {
        "score": round(score, 1),
        "stance": stance,
        "tone": tone,
        "reasons": reasons[:6],
        "conclusion": "；".join(reasons[:2]) if reasons else "结构信息不足",
    }


def last_bsp_index(structure: Dict[str, Any], bsp: Dict[str, Any]) -> int:
    """兼容旧字段名：买卖点K线在区间内的下标。"""
    return bsp.get("bar_index", 0)


def screen_filters(filters: Dict[str, Any], structure: Dict[str, Any],
                   summary: Dict[str, Any]) -> Dict[str, Any]:
    """对单只股票应用选股条件。返回 {matched, reasons, bsp}；bsp 是参与匹配的买卖点。"""
    reasons: List[str] = []
    side = (filters.get("side") or "buy").strip().lower()
    bs_types = [str(t).strip() for t in (filters.get("bs_types") or []) if str(t).strip()]
    recent_bars = int(filters.get("bsp_recent_bars") or 15)
    require_sure = bool(filters.get("require_sure", False))

    bsp = structure.get("bsp") or []
    candidate: Optional[Dict[str, Any]] = None
    for item in reversed(bsp):
        if side != "any" and item["is_buy"] != (side == "buy"):
            continue
        candidate = item
        break
    if not candidate:
        return {"matched": False, "reasons": [], "bsp": None}

    bars_since = max(0, structure.get("bars", 0) - 1 - candidate.get("bar_index", 0))
    if bars_since > recent_bars:
        return {"matched": False, "reasons": [], "bsp": None}
    if require_sure and not candidate["is_sure"]:
        return {"matched": False, "reasons": [], "bsp": None}
    if bs_types and not any(t in bs_types for t in candidate["types"]):
        return {"matched": False, "reasons": [], "bsp": None}

    reasons.append(f"{'+'.join(candidate['labels'])}（{bars_since}根K线前）")

    zs_state = filters.get("zs_state")
    if zs_state:
        pos = structure.get("zs_position")
        if not pos or pos["state"] != zs_state:
            return {"matched": False, "reasons": [], "bsp": None}
        reasons.append(f"中枢位置 {pos['state']}")

    bi_dir = filters.get("bi_direction")
    last_bi = structure.get("last_bi")
    if bi_dir and last_bi and last_bi["direction"] != bi_dir:
        return {"matched": False, "reasons": [], "bsp": None}

    min_score = float(filters.get("min_score") or 0)
    if summary["score"] < min_score:
        return {"matched": False, "reasons": [], "bsp": None}

    return {"matched": True, "reasons": reasons, "bsp": candidate}


# ─────────────────────────── 命令实现 ───────────────────────────

def run_analyze(args) -> Dict[str, Any]:
    conf = parse_conf(args.conf)
    names = parse_json_arg(args.names) or {}
    started = time.time()
    errors = preload_rows(args.backend, [args.symbol], args.period, args.limit,
                          args.token, args.timeout, 1, names)
    if args.symbol in errors:
        raise RuntimeError(f"获取K线失败: {errors[args.symbol]}")
    rows = backendAPI.PRELOAD[args.symbol]

    from Chan import CChan
    from ChanConfig import CChanConfig

    chan = CChan(
        code=args.symbol,
        begin_time=None,
        end_time=None,
        data_src="custom:backendAPI.CBackendStockAPI",
        lv_list=[kl_type_for(args.period)],
        config=CChanConfig(conf),
        autype=autype_for(args.autype),
    )
    structure = extract_structure(chan, len(rows))
    summary = score_structure(structure, recent_bars=int((parse_json_arg(args.filters) or {}).get("bsp_recent_bars") or 15))
    stock_info = backendAPI.STOCK_INFO.get(args.symbol)
    return {
        "ok": True,
        "symbol": args.symbol,
        "name": (stock_info[0] if stock_info else "") or names.get(args.symbol, ""),
        "period": args.period,
        "range": {"start": rows[0]["time"], "end": rows[-1]["time"], "bars": len(rows)},
        "structure": structure,
        "summary": summary,
        "elapsed_ms": int((time.time() - started) * 1000),
        "generated_at": _dt.datetime.now().astimezone().isoformat(timespec="seconds"),
        "engine": "chan.py",
    }


def run_screen(args) -> Dict[str, Any]:
    filters = parse_json_arg(args.filters) or {}
    names = parse_json_arg(args.names) or {}
    symbols = [s.strip() for s in args.symbols.split(",") if s.strip()]
    if not symbols:
        raise ValueError("symbols 不能为空")
    if len(symbols) > 200:
        raise ValueError("单次最多扫描 200 只")
    conf = parse_conf(args.conf)
    recent_bars = int(filters.get("bsp_recent_bars") or 15)
    started = time.time()

    errors = preload_rows(args.backend, symbols, args.period, args.limit,
                          args.token, args.timeout, args.workers, names)

    from Chan import CChan
    from ChanConfig import CChanConfig

    results: List[Dict[str, Any]] = []
    for symbol in symbols:
        if symbol in errors:
            continue
        try:
            rows = backendAPI.PRELOAD[symbol]
            chan = CChan(
                code=symbol,
                begin_time=None,
                end_time=None,
                data_src="custom:backendAPI.CBackendStockAPI",
                lv_list=[kl_type_for(args.period)],
                config=CChanConfig(dict(conf)),
                autype=autype_for(args.autype),
            )
            structure = extract_structure(chan, len(rows))
            summary = score_structure(structure, recent_bars)
            check = screen_filters(filters, structure, summary)
            info = backendAPI.STOCK_INFO.get(symbol)
            # 命中时展示参与匹配的那个买卖点（可能与全局最后一个不同）；
            # 未命中时回退到全局最后一个，供用户参考最新结构。
            shown_bsp = check.get("bsp") or (structure["bsp"][-1] if structure["bsp"] else None)
            results.append({
                "symbol": symbol,
                "name": (info[0] if info else "") or names.get(symbol, ""),
                "last_close": structure["last_close"],
                "matched": check["matched"],
                "match_reasons": check["reasons"],
                "score": summary["score"],
                "stance": summary["stance"],
                "last_bsp": shown_bsp,
                "zs_state": (structure.get("zs_position") or {}).get("state", ""),
                "bi_direction": (structure.get("last_bi") or {}).get("direction", ""),
                "bi_count": structure["bi_count"],
                "zs_count": structure["zs_count"],
                "reasons": summary["reasons"],
            })
        except CChanException as exc:
            errors[symbol] = describe_error(exc)
        except Exception as exc:  # noqa: BLE001 - 单只失败不拖垮整批
            errors[symbol] = describe_error(exc)

    results.sort(key=lambda item: (not item["matched"], -item["score"]))
    matched = sum(1 for item in results if item["matched"])
    return {
        "ok": True,
        "period": args.period,
        "filters": filters,
        "scanned": len(results),
        "matched": matched,
        "failed": len(errors),
        "errors": [{"symbol": s, "error": e} for s, e in errors.items()],
        "results": results,
        "elapsed_ms": int((time.time() - started) * 1000),
        "generated_at": _dt.datetime.now().astimezone().isoformat(timespec="seconds"),
        "engine": "chan.py",
    }


# ─────────────────────────── 工具函数 ───────────────────────────

def kl_type_for(period: str):
    from Common.CEnum import KL_TYPE
    mapping = {
        "day": KL_TYPE.K_DAY, "week": KL_TYPE.K_WEEK, "month": KL_TYPE.K_MON,
        "60min": KL_TYPE.K_60M, "30min": KL_TYPE.K_30M,
        "15min": KL_TYPE.K_15M, "5min": KL_TYPE.K_5M,
    }
    return mapping[period]


def autype_for(name: str):
    from Common.CEnum import AUTYPE
    return {"qfq": AUTYPE.QFQ, "hfq": AUTYPE.HFQ, "none": AUTYPE.NONE}.get(
        (name or "qfq").strip().lower(), AUTYPE.QFQ)


def parse_json_arg(raw: Optional[str]) -> Optional[Dict[str, Any]]:
    if raw and raw.strip():
        value = json.loads(raw)
        if isinstance(value, dict):
            return value
    return None


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="chan.py 缠论引擎服务（easy-stock）")
    sub = parser.add_subparsers(dest="mode", required=True)

    def common(p, with_symbol: bool):
        if with_symbol:
            p.add_argument("--symbol", required=True, help="股票代码，如 600519.SH")
        p.add_argument("--period", default="day", help="K线周期 day/week/month/60min/30min/15min/5min")
        p.add_argument("--limit", type=int, default=500, help="K线根数")
        p.add_argument("--backend", default="http://127.0.0.1:20081", help="easy-stock 后端地址")
        p.add_argument("--token", default=os.environ.get("A_STOCK_TOKEN", ""), help="后端访问令牌")
        p.add_argument("--autype", default="qfq", help="复权 qfq/hfq/none")
        p.add_argument("--conf", default="", help="CChanConfig 覆盖项 JSON")
        p.add_argument("--timeout", type=int, default=25, help="单只取数超时秒数")

    p_analyze = sub.add_parser("analyze", help="单只股票缠论分析")
    common(p_analyze, True)
    p_analyze.add_argument("--names", default="", help="{symbol: name} 映射 JSON")
    p_analyze.add_argument("--filters", default="", help="评分用条件 JSON（可选）")

    p_screen = sub.add_parser("screen", help="批量缠论选股")
    common(p_screen, False)
    p_screen.add_argument("--symbols", required=True, help="逗号分隔的股票代码")
    p_screen.add_argument("--filters", default="", help="选股条件 JSON")
    p_screen.add_argument("--names", default="", help="{symbol: name} 映射 JSON")
    p_screen.add_argument("--workers", type=int, default=8, help="并发取数线程数")
    return parser


def main() -> int:
    args = build_parser().parse_args()
    try:
        payload = run_analyze(args) if args.mode == "analyze" else run_screen(args)
    except Exception as exc:  # noqa: BLE001 - 约定：失败也输出 JSON
        print(json.dumps({"ok": False, "error": describe_error(exc)}, ensure_ascii=False))
        return 1
    print(json.dumps(payload, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    sys.exit(main())
