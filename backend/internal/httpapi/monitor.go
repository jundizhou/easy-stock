package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/monitor"
	"easy-stock/backend/internal/notification"
	"easy-stock/backend/internal/providers/eastmoney"
	"easy-stock/backend/internal/themelibrary"
)

// SymbolSuggester 是关注仓搜索添加的行情源窄接口：按名称 / 代码 / 拼音
// 搜 A 股，返回规范化的带后缀代码。由东财客户端实现。
type SymbolSuggester interface {
	StockSuggest(ctx context.Context, query string, count int) ([]eastmoney.SymbolHit, error)
}

// monitorUnavailable 在监控库打不开的降级场景里统一返回 503：
// 让页面能明确显示「监控暂不可用」，而不是 500 一闪而过。
var monitorUnavailable = errors.New("监控中心未就绪")

// monitorInspectionSource 把持仓AI巡检的最近一次成功报告翻译成
// monitor 需要的窄接口。monitor 不 import portfolioinspection，
// 依赖方向保持 httpapi → 两者，与 PortfolioSource 同一手法。
// 巡检报告是异步任务：没有成功过的报告就安静返回空，不报错。
type monitorInspectionSource struct {
	server *Server
}

func (source monitorInspectionSource) LatestInspectionConclusions(ctx context.Context) ([]monitor.InspectionConclusion, error) {
	if source.server == nil || source.server.portfolioInspection == nil {
		return nil, nil
	}
	jobs, err := source.server.portfolioInspection.List(ctx, 8)
	if err != nil {
		return nil, err
	}
	for _, job := range jobs {
		if job.Status != "succeeded" || job.Report == nil {
			continue
		}
		actions := map[string]string{}
		for _, action := range job.Report.Conclusion.Actions {
			actions[action.ID] = action.Text
		}
		evaluated := ""
		if !job.Report.GeneratedAt.IsZero() {
			evaluated = job.Report.GeneratedAt.In(foundation.AStockLocation).Format("2006-01-02")
		}
		conclusions := make([]monitor.InspectionConclusion, 0, len(job.Report.Conclusion.Holdings))
		for _, holding := range job.Report.Conclusion.Holdings {
			conclusions = append(conclusions, monitor.InspectionConclusion{
				Symbol:         holding.Symbol,
				Conclusion:     holding.Conclusion,
				Action:         actions[holding.ActionID],
				ActionPriority: holding.ActionPriority,
				EvaluatedAt:    evaluated,
			})
		}
		return conclusions, nil
	}
	return nil, nil
}

// LatestInspectionHoldings 取最近一次成功巡检报告里的持仓清单本身
// （用户在巡检页手动维护的组合），供「巡检持仓」作用域与作用域预览用。
func (source monitorInspectionSource) LatestInspectionHoldings(ctx context.Context) ([]monitor.ScopeTarget, error) {
	if source.server == nil || source.server.portfolioInspection == nil {
		return nil, nil
	}
	jobs, err := source.server.portfolioInspection.List(ctx, 8)
	if err != nil {
		return nil, err
	}
	for _, job := range jobs {
		if job.Status != "succeeded" || job.Report == nil {
			continue
		}
		holdings := job.Report.Request.Holdings
		targets := make([]monitor.ScopeTarget, 0, len(holdings))
		for _, holding := range holdings {
			if holding.Symbol == "" {
				continue
			}
			targets = append(targets, monitor.ScopeTarget{Symbol: holding.Symbol, Name: holding.Name})
		}
		return targets, nil
	}
	return nil, nil
}

// monitorSyncInspectionHoldings 收下巡检页当前草稿的持仓清单。
// 巡检组合只存在浏览器本地，后台引擎读不到，前端改动后推一份过来，
// 否则「巡检持仓」作用域会按历史报告的老清单跑。
func (s *Server) monitorSyncInspectionHoldings(w http.ResponseWriter, r *http.Request) {
	engine, err := s.requireMonitor()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	var body struct {
		Holdings []monitor.ScopeTarget `json:"holdings"`
	}
	if err := decodeMonitorBody(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	targets := make([]monitor.ScopeTarget, 0, len(body.Holdings))
	for _, item := range body.Holdings {
		symbol := strings.TrimSpace(item.Symbol)
		if symbol == "" {
			continue
		}
		targets = append(targets, monitor.ScopeTarget{Symbol: symbol, Name: strings.TrimSpace(item.Name)})
	}
	count, updatedAt, err := engine.SetInspectionHoldings(r.Context(), targets)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"count": count, "updated_at": updatedAt}})
}

// monitorSuggest 是关注仓添加标的的搜索接口：名称、代码、拼音全拼
// 或拼音首字母都行，底层走东财建议接口（上游原生支持拼音）。
func (s *Server) monitorSuggest(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		writeJSON(w, http.StatusOK, map[string]any{"data": []any{}})
		return
	}
	if s.stockSuggest == nil {
		writeError(w, http.StatusServiceUnavailable, "股票搜索源未配置")
		return
	}
	hits, err := s.stockSuggest.StockSuggest(r.Context(), query, 10)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": hits})
}

// monitorWatchlistList 返回监控关注仓清单，现价与涨跌幅尽力补上。
func (s *Server) monitorWatchlistList(w http.ResponseWriter, r *http.Request) {
	engine, err := s.requireMonitor()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	items, err := engine.Watchlist(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items, "coverage": engine.WatchCoverage(r.Context(), items)})
}

// monitorWatchlistAdd 加入一支标的。symbol 必填，name/note 可选；
// 前端从搜索结果带入名称，直接输入 6 位代码的由引擎按快照补名。
func (s *Server) monitorWatchlistAdd(w http.ResponseWriter, r *http.Request) {
	engine, err := s.requireMonitor()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	var body struct {
		Symbol string `json:"symbol"`
		Name   string `json:"name"`
		Note   string `json:"note"`
	}
	if err := decodeMonitorBody(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	symbol := strings.TrimSpace(body.Symbol)
	if symbol == "" {
		writeError(w, http.StatusBadRequest, "标的代码不能为空")
		return
	}
	if err := engine.AddWatchlist(r.Context(), symbol, strings.TrimSpace(body.Name), strings.TrimSpace(body.Note)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	items, err := engine.Watchlist(r.Context())
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"ok": true}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items})
}

// monitorWatchlistUpdate 二次编辑备注：PATCH 语义，note 允许清空，
// 与 POST 的 upsert（空备注不覆盖）刻意区分开。
func (s *Server) monitorWatchlistUpdate(w http.ResponseWriter, r *http.Request) {
	engine, err := s.requireMonitor()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	var body struct {
		Symbol string `json:"symbol"`
		Note   string `json:"note"`
	}
	if err := decodeMonitorBody(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	symbol := strings.TrimSpace(body.Symbol)
	if symbol == "" {
		writeError(w, http.StatusBadRequest, "标的代码不能为空")
		return
	}
	if len(body.Note) > 200 {
		writeError(w, http.StatusBadRequest, "备注最长 200 字")
		return
	}
	if err := engine.UpdateWatchlistNote(r.Context(), symbol, strings.TrimSpace(body.Note)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"ok": true}})
}

// monitorWatchlistRemove 移除一支标的（路径参数带不带交易所后缀都认）。
func (s *Server) monitorWatchlistRemove(w http.ResponseWriter, r *http.Request) {
	engine, err := s.requireMonitor()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	symbol := strings.TrimSpace(r.PathValue("symbol"))
	if symbol == "" {
		writeError(w, http.StatusBadRequest, "symbol is required")
		return
	}
	if err := engine.RemoveWatchlist(r.Context(), symbol); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"ok": true}})
}

// monitorWatchlistClear 清空关注仓。
func (s *Server) monitorWatchlistClear(w http.ResponseWriter, r *http.Request) {
	engine, err := s.requireMonitor()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	if err := engine.ClearWatchlist(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"ok": true}})
}

func (s *Server) requireMonitor() (*monitor.Engine, error) {
	if s == nil || s.monitor == nil {
		return nil, monitorUnavailable
	}
	return s.monitor, nil
}

// decodeMonitorBody 是监控接口统一的 JSON 读取：限 64 KB 并拒绝未知字段。
func decodeMonitorBody(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("请求格式无效")
	}
	return ensureJSONEOF(decoder)
}

func notFoundIfMissing(err error) int {
	if strings.Contains(strings.TrimSpace(err.Error()), "不存在") {
		return http.StatusNotFound
	}
	return http.StatusBadRequest
}

// monitorSectorOption 是板块选择器的一个候选。
type monitorSectorOption struct {
	Code          string  `json:"code"`
	Name          string  `json:"name"`
	Dimension     string  `json:"dimension"`
	ChangePercent float64 `json:"change_percent"`
	MainNetInflow float64 `json:"main_net_inflow"`
}

// monitorSectors 返回可选板块清单（行业 + 概念 + 系统题材库题材），
// 给规则编辑器的板块多选用。带当日涨跌幅与主力净额，选的时候顺便
// 看到谁在动。题材库题材以 TH 编号出现，监控时由成员股快照聚合。
func (s *Server) monitorSectors(w http.ResponseWriter, r *http.Request) {
	engine, err := s.requireMonitor()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	source := engine.SectorRankSource()
	if source == nil {
		writeError(w, http.StatusServiceUnavailable, "板块榜单数据源未配置")
		return
	}
	items := make([]monitorSectorOption, 0, 400)
	seen := map[string]bool{}
	for _, dimension := range []string{"industry", "concept"} {
		rows, _, err := source.SectorFlowRanking(r.Context(), dimension, 200)
		if err != nil {
			continue
		}
		for _, row := range rows {
			if row.Code == "" || row.Name == "" || seen[row.Code] {
				continue
			}
			seen[row.Code] = true
			items = append(items, monitorSectorOption{
				Code: row.Code, Name: row.Name, Dimension: dimension,
				ChangePercent: row.ChangePercent, MainNetInflow: row.MainNetInflow,
			})
		}
	}
	// 系统题材库题材：这是本系统自己的板块体系，必须与东财板块并列可选。
	if s.themeLibrary != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()
		library, err := s.themeLibrary.List(ctx, themelibrary.Filter{Limit: 300})
		if err == nil {
			for _, theme := range library.Items {
				if theme.ID <= 0 || theme.Name == "" {
					continue
				}
				code := fmt.Sprintf("TH%d", theme.ID)
				if seen[code] {
					continue
				}
				seen[code] = true
				items = append(items, monitorSectorOption{
					Code: code, Name: theme.Name, Dimension: "theme",
					ChangePercent: theme.ChangePercent, MainNetInflow: 0,
				})
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"items": items}})
}

// monitorThemeSource 把系统题材库适配给监控引擎的板块维度。题材代码
// 约定 TH+数字（TH5 = 题材库 id 5），成员股用裸 6 位代码——快照索引
// 两种口径都收，直接对齐。
type monitorThemeSource struct {
	server *Server
}

func (s monitorThemeSource) store() *themelibrary.Store {
	if s.server == nil {
		return nil
	}
	return s.server.themeLibrary
}

func (s monitorThemeSource) Themes(ctx context.Context) ([]monitor.ThemeSummary, error) {
	store := s.store()
	if store == nil {
		return nil, errors.New("题材库数据库尚未初始化")
	}
	library, err := store.List(ctx, themelibrary.Filter{Limit: 300})
	if err != nil {
		return nil, err
	}
	themes := make([]monitor.ThemeSummary, 0, len(library.Items))
	for _, theme := range library.Items {
		if theme.ID <= 0 {
			continue
		}
		themes = append(themes, monitor.ThemeSummary{
			ID: int(theme.ID), Name: theme.Name,
			ChangePercent: theme.ChangePercent,
			StockCount:    theme.StockCount, LimitUpCount: theme.LimitUpCount,
		})
	}
	return themes, nil
}

func (s monitorThemeSource) ThemeMembers(ctx context.Context, id int) ([]string, error) {
	store := s.store()
	if store == nil {
		return nil, errors.New("题材库数据库尚未初始化")
	}
	theme, err := store.Get(ctx, strconv.Itoa(id))
	if err != nil {
		return nil, err
	}
	codes := []string{}
	seen := map[string]bool{}
	for _, group := range theme.Groups {
		for _, stock := range group.Stocks {
			code := strings.TrimSpace(stock.Code)
			if code == "" || seen[code] {
				continue
			}
			seen[code] = true
			codes = append(codes, code)
		}
	}
	return codes, nil
}

// monitorSectorIntradaySource 把行情总览的板块分时能力适配给监控引擎，
// 并复用 handler 的降级策略：上游分钟序列不可用（如收盘后、周末）时，
// 回退到本地 3 分钟采样拼出的序列。直连 provider 在非交易时段会拿空，
// 采样库却仍有最近交易日的完整曲线——监控评估不该因此整维度沉默。
type monitorSectorIntradaySource struct {
	server *Server
}

func (s monitorSectorIntradaySource) provider() SectorFlowProvider {
	if s.server == nil {
		return nil
	}
	provider, _ := s.server.marketOverview.(SectorFlowProvider)
	return provider
}

func (s monitorSectorIntradaySource) SectorFlowIntraday(ctx context.Context, codes []string) ([]foundation.SectorFlowSeries, foundation.SourceMeta, error) {
	provider := s.provider()
	if provider == nil {
		return nil, foundation.SourceMeta{}, errors.New("板块分时数据源未配置")
	}
	data, meta, err := provider.SectorFlowIntraday(ctx, codes)
	if err != nil {
		if fallback, fallbackMeta, ok := s.server.sectorFlowSeriesFromSamples(ctx, codes); ok {
			return fallback, fallbackMeta, nil
		}
		return nil, meta, err
	}
	if merged, mergedMeta := s.server.mergeSectorFlowSamples(ctx, codes, data, meta); len(merged) > 0 {
		return merged, mergedMeta, nil
	}
	return data, meta, nil
}

func (s monitorSectorIntradaySource) SectorPriceIntraday(ctx context.Context, codes []string) ([]foundation.SectorPriceSeries, foundation.SourceMeta, error) {
	provider := s.provider()
	if provider == nil {
		return nil, foundation.SourceMeta{}, errors.New("板块分时数据源未配置")
	}
	data, meta, err := provider.SectorPriceIntraday(ctx, codes)
	if err != nil {
		if fallback, fallbackMeta, ok := s.server.sectorPriceSeriesFromSamples(ctx, codes); ok {
			return fallback, fallbackMeta, nil
		}
		return nil, meta, err
	}
	if merged, mergedMeta := s.server.mergeSectorPriceSamples(ctx, codes, data, meta); len(merged) > 0 {
		return merged, mergedMeta, nil
	}
	return data, meta, nil
}

func (s *Server) monitorOverview(w http.ResponseWriter, r *http.Request) {
	engine, err := s.requireMonitor()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	view, err := engine.Overview(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": view})
}

// monitorTargets 返回持仓 / 关注仓 / 巡检持仓三个作用域当前命中的标的，
// 供规则编辑器把「这条规则在盯哪几支票」透明地摆在用户面前。
func (s *Server) monitorTargets(w http.ResponseWriter, r *http.Request) {
	engine, err := s.requireMonitor()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": engine.ScopeTargets(r.Context())})
}

func (s *Server) monitorRulesList(w http.ResponseWriter, r *http.Request) {
	engine, err := s.requireMonitor()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	rules, err := engine.Rules(r.Context(), strings.TrimSpace(r.URL.Query().Get("type")))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": rules, "total": len(rules)})
}

func (s *Server) monitorRuleCreate(w http.ResponseWriter, r *http.Request) {
	engine, err := s.requireMonitor()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	var rule monitor.Rule
	if err := decodeMonitorBody(w, r, &rule); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	rule.ID = ""
	saved, err := engine.SaveRule(r.Context(), rule)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": saved})
}

func (s *Server) monitorRuleUpdate(w http.ResponseWriter, r *http.Request) {
	engine, err := s.requireMonitor()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	var rule monitor.Rule
	if err := decodeMonitorBody(w, r, &rule); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	rule.ID = r.PathValue("id")
	saved, err := engine.SaveRule(r.Context(), rule)
	if err != nil {
		writeError(w, notFoundIfMissing(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": saved})
}

func (s *Server) monitorRuleDelete(w http.ResponseWriter, r *http.Request) {
	engine, err := s.requireMonitor()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	if err := engine.RemoveRule(r.Context(), r.PathValue("id")); err != nil {
		status := notFoundIfMissing(err)
		if strings.Contains(err.Error(), "批次托管") {
			status = http.StatusConflict
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) monitorRuleToggle(w http.ResponseWriter, r *http.Request) {
	engine, err := s.requireMonitor()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	var payload struct {
		Enabled *bool `json:"enabled"`
	}
	if err := decodeMonitorBody(w, r, &payload); err != nil || payload.Enabled == nil {
		writeError(w, http.StatusBadRequest, "需要 enabled 字段")
		return
	}
	if err := engine.ToggleRule(r.Context(), r.PathValue("id"), *payload.Enabled); err != nil {
		writeError(w, notFoundIfMissing(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) monitorRuleTest(w http.ResponseWriter, r *http.Request) {
	engine, err := s.requireMonitor()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	var rule monitor.Rule
	if err := decodeMonitorBody(w, r, &rule); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	outcome, err := engine.TestRule(r.Context(), rule)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": outcome})
}

func (s *Server) monitorEventsList(w http.ResponseWriter, r *http.Request) {
	engine, err := s.requireMonitor()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	query := r.URL.Query()
	limit, _ := strconv.Atoi(query.Get("limit"))
	sinceID, _ := strconv.ParseInt(query.Get("since_id"), 10, 64)
	filter := monitor.EventFilter{
		RuleID:      strings.TrimSpace(query.Get("rule_id")),
		RuleType:    strings.TrimSpace(query.Get("type")),
		Symbol:      strings.TrimSpace(query.Get("symbol")),
		Severity:    strings.TrimSpace(query.Get("severity")),
		TradeDate:   strings.TrimSpace(query.Get("trade_date")),
		SinceID:     sinceID,
		Limit:       limit,
		OldestFirst: query.Get("order") == "asc",
	}
	events, err := engine.Events(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	nextSinceID := int64(0)
	matchedTotal, err := engine.EventCount(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, event := range events {
		if event.ID > nextSinceID {
			nextSinceID = event.ID
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": events, "total": len(events), "matched_total": matchedTotal, "has_more": matchedTotal > len(events), "since_id": nextSinceID})
}

func (s *Server) monitorEventDelete(w http.ResponseWriter, r *http.Request) {
	engine, err := s.requireMonitor()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "事件 ID 无效")
		return
	}
	if err := engine.DeleteEvent(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) monitorEventsClear(w http.ResponseWriter, r *http.Request) {
	engine, err := s.requireMonitor()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	before, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
	if err := engine.ClearEvents(r.Context(), before); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) monitorAbnormal(w http.ResponseWriter, r *http.Request) {
	engine, err := s.requireMonitor()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	view, err := engine.Abnormal(r.Context(), strings.TrimSpace(r.URL.Query().Get("tab")), limit)
	if err != nil {
		// 快照源不可用时 view 仍带着生成信息返回，页面把缘由直接展示出来。
		writeJSON(w, http.StatusBadGateway, map[string]any{"data": view, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": view})
}

// monitorAbnormalCalc 单票异动计算器：逐窗口阈值/剩余空间 + 近 30 日明细。
func (s *Server) monitorAbnormalCalc(w http.ResponseWriter, r *http.Request) {
	engine, err := s.requireMonitor()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	view, err := engine.AbnormalCalc(r.Context(), r.URL.Query().Get("symbol"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": view})
}

func (s *Server) monitorRadar(w http.ResponseWriter, r *http.Request) {
	engine, err := s.requireMonitor()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": engine.RadarScan(r.Context(), r.URL.Query().Get("force") == "1")})
}

func (s *Server) monitorLotsList(w http.ResponseWriter, r *http.Request) {
	engine, err := s.requireMonitor()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	lots, err := engine.Lots(r.Context(), strings.TrimSpace(r.URL.Query().Get("source")))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": lots, "total": len(lots)})
}

func (s *Server) monitorLotCreate(w http.ResponseWriter, r *http.Request) {
	engine, err := s.requireMonitor()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	var lot monitor.Lot
	if err := decodeMonitorBody(w, r, &lot); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	lot.ID, lot.Source = "", monitor.SourceManual
	saved, err := engine.SaveLot(r.Context(), lot)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": saved})
}

func (s *Server) monitorLotUpdate(w http.ResponseWriter, r *http.Request) {
	engine, err := s.requireMonitor()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	var lot monitor.Lot
	if err := decodeMonitorBody(w, r, &lot); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	lot.ID = r.PathValue("id")
	saved, err := engine.SaveLot(r.Context(), lot)
	if err != nil {
		status := notFoundIfMissing(err)
		if strings.Contains(err.Error(), "只读") {
			status = http.StatusConflict
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": saved})
}

func (s *Server) monitorLotDelete(w http.ResponseWriter, r *http.Request) {
	engine, err := s.requireMonitor()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	if err := engine.RemoveLot(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, notFoundIfMissing(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) monitorLotSync(w http.ResponseWriter, r *http.Request) {
	engine, err := s.requireMonitor()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	synced, err := engine.SyncPositions(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"synced": synced})
}

func (s *Server) monitorLotDetach(w http.ResponseWriter, r *http.Request) {
	engine, err := s.requireMonitor()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	lot, err := engine.DetachLot(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, notFoundIfMissing(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": lot})
}

func (s *Server) monitorSettingsGet(w http.ResponseWriter, r *http.Request) {
	if s.monitorPreferences == nil {
		writeError(w, http.StatusServiceUnavailable, monitorUnavailable.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": s.monitorPreferences.Snapshot()})
}

func (s *Server) monitorSettingsUpdate(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireMonitor(); err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	if s.monitorPreferences == nil {
		writeError(w, http.StatusServiceUnavailable, monitorUnavailable.Error())
		return
	}
	var payload struct {
		Enabled             *bool   `json:"enabled"`
		QuoteIntervalSec    *int    `json:"quote_interval_sec"`
		SnapshotIntervalSec *int    `json:"snapshot_interval_sec"`
		QuietHours          *string `json:"quiet_hours"`
		DigestSeconds       *int    `json:"digest_seconds"`
		NotifyEnabled       *bool   `json:"notify_enabled"`
	}
	if err := decodeMonitorBody(w, r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	saved, err := s.monitorPreferences.Update(r.Context(), func(preferences *monitor.Preferences) {
		next := &preferences.Settings
		if payload.Enabled != nil {
			next.Enabled = *payload.Enabled
		}
		if payload.QuoteIntervalSec != nil {
			next.QuoteIntervalSec = *payload.QuoteIntervalSec
		}
		if payload.SnapshotIntervalSec != nil {
			next.SnapshotIntervalSec = *payload.SnapshotIntervalSec
		}
		if payload.QuietHours != nil {
			next.QuietHours = *payload.QuietHours
		}
		if payload.DigestSeconds != nil {
			next.DigestSeconds = *payload.DigestSeconds
		}
		if payload.NotifyEnabled != nil {
			preferences.NotifyEnabled = *payload.NotifyEnabled
		}
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": saved})
}

func (s *Server) monitorRuns(w http.ResponseWriter, r *http.Request) {
	engine, err := s.requireMonitor()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	runs, err := engine.Runs(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": runs, "total": len(runs)})
}

// NotifyMonitorAlert 是 engine 的通知出口：只判断事件开关；静默时段
// 与摘要合并在 engine 侧已完成，这里不再做二次过滤。
func (s *Server) NotifyMonitorAlert(ctx context.Context, event monitor.Event) (string, error) {
	if s == nil || s.monitorPreferences == nil || s.monitorNotifications == nil {
		return monitor.NotifySkipped, nil
	}
	if !s.monitorPreferences.Snapshot().NotifyEnabled {
		return monitor.NotifySkipped, nil
	}
	s.monitorNotifications.Publish(notification.Event{Kind: "monitor_alert", Channels: []string{"feishu", "dingtalk"}, Message: notification.Message{
		Title: "easy-stock 监控提醒",
		Text:  event.Message,
	}})
	_ = ctx
	return monitor.NotifyQueued, nil
}
