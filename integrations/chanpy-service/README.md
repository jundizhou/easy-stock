# chanpy-service：chan.py 缠论引擎服务

把 [Vespa314/chan.py](https://github.com/Vespa314/chan.py)（MIT License）接入
easy-stock 的进程外服务，支撑「缠论选股」板块：

- `analyze`：单只股票的笔 / 线段 / 中枢 / 一二三类买卖点完整结构与确定性评分；
- `screen`：批量选股，对股票池逐只计算缠论结构并按买卖点类型、中枢位置、
  当前笔方向、时效与评分过滤。

K 线通过 easy-stock 后端 `/api/v1/quotes/kline` 回调获取（并发预取 + 串行计算），
输出为单个 JSON 信封（`ok` + 载荷），与 `czsc-service/analyze.py` 的约定一致。
核心计算只依赖 Python ≥3.11 标准库，无需 pip 安装任何第三方包。

## 目录结构

```text
chanpy-service/
├── chanpy_service.py      # 服务脚本（Go 侧调用的入口）
├── smoke_test.py          # 离线冒烟测试（合成K线，不依赖后端）
└── chanpy/                # vendored chan.py 核心代码（保留原 MIT LICENSE）
    ├── Chan.py / ChanConfig.py / __init__.py
    ├── Bi/ BuySellPoint/ ChanModel/ Combiner/ Common/
    ├── DataAPI/           # 含自定义的 backendAPI.py（easy-stock 后端数据源）
    └── KLine/ Math/ Seg/ ZS/
```

## 部署

后端按以下顺序探测脚本与解释器：

1. 环境变量 `A_STOCK_CHANPY_SCRIPT` / `A_STOCK_CHANPY_PYTHON`；
2. 本目录（`integrations/chanpy-service`，仓库克隆即用）；
3. `easy-stock-env/chanpy-service` 约定部署目录（`K:\easy-stock-env` 或
   用户主目录下的同名目录），并复用其中的 `a-stock-data-venv` 解释器。

任意 Python ≥3.11 均可运行，如需独立环境：

```bash
python3.11 -m venv .venv
```

## 验证

```bash
# 离线冒烟（合成K线，不依赖后端）
python smoke_test.py

# 真实链路（需 easy-stock 后端已启动）
python chanpy_service.py analyze --symbol 600519.SH --period day --limit 500 \
    --backend http://127.0.0.1:20081
python chanpy_service.py screen --symbols "600519.SH,000001.SZ" --limit 400 \
    --backend http://127.0.0.1:20081 \
    --filters '{"side":"buy","bs_types":["2","3a","3b"],"bsp_recent_bars":30}'
```

详见 [docs/chan-engine.md](../../docs/chan-engine.md)。
