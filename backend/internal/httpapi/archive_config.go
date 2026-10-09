package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"easy-stock/backend/internal/appsettings"
)

// archiveConfigView 是归档配置的可视化视图（供 App 界面编辑）。
type archiveConfigView struct {
	Enabled         bool     `json:"enabled"`
	IntervalSeconds int      `json:"interval_seconds"`
	Dir             string   `json:"dir"`
	DirEffective    string   `json:"dir_effective"` // 实际生效目录（含默认回退）
	SymbolsSource   string   `json:"symbols_source"`
	Symbols         []string `json:"symbols"`
	TradingOnly     bool     `json:"trading_only"`
	NewsLimit       int      `json:"news_limit"`

	// 只读派生字段
	ResolvedSymbols []string `json:"resolved_symbols"` // 按来源解析后的实际采集清单
	HoldingsCount   int      `json:"holdings_count"`   // 最近一次组合持仓可用数量
	ArchiveStatus   any      `json:"archive_status,omitempty"`
}

// buildArchiveConfigView 组装归档配置视图，并解析出实际生效的采集清单。
func (s *Server) buildArchiveConfigView() archiveConfigView {
	cfg := appsettings.DefaultArchive()
	if s.settingsStore != nil {
		cfg = appsettings.NormalizeArchive(s.settingsStore.Snapshot().Archive)
	}
	view := archiveConfigView{
		Enabled:         cfg.Enabled,
		IntervalSeconds: cfg.IntervalSeconds,
		Dir:             cfg.Dir,
		DirEffective:    s.effectiveArchiveDir(cfg),
		SymbolsSource:   cfg.SymbolsSource,
		Symbols:         append([]string(nil), cfg.Symbols...),
		TradingOnly:     cfg.TradingOnly,
		NewsLimit:       cfg.NewsLimit,
	}
	view.ResolvedSymbols = s.resolveArchiveSymbols(cfg)
	view.ResolvedSymbols = append([]string(nil), view.ResolvedSymbols...)
	view.HoldingsCount = len(s.latestHoldingSymbols())
	if s.realtimeArchiver != nil {
		view.ArchiveStatus = s.realtimeArchiver.Report()
	}
	return view
}

// effectiveArchiveDir 计算实际归档目录：自定义 dir 优先，否则用 server 配置的默认。
func (s *Server) effectiveArchiveDir(cfg appsettings.Archive) string {
	if dir := strings.TrimSpace(cfg.Dir); dir != "" {
		return dir
	}
	return s.archiveBaseDir
}

// latestHoldingSymbols 从最近一次组合巡检 Job 提取持仓标的（已规范化，去重）。
func (s *Server) latestHoldingSymbols() []string {
	if s.portfolioStore == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	jobs, err := s.portfolioStore.List(ctx, 10)
	if err != nil {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0)
	for _, job := range jobs {
		for _, h := range job.Request.Holdings {
			sym := strings.ToUpper(strings.TrimSpace(h.Symbol))
			if sym == "" {
				continue
			}
			if _, ok := seen[sym]; ok {
				continue
			}
			seen[sym] = struct{}{}
			out = append(out, sym)
		}
		if len(out) > 0 {
			return out
		}
	}
	return out
}

// resolveArchiveSymbols 按来源解析最终采集清单。
func (s *Server) resolveArchiveSymbols(cfg appsettings.Archive) []string {
	switch cfg.SymbolsSource {
	case appsettings.ArchiveSymbolsSourceHoldings:
		if holdings := s.latestHoldingSymbols(); len(holdings) > 0 {
			return holdings
		}
		// 无持仓时回退手填，再回退默认
		if len(cfg.Symbols) > 0 {
			return append([]string(nil), cfg.Symbols...)
		}
		return append([]string(nil), DefaultArchiveSymbols...)
	default:
		if len(cfg.Symbols) > 0 {
			return append([]string(nil), cfg.Symbols...)
		}
		return append([]string(nil), DefaultArchiveSymbols...)
	}
}

// archiveConfigGet 返回归档配置视图（含解析后的实际清单预览）。
//
//	GET /api/v1/archive/config
func (s *Server) archiveConfigGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"data": s.buildArchiveConfigView()})
}

// archiveConfigUpdate 保存归档配置到 settings.json。仅更新提供的字段。
//
//	PUT /api/v1/archive/config
func (s *Server) archiveConfigUpdate(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var req struct {
		Enabled         *bool     `json:"enabled"`
		IntervalSeconds *int      `json:"interval_seconds"`
		Dir             *string   `json:"dir"`
		SymbolsSource   *string   `json:"symbols_source"`
		Symbols         *[]string `json:"symbols"`
		TradingOnly     *bool     `json:"trading_only"`
		NewsLimit       *int      `json:"news_limit"`
	}
	if err := decoder.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid archive config: "+err.Error())
		return
	}
	if err := ensureJSONEOF(decoder); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if s.settingsStore == nil {
		writeError(w, http.StatusServiceUnavailable, "settings store unavailable")
		return
	}
	_, err := s.settingsStore.Update(func(values *appsettings.Values) error {
		next := appsettings.NormalizeArchive(values.Archive)
		if req.Enabled != nil {
			next.Enabled = *req.Enabled
		}
		if req.IntervalSeconds != nil {
			next.IntervalSeconds = *req.IntervalSeconds
		}
		if req.Dir != nil {
			next.Dir = strings.TrimSpace(*req.Dir)
		}
		if req.SymbolsSource != nil {
			next.SymbolsSource = *req.SymbolsSource
		}
		if req.Symbols != nil {
			next.Symbols = append([]string(nil), (*req.Symbols)...)
		}
		if req.TradingOnly != nil {
			next.TradingOnly = *req.TradingOnly
		}
		if req.NewsLimit != nil {
			next.NewsLimit = *req.NewsLimit
		}
		values.Archive = appsettings.NormalizeArchive(next)
		return nil
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": s.buildArchiveConfigView()})
}
