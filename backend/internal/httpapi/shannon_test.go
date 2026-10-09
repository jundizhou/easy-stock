package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestShannonScanEndpoint 验证 /api/v1/strategy/shannon/scan 经 HTTP 端到端可用
// （含 qfq 还原 + 日线重采样周/月线）。依赖本地 stockdb 引擎（127.0.0.1:7899）。
func TestShannonScanEndpoint(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过需要行情引擎的集成测试")
	}
	server := NewServer(nil)
	body := `{"codes":["600519","600919","000001"],"equity":100000,"workers":4}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/strategy/shannon/scan", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Data struct {
			Count  int `json:"count"`
			Plan   any `json:"plan"`
			Errors any `json:"errors"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v; body=%s", err, rec.Body.String())
	}
	if payload.Data.Count != 3 {
		t.Fatalf("count = %d, want 3 (errors=%v)", payload.Data.Count, payload.Data.Errors)
	}
	if payload.Data.Plan == nil {
		t.Fatalf("plan 不应为空")
	}
}

// TestShannonAnalyzeEndpoint 验证单标的分析端点。
func TestShannonAnalyzeEndpoint(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过需要行情引擎的集成测试")
	}
	server := NewServer(nil)
	body := `{"code":"600519","equity":100000}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/strategy/shannon/analyze", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Data struct {
			Code string `json:"code"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v; body=%s", err, rec.Body.String())
	}
	if payload.Data.Code != "600519" {
		t.Fatalf("code = %q, want 600519", payload.Data.Code)
	}
}

// TestShannonAnalyzeMissingCode 验证缺 code 时返回 400。
func TestShannonAnalyzeMissingCode(t *testing.T) {
	server := NewServer(nil)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/strategy/shannon/analyze", bytes.NewReader([]byte(`{"equity":100000}`)))
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// TestShannonBoardsEndpoint 验证板块列表端点。
func TestShannonBoardsEndpoint(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过需要行情引擎的集成测试")
	}
	server := NewServer(nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/strategy/shannon/boards", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Data []string `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v; body=%s", err, rec.Body.String())
	}
	if len(payload.Data) == 0 {
		t.Fatalf("板块列表不应为空")
	}
}
