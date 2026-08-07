package marketemotion

import "time"

type RawMetrics struct {
	LimitUpCount       int     `json:"limit_up_count"`
	LimitDownCount     int     `json:"limit_down_count"`
	BrokenCount        int     `json:"broken_count"`
	FirstBoardCount    int     `json:"first_board_count"`
	BoardCount         int     `json:"board_count"`
	MaxStreak          int     `json:"max_streak"`
	ReopenedCount      int     `json:"reopened_count"`
	FinalBreakRate     float64 `json:"final_break_rate"`
	ReopenSuccessRate  float64 `json:"reopen_success_rate"`
	PreviousLimitUpRet float64 `json:"previous_limit_up_return"`
	PreviousBoardRet   float64 `json:"previous_board_return"`
	OpenPremium        float64 `json:"open_premium"`
	CoreReturn         float64 `json:"core_return"`
	AdvanceRate        float64 `json:"advance_rate"`
	ThemeFocus         float64 `json:"theme_focus"`
	LeaderGap          float64 `json:"leader_gap"`
	LadderContinuity   float64 `json:"ladder_continuity"`
	HighSampleCount    int     `json:"high_sample_count"`
	HighWeakCount      int     `json:"high_weak_count"`
	HighKill           int     `json:"high_kill"`
	HighLimitDown      int     `json:"high_limit_down"`
	HighAverageReturn  float64 `json:"high_average_return"`
	HighDownRate       float64 `json:"high_down_rate"`
	HighAdvanceRate    float64 `json:"high_advance_rate"`
	HeightCollapse     int     `json:"height_collapse"`
	HighRiskScore      float64 `json:"high_risk_score"`
	MidKill            int     `json:"mid_kill"`
	LowKill            int     `json:"low_kill"`
	QuoteCoverage      float64 `json:"quote_coverage"`
	LimitUp10CM        int     `json:"limit_up_10cm"`
	LimitUp20CM        int     `json:"limit_up_20cm"`
	LimitUp30CM        int     `json:"limit_up_30cm"`
	MaxStreak10CM      int     `json:"max_streak_10cm"`
	MaxStreak20CM      int     `json:"max_streak_20cm"`
	MaxStreak30CM      int     `json:"max_streak_30cm"`
}

type Scores struct {
	Heat      float64 `json:"heat"`
	Profit    float64 `json:"profit"`
	Structure float64 `json:"structure"`
	Total     float64 `json:"total"`
}

type Snapshot struct {
	ModelVersion   int        `json:"model_version"`
	TradeDate      string     `json:"trade_date"`
	EmotionScore   float64    `json:"emotion_score"`
	Phase          string     `json:"phase"`
	Confidence     string     `json:"confidence"`
	HistorySamples int        `json:"history_samples"`
	Raw            RawMetrics `json:"raw"`
	Scores         Scores     `json:"scores"`
	Source         string     `json:"source"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type SyncState struct {
	LastAttemptDate string    `json:"last_attempt_date,omitempty"`
	LastSuccessDate string    `json:"last_success_date,omitempty"`
	LastError       string    `json:"last_error,omitempty"`
	UpdatedAt       time.Time `json:"updated_at,omitempty"`
}

type CacheStatus struct {
	Mode             string    `json:"mode"`
	CachedDays       int       `json:"cached_days"`
	BootstrapDays    int       `json:"bootstrap_days"`
	LastExternalSync string    `json:"last_external_sync,omitempty"`
	LastError        string    `json:"last_error,omitempty"`
	UpdatedAt        time.Time `json:"updated_at,omitempty"`
}

type IntradayMetrics struct {
	PreviousMaxStreak int     `json:"previous_max_streak"`
	CurrentMaxStreak  int     `json:"current_max_streak"`
	HeightCollapse    int     `json:"height_collapse"`
	HighLevels        []int   `json:"high_levels"`
	HighSampleCount   int     `json:"high_sample_count"`
	HighQuoteCount    int     `json:"high_quote_count"`
	HighAverageReturn float64 `json:"high_average_return"`
	HighDownCount     int     `json:"high_down_count"`
	HighDownRate      float64 `json:"high_down_rate"`
	HighWeakCount     int     `json:"high_weak_count"`
	HighWeakRate      float64 `json:"high_weak_rate"`
	HighSevereCount   int     `json:"high_severe_count"`
	HighSevereRate    float64 `json:"high_severe_rate"`
	HighLimitDown     int     `json:"high_limit_down"`
	HighAdvanceBase   int     `json:"high_advance_base"`
	HighAdvanceCount  int     `json:"high_advance_count"`
	HighAdvanceRate   float64 `json:"high_advance_rate"`
	LimitUpCount      int     `json:"limit_up_count"`
	BoardCount        int     `json:"board_count"`
	FirstBoardCount   int     `json:"first_board_count"`
}

type IntradaySnapshot struct {
	TradeDate      string          `json:"trade_date"`
	BaseTradeDate  string          `json:"base_trade_date,omitempty"`
	SessionStatus  string          `json:"session_status"`
	Status         string          `json:"status"`
	Breadth        string          `json:"breadth"`
	Summary        string          `json:"summary"`
	RiskScore      float64         `json:"risk_score"`
	Confidence     string          `json:"confidence"`
	Metrics        IntradayMetrics `json:"metrics"`
	UpdatedAt      time.Time       `json:"updated_at"`
	NextRefreshAt  time.Time       `json:"next_refresh_at"`
	CacheTTLSecond int             `json:"cache_ttl_seconds"`
	Stale          bool            `json:"stale"`
}

type History struct {
	Points        []Snapshot        `json:"points"`
	Latest        *Snapshot         `json:"latest,omitempty"`
	Intraday      *IntradaySnapshot `json:"intraday,omitempty"`
	IntradayError string            `json:"intraday_error,omitempty"`
	Cache         CacheStatus       `json:"cache"`
}
