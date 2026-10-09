package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/sector"
)

func (s *Server) loadThemeIndexMembers(ctx context.Context, theme, snapshotID string) (foundation.SectorMap, error) {
	key := theme + ":"
	if snapshotID != "" {
		key += "@" + snapshotID
	}
	snapshot, err := s.themeSnapshots.load(ctx, key, func(ctx context.Context) (foundation.SectorMap, error) {
		if provider, ok := s.sectorMap.(SnapshotSectorMapProvider); ok {
			return provider.BuildSnapshot(ctx, theme, snapshotID)
		}
		return s.sectorMap.Build(ctx, theme)
	})
	return snapshot.sectorMap, err
}

func (s *Server) themeIndexHandler(w http.ResponseWriter, r *http.Request) {
	theme := strings.TrimSpace(r.URL.Query().Get("theme"))
	snapshot := strings.TrimSpace(r.URL.Query().Get("snapshot_id"))
	if theme == "" || len(theme) > 2048 || len(snapshot) > 128 {
		writeError(w, http.StatusBadRequest, "invalid theme or snapshot_id")
		return
	}
	limit, err := positiveIntQuery(r, "limit", 240, 300)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if r.URL.Query().Get("refresh") == "1" {
		s.themeIndex.Refresh(theme, snapshot)
	}
	value, err := s.themeIndex.Series(r.Context(), theme, snapshot, limit)
	if err != nil {
		if errors.Is(err, sector.ErrSnapshotExpired) {
			writeError(w, http.StatusGone, "题材快照已更新，请刷新题材列表")
			return
		}
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": value})
}
