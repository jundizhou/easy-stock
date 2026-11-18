package review

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"easy-stock/backend/internal/agent"
)

// ZsxqSource is the review source id for 知识星球 (zsxq).
const ZsxqSource = "zsxq"

// zsxq-cli 由知识星球官方发布，凭据写入系统钥匙串，因此这里既不需要也不保存
// token。所有采集都经过官方 CLI，不抓页面、不伪造登录态。
const (
	zsxqPageLimit        = 30
	zsxqMaxPages         = 8
	zsxqMinContentRunes  = 8
	zsxqRequestTimeout   = 90 * time.Second
	zsxqInitialLookback  = 48 * time.Hour
	zsxqSyncOverlap      = 6 * time.Hour
	zsxqPermissionPhrase = "未开通 Skill 权限"
	// zsxqAuthTTL 是页面二维码的有效期。CLI 没有返回 expires_in，按 OAuth 设备码
	// 常见的 10 分钟处理；超过后让用户重新生成，而不是继续等一个必然失败的码。
	zsxqAuthTTL = 10 * time.Minute
)

// 设备授权会话状态。前端据此决定显示二维码、成功提示还是重新生成按钮。
const (
	ZsxqAuthPending    = "pending"
	ZsxqAuthAuthorized = "authorized"
	ZsxqAuthDenied     = "denied"
	ZsxqAuthExpired    = "expired"
	ZsxqAuthFailed     = "failed"
)

// ZsxqAuthSession 是一次设备授权流程的可展示快照。device_code 不放进结构体，
// 只留在后端内存里，页面脚本拿不到就不会去重复轮询或复用。
type ZsxqAuthSession struct {
	ID              string    `json:"id"`
	Status          string    `json:"status"`
	Message         string    `json:"message"`
	UserCode        string    `json:"user_code"`
	VerificationURL string    `json:"verification_url"`
	CreatedAt       time.Time `json:"created_at"`
	ExpiresAt       time.Time `json:"expires_at"`
}

var (
	// ErrZsxqCLIUnavailable 表示本机没有可执行的 zsxq-cli。
	ErrZsxqCLIUnavailable = errors.New("未找到知识星球采集程序：桌面端请重新安装完整安装包；独立运行后端时请配置 A_STOCK_ZSXQ_CLI")
	// ErrZsxqNotAuthorized 表示当前账号有成员身份，但星主未开通 Skill 权限。
	// 这是知识星球的服务端开关，客户端无法绕过，只能由星主在星球设置中申请开通。
	ErrZsxqNotAuthorized = errors.New("该星球尚未开通知识星球 Skill 权限，需要星主在星球设置中申请开通后才能同步")
	// ErrZsxqNotLoggedIn 表示本机还没有完成 zsxq-cli 的 OAuth 授权。
	ErrZsxqNotLoggedIn = errors.New("知识星球登录态不可用，请在终端执行 zsxq-cli auth login 完成授权")

	zsxqEntityPattern  = regexp.MustCompile(`(?is)<e\b([^>]*?)/?>`)
	zsxqAttrPattern    = regexp.MustCompile(`(?is)([a-zA-Z_][a-zA-Z0-9_:.-]*)\s*=\s*(?:"([^"]*)"|'([^']*)')`)
	zsxqGroupIDPattern = regexp.MustCompile(`\d{6,}`)
)

// ZsxqAuthStatus 是 zsxq-cli 的本地授权状态，不包含任何凭据。
type ZsxqAuthStatus struct {
	CLIInstalled bool   `json:"cli_installed"`
	LoggedIn     bool   `json:"logged_in"`
	UserName     string `json:"user_name,omitempty"`
	Message      string `json:"message"`
}

// ZsxqGroup 是一个已加入的星球及其可读状态。知识星球把内容读取权限做成
// 星球级开关，所以“已加入”不等于“可同步”，必须把两者分开呈现。
type ZsxqGroup struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type,omitempty"`
	Members  int    `json:"members,omitempty"`
	Owner    string `json:"owner,omitempty"`
	Readable bool   `json:"readable"`
	Status   string `json:"status"`
	Message  string `json:"message"`
}

type zsxqOwner struct {
	Alias  *string `json:"alias"`
	Name   string  `json:"name"`
	UserID string  `json:"user_id"`
}

func (o zsxqOwner) displayName() string {
	if o.Alias != nil {
		if alias := strings.TrimSpace(*o.Alias); alias != "" {
			return alias
		}
	}
	return strings.TrimSpace(o.Name)
}

type zsxqImage struct {
	Thumbnail struct {
		URL string `json:"url"`
	} `json:"thumbnail"`
	Large struct {
		URL string `json:"url"`
	} `json:"large"`
}

type zsxqTopic struct {
	TopicID    json.Number `json:"topic_id"`
	Type       string      `json:"type"`
	Title      string      `json:"title"`
	Content    string      `json:"content"`
	CreateTime string      `json:"create_time"`
	Owner      zsxqOwner   `json:"owner"`
	Images     []zsxqImage `json:"images"`
}

func (t zsxqTopic) id() string {
	return strings.TrimSpace(t.TopicID.String())
}

func (t zsxqTopic) coverURL() string {
	for _, image := range t.Images {
		if value := strings.TrimSpace(image.Large.URL); value != "" {
			return value
		}
		if value := strings.TrimSpace(image.Thumbnail.URL); value != "" {
			return value
		}
	}
	return ""
}

// zsxqTopicPage 对应 group +topics 的实际返回。官方文档写的是 topics，
// 实际字段是 topics_brief，按文档写会直接拿到空数组。
type zsxqTopicPage struct {
	Count       int         `json:"count"`
	HasMore     bool        `json:"has_more"`
	NextEndTime string      `json:"next_end_time"`
	Success     bool        `json:"success"`
	TopicsBrief []zsxqTopic `json:"topics_brief"`
}

type zsxqGroupList struct {
	Count  int `json:"count"`
	Groups []struct {
		GroupID    string    `json:"group_id"`
		Name       string    `json:"name"`
		Type       string    `json:"type"`
		Owner      zsxqOwner `json:"owner"`
		Statistics struct {
			Members struct {
				Count int `json:"count"`
			} `json:"members"`
		} `json:"statistics"`
	} `json:"groups"`
	Success bool `json:"success"`
}

// zsxqRunner 抽象 CLI 执行，便于测试注入固定输出。
type zsxqRunner func(ctx context.Context, path string, args ...string) ([]byte, error)

// ZsxqCollector 通过官方 zsxq-cli 采集知识星球主题。
type ZsxqCollector struct {
	cliPath string
	run     zsxqRunner
	// retryDelays 为 429/临时故障的重试间隔；测试可置空以免等待。
	retryDelays []time.Duration
}

func NewZsxqCollector(cliPath string) *ZsxqCollector {
	return &ZsxqCollector{
		cliPath:     strings.TrimSpace(cliPath),
		run:         runZsxqCLI,
		retryDelays: []time.Duration{2 * time.Second, 5 * time.Second},
	}
}

func (c *ZsxqCollector) cli() (string, error) {
	if c != nil && c.cliPath != "" {
		return c.cliPath, nil
	}
	return resolveZsxqCLIPath()
}

func (c *ZsxqCollector) runner() zsxqRunner {
	if c != nil && c.run != nil {
		return c.run
	}
	return runZsxqCLI
}

// AuthStatus 读取本机授权状态。CLI 缺失、未登录都通过返回值表达，不返回错误，
// 因为这两种情况都是可预期的用户状态，需要在界面上引导而不是报故障。
func (c *ZsxqCollector) AuthStatus(ctx context.Context) ZsxqAuthStatus {
	path, err := c.cli()
	if err != nil {
		return ZsxqAuthStatus{Message: err.Error()}
	}
	// 状态探测会拉起一个 node 进程，限制时长，避免拖住复盘页面的请求。
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	data, err := c.execute(ctx, path, "auth", "status", "--json")
	if err != nil {
		message := strings.ToLower(err.Error())
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) || strings.Contains(message, "deadline exceeded") || strings.Contains(message, "signal: killed") {
			// CLI 会把未完成的设备码落盘，此时 auth status 会自己恢复轮询并阻塞。
			// 这不是故障，给用户一个可行动的出口，而不是“稍后重试”。
			return ZsxqAuthStatus{CLIInstalled: true, Message: "知识星球有未完成的扫码授权，请在“设置 → 数据源 → 知识星球”重新生成二维码"}
		}
		return ZsxqAuthStatus{CLIInstalled: true, Message: "知识星球授权状态读取失败: " + err.Error()}
	}
	var payload struct {
		OK   bool `json:"ok"`
		Data struct {
			LoggedIn bool   `json:"loggedIn"`
			UserName string `json:"userName"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return ZsxqAuthStatus{CLIInstalled: true, Message: "zsxq-cli 授权状态返回格式无效: " + err.Error()}
	}
	if !payload.Data.LoggedIn {
		return ZsxqAuthStatus{CLIInstalled: true, Message: ErrZsxqNotLoggedIn.Error()}
	}
	return ZsxqAuthStatus{CLIInstalled: true, LoggedIn: true, UserName: strings.TrimSpace(payload.Data.UserName), Message: "zsxq-cli 授权有效，可读取星主已开通权限的星球"}
}

// StartAuth 拉起一次设备授权，返回页面要展示的确认码与授权链接。
// CLI 的 --json 输出前面还有中文横幅，不是纯 JSON，必须先把对象截出来。
func (c *ZsxqCollector) StartAuth(ctx context.Context) (ZsxqAuthSession, string, error) {
	path, err := c.cli()
	if err != nil {
		return ZsxqAuthSession{}, "", err
	}
	data, err := c.execute(ctx, path, "auth", "login", "--no-browser", "--no-wait", "--json")
	if err != nil {
		return ZsxqAuthSession{}, "", err
	}
	var payload struct {
		OK   bool `json:"ok"`
		Data struct {
			DeviceCode              string `json:"device_code"`
			UserCode                string `json:"user_code"`
			VerificationURIComplete string `json:"verification_uri_complete"`
		} `json:"data"`
	}
	if err := json.Unmarshal(extractZsxqJSONObject(data), &payload); err != nil {
		return ZsxqAuthSession{}, "", fmt.Errorf("zsxq-cli 授权返回格式无效: %w", err)
	}
	deviceCode := strings.TrimSpace(payload.Data.DeviceCode)
	verificationURL := strings.TrimSpace(payload.Data.VerificationURIComplete)
	if deviceCode == "" || verificationURL == "" {
		return ZsxqAuthSession{}, "", errors.New("zsxq-cli 没有返回可用的授权链接，请升级 CLI 后重试")
	}
	now := time.Now()
	return ZsxqAuthSession{
		ID:              stableID("zsxq-auth\n" + deviceCode)[:16],
		Status:          ZsxqAuthPending,
		Message:         "等待手机扫码授权",
		UserCode:        strings.TrimSpace(payload.Data.UserCode),
		VerificationURL: verificationURL,
		CreatedAt:       now,
		ExpiresAt:       now.Add(zsxqAuthTTL),
	}, deviceCode, nil
}

// WaitAuth 阻塞到用户完成授权、拒绝或设备码过期。CLI 自己负责轮询节奏，
// 这里只做状态归一化，避免前端反复拉起进程。
func (c *ZsxqCollector) WaitAuth(ctx context.Context, deviceCode string) (string, string) {
	path, err := c.cli()
	if err != nil {
		return ZsxqAuthFailed, err.Error()
	}
	if deviceCode == "" {
		return ZsxqAuthFailed, "授权设备码为空，请重新生成二维码"
	}
	ctx, cancel := context.WithTimeout(ctx, zsxqAuthTTL)
	defer cancel()
	if _, err := c.execute(ctx, path, "auth", "login", "--device-code", deviceCode, "--json"); err == nil {
		return ZsxqAuthAuthorized, "授权成功，可以同步星主已开通权限的星球"
	} else if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return ZsxqAuthExpired, "授权码已过期，请重新生成二维码"
	} else {
		text := strings.ToLower(err.Error())
		switch {
		case strings.Contains(text, "access_denied"):
			return ZsxqAuthDenied, "已在手机上拒绝授权，可重新生成二维码再试"
		case strings.Contains(text, "expired") || strings.Contains(text, "过期"):
			return ZsxqAuthExpired, "授权码已过期，请重新生成二维码"
		case strings.Contains(text, "deadline exceeded") || strings.Contains(text, "signal: killed"):
			// runZsxqCLI 把本地超时压成了普通 error，这里按过期处理。
			return ZsxqAuthExpired, "授权码已过期，请重新生成二维码"
		case strings.Contains(text, "没有返回内容"):
			// CLI 偶发只更新本地状态、不打印结果；不做乐观判断，提示用户刷新。
			return ZsxqAuthFailed, "授权结果未能确认，请刷新状态或重新生成二维码"
		default:
			return ZsxqAuthFailed, "授权失败：" + err.Error()
		}
	}
}

// extractZsxqJSONObject 从混合输出里截出第一个完整 JSON 对象。
// zsxq-cli 在 --json 前后都会打印中文提示，直接 Unmarshal 整段会失败。
func extractZsxqJSONObject(raw []byte) []byte {
	start := bytes.IndexByte(raw, '{')
	if start < 0 {
		return raw
	}
	depth := 0
	inString := false
	escaped := false
	for index := start; index < len(raw); index++ {
		ch := raw[index]
		if inString {
			switch {
			case escaped:
				escaped = false
			case ch == '\\':
				escaped = true
			case ch == '"':
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return raw[start : index+1]
			}
		}
	}
	return raw[start:]
}

// Groups 列出当前账号加入的星球，并逐个探测主题接口是否可用。
func (c *ZsxqCollector) Groups(ctx context.Context) ([]ZsxqGroup, error) {
	path, err := c.cli()
	if err != nil {
		return nil, err
	}
	groups, err := c.listGroups(ctx, path)
	if err != nil {
		return nil, err
	}
	for index := range groups {
		readable, probeErr := c.probe(ctx, path, groups[index].ID)
		groups[index].Readable = readable
		switch {
		case probeErr == nil && readable:
			groups[index].Status = "ready"
			groups[index].Message = "可同步"
		case errors.Is(probeErr, ErrZsxqNotAuthorized):
			groups[index].Status = "limited"
			groups[index].Message = ErrZsxqNotAuthorized.Error()
		default:
			groups[index].Status = "limited"
			groups[index].Message = firstNonEmpty(errText(probeErr), "星球内容接口不可用")
		}
	}
	return groups, nil
}

// Lookup 只针对一个星球做「取元信息 + 探测可读性」，比拉全量清单便宜。
func (c *ZsxqCollector) Lookup(ctx context.Context, groupID string) (ZsxqGroup, error) {
	path, err := c.cli()
	if err != nil {
		return ZsxqGroup{}, err
	}
	group := ZsxqGroup{ID: strings.TrimSpace(groupID)}
	if groups, listErr := c.listGroups(ctx, path); listErr == nil {
		for _, item := range groups {
			if item.ID == group.ID {
				group = item
				break
			}
		}
	}
	readable, probeErr := c.probe(ctx, path, group.ID)
	group.Readable = readable
	if probeErr != nil {
		group.Status = "limited"
		group.Message = probeErr.Error()
		if errors.Is(probeErr, ErrZsxqNotAuthorized) {
			group.Message = ErrZsxqNotAuthorized.Error()
		}
		return group, probeErr
	}
	group.Status = "ready"
	group.Message = "可同步"
	return group, nil
}

func (c *ZsxqCollector) listGroups(ctx context.Context, path string) ([]ZsxqGroup, error) {
	data, err := c.executeWithRetry(ctx, path, "group", "+list", "--json")
	if err != nil {
		return nil, err
	}
	var payload zsxqGroupList
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("zsxq-cli 星球清单返回格式无效: %w", err)
	}
	groups := make([]ZsxqGroup, 0, len(payload.Groups))
	for _, item := range payload.Groups {
		groups = append(groups, ZsxqGroup{
			ID:      strings.TrimSpace(item.GroupID),
			Name:    strings.TrimSpace(item.Name),
			Type:    strings.TrimSpace(item.Type),
			Members: item.Statistics.Members.Count,
			Owner:   item.Owner.displayName(),
		})
	}
	return groups, nil
}

func (c *ZsxqCollector) probe(ctx context.Context, path, groupID string) (bool, error) {
	if strings.TrimSpace(groupID) == "" {
		return false, errors.New("缺少星球 group_id")
	}
	_, err := c.executeWithRetry(ctx, path, "group", "+topics", "--group-id", groupID, "--limit", "1", "--json")
	if err == nil {
		return true, nil
	}
	return false, err
}

// Collect 拉取 since 之后（含 since 当天）的主题。主题按时间倒序返回，
// 一旦出现早于 since 的主题就可以停止翻页。
func (c *ZsxqCollector) Collect(ctx context.Context, groupID string, since time.Time) ([]Post, error) {
	path, err := c.cli()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(groupID) == "" {
		return nil, errors.New("缺少星球 group_id")
	}
	posts := make([]Post, 0, zsxqPageLimit)
	seen := map[string]bool{}
	cursor := ""
	for page := 0; page < zsxqMaxPages; page++ {
		args := []string{"group", "+topics", "--group-id", groupID, "--limit", strconv.Itoa(zsxqPageLimit), "--json"}
		if cursor != "" {
			args = append(args, "--end-time", cursor)
		}
		data, err := c.executeWithRetry(ctx, path, args...)
		if err != nil {
			return posts, fmt.Errorf("知识星球第 %d 页采集失败: %w", page+1, err)
		}
		var payload zsxqTopicPage
		if err := json.Unmarshal(data, &payload); err != nil {
			return posts, fmt.Errorf("zsxq-cli 第 %d 页主题列表返回格式无效: %w", page+1, err)
		}
		reachedOlder := false
		for _, topic := range payload.TopicsBrief {
			id := topic.id()
			// 游标是“含等于”的时间点，下一页首条必然是上一页末条，
			// 必须按 topic_id 去重，否则会重复导入。
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			publishedAt, ok := parseZsxqTime(topic.CreateTime)
			if !ok {
				continue
			}
			if !since.IsZero() && publishedAt.Before(since) {
				reachedOlder = true
				continue
			}
			if post, ok := zsxqTopicToPost(topic); ok {
				posts = append(posts, post)
			}
		}
		if reachedOlder || !payload.HasMore {
			return posts, nil
		}
		next := strings.TrimSpace(payload.NextEndTime)
		if next == "" || next == cursor {
			return posts, errors.New("知识星球分页游标缺失或未前进，本次同步未完成")
		}
		cursor = next
	}
	return posts, fmt.Errorf("知识星球本次已达 %d 页上限，尚未覆盖同步时间窗", zsxqMaxPages)
}

func zsxqTopicToPost(topic zsxqTopic) (Post, bool) {
	id := topic.id()
	if id == "" {
		return Post{}, false
	}
	publishedAt, ok := parseZsxqTime(topic.CreateTime)
	if !ok {
		return Post{}, false
	}
	content := zsxqContentToText(topic.Content)
	// 星球里有大量一句话点评（“还有东山精密。”），全量喂给作者归纳会把
	// 观点卡稀释掉，过短的碎片只保留在星球里，不进复盘素材。
	if len([]rune(content)) < zsxqMinContentRunes {
		return Post{}, false
	}
	author := topic.Owner.displayName()
	post := newPost(ZsxqSource, zsxqTopicURL(id), author, zsxqTopicTitle(content, topic.Title), content, topic.coverURL(), publishedAt)
	post.AuthorID = zsxqAuthorID(topic.Owner.UserID, author)
	post.Digest = truncateRunes(content, 180)
	return post, true
}

// zsxqAuthorID 让同一个星球里的不同发帖人各自成为独立作者。现有同步分支
// 会把整个订阅归一成一位作者，星球场景下必须按 owner 拆分。
func zsxqAuthorID(userID, name string) string {
	key := strings.TrimSpace(userID)
	if key == "" {
		key = "name:" + strings.TrimSpace(name)
	}
	return stableID(ZsxqSource + "\n" + key)[:24]
}

func zsxqTopicURL(topicID string) string {
	return "https://wx.zsxq.com/dweb2/index/topic_detail/" + strings.TrimSpace(topicID)
}

// zsxqContentToText 把“纯文本 + 内联 <e> 标签”转成可读纯文本。
// 这一步必须在 cleanDocument 之前做：cleanDocument 的 <[^>]+> 规则会把整个
// <e ... /> 连同标题一起剥掉，像“印发《<e type="web" title="十五五..."/>》”
// 这种嵌在句子中间的写法，会直接变成“印发《》”。
func zsxqContentToText(content string) string {
	if strings.TrimSpace(content) == "" {
		return ""
	}
	replaced := zsxqEntityPattern.ReplaceAllStringFunc(content, func(tag string) string {
		body := strings.TrimSuffix(strings.TrimPrefix(tag, "<e"), ">")
		body = strings.TrimSuffix(strings.TrimSpace(body), "/")
		attrs := zsxqTagAttributes(body)
		kind := strings.ToLower(strings.TrimSpace(attrs["type"]))
		title := zsxqDecodeAttribute(attrs["title"])
		href := zsxqDecodeAttribute(attrs["href"])
		switch kind {
		case "web", "link":
			if title != "" && href != "" {
				return title + " (" + href + ")"
			}
			return firstNonEmpty(title, href)
		case "hashtag":
			if title != "" {
				return "#" + strings.TrimPrefix(title, "#")
			}
			return ""
		case "mention":
			if title != "" {
				return "@" + strings.TrimPrefix(title, "@")
			}
			return ""
		default:
			// 未知类型不做特判，保留文案、丢掉标签，避免整段内容消失。
			return firstNonEmpty(title, href)
		}
	})
	return cleanDocument(replaced)
}

func zsxqTagAttributes(body string) map[string]string {
	attrs := map[string]string{}
	for _, match := range zsxqAttrPattern.FindAllStringSubmatch(body, -1) {
		value := match[2]
		if value == "" {
			value = match[3]
		}
		attrs[strings.ToLower(match[1])] = value
	}
	return attrs
}

func zsxqDecodeAttribute(value string) string {
	value = html.UnescapeString(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	// 这里的 href/title 是 URL 编码的（%E5%85%89...）。用 PathUnescape 而不是
	// QueryUnescape，避免把标题里正常的 "+" 变成空格。
	if decoded, err := url.PathUnescape(value); err == nil {
		return decoded
	}
	return value
}

// zsxqTopicTitle 还原被截断的标题。列表接口的 title 一定带省略号
// （“11.14市场研究汇总 （仅供...”），所以优先从正文首行取。首行可能是一句
// 免责声明，与标题完全无关，这时才退回截断值。
func zsxqTopicTitle(content, truncated string) string {
	firstLine := ""
	if content != "" {
		firstLine = strings.TrimSpace(strings.SplitN(content, "\n", 2)[0])
	}
	truncated = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(truncated), "..."))
	switch {
	case truncated != "" && strings.HasPrefix(firstLine, truncated):
		// 截断标题就是首行的前缀，说明两者同源，把首行的第一句补全。
		return zsxqTitleSentence(firstLine)
	case truncated != "":
		return truncated
	case firstLine != "":
		return zsxqTitleSentence(firstLine)
	default:
		return truncateRunes(content, 40)
	}
}

func zsxqTitleSentence(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	for index, char := range value {
		if char == '。' || char == '！' || char == '？' || char == '!' || char == '?' {
			if sentence := strings.TrimSpace(value[:index+len(string(char))]); len([]rune(sentence)) >= 6 {
				return sentence
			}
			break
		}
	}
	if runes := []rune(value); len(runes) > 40 {
		return truncateRunes(value, 40)
	}
	return value
}

func parseZsxqTime(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	// 实测格式 2026-11-16T15:07:28.925+0800，RFC3339 无法解析这种不带冒号的时区。
	for _, layout := range []string{"2006-01-02T15:04:05.000-0700", "2006-01-02T15:04:05-0700", time.RFC3339, "2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

// parseZsxqGroupInput 接受星球链接、dweb2 链接或裸 group_id。
func parseZsxqGroupInput(value string) (string, string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", "", errors.New("星球链接或 group_id 不能为空")
	}
	if isDigits(value) {
		return value, zsxqGroupURL(value), nil
	}
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
		return "", "", errors.New("请输入知识星球链接（如 https://wx.zsxq.com/group/123456789）或 group_id")
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "wx.zsxq.com" && host != "zsxq.com" && !strings.HasSuffix(host, ".zsxq.com") {
		return "", "", errors.New("请输入知识星球（wx.zsxq.com）的星球链接")
	}
	segments := strings.FieldsFunc(strings.Trim(parsed.Path, "/"), func(r rune) bool { return r == '/' })
	// 只接受星球首页链接；主题详情链接同样带一长串数字，误当 group_id 会得到
	// 一个永远拉不到内容的订阅。
	hasGroupSegment := false
	for _, segment := range segments {
		if strings.EqualFold(segment, "group") {
			hasGroupSegment = true
			break
		}
	}
	if !hasGroupSegment {
		return "", "", errors.New("请粘贴星球首页链接（形如 https://wx.zsxq.com/group/123456789）或 group_id")
	}
	for index := len(segments) - 1; index >= 0; index-- {
		if isDigits(segments[index]) {
			return segments[index], zsxqGroupURL(segments[index]), nil
		}
	}
	if match := zsxqGroupIDPattern.FindString(parsed.String()); match != "" {
		return match, zsxqGroupURL(match), nil
	}
	return "", "", errors.New("没有从链接中识别到星球 group_id，请直接粘贴星球首页链接")
}

func zsxqGroupURL(groupID string) string {
	return "https://wx.zsxq.com/group/" + strings.TrimSpace(groupID)
}

func zsxqGroupIDFromURL(value string) string {
	groupID, _, err := parseZsxqGroupInput(value)
	if err != nil {
		return ""
	}
	return groupID
}

func isDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func (c *ZsxqCollector) executeWithRetry(ctx context.Context, path string, args ...string) ([]byte, error) {
	delays := c.retryDelays
	if c == nil {
		delays = nil
	}
	var lastErr error
	for attempt := 0; ; attempt++ {
		data, err := c.execute(ctx, path, args...)
		if err == nil {
			return data, nil
		}
		lastErr = err
		if attempt >= len(delays) || !zsxqRetryable(err) {
			return nil, lastErr
		}
		// 429 是知识星球实际存在的限流，退避而不是立刻失败。
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delays[attempt]):
		}
	}
}

func (c *ZsxqCollector) execute(ctx context.Context, path string, args ...string) ([]byte, error) {
	out, err := c.runner()(ctx, path, args...)
	if err != nil {
		return nil, zsxqTranslateError(err)
	}
	if len(bytes.TrimSpace(out)) == 0 {
		return nil, errors.New("zsxq-cli 没有返回内容")
	}
	return out, nil
}

// zsxqTranslateError 把 CLI 的英文/半结构化错误翻译成用户能行动的提示。
// 注意 zsxq-cli 把权限拒绝写在 stderr，退出码 5，stdout 为空。
func zsxqTranslateError(err error) error {
	if err == nil {
		return nil
	}
	message := strings.TrimSpace(err.Error())
	switch {
	case strings.Contains(message, zsxqPermissionPhrase):
		return fmt.Errorf("%w（原始提示：%s）", ErrZsxqNotAuthorized, message)
	case strings.Contains(message, "登录") && (strings.Contains(message, "失效") || strings.Contains(message, "过期") || strings.Contains(message, "未登录")):
		return fmt.Errorf("%w（原始提示：%s）", ErrZsxqNotLoggedIn, message)
	default:
		return err
	}
}

func zsxqRetryable(err error) bool {
	message := strings.ToLower(err.Error())
	for _, marker := range []string{"429", "too many requests", "rate limit", "temporarily unavailable", "timeout", "timed out", "econnreset", "socket hang up"} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func resolveZsxqCLIPath() (string, error) {
	for _, key := range []string{"A_STOCK_ZSXQ_CLI", "ZSXQ_CLI_PATH"} {
		value := strings.TrimSpace(os.Getenv(key))
		if value == "" {
			continue
		}
		if info, err := os.Stat(value); err == nil && !info.IsDir() {
			return value, nil
		}
		if path, err := exec.LookPath(value); err == nil {
			return path, nil
		}
	}
	if path, err := exec.LookPath("zsxq-cli"); err == nil {
		return path, nil
	}
	// 桌面端启动的后端不一定继承 npm 全局 bin，这里补一轮常见路径。
	for _, candidate := range zsxqCLIFallbacks() {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", ErrZsxqCLIUnavailable
}

func zsxqCLIFallbacks() []string {
	names := []string{"zsxq-cli", "zsxq-cli.cmd", "zsxq-cli.exe"}
	directories := zsxqSearchDirs()
	paths := make([]string, 0, len(directories)*len(names))
	for _, directory := range directories {
		for _, name := range names {
			paths = append(paths, filepath.Join(directory, name))
		}
	}
	return paths
}

// zsxqSearchDirs 列出 npm 全局 bin 的常见位置。
// 桌面端从 Finder 启动时拿不到登录 shell 的 PATH，只能靠这份固定清单兜底。
func zsxqSearchDirs() []string {
	directories := []string{"/usr/local/bin", "/opt/homebrew/bin"}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		directories = append(directories,
			filepath.Join(home, ".npm-global", "bin"),
			filepath.Join(home, ".local", "bin"),
			filepath.Join(home, ".volta", "bin"),
			filepath.Join(home, "n", "bin"),
		)
		// nvm 把每个 node 版本装在自己的目录里，按版本目录名倒序让新版本优先。
		if matches, err := filepath.Glob(filepath.Join(home, ".nvm", "versions", "node", "*", "bin")); err == nil && len(matches) > 0 {
			sort.Sort(sort.Reverse(sort.StringSlice(matches)))
			directories = append(directories, matches...)
		}
	}
	if runtime.GOOS == "windows" {
		if appData := strings.TrimSpace(os.Getenv("APPDATA")); appData != "" {
			directories = append(directories, filepath.Join(appData, "npm"))
		}
		if localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); localAppData != "" {
			directories = append(directories, filepath.Join(localAppData, "Programs", "nodejs"))
		}
	}
	return directories
}

// zsxqProcessEnv 给 CLI 子进程补上 node 所在目录。
//
// npm 全局安装的 zsxq-cli 是 `#!/usr/bin/env node` 的脚本，从 Finder 启动的
// 桌面应用 PATH 里只有系统目录。这种情况下即使 resolveZsxqCLIPath 通过固定
// 目录找到了 CLI，执行时仍会因为找不到 node 直接失败（env: node: No such
// file or directory），表现成"CLI 装了但不可用"。返回 nil 表示无需改动环境。
func zsxqProcessEnv(cliPath string) []string {
	if _, err := exec.LookPath("node"); err == nil {
		// 当前 PATH 已能找到 node，子进程直接继承即可。
		return nil
	}
	dirs := zsxqNodeDirs(cliPath)
	if len(dirs) == 0 {
		return nil
	}
	return zsxqPrependPath(os.Environ(), dirs)
}

// zsxqNodeDirs 找出含 node 可执行文件的目录。优先 CLI 自己所在的目录，
// 因为 npm 全局 bin 通常和 node 装在同一处（brew、nvm、volta 都是这样）。
func zsxqNodeDirs(cliPath string) []string {
	if strings.TrimSpace(cliPath) != "" {
		if directory := filepath.Dir(cliPath); zsxqHasNode(directory) {
			return []string{directory}
		}
	}
	for _, directory := range zsxqSearchDirs() {
		if zsxqHasNode(directory) {
			return []string{directory}
		}
	}
	return nil
}

func zsxqHasNode(directory string) bool {
	for _, name := range []string{"node", "node.exe"} {
		if info, err := os.Stat(filepath.Join(directory, name)); err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}

// zsxqPrependPath 把 dirs 前置到 env 的 PATH，保留其余变量。
// Windows 上 PATH 的大小写不固定，按 key 做大小写无关匹配。
func zsxqPrependPath(env []string, dirs []string) []string {
	if len(dirs) == 0 {
		return env
	}
	prefix := strings.Join(dirs, string(os.PathListSeparator))
	out := make([]string, 0, len(env)+1)
	replaced := false
	for _, item := range env {
		key, value, ok := strings.Cut(item, "=")
		if ok && strings.EqualFold(key, "PATH") {
			if value != "" {
				out = append(out, key+"="+prefix+string(os.PathListSeparator)+value)
			} else {
				out = append(out, key+"="+prefix)
			}
			replaced = true
			continue
		}
		out = append(out, item)
	}
	if !replaced {
		out = append(out, "PATH="+prefix)
	}
	return out
}

func runZsxqCLI(ctx context.Context, path string, args ...string) ([]byte, error) {
	timeout := zsxqRequestTimeout
	// Only device polling waits for a person; collection still has a 90s limit
	// even when its scheduler context has a longer deadline.
	if len(args) >= 2 && args[0] == "auth" && args[1] == "login" && slices.Contains(args[2:], "--device-code") {
		timeout = zsxqAuthTTL
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	command := zsxqCommand(ctx, path, args...)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return nil, errors.New(message)
	}
	if stdout.Len() == 0 {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			return nil, errors.New("zsxq-cli 没有返回内容")
		}
		return nil, errors.New(message)
	}
	return stdout.Bytes(), nil
}

func zsxqCommand(ctx context.Context, path string, args ...string) *exec.Cmd {
	var command *exec.Cmd
	// Windows 上 npm 安装的是 .cmd 包装脚本，必须经由 cmd.exe 启动。
	if runtime.GOOS == "windows" {
		lower := strings.ToLower(path)
		if strings.HasSuffix(lower, ".cmd") || strings.HasSuffix(lower, ".bat") {
			command = exec.CommandContext(ctx, "cmd", append([]string{"/c", path}, args...)...)
		}
	}
	if command == nil {
		command = exec.CommandContext(ctx, path, args...)
	}
	// CLI 的 shebang 是 `#!/usr/bin/env node`，需要保证 node 也在 PATH 里。
	if env := zsxqProcessEnv(path); env != nil {
		command.Env = env
	}
	// The npm entry point spawns a native CLI. Cancel both processes so its
	// inherited output pipes cannot keep Run waiting after authorization ends.
	agent.IsolateProcess(command)
	command.WaitDelay = time.Second
	return command
}
