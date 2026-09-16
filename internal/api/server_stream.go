package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

func (s *Server) streamTraffic(w http.ResponseWriter, r *http.Request) {
	if s.traffic == nil {
		writeError(w, http.StatusServiceUnavailable, "TRAFFIC_UNAVAILABLE", "Live traffic streaming is unavailable.")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "STREAM_UNAVAILABLE", "Streaming is unavailable on this server.")
		return
	}
	events := s.traffic.Subscribe(r.Context())
	if events == nil {
		writeError(w, http.StatusServiceUnavailable, "STREAM_CAPACITY", "Live traffic stream capacity is full.")
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
			if _, err = fmt.Fprintf(w, "event: traffic\ndata: %s\n\n", data); err != nil {
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
