package httpapi

import (
	"crypto/subtle"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"golang.org/x/time/rate"
)

// accessLog logs one structured line per request (path only, never the query
// string, which may carry a token for EventSource requests).
func accessLog(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()
			next.ServeHTTP(ww, r)
			logger.Info("http",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
				"dur_ms", time.Since(start).Milliseconds(),
				"req_id", middleware.GetReqID(r.Context()),
			)
		})
	}
}

// cors allows exactly one configured origin.
func cors(origin string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if o := r.Header.Get("Origin"); o != "" && o == origin {
				h := w.Header()
				h.Set("Access-Control-Allow-Origin", origin)
				h.Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
				h.Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
				h.Set("Access-Control-Max-Age", "600")
				h.Add("Vary", "Origin")
			}
			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// bearerAuth requires "Authorization: Bearer <token>" when token is set.
// EventSource can't send headers, so SSE GET endpoints may pass ?token=.
func bearerAuth(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if token == "" {
			return next
		}
		want := []byte(token)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if got == "" && r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/stream/") {
				got = r.URL.Query().Get("token")
			}
			if subtle.ConstantTimeCompare([]byte(got), want) != 1 {
				writeError(w, http.StatusUnauthorized, "missing or invalid token")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// maxBody caps request body size.
func maxBody(n int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, n)
			next.ServeHTTP(w, r)
		})
	}
}

// ipLimiter is a per-client token-bucket rate limiter.
type ipLimiter struct {
	mu      sync.Mutex
	every   time.Duration
	burst   int
	clients map[string]*clientLimiter
	now     func() time.Time
}

type clientLimiter struct {
	lim  *rate.Limiter
	seen time.Time
}

// newIPLimiter allows perMinute requests per client per minute, with burst.
func newIPLimiter(perMinute, burst int) *ipLimiter {
	return &ipLimiter{
		every:   time.Minute / time.Duration(perMinute),
		burst:   burst,
		clients: make(map[string]*clientLimiter),
		now:     time.Now,
	}
}

func (l *ipLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if len(l.clients) > 1000 {
		for k, c := range l.clients {
			if now.Sub(c.seen) > 10*time.Minute {
				delete(l.clients, k)
			}
		}
	}
	c, ok := l.clients[key]
	if !ok {
		c = &clientLimiter{lim: rate.NewLimiter(rate.Every(l.every), l.burst)}
		l.clients[key] = c
	}
	c.seen = now
	return c.lim.AllowN(now, 1)
}

func (l *ipLimiter) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		if !l.allow(host) {
			w.Header().Set("Retry-After", "10")
			writeError(w, http.StatusTooManyRequests, "too many requests; slow down")
			return
		}
		next.ServeHTTP(w, r)
	})
}
