package foundation

import "time"

type HotStockRankItem struct {
	Symbol string
	Name   string
	Rank   int
}

type HotStockRankList struct {
	Source     string
	SourceName string
	Items      []HotStockRankItem
	FetchedAt  time.Time
	Error      string
}
