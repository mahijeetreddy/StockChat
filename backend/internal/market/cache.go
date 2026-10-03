package market

import (
	"context"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/mahijeetreddy/stockchat/backend/internal/domain"
)

// CacheTTLs configures how long each kind of response is cached.
type CacheTTLs struct {
	Quote          time.Duration
	Search         time.Duration
	Profile        time.Duration
	News           time.Duration
	HistoryIntra   time.Duration // 1D, 5D
	HistoryDaily   time.Duration // 1M and longer
	MarketStatus   time.Duration
	MaxEntries     int
	CleanupEveryOp int // sweep expired entries every N writes
}

// DefaultCacheTTLs are the TTLs suggested in PLAN.md section 6.
func DefaultCacheTTLs() CacheTTLs {
	return CacheTTLs{
		Quote:          10 * time.Second,
		Search:         24 * time.Hour,
		Profile:        24 * time.Hour,
		News:           5 * time.Minute,
		HistoryIntra:   time.Minute,
		HistoryDaily:   time.Hour,
		MarketStatus:   time.Minute,
		MaxEntries:     5000,
		CleanupEveryOp: 200,
	}
}

type cacheEntry struct {
	val     any
	expires time.Time
}

// Cache is a Provider decorator with per-method TTLs. Concurrent identical
// requests are collapsed with singleflight so upstream sees one call.
// Errors are never cached.
type Cache struct {
	next Provider
	ttl  CacheTTLs
	now  func() time.Time

	mu      sync.Mutex
	entries map[string]cacheEntry
	writes  int
	group   singleflight.Group
}

// WithCache wraps next with a TTL cache.
func WithCache(next Provider, ttl CacheTTLs) *Cache {
	return &Cache{next: next, ttl: ttl, now: time.Now, entries: make(map[string]cacheEntry)}
}

// Invalidate drops every cached entry (used when the mock price is nudged).
func (c *Cache) Invalidate() {
	c.mu.Lock()
	c.entries = make(map[string]cacheEntry)
	c.mu.Unlock()
}

func (c *Cache) get(key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok || !c.now().Before(e.expires) {
		return nil, false
	}
	return e.val, true
}

func (c *Cache) put(key string, val any, ttl time.Duration) {
	if ttl <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	c.writes++
	if c.ttl.CleanupEveryOp > 0 && c.writes%c.ttl.CleanupEveryOp == 0 {
		for k, e := range c.entries {
			if !now.Before(e.expires) {
				delete(c.entries, k)
			}
		}
	}
	if c.ttl.MaxEntries > 0 && len(c.entries) >= c.ttl.MaxEntries {
		// Simple bound: drop everything. Good enough for a single-user app.
		c.entries = make(map[string]cacheEntry)
	}
	c.entries[key] = cacheEntry{val: val, expires: now.Add(ttl)}
}

// cached runs fetch at most once per key concurrently and caches successes.
func cached[T any](ctx context.Context, c *Cache, key string, ttl time.Duration, fetch func(context.Context) (T, error)) (T, error) {
	if v, ok := c.get(key); ok {
		return v.(T), nil
	}
	ch := c.group.DoChan(key, func() (any, error) {
		// Re-check: another flight may have filled the cache meanwhile.
		if v, ok := c.get(key); ok {
			return v, nil
		}
		// Detach from the first caller's cancellation so one disconnecting
		// client doesn't fail every waiter; keep a hard timeout instead.
		fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		v, err := fetch(fctx)
		if err != nil {
			return nil, err
		}
		c.put(key, v, ttl)
		return v, nil
	})
	var zero T
	select {
	case <-ctx.Done():
		return zero, ctx.Err()
	case res := <-ch:
		if res.Err != nil {
			return zero, res.Err
		}
		return res.Val.(T), nil
	}
}

// SearchSymbol implements Provider.
func (c *Cache) SearchSymbol(ctx context.Context, query string) ([]domain.SymbolMatch, error) {
	return cached(ctx, c, "search:"+query, c.ttl.Search, func(ctx context.Context) ([]domain.SymbolMatch, error) {
		return c.next.SearchSymbol(ctx, query)
	})
}

// Quote implements Provider.
func (c *Cache) Quote(ctx context.Context, symbol string) (domain.Quote, error) {
	return cached(ctx, c, "quote:"+symbol, c.ttl.Quote, func(ctx context.Context) (domain.Quote, error) {
		return c.next.Quote(ctx, symbol)
	})
}

// History implements Provider.
func (c *Cache) History(ctx context.Context, symbol string, r domain.HistoryRange) ([]domain.Candle, error) {
	ttl := c.ttl.HistoryDaily
	if r.Intraday() {
		ttl = c.ttl.HistoryIntra
	}
	return cached(ctx, c, "history:"+symbol+":"+string(r), ttl, func(ctx context.Context) ([]domain.Candle, error) {
		return c.next.History(ctx, symbol, r)
	})
}

// Profile implements Provider.
func (c *Cache) Profile(ctx context.Context, symbol string) (domain.CompanyProfile, error) {
	return cached(ctx, c, "profile:"+symbol, c.ttl.Profile, func(ctx context.Context) (domain.CompanyProfile, error) {
		return c.next.Profile(ctx, symbol)
	})
}

// News implements Provider. from/to are keyed at day granularity so repeated
// "last week" requests within a day share an entry.
func (c *Cache) News(ctx context.Context, symbol string, from, to time.Time, limit int) ([]domain.NewsItem, error) {
	key := fmt.Sprintf("news:%s:%s:%s:%d", symbol, from.UTC().Format(time.DateOnly), to.UTC().Format(time.DateOnly), limit)
	return cached(ctx, c, key, c.ttl.News, func(ctx context.Context) ([]domain.NewsItem, error) {
		return c.next.News(ctx, symbol, from, to, limit)
	})
}

// MarketOpen implements Provider.
func (c *Cache) MarketOpen(ctx context.Context) (bool, error) {
	return cached(ctx, c, "market_open", c.ttl.MarketStatus, c.next.MarketOpen)
}
