package gemini

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mahijeetreddy/stockchat/backend/internal/llm"
)

// fakeAPI serves scripted responses for successive streamGenerateContent calls
// and records request bodies.
type fakeAPI struct {
	mu       sync.Mutex
	bodies   []map[string]any
	paths    []string
	scripted []func(w http.ResponseWriter)
}

func (f *fakeAPI) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		require.NoError(t, json.Unmarshal(raw, &body))
		f.mu.Lock()
		i := len(f.bodies)
		f.bodies = append(f.bodies, body)
		f.paths = append(f.paths, r.URL.Path+"?"+r.URL.RawQuery)
		f.mu.Unlock()
		if i >= len(f.scripted) {
			t.Errorf("unexpected request #%d", i+1)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		f.scripted[i](w)
	}
}

func sse(chunks ...string) func(w http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, c := range chunks {
			_, _ = fmt.Fprintf(w, "data: %s\r\n\r\n", c)
			w.(http.Flusher).Flush()
		}
	}
}

func status(code int, body string) func(w http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_, _ = io.WriteString(w, body)
	}
}

func newTestClient(t *testing.T, f *fakeAPI, fallback ...string) *Client {
	t.Helper()
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	cfg := Config{
		APIKey: "k", Model: "gemini-test", MaxRetries: 2, BaseURL: srv.URL,
		BaseBackoff: time.Millisecond, MaxBackoff: 5 * time.Millisecond,
	}
	if len(fallback) > 0 {
		cfg.FallbackModel = fallback[0]
	}
	c, err := New(context.Background(), cfg)
	require.NoError(t, err)
	return c
}

func collect(t *testing.T, c *Client, req llm.Request) []llm.Event {
	t.Helper()
	ch, err := c.Stream(context.Background(), req)
	require.NoError(t, err)
	var evs []llm.Event
	for ev := range ch {
		evs = append(evs, ev)
	}
	require.NotEmpty(t, evs)
	require.Equal(t, llm.EventStop, evs[len(evs)-1].Type, "stream must end with stop")
	return evs
}

var tools = []llm.ToolSpec{
	{
		Name:        "get_quote",
		Description: "Get a quote",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"symbol":{"type":"string","description":"ticker"},"repeat":{"type":"boolean","default":false}},"required":["symbol"],"additionalProperties":false}`),
	},
	{Name: "list_alerts", Description: "List alerts", InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)},
}

func TestTextStreamAndRequestShape(t *testing.T) {
	f := &fakeAPI{scripted: []func(http.ResponseWriter){sse(
		`{"candidates":[{"content":{"role":"model","parts":[{"text":"Hello "}]}}]}`,
		`{"candidates":[{"content":{"role":"model","parts":[{"text":"world"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":12,"candidatesTokenCount":3,"thoughtsTokenCount":2}}`,
	)}}
	c := newTestClient(t, f)
	evs := collect(t, c, llm.Request{
		System:    "be brief",
		Messages:  []llm.Message{llm.TextMessage(llm.RoleUser, "hi")},
		Tools:     tools,
		MaxTokens: 256,
	})

	var text strings.Builder
	for _, ev := range evs {
		if ev.Type == llm.EventTextDelta {
			text.WriteString(ev.TextDelta)
		}
	}
	assert.Equal(t, "Hello world", text.String())
	stop := evs[len(evs)-1]
	require.NoError(t, stop.Err)
	assert.Equal(t, "STOP", stop.StopReason)
	assert.Equal(t, llm.Usage{InputTokens: 12, OutputTokens: 5}, stop.Usage)

	require.Len(t, f.bodies, 1)
	assert.Contains(t, f.paths[0], "gemini-test:streamGenerateContent")
	body := f.bodies[0]
	sys := body["systemInstruction"].(map[string]any)["parts"].([]any)[0].(map[string]any)["text"]
	assert.Equal(t, "be brief", sys)
	assert.EqualValues(t, 256, body["generationConfig"].(map[string]any)["maxOutputTokens"])

	decls := body["tools"].([]any)[0].(map[string]any)["functionDeclarations"].([]any)
	require.Len(t, decls, 2)
	quote := decls[0].(map[string]any)
	schema, _ := json.Marshal(quote["parametersJsonSchema"])
	assert.NotContains(t, string(schema), "additionalProperties")
	assert.NotContains(t, string(schema), "default")
	assert.Contains(t, string(schema), `"required":["symbol"]`)
	_, hasParams := decls[1].(map[string]any)["parametersJsonSchema"]
	assert.False(t, hasParams, "parameterless tools omit the schema")
}

func TestParallelFunctionCallsAndRoundTrip(t *testing.T) {
	sig := base64.StdEncoding.EncodeToString([]byte("sig-1"))
	f := &fakeAPI{scripted: []func(http.ResponseWriter){
		sse(`{"candidates":[{"content":{"role":"model","parts":[` +
			`{"functionCall":{"name":"get_quote","args":{"symbol":"AAPL"}},"thoughtSignature":"` + sig + `"},` +
			`{"functionCall":{"name":"get_quote","args":{"symbol":"MSFT"}}}` +
			`]},"finishReason":"STOP"}]}`),
		sse(`{"candidates":[{"content":{"role":"model","parts":[{"text":"done"}]},"finishReason":"STOP"}]}`),
	}}
	c := newTestClient(t, f)
	evs := collect(t, c, llm.Request{Messages: []llm.Message{llm.TextMessage(llm.RoleUser, "quotes")}, Tools: tools})

	var calls []llm.ContentBlock
	for _, ev := range evs {
		if ev.Type == llm.EventToolUse {
			calls = append(calls, *ev.ToolUse)
		}
	}
	require.Len(t, calls, 2)
	assert.Equal(t, `{"symbol":"AAPL"}`, string(calls[0].Input))
	assert.Equal(t, `{"symbol":"MSFT"}`, string(calls[1].Input))
	assert.NotEqual(t, calls[0].ToolUseID, calls[1].ToolUseID)
	assert.True(t, strings.HasPrefix(calls[0].ToolUseID, "call_"))
	assert.Contains(t, string(calls[0].ProviderMeta), "thought_signature")
	assert.Empty(t, calls[1].ProviderMeta)

	// Second turn: replay the calls and send results back.
	history := []llm.Message{
		llm.TextMessage(llm.RoleUser, "quotes"),
		{Role: llm.RoleAssistant, Blocks: calls},
		{Role: llm.RoleUser, Blocks: []llm.ContentBlock{
			{Type: llm.BlockToolResult, ToolUseID: calls[0].ToolUseID, ToolName: "get_quote", Content: `{"price":1}`},
			{Type: llm.BlockToolResult, ToolUseID: calls[1].ToolUseID, ToolName: "get_quote", Content: "symbol not found", IsError: true},
		}},
	}
	collect(t, c, llm.Request{Messages: history, Tools: tools})

	contents := f.bodies[1]["contents"].([]any)
	require.Len(t, contents, 3)
	model := contents[1].(map[string]any)
	assert.Equal(t, "model", model["role"])
	parts := model["parts"].([]any)
	assert.Equal(t, sig, parts[0].(map[string]any)["thoughtSignature"], "thought signature replayed unchanged")
	_, hasID := parts[0].(map[string]any)["functionCall"].(map[string]any)["id"]
	assert.False(t, hasID, "locally generated IDs are not sent to Gemini")

	user := contents[2].(map[string]any)
	assert.Equal(t, "user", user["role"])
	rparts := user["parts"].([]any)
	require.Len(t, rparts, 2, "all responses in one user content")
	fr0 := rparts[0].(map[string]any)["functionResponse"].(map[string]any)
	assert.Equal(t, "get_quote", fr0["name"])
	assert.Equal(t, map[string]any{"output": map[string]any{"price": 1.0}}, fr0["response"])
	fr1 := rparts[1].(map[string]any)["functionResponse"].(map[string]any)
	assert.Equal(t, map[string]any{"error": "symbol not found"}, fr1["response"])
}

func TestProviderIDRoundTrip(t *testing.T) {
	f := &fakeAPI{scripted: []func(http.ResponseWriter){
		sse(`{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"id":"abc","name":"list_alerts","args":{}}}]},"finishReason":"STOP"}]}`),
		sse(`{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}]}`),
	}}
	c := newTestClient(t, f)
	evs := collect(t, c, llm.Request{Messages: []llm.Message{llm.TextMessage(llm.RoleUser, "x")}})
	call := *evs[0].ToolUse
	assert.Equal(t, "abc", call.ToolUseID)
	collect(t, c, llm.Request{Messages: []llm.Message{
		llm.TextMessage(llm.RoleUser, "x"),
		{Role: llm.RoleAssistant, Blocks: []llm.ContentBlock{call}},
		{Role: llm.RoleUser, Blocks: []llm.ContentBlock{{Type: llm.BlockToolResult, ToolUseID: "abc", ToolName: "list_alerts", Content: "[]"}}},
	}})
	contents := f.bodies[1]["contents"].([]any)
	fr := contents[2].(map[string]any)["parts"].([]any)[0].(map[string]any)["functionResponse"].(map[string]any)
	assert.Equal(t, "abc", fr["id"])
}

func TestRetryOn429ThenSuccess(t *testing.T) {
	rateLimited := `{"error":{"code":429,"message":"quota","status":"RESOURCE_EXHAUSTED","details":[{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"0.001s"}]}}`
	f := &fakeAPI{scripted: []func(http.ResponseWriter){
		status(http.StatusTooManyRequests, rateLimited),
		status(http.StatusServiceUnavailable, `{"error":{"code":503,"message":"overloaded","status":"UNAVAILABLE"}}`),
		sse(`{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}]}`),
	}}
	c := newTestClient(t, f)
	evs := collect(t, c, llm.Request{Messages: []llm.Message{llm.TextMessage(llm.RoleUser, "x")}})
	assert.NoError(t, evs[len(evs)-1].Err)
	assert.Len(t, f.bodies, 3)
}

func TestRetriesExhausted(t *testing.T) {
	body := `{"error":{"code":429,"message":"quota","status":"RESOURCE_EXHAUSTED"}}`
	f := &fakeAPI{scripted: []func(http.ResponseWriter){
		status(429, body), status(429, body), status(429, body),
	}}
	c := newTestClient(t, f)
	evs := collect(t, c, llm.Request{Messages: []llm.Message{llm.TextMessage(llm.RoleUser, "x")}})
	err := evs[len(evs)-1].Err
	var le *llm.Error
	require.ErrorAs(t, err, &le)
	assert.Equal(t, llm.KindRateLimited, le.Kind)
	assert.True(t, le.Retryable)
	assert.Len(t, f.bodies, 3, "1 try + 2 retries")
}

func TestNoRetryOnBadRequest(t *testing.T) {
	f := &fakeAPI{scripted: []func(http.ResponseWriter){
		status(400, `{"error":{"code":400,"message":"bad model","status":"INVALID_ARGUMENT"}}`),
	}}
	c := newTestClient(t, f)
	evs := collect(t, c, llm.Request{Messages: []llm.Message{llm.TextMessage(llm.RoleUser, "x")}})
	var le *llm.Error
	require.ErrorAs(t, evs[len(evs)-1].Err, &le)
	assert.Equal(t, llm.KindBadRequest, le.Kind)
	assert.Len(t, f.bodies, 1)
}

func TestSafetyFinishIsUserFacingError(t *testing.T) {
	f := &fakeAPI{scripted: []func(http.ResponseWriter){
		sse(`{"candidates":[{"finishReason":"SAFETY"}]}`),
	}}
	c := newTestClient(t, f)
	evs := collect(t, c, llm.Request{Messages: []llm.Message{llm.TextMessage(llm.RoleUser, "x")}})
	msg, retry := llm.UserMessage(evs[len(evs)-1].Err)
	assert.Contains(t, msg, "content filter")
	assert.False(t, retry)
}

func TestSanitizeSchemaNested(t *testing.T) {
	in := map[string]any{
		"type": "object", "additionalProperties": false, "$schema": "x",
		"properties": map[string]any{
			"symbols": map[string]any{"type": "array", "minItems": 2.0, "items": map[string]any{"type": "string", "pattern": "^[A-Z]+$"}},
		},
	}
	out := sanitizeSchema(in).(map[string]any)
	assert.NotContains(t, out, "additionalProperties")
	assert.NotContains(t, out, "$schema")
	items := out["properties"].(map[string]any)["symbols"].(map[string]any)["items"].(map[string]any)
	assert.NotContains(t, items, "pattern")
	assert.Equal(t, "string", items["type"])
}

func TestMergesConsecutiveSameRole(t *testing.T) {
	got, err := toContents([]llm.Message{
		llm.TextMessage(llm.RoleUser, "a"),
		llm.TextMessage(llm.RoleUser, "b"),
		llm.TextMessage(llm.RoleAssistant, "c"),
	})
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Len(t, got[0].Parts, 2)
}

func TestFallbackModelAfterOverload(t *testing.T) {
	overloaded := `{"error":{"code":503,"message":"This model is currently experiencing high demand.","status":"UNAVAILABLE"}}`
	f := &fakeAPI{scripted: []func(http.ResponseWriter){
		status(503, overloaded), status(503, overloaded), status(503, overloaded),
		sse(`{"candidates":[{"content":{"role":"model","parts":[{"text":"from fallback"}]},"finishReason":"STOP"}]}`),
	}}
	c := newTestClient(t, f, "gemini-backup")
	evs := collect(t, c, llm.Request{Messages: []llm.Message{llm.TextMessage(llm.RoleUser, "x")}})
	require.NoError(t, evs[len(evs)-1].Err)
	assert.Equal(t, "from fallback", evs[0].TextDelta)
	require.Len(t, f.paths, 4)
	assert.Contains(t, f.paths[2], "gemini-test:")
	assert.Contains(t, f.paths[3], "gemini-backup:", "fallback used after primary retries are exhausted")
}

func TestNoFallbackOnBadRequest(t *testing.T) {
	f := &fakeAPI{scripted: []func(http.ResponseWriter){
		status(400, `{"error":{"code":400,"message":"bad","status":"INVALID_ARGUMENT"}}`),
	}}
	c := newTestClient(t, f, "gemini-backup")
	evs := collect(t, c, llm.Request{Messages: []llm.Message{llm.TextMessage(llm.RoleUser, "x")}})
	require.Error(t, evs[len(evs)-1].Err)
	assert.Len(t, f.paths, 1, "non-retryable errors don't switch models")
}

const dailyQuotaBody = `{"error":{"code":429,"message":"You exceeded your current quota","status":"RESOURCE_EXHAUSTED","details":[` +
	`{"@type":"type.googleapis.com/google.rpc.QuotaFailure","violations":[{"quotaMetric":"generativelanguage.googleapis.com/generate_content_free_tier_requests","quotaId":"GenerateRequestsPerDayPerProjectPerModel-FreeTier","quotaValue":"20"}]},` +
	`{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"79963s"}]}}`

func TestDailyQuotaSkipsStraightToFallback(t *testing.T) {
	f := &fakeAPI{scripted: []func(http.ResponseWriter){
		status(429, dailyQuotaBody),
		sse(`{"candidates":[{"content":{"role":"model","parts":[{"text":"from fallback"}]},"finishReason":"STOP"}]}`),
	}}
	c := newTestClient(t, f, "gemini-backup")
	start := time.Now()
	evs := collect(t, c, llm.Request{Messages: []llm.Message{llm.TextMessage(llm.RoleUser, "x")}})
	require.NoError(t, evs[len(evs)-1].Err)
	assert.Len(t, f.paths, 2, "no pointless retries against an exhausted daily quota")
	assert.Contains(t, f.paths[1], "gemini-backup:")
	assert.Less(t, time.Since(start), time.Second)
}

func TestDailyQuotaWithoutFallbackFailsFast(t *testing.T) {
	f := &fakeAPI{scripted: []func(http.ResponseWriter){status(429, dailyQuotaBody)}}
	c := newTestClient(t, f)
	evs := collect(t, c, llm.Request{Messages: []llm.Message{llm.TextMessage(llm.RoleUser, "x")}})
	msg, _ := llm.UserMessage(evs[len(evs)-1].Err)
	assert.Contains(t, msg, "daily free quota")
	assert.Len(t, f.paths, 1)
}
