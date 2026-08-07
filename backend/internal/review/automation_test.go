package review

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"easy-stock/backend/internal/appsettings"
	"easy-stock/backend/internal/hermes"
)

func TestAutomationSyncsWechatSubscription(t *testing.T) {
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/public/searchbiz":
			_, _ = w.Write([]byte(`{"success":true,"data":{"list":[{"fakeid":"MzTest==","nickname":"测试复盘号"}]}}`))
		case "/api/public/articles":
			_, _ = w.Write([]byte(`{"success":true,"data":{"articles":[{"link":"https://mp.weixin.qq.com/s/test-1"}]}}`))
		case "/api/article":
			_, _ = w.Write([]byte(`{"success":true,"data":{"title":"每日复盘","plain_content":"市场情绪回暖，明日观察核心股承接。","author":"测试复盘号","publish_time":1785800000}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer service.Close()
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	settings, _ := appsettings.Open("")
	_, _ = settings.Update(func(values *appsettings.Values) error {
		values.ReviewAutomation.Profiles[0].BaseURL = service.URL
		return nil
	})
	automation := NewAutomation(store, NewImporter(service.Client(), ""), settings, service.Client(), "")
	sub, err := automation.AddSubscription(context.Background(), "wechat", "测试复盘号", "", "wechat-default")
	if err != nil {
		t.Fatal(err)
	}
	result := automation.SyncOne(context.Background(), sub.ID)
	if result.Error != "" || result.Imported != 1 {
		t.Fatalf("sync result = %+v", result)
	}
	posts, total, err := store.ListPosts(context.Background(), Query{Limit: 10})
	if err != nil || total != 1 || posts[0].AuthorName != "测试复盘号" {
		t.Fatalf("posts=%+v total=%d err=%v", posts, total, err)
	}
}

func TestAutomationMapsWechatFreqControlAndStopsScheduledRetries(t *testing.T) {
	articleRequests := 0
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/public/searchbiz":
			_, _ = w.Write([]byte(`{"success":true,"data":{"list":[{"fakeid":"MzBlocked==","nickname":"受限公众号"}]}}`))
		case "/api/public/articles":
			articleRequests++
			_, _ = w.Write([]byte(`{"success":false,"error":"获取文章列表失败: ret=200013, msg=freq control"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer service.Close()

	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	settings, _ := appsettings.Open("")
	_, _ = settings.Update(func(values *appsettings.Values) error {
		values.ReviewAutomation.Profiles[0].BaseURL = service.URL
		return nil
	})
	automation := NewAutomation(store, NewImporter(service.Client(), ""), settings, service.Client(), "")
	sub, err := automation.AddSubscription(context.Background(), "wechat", "受限公众号", "", "wechat-default")
	if err != nil {
		t.Fatal(err)
	}

	result := automation.SyncOne(context.Background(), sub.ID)
	if result.Error != WechatArticleListUnavailableMessage {
		t.Fatalf("sync error = %q, want %q", result.Error, WechatArticleListUnavailableMessage)
	}
	stored, err := store.GetSubscription(context.Background(), sub.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.LastError != WechatArticleListUnavailableMessage || stored.NextSyncAt.Before(time.Now().AddDate(9, 0, 0)) {
		t.Fatalf("stored subscription = %+v", stored)
	}

	if err := store.SetSubscriptionSync(context.Background(), sub.ID, "error", "ret=200013, msg=freq control", time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if automation.due(context.Background()) {
		t.Fatal("blocked WeChat subscription must not make the scheduler due")
	}
	if results := automation.syncDue(context.Background()); len(results) != 0 {
		t.Fatalf("scheduled results = %+v, want none", results)
	}
	if articleRequests != 1 {
		t.Fatalf("scheduled article requests = %d, want 1 total", articleRequests)
	}

	results := automation.SyncAll(context.Background())
	if len(results) != 1 || results[0].Error != WechatArticleListUnavailableMessage || articleRequests != 1 {
		t.Fatalf("manual sync-all results=%+v articleRequests=%d", results, articleRequests)
	}
	manual := automation.SyncOne(context.Background(), sub.ID)
	if manual.Error != WechatArticleListUnavailableMessage || articleRequests != 2 {
		t.Fatalf("manual sync-one result=%+v articleRequests=%d", manual, articleRequests)
	}
}

func TestAutomationPrefersBundledWechatServiceOverPersistedProfileAddress(t *testing.T) {
	settings, _ := appsettings.Open("")
	_, _ = settings.Update(func(values *appsettings.Values) error {
		values.ReviewAutomation.Profiles[0].BaseURL = "http://127.0.0.1:5000"
		values.ReviewAutomation.Profiles[0].Credential = "legacy-token"
		return nil
	})
	automation := NewAutomation(nil, nil, settings, http.DefaultClient, "http://127.0.0.1:32001")

	profile, err := automation.profileFor("wechat", "wechat-default")
	if err != nil {
		t.Fatal(err)
	}
	if profile.BaseURL != "http://127.0.0.1:32001" || profile.Credential != "" {
		t.Fatalf("wechat runtime profile = %+v", profile)
	}
}

func TestAutomationAnalyzesPostWithConfiguredLLM(t *testing.T) {
	store, _ := OpenStore(":memory:")
	defer store.Close()
	settings, _ := appsettings.Open("")
	post, err := store.UpsertPost(context.Background(), newPost("taoguba", "https://www.taoguba.com.cn/a/test", "作者", "复盘", strings.Repeat("正文", 20), "", time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	prompter := fakePrompter{content: `{"summary":"情绪回暖","key_points":["观察核心承接"],"outlook":"明日关注分歧转强"}`}
	automation := NewAutomation(store, NewImporter(http.DefaultClient, ""), settings, http.DefaultClient, "", prompter)
	analyzed, err := automation.AnalyzePost(context.Background(), post.ID)
	if err != nil {
		t.Fatal(err)
	}
	if analyzed.AISummary != "情绪回暖" || len(analyzed.AIKeyPoints) != 1 || analyzed.AIAnalyzedAt.IsZero() {
		t.Fatalf("analysis=%+v", analyzed)
	}
}

func TestAutomationSummarizesTodayAcrossIndependentAuthors(t *testing.T) {
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	window := effectiveReviewWindow(time.Now())
	published := window.Start.Add(16 * time.Hour)
	posts := []Post{
		newPost("taoguba", "https://www.tgb.cn/a/daily-summary-1", "作者甲", "早盘观察", "甲作者早文独有标记：算力方向在分歧后主动回流。", "", published),
		newPost("taoguba", "https://www.tgb.cn/a/daily-summary-1b", "作者甲", "收盘修正", "甲作者晚文独有标记：核心容量承接仍强，但后排扩散不足。", "", published.Add(time.Hour)),
		newPost("xueqiu", "https://xueqiu.com/1/daily-summary-2", "作者乙", "盘面结构", "乙作者原文独有标记：市场情绪修复，明日确认板块扩散。", "", published.Add(30*time.Minute)),
		newPost("taoguba", "https://www.tgb.cn/a/daily-summary-old", "作者丙", "过期复盘独有标记", "上上个交易日及更早文章不应进入总结。", "", window.Start.Add(-time.Hour)),
	}
	for _, post := range posts {
		if _, err := store.UpsertPost(context.Background(), post); err != nil {
			t.Fatal(err)
		}
	}
	prompter := &stagedSummaryPrompter{
		authorContent: map[string]string{
			"作者甲": `{"core_view":"算力分歧回流但后排扩散不足","market_interpretation":"核心强于后排","view_evolution":["晚文降低对扩散的判断"],"themes":["算力"],"today_surprises":[],"tomorrow_focus":[],"tomorrow_outlook":"观察核心承接","catalysts":[],"risks":["后排不足"],"confidence":"高","evidence":["[收盘修正] 核心强但扩散不足"]}`,
			"作者乙": `{"core_view":"情绪修复，等待板块扩散确认","market_interpretation":"修复阶段","view_evolution":[],"themes":["算力"],"today_surprises":[{"name":"核心股","symbol":"","logic":"弱转强","evidence":["主动走强"],"trigger":"主动走强","invalidation":"无承接","risk":"单一来源"}],"tomorrow_focus":[],"tomorrow_outlook":"确认扩散","catalysts":[],"risks":[],"confidence":"中","evidence":["[盘面结构] 情绪修复"]}`,
		},
		finalContent: `{
		"executive_summary":"两位作者共同认为市场处于修复阶段，算力分歧后回流，但明日仍需核心股量价与板块扩散确认。",
		"market_regime":"分歧修复",
		"market_analysis":"算力是共同关注方向，核心容量承接强于后排。",
		"consensus":[{"topic":"算力回流","conclusion":"分歧后仍有资金承接","support_count":9,"authors":["作者甲","作者乙","不存在的作者"],"evidence":["[作者甲] 核心容量承接较强","[作者乙] 算力仍受关注"]}],
		"disagreements":[],
		"today_surprises":[{"name":"核心股","symbol":"","logic":"弱转强超预期","support_count":1,"authors":["作者乙"],"evidence":["[作者乙] 弱转强超预期"],"trigger":"主动走强","invalidation":"次日无承接","risk":"单一来源"}],
		"tomorrow_focus":[{"name":"核心容量股","symbol":"","logic":"观察量价承接和扩散","support_count":2,"authors":["作者甲","作者乙"],"evidence":[],"trigger":"竞价与开盘承接","invalidation":"核心负反馈","risk":"一致性过高"}],
		"tomorrow_outlook":"基础情景为分歧中延续，偏强看板块扩散，偏弱看核心负反馈。",
		"tomorrow_playbook":{"pre_open":["观察竞价"],"opening":["观察核心承接"],"intraday":["观察扩散"],"close":["确认强弱"]},
		"catalysts":["资金回流"],"risks":["共识拥挤"],"verification_checklist":["核心股是否承接"],"limitations":["仅两位作者"]
	}`,
	}
	automation := NewAutomation(store, NewImporter(http.DefaultClient, ""), nil, http.DefaultClient, "", prompter)
	summary, err := automation.SummarizeToday(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if summary.ArticleCount != 3 || summary.AuthorCount != 2 || len(summary.Consensus) != 1 || summary.Consensus[0].SupportCount != 2 {
		t.Fatalf("summary = %+v", summary)
	}
	if summary.WindowStart.IsZero() || summary.WindowEnd.IsZero() || !strings.Contains(summary.FreshnessRule, "上上个交易日") {
		t.Fatalf("freshness metadata = %+v", summary)
	}
	prompts := prompter.Prompts()
	if len(prompts) != 3 {
		t.Fatalf("prompt count = %d, want 2 author summaries + 1 final summary", len(prompts))
	}
	authorPromptCount := 0
	finalPrompt := ""
	for _, prompt := range prompts {
		if strings.Contains(prompt, "任务阶段：单作者观点归纳") {
			authorPromptCount++
			if strings.Contains(prompt, "作者：作者甲") && (!strings.Contains(prompt, "甲作者早文独有标记") || !strings.Contains(prompt, "甲作者晚文独有标记")) {
				t.Fatalf("作者甲的多篇文章未在同一次归纳中：%q", prompt)
			}
		} else {
			finalPrompt = prompt
		}
		if strings.Contains(prompt, "过期复盘独有标记") {
			t.Fatalf("过期文章进入模型提示词：%q", prompt)
		}
	}
	if authorPromptCount != 2 || !strings.Contains(finalPrompt, "输入作者观点卡JSON") || !strings.Contains(finalPrompt, "算力分歧回流但后排扩散不足") || strings.Contains(finalPrompt, "甲作者早文独有标记") {
		t.Fatalf("staged prompts = %#v", prompts)
	}
	stored, err := automation.GetTodaySummary(context.Background())
	if err != nil || stored == nil || stored.ExecutiveSummary != summary.ExecutiveSummary {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}
}

func TestEffectiveReviewWindowExcludesSecondPreviousTradingDay(t *testing.T) {
	location := shanghaiLocation()
	tests := []struct {
		name      string
		now       time.Time
		wantStart string
		wantRule  string
	}{
		{name: "friday after open", now: time.Date(2026, 8, 7, 10, 0, 0, 0, location), wantStart: "2026-08-06", wantRule: "开盘后"},
		{name: "monday before open", now: time.Date(2026, 8, 10, 9, 0, 0, 0, location), wantStart: "2026-08-07", wantRule: "开盘前"},
		{name: "monday after open", now: time.Date(2026, 8, 10, 10, 0, 0, 0, location), wantStart: "2026-08-07", wantRule: "开盘后"},
		{name: "weekend", now: time.Date(2026, 8, 8, 12, 0, 0, 0, location), wantStart: "2026-08-07", wantRule: "非交易日"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			window := effectiveReviewWindow(test.now)
			if got := window.Start.In(location).Format("2006-01-02"); got != test.wantStart || !strings.Contains(window.Rule, test.wantRule) {
				t.Fatalf("window=%+v start=%s", window, got)
			}
		})
	}
}

func TestDailySummarySelectsAtMostThirtyRecentlyUpdatedAuthors(t *testing.T) {
	posts := make([]Post, 0, 35)
	base := time.Date(2026, 8, 7, 9, 30, 0, 0, shanghaiLocation())
	for index := range 35 {
		author := fmt.Sprintf("作者%02d", index)
		posts = append(posts, newPost("taoguba", fmt.Sprintf("https://www.tgb.cn/a/author-%d", index), author, "复盘", "有效正文", "", base.Add(time.Duration(index)*time.Minute)))
	}
	groups := dailySummaryAuthorGroups(posts)
	selected := selectDailySummaryAuthorGroups(groups)
	if len(groups) != 35 || len(selected) != 30 {
		t.Fatalf("groups=%d selected=%d", len(groups), len(selected))
	}
	if selected[0].Author != "作者34" || selected[len(selected)-1].Author != "作者05" {
		t.Fatalf("selected newest range = %s ... %s", selected[0].Author, selected[len(selected)-1].Author)
	}
}

func TestAutomationSkipsFailedAuthorSummaryAndContinues(t *testing.T) {
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	window := effectiveReviewWindow(time.Now())
	for index, author := range []string{"作者甲", "作者乙", "作者丙"} {
		post := newPost("taoguba", fmt.Sprintf("https://www.tgb.cn/a/failure-%d", index), author, "复盘", "有效观点正文", "", window.Start.Add(time.Duration(12+index)*time.Hour))
		if _, err := store.UpsertPost(context.Background(), post); err != nil {
			t.Fatal(err)
		}
	}
	prompter := &stagedSummaryPrompter{
		authorContent: map[string]string{
			"作者甲": `{"core_view":"甲观点","market_interpretation":"修复","view_evolution":[],"themes":[],"today_surprises":[],"tomorrow_focus":[],"tomorrow_outlook":"甲预期","catalysts":[],"risks":[],"confidence":"中","evidence":[]}`,
			"作者丙": `{"core_view":"丙观点","market_interpretation":"分歧","view_evolution":[],"themes":[],"today_surprises":[],"tomorrow_focus":[],"tomorrow_outlook":"丙预期","catalysts":[],"risks":[],"confidence":"中","evidence":[]}`,
		},
		authorErrors: map[string]error{"作者乙": errors.New("上游模型暂时不可用")},
		finalContent: `{"executive_summary":"两位成功作者的综合结论","market_regime":"混沌","market_analysis":"观点存在差异","consensus":[],"disagreements":[],"today_surprises":[],"tomorrow_focus":[],"tomorrow_outlook":"等待确认","tomorrow_playbook":{"pre_open":[],"opening":[],"intraday":[],"close":[]},"catalysts":[],"risks":[],"verification_checklist":[],"limitations":[]}`,
	}
	automation := NewAutomation(store, NewImporter(http.DefaultClient, ""), nil, http.DefaultClient, "", prompter)
	summary, err := automation.SummarizeToday(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if summary.AuthorCount != 2 || summary.ArticleCount != 2 || !strings.Contains(strings.Join(summary.Limitations, "；"), "作者乙") {
		t.Fatalf("summary = %+v", summary)
	}
	if len(prompter.Prompts()) != 4 {
		t.Fatalf("prompt count = %d, want 3 author attempts + final", len(prompter.Prompts()))
	}
}

func TestAutomationRunsDailySummaryAsPersistentBackgroundJob(t *testing.T) {
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	window := effectiveReviewWindow(time.Now())
	for index, author := range []string{"作者甲", "作者乙"} {
		post := newPost("taoguba", fmt.Sprintf("https://www.tgb.cn/a/background-%d", index), author, "复盘", "有效观点正文", "", window.Start.Add(time.Duration(12+index)*time.Hour))
		if _, err := store.UpsertPost(context.Background(), post); err != nil {
			t.Fatal(err)
		}
	}
	prompter := &blockingSummaryPrompter{
		started:      make(chan struct{}, 2),
		release:      make(chan struct{}),
		finalContent: `{"executive_summary":"后台综合结论","market_regime":"修复","market_analysis":"两位作者完成归纳","consensus":[],"disagreements":[],"today_surprises":[],"tomorrow_focus":[],"tomorrow_outlook":"等待确认","tomorrow_playbook":{"pre_open":[],"opening":[],"intraday":[],"close":[]},"catalysts":[],"risks":[],"verification_checklist":[],"limitations":[]}`,
	}
	automation := NewAutomation(store, NewImporter(http.DefaultClient, ""), nil, http.DefaultClient, "", prompter)
	requestCtx, cancelRequest := context.WithCancel(context.Background())
	job, err := automation.StartTodaySummary(requestCtx, false)
	if err != nil {
		t.Fatal(err)
	}
	cancelRequest()
	if job.Status != "running" || job.Stage != "preparing" {
		t.Fatalf("initial job = %+v", job)
	}
	for range 2 {
		select {
		case <-prompter.started:
		case <-time.After(2 * time.Second):
			t.Fatal("author background prompts did not start")
		}
	}
	running, err := automation.GetTodaySummaryJob(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if running.Status != "running" || running.Stage != "authors" || running.TotalAuthors != 2 || running.CompletedAuthors != 0 {
		t.Fatalf("running job = %+v", running)
	}
	close(prompter.release)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		completed, statusErr := automation.GetTodaySummaryJob(context.Background())
		if statusErr != nil {
			t.Fatal(statusErr)
		}
		if completed.Status == "succeeded" {
			if !completed.SummaryAvailable || completed.CompletedAuthors != 2 {
				t.Fatalf("completed job = %+v", completed)
			}
			summary, summaryErr := automation.GetTodaySummary(context.Background())
			if summaryErr != nil || summary == nil || summary.ExecutiveSummary != "后台综合结论" {
				t.Fatalf("cached summary=%+v err=%v", summary, summaryErr)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("background summary did not complete")
}

func TestAutomationMarksPersistedRunningSummaryAsInterruptedAfterRestart(t *testing.T) {
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	window := effectiveReviewWindow(time.Now())
	_, err = store.SaveDailySummaryJob(context.Background(), DailySummaryJob{
		TradeDate: window.TradeDate,
		Status:    "running", Stage: "authors", CompletedAuthors: 1, TotalAuthors: 4,
		Message: "处理中", StartedAt: time.Now().Add(-time.Minute), UpdatedAt: time.Now().Add(-time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	automation := NewAutomation(store, NewImporter(http.DefaultClient, ""), nil, http.DefaultClient, "", fakePrompter{})
	job, err := automation.GetTodaySummaryJob(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != "failed" || job.Stage != "interrupted" || !strings.Contains(job.Error, "重启") {
		t.Fatalf("job = %+v", job)
	}
}

func TestAutomationUsesHermesWithPersistedXueqiuBrowserState(t *testing.T) {
	t.Setenv("A_STOCK_BROWSER_STATE_DIR", t.TempDir())
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	settings, _ := appsettings.Open("")
	_, _ = settings.Update(func(values *appsettings.Values) error {
		for index := range values.ReviewAutomation.Profiles {
			if values.ReviewAutomation.Profiles[index].Source == "xueqiu" {
				values.ReviewAutomation.Profiles[index].AutoAnalyze = false
			}
		}
		return nil
	})
	requestCount := 0
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requestCount++
		return nil, context.Canceled
	})}
	prompter := &fakeBrowserPrompter{content: `{"author_name":"测试大V","external_id":"2799158966","articles":[{"title":"盘后复盘","original_url":"https://xueqiu.com/2799158966/123456789","content_text":"今天市场先分歧后回流，核心方向出现承接。","published_at":"2026-08-07T15:30:00+08:00"}],"error":""}`}
	automation := NewAutomation(store, NewImporter(client, ""), settings, client, "", prompter)
	statePath := automation.browserStatePath("xueqiu-default")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, []byte(`{"cookies":[{"name":"xq_is_login","value":"1","domain":".xueqiu.com","path":"/","expires":-1,"httpOnly":false,"secure":true,"sameSite":"Lax"}],"origins":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	sub, err := automation.AddSubscription(context.Background(), "xueqiu", "https://xueqiu.com/u/2799158966", "测试大V", "xueqiu-default")
	if err != nil {
		t.Fatal(err)
	}
	result := automation.SyncOne(context.Background(), sub.ID)
	if result.Error != "" || result.Found != 1 || result.Imported != 1 {
		t.Fatalf("sync result = %+v", result)
	}
	if requestCount != 0 {
		t.Fatalf("xueqiu direct HTTP requests = %d, want Hermes browser only", requestCount)
	}
	if prompter.statePath != statePath || strings.Contains(strings.ToLower(prompter.prompt), "cookie=") {
		t.Fatalf("browser state handoff path=%q prompt=%q", prompter.statePath, prompter.prompt)
	}
	posts, total, err := store.ListPosts(context.Background(), Query{Source: "xueqiu", Limit: 10})
	if err != nil || total != 1 || posts[0].Title != "盘后复盘" || posts[0].AuthorName != "测试大V" {
		t.Fatalf("posts=%+v total=%d err=%v", posts, total, err)
	}
}

func TestAutomationUsesElectronBrowserBridgeBeforeHermesNormalization(t *testing.T) {
	t.Setenv("A_STOCK_BROWSER_STATE_DIR", t.TempDir())
	const bridgeToken = "local-browser-bridge-token"
	bridgeRequests := 0
	bridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bridgeRequests++
		if r.Header.Get("X-A-Stock-Browser-Token") != bridgeToken {
			t.Fatalf("browser bridge token = %q", r.Header.Get("X-A-Stock-Browser-Token"))
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request["profile_id"] != "xueqiu-default" || request["homepage_url"] != "https://xueqiu.com/u/2799158966" {
			t.Fatalf("browser bridge request = %+v", request)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"data":{"author_name":"测试大V","external_id":"2799158966","articles":[{"title":"原始标题","original_url":"https://xueqiu.com/2799158966/123456789","content_text":"内置 Electron 浏览器读取到的真实文章正文，市场先分歧后回流。","published_at":"2026-08-07 15:30"}],"error":""}}`))
	}))
	defer bridge.Close()
	t.Setenv("A_STOCK_BROWSER_BRIDGE_URL", bridge.URL)
	t.Setenv("A_STOCK_BROWSER_BRIDGE_TOKEN", bridgeToken)

	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	settings, _ := appsettings.Open("")
	_, _ = settings.Update(func(values *appsettings.Values) error {
		for index := range values.ReviewAutomation.Profiles {
			if values.ReviewAutomation.Profiles[index].Source == "xueqiu" {
				values.ReviewAutomation.Profiles[index].AutoAnalyze = false
			}
		}
		return nil
	})
	prompter := &recordingPrompter{content: `{"author_name":"测试大V","external_id":"2799158966","articles":[{"title":"盘后复盘","original_url":"https://xueqiu.com/2799158966/123456789","content_text":"模型不得替换这段正文","published_at":"2026-08-07T15:30:00+08:00"}],"error":""}`}
	automation := NewAutomation(store, NewImporter(bridge.Client(), ""), settings, bridge.Client(), "", prompter)
	statePath := automation.browserStatePath("xueqiu-default")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, []byte(`{"cookies":[{"name":"xq_is_login","value":"1","domain":".xueqiu.com","expires":-1}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	sub, err := automation.AddSubscription(context.Background(), "xueqiu", "https://xueqiu.com/u/2799158966", "测试大V", "xueqiu-default")
	if err != nil {
		t.Fatal(err)
	}
	result := automation.SyncOne(context.Background(), sub.ID)
	if result.Error != "" || result.Found != 1 || result.Imported != 1 {
		t.Fatalf("sync result = %+v", result)
	}
	if bridgeRequests != 1 || !strings.Contains(prompter.prompt, "内置 Electron 浏览器") || strings.Contains(strings.ToLower(prompter.prompt), "cookie=") {
		t.Fatalf("bridgeRequests=%d prompt=%q", bridgeRequests, prompter.prompt)
	}
	posts, total, err := store.ListPosts(context.Background(), Query{Source: "xueqiu", Limit: 10})
	if err != nil || total != 1 || posts[0].Title != "盘后复盘" || !strings.Contains(posts[0].ContentText, "真实文章正文") || strings.Contains(posts[0].ContentText, "模型不得替换") {
		t.Fatalf("posts=%+v total=%d err=%v", posts, total, err)
	}
}

func TestAutomationUsesElectronBrowserBridgeForTaoguba(t *testing.T) {
	t.Setenv("A_STOCK_BROWSER_STATE_DIR", t.TempDir())
	const bridgeToken = "local-taoguba-browser-bridge-token"
	bridgeRequests := 0
	bridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bridgeRequests++
		if r.URL.Path != "/v1/taoguba/collect" {
			t.Fatalf("browser bridge path = %q", r.URL.Path)
		}
		if r.Header.Get("X-A-Stock-Browser-Token") != bridgeToken {
			t.Fatalf("browser bridge token = %q", r.Header.Get("X-A-Stock-Browser-Token"))
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request["profile_id"] != "taoguba-default" || request["homepage_url"] != "https://www.tgb.cn/blog/5894557" {
			t.Fatalf("browser bridge request = %+v", request)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"data":{"author_name":"延边刺客","external_id":"5894557","articles":[{"title":"807","original_url":"https://www.tgb.cn/a/2u4fmoTIX4i","content_text":"内置 Electron 浏览器读取到的淘股吧真实文章正文，市场主线继续围绕算力展开。","published_at":"2026-08-07 16:06"}],"error":""}}`))
	}))
	defer bridge.Close()
	t.Setenv("A_STOCK_TAOGUBA_BROWSER_BRIDGE_URL", bridge.URL)
	t.Setenv("A_STOCK_TAOGUBA_BROWSER_BRIDGE_TOKEN", bridgeToken)

	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	settings, _ := appsettings.Open("")
	_, _ = settings.Update(func(values *appsettings.Values) error {
		for index := range values.ReviewAutomation.Profiles {
			if values.ReviewAutomation.Profiles[index].Source == "taoguba" {
				values.ReviewAutomation.Profiles[index].AutoAnalyze = false
			}
		}
		return nil
	})
	prompter := &recordingPrompter{content: `{"author_name":"延边刺客","external_id":"5894557","articles":[{"title":"807复盘","original_url":"https://www.tgb.cn/a/2u4fmoTIX4i","content_text":"模型不得替换正文","published_at":"2026-08-07T16:06:00+08:00"}],"error":""}`}
	automation := NewAutomation(store, NewImporter(bridge.Client(), ""), settings, bridge.Client(), "", prompter)
	statePath := automation.browserStatePath("taoguba-default", "taoguba")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, []byte(`{"cookies":[],"origins":[{"origin":"https://www.tgb.cn","localStorage":[{"name":"__easy_stock_taoguba_login_verified","value":"1"}]}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	sub, err := automation.AddSubscription(context.Background(), "taoguba", "https://www.tgb.cn/blog/5894557", "延边刺客", "taoguba-default")
	if err != nil {
		t.Fatal(err)
	}
	result := automation.SyncOne(context.Background(), sub.ID)
	if result.Error != "" || result.Found != 1 || result.Imported != 1 {
		t.Fatalf("sync result = %+v", result)
	}
	if bridgeRequests != 1 || !strings.Contains(prompter.prompt, "淘股吧文章整理代理") || strings.Contains(strings.ToLower(prompter.prompt), "cookie=") {
		t.Fatalf("bridgeRequests=%d prompt=%q", bridgeRequests, prompter.prompt)
	}
	posts, total, err := store.ListPosts(context.Background(), Query{Source: "taoguba", Limit: 10})
	if err != nil || total != 1 || posts[0].Title != "807复盘" || !strings.Contains(posts[0].ContentText, "真实文章正文") || strings.Contains(posts[0].ContentText, "模型不得替换") {
		t.Fatalf("posts=%+v total=%d err=%v", posts, total, err)
	}
}

func TestAutomationRequiresXueqiuBrowserLoginState(t *testing.T) {
	t.Setenv("A_STOCK_BROWSER_STATE_DIR", t.TempDir())
	store, _ := OpenStore(":memory:")
	defer store.Close()
	settings, _ := appsettings.Open("")
	automation := NewAutomation(store, NewImporter(http.DefaultClient, ""), settings, http.DefaultClient, "", &fakeBrowserPrompter{})
	sub, err := automation.AddSubscription(context.Background(), "xueqiu", "https://xueqiu.com/u/2799158966", "测试大V", "xueqiu-default")
	if err != nil {
		t.Fatal(err)
	}
	result := automation.SyncOne(context.Background(), sub.ID)
	if !strings.Contains(result.Error, "打开雪球登录窗口") {
		t.Fatalf("sync error = %q, want login guidance", result.Error)
	}
}

func TestAutomationRequiresTaogubaBrowserLoginState(t *testing.T) {
	t.Setenv("A_STOCK_BROWSER_STATE_DIR", t.TempDir())
	store, _ := OpenStore(":memory:")
	defer store.Close()
	settings, _ := appsettings.Open("")
	automation := NewAutomation(store, NewImporter(http.DefaultClient, ""), settings, http.DefaultClient, "", &fakeBrowserPrompter{})
	sub, err := automation.AddSubscription(context.Background(), "taoguba", "https://www.tgb.cn/blog/5894557", "延边刺客", "taoguba-default")
	if err != nil {
		t.Fatal(err)
	}
	result := automation.SyncOne(context.Background(), sub.ID)
	if !strings.Contains(result.Error, "打开淘股吧登录窗口") {
		t.Fatalf("sync error = %q, want login guidance", result.Error)
	}
}

type fakePrompter struct{ content string }

func (p fakePrompter) Prompt(context.Context, string) (hermes.PromptResult, error) {
	return hermes.PromptResult{Content: p.content}, nil
}

type recordingPrompter struct {
	content string
	prompt  string
}

type stagedSummaryPrompter struct {
	mu            sync.Mutex
	prompts       []string
	authorContent map[string]string
	authorErrors  map[string]error
	finalContent  string
}

type blockingSummaryPrompter struct {
	started      chan struct{}
	release      chan struct{}
	finalContent string
}

func (p *blockingSummaryPrompter) Prompt(ctx context.Context, prompt string) (hermes.PromptResult, error) {
	if strings.Contains(prompt, "任务阶段：单作者观点归纳") {
		select {
		case p.started <- struct{}{}:
		default:
		}
		select {
		case <-p.release:
			return hermes.PromptResult{Content: `{"core_view":"作者最终观点","market_interpretation":"情绪修复","view_evolution":[],"themes":[],"today_surprises":[],"tomorrow_focus":[],"tomorrow_outlook":"等待确认","catalysts":[],"risks":[],"confidence":"中","evidence":[]}`}, nil
		case <-ctx.Done():
			return hermes.PromptResult{}, ctx.Err()
		}
	}
	return hermes.PromptResult{Content: p.finalContent}, nil
}

func (p *stagedSummaryPrompter) Prompt(_ context.Context, prompt string) (hermes.PromptResult, error) {
	p.mu.Lock()
	p.prompts = append(p.prompts, prompt)
	p.mu.Unlock()
	if strings.Contains(prompt, "任务阶段：单作者观点归纳") {
		for author, err := range p.authorErrors {
			if strings.Contains(prompt, "作者："+author+"\n") {
				return hermes.PromptResult{}, err
			}
		}
		for author, content := range p.authorContent {
			if strings.Contains(prompt, "作者："+author+"\n") {
				return hermes.PromptResult{Content: content}, nil
			}
		}
		return hermes.PromptResult{}, errors.New("missing staged author response")
	}
	return hermes.PromptResult{Content: p.finalContent}, nil
}

func (p *stagedSummaryPrompter) Prompts() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.prompts...)
}

func (p *recordingPrompter) Prompt(_ context.Context, prompt string) (hermes.PromptResult, error) {
	p.prompt = prompt
	return hermes.PromptResult{Content: p.content}, nil
}

type fakeBrowserPrompter struct {
	content   string
	prompt    string
	statePath string
}

func (p *fakeBrowserPrompter) Prompt(context.Context, string) (hermes.PromptResult, error) {
	return hermes.PromptResult{Content: p.content}, nil
}

func (p *fakeBrowserPrompter) PromptWithBrowserState(_ context.Context, prompt, statePath string) (hermes.PromptResult, error) {
	p.prompt = prompt
	p.statePath = statePath
	return hermes.PromptResult{Content: p.content}, nil
}
