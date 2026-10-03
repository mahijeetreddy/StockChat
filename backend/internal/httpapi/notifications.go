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
	stop := make(chan struct{})
	defer close(stop)
	go sse.heartbeat(s.opts.HeartbeatInterval, stop)
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
