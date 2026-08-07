package duanxianxia

import (
	"time"

	"easy-stock/backend/internal/foundation"
)

const (
	DefaultBaseURL = "https://duanxianxia.com"
	SourceID       = "duanxianxia:kaipanla"
)

type RankPoint struct {
	TradeDate string  `json:"trade_date"`
	Rank      int     `json:"rank"`
	Strength  float64 `json:"strength"`
}

type Leader struct {
	Rank   int    `json:"rank"`
	Role   string `json:"role"`
	Symbol string `json:"symbol"`
	Name   string `json:"name"`
}

type Theme struct {
	Code          string      `json:"code"`
	Name          string      `json:"name"`
	Rank          int         `json:"rank"`
	Strength      float64     `json:"strength"`
	History       []RankPoint `json:"history,omitempty"`
	Leaders       []Leader    `json:"leaders,omitempty"`
	LeadersLoaded bool        `json:"leaders_loaded,omitempty"`
	NoLeaders     bool        `json:"no_leaders,omitempty"`
}

type Snapshot struct {
	ID        string    `json:"id"`
	TradeDate string    `json:"trade_date"`
	FetchedAt time.Time `json:"fetched_at"`
	Themes    []Theme   `json:"themes"`
}

type LimitUpPoolSnapshot struct {
	ID         string                    `json:"id"`
	TradeDate  string                    `json:"trade_date"`
	FetchedAt  time.Time                 `json:"fetched_at"`
	ModifiedAt time.Time                 `json:"modified_at,omitempty"`
	SourceURL  string                    `json:"source_url"`
	ETag       string                    `json:"etag,omitempty"`
	Events     []foundation.LimitUpEvent `json:"events"`
}

func (s Snapshot) FindTheme(code string) (Theme, bool) {
	for _, theme := range s.Themes {
		if theme.Code == code {
			return theme, true
		}
	}
	return Theme{}, false
}

type SyncState struct {
	LastAttemptAt time.Time
	NextAllowedAt time.Time
	LastSuccessAt time.Time
	LastError     string
}

type FetchMeta struct {
	LastAttemptAt time.Time
	NextAllowedAt time.Time
	LastSuccessAt time.Time
	RefreshError  string
	Refreshed     bool
	FromCache     bool
}
