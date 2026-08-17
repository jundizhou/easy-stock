package review

import "time"

type Post struct {
	ID            string    `json:"id"`
	Source        string    `json:"source"`
	ExternalID    string    `json:"external_id"`
	AuthorID      string    `json:"author_id"`
	AuthorName    string    `json:"author_name"`
	Title         string    `json:"title"`
	Digest        string    `json:"digest"`
	ContentText   string    `json:"content_text"`
	CoverURL      string    `json:"cover_url,omitempty"`
	OriginalURL   string    `json:"original_url"`
	PublishedAt   time.Time `json:"published_at"`
	FetchedAt     time.Time `json:"fetched_at"`
	RelatedStocks []string  `json:"related_stocks"`
	RelatedThemes []string  `json:"related_themes"`
	Read          bool      `json:"read"`
	Favorite      bool      `json:"favorite"`
	AISummary     string    `json:"ai_summary,omitempty"`
	AIKeyPoints   []string  `json:"ai_key_points"`
	AIOutlook     string    `json:"ai_outlook,omitempty"`
	AIAnalyzedAt  time.Time `json:"ai_analyzed_at,omitempty"`
	AIError       string    `json:"ai_error,omitempty"`
}

type Subscription struct {
	ID          string    `json:"id"`
	Source      string    `json:"source"`
	Name        string    `json:"name"`
	HomepageURL string    `json:"homepage_url"`
	ExternalID  string    `json:"external_id,omitempty"`
	ConfigID    string    `json:"config_id,omitempty"`
	Enabled     bool      `json:"enabled"`
	LastSyncAt  time.Time `json:"last_sync_at,omitempty"`
	NextSyncAt  time.Time `json:"next_sync_at,omitempty"`
	LastStatus  string    `json:"last_status"`
	LastError   string    `json:"last_error,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

type SyncResult struct {
	SubscriptionID string `json:"subscription_id"`
	Found          int    `json:"found"`
	Imported       int    `json:"imported"`
	Analyzed       int    `json:"analyzed"`
	Error          string `json:"error,omitempty"`
}

type Author struct {
	ID        string    `json:"id"`
	Source    string    `json:"source"`
	Name      string    `json:"name"`
	PostCount int       `json:"post_count"`
	LatestAt  time.Time `json:"latest_at"`
}

type AuthorDeleteResult struct {
	AuthorID             string `json:"author_id"`
	AuthorName           string `json:"author_name"`
	Source               string `json:"source"`
	PostsDeleted         int64  `json:"posts_deleted"`
	SubscriptionsDeleted int64  `json:"subscriptions_deleted"`
	SummaryCacheCleared  bool   `json:"summary_cache_cleared"`
}

type Query struct {
	Source   string
	AuthorID string
	Keyword  string
	Limit    int
	Offset   int
}

type SourceStatus struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	Message     string `json:"message"`
	ImportReady bool   `json:"import_ready"`
	SyncReady   bool   `json:"sync_ready"`
}

type DailyConsensus struct {
	Topic        string   `json:"topic"`
	Conclusion   string   `json:"conclusion"`
	SupportCount int      `json:"support_count"`
	Authors      []string `json:"authors"`
	Evidence     []string `json:"evidence"`
}

type DailyDisagreement struct {
	Topic     string                      `json:"topic"`
	Views     []string                    `json:"views"`
	Authors   []string                    `json:"authors"`
	Positions []DailyDisagreementPosition `json:"positions"`
}

type DailyDisagreementPosition struct {
	Author   string `json:"author"`
	Stance   string `json:"stance"`
	View     string `json:"view"`
	Evidence string `json:"evidence"`
}

type DailyStockView struct {
	Name         string   `json:"name"`
	Symbol       string   `json:"symbol,omitempty"`
	Logic        string   `json:"logic"`
	SupportCount int      `json:"support_count"`
	Authors      []string `json:"authors"`
	Evidence     []string `json:"evidence"`
	Trigger      string   `json:"trigger,omitempty"`
	Invalidation string   `json:"invalidation,omitempty"`
	Risk         string   `json:"risk,omitempty"`
}

type DailyPlaybook struct {
	PreOpen  []string `json:"pre_open"`
	Opening  []string `json:"opening"`
	Intraday []string `json:"intraday"`
	Close    []string `json:"close"`
}

type DailySummarySource struct {
	PostID      string `json:"post_id,omitempty"`
	Author      string `json:"author"`
	Title       string `json:"title"`
	Source      string `json:"source"`
	URL         string `json:"url,omitempty"`
	PublishedAt string `json:"published_at,omitempty"`
}

type DailyAuthorView struct {
	Author                string               `json:"author"`
	Source                string               `json:"source"`
	ArticleCount          int                  `json:"article_count"`
	AvailableArticleCount int                  `json:"available_article_count"`
	TimeRange             string               `json:"time_range"`
	CoreView              string               `json:"core_view"`
	MarketInterpretation  string               `json:"market_interpretation"`
	ViewEvolution         []string             `json:"view_evolution"`
	Themes                []string             `json:"themes"`
	TodaySurprises        []DailyStockView     `json:"today_surprises"`
	TomorrowFocus         []DailyStockView     `json:"tomorrow_focus"`
	TomorrowOutlook       string               `json:"tomorrow_outlook"`
	Catalysts             []string             `json:"catalysts"`
	Risks                 []string             `json:"risks"`
	Confidence            string               `json:"confidence"`
	Evidence              []string             `json:"evidence"`
	Sources               []DailySummarySource `json:"sources"`
}

type DailyMarketFramework struct {
	Cycle                string `json:"cycle"`
	CapitalPricing       string `json:"capital_pricing"`
	DirectionCompetition string `json:"direction_competition"`
	TradingMethod        string `json:"trading_method"`
}

type DailyScenario struct {
	Key          string   `json:"key"`
	Name         string   `json:"name"`
	Summary      string   `json:"summary"`
	Trigger      string   `json:"trigger"`
	Confirmation string   `json:"confirmation"`
	Invalidation string   `json:"invalidation"`
	Focus        []string `json:"focus"`
}

type DailyDirectionView struct {
	Name              string   `json:"name"`
	Stance            string   `json:"stance"`
	Summary           string   `json:"summary"`
	SupportingAuthors []string `json:"supporting_authors"`
	OpposingAuthors   []string `json:"opposing_authors"`
	Stocks            []string `json:"stocks"`
	Trigger           string   `json:"trigger"`
	Invalidation      string   `json:"invalidation"`
	Risks             []string `json:"risks"`
}

type DailySummary struct {
	TradeDate             string               `json:"trade_date"`
	GeneratedAt           time.Time            `json:"generated_at"`
	PromptVersion         string               `json:"prompt_version"`
	WindowStart           time.Time            `json:"window_start"`
	WindowEnd             time.Time            `json:"window_end"`
	FreshnessRule         string               `json:"freshness_rule"`
	ArticleCount          int                  `json:"article_count"`
	AuthorCount           int                  `json:"author_count"`
	Authors               []string             `json:"authors"`
	Sources               []DailySummarySource `json:"sources"`
	AuthorViews           []DailyAuthorView    `json:"author_views"`
	ExecutiveSummary      string               `json:"executive_summary"`
	MarketRegime          string               `json:"market_regime"`
	MarketAnalysis        string               `json:"market_analysis"`
	MarketFramework       DailyMarketFramework `json:"market_framework"`
	Consensus             []DailyConsensus     `json:"consensus"`
	Disagreements         []DailyDisagreement  `json:"disagreements"`
	Scenarios             []DailyScenario      `json:"scenarios"`
	Directions            []DailyDirectionView `json:"directions"`
	TodaySurprises        []DailyStockView     `json:"today_surprises"`
	TomorrowFocus         []DailyStockView     `json:"tomorrow_focus"`
	TomorrowOutlook       string               `json:"tomorrow_outlook"`
	TomorrowPlaybook      DailyPlaybook        `json:"tomorrow_playbook"`
	Catalysts             []string             `json:"catalysts"`
	Risks                 []string             `json:"risks"`
	VerificationChecklist []string             `json:"verification_checklist"`
	Limitations           []string             `json:"limitations"`
}

type DailySummaryJob struct {
	TradeDate        string    `json:"trade_date"`
	WindowStart      time.Time `json:"window_start,omitempty"`
	WindowEnd        time.Time `json:"window_end,omitempty"`
	FreshnessRule    string    `json:"freshness_rule,omitempty"`
	Status           string    `json:"status"`
	Stage            string    `json:"stage"`
	CompletedAuthors int       `json:"completed_authors"`
	TotalAuthors     int       `json:"total_authors"`
	ArticleCount     int       `json:"article_count"`
	Message          string    `json:"message"`
	Error            string    `json:"error,omitempty"`
	StartedAt        time.Time `json:"started_at,omitempty"`
	UpdatedAt        time.Time `json:"updated_at,omitempty"`
	CompletedAt      time.Time `json:"completed_at,omitempty"`
	SummaryAvailable bool      `json:"summary_available"`
}

type DailySummaryWindow struct {
	TradeDate     string    `json:"trade_date"`
	WindowStart   time.Time `json:"window_start"`
	WindowEnd     time.Time `json:"window_end"`
	FreshnessRule string    `json:"freshness_rule"`
}

// DailyValidationSnapshot is the immutable market evidence captured for a
// next-trading-day review. It deliberately contains only the fields needed to
// explain a result, so the validation can be reproduced without re-querying
// live providers later.
type DailyValidationSnapshot struct {
	CapturedAt  time.Time                 `json:"captured_at"`
	TradeDate   string                    `json:"trade_date"`
	Indexes     []DailyValidationIndex    `json:"indexes"`
	Emotion     *DailyValidationEmotion   `json:"emotion,omitempty"`
	Intraday    *DailyValidationIntraday  `json:"intraday,omitempty"`
	Themes      []DailyValidationTheme    `json:"themes"`
	Industries  []DailyValidationIndustry `json:"industries"`
	Flows       []DailyValidationFlow     `json:"flows"`
	LimitUp     DailyValidationLimitUp    `json:"limit_up"`
	Stocks      []DailyValidationStock    `json:"stocks"`
	DataQuality []string                  `json:"data_quality"`
}

type DailyValidationIndex struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	ChangePercent float64 `json:"change_percent"`
	TradeDate     string  `json:"trade_date,omitempty"`
	Source        string  `json:"source,omitempty"`
}

type DailyValidationEmotion struct {
	Phase              string  `json:"phase"`
	EmotionScore       float64 `json:"emotion_score"`
	Heat               float64 `json:"heat"`
	Profit             float64 `json:"profit"`
	Structure          float64 `json:"structure"`
	LimitUpCount       int     `json:"limit_up_count"`
	LimitDownCount     int     `json:"limit_down_count"`
	BrokenCount        int     `json:"broken_count"`
	FirstBoardCount    int     `json:"first_board_count"`
	BoardCount         int     `json:"board_count"`
	MaxStreak          int     `json:"max_streak"`
	PreviousLimitUpRet float64 `json:"previous_limit_up_return"`
	FinalBreakRate     float64 `json:"final_break_rate"`
	AdvanceRate        float64 `json:"advance_rate"`
	HighAverageReturn  float64 `json:"high_average_return"`
	HighAdvanceRate    float64 `json:"high_advance_rate"`
	HeightCollapse     int     `json:"height_collapse"`
	HighRiskScore      float64 `json:"high_risk_score"`
	QuoteCoverage      float64 `json:"quote_coverage"`
	Confidence         string  `json:"confidence,omitempty"`
}

type DailyValidationIntraday struct {
	TradeDate         string  `json:"trade_date,omitempty"`
	Status            string  `json:"status"`
	Breadth           string  `json:"breadth"`
	RiskScore         float64 `json:"risk_score"`
	CurrentMaxStreak  int     `json:"current_max_streak"`
	HeightCollapse    int     `json:"height_collapse"`
	HighAverageReturn float64 `json:"high_average_return"`
	HighDownRate      float64 `json:"high_down_rate"`
	HighAdvanceRate   float64 `json:"high_advance_rate"`
	LimitUpCount      int     `json:"limit_up_count"`
	BoardCount        int     `json:"board_count"`
	FirstBoardCount   int     `json:"first_board_count"`
	Confidence        string  `json:"confidence,omitempty"`
}

type DailyValidationTheme struct {
	Name          string   `json:"name"`
	ChangePercent float64  `json:"change_percent"`
	NetInflow     float64  `json:"net_inflow"`
	RisingCount   int      `json:"rising_count"`
	FallingCount  int      `json:"falling_count"`
	LimitUpCount  int      `json:"limit_up_count"`
	MaxStreak     int      `json:"max_streak"`
	TrendScore    int      `json:"trend_score"`
	Stage         string   `json:"stage,omitempty"`
	Leaders       []string `json:"leaders,omitempty"`
	Source        string   `json:"source,omitempty"`
}

type DailyValidationIndustry struct {
	Name          string  `json:"name"`
	ChangePercent float64 `json:"change_percent"`
	NetInflow     float64 `json:"net_inflow"`
	RisingCount   int     `json:"rising_count"`
	FallingCount  int     `json:"falling_count"`
	Score         float64 `json:"score"`
	LeaderName    string  `json:"leader_name,omitempty"`
}

type DailyValidationFlow struct {
	Dimension     string  `json:"dimension"`
	Name          string  `json:"name"`
	ChangePercent float64 `json:"change_percent"`
	NetInflow     float64 `json:"net_inflow"`
	MainNetInflow float64 `json:"main_net_inflow"`
	LeaderName    string  `json:"leader_name,omitempty"`
}

type DailyValidationLimitUp struct {
	CurrentTradeDate   string                 `json:"current_trade_date"`
	PreviousTradeDate  string                 `json:"previous_trade_date"`
	CurrentCount       int                    `json:"current_count"`
	PreviousCount      int                    `json:"previous_count"`
	CurrentBoardCount  int                    `json:"current_board_count"`
	PreviousBoardCount int                    `json:"previous_board_count"`
	CurrentMaxStreak   int                    `json:"current_max_streak"`
	PreviousMaxStreak  int                    `json:"previous_max_streak"`
	Concepts           []DailyValidationTheme `json:"concepts,omitempty"`
}

type DailyValidationStock struct {
	Name          string  `json:"name"`
	Symbol        string  `json:"symbol"`
	Open          float64 `json:"open"`
	High          float64 `json:"high"`
	Low           float64 `json:"low"`
	Price         float64 `json:"price"`
	PreviousClose float64 `json:"previous_close"`
	ChangePercent float64 `json:"change_percent"`
	TradeDate     string  `json:"trade_date,omitempty"`
	LimitUpStreak int     `json:"limit_up_streak,omitempty"`
	Source        string  `json:"source,omitempty"`
	Matched       bool    `json:"matched"`
	MatchNote     string  `json:"match_note,omitempty"`
}

type DailyValidationMarketResult struct {
	ExpectedRegime string   `json:"expected_regime"`
	ActualPhase    string   `json:"actual_phase"`
	Verdict        string   `json:"verdict"`
	Summary        string   `json:"summary"`
	Evidence       []string `json:"evidence"`
}

type DailyValidationScenarioResult struct {
	ExpectedKey  string `json:"expected_key"`
	ExpectedName string `json:"expected_name"`
	ActualKey    string `json:"actual_key"`
	ActualName   string `json:"actual_name"`
	Verdict      string `json:"verdict"`
	Summary      string `json:"summary"`
}

type DailyValidationDirectionResult struct {
	Name         string   `json:"name"`
	Verdict      string   `json:"verdict"`
	Score        int      `json:"score"`
	Rank         int      `json:"rank,omitempty"`
	ActualChange float64  `json:"actual_change,omitempty"`
	Evidence     []string `json:"evidence"`
}

type DailyValidationStockResult struct {
	Name            string   `json:"name"`
	Symbol          string   `json:"symbol,omitempty"`
	Verdict         string   `json:"verdict"`
	ActualChange    float64  `json:"actual_change,omitempty"`
	OpenChange      float64  `json:"open_change,omitempty"`
	IntradayHigh    float64  `json:"intraday_high,omitempty"`
	IntradayLow     float64  `json:"intraday_low,omitempty"`
	TriggerHit      string   `json:"trigger_hit,omitempty"`
	InvalidationHit string   `json:"invalidation_hit,omitempty"`
	Summary         string   `json:"summary"`
	Evidence        []string `json:"evidence"`
}

type DailyValidationChecklistResult struct {
	Text     string   `json:"text"`
	Verdict  string   `json:"verdict"`
	Evidence []string `json:"evidence"`
}

type DailyValidation struct {
	SummaryDate      string                           `json:"summary_date"`
	VerificationDate string                           `json:"verification_date"`
	GeneratedAt      time.Time                        `json:"generated_at"`
	PromptVersion    string                           `json:"prompt_version"`
	SummaryHash      string                           `json:"summary_hash"`
	Status           string                           `json:"status"`
	AIStatus         string                           `json:"ai_status"`
	AIMessage        string                           `json:"ai_message,omitempty"`
	Score            float64                          `json:"score"`
	Coverage         float64                          `json:"coverage"`
	CorrectCount     int                              `json:"correct_count"`
	PartialCount     int                              `json:"partial_count"`
	WrongCount       int                              `json:"wrong_count"`
	UnverifiedCount  int                              `json:"unverified_count"`
	Headline         string                           `json:"headline"`
	ActualScenario   string                           `json:"actual_scenario"`
	Market           DailyValidationMarketResult      `json:"market"`
	Scenario         DailyValidationScenarioResult    `json:"scenario"`
	Directions       []DailyValidationDirectionResult `json:"directions"`
	Stocks           []DailyValidationStockResult     `json:"stocks"`
	Checklist        []DailyValidationChecklistResult `json:"checklist"`
	RealizedRisks    []string                         `json:"realized_risks"`
	Lessons          []string                         `json:"lessons"`
	DataQuality      []string                         `json:"data_quality"`
	Snapshot         DailyValidationSnapshot          `json:"snapshot"`
}

type DailyValidationJob struct {
	SummaryDate      string    `json:"summary_date"`
	VerificationDate string    `json:"verification_date,omitempty"`
	Status           string    `json:"status"`
	Stage            string    `json:"stage"`
	Message          string    `json:"message"`
	Error            string    `json:"error,omitempty"`
	StartedAt        time.Time `json:"started_at,omitempty"`
	UpdatedAt        time.Time `json:"updated_at,omitempty"`
	CompletedAt      time.Time `json:"completed_at,omitempty"`
	ResultAvailable  bool      `json:"result_available"`
}
