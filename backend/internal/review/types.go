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
