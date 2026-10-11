#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
qmt_adapter.py — easy-stock 信号 → 券商 QMT 执行 适配层（P2）

定位（见 V0.2_Execution_Integration.md §5.1）：
    - 本文件是**独立薄进程**，不进 easy-stock Go 二进制（xtquant 仅 Python）。
    - 职责：拉取网关 `/execution/target` → 与账户实际持仓 diff → 整百股换算 →
            调 QMT 下单 → 回传 `/execution/fill` 入审计。
    - 合规红线：订单只经券商官方 QMT 策略环境下达；禁止裸连柜台 / 公有云跑单 /
            破解客户端。本适配器默认 dry-run，**绝不触碰真实资金**。

运行模式：
    --mode dry-run （默认）  不依赖 xtquant，仅读取目标 + 与(可选)持仓 diff +
                              计算并【打印】订单计划。绝不发单、绝不写审计。
    --mode paper            需要 `pip install xtquant` 且本机已启动 QMT 客户端；
                              连 QMT 纸面账户，真实下纸面单并回灌成交。
    --demo-sim              向网关 POST 一份 sim OrderIntent，打印网关返回的
                              模拟成交（演示受理链路；网关已自行入审计）。

典型用法：
    # 1) 自测换算与 diff 数学（无需服务、无需 QMT）
    python qmt_adapter.py --self-test

    # 2) dry-run：读网关目标 → 与空持仓 diff → 打印完整建仓计划
    python qmt_adapter.py --mode dry-run --gateway-url http://127.0.0.1:8080

    # 3) dry-run：用本地 plan 文件（离线核对，适合 CI）
    python qmt_adapter.py --mode dry-run --plan-file /tmp/plan.json

    # 4) 纸面实跑（需 QMT 已登录纸面账户）：下一交易日上午 09:28 自动触发一次
    python qmt_adapter.py --mode paper --qmt-path "D:/迅投极速交易终端/userdata_mini" \
        --account 你的资金账号 --session-id 123456

依赖：
    - dry-run / demo-sim / self-test：仅标准库 + 可选 `requests`（无则降级为 urllib）。
    - paper：额外 `pip install xtquant`（券商提供的 Python 包，Python 3.6–3.12 64 位）。
"""

from __future__ import annotations

import argparse
import json
import os
import sys
import time
import urllib.request
from datetime import datetime, time as dtime

# ---------------------------------------------------------------------------
# 常量（与 Go 网关 limits.go / 适配层约定对齐）
# ---------------------------------------------------------------------------

SCHEMA_VERSION = "1.0.0"
DEFAULT_ROUND_LOT = 100
DEFAULT_TOLERANCE = 0.02          # 偏离目标权重超过该比例才调仓
DEFAULT_REBALANCE_TIME = dtime(9, 28)  # 盘前拉目标 + 09:30 前下达
DEFAULT_GATEWAY_URL = "http://127.0.0.1:8080"
STRATEGY = "core2"


# ---------------------------------------------------------------------------
# 纯函数：权重 → 股数、目标 diff → 订单（可单测，无副作用）
# ---------------------------------------------------------------------------

def weight_to_volume(target_weight: float, total_asset: float, price: float,
                    round_lot: int = DEFAULT_ROUND_LOT) -> int:
    """目标权重 → 整百股（A 股买入向下取整到 round_lot）。

    与 V0.2 §5.1 的 `weight_to_volume` 一致：raw = w * 总资产 / 价；向下取整到整手。
    """
    if price is None or price <= 0:
        return 0
    if target_weight is None or target_weight <= 0:
        return 0
    raw = target_weight * total_asset / price
    return int(raw // round_lot) * round_lot


def compute_orders(plan_targets: list[dict], positions: dict[str, int],
                  total_asset: float, round_lot: int = DEFAULT_ROUND_LOT,
                  tolerance: float = DEFAULT_TOLERANCE) -> list[dict]:
    """把网关目标权重表与当前持仓做差，产出需要下发的买卖单。

    - 单票目标股数 = weight_to_volume(target_weight, total_asset, assumed_price)。
    - delta = 目标股数 − 当前股数；|delta| < 1 手 跳过。
    - 相对偏离 = |delta| / max(目标股数, 当前股数)；< tolerance 且已有持仓则跳过。
    - 当前持仓中不在目标里的标的，整仓卖出（清仓）。
    返回订单列表：{code, side(BUY|SELL), shares, limit_price}。
    """
    orders: list[dict] = []
    target_codes: set[str] = set()

    for t in plan_targets:
        code = t.get("code")
        if not code:
            continue
        target_codes.add(code)
        price = float(t.get("assumed_price") or 0.0)
        tw = float(t.get("target_weight") or 0.0)
        target_shares = weight_to_volume(tw, total_asset, price, round_lot)
        current = int(positions.get(code, 0))
        delta = target_shares - current
        if abs(delta) < round_lot:
            continue  # 不足一手，忽略
        denom = max(target_shares, current)
        rel = (abs(delta) / denom) if denom else 0.0
        if current > 0 and rel < tolerance:
            continue  # 已在目标内，无需调仓
        orders.append({
            "code": code,
            "side": "BUY" if delta > 0 else "SELL",
            "shares": abs(delta),
            "limit_price": price if price > 0 else None,
        })

    # 清仓：持仓中不在目标里的标的
    for code, sh in positions.items():
        if code not in target_codes and int(sh) > 0:
            orders.append({
                "code": code,
                "side": "SELL",
                "shares": int(sh),
                "limit_price": None,  # 市价清仓（paper 模式再取实时价）
            })

    return orders


# ---------------------------------------------------------------------------
# 网关 HTTP 客户端（requests 优先，降级 urllib；不引入额外硬依赖）
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
    """拉取网关可执行目标：GET /api/v1/execution/target?strategy=core2。

    返回 plan 字典（含 targets / gross_weight / signal_id 等）。
    """
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
            "round_lot": DEFAULT_ROUND_LOT,
            "block_st": True,
            "block_suspended": True,
            "block_limit": True,
        },
        "generated_at": datetime.now().astimezone().isoformat(),
        "note": "由 qmt_adapter 构造（P2）",
    }


def report_fill(gateway_url: str, fill: dict) -> dict:
    """回灌一笔成交到网关审计：POST /api/v1/execution/fill。"""
    url = f"{gateway_url.rstrip('/')}/api/v1/execution/fill"
    return _http_post_json(url, fill)


# ---------------------------------------------------------------------------
# QMT 会话（paper 模式，xtquant 懒加载）
# ---------------------------------------------------------------------------

class QmtSession:
    """封装 xtquant 连接与下单。仅 paper 模式使用；xtquant 缺失时构造即失败。"""

    def __init__(self, qmt_path: str, account: str, session_id: int):
        try:
            from xtquant.xttrader import XtQuantTrader  # type: ignore
            from xtquant.xttype import StockAccount     # type: ignore
            from xtquant import xtconstant               # type: ignore
        except ImportError as e:  # pragma: no cover - 取决于券商环境
            raise RuntimeError(
                "未找到 xtquant（QMT 官方 Python 包）。paper 模式需先 "
                "`pip install xtquant` 并启动 QMT 客户端。") from e

        self._xt = XtQuantTrader(qmt_path, session_id=session_id)
        self._StockAccount = StockAccount
        self._xtconstant = xtconstant
        self._account = account
        self._acc = StockAccount(account)
        self._xt.start()
        if not self._xt.connect():
            raise RuntimeError("QMT 连接失败，请确认客户端已启动并登录")

    def query_positions(self) -> dict[str, int]:
        """返回 {code: 当前持仓股数}。"""
        out: dict[str, int] = {}
        positions = self._xt.query_stock_positions(self._acc) or []
        for p in positions:
            out[getattr(p, "stock_code", "")] = int(getattr(p, "volume", 0))
        return out

    def query_asset(self) -> float:
        """返回账户总资产（用于权重→股数换算）。"""
        asset = self._xt.query_stock_asset(self._acc)
        return float(getattr(asset, "cash", 0.0) + getattr(asset, "market_value", 0.0))

    def order_stock(self, code: str, side: str, shares: int,
                    limit_price: float | None) -> str:
        """下限价单：side=BUY/SELL。返回券商 order_id（字符串）。"""
        price = float(limit_price) if limit_price else 0.0
        order_type = (self._xtconstant.STOCK_BUY if side == "BUY"
                      else self._xtconstant.STOCK_SELL)
        # 限价单；price=0 时由 xtquant 按市价处理（paper 环境可用）
        oid = self._xt.order_stock(self._acc, code, order_type, price, shares,
                                   STRATEGY, "rebalance")
        return str(oid)

    def query_fills(self, order_id: str) -> list[dict]:
        """查询某订单的成交流水（paper 模式轮询用）。

        注：xtquant 实际通过回调推送成交；此处用查询接口做轮询兜底。
        返回 Fill 结构列表（不含 source，由调用方补）。
        """
        # 简化骨架：真实实现应基于回调累积；这里给出占位查询结构。
        orders = self._xt.query_stock_orders(self._acc) or []
        fills: list[dict] = []
        for o in orders:
            if str(getattr(o, "order_id", "")) != order_id:
                continue
            dealt = int(getattr(o, "deal_volume", 0))
            if dealt <= 0:
                continue
            fills.append({
                "code": getattr(o, "stock_code", ""),
                "side": "BUY" if getattr(o, "order_type", 0) == self._xtconstant.STOCK_BUY
                        else "SELL",
                "price": float(getattr(o, "deal_amount", 0)) / dealt if dealt else 0.0,
                "shares": dealt,
                "notional": float(getattr(o, "deal_amount", 0)),
                "order_id": order_id,
            })
        return fills


# ---------------------------------------------------------------------------
# 运行模式
# ---------------------------------------------------------------------------

def run_self_test() -> int:
    """校验换算与 diff 数学（无需服务/无需 QMT）。"""
    print("== self-test ==")
    ok = True

    # 1) 整百股换算
    assert weight_to_volume(0.10, 100000, 100.0, 100) == 100, "等权 10% 应 100 股"
    assert weight_to_volume(0.10, 100000, 40.0, 100) == 200, "向下取整到整百（250→200）"
    assert weight_to_volume(0.10, 100000, 333.0, 100) == 0, "不足一手返 0"
    assert weight_to_volume(0.10, 100000, 0.0, 100) == 0, "价为 0 返 0"
    assert weight_to_volume(0.0, 100000, 100.0, 100) == 0, "权重 0 返 0"
    print("  [ok] weight_to_volume 整手换算")

    # 2) 与空持仓 diff → 全仓建仓（价 50 / 12，10% 在 10w 账户均≥1 手）
    plan = [{"code": "600519.SH", "target_weight": 0.10, "assumed_price": 50.0},
            {"code": "000001.SZ", "target_weight": 0.10, "assumed_price": 12.0}]
    orders = compute_orders(plan, {}, 100000)
    assert len(orders) == 2, f"空仓应产生 2 笔买入，得到 {orders}"
    assert all(o["side"] == "BUY" for o in orders), "建仓全为 BUY"
    print("  [ok] compute_orders 空仓全建仓")

    # 3) 已在目标内 → 跳过
    positions = {"600519.SH": 200, "000001.SZ": 800}  # 精确目标股数
    orders = compute_orders(plan, positions, 100000)
    # 600519: 0.10*100000/50=200 股，当前 200→delta 0 跳过
    # 000001: 0.10*100000/12=833→800 股，当前 800→delta 0 跳过
    assert orders == [], f"已在目标内应无订单，得到 {orders}"
    print("  [ok] compute_orders 在目标内跳过")

    # 4) 清仓不在目标里的标的
    plan2 = [{"code": "600519.SH", "target_weight": 1.0, "assumed_price": 50.0}]
    orders = compute_orders(plan2, {"600519.SH": 200, "000001.SZ": 500}, 100000)
    sells = [o for o in orders if o["side"] == "SELL"]
    assert any(o["code"] == "000001.SZ" and o["shares"] == 500 for o in sells), \
        "应清仓非目标标的"
    print("  [ok] compute_orders 清仓非目标标的")

    print("== self-test passed ==" if ok else "== self-test FAILED ==")
    return 0 if ok else 1


def run_dry_run(args) -> int:
    """dry-run：读目标 + 与持仓 diff + 打印订单计划（绝不发单/绝不写审计）。"""
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
        print(f"    {t.get('code'):<12} w={t.get('target_weight'):.4f} "
              f"px={t.get('assumed_price')} shares={t.get('shares')}")

    # 持仓来源：--positions-file 或默认空仓（纯建仓视图）
    positions: dict[str, int] = {}
    if args.positions_file:
        with open(args.positions_file, "r", encoding="utf-8") as f:
            positions = {k: int(v) for k, v in json.load(f).items()}

    total_asset = float(args.asset) if args.asset else 100000.0
    orders = compute_orders(targets, positions, total_asset,
                            args.round_lot, args.tolerance)
    print(f"\n  以总资产 ¥{total_asset:,.0f} 换算，偏离阈值 {args.tolerance:.2%}：")
    if not orders:
        print("    （无调仓需求）")
    for o in orders:
        print(f"    {o['side']:<4} {o['code']:<12} {o['shares']:>6} 股 "
              f"限价={o['limit_price']}")
    print("\n  dry-run 完成：以上仅为计划，未下发任何订单。")
    return 0


def run_demo_sim(args) -> int:
    """demo-sim：向网关 POST 一份 sim OrderIntent，打印网关模拟成交（演示受理链路）。"""
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


def run_paper(args) -> int:  # pragma: no cover - 依赖券商 QMT 环境
    """paper：连 QMT 纸面账户真实下纸面单，并回灌成交到网关审计。"""
    print("== paper（QMT 纸面账户，真实但虚拟资金）==")
    sess = QmtSession(args.qmt_path, args.account, args.session_id)

    # 等待到盘前触发时刻（--once 则立即执行一次）
    if not args.once:
        _wait_until(DEFAULT_REBALANCE_TIME)

    plan = fetch_target(args.gateway_url, args.strategy)
    positions = sess.query_positions()
    total_asset = sess.query_asset()
    orders = compute_orders(plan.get("targets", []), positions, total_asset,
                            args.round_lot, args.tolerance)
    if not orders:
        print("  无需调仓，退出。")
        return 0

    print(f"  将下发 {len(orders)} 笔纸面单：")
    for o in orders:
        oid = sess.order_stock(o["code"], o["side"], o["shares"], o["limit_price"])
        print(f"    {o['side']:<4} {o['code']:<12} {o['shares']} 股 "
              f"限价={o['limit_price']} -> order_id={oid}")
        # 轮询成交并回灌（简化：立即查一次；生产应基于回调累积）
        time.sleep(0.5)
        for fl in sess.query_fills(oid):
            fl["source"] = "qmt"
            fl["fill_id"] = f"fill_{oid}_{fl['code']}"
            fl["filled_at"] = datetime.now().astimezone().isoformat()
            fl["note"] = "QMT 纸面成交"
            rep = report_fill(args.gateway_url, fl)
            print(f"      成交回灌 {fl['code']} {fl['shares']} 股 -> {rep.get('ok')}")
    print("  paper 完成：纸面单已下达，成交已回灌网关审计。")
    return 0


def _wait_until(target: dtime) -> None:  # pragma: no cover
    now = datetime.now().time()
    if now < target:
        sleep_sec = (datetime.combine(datetime.today(), target)
                     - datetime.now()).total_seconds()
        print(f"  等待至 {target} 触发（约 {sleep_sec:.0f}s）…")
        time.sleep(sleep_sec)


# ---------------------------------------------------------------------------
# 入口
# ---------------------------------------------------------------------------

def build_parser() -> argparse.ArgumentParser:
    p = argparse.ArgumentParser(
        description="easy-stock → QMT 执行适配层（P2，默认 dry-run）")
    p.add_argument("--mode", choices=["dry-run", "paper", "demo-sim"],
                   default="dry-run", help="运行模式（默认 dry-run，不碰资金）")
    p.add_argument("--gateway-url", default=DEFAULT_GATEWAY_URL,
                   help="easy-stock 后端地址")
    p.add_argument("--strategy", default=STRATEGY, help="策略名（默认 core2）")
    p.add_argument("--plan-file", help="dry-run 用：本地 plan JSON 文件")
    p.add_argument("--positions-file", help="dry-run 用：本地持仓 JSON {code:shares}")
    p.add_argument("--asset", type=float, help="dry-run 假定的账户总资产")
    p.add_argument("--round-lot", type=int, default=DEFAULT_ROUND_LOT)
    p.add_argument("--tolerance", type=float, default=DEFAULT_TOLERANCE)
    p.add_argument("--qmt-path", default=os.getenv("QMT_USERDATA",
                   r"D:\迅投极速交易终端 睿智融科版\userdata_mini"))
    p.add_argument("--account", default=os.getenv("QMT_ACCOUNT", ""))
    p.add_argument("--session-id", type=int,
                   default=int(os.getenv("QMT_SESSION_ID", "123456")))
    p.add_argument("--once", action="store_true", help="paper：立即跑一次，不等盘前")
    p.add_argument("--self-test", action="store_true",
                   help="仅运行换算/diff 自测（无需服务/QMT）")
    return p


def main(argv: list[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    if args.self_test:
        return run_self_test()
    if args.mode == "dry-run":
        return run_dry_run(args)
    if args.mode == "demo-sim":
        return run_demo_sim(args)
    if args.mode == "paper":
        return run_paper(args)
    print("未知模式", file=sys.stderr)
    return 2


if __name__ == "__main__":
    raise SystemExit(main())
