package api

import (
	"net/http"

	"github.com/nguyenduytan/proxysieve/pkg/auth"
)

func (s *Server) cacheStats(w http.ResponseWriter) {
	if s.responseCache == nil {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": true, "stats": s.responseCache.Stats(s.now())})
}

func (s *Server) purgeCache(w http.ResponseWriter, r *http.Request, user auth.User) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if s.responseCache == nil {
		writeError(w, http.StatusConflict, "CACHE_DISABLED", "The response cache is disabled.")
		return
	}
	removed := s.responseCache.Purge()
	s.record(r.Context(), user, "cache.purged", "cache", "response")
	writeJSON(w, http.StatusOK, map[string]any{"purged": removed, "stats": s.responseCache.Stats(s.now())})
}
