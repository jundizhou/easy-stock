package foundation

import "time"

type ETFSubscriptionPoint struct {
	Date           string   `json:"date"`
	PreviousDate   string   `json:"previous_date"`
	Shares         *float64 `json:"shares"` // shares, not 万份
	PreviousShares *float64 `json:"previous_shares"`
	ShareChange    *float64 `json:"share_change"`
	Close          *float64 `json:"close"`
	PriceDate      string   `json:"price_date"`
	EstimatedNet   *float64 `json:"estimated_net"` // CNY, approximate NAV with matching-day close
	Verification   string   `json:"verification"`
}

type ETFSubscriptionSeries struct {
	Code          string                 `json:"code"`
	Name          string                 `json:"name"`
	Index         string                 `json:"index"`
	Exchange      string                 `json:"exchange"`
	Points        []ETFSubscriptionPoint `json:"points"`
	LatestPrice   *float64               `json:"latest_price"`
	ChangePercent *float64               `json:"change_percent"`
	QuoteTime     *time.Time             `json:"quote_time"`
}

type ETFSubscriptionDay struct {
	Date         string   `json:"date"`
	PreviousDate string   `json:"previous_date"`
	EstimatedNet *float64 `json:"estimated_net"`
	Inflow       *float64 `json:"inflow"`
	Outflow      *float64 `json:"outflow"`
	Coverage     int      `json:"coverage"`
	Subscribed   int      `json:"subscribed"`
	Redeemed     int      `json:"redeemed"`
}

type ETFSubscriptionCheck struct {
	Code           string   `json:"code"`
	Date           string   `json:"date"`
	SnapshotShares *float64 `json:"snapshot_shares"`
	OfficialShares *float64 `json:"official_shares"`
	Status         string   `json:"status"`
}

type ETFSubscriptionDashboard struct {
	TodayTrial         *ETFSubscriptionTrial   `json:"today_trial"`
	StartDate          string                  `json:"start_date"`
	SourceDate         string                  `json:"source_date"`
	SourceGeneratedAt  *time.Time              `json:"source_generated_at"`
	ReportDate         string                  `json:"report_date"`
	LatestDate         string                  `json:"latest_date"`
	LatestCompleteDate string                  `json:"latest_complete_date"`
	Expected           int                     `json:"expected"`
	Days               []ETFSubscriptionDay    `json:"days"`
	Series             []ETFSubscriptionSeries `json:"series"`
	Checks             []ETFSubscriptionCheck  `json:"checks"`
	Warnings           []string                `json:"warnings"`
}

// Today's trial uses explicitly dated current-day shares and a current-day quote.
// It never relabels yesterday's outstanding shares as today's merely because the
// quote timestamp changed. Monetary totals remain null until the pool is complete.
type ETFSubscriptionTrialItem struct {
	Code           string     `json:"code"`
	Name           string     `json:"name"`
	ShareDate      string     `json:"share_date"`
	Shares         *float64   `json:"shares"`
	PreviousShares *float64   `json:"previous_shares"`
	ShareChange    *float64   `json:"share_change"`
	Price          *float64   `json:"price"`
	QuoteTime      *time.Time `json:"quote_time"`
	EstimatedNet   *float64   `json:"estimated_net"`
	Status         string     `json:"status"`
}

type ETFSubscriptionTrial struct {
	Date             string                     `json:"date"`
	PreviousDate     string                     `json:"previous_date"`
	Expected         int                        `json:"expected"`
	ShareCoverage    int                        `json:"share_coverage"`
	BaselineCoverage int                        `json:"baseline_coverage"`
	PriceCoverage    int                        `json:"price_coverage"`
	Coverage         int                        `json:"coverage"`
	EstimatedNet     *float64                   `json:"estimated_net"`
	CoveredNet       *float64                   `json:"covered_net"`
	Status           string                     `json:"status"`
	Items            []ETFSubscriptionTrialItem `json:"items"`
}
