package chananalysis

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestDecodeResultSuccess(t *testing.T) {
	payload := `{
		"ok": true,
		"symbol": "600519",
		"freq": "日线",
		"source": "eastmoney",
		"generated_at": "2026-09-10T16:00:00+08:00",
		"elapsed_ms": 420,
		"range": {"start": "2023-09-01", "end": "2026-09-04", "bars": 718},
		"structure": {
			"counts": {"bars": 718, "fx": 258, "bi": 50, "zs": 5},
			"last_close": 1338.86,
			"fx": [{"time": "2026-09-04", "price": 1338.86, "mark": "顶分型", "kind": "顶分型"}],
			"bi": [{
				"direction": "向上",
				"start": {"time": "2026-08-24", "price": 1270.33},
				"end": {"time": "2026-09-04", "price": 1338.86},
				"bars": 9, "power": 68.53, "slope": 7.614, "is_sure": true
			}],
			"zs": [{"start": "2026-06-01", "end": "2026-08-20", "zg": 1344.7, "zd": 1279.58,
				"gg": 1350.0, "dd": 1270.33, "amplitude": 0.05}],
			"current_bi": {
				"direction": "向上",
				"start": {"time": "2026-08-24", "price": 1270.33},
				"end": {"time": "2026-09-04", "price": 1338.86},
				"bars": 9, "power": 68.53, "slope": 7.614, "is_sure": true,
				"progress": 0.22, "sure": true
			},
			"zs_position": {
				"state": "中枢内部",
				"zone": {"start": "2026-06-01", "end": "2026-08-20", "zg": 1344.7, "zd": 1279.58,
					"gg": 1350.0, "dd": 1270.33, "amplitude": 0.05},
				"note": "现价 1338.86 在中枢 1279.58~1344.70 内震荡"
			},
			"divergence": [{"direction": "向上", "kind": "顶背驰", "time": "2026-08-03",
				"price": 1363.35, "prev_slope": 6.2, "slope": 4.59, "decay": 0.26}]
		},
		"summary": {
			"score": 82.9, "stance": "偏多", "tone": "up",
			"reasons": ["当前向上笔（2026-08-24→2026-09-04），结构偏多"],
			"conclusion": "当前向上笔（2026-08-24→2026-09-04），结构偏多"
		},
		"signals": [{
			"name": "cxt_bi_status_V230101", "label": "笔表里关系", "category": "kline",
			"value": "向上_顶分_任意_0", "glossary": "笔向上运行，当前为顶分型区域（可能面临回调）",
			"params": {"di": 1}, "ok": true
		}]
	}`

	result, err := decodeResult([]byte(payload), "600519")
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	if result.Symbol != "600519" || result.Freq != "日线" {
		t.Fatalf("基础字段错误: %+v", result)
	}
	if result.ElapsedMS != 420 {
		t.Fatalf("elapsed_ms 错误: %d", result.ElapsedMS)
	}
	if result.Range.Bars != 718 || result.Range.End != "2026-09-04" {
		t.Fatalf("range 错误: %+v", result.Range)
	}
	if result.Structure.Counts.Bi != 50 || result.Structure.Counts.FX != 258 {
		t.Fatalf("counts 错误: %+v", result.Structure.Counts)
	}
	if len(result.Structure.Bi) != 1 || result.Structure.Bi[0].Slope != 7.614 {
		t.Fatalf("bi 解析错误: %+v", result.Structure.Bi)
	}
	if result.Structure.CurrentBi == nil || result.Structure.CurrentBi.Progress != 0.22 {
		t.Fatalf("current_bi 解析错误: %+v", result.Structure.CurrentBi)
	}
	// CurrentBi 内嵌 Stroke，字段需正确提升。
	if result.Structure.CurrentBi.Direction != "向上" || result.Structure.CurrentBi.End.Price != 1338.86 {
		t.Fatalf("current_bi 内嵌字段错误: %+v", result.Structure.CurrentBi)
	}
	if result.Structure.ZsPosition == nil || result.Structure.ZsPosition.State != "中枢内部" {
		t.Fatalf("zs_position 解析错误: %+v", result.Structure.ZsPosition)
	}
	if result.Structure.ZsPosition.Zone.ZG != 1344.7 {
		t.Fatalf("zs_position.zone 解析错误: %+v", result.Structure.ZsPosition.Zone)
	}
	if len(result.Structure.Divergence) != 1 || result.Structure.Divergence[0].Decay != 0.26 {
		t.Fatalf("divergence 解析错误: %+v", result.Structure.Divergence)
	}
	if result.Summary.Score != 82.9 || result.Summary.Stance != "偏多" {
		t.Fatalf("summary 解析错误: %+v", result.Summary)
	}
	if len(result.Signals) != 1 || !result.Signals[0].OK {
		t.Fatalf("signals 解析错误: %+v", result.Signals)
	}
	if result.Signals[0].Glossary == "" {
		t.Fatal("glossary 未保留")
	}
	// generated_at 为带时区的 RFC3339，应解析成功。
	if result.GeneratedAt.Year() != 2026 || result.GeneratedAt.Hour() != 16 {
		t.Fatalf("generated_at 解析错误: %v", result.GeneratedAt)
	}
}

func TestDecodeResultBusinessError(t *testing.T) {
	// 脚本约定失败时输出 ok=false 且退出码非 0；exec 会原样把 JSON 交给上层。
	payload := `{"ok": false, "error": "无法连接后端 http://127.0.0.1:20081"}`

	_, err := decodeResult([]byte(payload), "600519")
	if err == nil {
		t.Fatal("期望返回错误")
	}
	if !strings.Contains(err.Error(), "无法连接后端") {
		t.Fatalf("错误信息未透传: %v", err)
	}
}

func TestDecodeResultMissingOK(t *testing.T) {
	if _, err := decodeResult([]byte(`{"symbol":"600519"}`), "600519"); err == nil {
		t.Fatal("缺少 ok 字段时应当报错")
	}
}

func TestDecodeResultInvalidJSON(t *testing.T) {
	if _, err := decodeResult([]byte(`not json`), "600519"); err == nil {
		t.Fatal("非法 JSON 应当报错")
	}
}

func TestDecodeResultGeneratesTimestampWhenMissing(t *testing.T) {
	result, err := decodeResult([]byte(`{"ok":true,"symbol":"600519"}`), "600519")
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	if result.GeneratedAt.IsZero() {
		t.Fatal("缺少 generated_at 时应回填当前时间")
	}
	if result.Symbol != "600519" {
		t.Fatalf("symbol 应回退到请求值: %s", result.Symbol)
	}
}

func TestDecodeResultToleratesChartAndBacktest(t *testing.T) {
	payload := `{
		"ok": true, "symbol": "600519",
		"chart": {"ok": true, "path": "K:/out/chan-600519.html", "size": 337540},
		"backtest": {"ok": true, "signal": "cxt_bi_status_V230101", "signal_value": "向下_底分_任意_0",
			"stats": {"年化收益": -0.2145, "夏普比率": -0.95, "交易次数": 83},
			"all_keys": ["绝对收益", "年化收益"]}
	}`
	result, err := decodeResult([]byte(payload), "600519")
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	if result.Chart == nil || result.Chart.Path != "K:/out/chan-600519.html" || result.Chart.Size != 337540 {
		t.Fatalf("chart 解析错误: %+v", result.Chart)
	}
	if result.Backtest == nil || !result.Backtest.OK || result.Backtest.Signal == "" {
		t.Fatalf("backtest 解析错误: %+v", result.Backtest)
	}
	if result.Backtest.Stats["交易次数"] != float64(83) {
		t.Fatalf("backtest.stats 解析错误: %+v", result.Backtest.Stats)
	}
}

func TestDecodeResultFailedBacktest(t *testing.T) {
	// 回测失败时脚本仍返回 ok=true 的整体结果，backtest 内部 ok=false。
	payload := `{"ok": true, "symbol": "600519",
		"backtest": {"ok": false, "error": "该信号在回测区间内未产生多头信号，无法评估"}}`
	result, err := decodeResult([]byte(payload), "600519")
	if err != nil {
		t.Fatalf("整体成功时不应报错: %v", err)
	}
	if result.Backtest == nil || result.Backtest.OK {
		t.Fatalf("backtest 应标记失败: %+v", result.Backtest)
	}
	if !strings.Contains(result.Backtest.Error, "未产生多头信号") {
		t.Fatalf("backtest 错误信息丢失: %+v", result.Backtest)
	}
}

func TestSignalBias(t *testing.T) {
	cases := []struct {
		signal Signal
		want   string
	}{
		{Signal{OK: true, Value: "向上_顶分_任意_0"}, "bullish"},
		{Signal{OK: true, Value: "向下_底分_任意_0"}, "bearish"},
		{Signal{OK: true, Value: "其他_形态_任意_0"}, "neutral"},
		{Signal{OK: false, Error: "boom"}, "unknown"},
	}
	for _, item := range cases {
		if got := item.signal.Bias(); got != item.want {
			t.Fatalf("Bias(%q, ok=%v) = %q, 期望 %q", item.signal.Value, item.signal.OK, got, item.want)
		}
	}
}

func TestParseSignalCatalog(t *testing.T) {
	// 实测 czsc 的 list_all_signals() 返回列表。
	payload := `[
		{"name": "adtm_up_dw_line_V230603", "param_template": "{freq}_D{di}N{n}M{m}TH{th}_ADTMV230603",
			"category": "kline", "namespace": "adtm"},
		{"name": "cxt_bi_status_V230101", "param_template": "{freq}_D{di}_笔表里V230101",
			"category": "kline", "namespace": "cxt"}
	]`
	metas, err := parseSignalCatalog([]byte(payload))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(metas) != 2 {
		t.Fatalf("条目数错误: %d", len(metas))
	}
	if metas[0].Namespace != "adtm" || metas[0].Category != "kline" {
		t.Fatalf("元数据错误: %+v", metas[0])
	}
	// freq 应被剔除，其余占位符按出现顺序保留。
	wantKeys := []string{"di", "n", "m", "th"}
	if len(metas[0].ParamKeys) != len(wantKeys) {
		t.Fatalf("ParamKeys = %v, 期望 %v", metas[0].ParamKeys, wantKeys)
	}
	for index, key := range wantKeys {
		if metas[0].ParamKeys[index] != key {
			t.Fatalf("ParamKeys = %v, 期望 %v", metas[0].ParamKeys, wantKeys)
		}
	}
	if len(metas[1].ParamKeys) != 1 || metas[1].ParamKeys[0] != "di" {
		t.Fatalf("ParamKeys = %v, 期望 [di]", metas[1].ParamKeys)
	}
}

func TestParseSignalCatalogEnvelope(t *testing.T) {
	metas, err := parseSignalCatalog([]byte(`{"ok": true, "signals": [{"name": "tas_sma_V230101"}]}`))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(metas) != 1 || metas[0].Name != "tas_sma_V230101" {
		t.Fatalf("条目错误: %+v", metas)
	}
	// namespace 应可从名称前缀推断。
	if metas[0].Namespace != "tas" {
		t.Fatalf("namespace 推断失败: %+v", metas[0])
	}
}

func TestParseSignalCatalogError(t *testing.T) {
	if _, err := parseSignalCatalog([]byte(`{"ok": false, "error": "boom"}`)); err == nil {
		t.Fatal("错误信封应当报错")
	}
	if _, err := parseSignalCatalog([]byte(`[]`)); err == nil {
		t.Fatal("空目录应当报错")
	}
}

func TestParseParamKeys(t *testing.T) {
	cases := []struct {
		template string
		want     []string
	}{
		{"{freq}_D{di}N{n}_SMAV230101", []string{"di", "n"}},
		{"{freq}_SMAV230101", nil},
		{"", nil},
		{"{freq}_D{di}D{di}_X", []string{"di"}},
	}
	for _, item := range cases {
		got := parseParamKeys(item.template)
		if len(got) != len(item.want) {
			t.Fatalf("parseParamKeys(%q) = %v, 期望 %v", item.template, got, item.want)
		}
		for index := range item.want {
			if got[index] != item.want[index] {
				t.Fatalf("parseParamKeys(%q) = %v, 期望 %v", item.template, got, item.want)
			}
		}
	}
}

func TestUnavailableReason(t *testing.T) {
	service := NewService(Config{
		PythonPath: filepath.Join(t.TempDir(), "missing-python.exe"),
		ScriptPath: filepath.Join(t.TempDir(), "missing-analyze.py"),
	})
	if service.Available() {
		t.Fatal("缺少解释器时应不可用")
	}
	if reason := service.UnavailableReason(); !strings.Contains(reason, "Python") {
		t.Fatalf("应优先报告解释器缺失: %s", reason)
	}
	if _, err := service.Query(context.Background(), Request{Symbol: "600519"}); err == nil {
		t.Fatal("不可用时 Query 应当报错")
	}
}

// TestUnavailableReasonPointsAtConventionalPath 提示信息里必须出现**约定的稳定路径**，
// 而不是探测出来的 cwd/exe 相对候选——后者在编译机与运行机上不同，会把用户带偏。
func TestUnavailableReasonPointsAtConventionalPath(t *testing.T) {
	t.Setenv("A_STOCK_CZSC_ENV_ROOT", "")
	t.Setenv("A_STOCK_CZSC_ROOT", "")
	t.Setenv("A_STOCK_CZSC_PYTHON", "")

	service := NewService(Config{
		PythonPath: filepath.Join(t.TempDir(), "missing-python.exe"),
		ScriptPath: filepath.Join(t.TempDir(), "missing-analyze.py"),
	})
	reason := service.UnavailableReason()
	if !strings.Contains(reason, "easy-stock-env") {
		t.Fatalf("提示应包含约定的 easy-stock-env 根目录: %s", reason)
	}
	if strings.Contains(reason, "chananalysis") {
		t.Fatalf("提示不应暴露 cwd 相对候选路径: %s", reason)
	}
	if !strings.Contains(reason, "A_STOCK_CZSC_PYTHON") {
		t.Fatalf("提示应给出环境变量兜底方案: %s", reason)
	}
}

// TestDefaultEnvRootAcceptsServiceDir A_STOCK_CZSC_ROOT 指向 czsc-service 时，
// 提示里应回退到它的上一级（env 根），而不是把 czsc-service 当根再拼一层。
func TestDefaultEnvRootAcceptsServiceDir(t *testing.T) {
	envRoot := t.TempDir()
	serviceDir := filepath.Join(envRoot, "czsc-service")
	t.Setenv("A_STOCK_CZSC_ROOT", serviceDir)
	t.Setenv("A_STOCK_CZSC_ENV_ROOT", "")
	if got := defaultEnvRoot(); got != filepath.Clean(envRoot) {
		t.Fatalf("defaultEnvRoot = %q, 期望 %q", got, envRoot)
	}
}

func TestAvailableWithRealFiles(t *testing.T) {
	dir := t.TempDir()
	python := filepath.Join(dir, "python.exe")
	script := filepath.Join(dir, "analyze.py")
	for _, path := range []string{python, script} {
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o600); err != nil {
			t.Fatalf("准备文件失败: %v", err)
		}
	}
	service := NewService(Config{PythonPath: python, ScriptPath: script, DisableCache: true})
	if !service.Available() {
		t.Fatalf("应当可用: %s", service.UnavailableReason())
	}
	if service.ScriptPath() != script {
		t.Fatalf("ScriptPath 错误: %s", service.ScriptPath())
	}
	// 脚本不是真正的 Python，执行必然失败，但错误应可读且不含 panic。
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := service.Query(ctx, Request{Symbol: "600519"}); err == nil {
		t.Fatal("伪脚本执行应当失败")
	}
}

// TestDiscoverPythonFindsSiblingVenv 锁定本机的目录布局约定：
// venv 与 czsc-service 是**同级**目录，都在 easy-stock-env 下。
//
//	K:\easy-stock-env\a-stock-data-venv\Scripts\python.exe
//	K:\easy-stock-env\czsc-service\analyze.py
//
// 早期实现只把 venv 相对路径拼到 serviceRoots()（也就是 czsc-service 本身）之下，
// 结果 analyze.py 能找到、解释器找不到，界面报「未找到缠论分析所需的 Python 解释器」。
func TestDiscoverPythonFindsSiblingVenv(t *testing.T) {
	envRoot := t.TempDir()
	serviceDir := filepath.Join(envRoot, "czsc-service")
	scriptsDir := filepath.Join(envRoot, "a-stock-data-venv", "Scripts")
	if runtime.GOOS == "windows" {
		scriptsDir = filepath.Join(envRoot, "a-stock-data-venv", "Scripts")
	} else {
		scriptsDir = filepath.Join(envRoot, "a-stock-data-venv", "bin")
	}
	if err := os.MkdirAll(serviceDir, 0o700); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	if err := os.MkdirAll(scriptsDir, 0o700); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	pythonName := "python"
	if runtime.GOOS == "windows" {
		pythonName = "python.exe"
	}
	python := filepath.Join(scriptsDir, pythonName)
	script := filepath.Join(serviceDir, "analyze.py")
	for _, path := range []string{python, script} {
		if err := os.WriteFile(path, []byte(""), 0o600); err != nil {
			t.Fatalf("准备文件失败: %v", err)
		}
	}

	// 通过 A_STOCK_CZSC_ENV_ROOT 指定 env 根，模拟真实部署。
	t.Setenv("A_STOCK_CZSC_ENV_ROOT", envRoot)
	t.Setenv("A_STOCK_CZSC_PYTHON", "")
	t.Setenv("A_STOCK_CZSC_SCRIPT", "")
	t.Setenv("A_STOCK_CZSC_ROOT", "")

	if got := discoverPython(); got != filepath.Clean(python) {
		t.Fatalf("discoverPython = %q, 期望 %q（venv 在 czsc-service 的上一级）", got, python)
	}
	if got := discoverScript(); got != filepath.Clean(script) {
		t.Fatalf("discoverScript = %q, 期望 %q", got, script)
	}
	// 两者都能定位时服务才算可用
	service := NewService(Config{DisableCache: true})
	if !service.Available() {
		t.Fatalf("应当可用: %s", service.UnavailableReason())
	}
}

// TestEnvironmentRootToleratesServiceDir 验证把 env 根写成 czsc-service 目录也能兜住，
// 避免用户手工配置时少写一层就失效。
func TestEnvironmentRootToleratesServiceDir(t *testing.T) {
	envRoot := t.TempDir()
	serviceDir := filepath.Join(envRoot, "czsc-service")
	scriptsDir := filepath.Join(envRoot, "a-stock-data-venv", "Scripts")
	if runtime.GOOS != "windows" {
		scriptsDir = filepath.Join(envRoot, "a-stock-data-venv", "bin")
	}
	for _, dir := range []string{serviceDir, scriptsDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("建目录失败: %v", err)
		}
	}
	pythonName := "python"
	if runtime.GOOS == "windows" {
		pythonName = "python.exe"
	}
	python := filepath.Join(scriptsDir, pythonName)
	if err := os.WriteFile(python, []byte(""), 0o600); err != nil {
		t.Fatalf("准备失败: %v", err)
	}

	t.Setenv("A_STOCK_CZSC_ROOT", serviceDir)
	t.Setenv("A_STOCK_CZSC_ENV_ROOT", "")
	t.Setenv("A_STOCK_CZSC_PYTHON", "")

	if got := discoverPython(); got != filepath.Clean(python) {
		t.Fatalf("discoverPython = %q, 期望 %q", got, python)
	}
}

func TestQueryValidatesSymbol(t *testing.T) {
	dir := t.TempDir()
	python := filepath.Join(dir, "python.exe")
	script := filepath.Join(dir, "analyze.py")
	for _, path := range []string{python, script} {
		if err := os.WriteFile(path, []byte(""), 0o600); err != nil {
			t.Fatalf("准备文件失败: %v", err)
		}
	}
	service := NewService(Config{PythonPath: python, ScriptPath: script, DisableCache: true})
	if _, err := service.Query(context.Background(), Request{Symbol: "   "}); err == nil {
		t.Fatal("空代码应当报错")
	}
}

func TestCacheKeyDistinguishesRequests(t *testing.T) {
	base := Request{Symbol: "600519", Period: "day", Limit: 800}
	same := Request{Symbol: "600519", Period: "day", Limit: 800}
	if cacheKeyFor(base) != cacheKeyFor(same) {
		t.Fatal("相同请求应产生相同缓存键")
	}
	variants := []Request{
		{Symbol: "000001", Period: "day", Limit: 800},
		{Symbol: "600519", Period: "week", Limit: 800},
		{Symbol: "600519", Period: "day", Limit: 400},
		{Symbol: "600519", Period: "day", Limit: 800, BacktestSignal: "cxt_bi_status_V230101"},
		{Symbol: "600519", Period: "day", Limit: 800, WithChart: true},
		{Symbol: "600519", Period: "day", Limit: 800, Signals: []SignalConfig{{Name: "zdy_dif_V230527"}}},
	}
	for _, variant := range variants {
		if cacheKeyFor(variant) == cacheKeyFor(base) {
			t.Fatalf("不同请求产生了相同缓存键: %+v", variant)
		}
	}
}

func TestQueryUsesCache(t *testing.T) {
	// 用一个会记录调用次数的假脚本验证缓存确实生效。脚本内容为一段 Python，
	// 每次运行向计数文件追加一行。
	python := findRealPython(t)
	if python == "" {
		t.Skip("未找到可用的 Python 解释器")
	}
	dir := t.TempDir()
	counter := filepath.Join(dir, "calls.txt")
	script := filepath.Join(dir, "analyze.py")
	body := "import json,sys\n" +
		"open(r'" + counter + "','a').write('1')\n" +
		"print(json.dumps({'ok': True, 'symbol': '600519', 'freq': '日线'}))\n"
	if err := os.WriteFile(script, []byte(body), 0o600); err != nil {
		t.Fatalf("写脚本失败: %v", err)
	}

	service := NewService(Config{PythonPath: python, ScriptPath: script})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	for attempt := 0; attempt < 3; attempt++ {
		if _, err := service.Query(ctx, Request{Symbol: "600519"}); err != nil {
			t.Fatalf("第 %d 次查询失败: %v", attempt+1, err)
		}
	}
	data, err := os.ReadFile(counter)
	if err != nil {
		t.Fatalf("读取计数失败: %v", err)
	}
	if len(data) != 1 {
		t.Fatalf("缓存未生效，脚本被调用 %d 次", len(data))
	}
}

func TestProcessEnvironmentSetsPolarsFlag(t *testing.T) {
	values := processEnvironment()
	found := false
	for _, value := range values {
		if value == "POLARS_SKIP_CPU_CHECK=1" {
			found = true
		}
	}
	if !found {
		t.Fatal("必须注入 POLARS_SKIP_CPU_CHECK=1，否则部分主机上 polars 启动即崩")
	}
}

// TestSetTokenPropagatesToScript 验证令牌会被透传给脚本的 --token 参数。
// 后端启用鉴权时，脚本取数必须带上同一令牌，否则被 401 拒绝。
func TestSetTokenPropagatesToScript(t *testing.T) {
	dir := t.TempDir()
	python := filepath.Join(dir, "python.exe")
	script := filepath.Join(dir, "analyze.py")
	for _, path := range []string{python, script} {
		if err := os.WriteFile(path, []byte(""), 0o600); err != nil {
			t.Fatalf("准备文件失败: %v", err)
		}
	}
	service := NewService(Config{PythonPath: python, ScriptPath: script, DisableCache: true})
	service.SetToken("  secret-token  ")
	if service.token != "secret-token" {
		t.Fatalf("token = %q, 期望去除空白后的 secret-token", service.token)
	}
	// 空令牌不得产生 --token 参数，否则脚本会拿到空串
	service.SetToken("   ")
	if service.token != "" {
		t.Fatalf("token = %q, 期望空串", service.token)
	}
}

// TestEndToEnd 走真实后端与真实 czsc，仅在 CZSC_E2E=1 时运行。
// 前置条件：easy-stock 后端在 127.0.0.1:20081 上运行。
func TestEndToEnd(t *testing.T) {
	if os.Getenv("CZSC_E2E") != "1" {
		t.Skip("设置 CZSC_E2E=1 后运行端到端测试")
	}
	service := NewService(Config{})
	if !service.Available() {
		t.Fatalf("分析服务不可用: %s", service.UnavailableReason())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	result, err := service.Query(ctx, Request{Symbol: "600519", Limit: 400})
	if err != nil {
		t.Fatalf("分析失败: %v", err)
	}
	if result.Structure.Counts.Bi == 0 {
		t.Fatal("未识别出任何笔")
	}
	if result.Structure.Counts.FX == 0 {
		t.Fatal("未识别出任何分型")
	}
	if len(result.Signals) == 0 {
		t.Fatal("未返回任何信号")
	}
	if result.Summary.Score <= 0 {
		t.Fatal("评分未生成")
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("结果无法序列化: %v", err)
	}
	if len(encoded) < 500 {
		t.Fatalf("结果异常小: %d 字节", len(encoded))
	}
	t.Logf("茅台: %d 分型 / %d 笔 / %d 中枢, 评分 %.1f (%s), 信号 %d 条",
		result.Structure.Counts.FX, result.Structure.Counts.Bi, result.Structure.Counts.Zs,
		result.Summary.Score, result.Summary.Stance, len(result.Signals))
}

// TestCatalogEndToEnd 验证 246 条信号目录可正常读取。
func TestCatalogEndToEnd(t *testing.T) {
	if os.Getenv("CZSC_E2E") != "1" {
		t.Skip("设置 CZSC_E2E=1 后运行端到端测试")
	}
	service := NewService(Config{})
	if !service.Available() {
		t.Fatalf("分析服务不可用: %s", service.UnavailableReason())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	metas, err := service.Catalog(ctx)
	if err != nil {
		t.Fatalf("读取信号目录失败: %v", err)
	}
	if len(metas) < 100 {
		t.Fatalf("信号目录条目过少: %d", len(metas))
	}
	namespaces := map[string]int{}
	for _, meta := range metas {
		namespaces[meta.Namespace]++
	}
	t.Logf("信号目录 %d 条，命名空间 %d 个", len(metas), len(namespaces))
}

// findRealPython 返回一个可用的 Python 解释器路径供缓存测试使用。
func findRealPython(t *testing.T) string {
	t.Helper()
	candidates := []string{}
	if configured := strings.TrimSpace(os.Getenv("A_STOCK_CZSC_PYTHON")); configured != "" {
		candidates = append(candidates, configured)
	}
	if runtime.GOOS == "windows" {
		candidates = append(candidates,
			`K:\easy-stock-env\a-stock-data-venv\Scripts\python.exe`,
			`K:\easy-stock-env\go-sdk\bin\python.exe`,
		)
	}
	if resolved, err := exec.LookPath("python"); err == nil {
		candidates = append(candidates, resolved)
	}
	for _, candidate := range candidates {
		if fileExists(candidate) {
			return candidate
		}
	}
	return ""
}

func TestValidateChartOutDir(t *testing.T) {
	allowed := filepath.Join(os.TempDir(), "chan-out")
	if err := validateChartOutDir(allowed); err != nil {
		t.Fatalf("temp subdir should be allowed, got %v", err)
	}
	rejected := []string{
		`C:\Windows\System32`,
		`C:\Users\admin\AppData\Roaming\Microsoft\Windows\Start Menu\Programs\Startup`,
		filepath.Join(os.TempDir(), "..", "..", "Windows"),
		"relative/out",
	}
	for _, dir := range rejected {
		if err := validateChartOutDir(dir); err == nil {
			t.Fatalf("expected %q to be rejected", dir)
		}
	}
}
