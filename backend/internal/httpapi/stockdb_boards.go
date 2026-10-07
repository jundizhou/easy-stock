package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// boardRef 是反查结果中的板块摘要（不含 symbols 全量，控制响应体积）。
type boardRef struct {
	Code     string `json:"code"`
	Name     string `json:"name"`
	Category string `json:"category"`
}

// boardIndex 是 Free-StockDB 板块* 表的进程级内存索引。
// 板块表每日同步一次，进程内缓存即可；索引懒加载，失败不缓存（下次请求重试）。
type boardIndex struct {
	mu        sync.RWMutex
	symbolMap map[string][]boardRef
	loadedAt  time.Time
	loading   bool
}

var globalBoardIndex = &boardIndex{}

// boardIndexTTL 板块表按交易日更新，缓存 6 小时足够；?refresh=1 可强制刷新。
const boardIndexTTL = 6 * time.Hour

// boardRowValue 对应 stockdb 板块表行值（与前端 sidecars.ts parseBoardRows 同口径）。
type boardRowValue struct {
	Code     string   `json:"code"`
	Name     string   `json:"name"`
	Category string   `json:"category"`
	Symbols  []string `json:"symbols"`
}

// ensureBoardIndex 保证索引可用并返回读锁下的快照使用方式：返回的 map 不可修改。
func (s *Server) ensureBoardIndex(ctx context.Context) (map[string][]boardRef, time.Time, error) {
	globalBoardIndex.mu.RLock()
	if globalBoardIndex.symbolMap != nil && time.Since(globalBoardIndex.loadedAt) < boardIndexTTL {
		symbolMap, loadedAt := globalBoardIndex.symbolMap, globalBoardIndex.loadedAt
		globalBoardIndex.mu.RUnlock()
		return symbolMap, loadedAt, nil
	}
	globalBoardIndex.mu.RUnlock()

	globalBoardIndex.mu.Lock()
	defer globalBoardIndex.mu.Unlock()
	// 双检：等锁期间可能已被其他请求加载。
	if globalBoardIndex.symbolMap != nil && time.Since(globalBoardIndex.loadedAt) < boardIndexTTL {
		return globalBoardIndex.symbolMap, globalBoardIndex.loadedAt, nil
	}
	if globalBoardIndex.loading {
		return nil, time.Time{}, errBoardIndexLoading
	}
	if s.stockDBProvider == nil {
		return nil, time.Time{}, errBoardIndexUnavailable
	}
	globalBoardIndex.loading = true
	defer func() { globalBoardIndex.loading = false }()

	raw, err := s.stockDBProvider.RawQuery(ctx, "板块*")
	if err != nil {
		return nil, time.Time{}, err
	}
	var rows []json.RawMessage
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, time.Time{}, err
	}
	symbolMap := make(map[string][]boardRef, 6000)
	for _, row := range rows {
		var pair [2]json.RawMessage
		if err := json.Unmarshal(row, &pair); err != nil {
			continue
		}
		var value boardRowValue
		if err := json.Unmarshal(pair[1], &value); err != nil {
			continue
		}
		if value.Code == "" || value.Name == "" || value.Category == "" || len(value.Symbols) == 0 {
			continue
		}
		ref := boardRef{Code: value.Code, Name: value.Name, Category: value.Category}
		for _, symbol := range value.Symbols {
			symbol = strings.TrimSpace(symbol)
			if symbol == "" {
				continue
			}
			symbolMap[symbol] = append(symbolMap[symbol], ref)
		}
	}
	if len(symbolMap) == 0 {
		return nil, time.Time{}, errBoardIndexEmpty
	}
	globalBoardIndex.symbolMap = symbolMap
	globalBoardIndex.loadedAt = time.Now()
	return globalBoardIndex.symbolMap, globalBoardIndex.loadedAt, nil
}

var (
	errBoardIndexLoading     = &boardIndexError{"板块索引正在加载，请稍后重试"}
	errBoardIndexUnavailable = &boardIndexError{"stockdb 数据底座不可用"}
	errBoardIndexEmpty       = &boardIndexError{"板块索引为空，stockdb 数据可能未同步"}
)

type boardIndexError struct{ message string }

func (e *boardIndexError) Error() string { return e.message }

// stockDBSymbolBoards 批量反查个股所属板块。
//
//	GET /api/v1/stockdb/symbol-boards?codes=000001,600519&limit=12[&refresh=1]
//
// 返回 {"boards": {"000001": [{code,name,category}...]}, "indexed_at": "..."}。
// codes 逗号分隔，最多 200 个；limit 限制每只股票返回的板块数（默认 12，按 申万一级→概念 优先排序）。
func (s *Server) stockDBSymbolBoards(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	codesParam := strings.TrimSpace(query.Get("codes"))
	if codesParam == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "require 'codes' query param, e.g. codes=000001,600519"})
		return
	}
	codes := strings.Split(codesParam, ",")
	if len(codes) > 200 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "codes accepts at most 200 symbols"})
		return
	}
	limit := 12
	if raw := query.Get("limit"); raw != "" {
		if parsed, ok := atoiInRange(raw, 1, 60); ok {
			limit = parsed
		}
	}
	if query.Get("refresh") == "1" {
		globalBoardIndex.mu.Lock()
		globalBoardIndex.symbolMap = nil
		globalBoardIndex.loadedAt = time.Time{}
		globalBoardIndex.mu.Unlock()
	}

	symbolMap, loadedAt, err := s.ensureBoardIndex(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	boards := make(map[string][]boardRef, len(codes))
	for _, code := range codes {
		code = normalizeBoardSymbol(code)
		if code == "" {
			continue
		}
		boards[code] = rankBoardRefs(symbolMap[code], limit)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"boards":     boards,
		"indexed_at": loadedAt.UTC().Format(time.RFC3339),
	})
}

// rankBoardRefs 申万一级优先、概念次之、其余类目最后；同级按名称稳定排序后截断。
func rankBoardRefs(refs []boardRef, limit int) []boardRef {
	if len(refs) == 0 {
		return []boardRef{}
	}
	ranked := make([]boardRef, len(refs))
	copy(ranked, refs)
	sort.SliceStable(ranked, func(i, j int) bool {
		pi, pj := boardCategoryPriority(ranked[i].Category), boardCategoryPriority(ranked[j].Category)
		if pi != pj {
			return pi < pj
		}
		return ranked[i].Name < ranked[j].Name
	})
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	return ranked
}

func boardCategoryPriority(category string) int {
	switch category {
	case "申万一级":
		return 0
	case "概念":
		return 1
	case "申万二级":
		return 2
	default:
		return 3
	}
}

// normalizeBoardSymbol 归一化输入代码：去空白与交易所前缀/后缀，保留 6 位数字。
func normalizeBoardSymbol(input string) string {
	trimmed := strings.ToUpper(strings.TrimSpace(input))
	if idx := strings.IndexByte(trimmed, '.'); idx > 0 {
		trimmed = trimmed[:idx]
	}
	start := -1
	for i := 0; i < len(trimmed); i++ {
		if trimmed[i] >= '0' && trimmed[i] <= '9' {
			start = i
			break
		}
	}
	if start < 0 || len(trimmed)-start < 6 {
		return ""
	}
	digits := trimmed[start : start+6]
	for i := 0; i < len(digits); i++ {
		if digits[i] < '0' || digits[i] > '9' {
			return ""
		}
	}
	return digits
}

func atoiInRange(raw string, min, max int) (int, bool) {
	value := 0
	for _, ch := range raw {
		if ch < '0' || ch > '9' {
			return 0, false
		}
		value = value*10 + int(ch-'0')
		if value > max*10 {
			return 0, false
		}
	}
	if value < min || value > max {
		return 0, false
	}
	return value, true
}
