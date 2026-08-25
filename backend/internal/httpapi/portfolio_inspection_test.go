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
		promptResult: hermes.PromptResult{Content: `{"health_score":1,"risk_level":"极高","style_match":"明显偏离","executive_summary":"组合结构总体可控，继续按确认与失效条件管理持仓。","primary_risks":[],"concentration_findings":[],"holdings":[{"symbol":"600519.SH","portfolio_role":"核心","risk_contribution":100,"conclusion":"趋势结构稳定","action_priority":"保持","action":"满足趋势条件时持有","confirmation":"趋势延续","invalidation":"跌破止损"}],"adjustment_order":[],"scenarios":[],"next_checklist":[],"data_limitations":[],"confidence":0.8}`},
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
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		statusRequest := httptest.NewRequest(http.MethodGet, "/api/v1/portfolio-inspections/"+created.Data.ID, nil)
		statusResponse := httptest.NewRecorder()
		server.ServeHTTP(statusResponse, statusRequest)
		if statusResponse.Code != http.StatusOK {
			t.Fatalf("status request=%d body=%s", statusResponse.Code, statusResponse.Body.String())
		}
		var payload struct {
			Data struct {
				Status          string `json:"status"`
				ReportAvailable bool   `json:"report_available"`
				Report          struct {
					AlgorithmVersion string `json:"algorithm_version"`
					Metrics          struct {
						Total           int  `json:"total_position_percent"`
						Cash            int  `json:"cash_percent"`
						Health          int  `json:"health_score"`
						HealthAvailable bool `json:"health_score_available"`
					} `json:"metrics"`
					Conclusion struct {
						Health     int    `json:"health_score"`
						RiskLevel  string `json:"risk_level"`
						StyleMatch string `json:"style_match"`
					} `json:"conclusion"`
				} `json:"report"`
			} `json:"data"`
		}
		if err := json.NewDecoder(statusResponse.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.Data.Status != "running" {
			if !payload.Data.ReportAvailable || payload.Data.Report.Metrics.Total != 60 || payload.Data.Report.Metrics.Cash != 40 || payload.Data.Report.AlgorithmVersion != "portfolio-health-v2" {
				t.Fatalf("unexpected completed job: %+v", payload.Data)
			}
			if !payload.Data.Report.Metrics.HealthAvailable || payload.Data.Report.Conclusion.Health != payload.Data.Report.Metrics.Health || payload.Data.Report.Conclusion.Health == 1 || payload.Data.Report.Conclusion.RiskLevel == "极高" || payload.Data.Report.Conclusion.StyleMatch == "明显偏离" {
				t.Fatalf("AI changed deterministic health score: %+v", payload.Data.Report)
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
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
