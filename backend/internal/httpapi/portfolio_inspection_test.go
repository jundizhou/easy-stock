package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"easy-stock/backend/internal/hermes"
)

func TestPortfolioInspectionRunsInBackgroundAndReturnsReport(t *testing.T) {
	gateway := &fakeHermesGateway{
		status:       hermes.Status{Available: true, Configured: true},
		promptResult: hermes.PromptResult{Content: `{"health_score":76,"risk_level":"中","style_match":"匹配","executive_summary":"组合结构总体可控，继续按确认与失效条件管理持仓。","primary_risks":[],"concentration_findings":[],"holdings":[{"symbol":"600519.SH","portfolio_role":"核心","risk_contribution":100,"conclusion":"趋势结构稳定","action_priority":"保持","action":"满足趋势条件时持有","confirmation":"趋势延续","invalidation":"跌破止损"}],"adjustment_order":[],"scenarios":[],"next_checklist":[],"data_limitations":[],"confidence":0.8}`},
	}
	server := NewServer(Config{
		Realtime: stockAnalysisRealtime{}, KLinePrimary: stockAnalysisKLines{}, KLineFallback: stockAnalysisKLines{},
		LimitUp: stockAnalysisLimitUps{}, StockConcept: stockAnalysisCatalog{}, SectorMap: fakeSectorMapProvider{},
		ThemeOverview: stockAnalysisThemes{}, News: stockAnalysisNews{}, ReviewDBPath: ":memory:", PortfolioDBPath: ":memory:",
		SettingsPath: "", HermesGateway: gateway,
	})
	t.Cleanup(func() { _ = server.Close() })

	request := httptest.NewRequest(http.MethodPost, "/api/v1/portfolio-inspections", strings.NewReader(`{"trader_profile":"balanced","holdings":[{"symbol":"600519","weight_percent":60}]}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("create status=%d body=%s", response.Code, response.Body.String())
	}
	var created struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		statusRequest := httptest.NewRequest(http.MethodGet, "/api/v1/portfolio-inspections/"+created.Data.ID, nil)
		statusResponse := httptest.NewRecorder()
		server.ServeHTTP(statusResponse, statusRequest)
		var payload struct {
			Data struct {
				Status          string `json:"status"`
				ReportAvailable bool   `json:"report_available"`
				Report          struct {
					Metrics struct {
						Total int `json:"total_position_percent"`
						Cash  int `json:"cash_percent"`
					} `json:"metrics"`
				} `json:"report"`
			} `json:"data"`
		}
		if err := json.NewDecoder(statusResponse.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.Data.Status != "running" {
			if !payload.Data.ReportAvailable || payload.Data.Report.Metrics.Total != 60 || payload.Data.Report.Metrics.Cash != 40 {
				t.Fatalf("unexpected completed job: %+v", payload.Data)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("portfolio inspection did not complete")
}

func TestPortfolioInspectionRejectsOverAllocation(t *testing.T) {
	server := NewServer(Config{ReviewDBPath: ":memory:", PortfolioDBPath: ":memory:", SettingsPath: "", HermesGateway: &fakeHermesGateway{status: hermes.Status{Available: true, Configured: true}}})
	t.Cleanup(func() { _ = server.Close() })
	request := httptest.NewRequest(http.MethodPost, "/api/v1/portfolio-inspections", strings.NewReader(`{"trader_profile":"balanced","holdings":[{"symbol":"600519","weight_percent":60},{"symbol":"000858","weight_percent":50}]}`))
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
