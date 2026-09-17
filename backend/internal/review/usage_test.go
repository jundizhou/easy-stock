package review

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestBrowserNormalizationOnlySendsNewArticleMetadata(t *testing.T) {
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	oldURL, newURL := "https://www.tgb.cn/a/old", "https://www.tgb.cn/a/new"
	if _, err := store.UpsertPost(ctx, newPost("taoguba", oldURL, "作者", "旧标题", "旧正文", "", time.Now())); err != nil {
		t.Fatal(err)
	}
	prompter := &recordingPrompter{content: `{"articles":[{"original_url":"https://www.tgb.cn/a/old","title":"不得改写的旧标题"},{"original_url":"https://www.tgb.cn/a/new","title":"新标题整理后","content_text":"不得使用模型正文","published_at":"2026-09-17T15:00:00+08:00"}]}`}
	automation := NewAutomation(store, nil, nil, http.DefaultClient, "", prompter)
	raw := hermesXueqiuCollection{AuthorName: "作者", Articles: []hermesXueqiuArticle{
		{Title: "旧标题", OriginalURL: oldURL, ContentText: "旧正文", PublishedAt: "2026-09-16 15:00"},
		{Title: "新标题", OriginalURL: newURL, ContentText: "浏览器原始新正文", PublishedAt: "2026-09-17 15:00"},
	}}
	result, err := automation.normalizeBrowserBridgeCollection(ctx, Subscription{Source: "taoguba"}, raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(prompter.modules) != 1 || prompter.modules[0] != "review-normalization" || !strings.Contains(prompter.prompt, newURL) {
		t.Fatalf("missing new-article request: %+v", prompter)
	}
	for _, excluded := range []string{oldURL, "旧标题", "旧正文", "浏览器原始新正文"} {
		if strings.Contains(prompter.prompt, excluded) {
			t.Fatalf("normalization prompt includes unnecessary data %q", excluded)
		}
	}
	if len(result.Articles) != 2 {
		t.Fatalf("normalization lost discovered articles: %+v", result)
	}
	for _, article := range result.Articles {
		if article.OriginalURL == newURL && (article.Title != "新标题整理后" || article.ContentText != "浏览器原始新正文") {
			t.Fatalf("new article lost original content: %+v", article)
		}
		if article.OriginalURL == oldURL && article.Title != "旧标题" {
			t.Fatalf("model changed an existing article: %+v", article)
		}
	}
	if raw.Articles[1].ContentText != "浏览器原始新正文" || raw.Articles[1].Title != "新标题" {
		t.Fatalf("mutated browser input: %+v", raw)
	}
}

func TestBrowserNormalizationSkipsAIWhenDeduplicationFails(t *testing.T) {
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
	prompter := &recordingPrompter{}
	automation := NewAutomation(store, nil, nil, http.DefaultClient, "", prompter)
	_, err = automation.normalizeBrowserBridgeCollection(context.Background(), Subscription{Source: "taoguba"}, hermesXueqiuCollection{
		Articles: []hermesXueqiuArticle{{OriginalURL: "https://www.tgb.cn/a/new", Title: "新文章"}},
	})
	if err == nil || len(prompter.modules) != 0 {
		t.Fatalf("database failure should stop before AI: err=%v modules=%v", err, prompter.modules)
	}
}

func TestDailySummaryRepairKeepsUsageModule(t *testing.T) {
	prompter := &recordingPrompter{content: `{}`}
	attempts := 0
	_, err := promptDailySummaryJSON(context.Background(), prompter, "总结原始任务", "今日总结", func(string) (string, error) {
		attempts++
		if attempts == 1 {
			return "", errors.New("invalid initial output")
		}
		return "repaired", nil
	})
	if err != nil || !reflect.DeepEqual(prompter.modules, []string{"review-summary", "review-summary"}) {
		t.Fatalf("repair attribution: err=%v modules=%v", err, prompter.modules)
	}
}

func TestDailyValidationUsageModule(t *testing.T) {
	prompter := &recordingPrompter{content: `{"headline":"验证完成"}`}
	validation := DailyValidation{}
	err := EnrichDailyValidationWithAI(context.Background(), prompter, DailySummary{}, DailyValidationSnapshot{}, &validation)
	if err != nil || !reflect.DeepEqual(prompter.modules, []string{"review-validation"}) {
		t.Fatalf("validation attribution: err=%v modules=%v", err, prompter.modules)
	}
}
