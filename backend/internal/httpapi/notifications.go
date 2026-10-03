package httpapi

import (
	"net/http"
)

// handleNotifications streams alert-fired events as SSE ("event: alert").
func (s *Server) handleNotifications(w http.ResponseWriter, r *http.Request) {
	if s.hub == nil {
		writeError(w, http.StatusServiceUnavailable, "notifications are disabled")
		return
	}
	ch, unsubscribe := s.hub.Subscribe()
	defer unsubscribe()

	sse := newSSEWriter(w)
	stopHeartbeat := sse.startHeartbeat(s.opts.HeartbeatInterval)
	defer stopHeartbeat()
	_ = sse.comment("connected")

	for {
		select {
		case <-r.Context().Done():
			return
		case n, ok := <-ch:
			if !ok {
				return
			}
			if err := sse.event("alert", n); err != nil {
				return
			}
		}
	}
}
