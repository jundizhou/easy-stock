"""easy-stock 后端数据源。

通过 chan.py 的 custom 数据源机制接入：
    data_src="custom:backendAPI.CBackendStockAPI"

K 线不由本类现场请求，而是由 chanpy_service.py 预取后塞进 PRELOAD 注册表
（键为股票代码）。这样选股时可以用线程池并发拉数（IO 密集），chan.py 计算
仍保持串行，互不拖累。
"""
import datetime as _dt
import json
import os
import urllib.parse
import urllib.request
from typing import Dict, List, Optional

from Common.CEnum import KL_TYPE
from Common.ChanException import CChanException, ErrCode
from Common.CTime import CTime
from KLine.KLine_Unit import CKLine_Unit

from .CommonStockAPI import CCommonStockApi

# code -> [{"time": "2024-01-02", "open": .., "high": .., "low": .., "close": .., "volume": .., "amount": ..}]
PRELOAD: Dict[str, List[dict]] = {}
# code -> (name, is_stock)
STOCK_INFO: Dict[str, tuple] = {}


def parse_time(text: str) -> CTime:
    """后端时间串 → CTime。

    兼容三种形态：2024-01-02、2024-01-02 15:00、2024-01-02T15:00:00+08:00。
    """
    text = str(text).strip().replace("/", "-")
    try:
        # Python ≥3.11 的 fromisoformat 原生支持日期串、空格分隔与带时区偏移。
        stamp = _dt.datetime.fromisoformat(text)
    except ValueError:
        date_part, _, time_part = text.partition(" ")
        year, month, day = (int(x) for x in date_part.split("-")[:3])
        hour = minute = 0
        if time_part:
            pieces = time_part.split(":")
            hour = int(pieces[0])
            minute = int(pieces[1]) if len(pieces) > 1 else 0
        return CTime(year, month, day, hour, minute)
    return CTime(stamp.year, stamp.month, stamp.day, stamp.hour, stamp.minute)


class CBackendStockAPI(CCommonStockApi):
    def __init__(self, code, k_type=KL_TYPE.K_DAY, begin_date=None, end_date=None, autype=None):
        self.rows = PRELOAD.get(str(code))
        super().__init__(code, k_type, begin_date, end_date, autype)

    def get_kl_data(self):
        if not self.rows:
            raise CChanException(f"no preloaded kline for {self.code}", ErrCode.SRC_DATA_NOT_FOUND)
        for row in self.rows:
            time_text = str(row["time"])
            if self.begin_date is not None and time_text < str(self.begin_date):
                continue
            if self.end_date is not None and time_text > str(self.end_date):
                continue
            yield CKLine_Unit({
                "time_key": parse_time(row["time"]),
                "open": float(row["open"]),
                "high": float(row["high"]),
                "low": float(row["low"]),
                "close": float(row["close"]),
                "volume": float(row.get("volume") or 0.0),
                "turnover": float(row.get("amount") or 0.0),
            })

    def SetBasciInfo(self):
        info = STOCK_INFO.get(str(self.code))
        self.name = info[0] if info else None
        self.is_stock = info[1] if info else True

    @classmethod
    def do_init(cls):
        pass

    @classmethod
    def do_close(cls):
        pass


def fetch_backend_kline(backend: str, symbol: str, period: str, limit: int,
                        token: str = "", timeout: int = 20) -> List[dict]:
    """从 easy-stock 后端 /api/v1/quotes/kline 拉 K 线（升序返回）。"""
    query = urllib.parse.urlencode({"symbol": symbol, "period": period, "limit": limit})
    url = f"{backend.rstrip('/')}/api/v1/quotes/kline?{query}"
    headers = {"Accept": "application/json"}
    if token:
        headers["Authorization"] = f"Bearer {token}"
    request = urllib.request.Request(url, headers=headers)
    with urllib.request.urlopen(request, timeout=timeout) as response:
        payload = json.load(response)
    rows = payload.get("data") or []
    if not rows:
        raise CChanException(f"backend returned empty kline for {symbol}", ErrCode.SRC_DATA_NOT_FOUND)
    return rows


def preload_from_backend(backend: str, symbols: List[str], period: str, limit: int,
                         token: str = "", timeout: int = 20,
                         names: Optional[Dict[str, str]] = None,
                         on_error=None) -> Dict[str, str]:
    """批量预取 K 线。返回 {symbol: error}，成功的通过 PRELOAD 暴露给数据源。"""
    errors: Dict[str, str] = {}
    for symbol in symbols:
        try:
            PRELOAD[str(symbol)] = fetch_backend_kline(backend, symbol, period, limit, token, timeout)
            if names and names.get(symbol):
                STOCK_INFO[str(symbol)] = (names[symbol], True)
        except CChanException as exc:
            errors[symbol] = exc.msg if hasattr(exc, "msg") else str(exc)
            if on_error:
                on_error(symbol, errors[symbol])
        except Exception as exc:  # noqa: BLE001 - 逐只隔离，不让单只失败拖垮整批
            errors[symbol] = str(exc)
            if on_error:
                on_error(symbol, errors[symbol])
    return errors


def preload_from_rows(rows_by_symbol: Dict[str, List[dict]]) -> None:
    for symbol, rows in rows_by_symbol.items():
        PRELOAD[str(symbol)] = rows


def clear_preload() -> None:
    PRELOAD.clear()
    STOCK_INFO.clear()


def backend_token_from_env() -> str:
    return (os.environ.get("A_STOCK_TOKEN") or "").strip()
