package stockanalysis

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"
)

// TestDiagCaptureCorePromptOutput 把「核心合成」阶段的真实提示词直接发给 Ollama，
// 落盘模型原始输出，用于诊断 `does not contain expected fields` 的 JSON 形状问题。
// 仅在 ES_DIAG=1 时运行。
func TestDiagCaptureCorePromptOutput(t *testing.T) {
	if os.Getenv("ES_DIAG") != "1" {
		t.Skip("set ES_DIAG=1 to run diagnostic capture")
	}
	_, snapshot := researchFixture(t)
	request := ResearchRequest{Symbol: snapshot.Symbol, Purpose: "observe",
		Horizon: "swing", AnalysisLevel: ResearchLevelDeep}
	outline := ResearchOutline{
		Questions: []ResearchQuestion{{Question: "2026-09-30 涨停原因",
			Why: "事件驱动", Tool: "news", Query: "涨停", Status: "open"}},
		MissingFacts: []string{"公司公告"},
	}
	pack := buildResearchCoreEvidencePack(snapshot, request, outline)
	payload := buildResearchPromptPayload(snapshot, request, pack, nil)

	body, _ := json.Marshal(map[string]any{
		"model": "qwen3:14b-64k",
		"messages": []map[string]string{
			{"role": "system", "content": "你是 A 股研究员。只输出一个合法 JSON 对象。"},
			{"role": "user", "content": string(payload)},
		},
		"stream":  false,
		"options": map[string]any{"num_ctx": 65536},
	})
	req, _ := http.NewRequest("POST", "http://127.0.0.1:11434/v1/chat/completions",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 15 * time.Minute}
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("call ollama: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	_ = json.Unmarshal(raw, &parsed)
	content := ""
	if len(parsed.Choices) > 0 {
		content = parsed.Choices[0].Message.Content
	}
	out := fmt.Sprintf("C:/quant/es-eval/s5/diag_core_%d.txt", time.Now().Unix())
	_ = os.WriteFile(out, []byte(content), 0o644)

	t.Logf("elapsed=%s raw=%dB content=%dchars -> %s",
		time.Since(start).Round(time.Second), len(raw), len(content), out)

	var target ResearchCoreSynthesis
	err = decodeJSONObject(content, &target)
	t.Logf("decodeJSONObject err = %v", err)
	t.Logf("headline=%q support=%d", target.Headline, len(target.Support))

	if os.Getenv("ES_DIAG_PROMPT") == "1" {
		pout := fmt.Sprintf("C:/quant/es-eval/s5/diag_prompt_%d.txt", time.Now().Unix())
		_ = os.WriteFile(pout, payload, 0o644)
		t.Logf("prompt -> %s", pout)
	}
}
