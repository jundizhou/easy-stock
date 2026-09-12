package sector

import (
	"context"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/providers/duanxianxia"
)

// eastMoneyBoardCacheTTL keeps the EastMoney board lists (industry + concept)
// from being re-fetched on every radar request.
const eastMoneyBoardCacheTTL = 3 * time.Minute

// eastMoneyBoardLimit is generous enough to cover every board a Kaipanla theme
// can map to while staying a single request per family.
const eastMoneyBoardLimit = 520

// radarIntradayBlend weights today's EastMoney reading against the
// (carried-forward) Kaipanla recipe when the snapshot is not from today.
// 开盘啦快照是日频的，盘中当天的列并不存在，所以它描述的只能是上一个交易日。
// 权重给到 0.8 才能让「当日强度」真正跟随今天：东财概念板块提供的是**今天**
// 的涨幅与涨跌家数，这才是当日口径唯一可信的观测。
const radarIntradayBlend = 0.8

// boardIndex is a name-indexed view of EastMoney boards.
type boardIndex struct {
	boards []foundation.MarketIndustryMomentum
	keys   []string
}

func newBoardIndex(boards []foundation.MarketIndustryMomentum) *boardIndex {
	index := &boardIndex{boards: make([]foundation.MarketIndustryMomentum, 0, len(boards)), keys: make([]string, 0, len(boards))}
	for _, board := range boards {
		key := normalizeRadarMatchName(board.Name)
		if key == "" {
			continue
		}
		index.boards = append(index.boards, board)
		index.keys = append(index.keys, key)
	}
	return index
}

// find resolves one board/concept name to its board data.
func (index *boardIndex) find(term string) (foundation.MarketIndustryMomentum, bool) {
	if index == nil {
		return foundation.MarketIndustryMomentum{}, false
	}
	key := normalizeRadarMatchName(term)
	if key == "" {
		return foundation.MarketIndustryMomentum{}, false
	}
	best := -1
	for position, candidate := range index.keys {
		if candidate == key {
			return index.boards[position], true
		}
		if !strings.Contains(candidate, key) && !strings.Contains(key, candidate) {
			continue
		}
		shorter, longer := key, candidate
		if len([]rune(shorter)) > len([]rune(longer)) {
			shorter, longer = longer, shorter
		}
		// 少于 3 个字的板块名只做精确匹配，否则「电力」会命中「电力设备」。
		if len([]rune(shorter)) < 3 {
			continue
		}
		if best < 0 || len(index.keys[position]) > len(index.keys[best]) {
			best = position
		}
	}
	if best < 0 {
		return foundation.MarketIndustryMomentum{}, false
	}
	return index.boards[best], true
}

// eastMoneyBoards returns the EastMoney board lists used by the radar:
//   - industries (m:90+t:2) for the rising/falling counts the Tencent ranking lacks;
//   - combined industries + concepts (m:90+t:3) so a Kaipanla theme can be
//     resolved to whichever family its mapped keywords live in.
//
// Both lists share one cache entry, so a radar request costs at most two
// upstream calls every cache window.
func (p *RadarProvider) eastMoneyBoards(ctx context.Context) (industry *boardIndex, combined *boardIndex) {
	if p.industryBreadth == nil && p.concepts == nil {
		return nil, nil
	}
	p.boardMu.Lock()
	if p.boardIndustry != nil || p.boardCombined != nil {
		if p.now().Sub(p.boardFetchedAt) < eastMoneyBoardCacheTTL {
			industry, combined = p.boardIndustry, p.boardCombined
			p.boardMu.Unlock()
			return industry, combined
		}
	}
	p.boardMu.Unlock()

	items := make([]foundation.MarketIndustryMomentum, 0, eastMoneyBoardLimit*2)
	var industryItems []foundation.MarketIndustryMomentum
	if p.industryBreadth != nil {
		if list, _, err := p.industryBreadth.IndustryMomentum(ctx, eastMoneyBoardLimit); err == nil {
			industryItems = list
			items = append(items, list...)
		}
	}
	if p.concepts != nil {
		if list, _, err := p.concepts.ConceptMomentum(ctx, eastMoneyBoardLimit); err == nil {
			items = append(items, list...)
		}
	}
	if len(industryItems) == 0 && len(items) == 0 {
		return nil, nil
	}
	industry = newBoardIndex(industryItems)
	combined = newBoardIndex(items)

	p.boardMu.Lock()
	p.boardIndustry = industry
	p.boardCombined = combined
	p.boardFetchedAt = p.now()
	p.boardMu.Unlock()
	return industry, combined
}

// intradayThemeSignal reads today's strength for one Kaipanla theme from the
// EastMoney boards/concepts that theme maps to. Kaipanla's own rolling table is
// a daily aggregate, so during the session this is the only same-day reading.
func intradayThemeSignal(theme duanxianxia.Theme, index *boardIndex) (daily int, change float64, breadth float64, ok bool) {
	if index == nil {
		return 0, 0, 0, false
	}
	terms := themeBoardTerms(theme)
	if len(terms) == 0 {
		return 0, 0, 0, false
	}
	weightedChange := 0.0
	weight := 0.0
	rising := 0
	falling := 0
	matched := 0
	seen := map[string]bool{}
	for _, term := range terms {
		board, exists := index.find(term)
		if !exists || seen[board.Code] {
			continue
		}
		seen[board.Code] = true
		matched++
		boardWeight := float64(max(board.RisingCount+board.FallingCount, 1))
		weightedChange += board.ChangePercent * boardWeight
		weight += boardWeight
		rising += board.RisingCount
		falling += board.FallingCount
	}
	if matched == 0 || weight == 0 {
		return 0, 0, 0, false
	}
	change = weightedChange / weight
	breadth = float64(rising) / float64(max(rising+falling, 1))
	// 与成分股口径保持一致：广度主导、涨幅次之，且涨幅按板块家数做可信度收缩，
	// 否则 7 家的小板块会靠少数票的涨幅压过百家普涨的大板块。
	returnScore := (50 + clampFloat(change, -5, 5)*8) * constituentConfidence(rising+falling)
	score := returnScore*0.35 + breadth*100*0.65
	return int(clampFloat(score, 0, 100)), change, breadth, true
}

// themeBoardTerms lists the EastMoney board/concept names a Kaipanla theme maps
// to: the crosswalk entry, the static narrative theme's tabs/keywords when the
// crosswalk only points at one, and the theme's own name as a last resort.
func themeBoardTerms(theme duanxianxia.Theme) []string {
	terms := make([]string, 0, 16)
	if mapping, ok := lookupRadarThemeMapping(theme.Code, theme.Name); ok {
		if name := strings.TrimSpace(mapping.EastMoneyName); name != "" {
			terms = append(terms, name)
		}
		terms = append(terms, mapping.EastMoneyTerms...)
		if mapping.StaticThemeID != "" {
			if static, found := FindTheme(mapping.StaticThemeID); found {
				terms = append(terms, static.Name)
				terms = append(terms, static.Tabs...)
				for _, group := range static.Groups {
					for _, node := range group.Nodes {
						terms = append(terms, node.BoardKeywords...)
					}
				}
			}
		}
	}
	terms = append(terms, theme.Name)
	return uniqueRadarStrings(terms)
}
