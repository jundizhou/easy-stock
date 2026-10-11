package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	s := NewServer(nil)
	return httptest.NewServer(s)
}

func TestExecutionHTTPEndpoints(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()
	client := ts.Client()

	// 1) status
	resp, err := client.Get(ts.URL + "/api/v1/execution/status")
	if err != nil {
		t.Fatalf("status req: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status status=%d body=%s", resp.StatusCode, body)
	}
	var statusResp map[string]any
	_ = json.Unmarshal(body, &statusResp)
	if statusResp["ok"] != true {
		t.Fatalf("status ok != true: %s", body)
	}
	t.Logf("status: %s", body)

	// 2) target
	resp, err = client.Get(ts.URL + "/api/v1/execution/target?strategy=core2")
	if err != nil {
		t.Fatalf("target req: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("target status=%d body=%s", resp.StatusCode, body)
	}
	var targetResp struct {
		Ok   bool `json:"ok"`
		Plan struct {
			Strategy  string `json:"strategy"`
			SignalID  string `json:"signal_id"`
			NHoldings int    `json:"n_holdings"`
			Targets   []struct {
				Code         string  `json:"code"`
				TargetWeight float64 `json:"target_weight"`
				AssumedPrice float64 `json:"assumed_price"`
			} `json:"targets"`
		} `json:"plan"`
	}
	if err := json.Unmarshal(body, &targetResp); err != nil {
		t.Fatalf("target decode: %v body=%s", err, body)
	}
	if !targetResp.Ok || targetResp.Plan.Strategy != "core2" || targetResp.Plan.NHoldings == 0 {
		t.Fatalf("target unexpected: %s", body)
	}
	t.Logf("target n_holdings=%d signal=%s", targetResp.Plan.NHoldings, targetResp.Plan.SignalID)

	// 3) sim intent (echo targets)
	intent := map[string]any{
		"schema_version":  "1.0.0",
		"strategy":        "core2",
		"pit_audit_passed": true,
		"signal_id":       targetResp.Plan.SignalID,
		"execute_on":      "sim",
		"targets":         targetResp.Plan.Targets,
		"constraints":     map[string]any{"max_single_weight": 0.10, "max_daily_turnover": 0.30, "round_lot": 100, "block_st": true, "block_suspended": true, "block_limit": true},
	}
	payload, _ := json.Marshal(intent)
	resp, err = client.Post(ts.URL+"/api/v1/execution/intent", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("intent req: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("intent status=%d body=%s", resp.StatusCode, body)
	}
	var intentResp struct {
		Ok   bool `json:"ok"`
		Ack  struct {
			Accepted bool `json:"accepted"`
			Fills    []any `json:"fills"`
			Message  string `json:"message"`
		} `json:"ack"`
	}
	if err := json.Unmarshal(body, &intentResp); err != nil {
		t.Fatalf("intent decode: %v body=%s", err, body)
	}
	if !intentResp.Ack.Accepted {
		t.Fatalf("sim intent not accepted: %s", body)
	}
	t.Logf("sim ack fills=%d msg=%s", len(intentResp.Ack.Fills), intentResp.Ack.Message)

	// 4) live intent must be rejected
	intent["execute_on"] = "live"
	payload, _ = json.Marshal(intent)
	resp, err = client.Post(ts.URL+"/api/v1/execution/intent", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("live intent req: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode == 200 {
		t.Fatalf("live intent should be rejected, got 200: %s", body)
	}
	t.Logf("live intent rejected as expected (status=%d)", resp.StatusCode)

	// 5) non-whitelist strategy rejected
	resp, err = client.Get(ts.URL + "/api/v1/execution/target?strategy=resonance")
	if err != nil {
		t.Fatalf("resonance target req: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode == 200 {
		t.Fatalf("resonance should be rejected, got 200: %s", body)
	}
	t.Logf("resonance target rejected as expected (status=%d)", resp.StatusCode)

	// 6) fill record
	fill := map[string]any{"code": "600076", "side": "BUY", "price": 29.5, "shares": 300, "notional": 8850, "source": "sim"}
	fpayload, _ := json.Marshal(fill)
	resp, err = client.Post(ts.URL+"/api/v1/execution/fill", "application/json", bytes.NewReader(fpayload))
	if err != nil {
		t.Fatalf("fill req: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("fill status=%d body=%s", resp.StatusCode, body)
	}
	t.Logf("fill recorded: %s", body)
}
