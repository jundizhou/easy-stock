package httpapi

import (
	"context"
	"net/http"
	"time"

	"easy-stock/backend/internal/foundation"
)

type ETFSubscriptionProvider interface {
	ETFSubscriptions(context.Context) (foundation.ETFSubscriptionDashboard, foundation.SourceMeta, error)
}

func (s *Server) etfSubscriptionsHandler(w http.ResponseWriter, r *http.Request) {
	if s.etfSubscriptions == nil {
		writeError(w, http.StatusServiceUnavailable, "ETF subscription provider is unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()
	data, meta, err := loadMarketOverview(ctx, s.marketSnapshots, "etf-subscriptions", s.etfSubscriptions.ETFSubscriptions)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data, "meta": meta})
}
