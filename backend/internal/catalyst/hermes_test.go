package catalyst

import (
	"strings"
	"testing"
)

func TestParseResponseKeepsOnlyKnownIDs(t *testing.T) {
	candidates := []Candidate{
		{Item: newsItem("known-1", "工信部批准车规级芯片量产", ""), Score: 55},
	}
	content := `{"items":[
		{"id":"known-1","impact":"bullish","strength":85,"sectors":["半导体"],"stocks":["某公司"],"horizon":"short","why":"国产替代进入量产"},
		{"id":"hallucinated","impact":"bullish","strength":99,"sectors":["不存在"],"why":"模型凭空生成"}
	]}`
	items, err := parseResponse(content, candidates)
	if err != nil {
		t.Fatalf("parseResponse: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("只应保留候选里存在的 id，实际 %d 条：%+v", len(items), items)
	}
	if items[0].Title != "工信部批准车规级芯片量产" {
		t.Fatalf("应回填候选原文，实际 %s", items[0].Title)
	}
	if items[0].Strength != 85 || items[0].Impact != "bullish" {
		t.Fatalf("模型给出的字段应保留：%+v", items[0])
	}
}

func TestParseResponseAcceptsEmptyItems(t *testing.T) {
	items, err := parseResponse(`{"items":[]}`, []Candidate{{Item: newsItem("1", "x", "")}})
	if err != nil {
		t.Fatalf("空结果是合法输出，不应报错：%v", err)
	}
	if len(items) != 0 {
		t.Fatalf("应为空，实际 %d", len(items))
	}
}

func TestParseResponseStripsCodeFence(t *testing.T) {
	candidates := []Candidate{{Item: newsItem("1", "标题", ""), Score: 40}}
	content := "```json\n{\"items\":[{\"id\":\"1\",\"impact\":\"bullish\",\"strength\":70}]}\n```"
	items, err := parseResponse(content, candidates)
	if err != nil {
		t.Fatalf("应容忍 Markdown 围栏：%v", err)
	}
	if len(items) != 1 {
		t.Fatalf("应为 1 条，实际 %d", len(items))
	}
}

func TestParseResponseRejectsNonJSON(t *testing.T) {
	if _, err := parseResponse("我觉得今天没什么重要的消息", nil); err == nil {
		t.Fatal("非 JSON 输出应报错，让上层走降级")
	}
}

func TestBuildPromptIncludesCandidatesAndEmptyInstruction(t *testing.T) {
	candidates := []Candidate{{
		Item:  newsItem("1", "摩根士丹利发布厄尔尼诺影响时间表", "预计影响时间至明年一季度"),
		Score: 52,
		Hints: []string{"权威来源：摩根士丹利"},
	}}
	prompt := buildPrompt(candidates)
	if !strings.Contains(prompt, "摩根士丹利") {
		t.Fatal("prompt 应包含候选标题")
	}
	if !strings.Contains(prompt, "权威来源") {
		t.Fatal("prompt 应包含规则线索")
	}
	// 核心诉求：必须明确允许空结果，否则模型会倾向于凑数。
	if !strings.Contains(prompt, "返回空数组") {
		t.Fatal("prompt 必须明确允许并鼓励返回空数组")
	}
	if !strings.Contains(prompt, "10 条") {
		t.Fatal("prompt 应说明上限")
	}
}

func TestBuildPromptTruncatesLongContent(t *testing.T) {
	long := strings.Repeat("测", 900)
	candidates := []Candidate{{Item: newsItem("1", "标题", long), Score: 40}}
	prompt := buildPrompt(candidates)
	if strings.Count(prompt, "测") > 500 {
		t.Fatalf("正文应被截断，实际长度 %d", strings.Count(prompt, "测"))
	}
}
