package httpapi

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"easy-stock/backend/internal/hermes"
)

const validLadderTheme = `{"themes":["固态电池"],"reason":"逐股涨停资料指向固态电池炒作，尚不能确认具体业务进展","caveat":"具体业务进展尚待公告验证","basis":"web","confidence":"medium","sources":[{"title":"当日上涨题材报道","url":"https://example.com/news","date":"2026-09-18","snippet":"报道提到该股受固态电池题材关注"}]}`

type ladderThemePrompter struct {
	response string
	err      error
	calls    int
	options  hermes.PromptOptions
	prompt   string
}

func (p *ladderThemePrompter) PromptWithOptions(_ context.Context, prompt string, options hermes.PromptOptions) (hermes.PromptResult, error) {
	p.calls++
	p.options = options
	p.prompt = prompt
	return hermes.PromptResult{Content: p.response}, p.err
}
func TestLadderThemeCacheSuccessForeverAndForceFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	c := newLadderThemeAI(path)
	input := ladderThemeInput{Symbol: "300285.SZ", Name: "国瓷材料", TradeDate: "2026-09-18", Evidence: []string{"当日逐股涨停题材为固态电池"}}
	now := time.Now()
	if _, start, err := c.reserve(input, now); err != nil || !start {
		t.Fatalf("reserve: %v %v", start, err)
	}
	p := &ladderThemePrompter{response: validLadderTheme}
	c.run(input, p)
	if p.options.DisableTools || !p.options.Sandbox || !p.options.AutoApprove || len(p.options.Toolsets) != 1 || p.options.Toolsets[0] != "web" || !strings.Contains(p.prompt, "web_extract") {
		t.Fatalf("unsafe options: %+v", p.options)
	}
	c = newLadderThemeAI(path)
	entry, start, err := c.reserve(input, now.Add(365*24*time.Hour))
	if err != nil || start || entry.Result == nil || entry.TradeDate != input.TradeDate || len(entry.Result.Sources) != 1 {
		t.Fatalf("successful cache expired: %+v %v", entry, err)
	}
	input.Force = true
	input.TradeDate = "2026-09-21"
	if _, start, err = c.reserve(input, now.Add(time.Minute)); !start || err != nil {
		t.Fatal("manual refresh blocked")
	}
	p.err = errors.New("unavailable")
	c.run(input, p)
	entry = c.entries[input.Symbol]
	if entry.Result == nil || entry.Status != "failed" || entry.TradeDate != "2026-09-18" {
		t.Fatalf("failed refresh discarded success: %+v", entry)
	}
	input.Force = false
	if _, start, _ = c.reserve(input, now.Add(400*24*time.Hour)); start {
		t.Fatal("old success not reused")
	}
}
func TestLadderThemeFailureCooldownAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	c := newLadderThemeAI(path)
	input := ladderThemeInput{Symbol: "300285.SZ", Name: "国瓷材料", TradeDate: "2026-09-18", Evidence: []string{"当日逐股涨停题材为固态电池"}}
	now := time.Now()
	_, _, _ = c.reserve(input, now)
	// A crash after dispatch is still an attempt, even with no final response.
	c = newLadderThemeAI(path)
	if e, start, _ := c.reserve(input, now.Add(ladderThemeCooldown-time.Second)); start || e.Status != "failed" {
		t.Fatal("restart evaded cooldown")
	}
	if _, start, _ := c.reserve(input, now.Add(ladderThemeCooldown)); !start {
		t.Fatal("cooldown never expires")
	}
}
func TestLadderThemeConcurrentManualRefreshDeduplicates(t *testing.T) {
	c := newLadderThemeAI("")
	input := ladderThemeInput{Symbol: "300285.SZ", Name: "国瓷材料", Force: true}
	var wg sync.WaitGroup
	starts := make(chan bool, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, start, _ := c.reserve(input, time.Now()); starts <- start }()
	}
	wg.Wait()
	close(starts)
	count := 0
	for start := range starts {
		if start {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("started %d duplicate calls", count)
	}
}
func TestLadderThemeInvalidAndUncertainResults(t *testing.T) {
	for _, content := range []string{"not json", `{}`, strings.Replace(validLadderTheme, `"medium"`, `"low"`, 1), strings.Replace(validLadderTheme, `["固态电池"]`, `["固态电池","电子陶瓷"]`, 1), strings.Replace(validLadderTheme, `"web"`, `"model_knowledge"`, 1)} {
		if _, err := parseLadderThemeResult(content); err == nil {
			t.Fatalf("accepted %s", content)
		}
	}
	if _, err := parseLadderThemeResult("```json\n" + validLadderTheme + "\n```"); err != nil {
		t.Fatal(err)
	}
}
func TestLadderThemeCorruptCacheFailsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), "ladder-theme-ai.json"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	c := newLadderThemeAI(path)
	if _, start, err := c.reserve(ladderThemeInput{Symbol: "300285.SZ"}, time.Now()); start || err == nil {
		t.Fatal("cache error started paid request")
	}
}

func TestLadderThemeRejectsFutureSources(t *testing.T) {
	c := newLadderThemeAI("")
	input := ladderThemeInput{Symbol: "300285.SZ", TradeDate: "2026-09-18"}
	_, _, _ = c.reserve(input, time.Now())
	c.run(input, &ladderThemePrompter{response: strings.Replace(validLadderTheme, "2026-09-18", "2026-09-21", 1)})
	if e := c.entries[input.Symbol]; e.Result != nil || e.Status != "failed" {
		t.Fatalf("accepted future evidence: %+v", e)
	}
}

func TestLadderThemeRetiresLegacyBusinessCacheButPreservesCooldown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	c := newLadderThemeAI(path)
	now := time.Now()
	c.entries["300285.SZ"] = ladderThemeEntry{Symbol: "300285.SZ", Status: "success", LastAttempt: now, Result: &ladderThemeResult{Themes: []string{"电子陶瓷"}}}
	if err := c.save(); err != nil {
		t.Fatal(err)
	}
	c = newLadderThemeAI(path)
	input := ladderThemeInput{Symbol: "300285.SZ"}
	e, start, err := c.reserve(input, now.Add(time.Hour))
	if err != nil || start || e.Result != nil || e.Status != "failed" {
		t.Fatalf("legacy cache: %+v %v", e, err)
	}
	input.Force = true
	if _, start, err = c.reserve(input, now.Add(time.Hour)); err != nil || !start {
		t.Fatal("manual migration blocked")
	}
}

func TestLadderThemeRejectsUnsafeOrMissingWebSources(t *testing.T) {
	for _, content := range []string{
		strings.Replace(validLadderTheme, "https://example.com/news", "javascript:alert(1)", 1),
		strings.Replace(validLadderTheme, "2026-09-18", "unknown", 1),
		`{"themes":["固态电池"],"reason":"推断","basis":"web","confidence":"high","sources":[]}`,
	} {
		if _, err := parseLadderThemeResult(content); err == nil {
			t.Fatalf("accepted bad source: %s", content)
		}
	}
}

func TestLadderThemeAcceptsSearchSummaryWithoutExtraMetadata(t *testing.T) {
	content := `结论如下：{"themes":["电子材料"],"reason":"近期报道指向电子材料题材炒作","caveat":"尚未量产","sources":[{"title":"异动报道","url":"https://example.com/news"}]}`
	result, err := parseLadderThemeResult(content)
	if err != nil || result.Basis != "web" || result.Confidence != "medium" || result.Caveat != "尚未量产" {
		t.Fatalf("rejected concise search answer: %+v %v", result, err)
	}
}

func TestLadderThemePromptDoesNotAnchorOnOldTheme(t *testing.T) {
	prompt := ladderThemePrompt(ladderThemeInput{Symbol: "001216.SZ", Name: "华瓷股份", TradeDate: "2026-09-18", ExistingTheme: "固态电池", Concepts: []string{"参股银行"}})
	for _, forbidden := range []string{"固态电池", "参股银行", "MLCC"} {
		if strings.Contains(prompt, forbidden) {
			t.Fatalf("prompt anchors on %s", forbidden)
		}
	}
	for _, required := range []string{"华瓷股份", "2026-09-18", "有明确指向就立即回答", "不强制抓正文"} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("prompt missing %s", required)
		}
	}
}
