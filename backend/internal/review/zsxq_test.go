package review

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestZsxqContentToTextKeepsInlineLinkTitle(t *testing.T) {
	// 真实样本：链接标签嵌在句子中间，直接走 cleanDocument 会变成“印发《》”。
	content := `第五：印发《<e type="web" href="https%3A%2F%2Fexample.com%2Fa" title="%E5%8D%81%E4%BA%94%E4%BA%94%E6%96%B0%E5%9E%8B%E5%82%A8%E8%83%BD%E5%AE%9E%E6%96%BD%E6%96%B9%E6%A1%88" style="book" />》，到2030年，装机300GW。`
	got := zsxqContentToText(content)
	if !strings.Contains(got, "十五五新型储能实施方案") {
		t.Fatalf("链接标题被丢掉: %q", got)
	}
	if !strings.Contains(got, "https://example.com/a") {
		t.Fatalf("链接地址未解码: %q", got)
	}
	if !strings.HasPrefix(got, "第五：印发《十五五新型储能实施方案") {
		t.Fatalf("句子结构被破坏: %q", got)
	}
}

func TestZsxqContentToTextHandlesHashtagAndUnknownTypes(t *testing.T) {
	content := "关注 <e type=\"hashtag\" hid=\"1\" title=\"%23%E5%85%89%E6%A8%A1%E5%9D%97\" /> 和 <e type=\"mention\" uid=\"2\" title=\"%40%E4%B8%81%E4%B8%81\" />，另见 <e type=\"unknown\" title=\"%E5%A4%87%E6%B3%A8\" />"
	got := zsxqContentToText(content)
	for _, want := range []string{"#光模块", "@丁丁", "备注"} {
		if !strings.Contains(got, want) {
			t.Fatalf("缺少 %q: %q", want, got)
		}
	}
	if strings.Contains(got, "<e") {
		t.Fatalf("仍残留标签: %q", got)
	}
}

func TestZsxqContentToTextKeepsPlusSign(t *testing.T) {
	// PathUnescape 不会把 "+" 变成空格，QueryUnescape 会。
	got := zsxqContentToText(`会议 <e type="web" href="https%3A%2F%2Fx.cn" title="GopherChina+2019" />`)
	if !strings.Contains(got, "GopherChina+2019") {
		t.Fatalf("加号被改写: %q", got)
	}
}

func TestZsxqTopicTitlePrefersFirstLineAndFallsBack(t *testing.T) {
	cases := []struct {
		name      string
		content   string
		truncated string
		want      string
	}{
		{name: "首行即完整标题", content: "11.14 下周策略（个人分享，仅供参考）\n后面是正文", truncated: "11.14 下周策略（个...", want: "11.14 下周策略（个人分享，仅供参考）"},
		{name: "首行是免责声明", content: "以下信息，仅供参考。\n中际旭创和东山精密有H股", truncated: "中际旭创和东山精...", want: "中际旭创和东山精"},
		{name: "首行是一整句", content: "还有东山精密。\n今天创业板跌太多", truncated: "还有东山精密", want: "还有东山精密。"},
		{name: "截断标题被补全到句末", content: "美国要出台算力出口措施。那么很多人会觉得光模块要完。\n后面是正文", truncated: "美国要出台算力出口措施。那么很", want: "美国要出台算力出口措施。"},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			if got := zsxqTopicTitle(item.content, item.truncated); got != item.want {
				t.Fatalf("got %q want %q", got, item.want)
			}
		})
	}
}

func TestParseZsxqTimeAcceptsOffsetWithoutColon(t *testing.T) {
	parsed, ok := parseZsxqTime("2026-11-16T15:07:28.925+0800")
	if !ok {
		t.Fatal("未能解析实测时间格式")
	}
	if parsed.UTC().Format(time.RFC3339) != "2026-11-16T07:07:28Z" {
		t.Fatalf("时区换算错误: %s", parsed.UTC().Format(time.RFC3339))
	}
}

func TestParseZsxqGroupInput(t *testing.T) {
	cases := map[string]string{
		"225418445851":                                       "225418445851",
		"https://wx.zsxq.com/group/225418445851":             "225418445851",
		"https://wx.zsxq.com/dweb2/index/group/225418445851": "225418445851",
	}
	for input, want := range cases {
		got, canonical, err := parseZsxqGroupInput(input)
		if err != nil {
			t.Fatalf("%s: %v", input, err)
		}
		if got != want {
			t.Fatalf("%s: got %q want %q", input, got, want)
		}
		if canonical != "https://wx.zsxq.com/group/"+want {
			t.Fatalf("%s: canonical %q", input, canonical)
		}
	}
	if _, _, err := parseZsxqGroupInput("https://xueqiu.com/1234"); err == nil {
		t.Fatal("应拒绝非知识星球链接")
	}
	if _, _, err := parseZsxqGroupInput("https://wx.zsxq.com/dweb2/index/topic_detail/14425145288814442"); err == nil {
		t.Fatal("应拒绝主题详情链接，避免把 topic_id 当成 group_id")
	}
}

func TestZsxqAuthorIDSplitsAuthorsByUserID(t *testing.T) {
	first := zsxqAuthorID("28221528458551", "波段之王")
	second := zsxqAuthorID("28221528211181", "丁丁船长")
	if first == second {
		t.Fatal("同一星球的不同发帖人必须是不同作者")
	}
	if first != zsxqAuthorID("28221528458551", "改名了") {
		t.Fatal("同一 user_id 改名后应保持同一作者")
	}
}

func TestZsxqCollectDedupesPageBoundaryAndStopsAtSince(t *testing.T) {
	page1 := `{"count":2,"has_more":true,"next_end_time":"2026-11-14T09:00:00.000+0800","success":true,"topics_brief":[
		{"topic_id":3,"type":"talk","title":"今天创业板跌太多...","content":"今天创业板跌得太多了。主要是由于光模块。","create_time":"2026-11-14T10:00:00.000+0800","owner":{"alias":"波段之王","name":"东方之龙","user_id":"28221528458551"}},
		{"topic_id":2,"type":"talk","title":"中际旭创...","content":"中际旭创这个股票，港股价格高于A股10%左右。","create_time":"2026-11-14T09:00:00.000+0800","owner":{"alias":null,"name":"柴树杉","user_id":"999"}}
	]}`
	page2 := `{"count":2,"has_more":true,"next_end_time":"2026-11-13T20:00:00.000+0800","success":true,"topics_brief":[
		{"topic_id":2,"type":"talk","title":"中际旭创...","content":"中际旭创这个股票，港股价格高于A股10%左右。","create_time":"2026-11-14T09:00:00.000+0800","owner":{"alias":null,"name":"柴树杉","user_id":"999"}},
		{"topic_id":1,"type":"talk","title":"旧主题...","content":"这条早于窗口，不应导入。","create_time":"2026-11-13T20:00:00.000+0800","owner":{"alias":null,"name":"柴树杉","user_id":"999"}}
	]}`
	var calls [][]string
	collector := NewZsxqCollector("fake-zsxq-cli")
	collector.retryDelays = nil
	collector.run = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		calls = append(calls, args)
		if len(calls) == 1 {
			return []byte(page1), nil
		}
		return []byte(page2), nil
	}
	since, _ := parseZsxqTime("2026-11-14T00:00:00.000+0800")
	posts, err := collector.Collect(context.Background(), "225418445851", since)
	if err != nil {
		t.Fatalf("采集失败: %v", err)
	}
	if len(posts) != 2 {
		t.Fatalf("应导入 2 条（去重 + 过滤窗口外），实际 %d", len(posts))
	}
	if len(calls) != 2 {
		t.Fatalf("应翻 2 页，实际 %d", len(calls))
	}
	if !strings.Contains(strings.Join(calls[0], " "), "--group-id 225418445851") {
		t.Fatalf("首屏参数错误: %v", calls[0])
	}
	second := strings.Join(calls[1], " ")
	if !strings.Contains(second, "--end-time 2026-11-14T09:00:00.000+0800") {
		t.Fatalf("第二页未带游标: %v", calls[1])
	}
	if posts[1].AuthorName != "柴树杉" {
		t.Fatalf("alias 为 null 时应回退 name，实际 %q", posts[1].AuthorName)
	}
	if posts[0].AuthorName != "波段之王" {
		t.Fatalf("应优先使用 alias，实际 %q", posts[0].AuthorName)
	}
	if posts[0].OriginalURL != "https://wx.zsxq.com/dweb2/index/topic_detail/3" {
		t.Fatalf("原文链接需由 topic_id 构造: %q", posts[0].OriginalURL)
	}
}

func TestZsxqCollectSkipsFragmentsAndReportsPermission(t *testing.T) {
	collector := NewZsxqCollector("fake-zsxq-cli")
	collector.retryDelays = nil
	collector.run = func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte(`{"count":2,"has_more":false,"success":true,"topics_brief":[
			{"topic_id":7,"content":"还有东山精密。","create_time":"2026-11-14T10:00:00.000+0800","owner":{"alias":"波段之王","name":"东方之龙","user_id":"1"}},
			{"topic_id":8,"content":"现在股市在低位，大家有没有发现身边讨论股票的人明显变多了，这通常是情绪修复的早期信号。","create_time":"2026-11-14T09:00:00.000+0800","owner":{"alias":"波段之王","name":"东方之龙","user_id":"1"}}
		]}`), nil
	}
	posts, err := collector.Collect(context.Background(), "225418445851", time.Time{})
	if err != nil {
		t.Fatalf("采集失败: %v", err)
	}
	if len(posts) != 1 {
		t.Fatalf("过短碎片应被跳过，实际 %d 条", len(posts))
	}

	denied := NewZsxqCollector("fake-zsxq-cli")
	denied.retryDelays = nil
	denied.run = func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return nil, errors.New("error: get_group_topics: MCP tool error: 该星球暂未开通 Skill 权限，如需使用，可联系星主申请开通。")
	}
	_, err = denied.Collect(context.Background(), "881251425252", time.Time{})
	if !errors.Is(err, ErrZsxqNotAuthorized) {
		t.Fatalf("权限拒绝应映射为 ErrZsxqNotAuthorized，实际 %v", err)
	}
}

func TestZsxqCollectRetriesRateLimit(t *testing.T) {
	attempts := 0
	collector := NewZsxqCollector("fake-zsxq-cli")
	collector.retryDelays = []time.Duration{time.Millisecond}
	collector.run = func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		attempts++
		if attempts == 1 {
			return nil, errors.New("HTTP 429 Too Many Requests")
		}
		return []byte(`{"count":0,"has_more":false,"success":true,"topics_brief":[]}`), nil
	}
	if _, err := collector.Collect(context.Background(), "225418445851", time.Time{}); err != nil {
		t.Fatalf("429 应退避重试成功: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("应重试一次，实际调用 %d 次", attempts)
	}
}

func TestZsxqCollectReportsLaterPageFailures(t *testing.T) {
	for _, failure := range []string{"cli", "json"} {
		t.Run(failure, func(t *testing.T) {
			collector := NewZsxqCollector("fake-zsxq-cli")
			collector.retryDelays = nil
			calls := 0
			collector.run = func(context.Context, string, ...string) ([]byte, error) {
				calls++
				if calls == 1 {
					return zsxqSyncTestPage(t, true, "cursor", zsxqSyncTestTopic("1", "111", time.Now())), nil
				}
				if failure == "json" {
					return []byte(`{invalid`), nil
				}
				return nil, errors.New("HTTP 429 Too Many Requests")
			}
			posts, err := collector.Collect(context.Background(), "12345678", time.Time{})
			if err == nil || !strings.Contains(err.Error(), "第 2 页") || len(posts) != 1 || calls != 2 {
				t.Fatalf("partial page failure: posts=%d calls=%d err=%v", len(posts), calls, err)
			}
		})
	}
}

func TestZsxqCollectReportsIncompletePagination(t *testing.T) {
	for _, scenario := range []struct {
		name    string
		calls   int
		message string
	}{
		{name: "missing cursor", calls: 1, message: "游标"},
		{name: "repeated cursor", calls: 2, message: "游标"},
		{name: "page limit", calls: zsxqMaxPages, message: "页上限"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			collector := NewZsxqCollector("fake-zsxq-cli")
			calls := 0
			collector.run = func(context.Context, string, ...string) ([]byte, error) {
				calls++
				cursor := ""
				if scenario.name == "repeated cursor" {
					cursor = "cursor"
				} else if scenario.name == "page limit" {
					cursor = fmt.Sprintf("cursor-%d", calls)
				}
				return zsxqSyncTestPage(t, true, cursor, zsxqSyncTestTopic("1", "111", time.Now())), nil
			}
			posts, err := collector.Collect(context.Background(), "12345678", time.Time{})
			if err == nil || !strings.Contains(err.Error(), scenario.message) || len(posts) != 1 || calls != scenario.calls {
				t.Fatalf("incomplete pagination: posts=%d calls=%d err=%v", len(posts), calls, err)
			}
		})
	}
}

func TestZsxqAuthStatusReportsMissingAndLoggedIn(t *testing.T) {
	loggedIn := NewZsxqCollector("fake-zsxq-cli")
	loggedIn.run = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if strings.Join(args, " ") != "auth status --json" {
			return nil, fmt.Errorf("unexpected args %v", args)
		}
		return []byte(`{"ok":true,"data":{"loggedIn":true,"userId":"1","userName":"cygnushan"}}`), nil
	}
	status := loggedIn.AuthStatus(context.Background())
	if !status.LoggedIn || status.UserName != "cygnushan" {
		t.Fatalf("授权状态解析错误: %+v", status)
	}

	loggedOut := NewZsxqCollector("fake-zsxq-cli")
	loggedOut.run = func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte(`{"ok":true,"data":{"loggedIn":false}}`), nil
	}
	status = loggedOut.AuthStatus(context.Background())
	if status.LoggedIn || status.Message == "" {
		t.Fatalf("未登录应给出提示: %+v", status)
	}
}

func TestExtractZsxqJSONObjectSkipsBanner(t *testing.T) {
	// 实测：--json 前后都有中文提示，整段 Unmarshal 会失败。
	raw := []byte("┌─────────────────────────┐\n│      知识星球 授权登录      │\n└─────────────────────────┘\n\n请访问以下链接完成授权：\n\n  →  https://garden.zsxq.com/jasmine/index.html?code=KPSW-FVRT\n\n{\n  \"ok\": true,\n  \"data\": {\n    \"device_code\": \"8440a82c\",\n    \"user_code\": \"KPSW-FVRT\",\n    \"verification_uri_complete\": \"https://garden.zsxq.com/jasmine/index.html?code=KPSW-FVRT\"\n  }\n}\nAfter completing authorization, run one of:\n  zsxq-cli auth status\n")
	trimmed := extractZsxqJSONObject(raw)
	var payload struct {
		OK   bool `json:"ok"`
		Data struct {
			DeviceCode string `json:"device_code"`
			UserCode   string `json:"user_code"`
		} `json:"data"`
	}
	if err := json.Unmarshal(trimmed, &payload); err != nil {
		t.Fatalf("截取后仍不是合法 JSON: %v\n%s", err, trimmed)
	}
	if !payload.OK || payload.Data.DeviceCode != "8440a82c" || payload.Data.UserCode != "KPSW-FVRT" {
		t.Fatalf("解析结果错误: %+v", payload)
	}
}

func TestZsxqStartAuthBuildsSession(t *testing.T) {
	collector := NewZsxqCollector("fake-zsxq-cli")
	collector.run = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if strings.Join(args, " ") != "auth login --no-browser --no-wait --json" {
			return nil, fmt.Errorf("unexpected args %v", args)
		}
		return []byte("知识星球 授权登录\n{\"ok\":true,\"data\":{\"device_code\":\"dev-1\",\"user_code\":\"KPSW-FVRT\",\"verification_uri_complete\":\"https://garden.zsxq.com/jasmine/index.html?code=KPSW-FVRT\"}}\nrun zsxq-cli auth status"), nil
	}
	session, deviceCode, err := collector.StartAuth(context.Background())
	if err != nil {
		t.Fatalf("生成授权失败: %v", err)
	}
	if deviceCode != "dev-1" {
		t.Fatalf("device code 应留给后端轮询: %q", deviceCode)
	}
	if session.Status != ZsxqAuthPending || session.UserCode != "KPSW-FVRT" {
		t.Fatalf("会话字段错误: %+v", session)
	}
	if session.ID == "" || strings.Contains(session.ID, "dev-1") {
		t.Fatalf("会话 ID 不应直接暴露 device code: %q", session.ID)
	}
	if !strings.Contains(session.VerificationURL, "code=KPSW-FVRT") {
		t.Fatalf("二维码内容应为完整授权链接: %q", session.VerificationURL)
	}
	if remaining := time.Until(session.ExpiresAt); remaining < 9*time.Minute || remaining > 11*time.Minute {
		t.Fatalf("有效期应约 10 分钟，实际 %v", remaining)
	}
}

func TestZsxqWaitAuthMapsOutcomes(t *testing.T) {
	success := NewZsxqCollector("fake-zsxq-cli")
	success.run = func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte(`{"ok":true,"data":{"loggedIn":true}}`), nil
	}
	if status, _ := success.WaitAuth(context.Background(), "dev-1"); status != ZsxqAuthAuthorized {
		t.Fatalf("成功应映射为 authorized，实际 %q", status)
	}

	cases := []struct {
		name string
		err  error
		want string
	}{
		{name: "拒绝", err: errors.New("oauth: access_denied"), want: ZsxqAuthDenied},
		{name: "过期", err: errors.New("device code expired"), want: ZsxqAuthExpired},
		{name: "本地超时", err: errors.New("signal: killed"), want: ZsxqAuthExpired},
		{name: "其他失败", err: errors.New("network unreachable"), want: ZsxqAuthFailed},
	}
	for _, tc := range cases {
		collector := NewZsxqCollector("fake-zsxq-cli")
		collector.run = func(_ context.Context, _ string, _ ...string) ([]byte, error) { return nil, tc.err }
		status, message := collector.WaitAuth(context.Background(), "dev-1")
		if status != tc.want {
			t.Fatalf("%s: 期望 %q，实际 %q（%s）", tc.name, tc.want, status, message)
		}
		if message == "" {
			t.Fatalf("%s: 应给出可读提示", tc.name)
		}
	}
}

func TestAutomationZsxqAuthSessionStaysPendingAndIsIdempotent(t *testing.T) {
	collector := NewZsxqCollector("fake-zsxq-cli")
	collector.run = func(ctx context.Context, _ string, args ...string) ([]byte, error) {
		if strings.Contains(strings.Join(args, " "), "--device-code") {
			// 模拟 CLI 阻塞轮询；取消后返回，避免测试泄漏 goroutine。
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return []byte(`{"ok":true,"data":{"device_code":"dev-1","user_code":"KPSW-FVRT","verification_uri_complete":"https://garden.zsxq.com/x?code=KPSW-FVRT"}}`), nil
	}
	automation := NewAutomation(nil, nil, nil, nil)
	automation.SetZsxqCollector(collector)
	defer automation.CancelZsxqAuth()

	first, err := automation.StartZsxqAuth(context.Background())
	if err != nil {
		t.Fatalf("首次授权失败: %v", err)
	}
	second, err := automation.StartZsxqAuth(context.Background())
	if err != nil {
		t.Fatalf("重复授权失败: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("同一设备码应复用会话，避免重复申请：%q vs %q", first.ID, second.ID)
	}

	// pending 期间不能去探测 CLI，否则 auth status 会自己恢复轮询并阻塞页面。
	status := automation.ZsxqAuthStatus(context.Background())
	if status.LoggedIn || !strings.Contains(status.Message, "扫码") {
		t.Fatalf("pending 期间应返回等待扫码: %+v", status)
	}
	snapshot := automation.ZsxqAuthSessionSnapshot()
	if snapshot == nil || snapshot.Status != ZsxqAuthPending {
		t.Fatalf("快照错误: %+v", snapshot)
	}
}

func TestZsxqPrependPathOverridesExistingPath(t *testing.T) {
	// 桌面端从 Finder 启动时 PATH 只有系统目录，必须整体替换而不是追加到末尾，
	// 否则 /usr/bin/env node 仍然找不到 node。
	env := []string{"HOME=/Users/demo", "PATH=/usr/bin:/bin", "LANG=zh_CN.UTF-8"}
	got := zsxqPrependPath(env, []string{"/opt/homebrew/bin"})
	if len(got) != 3 {
		t.Fatalf("不应新增条目: %v", got)
	}
	if got[1] != "PATH=/opt/homebrew/bin:/usr/bin:/bin" {
		t.Fatalf("PATH 未被前置覆盖: %q", got[1])
	}
	if got[0] != "HOME=/Users/demo" || got[2] != "LANG=zh_CN.UTF-8" {
		t.Fatalf("其余变量被破坏: %v", got)
	}
}

func TestZsxqPrependPathAppendsWhenMissing(t *testing.T) {
	got := zsxqPrependPath([]string{"HOME=/Users/demo"}, []string{"/usr/local/bin"})
	if len(got) != 2 || got[1] != "PATH=/usr/local/bin" {
		t.Fatalf("缺少 PATH 时应追加: %v", got)
	}
}

func TestZsxqPrependPathHandlesWindowsPathKey(t *testing.T) {
	// Windows 的 PATH 变量名可能是 "Path"，大小写不敏感匹配，避免写出两份 PATH。
	got := zsxqPrependPath([]string{`Path=C:\Windows\system32`}, []string{`C:\node`})
	if len(got) != 1 {
		t.Fatalf("不应出现两个 PATH 条目: %v", got)
	}
	if !strings.Contains(got[0], `C:\node`) || !strings.Contains(got[0], `C:\Windows\system32`) {
		t.Fatalf("PATH 合并结果错误: %v", got)
	}
}

func TestZsxqPrependPathNoopWithoutDirs(t *testing.T) {
	env := []string{"PATH=/usr/bin"}
	if got := zsxqPrependPath(env, nil); len(got) != 1 || got[0] != "PATH=/usr/bin" {
		t.Fatalf("无候选目录不应改动环境: %v", got)
	}
}

func TestZsxqNodeDirsPrefersCLIDirectory(t *testing.T) {
	// npm 全局 bin 一般和 node 装在同一目录（brew / nvm / volta 都是如此）。
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "node"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("准备 node 失败: %v", err)
	}
	got := zsxqNodeDirs(filepath.Join(dir, "zsxq-cli"))
	if len(got) != 1 || got[0] != dir {
		t.Fatalf("应优先使用 CLI 所在目录: %v", got)
	}
}

func TestZsxqNodeDirsEmptyWhenNodeMissing(t *testing.T) {
	dir := t.TempDir()
	if got := zsxqNodeDirs(filepath.Join(dir, "zsxq-cli")); len(got) > 0 && got[0] == dir {
		t.Fatalf("目录里没有 node 时不应选中它: %v", got)
	}
}

func TestZsxqSearchDirsCoversNvmInstallations(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("无 home 目录")
	}
	// nvm 用户是最常见的“装了但桌面端找不到”的场景，路径清单必须覆盖。
	dirs := zsxqSearchDirs()
	joined := strings.Join(dirs, "\n")
	if !strings.Contains(joined, filepath.Join(home, ".npm-global", "bin")) {
		t.Fatalf("缺少 npm-global: %v", dirs)
	}
	for _, dir := range dirs {
		if strings.Contains(dir, ".nvm") && !strings.HasSuffix(dir, "bin") {
			t.Fatalf("nvm 路径形状错误: %q", dir)
		}
	}
}
