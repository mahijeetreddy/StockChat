// Package tools implements the functions the LLM can call. Each tool parses
// and validates its own input strictly; LLM output is untrusted.
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/mahijeetreddy/stockchat/backend/internal/llm"
	"github.com/mahijeetreddy/stockchat/backend/internal/market"
)

// Result is a tool's output.
type Result struct {
	ForModel string   // compact JSON sent back to the LLM
	UI       *UIBlock // structured payload for React; may be nil
}

// UI block types.
const (
	UIQuoteCard   = "quote_card"
	UIQuoteList   = "quote_list"
	UIPriceChart  = "price_chart"
	UICompare     = "compare"
	UINewsList    = "news_list"
	UIProfileCard = "profile_card"
	UIAlertList   = "alert_list"
	UIConfirmCard = "confirm_card"
)

// UIBlock is a typed payload the frontend renders directly.
type UIBlock struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// NewUIBlock marshals data into a UIBlock.
func NewUIBlock(typ string, data any) (*UIBlock, error) {
	b, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("marshal %s ui block: %w", typ, err)
	}
	return &UIBlock{Type: typ, Data: b}, nil
}

// Tool is one callable function.
type Tool interface {
	Spec() llm.ToolSpec
	Mutating() bool // true => needs user confirmation before Execute
	Execute(ctx context.Context, input json.RawMessage) (Result, error)
}

// Labeler is optionally implemented to give a progress label ("Fetching AAPL quote…").
type Labeler interface {
	Label(input json.RawMessage) string
}

// Preparer is implemented by mutating tools. Prepare validates and normalises
// the input and returns a human-readable summary for the confirmation card,
// without changing any state.
type Preparer interface {
	Prepare(ctx context.Context, input json.RawMessage) (summary string, normalized json.RawMessage, err error)
}

// Registry maps tool names to tools.
type Registry struct {
	tools map[string]Tool
}

// NewRegistry builds a registry. Duplicate names panic (programming error).
func NewRegistry(ts ...Tool) *Registry {
	r := &Registry{tools: make(map[string]Tool, len(ts))}
	for _, t := range ts {
		name := t.Spec().Name
		if _, dup := r.tools[name]; dup {
			panic("duplicate tool " + name)
		}
		if t.Mutating() {
			if _, ok := t.(Preparer); !ok {
				panic("mutating tool " + name + " must implement Preparer")
			}
		}
		r.tools[name] = t
	}
	return r
}

// Get returns the named tool.
func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// Specs returns all tool specs sorted by name (stable prompts).
func (r *Registry) Specs() []llm.ToolSpec {
	specs := make([]llm.ToolSpec, 0, len(r.tools))
	for _, t := range r.tools {
		specs = append(specs, t.Spec())
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].Name < specs[j].Name })
	return specs
}

// Label returns a progress label for a call.
func (r *Registry) Label(name string, input json.RawMessage) string {
	if t, ok := r.tools[name]; ok {
		if l, ok := t.(Labeler); ok {
			if s := l.Label(input); s != "" {
				return s
			}
		}
	}
	return "Running " + strings.ReplaceAll(name, "_", " ") + "…"
}

// ---- validation helpers ----

// InputError is a validation failure whose message is safe to show the model.
type InputError struct{ Msg string }

func (e *InputError) Error() string { return "invalid input: " + e.Msg }

func inputErr(format string, args ...any) error {
	return &InputError{Msg: fmt.Sprintf(format, args...)}
}

// decodeStrict decodes JSON into dst rejecting unknown fields and trailing data.
func decodeStrict(input json.RawMessage, dst any) error {
	if len(bytes.TrimSpace(input)) == 0 {
		input = json.RawMessage(`{}`)
	}
	dec := json.NewDecoder(bytes.NewReader(input))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return inputErr("%s", cleanJSONErr(err))
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return inputErr("unexpected trailing data")
	}
	return nil
}

func cleanJSONErr(err error) string {
	msg := err.Error()
	msg = strings.TrimPrefix(msg, "json: ")
	if len(msg) > 120 {
		msg = msg[:120]
	}
	return msg
}

var symbolRe = regexp.MustCompile(`^[A-Z.\-]{1,10}$`)

// NormalizeSymbol upper-cases, trims, and validates a ticker.
func NormalizeSymbol(s string) (string, error) {
	s = strings.ToUpper(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, "$")
	if !symbolRe.MatchString(s) {
		return "", inputErr("symbol must be a US ticker like AAPL (1-10 letters, '.' or '-'); use search_symbol to find it")
	}
	return s, nil
}

// SafeMessage converts any tool error into a short message safe for the model
// and the UI. Raw upstream errors (which might contain URLs or keys) never leak.
func SafeMessage(err error) string {
	var ie *InputError
	switch {
	case errors.As(err, &ie):
		return ie.Error()
	case errors.Is(err, market.ErrNotFound):
		return "not found: no data for that symbol (it may be invalid or not a US listing)"
	case errors.Is(err, market.ErrRateLimited):
		return "market data rate limit reached; try again in a minute"
	case errors.Is(err, market.ErrNoAccess):
		return "this data is not available on the current market data plan"
	case errors.Is(err, context.DeadlineExceeded):
		return "market data request timed out"
	case errors.Is(err, context.Canceled):
		return "request cancelled"
	default:
		return "market data provider error; try again later"
	}
}

// marshalForModel produces compact JSON for the model.
func marshalForModel(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("marshal tool result: %w", err)
	}
	return string(b), nil
}

// round rounds to n decimals for compact model payloads.
func round(f float64, n int) float64 {
	p := 1.0
	for range n {
		p *= 10
	}
	if f >= 0 {
		return float64(int64(f*p+0.5)) / p
	}
	return float64(int64(f*p-0.5)) / p
}
