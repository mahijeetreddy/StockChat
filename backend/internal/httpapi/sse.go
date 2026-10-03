package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// sseWriter writes Server-Sent Events. Safe for concurrent use (events and
// heartbeats come from different goroutines).
type sseWriter struct {
	mu sync.Mutex
	w  http.ResponseWriter
	rc *http.ResponseController
}

func newSSEWriter(w http.ResponseWriter) *sseWriter {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache, no-transform")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no") // disable proxy buffering (nginx)
	w.WriteHeader(http.StatusOK)
	s := &sseWriter{w: w, rc: http.NewResponseController(w)}
	_ = s.rc.Flush()
	return s
}

// event writes "event: <name>\ndata: <json>\n\n" and flushes.
func (s *sseWriter) event(name string, data any) error {
	b, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("sse marshal %s: %w", name, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", name, b); err != nil {
		return err
	}
	return s.rc.Flush()
}

// comment writes a ": <text>" line (heartbeat).
func (s *sseWriter) comment(text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := fmt.Fprintf(s.w, ": %s\n\n", text); err != nil {
		return err
	}
	return s.rc.Flush()
}

// startHeartbeat sends ": ping" every interval in the background. The
// returned func stops it and waits for the goroutine to exit, so nothing
// writes to the ResponseWriter after the handler returns.
func (s *sseWriter) startHeartbeat(interval time.Duration) (stop func()) {
	done := make(chan struct{})
	exited := make(chan struct{})
	go func() {
		defer close(exited)
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				if s.comment("ping") != nil {
					return
				}
			}
		}
	}()
	return func() {
		close(done)
		<-exited
	}
}
