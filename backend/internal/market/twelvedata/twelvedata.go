// Package twelvedata implements market.HistorySource using Twelve Data's
// /time_series endpoint (available on the free Basic plan). The API key is
// sent in the Authorization header ("apikey <key>").
package twelvedata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mahijeetreddy/stockchat/backend/internal/domain"
	"github.com/mahijeetreddy/stockchat/backend/internal/market"
)

// DefaultBaseURL is the Twelve Data API root.
const DefaultBaseURL = "https://api.twelvedata.com"

const maxBody = 4 << 20

// Client fetches price history from Twelve Data.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// Option configures a Client.
type Option func(*Client)

// WithBaseURL points the client at a different host (tests).
func WithBaseURL(u string) Option { return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") } }

// New returns a Twelve Data client.
func New(apiKey string, opts ...Option) *Client {
	c := &Client{baseURL: DefaultBaseURL, apiKey: apiKey, http: &http.Client{Timeout: 15 * time.Second}}
	for _, o := range opts {
		o(c)
	}
	return c
}

var _ market.HistorySource = (*Client)(nil)

type seriesResp struct {
	Status  string `json:"status"`
	Code    int    `json:"code"`
	Message string `json:"message"`
	Meta    struct {
		Symbol           string `json:"symbol"`
		Interval         string `json:"interval"`
		ExchangeTimezone string `json:"exchange_timezone"`
	} `json:"meta"`
	Values []struct {
		Datetime string `json:"datetime"`
		Open     string `json:"open"`
		High     string `json:"high"`
		Low      string `json:"low"`
		Close    string `json:"close"`
		Volume   string `json:"volume"`
	} `json:"values"`
}

func interval(r domain.HistoryRange) string {
	switch r {
	case domain.Range1D:
		return "5min"
	case domain.Range5D:
		return "30min"
	case domain.Range5Y:
		return "1week"
	default:
		return "1day"
	}
}

// History implements market.HistorySource.
func (c *Client) History(ctx context.Context, symbol string, r domain.HistoryRange) ([]domain.Candle, error) {
	if !r.Valid() {
		return nil, fmt.Errorf("twelvedata history: invalid range %q", r)
	}
	spec := r.BarSpec()
	q := url.Values{
		"symbol":     {symbol},
		"interval":   {interval(r)},
		"outputsize": {strconv.Itoa(spec.Count)},
		"timezone":   {"America/New_York"},
		"order":      {"asc"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/time_series?"+q.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("twelvedata: build request: %w", err)
	}
	req.Header.Set("Authorization", "apikey "+c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("twelvedata %s: %w", symbol, errors.Join(market.ErrUpstream, err))
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, fmt.Errorf("twelvedata %s: read: %w", symbol, errors.Join(market.ErrUpstream, err))
	}

	var sr seriesResp
	if err := json.Unmarshal(body, &sr); err != nil {
		if resp.StatusCode >= 300 {
			return nil, fmt.Errorf("twelvedata %s: %w", symbol, statusErr(resp.StatusCode))
		}
		return nil, fmt.Errorf("twelvedata %s: decode: %w", symbol, errors.Join(market.ErrUpstream, err))
	}
	// Twelve Data reports errors in the body, sometimes with HTTP 200.
	if sr.Status == "error" || resp.StatusCode >= 300 {
		code := sr.Code
		if code == 0 {
			code = resp.StatusCode
		}
		return nil, fmt.Errorf("twelvedata %s: %w", symbol, statusErr(code))
	}

	layout := "2006-01-02 15:04:05"
	if !r.Intraday() {
		layout = time.DateOnly
	}
	out := make([]domain.Candle, 0, len(sr.Values))
	for _, v := range sr.Values {
		t, err := time.ParseInLocation(layout, v.Datetime, market.NewYork)
		if err != nil {
			return nil, fmt.Errorf("twelvedata %s: bad datetime %q: %w", symbol, v.Datetime, market.ErrUpstream)
		}
		if !r.Intraday() {
			t = t.Add(9*time.Hour + 30*time.Minute) // daily bars are stamped at the session open
		}
		cdl := domain.Candle{Time: t.UTC()}
		for _, f := range []struct {
			s   string
			dst *float64
		}{{v.Open, &cdl.Open}, {v.High, &cdl.High}, {v.Low, &cdl.Low}, {v.Close, &cdl.Close}, {v.Volume, &cdl.Volume}} {
			if f.s == "" {
				continue
			}
			n, err := strconv.ParseFloat(f.s, 64)
			if err != nil {
				return nil, fmt.Errorf("twelvedata %s: bad number %q: %w", symbol, f.s, market.ErrUpstream)
			}
			*f.dst = n
		}
		out = append(out, cdl)
	}
	// Defensive: ensure ascending order even if "order" is ignored.
	if len(out) > 1 && out[0].Time.After(out[len(out)-1].Time) {
		for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
			out[i], out[j] = out[j], out[i]
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("twelvedata %s: no data: %w", symbol, market.ErrNotFound)
	}
	if r == domain.Range1D {
		out = lastSession(out)
	}
	return out, nil
}

func statusErr(code int) error {
	switch code {
	case http.StatusTooManyRequests:
		return market.ErrRateLimited
	case http.StatusUnauthorized, http.StatusForbidden:
		return market.ErrNoAccess
	case http.StatusNotFound, http.StatusBadRequest:
		return market.ErrNotFound
	default:
		return market.ErrUpstream
	}
}

// lastSession keeps only bars from the most recent trading day.
func lastSession(c []domain.Candle) []domain.Candle {
	last := c[len(c)-1].Time.In(market.NewYork).Format(time.DateOnly)
	i := len(c)
	for i > 0 && c[i-1].Time.In(market.NewYork).Format(time.DateOnly) == last {
		i--
	}
	return c[i:]
}
