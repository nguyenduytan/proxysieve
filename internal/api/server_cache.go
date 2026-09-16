package api

import (
	"net/http"
	"strings"

	"github.com/nguyenduytan/proxysieve/pkg/auth"
	"github.com/nguyenduytan/proxysieve/pkg/proxy"
)

func (s *Server) cacheStats(w http.ResponseWriter) {
	if s.responseCache == nil {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": true, "storage": s.responseCache.Kind(), "stats": s.responseCache.Stats(s.now())})
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
	writeJSON(w, http.StatusOK, map[string]any{"storage": s.responseCache.Kind(), "purged": removed, "stats": s.responseCache.Stats(s.now())})
}

func (s *Server) purgeCacheDomain(w http.ResponseWriter, r *http.Request, user auth.User) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if s.responseCache == nil {
		writeError(w, http.StatusConflict, "CACHE_DISABLED", "The response cache is disabled.")
		return
	}
	var input struct {
		Domain string `json:"domain"`
	}
	if !decode(w, r, &input) {
		return
	}
	input.Domain = strings.ToLower(strings.TrimSuffix(input.Domain, "."))
	if !proxy.ValidHost(input.Domain) {
		writeError(w, http.StatusBadRequest, "INVALID_DOMAIN", "Provide one valid hostname without a port.")
		return
	}
	removed := s.responseCache.PurgeDomain(input.Domain)
	s.record(r.Context(), user, "cache.domain_purged", "domain", input.Domain)
	writeJSON(w, http.StatusOK, map[string]any{"storage": s.responseCache.Kind(), "domain": input.Domain, "purged": removed, "stats": s.responseCache.Stats(s.now())})
}
