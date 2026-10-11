#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
ptrade_adapter.py — easy-stock 信号 → 券商 Ptrade 执行 适配层（P3）

定位（见 V0.2_Execution_Integration.md §5.2）：
    - 本文件是**独立薄进程/工具**，不进 easy-stock Go 二进制。
    - Ptrade 与 QMT 本质不同：Ptrade 策略运行在**券商托管云**内，没有本地 API
      （xtquant 那种）。因此本适配器不"驱动"Ptrade，而是：
        1) 生成一份可直接上传到 Ptrade 的策略模板（order_target_percent 再平衡）；
        2) 在本地做 dry-run / 自测，验证目标权重→百分比换算与清仓逻辑；
        3) 通过网关 /execution 接口演示 sim 受理链（与 QMT 适配层一致）。
    - 合规红线：订单只经券商官方 Ptrade 策略环境下达；禁止裸连柜台 / 公有云跑单 /
            破解客户端。本适配器默认 dry-run / emit-template，**绝不触碰真实资金**。

运行模式：
    --mode dry-run      （默认）读网关目标 → 与(可选)当前权重 diff → 打印
                        order_target_percent 调仓计划。绝不发单、绝不写审计。
    --mode emit-template 生成可上传 Ptrade 的 strategy.py（默认写到 --output）。
    --mode export-json  把当前网关 core2 目标导出为 core2_target.json，供与策略
                        一同上传做离线兜底。
    --mode demo-sim     向网关 POST 一份 sim OrderIntent，打印网关模拟成交。

典型用法：
    # 1) 自测换算/归一化/清仓数学（无需服务）
    python ptrade_adapter.py --self-test

    # 2) dry-run：读网关目标 → 打印再平衡计划
    python ptrade_adapter.py --mode dry-run --gateway-url http://127.0.0.1:20081

    # 3) 生成可上传策略
    python ptrade_adapter.py --mode emit-template --output ptrade_strategy.py

    # 4) 导出目标 JSON（与策略同目录上传）
    python ptrade_adapter.py --mode export-json --output core2_target.json

依赖：
    - 标准库 + 可选 `requests`（无则降级 urllib）。
    - emit-template / export-json 不依赖 Ptrade 运行时；真正的下单发生在你把
      ptrade_strategy.py 上传到券商 Ptrade 后，由券商云执行。
"""

from __future__ import annotations

import argparse
import json
import os
import sys
import urllib.request
from datetime import datetime

# ---------------------------------------------------------------------------
# 常量（与 Go 网关 / QMT 适配层对齐）
# ---------------------------------------------------------------------------

SCHEMA_VERSION = "1.0.0"
DEFAULT_TOLERANCE = 0.02          # 目标权重偏离超过该比例才调仓
DEFAULT_GATEWAY_URL = "http://127.0.0.1:20081"
STRATEGY = "core2"

# Ptrade 代码后缀与 QMT 不同：沪市用 .SS、深市用 .SZ（QMT 用 .SH/.SZ）。
PTRADE_SH_SUFFIX = "SS"
PTRADE_SZ_SUFFIX = "SZ"


# ---------------------------------------------------------------------------
# 纯函数：代码归一化、权重→百分比、目标 diff → Ptrade 订单（可单测）
# ---------------------------------------------------------------------------

def normalize_code_for_ptrade(code: str) -> str:
    """把 QMT/网关代码（600519.SH / 000001.SZ）归一化为 Ptrade 代码
    （600519.SS / 000001.SZ）。已是 .SS/.SZ 则原样返回。"""
    if not code:
        return code
    base, _, suffix = code.partition(".")
    suffix = suffix.upper()
    if suffix == "SH":
        return f"{base}.{PTRADE_SH_SUFFIX}"
    if suffix == "SZ":
        return f"{base}.{PTRADE_SZ_SUFFIX}"
    return code


def weight_to_target_percent(target_weight: float) -> float:
    """目标权重 → Ptrade 目标百分比（order_target_percent 口径，0–100）。"""
    return round(float(target_weight) * 100.0, 4)


def compute_ptrade_orders(plan_targets: list[dict], current_weights: dict[str, float],
                          tolerance: float = DEFAULT_TOLERANCE) -> list[dict]:
    """由网关目标权重表与"当前各持仓目标权重"做差，产出 Ptrade 再平衡订单。

    - 每个目标：order_target_percent(code_SS, target_weight*100)。
    - 相对偏离 = |目标权重 − 当前权重|；< tolerance 且当前已持仓则跳过。
    - 当前持仓中不在目标里的标的：order_target_percent(code, 0) 清仓。
    返回订单列表：{code(Ptrade 后缀), action(REBALANCE|LIQUIDATE), target_percent}。
    注：实际股数换算由 Ptrade 在云端完成，本层只给出百分比指令。
    """
    orders: list[dict] = []
    target_codes: set[str] = set()

    for t in plan_targets:
        raw_code = t.get("code")
        if not raw_code:
            continue
        code = normalize_code_for_ptrade(raw_code)
        tw = float(t.get("target_weight") or 0.0)
        target_pct = weight_to_target_percent(tw)
        target_codes.add(code)
        cur = float(current_weights.get(code, 0.0) or 0.0)
        if cur > 0 and abs(tw - cur) < tolerance:
            continue  # 已在目标内，无需调仓
        orders.append({
            "code": code,
            "action": "REBALANCE",
            "target_percent": target_pct,
            "current_percent": round(cur * 100.0, 4),
        })

    # 清仓：当前持仓中不在目标里的标的
    for code, w in current_weights.items():
        code_norm = normalize_code_for_ptrade(code)
        if code_norm not in target_codes and float(w or 0.0) > 0:
            orders.append({
                "code": code_norm,
                "action": "LIQUIDATE",
                "target_percent": 0.0,
                "current_percent": round(float(w or 0.0) * 100.0, 4),
            })

    return orders


# ---------------------------------------------------------------------------
# 网关 HTTP 客户端（requests 优先，降级 urllib）
# ---------------------------------------------------------------------------

def _http_get_json(url: str, timeout: float = 10.0) -> dict:
    try:
        import requests  # type: ignore
        resp = requests.get(url, timeout=timeout)
        return resp.json()
    except ImportError:
        req = urllib.request.Request(url, headers={"Accept": "application/json"})
        with urllib.request.urlopen(req, timeout=timeout) as r:  # noqa: S310
            return json.loads(r.read().decode("utf-8"))


def _http_post_json(url: str, payload: dict, timeout: float = 10.0) -> dict:
    body = json.dumps(payload, ensure_ascii=False).encode("utf-8")
    try:
        import requests  # type: ignore
        resp = requests.post(url, data=body,
                             headers={"Content-Type": "application/json"},
                             timeout=timeout)
        return resp.json()
    except ImportError:
        req = urllib.request.Request(url, data=body,
                                    headers={"Content-Type": "application/json"})
        with urllib.request.urlopen(req, timeout=timeout) as r:  # noqa: S310
            return json.loads(r.read().decode("utf-8"))


def fetch_target(gateway_url: str, strategy: str = STRATEGY) -> dict:
    """拉取网关可执行目标：GET /api/v1/execution/target?strategy=core2。"""
    url = f"{gateway_url.rstrip('/')}/api/v1/execution/target?strategy={strategy}"
    data = _http_get_json(url)
    if not data.get("ok"):
        raise RuntimeError(f"网关 target 返回非 ok：{data}")
    plan = data.get("plan")
    if not plan:
        raise RuntimeError(f"网关 target 无 plan：{data}")
    return plan


def build_order_intent(plan: dict, execute_on: str = "sim",
                       account: str = "ENV") -> dict:
    """由网关 plan 构造统一 OrderIntent（适配层 → 网关受理契约）。"""
    targets = []
    for t in plan.get("targets", []):
        targets.append({
            "code": t.get("code"),
            "target_weight": t.get("target_weight"),
            "assumed_price": t.get("assumed_price"),
            "action": "HOLD",
        })
    return {
        "schema_version": SCHEMA_VERSION,
        "strategy": plan.get("strategy", STRATEGY),
        "pit_audit_passed": bool(plan.get("pit_audit_passed")),
        "signal_id": plan.get("signal_id", ""),
        "execute_on": execute_on,
        "account": account,
        "targets": targets,
        "constraints": {
            "max_single_weight": 0.10,
            "max_daily_turnover": 0.30,
            "round_lot": 100,
            "block_st": True,
            "block_suspended": True,
            "block_limit": True,
        },
        "generated_at": datetime.now().astimezone().isoformat(),
        "note": "由 ptrade_adapter 构造（P3）",
    }


def report_fill(gateway_url: str, fill: dict) -> dict:
    """回灌一笔成交到网关审计：POST /api/v1/execution/fill（source 默认 ptrade）。"""
    url = f"{gateway_url.rstrip('/')}/api/v1/execution/fill"
    fill.setdefault("source", "ptrade")
    return _http_post_json(url, fill)


# ---------------------------------------------------------------------------
# Ptrade 策略模板（可上传到券商 Ptrade 云；订单只经官方策略环境下达）
# ---------------------------------------------------------------------------

PTRACE_STRATEGY_TEMPLATE = '''# ptrade_strategy.py — easy-stock core2 再平衡策略（上传到 Ptrade）
#
# 合规：本策略在券商 Ptrade 官方策略环境下运行；不裸连柜台、不在公有云自建跑单服务。
# 下单仅通过 Ptrade 提供的 order_target_percent 在 handle_data/before_trading_start
# /after_trading_end 内下达。
#
# 部署步骤：
#   1) 将本文件与 core2_target.json（由 ptrade_adapter.py --mode export-json 生成）
#      一同上传到 Ptrade；或填写下方 GATEWAY_TARGET_URL 指向可公网访问的网关。
#   2) Ptrade 在交易时段自动按目标权重再平衡。

import json
import os

from ptrade.api import (initialize, before_trading_start, handle_data,
                        after_trading_end, order_target_percent, get_current_data)

# 可调参数 ----------------------------------------------------------------
GATEWAY_TARGET_URL = ""   # 可选：填你的公网可达网关 /execution/target?strategy=core2
LOCAL_TARGET_JSON = "core2_target.json"   # 与策略同目录，离线兜底
TOLERANCE = 0.02          # 权重偏离小于该值不调仓
STRATEGY = "core2"


def normalize_code(code):
    base, _, suf = code.partition(".")
    suf = suf.upper()
    if suf == "SH":
        return base + ".SS"
    if suf == "SZ":
        return base + ".SZ"
    return code


def load_targets():
    """返回 [(ptrade_code, target_percent), ...]，优先网关，失败回退本地 JSON。"""
    plan = None
    if GATEWAY_TARGET_URL:
        try:
            import requests
            plan = requests.get(GATEWAY_TARGET_URL, timeout=10).json().get("plan", {})
        except Exception as exc:   # noqa: BLE001
            print("[easy-stock] 网关拉取失败，回退本地 JSON: %s" % exc)
    if not plan:
        try:
            with open(LOCAL_TARGET_JSON, "r", encoding="utf-8") as fh:
                plan = json.load(fh).get("plan", {})
        except Exception as exc:   # noqa: BLE001
            print("[easy-stock] 本地目标读取失败: %s" % exc)
            return []
    out = []
    for t in plan.get("targets", []):
        code = normalize_code(t.get("code", ""))
        if not code:
            continue
        out.append((code, float(t.get("target_weight") or 0.0) * 100.0))
    return out


def initialize(context):
    context.targets = []
    context.rebalanced_on = None


def before_trading_start(context):
    context.targets = load_targets()
    context.rebalanced_on = context.current_dt.date()


def handle_data(context, data):
    today = context.current_dt.date()
    if context.rebalanced_on != today:
        context.targets = load_targets()
        context.rebalanced_on = today
    apply_rebalance(context)


def after_trading_end(context):
    pass


def apply_rebalance(context):
    target_map = dict(context.targets)
    target_codes = set(target_map.keys())
    # 现有持仓（Ptrade 持仓 key 形如 600519.SS）
    held = {normalize_code(k): v for k, v in context.portfolio.positions.items()}
    # 调仓到目标权重
    for code, pct in context.targets:
        order_target_percent(code, pct)
    # 清仓不在目标里的持仓
    for code in held:
        if code not in target_codes:
            order_target_percent(code, 0)
'''


# ---------------------------------------------------------------------------
# 运行模式
# ---------------------------------------------------------------------------

def run_self_test() -> int:
    """校验归一化 / 百分比 / diff 数学（无需服务）。"""
    print("== self-test ==")
    ok = True

    # 1) 代码归一化
    assert normalize_code_for_ptrade("600519.SH") == "600519.SS", "沪市 .SH→.SS"
    assert normalize_code_for_ptrade("000001.SZ") == "000001.SZ", "深市 .SZ 不变"
    assert normalize_code_for_ptrade("600519.SS") == "600519.SS", "已是 .SS 不变"
    print("  [ok] normalize_code_for_ptrade")

    # 2) 权重→百分比
    assert weight_to_target_percent(0.10) == 10.0, "10% → 10.0"
    assert weight_to_target_percent(0.3333) == 33.33, "33.33% 四舍五入"
    print("  [ok] weight_to_target_percent")

    # 3) 空目标 → 全 REBALANCE
    plan = [{"code": "600519.SH", "target_weight": 0.20},
            {"code": "000001.SZ", "target_weight": 0.15}]
    orders = compute_ptrade_orders(plan, {})
    assert len(orders) == 2, f"应 2 笔再平衡，得到 {orders}"
    assert all(o["action"] == "REBALANCE" for o in orders), "全为 REBALANCE"
    assert any(o["code"] == "600519.SS" and o["target_percent"] == 20.0 for o in orders)
    print("  [ok] compute_ptrade_orders 空仓全再平衡")

    # 4) 已在目标内 → 跳过
    cur = {"600519.SS": 0.20, "000001.SZ": 0.15}
    orders = compute_ptrade_orders(plan, cur)
    assert orders == [], f"已在目标内应无订单，得到 {orders}"
    print("  [ok] compute_ptrade_orders 在目标内跳过")

    # 5) 清仓非目标标的
    plan2 = [{"code": "600519.SH", "target_weight": 1.0}]
    orders = compute_ptrade_orders(plan2, {"600519.SS": 0.5, "000001.SZ": 0.5})
    liq = [o for o in orders if o["action"] == "LIQUIDATE"]
    assert any(o["code"] == "000001.SZ" for o in liq), "应清仓非目标标的"
    print("  [ok] compute_ptrade_orders 清仓非目标标的")

    print("== self-test passed ==" if ok else "== self-test FAILED ==")
    return 0 if ok else 1


def run_dry_run(args) -> int:
    """dry-run：读目标 + 与当前权重 diff + 打印 order_target_percent 计划。"""
    print("== dry-run（不触达任何券商，不写审计）==")
    if args.plan_file:
        with open(args.plan_file, "r", encoding="utf-8") as f:
            plan = json.load(f)
        if "plan" in plan:
            plan = plan["plan"]
    else:
        plan = fetch_target(args.gateway_url, args.strategy)

    targets = plan.get("targets", [])
    print(f"  信号: signal_id={plan.get('signal_id')} "
          f"gross_weight={plan.get('gross_weight')} "
          f"n_holdings={plan.get('n_holdings')} mode={plan.get('mode')}")
    print(f"  目标持仓 {len(targets)} 只：")
    for t in targets:
        print(f"    {normalize_code_for_ptrade(t.get('code', '')):<12} "
              f"w={t.get('target_weight'):.4f} -> {weight_to_target_percent(t.get('target_weight'))}%")

    current_weights: dict[str, float] = {}
    if args.positions_file:
        with open(args.positions_file, "r", encoding="utf-8") as f:
            current_weights = {k: float(v) for k, v in json.load(f).items()}

    orders = compute_ptrade_orders(targets, current_weights, args.tolerance)
    print(f"\n  以偏离阈值 {args.tolerance:.2%} 生成 Ptrade 再平衡指令：")
    if not orders:
        print("    （无调仓需求）")
    for o in orders:
        tag = "清仓" if o["action"] == "LIQUIDATE" else "调仓"
        print(f"    {tag:<4} {o['code']:<12} -> {o['target_percent']:.2f}% "
              f"(当前 {o['current_percent']:.2f}%)")
    print("\n  dry-run 完成：以上仅为计划，未下发任何订单（真下单在 Ptrade 云内）。")
    return 0


def run_emit_template(args) -> int:
    """emit-template：写出可上传 Ptrade 的策略文件。"""
    out = args.output or "ptrade_strategy.py"
    with open(out, "w", encoding="utf-8") as f:
        f.write(PTRACE_STRATEGY_TEMPLATE)
    print(f"== emit-template ==")
    print(f"  已写出 Ptrade 策略模板：{out}")
    print(f"  部署：将 {out} 与 core2_target.json 一同上传到 Ptrade；")
    print(f"  或填写 GATEWAY_TARGET_URL 指向公网可达网关。真下单在 Ptrade 云内执行。")
    return 0


def run_export_json(args) -> int:
    """export-json：导出当前网关 core2 目标为 core2_target.json（与策略同传）。"""
    plan = fetch_target(args.gateway_url, args.strategy)
    out = args.output or "core2_target.json"
    with open(out, "w", encoding="utf-8") as f:
        json.dump({"ok": True, "plan": plan}, f, ensure_ascii=False, indent=2)
    print(f"== export-json ==")
    print(f"  已导出 {len(plan.get('targets', []))} 只目标到 {out}")
    print(f"  将该文件与策略模板一同上传到 Ptrade 做离线兜底。")
    return 0


def run_demo_sim(args) -> int:
    """demo-sim：向网关 POST 一份 sim OrderIntent，打印网关模拟成交。"""
    print("== demo-sim（向网关提交 sim 意图，演示受理 + 模拟成交）==")
    plan = fetch_target(args.gateway_url, args.strategy)
    intent = build_order_intent(plan, execute_on="sim", account="DEMO")
    url = f"{args.gateway_url.rstrip('/')}/api/v1/execution/intent"
    ack = _http_post_json(url, intent)
    if not ack.get("ok"):
        print(f"  受理失败：{ack}")
        return 1
    a = ack.get("ack", {})
    print(f"  accepted={a.get('accepted')} mode={a.get('mode')} "
          f"intent_id={a.get('intent_id')}")
    fills = a.get("fills", [])
    print(f"  网关模拟成交 {len(fills)} 笔：")
    for fl in fills:
        print(f"    {fl.get('side'):<4} {fl.get('code'):<12} "
              f"{fl.get('shares')} 股 @ {fl.get('price')} src={fl.get('source')}")
    print("  demo-sim 完成（网关已将该批模拟成交记入审计）。")
    return 0


# ---------------------------------------------------------------------------
# 入口
# ---------------------------------------------------------------------------

def build_parser() -> argparse.ArgumentParser:
    p = argparse.ArgumentParser(
        description="easy-stock → Ptrade 执行适配层（P3，默认 dry-run）")
    p.add_argument("--mode", choices=["dry-run", "emit-template", "export-json", "demo-sim"],
                   default="dry-run", help="运行模式（默认 dry-run，不碰资金）")
    p.add_argument("--gateway-url", default=DEFAULT_GATEWAY_URL,
                   help="easy-stock 后端地址")
    p.add_argument("--strategy", default=STRATEGY, help="策略名（默认 core2）")
    p.add_argument("--plan-file", help="dry-run 用：本地 plan JSON 文件")
    p.add_argument("--positions-file", help="dry-run 用：本地当前权重 JSON {code:weight}")
    p.add_argument("--tolerance", type=float, default=DEFAULT_TOLERANCE)
    p.add_argument("--output", help="emit-template/export-json 输出文件")
    p.add_argument("--self-test", action="store_true",
                   help="仅运行换算/归一化/清仓自测（无需服务）")
    return p


def main(argv: list[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    if args.self_test:
        return run_self_test()
    if args.mode == "dry-run":
        return run_dry_run(args)
    if args.mode == "emit-template":
        return run_emit_template(args)
    if args.mode == "export-json":
        return run_export_json(args)
    if args.mode == "demo-sim":
        return run_demo_sim(args)
    print("未知模式", file=sys.stderr)
    return 2


if __name__ == "__main__":
    raise SystemExit(main())
