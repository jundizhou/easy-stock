package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"easy-stock/backend/internal/chananalysis"
)

// newChanTestServer 构造一个注入了可用缠论服务的 Server。解释器与脚本用空文件
// 占位，只为让 Available() 为真；真正执行会在子进程阶段失败，这不影响对参数
// 解析与错误码的验证。
func newChanTestServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	python := filepath.Join(dir, "python.exe")
	script := filepath.Join(dir, "analyze.py")
	for _, path := range []string{python, script} {
		if err := os.WriteFile(path, []byte(""), 0o600); err != nil {
			t.Fatalf("准备文件失败: %v", err)
		}
	}
	return &Server{
		chanAnalysisService: chananalysis.NewService(chananalysis.Config{
			PythonPath:   python,
			ScriptPath:   script,
			DisableCache: true,
		}),
	}
}

func TestChanStatusReportsAvailability(t *testing.T) {
	server := newChanTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/stocks/chan-status", nil)
	rec := httptest.NewRecorder()
	server.chanStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload["available"] != true {
		t.Fatalf("available = %v, 期望 true", payload["available"])
	}
}

func TestChanStatusWithoutService(t *testing.T) {
	server := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/stocks/chan-status", nil)
	rec := httptest.NewRecorder()
	server.chanStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var payload map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload["available"] != false {
		t.Fatalf("未注入服务时 available 应为 false: %v", payload)
	}
}

func TestChanAnalysisRejectsInvalidSymbol(t *testing.T) {
	server := newChanTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/stocks/chan-analysis?symbol=", nil)
	rec := httptest.NewRecorder()
	server.chanAnalysis(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestChanAnalysisRejectsInvalidLimit(t *testing.T) {
	server := newChanTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/stocks/chan-analysis?symbol=600519&limit=abc", nil)
	rec := httptest.NewRecorder()
	server.chanAnalysis(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "limit") {
		t.Fatalf("错误信息应指出 limit 问题: %s", rec.Body.String())
	}
}

func TestChanAnalysisRejectsInvalidChartFlag(t *testing.T) {
	server := newChanTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/stocks/chan-analysis?symbol=600519&chart=yes-please", nil)
	rec := httptest.NewRecorder()
	server.chanAnalysis(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestChanAnalysisRejectsMalformedJSON(t *testing.T) {
	server := newChanTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/stocks/chan-analysis", strings.NewReader(`{"symbol":`))
	rec := httptest.NewRecorder()
	server.chanAnalysis(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestChanAnalysisRejectsUnknownFields(t *testing.T) {
	server := newChanTestServer(t)
	body := `{"symbol":"600519","bogus_field":1}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/stocks/chan-analysis", strings.NewReader(body))
	rec := httptest.NewRecorder()
	server.chanAnalysis(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("未知字段应被拒绝，status = %d", rec.Code)
	}
}

func TestChanAnalysisUnavailableServiceReturns503(t *testing.T) {
	// 未注入服务时不应 panic，而应给出明确的 503。
	server := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/stocks/chan-analysis?symbol=600519", nil)
	rec := httptest.NewRecorder()
	server.chanAnalysis(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestChanAnalysisBrokenScriptReturns502(t *testing.T) {
	// 脚本为空文件，解释器执行必然失败，应映射为 502 并带可读错误。
	server := newChanTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/stocks/chan-analysis?symbol=600519", nil)
	rec := httptest.NewRecorder()
	server.chanAnalysis(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestChanSignalCatalogUnavailableReturns503(t *testing.T) {
	server := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/stocks/chan-signals", nil)
	rec := httptest.NewRecorder()
	server.chanSignalCatalog(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestParseChanAnalysisRequestGET(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/stocks/chan-analysis?symbol=600519&period=week&limit=500&backtest=cxt_bi_status_V230101&chart=true&chart_dir=K:/tmp", nil)
	parsed, err := parseChanAnalysisRequest(httptest.NewRecorder(), req)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if parsed.Symbol != "600519" || parsed.Period != "week" || parsed.Limit != 500 {
		t.Fatalf("基础字段错误: %+v", parsed)
	}
	if parsed.Backtest != "cxt_bi_status_V230101" || !parsed.Chart || parsed.ChartDir != "K:/tmp" {
		t.Fatalf("可选字段错误: %+v", parsed)
	}
}

func TestParseChanAnalysisRequestPOST(t *testing.T) {
	body := `{"symbol":"000001","period":"day","limit":300,"chart":false,
		"signals":[{"name":"zdy_dif_V230527","params":{"n":10}}],
		"backtest":"zdy_dif_V230527","backtest_params":{"n":10,"t":10}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/stocks/chan-analysis", strings.NewReader(body))
	parsed, err := parseChanAnalysisRequest(httptest.NewRecorder(), req)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if parsed.Symbol != "000001" || parsed.Limit != 300 {
		t.Fatalf("基础字段错误: %+v", parsed)
	}
	if len(parsed.Signals) != 1 || parsed.Signals[0].Name != "zdy_dif_V230527" {
		t.Fatalf("signals 解析错误: %+v", parsed.Signals)
	}
	if parsed.BacktestParams["n"] != float64(10) {
		t.Fatalf("backtest_params 解析错误: %+v", parsed.BacktestParams)
	}
}

func TestChanAnalysisRoutesRegistered(t *testing.T) {
	// 确保三个缠论路由都挂上了 mux。
	server := NewServer(nil)
	for _, route := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/stocks/chan-analysis"},
		{http.MethodPost, "/api/v1/stocks/chan-analysis"},
		{http.MethodGet, "/api/v1/stocks/chan-signals"},
		{http.MethodGet, "/api/v1/stocks/chan-status"},
	} {
		req := httptest.NewRequest(route.method, route.path, nil)
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)
		// 未注入服务时状态码应为 503；关键是不能被 mux 判为 404（未注册）。
		if rec.Code == http.StatusNotFound {
			t.Fatalf("%s %s 未注册", route.method, route.path)
		}
	}
}
