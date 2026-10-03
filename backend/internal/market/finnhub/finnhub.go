// Package finnhub implements market.Provider against the Finnhub REST API.
// The API key is sent in the X-Finnhub-Token header, never in the URL.
package finnhub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mahijeetreddy/stockchat/backend/internal/domain"
	"github.com/mahijeetreddy/stockchat/backend/internal/market"
)

// DefaultBaseURL is Finnhub's v1 API root.
const DefaultBaseURL = "https://finnhub.io/api/v1"

const maxBody = 4 << 20 // 4 MiB

// Client is a Finnhub market data client.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
	now     func() time.Time
}

// Option configures a Client.
type Option func(*Client)

// WithBaseURL points the client at a different host (tests).
func WithBaseURL(u string) Option { return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") } }

// WithHTTPClient replaces the HTTP client.
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

// New returns a Finnhub client.
func New(apiKey string, opts ...Option) *Client {
	c := &Client{
		baseURL: DefaultBaseURL,
		apiKey:  apiKey,
		http:    &http.Client{Timeout: 10 * time.Second},
		now:     time.Now,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

var _ market.Provider = (*Client)(nil)

// get performs a GET and decodes JSON into out. Errors never contain the key.
func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	u := c.baseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("X-Finnhub-Token", c.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%s: %w", path, errors.Join(market.ErrUpstream, scrub(err)))
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return fmt.Errorf("%s: read body: %w", path, errors.Join(market.ErrUpstream, err))
	}

	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return fmt.Errorf("%s: %w", path, market.ErrRateLimited)
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return fmt.Errorf("%s: status %d: %w", path, resp.StatusCode, market.ErrNoAccess)
	case resp.StatusCode == http.StatusNotFound:
		return fmt.Errorf("%s: %w", path, market.ErrNotFound)
	case resp.StatusCode >= 300:
		return fmt.Errorf("%s: status %d: %w", path, resp.StatusCode, market.ErrUpstream)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("%s: decode: %w", path, errors.Join(market.ErrUpstream, err))
	}
	return nil
}

// scrub removes anything that looks like a token from transport errors.
// The key lives in a header so it should never appear, but be defensive.
func scrub(err error) error {
	return errors.New(tokenRe.ReplaceAllString(err.Error(), "token=REDACTED"))
}

var tokenRe = regexp.MustCompile(`token=[^&\s"]+`)

// ---- search ----

type searchResp struct {
	Count  int `json:"count"`
	Result []struct {
		Description   string `json:"description"`
		DisplaySymbol string `json:"displaySymbol"`
		Symbol        string `json:"symbol"`
		Type          string `json:"type"`
	} `json:"result"`
}

var usTicker = regexp.MustCompile(`^[A-Z.\-]{1,10}$`)

// isUSTicker accepts plain tickers and share classes ("BRK.B") but rejects
// foreign exchange suffixes ("AAPL.MX", "SHOP.TO").
func isUSTicker(s string) bool {
	if !usTicker.MatchString(s) {
		return false
	}
	if i := strings.LastIndexByte(s, '.'); i >= 0 && len(s)-i-1 > 1 {
		return false
	}
	return true
}

// SearchSymbol implements market.Provider. Results are restricted to US listings.
func (c *Client) SearchSymbol(ctx context.Context, query string) ([]domain.SymbolMatch, error) {
	var r searchResp
	if err := c.get(ctx, "/search", url.Values{"q": {query}, "exchange": {"US"}}, &r); err != nil {
		return nil, fmt.Errorf("finnhub search: %w", err)
	}
	out := make([]domain.SymbolMatch, 0, len(r.Result))
	for _, m := range r.Result {
		if !isUSTicker(m.Symbol) {
			continue
		}
		out = append(out, domain.SymbolMatch{Symbol: m.Symbol, Description: m.Description, Type: m.Type})
	}
	return out, nil
}

// ---- quote ----

type quoteResp struct {
	C  float64 `json:"c"`  // current price
	D  float64 `json:"d"`  // change
	DP float64 `json:"dp"` // percent change
	H  float64 `json:"h"`
	L  float64 `json:"l"`
	O  float64 `json:"o"`
	PC float64 `json:"pc"`
	T  int64   `json:"t"`
}

// Quote implements market.Provider. MarketOpen is left false; callers combine
// with MarketOpen() (which is cached) to avoid an extra request per quote.
func (c *Client) Quote(ctx context.Context, symbol string) (domain.Quote, error) {
	var r quoteResp
	if err := c.get(ctx, "/quote", url.Values{"symbol": {symbol}}, &r); err != nil {
		return domain.Quote{}, fmt.Errorf("finnhub quote %s: %w", symbol, err)
	}
	// Finnhub returns all zeros for unknown symbols.
	if r.C == 0 && r.T == 0 {
		return domain.Quote{}, fmt.Errorf("finnhub quote %s: %w", symbol, market.ErrNotFound)
	}
	return domain.Quote{
		Symbol:        symbol,
		Price:         r.C,
		Change:        r.D,
		ChangePercent: r.DP,
		High:          r.H,
		Low:           r.L,
		Open:          r.O,
		PrevClose:     r.PC,
		AsOf:          time.Unix(r.T, 0).UTC(),
	}, nil
}

// ---- profile ----

type profileResp struct {
	Country              string  `json:"country"`
	Currency             string  `json:"currency"`
	Exchange             string  `json:"exchange"`
	FinnhubIndustry      string  `json:"finnhubIndustry"`
	Logo                 string  `json:"logo"`
	MarketCapitalization float64 `json:"marketCapitalization"` // millions
	Name                 string  `json:"name"`
	Ticker               string  `json:"ticker"`
	WebURL               string  `json:"weburl"`
}

// Profile implements market.Provider.
func (c *Client) Profile(ctx context.Context, symbol string) (domain.CompanyProfile, error) {
	var r profileResp
	if err := c.get(ctx, "/stock/profile2", url.Values{"symbol": {symbol}}, &r); err != nil {
		return domain.CompanyProfile{}, fmt.Errorf("finnhub profile %s: %w", symbol, err)
	}
	if r.Name == "" && r.Ticker == "" {
		return domain.CompanyProfile{}, fmt.Errorf("finnhub profile %s: %w", symbol, market.ErrNotFound)
	}
	return domain.CompanyProfile{
		Symbol:    symbol,
		Name:      r.Name,
		Exchange:  r.Exchange,
		Industry:  r.FinnhubIndustry,
		Country:   r.Country,
		Currency:  r.Currency,
		WebURL:    r.WebURL,
		LogoURL:   r.Logo,
		MarketCap: r.MarketCapitalization * 1e6,
	}, nil
}

// ---- news ----

type newsItem struct {
	Datetime int64  `json:"datetime"`
	Headline string `json:"headline"`
	Source   string `json:"source"`
	Summary  string `json:"summary"`
	URL      string `json:"url"`
}

// News implements market.Provider. Finnhub filters by date; limit is applied here.
func (c *Client) News(ctx context.Context, symbol string, from, to time.Time, limit int) ([]domain.NewsItem, error) {
	q := url.Values{
		"symbol": {symbol},
		"from":   {from.In(market.NewYork).Format(time.DateOnly)},
		"to":     {to.In(market.NewYork).Format(time.DateOnly)},
	}
	var r []newsItem
	if err := c.get(ctx, "/company-news", q, &r); err != nil {
		return nil, fmt.Errorf("finnhub news %s: %w", symbol, err)
	}
	sort.SliceStable(r, func(i, j int) bool { return r[i].Datetime > r[j].Datetime })
	out := make([]domain.NewsItem, 0, len(r))
	for _, n := range r {
		if n.Headline == "" {
			continue
		}
		out = append(out, domain.NewsItem{
			Headline:    n.Headline,
			Summary:     n.Summary,
			Source:      n.Source,
			URL:         n.URL,
			PublishedAt: time.Unix(n.Datetime, 0).UTC(),
		})
		if limit > 0 && len(out) == limit {
			break
		}
	}
	return out, nil
}

// ---- market status ----

type statusResp struct {
	Exchange string `json:"exchange"`
	IsOpen   bool   `json:"isOpen"`
	Session  string `json:"session"`
	Holiday  string `json:"holiday"`
}

// MarketOpen implements market.Provider using /stock/market-status. Only the
// regular session counts as open.
func (c *Client) MarketOpen(ctx context.Context) (bool, error) {
	var r statusResp
	if err := c.get(ctx, "/stock/market-status", url.Values{"exchange": {"US"}}, &r); err != nil {
		return false, fmt.Errorf("finnhub market status: %w", err)
	}
	if r.Session != "" {
		return r.IsOpen && r.Session == "regular", nil
	}
	return r.IsOpen, nil
}

// ---- candles (paid plans only; kept for completeness) ----

type candleResp struct {
	C []float64 `json:"c"`
	H []float64 `json:"h"`
	L []float64 `json:"l"`
	O []float64 `json:"o"`
	T []int64   `json:"t"`
	V []float64 `json:"v"`
	S string    `json:"s"`
}

func resolution(r domain.HistoryRange) string {
	switch r {
	case domain.Range1D:
		return "5"
	case domain.Range5D:
		return "30"
	case domain.Range5Y:
		return "W"
	default:
		return "D"
	}
}

// History implements market.Provider via /stock/candle. On the free plan this
// returns ErrNoAccess; configure HISTORY_PROVIDER to use another source.
func (c *Client) History(ctx context.Context, symbol string, r domain.HistoryRange) ([]domain.Candle, error) {
	to := c.now()
	from := to.Add(-r.Lookback())
	q := url.Values{
		"symbol":     {symbol},
		"resolution": {resolution(r)},
		"from":       {strconv.FormatInt(from.Unix(), 10)},
		"to":         {strconv.FormatInt(to.Unix(), 10)},
	}
	var cr candleResp
	if err := c.get(ctx, "/stock/candle", q, &cr); err != nil {
		return nil, fmt.Errorf("finnhub candles %s: %w", symbol, err)
	}
	if cr.S == "no_data" || len(cr.T) == 0 {
		return nil, fmt.Errorf("finnhub candles %s: %w", symbol, market.ErrNotFound)
	}
	n := len(cr.T)
	if len(cr.C) != n || len(cr.O) != n || len(cr.H) != n || len(cr.L) != n {
		return nil, fmt.Errorf("finnhub candles %s: ragged arrays: %w", symbol, market.ErrUpstream)
	}
	out := make([]domain.Candle, n)
	for i := range n {
		var v float64
		if i < len(cr.V) {
			v = cr.V[i]
		}
		out[i] = domain.Candle{Time: time.Unix(cr.T[i], 0).UTC(), Open: cr.O[i], High: cr.H[i], Low: cr.L[i], Close: cr.C[i], Volume: v}
	}
	return trimToBars(out, r), nil
}

// trimToBars keeps the most recent bars for the range (and only the last
// session for 1D).
func trimToBars(c []domain.Candle, r domain.HistoryRange) []domain.Candle {
	if r == domain.Range1D && len(c) > 0 {
		last := c[len(c)-1].Time.In(market.NewYork)
		i := len(c)
		for i > 0 {
			t := c[i-1].Time.In(market.NewYork)
			if t.YearDay() != last.YearDay() || t.Year() != last.Year() {
				break
			}
			i--
		}
		return c[i:]
	}
	if n := r.BarSpec().Count; len(c) > n {
		return c[len(c)-n:]
	}
	return c
}
