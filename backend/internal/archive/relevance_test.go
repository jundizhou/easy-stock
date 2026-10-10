package archive

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"easy-stock/backend/internal/foundation"
)

type fixedNews struct{ items []foundation.NewsItem }

func (f *fixedNews) LatestNews(context.Context, int) ([]foundation.NewsItem, error) {
	return f.items, nil
}

// TestNewsRelevanceTagInjected 验证归档时注入 relevance 标记。
func TestNewsRelevanceTagInjected(t *testing.T) {
	root := t.TempDir()
	items := []foundation.NewsItem{
		{ID: "a", Title: "固态电池量产装车提速"},                          // RELEVANT
		{ID: "b", Title: "某公司中标5亿元大单"},                          // WEAK
		{ID: "c", Title: "交易员减少对加拿大央行年底前加息的押注"},        // IRRELEVANT
		{ID: "d", Title: "伊朗霍尔木兹海峡LPG油轮遇袭"},                  // IRRELEVANT
	}
	ar, err := NewNewsArchiver(&fixedNews{items: items}, root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ar.LatestNews(context.Background(), 20); err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(root, "news")
	entries, _ := os.ReadDir(dir)
	var content string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".jsonl") && !strings.HasPrefix(e.Name(), ".seen-") {
			b, _ := os.ReadFile(filepath.Join(dir, e.Name()))
			content = string(b)
		}
	}
	wants := []string{
		"relevance:RELEVANT",
		"relevance:WEAK",
		"relevance:IRRELEVANT",
	}
	for _, w := range wants {
		if !strings.Contains(content, w) {
			t.Fatalf("归档内容缺少标记 %q\n%s", w, content)
		}
	}
}

// TestNewsRelevanceDisabled 验证开关生效。
func TestNewsRelevanceDisabled(t *testing.T) {
	root := t.TempDir()
	items := []foundation.NewsItem{{ID: "a", Title: "加拿大央行加息"}}
	ar, _ := NewNewsArchiver(&fixedNews{items: items}, root,
		Options{NoRelevanceTag: true})
	ar.LatestNews(context.Background(), 20)

	dir := filepath.Join(root, "news")
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".jsonl") && !strings.HasPrefix(e.Name(), ".seen-") {
			b, _ := os.ReadFile(filepath.Join(dir, e.Name()))
			if strings.Contains(string(b), "relevance:") {
				t.Fatal("关闭开关后不应注入 relevance 标记")
			}
		}
	}
}

// TestClassifyNewsRelevance 单元测试判别函数。
func TestClassifyNewsRelevance(t *testing.T) {
	cases := []struct{ text, want string }{
		{"加拿大央行加息预期下降", relevanceIrrelevant},
		{"伊朗霍尔木兹海峡油轮遇袭", relevanceIrrelevant},
		{"白宫宣布新政策", relevanceIrrelevant},
		{"某公司中标大单", relevanceWeak},
		{"产品涨价通知", relevanceWeak},
		{"固态电池技术突破", relevanceRelevant},
	}
	for _, c := range cases {
		if got := classifyNewsRelevance(c.text, ""); got != c.want {
			t.Errorf("classifyNewsRelevance(%q) = %q, want %q", c.text, got, c.want)
		}
	}
}
