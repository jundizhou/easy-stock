package cockpit

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestFlowBoardEncodesEmptySidesAsArrays(t *testing.T) {
	board := BuildFlowBoard([]FlowItem{{Name: "A", NetInflow: -5}}, 3)
	encoded, err := json.Marshal(board)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"inflow":[]`) || !strings.Contains(string(encoded), `"outflow":[{`) {
		t.Fatalf("empty inflow must encode as [], got %s", encoded)
	}
}

func TestCollectFactsRetainsMorePreOpenNewsAndBoundsOriginalExcerpts(t *testing.T) {
	items := make([]NewsLine, 60)
	for i := range items {
		items[i] = NewsLine{Title: "测试新闻", Content: strings.Repeat("测", 1005), PublishedDate: "2026-10-28", PublishedAt: "08:20", Source: "test:news", URL: "https://example.invalid/news", Tags: []string{"测试题材"}}
	}
	feed := Feed{News: func(context.Context) ([]NewsLine, error) { return items, nil }}
	before := time.Now().Truncate(time.Second)
	for _, scenario := range []struct {
		phase Phase
		count int
	}{{PhasePreOpen, 50}, {PhaseRest, 50}, {PhaseIntraday, 15}, {PhasePostClose, 15}} {
		facts := CollectFacts(context.Background(), scenario.phase, feed)
		if len(facts.News) != scenario.count {
			t.Fatalf("phase %s lost source or news sample: %+v", scenario.phase, facts)
		}
		newsSourceOK := false
		for _, source := range facts.Sources {
			if source.Name == "快讯" && source.OK {
				newsSourceOK = true
			}
		}
		if !newsSourceOK {
			t.Fatalf("phase %s lost successful news source: %+v", scenario.phase, facts.Sources)
		}
		published := facts.News[0]
		if !published.ContentTruncated || published.Content != strings.Repeat("测", 1000)+"…" || published.PublishedDate != items[0].PublishedDate || published.PublishedAt != items[0].PublishedAt || published.Source != items[0].Source || published.URL != items[0].URL || published.Tags[0] != "测试题材" {
			t.Fatalf("original excerpt or provenance lost: %+v", published)
		}
		collected, err := time.Parse(time.RFC3339, facts.CollectedAt)
		if err != nil || collected.Before(before) || collected.After(time.Now()) {
			t.Fatalf("missing or invalid actual collection time %q: %v", facts.CollectedAt, err)
		}
	}
	if items[0].ContentTruncated || len([]rune(items[0].Content)) != 1005 {
		t.Fatal("news excerpting mutated shared provider data")
	}
}

func TestCollectFactsDoesNotFillMissingNewsOrInventProvenance(t *testing.T) {
	facts := CollectFacts(context.Background(), PhasePreOpen, Feed{News: func(context.Context) ([]NewsLine, error) {
		return []NewsLine{{Title: "只有标题"}}, nil
	}})
	if len(facts.News) != 1 {
		t.Fatalf("news must not be padded: %+v", facts.News)
	}
	encoded, err := json.Marshal(facts.News[0])
	if err != nil || string(encoded) != `{"title":"只有标题"}` {
		t.Fatalf("missing evidence must remain absent: %s, %v", encoded, err)
	}
}

func TestCollectFactsEncodesEmptySourcesAsArray(t *testing.T) {
	facts := CollectFacts(context.Background(), PhasePreOpen, Feed{})
	encoded, err := json.Marshal(facts)
	if err != nil {
		t.Fatal(err)
	}
	if facts.Sources == nil || !strings.Contains(string(encoded), `"sources":[]`) {
		t.Fatalf("sources must be an empty array, got %s", encoded)
	}
}
