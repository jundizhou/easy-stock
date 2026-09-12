# 缠论引擎：chan.py 选股与买卖点分析

easy-stock 目前内置两条缠论计算链路，二者并列、互不依赖：

| 引擎 | 上游项目 | 部署目录 | 能力 | 前端入口 |
| --- | --- | --- | --- | --- |
| czsc 引擎 | [czsc](https://github.com/waditu/czsc) | `easy-stock-env/czsc-service` | 个股缠论结构、信号目录（246 个）、权重回测 | 个股分析 → 缠论 |
| chan.py 引擎 | [Vespa314/chan.py](https://github.com/Vespa314/chan.py) | `easy-stock-env/chanpy-service` | 一/二/三类买卖点（含 a/b 变体）、批量选股、结构评分 | 缠论选股 |

两条链路的取数方式相同：Python 脚本回调本机后端 `/api/v1/quotes/kline` 拉 K 线，
计算在本机完成，不经过任何外部服务。

## chan.py 引擎部署（缠论选股板块依赖）

1. 准备 Python ≥3.11（chan.py 官方要求；核心计算只用标准库，无需第三方包）。
   复用 `a-stock-data-venv` 虚拟环境即可。
2. 服务代码已随仓库自带：`integrations/chanpy-service/`（含服务脚本
   `chanpy_service.py`、自定义后端数据源与 vendored 的 chan.py 核心代码，
   原始 MIT License 保留，克隆即用）。也可以从上游重新复制：

   ```bash
   git clone https://github.com/Vespa314/chan.py.git
   mkdir -p /path/to/easy-stock-env/chanpy-service/chanpy
   cp -r chan.py/{Chan.py,ChanConfig.py,__init__.py,Bi,BuySellPoint,ChanModel,Combiner,Common,DataAPI,KLine,Math,Seg,ZS,LICENSE} \
         /path/to/easy-stock-env/chanpy-service/chanpy/
   ```

3. 把 `integrations/chanpy-service/` 下的 `chanpy_service.py` 与 `chanpy/` 目录
   放到以下任一位置：

   ```text
   easy-stock-env/
   ├── a-stock-data-venv/          # 复用的解释器（也可以用任意 Python ≥3.11）
   ├── czsc-service/               # czsc 引擎（另一个板块）
   └── chanpy-service/
       ├── chanpy_service.py       # 服务脚本（analyze / screen 两个子命令）
       └── chanpy/                 # vendored chan.py 核心代码
           ├── Chan.py / ChanConfig.py
           ├── Bi/ BuySellPoint/ Common/ DataAPI/ KLine/ Math/ Seg/ ZS/ ...
           └── DataAPI/backendAPI.py   # easy-stock 后端数据源
   ```

4. 重启 easy-stock。后端按以下顺序自动探测脚本与解释器：
   - 环境变量 `A_STOCK_CHANPY_PYTHON`（解释器）与 `A_STOCK_CHANPY_SCRIPT`（脚本）；
   - 仓库自带副本 `integrations/chanpy-service`（克隆即用）；
   - `easy-stock-env` 约定目录（`K:\easy-stock-env` 或用户主目录同名目录）；
   - 可执行文件相邻目录。

`GET /api/v1/stocks/chan-screen-status` 可直接查看可用性与缺失原因。

## 选股条件（filters）

| 字段 | 取值 | 说明 |
| --- | --- | --- |
| `side` | `buy` / `sell` / `any` | 只看买点 / 只看卖点 / 都看 |
| `bs_types` | `1` `1p` `2` `2s` `3a` `3b` 的子集 | 一类、类一、二类、类二、三类a（中枢后）、三类b（中枢前） |
| `bsp_recent_bars` | 整数 | 买卖点距今不超过多少根 K 线（时效） |
| `zs_state` | `above` / `inside` / `below` | 现价与最近中枢的位置关系 |
| `bi_direction` | `up` / `down` | 当前笔方向 |
| `min_score` | 0–100 | 结构评分下限 |
| `require_sure` | 布尔 | 是否只接受已确认（笔已完成）的买卖点 |

评分模型是确定性的：以买卖点类型权重为主体（三类 28 > 二类 24 > 一类 20 > 类一 16…，
卖点为负向），叠加中枢位置（±10）、当前笔方向（±5）。评分只用于排序参考，不构成投资建议。

## 环境变量

| 变量 | 作用 |
| --- | --- |
| `A_STOCK_CHANPY_PYTHON` | 指定 chan.py 使用的 Python 解释器 |
| `A_STOCK_CHANPY_SCRIPT` | 指定 `chanpy_service.py` 路径 |
| `A_STOCK_CHANPY_WORKDIR` | 脚本工作目录（默认脚本所在目录） |
| `A_STOCK_CZSC_ENV_ROOT` | 同时影响两个引擎的环境根目录探测 |
