package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mahijeetreddy/stockchat/backend/internal/domain"
	"github.com/mahijeetreddy/stockchat/backend/internal/llm"
	"github.com/mahijeetreddy/stockchat/backend/internal/market"
)

// MaxActiveAlerts caps the number of active alerts.
const MaxActiveAlerts = 100

// AlertStore persists alerts.
type AlertStore interface {
	CreateAlert(ctx context.Context, a domain.Alert) (domain.Alert, error)
	GetAlert(ctx context.Context, id int64) (domain.Alert, error)
	ListAlerts(ctx context.Context, activeOnly bool) ([]domain.Alert, error)
	CountActiveAlerts(ctx context.Context) (int, error)
	DeleteAlert(ctx context.Context, id int64) error
}

// AlertView is the API/UI representation of an alert.
type AlertView struct {
	ID              int64            `json:"id"`
	Symbol          string           `json:"symbol"`
	Kind            domain.AlertKind `json:"kind"`
	Threshold       float64          `json:"threshold"`
	Repeat          bool             `json:"repeat"`
	CooldownMinutes int              `json:"cooldown_minutes"`
	Status          string           `json:"status"`
	CreatedAt       time.Time        `json:"created_at"`
	LastFiredAt     *time.Time       `json:"last_fired_at,omitempty"`
	Description     string           `json:"description"`
}

// ViewAlert converts a domain alert for the API/UI.
func ViewAlert(a domain.Alert) AlertView {
	return AlertView{
		ID: a.ID, Symbol: a.Symbol, Kind: a.Kind, Threshold: a.Threshold, Repeat: a.Repeat,
		CooldownMinutes: a.CooldownMinutes(), Status: a.Status, CreatedAt: a.CreatedAt,
		LastFiredAt: a.LastFired, Description: a.Describe(),
	}
}

// AlertListData is the alert_list UI payload.
type AlertListData struct {
	Alerts []AlertView `json:"alerts"`
}

// ListAlerts is the list_alerts tool.
type ListAlerts struct {
	Store AlertStore
}

// Spec implements Tool.
func (t *ListAlerts) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        "list_alerts",
		Description: "List the user's price alerts (active and recently triggered), with their IDs. Use for 'what alerts do I have' and before deleting an alert to find its ID.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
	}
}

// Mutating implements Tool.
func (t *ListAlerts) Mutating() bool { return false }

// Label implements Labeler.
func (t *ListAlerts) Label(json.RawMessage) string { return "Loading your alerts…" }

// Execute implements Tool.
func (t *ListAlerts) Execute(ctx context.Context, input json.RawMessage) (Result, error) {
	if err := decodeStrict(input, &struct{}{}); err != nil {
		return Result{}, err
	}
	alerts, err := t.Store.ListAlerts(ctx, false)
	if err != nil {
		return Result{}, fmt.Errorf("list_alerts: %w", err)
	}
	views := make([]AlertView, len(alerts))
	type row struct {
		ID          int64  `json:"id"`
		Description string `json:"description"`
		Status      string `json:"status"`
	}
	rows := make([]row, len(alerts))
	for i, a := range alerts {
		views[i] = ViewAlert(a)
		rows[i] = row{ID: a.ID, Description: views[i].Description, Status: a.Status}
	}
	out := map[string]any{"alerts": rows}
	if len(rows) == 0 {
		out["note"] = "No alerts set."
	}
	fm, err := marshalForModel(out)
	if err != nil {
		return Result{}, err
	}
	ui, err := NewUIBlock(UIAlertList, AlertListData{Alerts: views})
	if err != nil {
		return Result{}, err
	}
	return Result{ForModel: fm, UI: ui}, nil
}

// CreateAlert is the create_alert tool (mutating).
type CreateAlert struct {
	Store  AlertStore
	Market market.Provider
}

type createAlertInput struct {
	Symbol          string  `json:"symbol"`
	Kind            string  `json:"kind"`
	Threshold       float64 `json:"threshold"`
	Repeat          *bool   `json:"repeat,omitempty"`
	CooldownMinutes *int    `json:"cooldown_minutes,omitempty"`
}

// Spec implements Tool.
func (t *CreateAlert) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name: "create_alert",
		Description: "Create a price alert for a US ticker. Kinds: price_above (fires when price >= threshold USD), price_below (price <= threshold USD), " +
			"percent_change_day (fires when today's move vs previous close is at least threshold percent, up or down). " +
			"Use for 'alert me if', 'notify me when', 'tell me if X drops 5%'. " +
			"Takes effect only after the user clicks Confirm in the UI; tell them it is waiting for confirmation.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` +
			`"symbol":{"type":"string","description":"US ticker, e.g. AAPL. Use search_symbol first if unsure."},` +
			`"kind":{"type":"string","enum":["price_above","price_below","percent_change_day"]},` +
			`"threshold":{"type":"number","description":"Price in USD for price_* kinds; percent (e.g. 5 for 5%) for percent_change_day."},` +
			`"repeat":{"type":"boolean","default":false,"description":"Keep firing (with cooldown) instead of once"},` +
			`"cooldown_minutes":{"type":"integer","minimum":1,"maximum":1440,"default":60,"description":"Minimum minutes between repeat firings"}` +
			`},"required":["symbol","kind","threshold"],"additionalProperties":false}`),
	}
}

// Mutating implements Tool.
func (t *CreateAlert) Mutating() bool { return true }

// Label implements Labeler.
func (t *CreateAlert) Label(input json.RawMessage) string {
	var in createAlertInput
	_ = json.Unmarshal(input, &in)
	if s := strings.ToUpper(strings.TrimSpace(in.Symbol)); symbolRe.MatchString(s) {
		return fmt.Sprintf("Preparing %s alert…", s)
	}
	return "Preparing alert…"
}

func (t *CreateAlert) validate(ctx context.Context, input json.RawMessage) (domain.Alert, domain.Quote, error) {
	var in createAlertInput
	if err := decodeStrict(input, &in); err != nil {
		return domain.Alert{}, domain.Quote{}, err
	}
	sym, err := NormalizeSymbol(in.Symbol)
	if err != nil {
		return domain.Alert{}, domain.Quote{}, err
	}
	kind := domain.AlertKind(strings.ToLower(strings.TrimSpace(in.Kind)))
	if !kind.Valid() {
		return domain.Alert{}, domain.Quote{}, inputErr("kind must be price_above, price_below, or percent_change_day")
	}
	switch {
	case kind == domain.AlertPercentChangeDay && (in.Threshold <= 0 || in.Threshold > 100):
		return domain.Alert{}, domain.Quote{}, inputErr("percent threshold must be between 0 and 100 (e.g. 5 for 5%%)")
	case kind != domain.AlertPercentChangeDay && (in.Threshold <= 0 || in.Threshold > 1_000_000):
		return domain.Alert{}, domain.Quote{}, inputErr("price threshold must be a positive USD amount")
	}
	a := domain.Alert{Symbol: sym, Kind: kind, Threshold: round(in.Threshold, 4), Cooldown: time.Hour}
	if in.Repeat != nil {
		a.Repeat = *in.Repeat
	}
	if in.CooldownMinutes != nil {
		if *in.CooldownMinutes < 1 || *in.CooldownMinutes > 1440 {
			return domain.Alert{}, domain.Quote{}, inputErr("cooldown_minutes must be between 1 and 1440")
		}
		a.Cooldown = time.Duration(*in.CooldownMinutes) * time.Minute
	}
	n, err := t.Store.CountActiveAlerts(ctx)
	if err != nil {
		return domain.Alert{}, domain.Quote{}, fmt.Errorf("count alerts: %w", err)
	}
	if n >= MaxActiveAlerts {
		return domain.Alert{}, domain.Quote{}, inputErr("too many active alerts (%d); delete some first", MaxActiveAlerts)
	}
	q, err := t.Market.Quote(ctx, sym)
	if err != nil {
		if errors.Is(err, market.ErrNotFound) {
			return domain.Alert{}, domain.Quote{}, inputErr("%s doesn't look like a valid US ticker; use search_symbol", sym)
		}
		return domain.Alert{}, domain.Quote{}, fmt.Errorf("verify %s: %w", sym, err)
	}
	return a, q, nil
}

// Prepare implements Preparer.
func (t *CreateAlert) Prepare(ctx context.Context, input json.RawMessage) (string, json.RawMessage, error) {
	a, q, err := t.validate(ctx, input)
	if err != nil {
		return "", nil, err
	}
	repeat, cd := a.Repeat, a.CooldownMinutes()
	norm, _ := json.Marshal(createAlertInput{Symbol: a.Symbol, Kind: string(a.Kind), Threshold: a.Threshold, Repeat: &repeat, CooldownMinutes: &cd})
	summary := fmt.Sprintf("Alert when %s (now %s)", a.Describe(), formatUSD(q.Price))
	return summary, norm, nil
}

// Execute implements Tool. Only called after user confirmation.
func (t *CreateAlert) Execute(ctx context.Context, input json.RawMessage) (Result, error) {
	a, _, err := t.validate(ctx, input)
	if err != nil {
		return Result{}, err
	}
	created, err := t.Store.CreateAlert(ctx, a)
	if err != nil {
		return Result{}, fmt.Errorf("create_alert: %w", err)
	}
	return Result{ForModel: fmt.Sprintf("Alert #%d created: %s", created.ID, created.Describe())}, nil
}

// DeleteAlertTool is the delete_alert tool (mutating).
type DeleteAlertTool struct {
	Store AlertStore
}

type deleteAlertInput struct {
	AlertID int64 `json:"alert_id"`
}

// Spec implements Tool.
func (t *DeleteAlertTool) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name: "delete_alert",
		Description: "Delete one price alert by its numeric ID (get IDs from list_alerts). " +
			"Takes effect only after the user clicks Confirm in the UI; tell them it is waiting for confirmation.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"alert_id":{"type":"integer","minimum":1,"description":"Alert ID from list_alerts"}},"required":["alert_id"],"additionalProperties":false}`),
	}
}

// Mutating implements Tool.
func (t *DeleteAlertTool) Mutating() bool { return true }

// Label implements Labeler.
func (t *DeleteAlertTool) Label(input json.RawMessage) string {
	var in deleteAlertInput
	_ = json.Unmarshal(input, &in)
	if in.AlertID > 0 {
		return fmt.Sprintf("Preparing to delete alert #%d…", in.AlertID)
	}
	return "Preparing to delete alert…"
}

func (t *DeleteAlertTool) validate(ctx context.Context, input json.RawMessage) (domain.Alert, error) {
	var in deleteAlertInput
	if err := decodeStrict(input, &in); err != nil {
		return domain.Alert{}, err
	}
	if in.AlertID < 1 {
		return domain.Alert{}, inputErr("alert_id must be a positive integer from list_alerts")
	}
	a, err := t.Store.GetAlert(ctx, in.AlertID)
	if err != nil || a.Status == domain.AlertDeleted {
		return domain.Alert{}, inputErr("alert #%d doesn't exist; call list_alerts to see IDs", in.AlertID)
	}
	return a, nil
}

// Prepare implements Preparer.
func (t *DeleteAlertTool) Prepare(ctx context.Context, input json.RawMessage) (string, json.RawMessage, error) {
	a, err := t.validate(ctx, input)
	if err != nil {
		return "", nil, err
	}
	norm, _ := json.Marshal(deleteAlertInput{AlertID: a.ID})
	return fmt.Sprintf("Delete alert #%d (%s)", a.ID, a.Describe()), norm, nil
}

// Execute implements Tool. Only called after user confirmation.
func (t *DeleteAlertTool) Execute(ctx context.Context, input json.RawMessage) (Result, error) {
	a, err := t.validate(ctx, input)
	if err != nil {
		return Result{}, err
	}
	if err := t.Store.DeleteAlert(ctx, a.ID); err != nil {
		return Result{}, fmt.Errorf("delete_alert: %w", err)
	}
	return Result{ForModel: fmt.Sprintf("Alert #%d deleted", a.ID)}, nil
}

func formatUSD(v float64) string { return fmt.Sprintf("$%.2f", v) }
