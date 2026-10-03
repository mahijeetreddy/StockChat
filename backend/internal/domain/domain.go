// Package domain holds the shared types used across the backend.
package domain

import (
	"encoding/json"
	"fmt"
	"time"
)

// Quote is a point-in-time price snapshot for one symbol.
type Quote struct {
	Symbol        string    `json:"symbol"`
	Price         float64   `json:"price"`
	Change        float64   `json:"change"` // absolute vs previous close
	ChangePercent float64   `json:"change_percent"`
	High          float64   `json:"high"`
	Low           float64   `json:"low"`
	Open          float64   `json:"open"`
	PrevClose     float64   `json:"prev_close"`
	AsOf          time.Time `json:"as_of"`       // timestamp of the quote
	MarketOpen    bool      `json:"market_open"` // whether the US market is currently open
}

// Candle is one OHLCV bar.
type Candle struct {
	Time   time.Time `json:"time"`
	Open   float64   `json:"open"`
	High   float64   `json:"high"`
	Low    float64   `json:"low"`
	Close  float64   `json:"close"`
	Volume float64   `json:"volume"`
}

// HistoryRange is a lookback window for price history.
type HistoryRange string

// Supported history ranges.
const (
	Range1D HistoryRange = "1D"
	Range5D HistoryRange = "5D"
	Range1M HistoryRange = "1M"
	Range3M HistoryRange = "3M"
	Range6M HistoryRange = "6M"
	Range1Y HistoryRange = "1Y"
	Range5Y HistoryRange = "5Y"
)

// HistoryRanges lists every valid range in ascending order.
var HistoryRanges = []HistoryRange{Range1D, Range5D, Range1M, Range3M, Range6M, Range1Y, Range5Y}

// Valid reports whether r is a supported range.
func (r HistoryRange) Valid() bool {
	for _, v := range HistoryRanges {
		if r == v {
			return true
		}
	}
	return false
}

// Intraday reports whether the range uses sub-daily bars.
func (r HistoryRange) Intraday() bool { return r == Range1D || r == Range5D }

// Bars describes how a range maps onto bars: bar width and the number of bars.
// Providers use it so mock and real data have the same shape.
type Bars struct {
	Interval time.Duration
	Count    int
}

// BarSpec returns the bar layout for a range (US regular session = 6.5h).
func (r HistoryRange) BarSpec() Bars {
	switch r {
	case Range1D:
		return Bars{5 * time.Minute, 78}
	case Range5D:
		return Bars{30 * time.Minute, 65}
	case Range1M:
		return Bars{24 * time.Hour, 22}
	case Range3M:
		return Bars{24 * time.Hour, 63}
	case Range6M:
		return Bars{24 * time.Hour, 126}
	case Range1Y:
		return Bars{24 * time.Hour, 252}
	case Range5Y:
		return Bars{7 * 24 * time.Hour, 260}
	default:
		panic(fmt.Sprintf("BarSpec: invalid range %q", string(r)))
	}
}

// Lookback is the calendar window a range covers, used for from/to style APIs.
func (r HistoryRange) Lookback() time.Duration {
	day := 24 * time.Hour
	switch r {
	case Range1D:
		return 4 * day // covers a weekend plus a holiday; providers trim to the last session
	case Range5D:
		return 9 * day
	case Range1M:
		return 31 * day
	case Range3M:
		return 92 * day
	case Range6M:
		return 183 * day
	case Range1Y:
		return 366 * day
	default:
		return 5 * 366 * day
	}
}

// SymbolMatch is one result from a symbol search.
type SymbolMatch struct {
	Symbol      string `json:"symbol"`
	Description string `json:"description"`
	Type        string `json:"type"`
}

// CompanyProfile is basic company metadata.
type CompanyProfile struct {
	Symbol    string  `json:"symbol"`
	Name      string  `json:"name"`
	Exchange  string  `json:"exchange"`
	Industry  string  `json:"industry"`
	Country   string  `json:"country"`
	Currency  string  `json:"currency"`
	WebURL    string  `json:"web_url"`
	LogoURL   string  `json:"logo_url"`
	MarketCap float64 `json:"market_cap"` // USD
}

// NewsItem is one news article about a company.
type NewsItem struct {
	Headline    string    `json:"headline"`
	Summary     string    `json:"summary"`
	Source      string    `json:"source"`
	URL         string    `json:"url"`
	PublishedAt time.Time `json:"published_at"`
}

// AlertKind is the condition an alert watches.
type AlertKind string

// Supported alert kinds.
const (
	AlertPriceAbove       AlertKind = "price_above"
	AlertPriceBelow       AlertKind = "price_below"
	AlertPercentChangeDay AlertKind = "percent_change_day"
)

// Valid reports whether k is a supported alert kind.
func (k AlertKind) Valid() bool {
	switch k {
	case AlertPriceAbove, AlertPriceBelow, AlertPercentChangeDay:
		return true
	}
	return false
}

// Alert statuses.
const (
	AlertActive    = "active"
	AlertTriggered = "triggered"
	AlertDeleted   = "deleted"
)

// Alert is a user-defined price alert.
type Alert struct {
	ID        int64         `json:"id"`
	Symbol    string        `json:"symbol"`
	Kind      AlertKind     `json:"kind"`
	Threshold float64       `json:"threshold"`
	Repeat    bool          `json:"repeat"` // false = trigger once
	Cooldown  time.Duration `json:"-"`
	Status    string        `json:"status"`
	CreatedAt time.Time     `json:"created_at"`
	LastFired *time.Time    `json:"last_fired_at,omitempty"`
}

// CooldownMinutes exposes the cooldown in the unit the API uses.
func (a Alert) CooldownMinutes() int { return int(a.Cooldown / time.Minute) }

// Describe returns a short human-readable description of the alert condition.
func (a Alert) Describe() string {
	var cond string
	switch a.Kind {
	case AlertPriceAbove:
		cond = fmt.Sprintf("%s rises above $%.2f", a.Symbol, a.Threshold)
	case AlertPriceBelow:
		cond = fmt.Sprintf("%s falls below $%.2f", a.Symbol, a.Threshold)
	case AlertPercentChangeDay:
		cond = fmt.Sprintf("%s moves %.2f%% or more today (up or down)", a.Symbol, a.Threshold)
	default:
		cond = fmt.Sprintf("%s %s %.2f", a.Symbol, a.Kind, a.Threshold)
	}
	if a.Repeat {
		return fmt.Sprintf("%s, repeating every %d min at most", cond, a.CooldownMinutes())
	}
	return cond + ", one time"
}

// Pending action statuses.
const (
	ActionPending   = "pending"
	ActionDone      = "done"
	ActionCancelled = "cancelled"
	ActionExpired   = "expired"
)

// PendingAction is a mutating tool call waiting for user confirmation.
type PendingAction struct {
	ID             string          `json:"id"`
	ConversationID string          `json:"conversation_id"`
	ToolName       string          `json:"tool_name"`
	CallID         string          `json:"call_id"`
	Input          json.RawMessage `json:"input"`
	Summary        string          `json:"summary"`
	Status         string          `json:"status"`
	Result         string          `json:"result,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	ExpiresAt      time.Time       `json:"expires_at"`
}
