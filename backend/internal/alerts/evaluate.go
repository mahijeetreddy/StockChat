// Package alerts runs the background price-alert engine and delivers
// notifications.
package alerts

import (
	"fmt"
	"math"
	"time"

	"github.com/mahijeetreddy/stockchat/backend/internal/domain"
)

// Evaluate reports whether alert a fires for quote q at time now. It is a pure
// function so the rules can be table-tested.
//
// Rules:
//   - Only active alerts fire, and only for a positive price of the same symbol.
//   - price_above fires when price >= threshold; price_below when price <= threshold.
//   - percent_change_day fires when |change_percent| >= threshold, i.e. on a
//     move of that size in either direction (absolute semantics).
//   - One-time alerts (repeat=false) fire at most once.
//   - Repeating alerts wait at least Cooldown between firings.
func Evaluate(a domain.Alert, q domain.Quote, now time.Time) bool {
	if a.Status != domain.AlertActive || q.Symbol != a.Symbol || q.Price <= 0 {
		return false
	}
	if a.LastFired != nil {
		if !a.Repeat {
			return false
		}
		if now.Sub(*a.LastFired) < a.Cooldown {
			return false
		}
	}
	switch a.Kind {
	case domain.AlertPriceAbove:
		return q.Price >= a.Threshold
	case domain.AlertPriceBelow:
		return q.Price <= a.Threshold
	case domain.AlertPercentChangeDay:
		return math.Abs(q.ChangePercent) >= a.Threshold
	default:
		return false
	}
}

// Notification is what users receive when an alert fires.
type Notification struct {
	AlertID   int64            `json:"alert_id"`
	Symbol    string           `json:"symbol"`
	Kind      domain.AlertKind `json:"kind"`
	Threshold float64          `json:"threshold"`
	Price     float64          `json:"price"`
	Change    float64          `json:"change_percent"`
	Message   string           `json:"message"`
	FiredAt   time.Time        `json:"fired_at"`
}

// NewNotification builds the user-facing message for a firing.
func NewNotification(a domain.Alert, q domain.Quote, at time.Time) Notification {
	var msg string
	switch a.Kind {
	case domain.AlertPriceAbove:
		msg = fmt.Sprintf("%s is at $%.2f, above your $%.2f alert.", a.Symbol, q.Price, a.Threshold)
	case domain.AlertPriceBelow:
		msg = fmt.Sprintf("%s is at $%.2f, below your $%.2f alert.", a.Symbol, q.Price, a.Threshold)
	default:
		msg = fmt.Sprintf("%s moved %+.2f%% today (now $%.2f), past your %.2f%% alert.", a.Symbol, q.ChangePercent, q.Price, a.Threshold)
	}
	return Notification{
		AlertID: a.ID, Symbol: a.Symbol, Kind: a.Kind, Threshold: a.Threshold,
		Price: q.Price, Change: q.ChangePercent, Message: msg, FiredAt: at.UTC(),
	}
}
