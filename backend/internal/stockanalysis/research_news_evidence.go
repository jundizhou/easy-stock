package stockanalysis

import (
	"net/url"
	"strings"
	"time"
)

// Shared by stock research, portfolio inspection and allocation. Provenance
// makes a report assessable; it does not certify the article's interpretation.
const NewsEvidencePolicy = `新闻按具体陈述评估：来源可追溯且内容明确的可信媒体报道可支持其转述的事实，不因news类型、摘要形式或未取得公告原文自动降级。先核对媒体、链接、发布时间、公司、年份/报告期、指标口径及限定词；traceable仅表示出处与内容可检查，不代表真实性认证。观点、传闻、预测及不完整或冲突内容仍须注明具体疑点；转载同稿不算独立交叉验证。
区分“公司披露的业绩区间”“未来经营展望”“消息解释股价上涨”，分别判断，不把已结束报告期的初步核算当未来经营预测。fact表示来源明确陈述的事实，保留“据某媒体报道/公司预计”等归属与限定；推断用inference，观点用opinion。
缺公告正文、抓取失败或来源未直达只列资料限制/待补证，不得单独写成反证、真实性存疑、证伪风险或减仓理由；真实数字冲突、公司否认、更正及经营恶化须保留并引用。兑现、估值、波动和集中度风险分别评价，不能用资料缺口冒充。历史研究中的同类缺口也按此规则重新区分，不把旧负面措辞当新事实。
` + EarningsDisclosurePolicy

// Strip only provider-generated wrappers. Empty/title-only search hits must
// not become body evidence merely because the transport added a disclaimer.
func researchNewsBody(source ResearchSource) string {
	body := strings.TrimSpace(source.Content)
	for _, prefix := range []string{"新闻检索摘要（非全文，第三方报道需核实）：", "新闻检索摘要（非全文）："} {
		body = strings.TrimSpace(strings.TrimPrefix(body, prefix))
	}
	if source.ContentStatus == "title_only" || body == strings.TrimSpace(source.Title) {
		return ""
	}
	return body
}

func researchNewsTraceable(source ResearchSource) bool {
	if source.Kind != "news" || researchNewsBody(source) == "" || source.PublishedAt.IsZero() {
		return false
	}
	publisher := strings.TrimSpace(strings.TrimPrefix(source.Provider, "eastmoney:stock-news-search:"))
	u, err := url.Parse(source.URL)
	return publisher != "" && err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Hostname() != "" && u.User == nil
}

// Kept in compact downstream dossiers as well as individual research prompts,
// so a news citation does not lose its provenance during portfolio aggregation.
type NewsEvidenceContext struct {
	Provider      string    `json:"provider"`
	URL           string    `json:"url"`
	PublishedAt   time.Time `json:"published_at"`
	ContentStatus string    `json:"content_status,omitempty"`
	Traceable     bool      `json:"traceable"`
	Excerpt       string    `json:"excerpt,omitempty"`
}

func ResearchNewsEvidenceContext(source ResearchSource, excerptLimit int) *NewsEvidenceContext {
	if source.Kind != "news" {
		return nil
	}
	context := &NewsEvidenceContext{
		Provider: truncateExactText(source.Provider, 120), URL: truncateExactText(source.URL, 400),
		PublishedAt: source.PublishedAt, ContentStatus: source.ContentStatus, Traceable: researchNewsTraceable(source),
	}
	if excerptLimit > 0 {
		context.Excerpt = truncateExactText(researchNewsBody(source), excerptLimit)
	}
	return context
}
