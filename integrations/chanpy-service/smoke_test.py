"""用合成 K 线做端到端冒烟测试：不依赖后端，验证 chan.py 引擎 + 数据源 + 结构提取。"""
import datetime as dt
import json
import os
import random
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import chanpy_service as svc
from DataAPI import backendAPI


def make_rows(n=600, start_price=10.0):
    """多段震荡行情：跌→筑底→涨→回调→再涨，噪声足够大让笔反复生成。"""
    random.seed(7)
    price = start_price
    rows = []
    cur = dt.date(2023, 8, 1)
    while len(rows) < n:
        if cur.weekday() >= 5:
            cur += dt.timedelta(days=1)
            continue
        phase = len(rows) % 60
        if phase < 25:
            drift = -0.02
        elif phase < 35:
            drift = 0.0
        else:
            drift = 0.02
        change = drift + random.gauss(0, 0.02)
        open_p = price
        close = max(1.0, price * (1 + change))
        high = max(open_p, close) * (1 + abs(random.gauss(0, 0.006)))
        low = min(open_p, close) * (1 - abs(random.gauss(0, 0.006)))
        rows.append({
            "time": cur.isoformat(),
            "open": round(open_p, 2), "high": round(high, 2),
            "low": round(low, 2), "close": round(close, 2),
            "volume": random.randint(8_000_000, 30_000_000),
            "amount": random.randint(80_000_000, 300_000_000),
        })
        price = close
        cur += dt.timedelta(days=1)
    return rows


def main():
    backendAPI.PRELOAD["TEST01"] = make_rows()
    backendAPI.STOCK_INFO["TEST01"] = ("测试股", True)

    from Chan import CChan
    from ChanConfig import CChanConfig
    conf = svc.parse_conf("")
    started = dt.datetime.now()
    chan = CChan(
        code="TEST01", begin_time=None, end_time=None,
        data_src="custom:backendAPI.CBackendStockAPI",
        lv_list=[svc.kl_type_for("day")],
        config=CChanConfig(conf),
    )
    elapsed = (dt.datetime.now() - started).total_seconds()
    rows = backendAPI.PRELOAD["TEST01"]
    structure = svc.extract_structure(chan, len(rows), float(rows[-1]["close"]))
    summary = svc.score_structure(structure, 15)
    print(json.dumps({
        "compute_seconds": round(elapsed, 3),
        "bi_count": structure["bi_count"],
        "zs_count": structure["zs_count"],
        "bsp_count": structure["bsp_count"],
        "seg_count": structure["seg_count"],
        "last_close": structure["last_close"],
        "zs_position": structure["zs_position"] and structure["zs_position"]["state"],
        "bsps": [
            {"labels": b["labels"], "is_buy": b["is_buy"], "time": b["time"], "is_sure": b["is_sure"]}
            for b in structure["bsp"][-6:]
        ],
        "summary": summary,
    }, ensure_ascii=False, indent=1))

    # screen 的过滤逻辑离线验证（买点 + 最近500根内）
    filters = {"bs_types": ["1", "2", "3a", "3b", "1p", "2s"], "side": "buy", "bsp_recent_bars": 500}
    check = svc.screen_filters(filters, structure, summary)
    print("filter_result:", json.dumps(check, ensure_ascii=False))
    assert structure["bi_count"] > 10, f"笔数异常 {structure['bi_count']}"
    assert structure["bsp_count"] > 0, "未产生买卖点"
    print("SMOKE_OK")


if __name__ == "__main__":
    main()
