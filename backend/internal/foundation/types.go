package foundation

import "time"

type SourceMeta struct {
	Source          string     `json:"source"`
	SourceURL       string     `json:"source_url,omitempty"`
	AvailableFields []string   `json:"available_fields,omitempty"`
	FetchedAt       time.Time  `json:"fetched_at"`
	LatencyMS       int64      `json:"latency_ms"`
	Stale           bool       `json:"stale"`
	TradeDate       string     `json:"trade_date,omitempty"`
	SnapshotID      string     `json:"snapshot_id,omitempty"`
	NextRefreshAt   *time.Time `json:"next_refresh_at,omitempty"`
	FallbackReason  string     `json:"fallback_reason,omitempty"`
	CarryForward    bool       `json:"carry_forward,omitempty"`
}

type Quote struct {
	Symbol        string     `json:"symbol"`
	Name          string     `json:"name"`
	Price         float64    `json:"price"`
	Open          float64    `json:"open"`
	PreviousClose float64    `json:"previous_close"`
	High          float64    `json:"high"`
	Low           float64    `json:"low"`
	Change        float64    `json:"change"`
	ChangePercent float64    `json:"change_percent"`
	TradeTime     time.Time  `json:"trade_time,omitempty"`
	Meta          SourceMeta `json:"meta"`
}

type KLine struct {
	Symbol        string     `json:"symbol"`
	Time          time.Time  `json:"time"`
	Open          float64    `json:"open"`
	High          float64    `json:"high"`
	Low           float64    `json:"low"`
	Close         float64    `json:"close"`
	Volume        float64    `json:"volume"`
	Amount        float64    `json:"amount"`
	TurnoverRate  float64    `json:"turnover_rate,omitempty"`
	ChangePercent float64    `json:"change_percent,omitempty"`
	Meta          SourceMeta `json:"meta"`
}

type NewsItem struct {
	ID          string     `json:"id,omitempty"`
	Title       string     `json:"title"`
	Content     string     `json:"content,omitempty"`
	URL         string     `json:"url,omitempty"`
	PublishedAt time.Time  `json:"published_at,omitempty"`
	Tags        []string   `json:"tags,omitempty"`
	Meta        SourceMeta `json:"meta"`
}

type SourceHealth struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Category  string    `json:"category"`
	OK        bool      `json:"ok"`
	Message   string    `json:"message,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
}

type Board struct {
	Code           string     `json:"code"`
	Name           string     `json:"name"`
	ChangePercent  float64    `json:"change_percent"`
	TotalMarketCap float64    `json:"total_market_cap"`
	FloatMarketCap float64    `json:"float_market_cap"`
	MainNetInflow  float64    `json:"main_net_inflow"`
	Meta           SourceMeta `json:"meta"`
}

type BoardStock struct {
	Symbol               string     `json:"symbol"`
	Name                 string     `json:"name"`
	Price                float64    `json:"price"`
	Change               float64    `json:"change"`
	ChangePercent        float64    `json:"change_percent"`
	FiveDayChangePercent float64    `json:"five_day_change_percent,omitempty"`
	Volume               float64    `json:"volume"`
	Amount               float64    `json:"amount"`
	TotalMarketCap       float64    `json:"total_market_cap"`
	FloatMarketCap       float64    `json:"float_market_cap"`
	MainNetInflow        float64    `json:"main_net_inflow"`
	LimitUpStreak        int        `json:"limit_up_streak,omitempty"`
	LimitUpDays          int        `json:"limit_up_days,omitempty"`
	LimitUpCount         int        `json:"limit_up_count,omitempty"`
	FirstLimitTime       string     `json:"first_limit_time,omitempty"`
	LastLimitTime        string     `json:"last_limit_time,omitempty"`
	FirstLimitDate       string     `json:"first_limit_date,omitempty"`
	LastLimitDate        string     `json:"last_limit_date,omitempty"`
	LimitRegime          string     `json:"limit_regime,omitempty"`
	RankScore            int        `json:"rank_score,omitempty"`
	RankRole             string     `json:"rank_role,omitempty"`
	Meta                 SourceMeta `json:"meta"`
}

// StockCatalogEntry is one A-share from EastMoney's stock-selection catalog.
// Industry and Concepts are membership evidence used to build thematic
// constituent pools without maintaining stock-code lists in local rules.
type StockCatalogEntry struct {
	BoardStock
	Industry string
	Concepts []string
}

type LimitUpEvent struct {
	Symbol          string     `json:"symbol"`
	Name            string     `json:"name"`
	Date            time.Time  `json:"date"`
	Price           float64    `json:"price"`
	ChangePercent   float64    `json:"change_percent"`
	Amount          float64    `json:"amount"`
	FloatMarketCap  float64    `json:"float_market_cap"`
	TurnoverRate    float64    `json:"turnover_rate"`
	Streak          int        `json:"streak"`
	FirstLimitTime  string     `json:"first_limit_time"`
	LastLimitTime   string     `json:"last_limit_time"`
	OpenCount       int        `json:"open_count"`
	Industry        string     `json:"industry"`
	Days            int        `json:"days"`
	Count           int        `json:"count"`
	Concepts        []string   `json:"concepts,omitempty"`
	PrimaryTheme    string     `json:"primary_theme,omitempty"`
	ThemeSource     string     `json:"theme_source,omitempty"`
	ThemeRank       int        `json:"theme_rank,omitempty"`
	ThemeLeaderRole string     `json:"theme_leader_role,omitempty"`
	StreakLabel     string     `json:"streak_label,omitempty"`
	BoardType       string     `json:"board_type,omitempty"`
	Meta            SourceMeta `json:"meta"`
}

// StockThemeAttribution is an authoritative or cached per-stock theme label.
// It keeps source provenance so downstream analysis can prefer Kaipanla data
// without conflating it with broad industry/catalog fallbacks.
type StockThemeAttribution struct {
	Symbol    string   `json:"symbol"`
	Theme     string   `json:"theme"`
	Concepts  []string `json:"concepts,omitempty"`
	Source    string   `json:"source"`
	TradeDate string   `json:"trade_date,omitempty"`
	Rank      int      `json:"rank,omitempty"`
	Role      string   `json:"role,omitempty"`
}

// MarketLimitEvent represents one stock in a daily limit-event pool that is
// not necessarily still sealed at the close. It is used for final broken-board
// and limit-down pools while LimitUpEvent remains the richer sealed-limit-up
// record used by the ladder.
type MarketLimitEvent struct {
	Symbol        string     `json:"symbol"`
	Name          string     `json:"name"`
	Date          time.Time  `json:"date"`
	Price         float64    `json:"price"`
	ChangePercent float64    `json:"change_percent"`
	Amount        float64    `json:"amount"`
	Industry      string     `json:"industry,omitempty"`
	Meta          SourceMeta `json:"meta"`
}

type SectorMap struct {
	Theme     string           `json:"theme"`
	Name      string           `json:"name"`
	Tabs      []string         `json:"tabs"`
	ThemeTabs []SectorMapTab   `json:"theme_tabs,omitempty"`
	Groups    []SectorMapGroup `json:"groups"`
	Meta      SourceMeta       `json:"meta"`
}

type SectorMapTab struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ThemeOverview struct {
	Theme                string   `json:"theme"`
	Name                 string   `json:"name"`
	ChangePercent        float64  `json:"change_percent"`
	MainNetInflow        float64  `json:"main_net_inflow"`
	RisingNodes          int      `json:"rising_nodes"`
	FallingNodes         int      `json:"falling_nodes"`
	MatchedNodes         int      `json:"matched_nodes"`
	TotalNodes           int      `json:"total_nodes"`
	TopNode              string   `json:"top_node,omitempty"`
	TopNodeChangePercent float64  `json:"top_node_change_percent"`
	TrendScore           int      `json:"trend_score,omitempty"`
	DailyStrengthScore   int      `json:"daily_strength_score,omitempty"`
	FiveDayStrengthScore int      `json:"five_day_strength_score,omitempty"`
	TrendStage           string   `json:"trend_stage,omitempty"`
	LimitUpCount         int      `json:"limit_up_count,omitempty"`
	BoardCount           int      `json:"board_count,omitempty"`
	PreviousCount        int      `json:"previous_count,omitempty"`
	ActiveDays           int      `json:"active_days,omitempty"`
	MaxStreak            int      `json:"max_streak,omitempty"`
	Leaders              []string `json:"leaders,omitempty"`
	Source               string   `json:"source,omitempty"`
	SourceRank           int      `json:"source_rank,omitempty"`
	ProviderRank         int      `json:"provider_rank,omitempty"`
	SourceStrength       float64  `json:"source_strength,omitempty"`
	TradeDate            string   `json:"trade_date,omitempty"`
	SnapshotID           string   `json:"snapshot_id,omitempty"`
	CarryForward         bool     `json:"carry_forward,omitempty"`
	Provisional          bool     `json:"provisional,omitempty"`
}

type SectorMapGroup struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Nodes []SectorMapNode `json:"nodes"`
}

type SectorMapNode struct {
	ID             string       `json:"id"`
	Name           string       `json:"name"`
	Description    string       `json:"description,omitempty"`
	BoardCode      string       `json:"board_code,omitempty"`
	BoardName      string       `json:"board_name,omitempty"`
	BoardSource    string       `json:"board_source,omitempty"`
	ChangePercent  float64      `json:"change_percent"`
	MainNetInflow  float64      `json:"main_net_inflow"`
	Stocks         []BoardStock `json:"stocks"`
	StockSource    string       `json:"stock_source,omitempty"`
	MatchStatus    string       `json:"match_status"`
	MatchedBy      []string     `json:"matched_by,omitempty"`
	Warnings       []string     `json:"warnings,omitempty"`
	CandidateCount int          `json:"candidate_count,omitempty"`
}
