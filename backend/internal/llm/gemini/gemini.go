// Package gemini implements llm.Client on the Google Gemini API using the
// official Go GenAI SDK (google.golang.org/genai).
package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"google.golang.org/genai"

	"github.com/mahijeetreddy/stockchat/backend/internal/llm"
)

// Config configures the client.
type Config struct {
	APIKey string
	Model  string
	// FallbackModel (optional) is tried when Model is still rate limited or
	// overloaded after its retries. Google's docs say history (including
	// thought signatures) may be resent to a different model as-is.
	FallbackModel string
	MaxRetries    int    // retries on 429/5xx before any output was streamed
	BaseURL       string // optional override (tests)
	// BaseBackoff is the first retry delay (doubles each attempt, with jitter).
	BaseBackoff time.Duration
	MaxBackoff  time.Duration
}

// Client is a Gemini-backed llm.Client.
type Client struct {
	sdk *genai.Client
	cfg Config
}

var _ llm.Client = (*Client)(nil)

// New creates a Gemini client.
func New(ctx context.Context, cfg Config) (*Client, error) {
	if cfg.APIKey == "" || cfg.Model == "" {
		return nil, errors.New("gemini: API key and model are required")
	}
	if cfg.BaseBackoff <= 0 {
		cfg.BaseBackoff = 2 * time.Second
	}
	if cfg.MaxBackoff <= 0 {
		cfg.MaxBackoff = 30 * time.Second
	}
	cc := &genai.ClientConfig{
		APIKey:     cfg.APIKey,
		Backend:    genai.BackendGeminiAPI,
		HTTPClient: &http.Client{Timeout: 120 * time.Second},
	}
	if cfg.BaseURL != "" {
		cc.HTTPOptions = genai.HTTPOptions{BaseURL: cfg.BaseURL}
	}
	sdk, err := genai.NewClient(ctx, cc)
	if err != nil {
		return nil, fmt.Errorf("gemini: new client: %w", err)
	}
	return &Client{sdk: sdk, cfg: cfg}, nil
}

// meta is what we store in ContentBlock.ProviderMeta.
type meta struct {
	// ThoughtSignature must be replayed on the same part in later turns.
	ThoughtSignature []byte `json:"thought_signature,omitempty"`
	// ProviderID is true when the call ID came from Gemini (send it back) rather
	// than being generated locally.
	ProviderID bool `json:"provider_id,omitempty"`
}

func encodeMeta(m meta) json.RawMessage {
	if len(m.ThoughtSignature) == 0 && !m.ProviderID {
		return nil
	}
	b, _ := json.Marshal(m)
	return b
}

func decodeMeta(raw json.RawMessage) meta {
	var m meta
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &m)
	}
	return m
}

// Stream implements llm.Client.
func (c *Client) Stream(ctx context.Context, req llm.Request) (<-chan llm.Event, error) {
	contents, err := toContents(req.Messages)
	if err != nil {
		return nil, err
	}
	gcfg := &genai.GenerateContentConfig{}
	if req.System != "" {
		gcfg.SystemInstruction = &genai.Content{Parts: []*genai.Part{{Text: req.System}}}
	}
	if req.MaxTokens > 0 {
		gcfg.MaxOutputTokens = int32(min(req.MaxTokens, 1<<20)) // #nosec G115 -- bounded above
	}
	if len(req.Tools) > 0 {
		decls, err := toDeclarations(req.Tools)
		if err != nil {
			return nil, err
		}
		gcfg.Tools = []*genai.Tool{{FunctionDeclarations: decls}}
		// Default (AUTO) function-calling mode: plain-text answers are valid.
	}

	out := make(chan llm.Event, 16)
	go func() {
		defer close(out)
		c.run(ctx, contents, gcfg, out)
	}()
	return out, nil
}

func send(ctx context.Context, out chan<- llm.Event, ev llm.Event) bool {
	select {
	case out <- ev:
		return true
	case <-ctx.Done():
		return false
	}
}

// run performs the call with retries. Retries happen only if nothing has been
// emitted yet, so the consumer never sees duplicated output.
func (c *Client) run(ctx context.Context, contents []*genai.Content, gcfg *genai.GenerateContentConfig, out chan<- llm.Event) {
	models := []string{c.cfg.Model}
	if c.cfg.FallbackModel != "" && c.cfg.FallbackModel != c.cfg.Model {
		models = append(models, c.cfg.FallbackModel)
	}
	var lastErr error
models:
	for _, model := range models {
		for attempt := 0; attempt <= c.cfg.MaxRetries; attempt++ {
			if attempt > 0 {
				delay := c.backoff(attempt, lastErr)
				select {
				case <-time.After(delay):
				case <-ctx.Done():
					send(context.Background(), out, llm.Event{Type: llm.EventStop, Err: ctx.Err()})
					return
				}
			}
			emitted, err := c.once(ctx, model, contents, gcfg, out)
			if err == nil {
				return
			}
			lastErr = err
			if !emitted && ctx.Err() == nil && c.quotaExhausted(err) {
				continue models // waiting won't help soon; try the fallback model
			}
			var le *llm.Error
			if emitted || ctx.Err() != nil || !errors.As(err, &le) || !le.Retryable {
				break models // not worth retrying, or output already streamed
			}
		}
		// Retries exhausted on a retryable error: fall through to the next model.
	}
	if ctx.Err() != nil {
		lastErr = ctx.Err()
	}
	// Use a background context so the final error is delivered even if ctx is done;
	// the buffered channel and closing consumer make this non-blocking in practice.
	select {
	case out <- llm.Event{Type: llm.EventStop, Err: lastErr}:
	case <-time.After(time.Second):
	}
}

// quotaExhausted reports a 429 that won't clear within our retry budget: a
// daily quota, or a server-suggested delay longer than MaxBackoff.
func (c *Client) quotaExhausted(err error) bool {
	var e apiErrWithDelay
	if !errors.As(err, &e) {
		return false
	}
	return dailyQuota(e.APIError) || retryAfter(err) > c.cfg.MaxBackoff
}

// dailyQuota reports whether a 429 is for a per-day quota (google.rpc.QuotaFailure).
func dailyQuota(e genai.APIError) bool {
	for _, d := range e.Details {
		if t, _ := d["@type"].(string); !strings.HasSuffix(t, "QuotaFailure") {
			continue
		}
		vs, _ := d["violations"].([]any)
		for _, v := range vs {
			m, _ := v.(map[string]any)
			if id, _ := m["quotaId"].(string); strings.Contains(id, "PerDay") {
				return true
			}
		}
	}
	return false
}

func (c *Client) backoff(attempt int, err error) time.Duration {
	if d := retryAfter(err); d > 0 {
		return min(d, c.cfg.MaxBackoff)
	}
	d := c.cfg.BaseBackoff << (attempt - 1)
	d = min(d, c.cfg.MaxBackoff)
	// Full jitter in [d/2, d].
	return d/2 + time.Duration(rand.Int64N(int64(d/2)+1))
}

// once streams one attempt. It returns whether any event was emitted.
func (c *Client) once(ctx context.Context, model string, contents []*genai.Content, gcfg *genai.GenerateContentConfig, out chan<- llm.Event) (bool, error) {
	var (
		emitted      bool
		finish       genai.FinishReason
		finishMsg    string
		usage        llm.Usage
		blockReason  string
		gotAnyOutput bool
	)
	for resp, err := range c.sdk.Models.GenerateContentStream(ctx, model, contents, gcfg) {
		if err != nil {
			return emitted, classify(err)
		}
		if resp.UsageMetadata != nil {
			usage = llm.Usage{
				InputTokens:  int(resp.UsageMetadata.PromptTokenCount),
				OutputTokens: int(resp.UsageMetadata.CandidatesTokenCount + resp.UsageMetadata.ThoughtsTokenCount),
			}
		}
		if resp.PromptFeedback != nil && resp.PromptFeedback.BlockReason != "" {
			blockReason = string(resp.PromptFeedback.BlockReason)
		}
		if len(resp.Candidates) == 0 {
			continue
		}
		cand := resp.Candidates[0]
		if cand.FinishReason != "" {
			finish = cand.FinishReason
			finishMsg = cand.FinishMessage
		}
		if cand.Content == nil {
			continue
		}
		for _, p := range cand.Content.Parts {
			if p == nil || p.Thought {
				continue // internal reasoning summaries are not shown
			}
			switch {
			case p.FunctionCall != nil:
				ev, err := toolUseEvent(p)
				if err != nil {
					return emitted, err
				}
				if !send(ctx, out, ev) {
					return true, ctx.Err()
				}
				emitted, gotAnyOutput = true, true
			case p.Text != "" || len(p.ThoughtSignature) > 0:
				ev := llm.Event{Type: llm.EventTextDelta, TextDelta: p.Text}
				if len(p.ThoughtSignature) > 0 {
					ev.TextMeta = encodeMeta(meta{ThoughtSignature: p.ThoughtSignature})
				}
				if !send(ctx, out, ev) {
					return true, ctx.Err()
				}
				emitted = true
				if p.Text != "" {
					gotAnyOutput = true
				}
			}
		}
	}

	if blockReason != "" && !gotAnyOutput {
		return emitted, &llm.Error{Kind: llm.KindBlocked, UserMsg: "The request was blocked by the model's safety filters. Please rephrase.", Err: fmt.Errorf("prompt blocked: %s", blockReason)}
	}
	switch finish {
	case genai.FinishReasonSafety, genai.FinishReasonBlocklist, genai.FinishReasonProhibitedContent,
		genai.FinishReasonSPII, genai.FinishReasonRecitation:
		if !gotAnyOutput {
			return emitted, &llm.Error{Kind: llm.KindBlocked, UserMsg: "The model declined to answer that (content filter). Please rephrase.", Err: fmt.Errorf("finish reason %s", finish)}
		}
	case genai.FinishReasonMalformedFunctionCall, genai.FinishReasonUnexpectedToolCall:
		if !gotAnyOutput {
			return emitted, &llm.Error{Kind: llm.KindOther, UserMsg: "The model produced an invalid tool call. Please try again.", Retryable: true, Err: fmt.Errorf("finish reason %s: %s", finish, finishMsg)}
		}
	}
	stop := string(finish)
	if stop == "" {
		stop = "STOP"
	}
	if !send(ctx, out, llm.Event{Type: llm.EventStop, StopReason: stop, Usage: usage}) {
		return true, ctx.Err()
	}
	return true, nil
}

func toolUseEvent(p *genai.Part) (llm.Event, error) {
	fc := p.FunctionCall
	args := fc.Args
	if args == nil {
		args = map[string]any{}
	}
	input, err := json.Marshal(args)
	if err != nil {
		return llm.Event{}, fmt.Errorf("gemini: marshal function args: %w", err)
	}
	m := meta{ThoughtSignature: p.ThoughtSignature}
	id := fc.ID
	if id != "" {
		m.ProviderID = true
	} else {
		id = "call_" + ulid.Make().String()
	}
	return llm.Event{Type: llm.EventToolUse, ToolUse: &llm.ContentBlock{
		Type:         llm.BlockToolUse,
		ToolUseID:    id,
		ToolName:     fc.Name,
		Input:        input,
		ProviderMeta: encodeMeta(m),
	}}, nil
}

// classify maps SDK errors onto llm.Error.
func classify(err error) error {
	var apiErr genai.APIError
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.Code == http.StatusTooManyRequests && dailyQuota(apiErr):
			return &llm.Error{Kind: llm.KindRateLimited, UserMsg: "The AI model's daily free quota is used up. Try again tomorrow, or set LLM_FALLBACK_MODEL / use a paid API key.", Retryable: true, Err: apiErrWithDelay{apiErr}}
		case apiErr.Code == http.StatusTooManyRequests:
			return &llm.Error{Kind: llm.KindRateLimited, UserMsg: "The AI model is rate limited right now. Please wait a moment and try again.", Retryable: true, Err: apiErrWithDelay{apiErr}}
		case apiErr.Code == http.StatusRequestTimeout || apiErr.Code >= 500:
			return &llm.Error{Kind: llm.KindUnavailable, UserMsg: "The AI model is temporarily unavailable. Please try again.", Retryable: true, Err: apiErr}
		case apiErr.Code == http.StatusUnauthorized || apiErr.Code == http.StatusForbidden:
			return &llm.Error{Kind: llm.KindAuth, UserMsg: "The AI model rejected the API key. Check GEMINI_API_KEY.", Err: apiErr}
		case apiErr.Code == http.StatusBadRequest || apiErr.Code == http.StatusNotFound:
			return &llm.Error{Kind: llm.KindBadRequest, UserMsg: "The AI model rejected the request. Check LLM_MODEL and try again.", Err: apiErr}
		}
		return &llm.Error{Kind: llm.KindOther, UserMsg: "The AI model returned an error.", Err: apiErr}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return &llm.Error{Kind: llm.KindUnavailable, UserMsg: "Couldn't reach the AI model. Please try again.", Retryable: true, Err: err}
}

// apiErrWithDelay keeps the APIError so retryAfter can read RetryInfo.
type apiErrWithDelay struct{ genai.APIError }

func (e apiErrWithDelay) Unwrap() error { return e.APIError }

// retryAfter extracts google.rpc.RetryInfo.retryDelay (e.g. "37s") from a 429.
func retryAfter(err error) time.Duration {
	var e apiErrWithDelay
	if !errors.As(err, &e) {
		return 0
	}
	for _, d := range e.Details {
		if t, _ := d["@type"].(string); !strings.HasSuffix(t, "RetryInfo") {
			continue
		}
		if s, ok := d["retryDelay"].(string); ok {
			if dur, err := time.ParseDuration(s); err == nil {
				return dur
			}
		}
	}
	return 0
}

// ---- request conversion ----

func toContents(msgs []llm.Message) ([]*genai.Content, error) {
	var out []*genai.Content
	// Call IDs issued by Gemini must be echoed on the matching FunctionResponse.
	providerIDs := map[string]bool{}
	for _, m := range msgs {
		role := genai.RoleUser
		if m.Role == llm.RoleAssistant {
			role = genai.RoleModel
		}
		var parts []*genai.Part
		for _, b := range m.Blocks {
			md := decodeMeta(b.ProviderMeta)
			switch b.Type {
			case llm.BlockText:
				if b.Text == "" && len(md.ThoughtSignature) == 0 {
					continue
				}
				parts = append(parts, &genai.Part{Text: b.Text, ThoughtSignature: md.ThoughtSignature})
			case llm.BlockToolUse:
				var args map[string]any
				if len(b.Input) > 0 {
					if err := json.Unmarshal(b.Input, &args); err != nil {
						return nil, fmt.Errorf("gemini: tool_use %s input: %w", b.ToolName, err)
					}
				}
				fc := &genai.FunctionCall{Name: b.ToolName, Args: args}
				if md.ProviderID {
					fc.ID = b.ToolUseID
					providerIDs[b.ToolUseID] = true
				}
				parts = append(parts, &genai.Part{FunctionCall: fc, ThoughtSignature: md.ThoughtSignature})
			case llm.BlockToolResult:
				fr := &genai.FunctionResponse{Name: b.ToolName, Response: resultMap(b)}
				if md.ProviderID || providerIDs[b.ToolUseID] {
					fr.ID = b.ToolUseID
				}
				parts = append(parts, &genai.Part{FunctionResponse: fr})
			}
		}
		if len(parts) == 0 {
			continue
		}
		// Merge consecutive same-role turns (e.g. a synthetic note after a user message).
		if n := len(out); n > 0 && out[n-1].Role == string(role) {
			out[n-1].Parts = append(out[n-1].Parts, parts...)
			continue
		}
		out = append(out, &genai.Content{Role: string(role), Parts: parts})
	}
	return out, nil
}

// resultMap wraps tool output in the object Gemini expects: {"output": ...}
// or {"error": ...}. JSON objects/arrays are passed as structured values.
func resultMap(b llm.ContentBlock) map[string]any {
	key := "output"
	if b.IsError {
		key = "error"
	}
	var v any
	if err := json.Unmarshal([]byte(b.Content), &v); err != nil {
		v = b.Content
	}
	return map[string]any{key: v}
}

// Schema keywords Gemini's function-declaration schema subset accepts. Others
// (additionalProperties, default, $schema, ...) are stripped; Go-side tool
// validation enforces them.
var allowedSchemaKeys = map[string]bool{
	"type": true, "properties": true, "enum": true, "required": true, "description": true,
	"minimum": true, "maximum": true, "items": true, "minItems": true, "maxItems": true,
	"format": true, "nullable": true, "minLength": true, "maxLength": true,
}

func sanitizeSchema(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			if !allowedSchemaKeys[k] {
				continue
			}
			if k == "properties" {
				props, _ := val.(map[string]any)
				clean := make(map[string]any, len(props))
				for name, ps := range props {
					clean[name] = sanitizeSchema(ps)
				}
				out[k] = clean
				continue
			}
			out[k] = sanitizeSchema(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, x := range t {
			out[i] = sanitizeSchema(x)
		}
		return out
	default:
		return v
	}
}

func toDeclarations(specs []llm.ToolSpec) ([]*genai.FunctionDeclaration, error) {
	decls := make([]*genai.FunctionDeclaration, 0, len(specs))
	for _, s := range specs {
		d := &genai.FunctionDeclaration{Name: s.Name, Description: s.Description}
		if len(s.InputSchema) > 0 {
			var schema map[string]any
			if err := json.Unmarshal(s.InputSchema, &schema); err != nil {
				return nil, fmt.Errorf("gemini: tool %s schema: %w", s.Name, err)
			}
			clean, _ := sanitizeSchema(schema).(map[string]any)
			// Tools without parameters omit the schema entirely.
			if props, _ := clean["properties"].(map[string]any); len(props) > 0 {
				d.ParametersJsonSchema = clean
			}
		}
		decls = append(decls, d)
	}
	return decls, nil
}
