package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

func (s *Server) listEvents(w http.ResponseWriter, r *http.Request) {
	if s.events == nil {
		writeJSON(w, http.StatusOK, map[string]any{"items": []any{}, "dropped": 0})
		return
	}
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 1000 {
			writeError(w, http.StatusBadRequest, "INVALID_PAGE", "Pagination values were not accepted.")
			return
		}
		limit = value
	}
	events, dropped, err := s.events.Snapshot(limit)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "EVENTS_UNAVAILABLE", "Operational events are unavailable.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": events, "dropped": dropped})
}

func (s *Server) streamEvents(w http.ResponseWriter, r *http.Request) {
	if s.events == nil {
		writeError(w, http.StatusServiceUnavailable, "EVENTS_UNAVAILABLE", "Operational event streaming is unavailable.")
		return
	}
	streamSSE(w, r, "event", s.events.Subscribe(r.Context()))
}

func (s *Server) streamTraffic(w http.ResponseWriter, r *http.Request) {
	if s.traffic == nil {
		writeError(w, http.StatusServiceUnavailable, "TRAFFIC_UNAVAILABLE", "Live traffic streaming is unavailable.")
		return
	}
	streamSSE(w, r, "traffic", s.traffic.Subscribe(r.Context()))
}

func streamSSE[T any](w http.ResponseWriter, r *http.Request, eventType string, events <-chan T) {
	if events == nil {
		writeError(w, http.StatusServiceUnavailable, "STREAM_CAPACITY", "Live stream capacity is full.")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "STREAM_UNAVAILABLE", "Streaming is unavailable on this server.")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, "retry: 5000\n\n")
	flusher.Flush()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case event, open := <-events:
			if !open {
				return
			}
			data, err := json.Marshal(event)
			if err != nil {
				continue
			}
			if _, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, data); err != nil {
				return
			}
			flusher.Flush()
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}
