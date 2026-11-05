package review

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"easy-stock/backend/internal/agent"
)

type reviewCompactionPrompter struct {
	mu             sync.Mutex
	prompts        []string
	records        []reviewTextRecord
	largeSummaries bool
	invalidSummary bool
	failFinal      bool
	cancel         context.CancelFunc
}

func (p *reviewCompactionPrompter) Prompt(ctx context.Context, prompt string) (agent.PromptResult, error) {
	return p.PromptWithOptions(ctx, prompt, agent.PromptOptions{})
}

func (p *reviewCompactionPrompter) PromptWithOptions(ctx context.Context, prompt string, options agent.PromptOptions) (agent.PromptResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(prompt) > maxReviewPromptBytes {
		return agent.PromptResult{}, fmt.Errorf("oversized model request: %d", len(prompt))
	}
	p.prompts = append(p.prompts, prompt)
	if _, payload, ok := strings.Cut(prompt, "[复盘资料JSON]\n"); ok {
		if !options.DisableTools || agent.UsageModule(ctx) != "review-summary" {
			return agent.PromptResult{}, errors.New("compaction lost sandbox/usage policy")
		}
		var records []reviewTextRecord
		if err := json.NewDecoder(strings.NewReader(payload)).Decode(&records); err != nil {
			return agent.PromptResult{}, err
		}
		p.records = append(p.records, records...)
		_, budget, _ := strings.Cut(prompt, "summary字符串JSON编码不得超过")
		var limit int
		_, _ = fmt.Sscanf(budget, "%d字节", &limit)
		summary := "作者观点与分歧须按来源核验；风险触发后原判断失效。"
		if p.largeSummaries {
			summary = strings.Repeat("风险失效须核验。", (min(limit, 3602)-2)/24)
		}
		if p.invalidSummary {
			summary = strings.Repeat("风", reviewSummaryBytes)
		}
		data, _ := json.Marshal(map[string]string{"summary": summary})
		if p.cancel != nil {
			p.cancel()
			p.cancel = nil
		}
		return agent.PromptResult{Content: string(data)}, nil
	}
	if strings.Contains(prompt, "任务阶段：单作者观点归纳") {
		return agent.PromptResult{Content: validAuthorSummaryJSON("作者最终修正：失效条件触发则不再看好")}, nil
	}
	if p.failFinal {
		return agent.PromptResult{}, errors.New("simulated final phase interruption")
	}
	if strings.Contains(prompt, "任务阶段：跨作者市场结构归纳") {
		return agent.PromptResult{Content: validMarketSynthesisJSON("按独立作者计票")}, nil
	}
	if strings.Contains(prompt, "任务阶段：跨作者明日计划归纳") {
		return agent.PromptResult{Content: validTomorrowPlanJSON("等待条件验证")}, nil
	}
	return agent.PromptResult{Content: `{"summary":"全文已提炼","key_points":["文末修正须保留"],"outlook":"等待验证"}`}, nil
}

func checkpointContext(store *Store, identity string) context.Context {
	return context.WithValue(context.Background(), reviewSummarySessionKey{}, &reviewSummarySession{store: store, identity: identity})
}

func TestReviewAuthorInputsRetainAllArticlesAndFullText(t *testing.T) {
	var posts []Post
	for i := 0; i < 30; i++ {
		posts = append(posts, newPost("taoguba", fmt.Sprintf("https://example.invalid/%d", i), "作者甲", fmt.Sprintf("文章%d", i), strings.Repeat("完整正文。", 3000)+fmt.Sprintf("文末失效条件%d", i), "", time.Now().Add(-time.Duration(i)*time.Hour)))
	}
	group := dailySummaryAuthorGroups(posts)[0]
	before, _ := json.Marshal(group)
	articles, sources := authorSummaryInputs(group)
	if len(articles) != len(posts) || len(sources) != len(posts) {
		t.Fatal("articles were dropped at the old per-author limit")
	}
	for i, article := range articles {
		original := group.Posts[len(posts)-1-i]
		if article.Content != original.ContentText || article.PostID != original.ID || sources[i].PostID != original.ID || article.URL != original.OriginalURL || article.Index != i+1 {
			t.Fatal("article content, source identity or chronology changed")
		}
	}
	after, _ := json.Marshal(group)
	if string(before) != string(after) {
		t.Fatal("modified original posts")
	}
}

func TestReviewChunksPreserveExactEscapedTextAndProvenance(t *testing.T) {
	record := reviewTextRecord{Reference: reviewTextReference{Author: "作者甲", PostID: "post1", URL: "https://example.invalid/1", PublishedAt: "2026-11-04 15:30"}, Index: 3, Text: strings.Repeat("风险\"<>&🙂。\n", 12000) + "最终修正：不再看好"}
	chunks, err := reviewTextChunks([]reviewTextRecord{record})
	if err != nil || len(chunks) < 2 {
		t.Fatal("bad chunking", err)
	}
	var text strings.Builder
	parts := 0
	for _, chunk := range chunks {
		data, _ := json.Marshal(chunk)
		if len(data) > reviewChunkBytes || !json.Valid(data) || !utf8.Valid(data) {
			t.Fatal("invalid encoded chunk")
		}
		for _, fragment := range chunk {
			parts++
			if !reflect.DeepEqual(fragment.Reference, record.Reference) || fragment.Index != record.Index || fragment.Part != parts {
				t.Fatal("fragment lost provenance or order")
			}
			text.WriteString(fragment.Text)
		}
	}
	if text.String() != record.Text || chunks[0][0].Parts != parts {
		t.Fatal("full-text reconstruction failed")
	}
}

func TestReviewLongAuthorUsesAllFragmentsAndBoundedMerge(t *testing.T) {
	p := &reviewCompactionPrompter{largeSummaries: true}
	a := NewAutomation(nil, nil, nil, nil, p)
	post := newPost("taoguba", "https://example.invalid/long", "作者甲", "长文", strings.Repeat("早先观点，需要核验。", 15000)+"文末独有修正：跌破则原判断失效", "", time.Now())
	group := dailySummaryAuthorGroups([]Post{post})[0]
	view, articles, sources, fallback, err := a.summarizeDailyAuthor(context.Background(), group)
	if err != nil || fallback != "" || view.ArticleCount != 1 || len(articles) != 1 || len(sources) != 1 {
		t.Fatalf("long author failed: %v", err)
	}
	var original strings.Builder
	merged := false
	for _, record := range p.records {
		if record.Reference.Merging {
			merged = true
			if record.Reference.PostID != "" {
				t.Fatal("merged summary was attributed to the first article")
			}
			continue
		}
		if record.Reference.PostID != post.ID {
			t.Fatal("fragment lost original post ID")
		}
		original.WriteString(record.Text)
	}
	if original.String() != post.ContentText || !merged {
		t.Fatal("tail text lost or large partial summaries were not merged")
	}
	if len(p.prompts[len(p.prompts)-1]) > targetReviewPromptBytes {
		t.Fatal("author model input over target budget")
	}
}

func TestReviewThirtyAuthorsFitBothFinalPhaseBudgets(t *testing.T) {
	views := make([]DailyAuthorView, 30)
	for i := range views {
		views[i] = DailyAuthorView{Author: fmt.Sprintf("作者%02d", i), Source: "淘股吧", ArticleCount: 5, AvailableArticleCount: 5, CoreView: strings.Repeat("必须保留反证与修正。", 1600), Risks: []string{"触发失效条件则原判断不成立"}, Sources: []DailySummarySource{{PostID: fmt.Sprintf("post-%d", i), Author: fmt.Sprintf("作者%02d", i), URL: fmt.Sprintf("https://example.invalid/%d", i), PublishedAt: "2026-11-04T15:30:00+08:00"}}}
		for j := 0; j < 10; j++ {
			views[i].Sources = append(views[i].Sources, DailySummarySource{PostID: fmt.Sprintf("source-%d-%d", i, j), Author: views[i].Author, Title: strings.Repeat("披露与风险说明", 10), URL: fmt.Sprintf("https://example.invalid/%d/%d", i, j), PublishedAt: "2026-11-04T15:30:00+08:00"})
		}
	}
	before, _ := json.Marshal(views)
	p := &reviewCompactionPrompter{largeSummaries: true}
	a := NewAutomation(nil, nil, nil, nil, p)
	market, tomorrow, compacted, err := a.prepareDailySummaryPhasePrompts(context.Background(), effectiveReviewWindow(time.Now()), views, DailyUSMarket{})
	if err != nil || !compacted || max(len(market), len(tomorrow)) > targetReviewPromptBytes {
		t.Fatalf("30-author budget failed: %d/%d %v", len(market), len(tomorrow), err)
	}
	for _, prompt := range []string{market, tomorrow} {
		_, payload, _ := strings.Cut(prompt, "输入作者观点卡JSON：")
		var cards []compactDailyAuthorView
		if err := json.NewDecoder(strings.NewReader(payload)).Decode(&cards); err != nil {
			t.Fatal(err)
		}
		if len(cards) != len(views) {
			t.Fatal("authors were removed to fit the budget")
		}
		for i, card := range cards {
			if card.Author != views[i].Author || card.ArticleCount != 5 || !reflect.DeepEqual(card.Sources, views[i].Sources) {
				t.Fatal("votes or source catalog changed")
			}
		}
	}
	after, _ := json.Marshal(views)
	if string(before) != string(after) {
		t.Fatal("original cards were modified")
	}
	t.Logf("30 authors: market_bytes=%d tomorrow_bytes=%d calls=%d", len(market), len(tomorrow), len(p.prompts))
}

func TestReviewCompactionFailureDoesNotSendOversizedAuthorPrompt(t *testing.T) {
	p := &reviewCompactionPrompter{invalidSummary: true}
	a := NewAutomation(nil, nil, nil, nil, p)
	post := newPost("taoguba", "https://example.invalid/invalid", "作者甲", "长文", strings.Repeat("反证与风险。", 20000), "", time.Now())
	_, _, _, _, err := a.summarizeDailyAuthor(context.Background(), dailySummaryAuthorGroups([]Post{post})[0])
	if err == nil || len(p.prompts) != 2 {
		t.Fatalf("invalid summary was not bounded: calls=%d err=%v", len(p.prompts), err)
	}
	for _, prompt := range p.prompts {
		if strings.HasPrefix(prompt, "任务阶段：单作者观点归纳") {
			t.Fatal("scoring proceeded after compaction failure")
		}
	}
}

func TestReviewAuthorCheckpointsSurviveRestartAndInvalidateChanges(t *testing.T) {
	file := filepath.Join(t.TempDir(), "review.db")
	store, err := OpenStore(file)
	if err != nil {
		t.Fatal(err)
	}
	p := &reviewCompactionPrompter{}
	post := newPost("taoguba", "https://example.invalid/cache", "作者甲", "复盘", "原始观点", "", time.Now())
	run := func(identity string, wantCalls int) {
		t.Helper()
		a := NewAutomation(store, nil, nil, nil, p)
		view, _, _, _, err := a.summarizeDailyAuthor(checkpointContext(store, identity), dailySummaryAuthorGroups([]Post{post})[0])
		if err != nil || view.Author != "作者甲" || len(p.prompts) != wantCalls {
			t.Fatalf("checkpoint reuse: calls=%d want=%d err=%v", len(p.prompts), wantCalls, err)
		}
	}
	run("model-a", 1)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenStore(file)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	run("model-a", 1)
	run("model-b", 2)
	post.ContentText += "；作者更新了失效条件"
	run("model-a", 3)
	run("", 4)
	run("", 5)
}

func TestReviewCancelledCompactionResumesCompletedFragments(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "review.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	records := []reviewTextRecord{{Reference: reviewTextReference{Author: "作者甲", PostID: "post1"}, Text: strings.Repeat("全部文章需要逐段覆盖。", 7000)}}
	chunks, _ := reviewTextChunks(records)
	ctx, cancel := context.WithCancel(checkpointContext(store, "model-a"))
	p := &reviewCompactionPrompter{cancel: cancel}
	_, err = compactReviewRecords(ctx, p, records, reviewSummaryBytes)
	if !errors.Is(err, context.Canceled) || len(p.prompts) != 1 {
		t.Fatalf("cancel did not preserve one completed fragment: %v", err)
	}
	p = &reviewCompactionPrompter{}
	_, err = compactReviewRecords(checkpointContext(store, "model-a"), p, records, reviewSummaryBytes)
	if err != nil || len(p.prompts) != len(chunks)-1 {
		t.Fatalf("resume repeated completed work: calls=%d chunks=%d err=%v", len(p.prompts), len(chunks), err)
	}
}

func TestReviewPartialSummaryReusesAuthorsAndKeepsOneVoteEach(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "review.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	window := effectiveReviewWindow(time.Now())
	for i := 0; i < 2; i++ {
		post := newPost("taoguba", fmt.Sprintf("https://example.invalid/author-%d", i), fmt.Sprintf("作者%d", i), "复盘", "原作者观点与风险", "", window.Start.Add(time.Hour))
		if _, err := store.UpsertPost(context.Background(), post); err != nil {
			t.Fatal(err)
		}
	}
	p := &reviewCompactionPrompter{failFinal: true}
	a := NewAutomation(store, nil, nil, nil, p)
	summary, err := a.summarizeWindow(checkpointContext(store, "model-a"), window, nil)
	if err != nil || !summary.Partial {
		t.Fatalf("expected recoverable partial result: %v", err)
	}
	before := len(p.prompts)
	p.failFinal = false
	summary, err = a.summarizeWindow(checkpointContext(store, "model-a"), window, nil)
	if err != nil || summary.Partial || summary.AuthorCount != 2 || summary.ArticleCount != 2 {
		t.Fatalf("resume failed: %+v %v", summary, err)
	}
	for _, prompt := range p.prompts[before:] {
		if strings.Contains(prompt, "任务阶段：单作者观点归纳") {
			t.Fatal("completed authors were regenerated")
		}
	}
}

func TestReviewSingleArticleAnalysisIncludesTextAfterOldCutoff(t *testing.T) {
	p := &reviewCompactionPrompter{}
	post := newPost("taoguba", "https://example.invalid/single", "作者甲", "长文章", strings.Repeat("正文。", 5000)+"尾部独有风险：预期失效", "", time.Now())
	_, err := analyzeWithHermes(context.Background(), p, post)
	if err != nil || len(p.prompts) != 1 || !strings.Contains(p.prompts[0], "尾部独有风险：预期失效") {
		t.Fatalf("single article tail lost: calls=%d err=%v", len(p.prompts), err)
	}
}

func TestReviewProtectedCatalogFailureStopsBeforeModel(t *testing.T) {
	p := &reviewCompactionPrompter{}
	a := NewAutomation(nil, nil, nil, nil, p)
	views := []DailyAuthorView{{Author: "作者甲", Sources: []DailySummarySource{{Title: strings.Repeat("来源", 50000)}}}}
	_, _, _, err := a.prepareDailySummaryPhasePrompts(context.Background(), effectiveReviewWindow(time.Now()), views, DailyUSMarket{})
	if err == nil || len(p.prompts) != 0 {
		t.Fatalf("protected catalog was silently dropped or sent oversized: %v", err)
	}
}

func TestReviewOversizedRequestAndRepairStayWithinHardLimit(t *testing.T) {
	p := &reviewCompactionPrompter{}
	_, err := promptDailySummaryJSON(context.Background(), p, strings.Repeat("x", maxReviewPromptBytes+1), "测试", parseAuthorViewpointModel)
	if err == nil || len(p.prompts) != 0 {
		t.Fatal("oversized initial request reached model")
	}
	parseCalls := 0
	_, err = promptDailySummaryJSON(context.Background(), p, strings.Repeat("x", targetReviewPromptBytes), "测试", func(string) (string, error) {
		parseCalls++
		if parseCalls == 1 {
			return "", errors.New("invalid JSON")
		}
		return "repaired", nil
	})
	if err != nil || len(p.prompts) != 2 || !strings.Contains(p.prompts[1], "[原始任务]") {
		t.Fatalf("bounded repair failed: %v", err)
	}
}

func TestReviewSummaryLengthUsesEncodedJSONBytes(t *testing.T) {
	text := "风险\"<>&🙂\n"
	encoded, _ := json.Marshal(text)
	if validReviewSummary(text, len(encoded)-1) || !validReviewSummary(text, len(encoded)) || validReviewSummary("", reviewSummaryBytes) {
		t.Fatal("summary did not enforce encoded byte limit")
	}
}

func TestReviewCompactionStopsAtSharedCallLimit(t *testing.T) {
	p := &reviewCompactionPrompter{}
	ctx := checkpointContext(nil, "")
	ctx.Value(reviewSummarySessionKey{}).(*reviewSummarySession).calls.Store(reviewMaxCompactCalls)
	_, err := compactReviewRecords(ctx, p, []reviewTextRecord{{Text: "必须保留的正文"}}, reviewSummaryBytes)
	if err == nil || len(p.prompts) != 0 || !strings.Contains(err.Error(), "调用上限") {
		t.Fatalf("call limit was ignored: %v", err)
	}
}

func TestReviewDeletionClearsDerivedCheckpoints(t *testing.T) {
	for _, author := range []bool{false, true} {
		t.Run(fmt.Sprintf("author_%v", author), func(t *testing.T) {
			store, err := OpenStore(filepath.Join(t.TempDir(), "review.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			post, err := store.UpsertPost(context.Background(), newPost("taoguba", "https://example.invalid/delete", "作者甲", "文章", "作者观点", "", time.Now()))
			if err != nil {
				t.Fatal(err)
			}
			ctx := checkpointContext(store, "model-a")
			key := reviewSummaryCheckpointKey(ctx, "作者观点归纳", "正文")
			if err := saveReviewSummaryCheckpoint(ctx, key, map[string]string{"core_view": "作者观点"}); err != nil {
				t.Fatal(err)
			}
			if author {
				_, err = store.DeleteAuthor(ctx, post.Source, post.AuthorID)
			} else {
				err = store.DeletePost(ctx, post.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, found, err := loadReviewSummaryCheckpoint(ctx, key); err != nil || found {
				t.Fatalf("deleted source kept derived checkpoint: %v", err)
			}
		})
	}
}
