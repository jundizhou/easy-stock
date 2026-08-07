package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
	"github.com/gorilla/websocket"
)

type streamMessage struct {
	Type      string             `json:"type"`
	Quotes    []foundation.Quote `json:"quotes,omitempty"`
	Error     string             `json:"error,omitempty"`
	FetchedAt time.Time          `json:"fetched_at"`
}

var streamUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func (s *Server) stream(w http.ResponseWriter, r *http.Request) {
	symbolsParam := strings.TrimSpace(r.URL.Query().Get("symbols"))
	if symbolsParam == "" {
		writeError(w, http.StatusBadRequest, "symbols is required")
		return
	}
	symbols, err := foundation.SplitSymbols(symbolsParam)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	interval := parseStreamInterval(r.URL.Query().Get("interval_ms"))
	conn, err := streamUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	if !s.writeQuoteSnapshot(r.Context(), conn, symbols) {
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if !s.writeQuoteSnapshot(r.Context(), conn, symbols) {
				return
			}
		}
	}
}

func parseStreamInterval(raw string) time.Duration {
	if raw == "" {
		return 3 * time.Second
	}
	ms, err := strconv.Atoi(raw)
	if err != nil || ms <= 0 {
		return 3 * time.Second
	}
	if ms < 500 {
		return 500 * time.Millisecond
	}
	if ms > 60000 {
		return 60 * time.Second
	}
	return time.Duration(ms) * time.Millisecond
}

func (s *Server) writeQuoteSnapshot(ctx context.Context, conn *websocket.Conn, symbols []string) bool {
	quotes, err := s.realtimeProvider.Realtime(ctx, symbols)
	message := streamMessage{
		Type:      "quotes",
		Quotes:    quotes,
		FetchedAt: time.Now(),
	}
	if err != nil {
		message.Type = "error"
		message.Error = err.Error()
	}
	return conn.WriteJSON(message) == nil
}
