package stockanalysis

import (
	"encoding/json"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
)

const researchTradingLogicRules = `
近期交易逻辑：trading_logic回答“近期市场可能在交易什么预期”，与主营行业、量化题材评分和已确认盘面共振分开。
优先识别输入原文中的具体产品、应用、业绩兑现、订单、资本事件及板块共同修复；可以提出端侧AI、SoC等细分逻辑，不限于预设题材名称。端侧NPU、消费电子SoC不能自动等同于云端算力、光通信或CPO；必须说明具体公司关系。事实与市场解释分开：公司涉及某业务不证明该业务主导涨价，逻辑解释一律inference。
公司经营预期与最近两日上涨原因分别判断：有公司原文支持的新品、客户导入、量产和业绩逻辑，即使没有异动当天新公告，仍可列为候选并标注盘面待验证。候选排序须比较具体经营进展、兑现阶段、风险与事件时效，不能仅因资本公告日期较新就排在主线。公司预计、有望、客户预测不等于已量产或已实现收入，不能删去原文限定条件。
business是有来源的主营背景；mainlines最多2项、secondary最多2项、catalysts最多3条，每项name至多24字、explanation至多140字。只有输入证据支持的候选才能输出，没有依据就空数组并在gaps写具体缺口，不能凭模型记忆填事实。催化事件必须交代来源日期，日期未知须明示，旧消息不能当作新催化。新闻检索摘要不是全文，第三方报道不能自动升级为公司披露。
每项的evidence_level仅评价公司关系与事件依据，取sufficient/limited/insufficient；market_status单独取supported/mixed/unverified。market_evidence引用同日期、与候选相关的m-sector样本或usable_for_current_move=true的m-themes节点，说明样本范围；它只是价格对照研究推断，不能证明因果、资金流或完整板块表现。泛行业样本不能确认细分题材，缺少相关数据就unverified并写gaps。没有确认盘面时保留有依据的候选，不能回退为泛行业名称。以市场逻辑为假设，比较已有alternatives，不强求唯一主线。
trading_logic内每个explanation、business、catalyst、market_evidence都用ResearchClaim结构text/kind/source_ids/可选quote，引用本次输入编号；gaps只写尚未解决的具体缺口，不伪造引用。缺少证据时business和market_evidence可为null。
`

const researchTradingLogicSchema = `"trading_logic":{"business":null,"mainlines":[{"name":"具体逻辑候选","explanation":{"text":"公司关系、预期及限制","kind":"inference","source_ids":["编号"]},"evidence_level":"sufficient|limited|insufficient","market_status":"supported|mixed|unverified","market_evidence":null,"gaps":["盘面或事件待核实事项"]}],"secondary":[],"catalysts":[{"text":"事件、日期及原文实际披露内容","kind":"fact|opinion|inference","source_ids":["编号"]}],"gaps":[]}`

func normalizeTradingLogic(logic *ResearchTradingLogic, snapshot ResearchSnapshot, validateClaim func(*ResearchClaim) error) []string {
	if logic == nil {
		return nil
	}
	notes := []string{}
	sources := map[string]ResearchSource{}
	for _, source := range snapshot.Sources {
		sources[source.ID] = source
	}
	valid := func(claim *ResearchClaim) bool {
		if err := validateClaim(claim); err != nil {
			return false
		}
		for _, id := range claim.SourceIDs {
			if source := sources[id]; !source.PublishedAt.IsZero() && source.PublishedAt.After(snapshot.CutoffAt) {
				return false
			}
		}
		return true
	}
	companyEvidence := func(claim ResearchClaim) bool {
		for _, id := range claim.SourceIDs {
			source := sources[id]
			if source.Kind == "company_profile" || source.Kind == "disclosure" || (source.Kind == "announcement" && ResearchSourceHasBody(source)) {
				return true
			}
		}
		return false
	}
	if logic.Business != nil && (!valid(logic.Business) || !companyEvidence(*logic.Business)) {
		logic.Business = nil
		logic.Gaps = append(logic.Gaps, "主营背景缺少有效来源，尚待补充")
	}
	seen := map[string]bool{}
	for _, group := range []*[]ResearchLogicItem{&logic.Mainlines, &logic.Secondary} {
		items := []ResearchLogicItem{}
		for _, item := range *group {
			item.Name = truncateExactText(strings.TrimSpace(item.Name), 24)
			if item.Name == "" || seen[item.Name] {
				continue
			}
			if !valid(&item.Explanation) {
				logic.Gaps = append(logic.Gaps, item.Name+"未通过来源校验，不能作为有效逻辑展示")
				continue
			}
			seen[item.Name] = true
			item.Explanation.Kind = "inference"
			if item.EvidenceLevel != "sufficient" && item.EvidenceLevel != "limited" && item.EvidenceLevel != "insufficient" {
				item.EvidenceLevel = "limited"
			}
			if item.EvidenceLevel == "sufficient" && !companyEvidence(item.Explanation) {
				item.EvidenceLevel = "limited"
				item.Gaps = append(item.Gaps, "公司关系或事件仍需原始披露核实")
			}
			if item.MarketEvidence != nil && !valid(item.MarketEvidence) {
				item.MarketEvidence = nil
			}
			marketUsable := false
			if item.MarketEvidence != nil {
				item.MarketEvidence.Kind = "inference"
				for _, id := range item.MarketEvidence.SourceIDs {
					marketUsable = marketUsable || tradingLogicMarketUsable(sources[id], item.Name, snapshot.CutoffAt)
				}
			}
			if !marketUsable || (item.MarketStatus != "supported" && item.MarketStatus != "mixed") {
				item.MarketStatus = "unverified"
				if !marketUsable {
					item.MarketEvidence = nil
				}
				item.Gaps = append(item.Gaps, "缺少同日期且对应此逻辑的有效盘面对照，尚未确认共振")
			}
			item.Gaps = boundedLogicGaps(item.Gaps, 4)
			items = append(items, item)
			if len(items) == 2 {
				break
			}
		}
		*group = items
	}
	catalysts := []ResearchClaim{}
	for _, claim := range logic.Catalysts {
		if !valid(&claim) {
			logic.Gaps = append(logic.Gaps, "部分催化事件缺少有效来源，已移除")
			continue
		}
		eventEvidence, recent, dated := false, false, false
		for _, id := range claim.SourceIDs {
			source := sources[id]
			if source.Kind == "news" || source.Kind == "opinion" || source.Kind == "disclosure" || (source.Kind == "announcement" && ResearchSourceHasBody(source)) {
				eventEvidence = true
				if !source.PublishedAt.IsZero() {
					dated = true
					recent = recent || !source.PublishedAt.Before(snapshot.CutoffAt.AddDate(0, 0, -60))
				}
			}
		}
		if !eventEvidence || (dated && !recent) {
			logic.Gaps = append(logic.Gaps, "部分催化仅有业务背景、量价数据或旧事件，不能作为近期事件展示")
			continue
		}
		if !dated {
			logic.Gaps = append(logic.Gaps, "部分催化来源发布时间未知，不能确认事件先后")
		}
		catalysts = append(catalysts, claim)
		if len(catalysts) == 3 {
			break
		}
	}
	logic.Catalysts = catalysts
	logic.Gaps = boundedLogicGaps(logic.Gaps, 6)
	if len(logic.Mainlines) == 0 {
		logic.Gaps = boundedLogicGaps(append(logic.Gaps, "尚未取得足够依据形成近期主线候选"), 6)
	}
	notes = append(notes, "近期交易逻辑已检查来源结构；公司证据与盘面状态分别评估，归因仍为研究推断")
	return notes
}

func boundedLogicGaps(values []string, limit int) []string {
	for i := range values {
		values[i] = truncateExactText(values[i], 160)
	}
	return uniqueStrings(values, limit)
}

func tradingLogicMarketUsable(source ResearchSource, name string, cutoff time.Time) bool {
	sameTopic := func(topic string) bool {
		return strings.EqualFold(strings.ReplaceAll(strings.TrimSpace(topic), " ", ""), strings.ReplaceAll(name, " ", ""))
	}
	fresh := func(date string) bool {
		lag, valid := foundation.AStockSessionLag(date, cutoff)
		return valid && lag <= 5
	}
	if source.ID == "m-sector" {
		var data struct {
			AsOf    string                        `json:"as_of"`
			Group   string                        `json:"peer_group"`
			Scope   string                        `json:"selection_scope"`
			Windows map[string]researchPeerWindow `json:"windows"`
		}
		if json.Unmarshal([]byte(source.Content), &data) != nil || !fresh(data.AsOf) || data.Scope == "broad_industry_fallback" {
			return false
		}
		topic := strings.TrimPrefix(strings.TrimPrefix(data.Group, "概念目录："), "行业目录：")
		if !sameTopic(topic) {
			return false
		}
		for _, window := range data.Windows {
			if window.SampleSize >= 2 && window.EndDate == data.AsOf {
				return true
			}
		}
	}
	if source.ID == "m-themes" {
		var themes []struct {
			Name        string `json:"name"`
			Date        string `json:"trade_date"`
			Usable      bool   `json:"usable_for_current_move"`
			Carry       bool   `json:"carry_forward"`
			Provisional bool   `json:"provisional"`
		}
		if json.Unmarshal([]byte(source.Content), &themes) != nil {
			return false
		}
		for _, theme := range themes {
			if sameTopic(theme.Name) && fresh(theme.Date) && theme.Usable && !theme.Carry && !theme.Provisional {
				return true
			}
		}
	}
	return false
}
