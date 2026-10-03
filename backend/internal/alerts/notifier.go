package alerts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// Notifier delivers a notification somewhere.
type Notifier interface {
	Notify(ctx context.Context, n Notification) error
}

// LogNotifier writes notifications to the log (default).
type LogNotifier struct{ Logger *slog.Logger }

// Notify implements Notifier.
func (l LogNotifier) Notify(_ context.Context, n Notification) error {
	l.Logger.Info("alert fired", "alert_id", n.AlertID, "symbol", n.Symbol, "price", n.Price, "message", n.Message)
	return nil
}

// DiscordWebhookNotifier posts to a Discord webhook. The URL is a secret: it
// is never logged or included in errors.
type DiscordWebhookNotifier struct {
	url  string
	http *http.Client
}

// NewDiscordWebhookNotifier returns a Discord notifier for webhookURL.
func NewDiscordWebhookNotifier(webhookURL string) *DiscordWebhookNotifier {
	return &DiscordWebhookNotifier{url: webhookURL, http: &http.Client{Timeout: 10 * time.Second}}
}

// Notify implements Notifier.
func (d *DiscordWebhookNotifier) Notify(ctx context.Context, n Notification) error {
	body, err := json.Marshal(map[string]any{
		"username": "StockChat",
		"content":  "🔔 " + n.Message,
		// Never let alert text ping @everyone or roles.
		"allowed_mentions": map[string]any{"parse": []string{}},
	})
	if err != nil {
		return fmt.Errorf("discord: marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.url, bytes.NewReader(body))
	if err != nil {
		return errors.New("discord: invalid webhook URL")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("discord: %w", ctx.Err())
		}
		return errors.New("discord: request failed") // don't wrap: the error text contains the URL
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("discord: status %d", resp.StatusCode)
	}
	return nil
}

// MultiNotifier fans out to several notifiers and joins their errors.
type MultiNotifier []Notifier

// Notify implements Notifier.
func (m MultiNotifier) Notify(ctx context.Context, n Notification) error {
	var errs []error
	for _, x := range m {
		if err := x.Notify(ctx, n); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Hub broadcasts notifications to live SSE subscribers.
type Hub struct {
	mu   sync.Mutex
	subs map[chan Notification]struct{}
}

// NewHub returns an empty hub.
func NewHub() *Hub { return &Hub{subs: map[chan Notification]struct{}{}} }

// Subscribe registers a listener. Call the returned func to unsubscribe.
func (h *Hub) Subscribe() (<-chan Notification, func()) {
	ch := make(chan Notification, 16)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		if _, ok := h.subs[ch]; ok {
			delete(h.subs, ch)
			close(ch)
		}
		h.mu.Unlock()
	}
}

// Broadcast sends n to every subscriber without blocking (slow ones miss it).
func (h *Hub) Broadcast(n Notification) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- n:
		default:
		}
	}
}

// Notify implements Notifier so the hub can sit in a MultiNotifier.
func (h *Hub) Notify(_ context.Context, n Notification) error {
	h.Broadcast(n)
	return nil
}
