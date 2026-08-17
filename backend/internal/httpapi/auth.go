package httpapi

import (
	"net/http"
	"strings"
)

func (s *Server) withCORS(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if origin != "" {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Vary", "Origin")
	}
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-A-Stock-Token")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
}

func (s *Server) authorized(r *http.Request) bool {
	if s.token == "" || r.URL.Path == "/api/health" || r.Method == http.MethodOptions {
		return true
	}
	if r.URL.Query().Get("token") == s.token {
		return true
	}
	if r.Header.Get("X-A-Stock-Token") == s.token {
		return true
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(auth, "Bearer ") && strings.TrimSpace(strings.TrimPrefix(auth, "Bearer ")) == s.token {
		return true
	}
	return false
}
