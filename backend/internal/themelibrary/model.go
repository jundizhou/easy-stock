package themelibrary

// Theme is one 开盘啦题材. StockCount is the deduplicated member count from the
// upstream list; Relations counts the category memberships (a stock can belong
// to more than one sub-industry inside the same theme).
type Theme struct {
	ID             int64   `json:"id,omitempty"`
	SourceID       string  `json:"source_id"`
	Name           string  `json:"name"`
	Description    string  `json:"description"`
	Introduction   string  `json:"introduction,omitempty"`
	ChangePercent  float64 `json:"change_percent"`
	StockCount     int     `json:"stock_count"`
	LimitUpCount   int     `json:"limit_up_count"`
	RisingCount    int     `json:"rising_count"`
	FallingCount   int     `json:"falling_count"`
	FlatCount      int     `json:"flat_count"`
	Hot            float64 `json:"hot"`
	GroupCount     int     `json:"group_count"`
	RelationCount  int     `json:"relation_count"`
	ThemeUpdatedAt string  `json:"theme_updated_at"`
	AsOf           string  `json:"as_of"`
	SourceURL      string  `json:"source_url"`
	SourceAPIURL   string  `json:"source_api_url,omitempty"`
	Groups         []Group `json:"groups,omitempty"`
}

// Group is a sub-industry node in a theme's category tree.
type Group struct {
	SourceID       string   `json:"source_id"`
	Name           string   `json:"name"`
	ParentSourceID string   `json:"parent_source_id,omitempty"`
	ParentName     string   `json:"parent_name,omitempty"`
	Path           []string `json:"path,omitempty"`
	ChangePercent  float64  `json:"change_percent"`
	LimitUpCount   int      `json:"limit_up_count"`
	StockCount     int      `json:"stock_count"`
	Stocks         []Stock  `json:"stocks"`
}

// Stock is a theme member. Description and Tags are kept per membership
// because upstream writes theme-specific business context for the same code.
type Stock struct {
	Code          string   `json:"code"`
	Name          string   `json:"name"`
	Price         float64  `json:"price"`
	ChangePercent float64  `json:"change_percent"`
	IsLimitUp     bool     `json:"is_limit_up"`
	Hot           float64  `json:"hot"`
	Tags          []string `json:"tags,omitempty"`
	Description   string   `json:"description,omitempty"`
}

type Meta struct {
	SchemaVersion   int    `json:"schema_version"`
	Source          string `json:"source"`
	SourceName      string `json:"source_name"`
	SourceURL       string `json:"source_url"`
	APIURL          string `json:"api_url"`
	SourceUpdatedAt string `json:"source_updated_at"`
	GeneratedAt     string `json:"generated_at"`
	Selection       string `json:"selection"`
}

type Stats struct {
	Themes    int `json:"themes"`
	Groups    int `json:"groups"`
	Relations int `json:"relations"`
	Stocks    int `json:"stocks"`
}

// Library is the list payload consumed by the 题材库 workspace.
type Library struct {
	Available bool    `json:"available"`
	Message   string  `json:"message,omitempty"`
	Meta      Meta    `json:"meta"`
	Stats     Stats   `json:"stats"`
	Total     int     `json:"total"`
	Items     []Theme `json:"items"`
}

type Filter struct {
	Query string
	Sort  string
	Limit int
}

type Seed struct {
	SchemaVersion   int     `json:"schema_version"`
	Source          string  `json:"source"`
	SourceName      string  `json:"source_name"`
	SourceURL       string  `json:"source_url"`
	APIURL          string  `json:"api_url"`
	SourceUpdatedAt string  `json:"source_updated_at"`
	GeneratedAt     string  `json:"generated_at"`
	Selection       string  `json:"selection"`
	ThemeCount      int     `json:"theme_count"`
	Themes          []Theme `json:"themes"`
}
