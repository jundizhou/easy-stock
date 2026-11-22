package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"easy-stock/backend/internal/etfanalysis"
	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/stockanalysis"
)

func (s *Server) instrumentDirectoryHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	var stock stockDirectoryData
	var etfs []foundation.Instrument
	var stockErr, etfErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); stock, stockErr = s.stockDirectories.load(ctx, s.stockDirectory) }()
	go func() { defer wg.Done(); etfs, etfErr = s.etfProvider.ETFDirectory(ctx) }()
	wg.Wait()
	items := []foundation.Instrument{}
	warnings := []string{}
	if stockErr != nil {
		warnings = append(warnings, "股票目录暂不可用")
	} else {
		for _, v := range stock.Stocks {
			items = append(items, foundation.Instrument{Symbol: v.Symbol, Code: v.Code, Name: v.Name, InstrumentType: "stock"})
		}
	}
	if etfErr != nil {
		warnings = append(warnings, "ETF目录暂不可用，可输入完整代码核实基金档案")
	} else {
		items = append(items, etfs...)
	}
	if len(items) == 0 {
		writeError(w, 502, "标的目录暂不可用，请稍后重试")
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]any{"stocks": items, "total": len(items), "warnings": warnings, "stale": stock.Stale || stockErr != nil || etfErr != nil}})
}

func (s *Server) resolveResearchInstrument(ctx context.Context, request stockanalysis.ResearchRequest, generic bool) (stockanalysis.ResearchRequest, error) {
	if foundation.IsHongKongSymbol(request.Symbol) {
		quotes, err := s.realtimeProvider.Realtime(ctx, []string{request.Symbol})
		if err != nil {
			return request, err
		}
		if len(quotes) == 0 || quotes[0].Symbol != request.Symbol {
			return request, fmt.Errorf("港股证券类型暂不可用")
		}
		if err := validateHongKongEquity(quotes[0]); err != nil {
			return request, err
		}
	}
	isFund := foundation.IsListedFundCandidate(request.Symbol)
	if request.InstrumentType == "etf" && !isFund {
		return request, fmt.Errorf("ETF代码与标的类型不一致")
	}
	if isFund {
		if !generic {
			return request, fmt.Errorf("ETF请使用股票 / ETF分析入口；当前股票专用接口不接受基金")
		}
		profile, err := s.etfProvider.ETFProfile(ctx, request.Symbol)
		if err != nil {
			return request, err
		}
		request.Symbol = profile.Symbol
		request.InstrumentType = "etf"
	} else {
		request.InstrumentType = ""
	}
	return request, nil
}

func (s *Server) loadETFKLine(ctx context.Context, symbol string, limit int) ([]foundation.KLine, error) {
	primaryCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	lines, err := s.etfKLinePrimary.RawKLine(primaryCtx, symbol, "day", limit)
	cancel()
	if err == nil && len(lines) > 0 {
		return lines, nil
	}
	fallbackCtx, stop := context.WithTimeout(ctx, 10*time.Second)
	defer stop()
	lines, err = s.etfKLineFallback.RawKLine(fallbackCtx, symbol, "day", limit)
	return lines, err
}

func (s *Server) collectETFResearch(ctx context.Context, request stockanalysis.ResearchRequest) (stockanalysis.Analysis, *stockanalysis.ResearchSnapshot, error) {
	profile, err := s.etfProvider.ETFProfile(ctx, request.Symbol)
	if err != nil {
		return stockanalysis.Analysis{}, nil, err
	}
	var lines, benchmark []foundation.KLine
	classification := etfanalysis.ClassifyDirection(profile)
	direction := &etfanalysis.DirectionAnalysis{Classification: classification, News: []etfanalysis.DirectionNews{}, Limitations: []string{}}
	var directionMarket foundation.ETFMarketSeries
	var nav foundation.ETFNetValues
	var actions foundation.ETFActions
	var quotes []foundation.Quote
	var lineErr, navErr, quoteErr error
	var holdings []foundation.ETFHoldings
	var announcements []foundation.ETFAnnouncement
	var execution foundation.ETFExecution
	var wg sync.WaitGroup
	if provider, ok := s.etfProvider.(ETFActionProvider); ok {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if v, e := provider.ETFActions(ctx, profile.Symbol); e == nil {
				actions = v
			}
		}()
	}
	if provider, ok := s.etfProvider.(ETFEvidenceProvider); ok {
		wg.Add(1)
		go func() { defer wg.Done(); announcements, _ = provider.ETFDisclosures(ctx, profile.Symbol) }()
		if profile.AssetClass == "equity" || profile.AssetClass == "bond" {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if h, e := provider.ETFHoldings(ctx, profile.Symbol, profile.AssetClass); e == nil {
					holdings = append(holdings, h)
				}
			}()
		}
	}
	if s.etfExecution != nil {
		wg.Add(1)
		go func() { defer wg.Done(); execution, _ = s.etfExecution.ETFExecution(ctx, profile.Symbol) }()
	}
	wg.Add(3)
	go func() { defer wg.Done(); lines, lineErr = s.loadETFKLine(ctx, profile.Symbol, 300) }()
	go func() { defer wg.Done(); nav, navErr = s.etfProvider.ETFNetValues(ctx, profile.Symbol) }()
	go func() { defer wg.Done(); quotes, quoteErr = s.realtimeProvider.Realtime(ctx, []string{profile.Symbol}) }()
	wg.Add(1)
	go func() { defer wg.Done(); directionMarket = s.collectETFDirectionMarket(ctx, profile, classification) }()
	if request.AnalysisLevel != stockanalysis.ResearchLevelQuantitative {
		wg.Add(1)
		go func() {
			defer wg.Done()
			direction.News, direction.NewsCoverage = s.collectETFDirectionNews(ctx, profile.Symbol, classification, time.Now().UTC())
		}()
	} else {
		direction.NewsCoverage.Status = "not_requested"
	}
	wg.Wait()
	now := time.Now().UTC()
	data := etfanalysis.BuildWithActions(profile, nav, lines, actions, now)
	data.Enrich(holdings, announcements, execution, now)
	nav = data.NetValues
	benchmarkSymbol := ""
	if directionMarket.Relationship == "tracking_index" {
		benchmarkSymbol = directionMarket.Symbol
		data.AddBenchmark(benchmarkSymbol, directionMarket.Bars, now)
	}
	direction.Technical = etfanalysis.BuildDirectionTechnical(directionMarket.Symbol, directionMarket.Name, directionMarket.Relationship, directionMarket.Bars, now)
	if direction.Technical == nil && profile.AssetClass != "money" {
		direction.Technical = etfanalysis.BuildDirectionTechnical(profile.Symbol, profile.Name, "etf_price_proxy", data.TrendBars, now)
	}
	if profile.CrossBorder && direction.Technical != nil {
		direction.Technical.Limitations = append(direction.Technical.Limitations, "跨境指数按来源当地日期解释；保守排除采集当日未确认收盘的数据，不与境内ETF做同步涨幅或汇率归因")
	}
	if classification.Status != "disclosed_target" && classification.Status != "asset_class" {
		direction.Limitations = append(direction.Limitations, "投资方向尚需以明确跟踪标的或策略文件核实")
	}
	if direction.NewsCoverage.Status != "retrieved" && direction.NewsCoverage.Status != "not_requested" {
		direction.Limitations = append(direction.Limitations, "方向消息检索覆盖不完整或未检出匹配资料；不代表没有事件或风险")
	}
	data.Direction = direction
	data.Limitations = append(data.Limitations, direction.Limitations...)
	if direction.Technical != nil {
		data.Limitations = append(data.Limitations, direction.Technical.Limitations...)
	}
	benchmark = data.BenchmarkBars
	if len(benchmark) == 0 && profile.AssetClass != "money" {
		data.Limitations = append(data.Limitations, "未取得真实跟踪基准的可比日线，不能计算相对基准强弱")
	}
	if lineErr != nil {
		data.Limitations = append(data.Limitations, "ETF未复权日线暂不可用，仅展示已取得的基金资料")
	}
	if navErr != nil {
		data.Limitations = append(data.Limitations, "基金净值或收益资料暂不可用")
	}
	quote := foundation.Quote{Symbol: profile.Symbol, Name: profile.Name}
	for _, q := range quotes {
		if q.Symbol == profile.Symbol && q.Price > 0 && !q.TradeTime.After(now) {
			quote = q
			break
		}
	}
	if quoteErr != nil || quote.Price <= 0 {
		data.Limitations = append(data.Limitations, "未取得有效当前报价，不以净值替代成交价格")
	}
	if request.AnalysisLevel == stockanalysis.ResearchLevelStandard || request.AnalysisLevel == stockanalysis.ResearchLevelDeep {
		limit := 3
		if request.AnalysisLevel == stockanalysis.ResearchLevelDeep {
			limit = 5
		}
		var enrich sync.WaitGroup
		enrich.Add(2)
		go func() { defer enrich.Done(); data.Peers, data.PeerScope = s.collectETFPeers(ctx, profile, limit) }()
		go func() { defer enrich.Done(); data.Underlying = s.collectETFUnderlying(ctx, data.Holdings, now) }()
		enrich.Wait()
	} else {
		data.PeerScope = "本级别仅分析当前ETF，不执行同类比较"
	}
	now = time.Now().UTC()
	data.Enrich(holdings, announcements, execution, now)
	snapshot := &stockanalysis.ResearchSnapshot{InstrumentType: "etf", ETF: &data, ID: stockanalysis.NewResearchID(), Version: 1, Symbol: profile.Symbol, Name: profile.Name, CapturedAt: now, CutoffAt: now, Quote: quote, Sources: []stockanalysis.ResearchSource{}, Anchors: []stockanalysis.PriceAnchor{}, DailyBars: []stockanalysis.AIDailyBar{}, Limitations: data.Limitations}
	add := func(id, kind, title string, value any, meta foundation.SourceMeta, date string) {
		body, _ := json.Marshal(value)
		snapshot.Sources = append(snapshot.Sources, stockanalysis.ResearchSource{ID: id, Kind: kind, Title: title, Content: string(body), Provider: meta.Source, URL: meta.SourceURL, CapturedAt: meta.FetchedAt, ReportDate: date, TimeStatus: "publication_unknown"})
	}
	add("e-profile", "fund_profile", "ETF档案与投资范围（日期以各字段披露为准）", profile, profile.Meta, "")
	add("e-nav", "fund_data", "基金单位净值 / 货币收益（非实时IOPV，日期为估值日）", map[string]any{"points": tailNAV(nav.Points, 65), "income_per_ten_thousand": tailNAV(nav.IncomePerTenThousand, 20), "seven_day_annualized": tailNAV(nav.SevenDayAnnualized, 20), "limitation": "净值未做分红总回报调整，收益分配日期与发布时点未完整取得"}, nav.Meta, "")
	meta := foundation.SourceMeta{Source: "local-calculation", FetchedAt: now}
	add("e-direction", "fund_data", "ETF投资方向分类与研究重点（先判断方向，再评估产品）", classification, profile.Meta, "")
	if direction.Technical != nil {
		add("e-direction-technical", "fund_data", "投资方向技术事实（严格区分跟踪指数、行业参照和ETF市价）", direction.Technical, direction.Technical.Bars[len(direction.Technical.Bars)-1].Meta, direction.Technical.AsOf)
	}
	add("e-direction-news-coverage", "fund_data", "投资方向消息检索范围与缺口", direction.NewsCoverage, meta, "")
	snapshot.Sources = append(snapshot.Sources, etfDirectionNewsSources(direction.News, now)...)
	add("e-quality", "calculation", "数据限制与执行边界", map[string]any{"limitations": data.Limitations, "execution_blockers": data.ExecutionBlockers}, meta, "")
	add("m-quote", "calculation", "ETF市场报价（独立于净值）", quote, quote.Meta, "")
	add("m-price", "calculation", "ETF完整日线与量价计算", map[string]any{"metrics": data.Metrics, "bars": data.TrendBars[max(0, len(data.TrendBars)-65):], "raw_bars": data.Bars[max(0, len(data.Bars)-65):], "price_basis": data.PriceBasis, "volume_unit": "份"}, meta, "")
	if len(benchmark) > 0 {
		add("e-benchmark", "calculation", "真实跟踪基准行情（不等于ETF净值或总回报）", map[string]any{"name": profile.TrackingTarget, "symbol": benchmarkSymbol, "bars": benchmark[max(0, len(benchmark)-65):]}, benchmark[len(benchmark)-1].Meta, "")
	}
	if data.Actions.Meta.Source != "" {
		add("e-actions", "fund_data", "基金分红拆分记录（比率用于调整历史趋势）", data.Actions, data.Actions.Meta, "")
	}
	if len(data.Underlying) > 0 {
		add("e-underlying", "fund_data", "已披露重仓股财务与估值样本（不代表基金或完整指数）", map[string]any{"sample": data.Underlying, "scope": "按披露权重选取最多3只境内重仓股；报告期持仓非实时，不能把样本外推为整个行业或基金财务，估值为采集时点，财务披露日未知时不作历史可见性推断"}, meta, "")
	}
	add("e-execution", "fund_data", "ETF盘口与IOPV（按各自时点，非交易许可）", data.Execution, execution.Meta, "")
	for i, h := range data.Holdings {
		add(fmt.Sprintf("e-holdings-%d", i), "fund_data", "ETF披露持仓（部分覆盖）", h, h.Meta, h.ReportDate)
	}
	for _, n := range data.Announcements {
		snapshot.Sources = append(snapshot.Sources, etfAnnouncementSource(n))
	}
	add("e-peers", "fund_data", "同指数产品对照与样本范围", map[string]any{"scope": data.PeerScope, "peers": data.Peers}, meta, "")
	for _, b := range data.Bars {
		snapshot.DailyBars = append(snapshot.DailyBars, stockanalysis.AIDailyBar{Date: b.Time.Format("2006-01-02"), Open: b.Open, High: b.High, Low: b.Low, Close: b.Close, Volume: b.Volume, Amount: b.Amount})
	}
	if len(data.Bars) > 0 && profile.AssetClass != "money" {
		last := data.Bars[len(data.Bars)-1]
		date := last.Time.Format("2006-01-02")
		for _, v := range []struct {
			id, label string
			price     float64
		}{{"etf-close", "最近完整收盘", last.Close}, {"etf-low", "最近完整日低点", last.Low}, {"etf-high", "最近完整日高点", last.High}} {
			snapshot.Anchors = append(snapshot.Anchors, stockanalysis.PriceAnchor{ID: v.id, Label: v.label, Price: v.price, SourceID: "m-price", AsOf: date})
		}
	}
	a := stockanalysis.Analysis{InstrumentType: "etf", ETF: &data, Symbol: profile.Symbol, Name: profile.Name, GeneratedAt: now, Quote: quote, SnapshotID: snapshot.ID, Risks: data.Limitations, AI: stockanalysis.AISynthesisStatus{Status: "pending", Message: "ETF资料已就绪，等待研究"}}
	a = stockanalysis.QuantitativeOnly(a)
	return a, snapshot, nil
}

func tailNAV(items []foundation.ETFNAVPoint, n int) []foundation.ETFNAVPoint {
	return items[max(0, len(items)-n):]
}

func (s *Server) collectETFPeers(ctx context.Context, profile foundation.ETFProfile, limit int) ([]etfanalysis.Peer, string) {
	peers := []etfanalysis.Peer{}
	target := strings.TrimSpace(profile.TrackingTarget)
	if target == "" || target == "该基金无跟踪标的" || strings.Contains(target, "--") {
		return peers, "未取得明确跟踪标的，不建立同指数产品比较"
	}
	directory, err := s.etfProvider.ETFDirectory(ctx)
	if err != nil {
		return peers, "ETF目录暂不可用，未完成同指数候选检索"
	}
	needle := etfanalysis.PeerSearchKey(target)
	candidates := []foundation.Instrument{}
	for _, item := range directory {
		if item.Symbol != profile.Symbol && len([]rune(needle)) >= 2 && strings.Contains(etfanalysis.PeerSearchKey(item.Name), needle) {
			candidates = append(candidates, item)
		}
	}
	candidates = candidates[:min(12, len(candidates))]
	peerCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	profiles := make([]foundation.ETFProfile, len(candidates))
	var wg sync.WaitGroup
	slots := make(chan struct{}, 4)
	for i, item := range candidates {
		wg.Add(1)
		go func(i int, item foundation.Instrument) {
			defer wg.Done()
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			case <-peerCtx.Done():
				return
			}
			p, err := s.etfProvider.ETFProfile(peerCtx, item.Symbol)
			if err == nil && p.Symbol == item.Symbol && etfanalysis.SameIndex(p, profile) {
				profiles[i] = p
			}
		}(i, item)
	}
	wg.Wait()
	symbols := []string{}
	for _, p := range profiles {
		if p.Symbol != "" && len(peers) < limit {
			peers = append(peers, etfanalysis.Peer{Profile: p})
			symbols = append(symbols, p.Symbol)
		}
	}
	if len(symbols) > 0 && s.realtimeProvider != nil {
		quotes, _ := s.realtimeProvider.Realtime(peerCtx, symbols)
		for i := range peers {
			for _, q := range quotes {
				if q.Symbol == peers[i].Profile.Symbol && q.Price > 0 && !q.TradeTime.After(time.Now()) {
					copy := q
					peers[i].Quote = &copy
					break
				}
			}
		}
	}
	return peers, fmt.Sprintf("按指数简称检索%d只候选，核实档案后保留%d只同跟踪标的产品；非全市场排名，策略与费用差异仍需比较", len(candidates), len(peers))
}

func etfAnnouncementSource(n foundation.ETFAnnouncement) stockanalysis.ResearchSource {
	return stockanalysis.ResearchSource{ID: "e-notice-" + n.ID + "-" + n.ContentStatus, Kind: "announcement", Title: n.Title, Content: n.Content, Provider: n.Meta.Source, URL: n.URL, CapturedAt: n.Meta.FetchedAt, PublishedAt: n.PublishedAt, TimeStatus: "date_only", ContentStatus: n.ContentStatus}
}

func etfNoticeMatches(content, query string) bool {
	if strings.TrimSpace(query) == "" {
		return true
	}
	for _, term := range strings.Fields(query) {
		if strings.Contains(content, term) {
			return true
		}
	}
	return false
}
