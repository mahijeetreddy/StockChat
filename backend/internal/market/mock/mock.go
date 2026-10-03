// Package mock is a deterministic, offline market.Provider used for tests,
// evals, offline development, and demos. Prices follow a smooth pseudo-random
// curve seeded by symbol, so the same symbol and time always give the same data.
package mock

import (
	"context"
	"fmt"
	"hash/fnv"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mahijeetreddy/stockchat/backend/internal/domain"
	"github.com/mahijeetreddy/stockchat/backend/internal/market"
)

// MarketMode controls what MarketOpen reports.
type MarketMode string

// Market modes.
const (
	MarketAuto   MarketMode = "auto"   // follow real US regular hours
	MarketOpen   MarketMode = "open"   // always open
	MarketClosed MarketMode = "closed" // always closed
)

type company struct {
	Symbol, Name, Exchange, Industry, Type string
	Base                                   float64 // approximate price level
	CapB                                   float64 // market cap in billions USD (0 for ETFs)
}

// universe is the fixed set of symbols the mock knows about. Read-only.
var universe = []company{
	{"AAPL", "Apple Inc", "NASDAQ", "Technology", "Common Stock", 232, 3500},
	{"APLE", "Apple Hospitality REIT Inc", "NYSE", "Real Estate", "Common Stock", 15, 3.6},
	{"MSFT", "Microsoft Corp", "NASDAQ", "Technology", "Common Stock", 431, 3200},
	{"NVDA", "NVIDIA Corp", "NASDAQ", "Semiconductors", "Common Stock", 126, 3100},
	{"AMD", "Advanced Micro Devices Inc", "NASDAQ", "Semiconductors", "Common Stock", 158, 255},
	{"INTC", "Intel Corp", "NASDAQ", "Semiconductors", "Common Stock", 23, 100},
	{"TSLA", "Tesla Inc", "NASDAQ", "Automobiles", "Common Stock", 248, 790},
	{"COST", "Costco Wholesale Corp", "NASDAQ", "Retail", "Common Stock", 905, 400},
	{"META", "Meta Platforms Inc", "NASDAQ", "Media", "Common Stock", 572, 1450},
	{"MMAT", "Meta Materials Inc", "NASDAQ", "Electrical Equipment", "Common Stock", 1.2, 0.01},
	{"GOOGL", "Alphabet Inc Class A", "NASDAQ", "Media", "Common Stock", 166, 2050},
	{"AMZN", "Amazon.com Inc", "NASDAQ", "Retail", "Common Stock", 186, 1950},
	{"NFLX", "Netflix Inc", "NASDAQ", "Media", "Common Stock", 705, 300},
	{"JPM", "JPMorgan Chase & Co", "NYSE", "Banking", "Common Stock", 211, 600},
	{"V", "Visa Inc", "NYSE", "Financial Services", "Common Stock", 281, 550},
	{"KO", "Coca-Cola Co", "NYSE", "Beverages", "Common Stock", 71, 305},
	{"DIS", "Walt Disney Co", "NYSE", "Media", "Common Stock", 95, 172},
	{"DAL", "Delta Air Lines Inc", "NYSE", "Airlines", "Common Stock", 50, 32},
	{"DLA", "Delta Apparel Inc", "NYSE", "Textiles", "Common Stock", 3.1, 0.02},
	{"BRK.B", "Berkshire Hathaway Inc Class B", "NYSE", "Insurance", "Common Stock", 455, 985},
	{"WMT", "Walmart Inc", "NYSE", "Retail", "Common Stock", 80, 650},
	{"IBM", "International Business Machines Corp", "NYSE", "Technology", "Common Stock", 221, 205},
	{"ORCL", "Oracle Corp", "NYSE", "Technology", "Common Stock", 170, 470},
	{"BA", "Boeing Co", "NYSE", "Aerospace & Defense", "Common Stock", 158, 97},
	{"SPY", "SPDR S&P 500 ETF Trust", "NYSE ARCA", "", "ETP", 572, 0},
}

// Provider is the mock market data provider. Safe for concurrent use.
type Provider struct {
	now func() time.Time

	mu        sync.RWMutex
	mode      MarketMode
	overrides map[string]float64           // symbol -> forced current price (demo nudges)
	extraNews map[string][]domain.NewsItem // symbol -> injected items (eval fixtures)
}

// Option configures a Provider.
type Option func(*Provider)

// WithClock overrides the time source (tests).
func WithClock(now func() time.Time) Option { return func(p *Provider) { p.now = now } }

// WithMarketMode sets the initial market mode.
func WithMarketMode(m MarketMode) Option { return func(p *Provider) { p.mode = m } }

// New returns a mock provider.
func New(opts ...Option) *Provider {
	p := &Provider{
		now:       time.Now,
		mode:      MarketAuto,
		overrides: make(map[string]float64),
		extraNews: make(map[string][]domain.NewsItem),
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

// SetMarketMode changes whether the market reports open or closed.
func (p *Provider) SetMarketMode(m MarketMode) {
	p.mu.Lock()
	p.mode = m
	p.mu.Unlock()
}

// SetPrice forces the current price of a symbol (demo mode for alerts).
// A price <= 0 clears the override.
func (p *Provider) SetPrice(symbol string, price float64) error {
	if _, ok := lookup(symbol); !ok {
		return fmt.Errorf("mock set price %s: %w", symbol, market.ErrNotFound)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if price <= 0 {
		delete(p.overrides, symbol)
		return nil
	}
	p.overrides[symbol] = price
	return nil
}

// AddNews injects an extra news item for a symbol (used by eval fixtures).
func (p *Provider) AddNews(symbol string, item domain.NewsItem) {
	p.mu.Lock()
	p.extraNews[symbol] = append(p.extraNews[symbol], item)
	p.mu.Unlock()
}

func lookup(symbol string) (company, bool) {
	for _, c := range universe {
		if c.Symbol == symbol {
			return c, true
		}
	}
	return company{}, false
}

// ---- deterministic price model ----

func hash01(parts ...string) float64 {
	h := fnv.New64a()
	for _, p := range parts {
		_, _ = h.Write([]byte(p))
		_, _ = h.Write([]byte{0})
	}
	return float64(h.Sum64()%1_000_000) / 1_000_000
}

// logPrice returns log(price) for symbol at time t. It is a sum of slow
// sinusoids with symbol-specific phases plus small hashed noise, anchored so
// that prices stay near the company's base level.
func logPrice(c company, t time.Time) float64 {
	days := float64(t.Unix()) / 86400
	ph := func(k string) float64 { return 2 * math.Pi * hash01(c.Symbol, k) }
	amp := 0.6 + 0.8*hash01(c.Symbol, "amp") // per-symbol volatility scale
	v := amp * (0.12*math.Sin(2*math.Pi*days/400+ph("a")) +
		0.07*math.Sin(2*math.Pi*days/90+ph("b")) +
		0.035*math.Sin(2*math.Pi*days/23+ph("c")) +
		0.015*math.Sin(2*math.Pi*days/4.3+ph("d")))
	// Intraday wiggle (period ~2.2h) and per-minute noise.
	minute := t.Unix() / 60
	v += amp * 0.004 * math.Sin(2*math.Pi*float64(t.Unix())/7900+ph("e"))
	v += amp * 0.002 * (hash01(c.Symbol, fmt.Sprint(minute)) - 0.5)
	return math.Log(c.Base) + v
}

func priceAt(c company, t time.Time) float64 { return round2(math.Exp(logPrice(c, t))) }

func round2(f float64) float64 { return math.Round(f*100) / 100 }

func volumeAt(c company, t time.Time, bar time.Duration) float64 {
	capB := c.CapB
	if capB <= 0 {
		capB = 500
	}
	perDay := 2e6 + 4e7*math.Sqrt(capB/3000)
	frac := bar.Hours() / 6.5
	if bar >= 24*time.Hour {
		frac = bar.Hours() / 24
	}
	return math.Round(perDay * frac * (0.6 + 0.8*hash01(c.Symbol, "vol", t.Format(time.RFC3339))))
}

// session returns the open and close of the regular session on t's NY date.
func session(t time.Time) (time.Time, time.Time) {
	ny := t.In(market.NewYork)
	open := time.Date(ny.Year(), ny.Month(), ny.Day(), 9, 30, 0, 0, market.NewYork)
	return open, open.Add(390 * time.Minute)
}

func (p *Provider) isOpen(now time.Time) bool {
	p.mu.RLock()
	mode := p.mode
	p.mu.RUnlock()
	switch mode {
	case MarketOpen:
		return true
	case MarketClosed:
		return false
	default:
		return market.IsRegularHours(now)
	}
}

// ---- Provider implementation ----

// SearchSymbol implements market.Provider. Exact ticker matches rank first,
// then name prefix matches, then substring matches.
func (p *Provider) SearchSymbol(ctx context.Context, query string) ([]domain.SymbolMatch, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil, nil
	}
	type scored struct {
		c     company
		score int
	}
	var hits []scored
	for _, c := range universe {
		sym, name := strings.ToLower(c.Symbol), strings.ToLower(c.Name)
		switch {
		case sym == q:
			hits = append(hits, scored{c, 0})
		case strings.HasPrefix(name, q):
			hits = append(hits, scored{c, 1})
		case strings.HasPrefix(sym, q) && len(q) >= 2:
			hits = append(hits, scored{c, 2})
		case strings.Contains(name, q) && len(q) >= 3:
			hits = append(hits, scored{c, 3})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score < hits[j].score })
	out := make([]domain.SymbolMatch, 0, len(hits))
	for _, h := range hits {
		out = append(out, domain.SymbolMatch{Symbol: h.c.Symbol, Description: strings.ToUpper(h.c.Name), Type: h.c.Type})
	}
	return out, nil
}

// Quote implements market.Provider.
func (p *Provider) Quote(ctx context.Context, symbol string) (domain.Quote, error) {
	if err := ctx.Err(); err != nil {
		return domain.Quote{}, err
	}
	c, ok := lookup(symbol)
	if !ok {
		return domain.Quote{}, fmt.Errorf("mock quote %s: %w", symbol, market.ErrNotFound)
	}
	now := p.now()
	open := p.isOpen(now)

	// Session we are reporting: today's if open, else the last completed one.
	var sessOpen, asOf time.Time
	if open {
		sessOpen, _ = session(now)
		asOf = now.Truncate(15 * time.Second)
	} else {
		lc := market.LastClose(now)
		sessOpen, _ = session(lc)
		asOf = lc
	}
	prevClose := priceAt(c, market.LastClose(sessOpen))
	price := priceAt(c, asOf)

	p.mu.RLock()
	if ov, ok := p.overrides[symbol]; ok {
		price = ov
	}
	p.mu.RUnlock()

	hi, lo := price, price
	for t := sessOpen; t.Before(asOf); t = t.Add(5 * time.Minute) {
		v := priceAt(c, t)
		hi, lo = math.Max(hi, v), math.Min(lo, v)
	}
	change := round2(price - prevClose)
	return domain.Quote{
		Symbol:        symbol,
		Price:         price,
		Change:        change,
		ChangePercent: round2(change / prevClose * 100),
		High:          hi,
		Low:           lo,
		Open:          priceAt(c, sessOpen),
		PrevClose:     prevClose,
		AsOf:          asOf.UTC(),
		MarketOpen:    open,
	}, nil
}

// History implements market.Provider. Bars end at the current time (if the
// market is open) or the last close, and skip weekends.
func (p *Provider) History(ctx context.Context, symbol string, r domain.HistoryRange) ([]domain.Candle, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !r.Valid() {
		return nil, fmt.Errorf("mock history: invalid range %q", r)
	}
	c, ok := lookup(symbol)
	if !ok {
		return nil, fmt.Errorf("mock history %s: %w", symbol, market.ErrNotFound)
	}
	now := p.now()
	spec := r.BarSpec()
	var times []time.Time
	if r.Intraday() {
		end := market.LastClose(now)
		if p.isOpen(now) {
			end = now.Truncate(spec.Interval)
		}
		// Walk back bar by bar through regular sessions only.
		for t := end.Add(-spec.Interval); len(times) < spec.Count; {
			if market.IsRegularHours(t) {
				times = append(times, t)
				t = t.Add(-spec.Interval)
				continue
			}
			// Jump to the last bar of the previous session.
			t = market.LastClose(t).Add(-spec.Interval)
		}
	} else {
		day := market.LastClose(now)
		step := 1
		if spec.Interval >= 7*24*time.Hour {
			step = 7
		}
		for len(times) < spec.Count {
			o, _ := session(day)
			times = append(times, o)
			day = day.AddDate(0, 0, -step)
			for day.Weekday() == time.Saturday || day.Weekday() == time.Sunday {
				day = day.AddDate(0, 0, -1)
			}
		}
	}
	sort.Slice(times, func(i, j int) bool { return times[i].Before(times[j]) })

	out := make([]domain.Candle, 0, len(times))
	for _, t := range times {
		end := t.Add(spec.Interval)
		if !r.Intraday() {
			end = t.Add(390 * time.Minute)
		}
		o, cl := priceAt(c, t), priceAt(c, end)
		mid := priceAt(c, t.Add(end.Sub(t)/2))
		spread := 0.002 + 0.004*hash01(c.Symbol, "hl", t.String())
		out = append(out, domain.Candle{
			Time:   t.UTC(),
			Open:   o,
			High:   round2(math.Max(math.Max(o, cl), mid) * (1 + spread)),
			Low:    round2(math.Min(math.Min(o, cl), mid) * (1 - spread)),
			Close:  cl,
			Volume: volumeAt(c, t, spec.Interval),
		})
	}
	return out, nil
}

// Profile implements market.Provider.
func (p *Provider) Profile(ctx context.Context, symbol string) (domain.CompanyProfile, error) {
	if err := ctx.Err(); err != nil {
		return domain.CompanyProfile{}, err
	}
	c, ok := lookup(symbol)
	if !ok || c.Type == "ETP" {
		return domain.CompanyProfile{}, fmt.Errorf("mock profile %s: %w", symbol, market.ErrNotFound)
	}
	return domain.CompanyProfile{
		Symbol:    c.Symbol,
		Name:      c.Name,
		Exchange:  c.Exchange,
		Industry:  c.Industry,
		Country:   "US",
		Currency:  "USD",
		MarketCap: c.CapB * 1e9,
	}, nil
}

var headlineTemplates = []struct{ headline, summary, source string }{
	{"%s shares move as analysts revisit estimates", "Several analysts updated their models for %s ahead of the next earnings report.", "MarketWire"},
	{"What to watch from %s's upcoming earnings", "Investors are focused on margins and guidance from %s.", "Finance Daily"},
	{"%s announces new product lineup", "%s unveiled updates to its product portfolio at an industry event.", "TechBeat"},
	{"Options traders position for volatility in %s", "Implied volatility for %s options rose this week.", "Options Desk"},
	{"%s executive comments on industry outlook", "A senior %s executive discussed demand trends in an interview.", "Business Journal"},
	{"Institutional ownership in %s shifts, filings show", "Quarterly filings show changes in fund positions in %s.", "Filings Monitor"},
}

// News implements market.Provider. Items are synthetic and clearly generic.
func (p *Provider) News(ctx context.Context, symbol string, from, to time.Time, limit int) ([]domain.NewsItem, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c, ok := lookup(symbol)
	if !ok {
		return nil, fmt.Errorf("mock news %s: %w", symbol, market.ErrNotFound)
	}
	now := p.now()
	var items []domain.NewsItem
	p.mu.RLock()
	items = append(items, p.extraNews[symbol]...)
	p.mu.RUnlock()
	for i, tpl := range headlineTemplates {
		published := now.Add(-time.Duration(3+i*19) * time.Hour).Truncate(time.Hour)
		items = append(items, domain.NewsItem{
			Headline:    fmt.Sprintf(tpl.headline, c.Name),
			Summary:     fmt.Sprintf(tpl.summary, c.Name),
			Source:      tpl.source,
			URL:         fmt.Sprintf("https://example.com/news/%s/%d", strings.ToLower(c.Symbol), i+1),
			PublishedAt: published.UTC(),
		})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].PublishedAt.After(items[j].PublishedAt) })
	out := items[:0]
	for _, it := range items {
		if (!from.IsZero() && it.PublishedAt.Before(from)) || (!to.IsZero() && it.PublishedAt.After(to)) {
			continue
		}
		out = append(out, it)
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// MarketOpen implements market.Provider.
func (p *Provider) MarketOpen(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return p.isOpen(p.now()), nil
}
