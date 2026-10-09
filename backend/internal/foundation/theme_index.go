package foundation

// ThemeIndexTarget contains only verified names, never fuzzy search terms.
type ThemeIndexTarget struct {
	Name      string
	BoardCode string
	Names     []string
}

type ThemeIndexQuality struct {
	Constituents              int     `json:"constituents"`
	Sampled                   int     `json:"sampled"`
	Loaded                    int     `json:"loaded"`
	HistoryCoverage           float64 `json:"history_coverage"` // Loaded / sampled; independent of per-session coverage.
	MinimumCoverage           float64 `json:"minimum_coverage"`
	SamplingErrorPercent      float64 `json:"sampling_error_percent"`
	SamplingEstimateAvailable bool    `json:"sampling_estimate_available"`
	EstimatedExtrema          bool    `json:"estimated_extrema"`
	SkippedDays               int     `json:"skipped_days"`
}

type ThemeIndexSeries struct {
	Theme     string            `json:"theme"`
	Name      string            `json:"name"`
	Method    string            `json:"method"`
	IndexCode string            `json:"index_code,omitempty"`
	IndexName string            `json:"index_name,omitempty"`
	BaseValue float64           `json:"base_value,omitempty"`
	BaseDate  string            `json:"base_date,omitempty"`
	Lines     []KLine           `json:"lines"`
	Quality   ThemeIndexQuality `json:"quality"`
	Warnings  []string          `json:"warnings"`
	Meta      SourceMeta        `json:"meta"`
}
