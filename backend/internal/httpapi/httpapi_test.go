package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mahijeetreddy/stockchat/backend/internal/agent"
	"github.com/mahijeetreddy/stockchat/backend/internal/llm"
	"github.com/mahijeetreddy/stockchat/backend/internal/llm/fake"
	"github.com/mahijeetreddy/stockchat/backend/internal/market/mock"
	"github.com/mahijeetreddy/stockchat/backend/internal/store"
	"github.com/mahijeetreddy/stockchat/backend/internal/tools"
)

var friday = time.Date(2026, 10, 2, 15, 0, 0, 0, time.UTC)

type harness struct {
	srv   *httptest.Server
	store *store.Store
	llm   llm.Client
	api   *Server
}

func newHarness(t *testing.T, client llm.Client, opts Options) *harness {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	mkt := mock.New(mock.WithClock(func() time.Time { return friday }))
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ag := &agent.Agent{
		LLM:     client,
		Tools:   tools.NewRegistry(&tools.GetQuote{Market: mkt, Now: func() time.Time { return friday }}, &tools.SearchSymbol{Market: mkt}),
		History: st,
		Actions: st,
		Logger:  logger,
	}
	api := New(st, ag, mkt, logger, opts)
	srv := httptest.NewServer(api.Handler())
	t.Cleanup(srv.Close)
	return &harness{srv: srv, store: st, llm: client, api: api}
}

type sseEvent struct {
	Name string
	Data json.RawMessage
}

// readSSE parses an SSE stream until EOF (or until stopAfter returns true).
func readSSE(t *testing.T, r io.Reader, stopAfter func(sseEvent) bool) ([]sseEvent, int) {
	t.Helper()
	var (
		events     []sseEvent
		heartbeats int
		cur        sseEvent
	)
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if cur.Name != "" {
				events = append(events, cur)
				if stopAfter != nil && stopAfter(cur) {
					return events, heartbeats
				}
			}
			cur = sseEvent{}
		case strings.HasPrefix(line, ":"):
			heartbeats++
		case strings.HasPrefix(line, "event: "):
			cur.Name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			cur.Data = json.RawMessage(strings.TrimPrefix(line, "data: "))
		}
	}
	return events, heartbeats
}

func names(evs []sseEvent) []string {
	out := make([]string, len(evs))
	for i, e := range evs {
		out[i] = e.Name
	}
	return out
}

func postChat(t *testing.T, h *harness, body string, header http.Header) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, h.srv.URL+"/api/chat", strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range header {
		req.Header[k] = v
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

func quoteScript() *fake.Client {
	return fake.New(
		fake.Response{Text: []string{"Checking. "}, ToolCalls: []fake.ToolCall{{ID: "call_1", Name: "get_quote", Input: map[string]string{"symbol": "AAPL"}}}},
		fake.Response{Text: []string{"AAPL ", "is up."}, Usage: llm.Usage{InputTokens: 10, OutputTokens: 4}},
	)
}

func TestChatStreamsOrderedEventsAndPersists(t *testing.T) {
	h := newHarness(t, quoteScript(), Options{})
	resp := postChat(t, h, `{"message":"What's AAPL at?","timezone":"America/Chicago"}`, nil)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))

	evs, _ := readSSE(t, resp.Body, nil)
	assert.Equal(t, []string{"conversation", "text_delta", "tool_start", "tool_result", "text_delta", "text_delta", "done"}, names(evs))

	var conv agent.ConversationData
	require.NoError(t, json.Unmarshal(evs[0].Data, &conv))
	require.NotEmpty(t, conv.ConversationID)

	var start agent.ToolStartData
	require.NoError(t, json.Unmarshal(evs[2].Data, &start))
	assert.Equal(t, "Fetching AAPL quote…", start.Label)

	var res map[string]any
	require.NoError(t, json.Unmarshal(evs[3].Data, &res))
	assert.Equal(t, true, res["ok"])
	assert.Equal(t, "quote_card", res["ui"].(map[string]any)["type"])

	var done agent.DoneData
	require.NoError(t, json.Unmarshal(evs[6].Data, &done))
	assert.Equal(t, 10, done.Usage.InputTokens)
	assert.NotEmpty(t, done.MessageID)

	// The user's time zone reaches the system prompt.
	assert.Contains(t, h.llm.(*fake.Client).Requests()[0].System, "America/Chicago")

	// Reloading the conversation returns messages with UI blocks.
	get, err := http.Get(h.srv.URL + "/api/conversations/" + conv.ConversationID)
	require.NoError(t, err)
	defer get.Body.Close()
	require.Equal(t, http.StatusOK, get.StatusCode)
	var body struct {
		Conversation store.Conversation `json:"conversation"`
		Messages     []map[string]any   `json:"messages"`
	}
	require.NoError(t, json.NewDecoder(get.Body).Decode(&body))
	assert.Equal(t, "What's AAPL at?", body.Conversation.Title)
	require.Len(t, body.Messages, 4)
	ui := body.Messages[2]["ui"].([]any)[0].(map[string]any)
	assert.Equal(t, "call_1", ui["call_id"])
	assert.Equal(t, "quote_card", ui["ui"].(map[string]any)["type"])
	raw, _ := json.Marshal(body.Messages)
	assert.NotContains(t, string(raw), "provider_meta", "provider metadata stays server-side")
	assert.NotContains(t, string(raw), `"content"`, "raw tool payloads stay server-side")

	// List and delete.
	list, err := http.Get(h.srv.URL + "/api/conversations")
	require.NoError(t, err)
	var lb struct{ Conversations []store.Conversation }
	require.NoError(t, json.NewDecoder(list.Body).Decode(&lb))
	list.Body.Close()
	require.Len(t, lb.Conversations, 1)

	req, _ := http.NewRequest(http.MethodDelete, h.srv.URL+"/api/conversations/"+conv.ConversationID, nil)
	del, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	del.Body.Close()
	assert.Equal(t, http.StatusNoContent, del.StatusCode)
}

func TestChatContinuesExistingConversation(t *testing.T) {
	client := fake.New(fake.Response{Text: []string{"one"}}, fake.Response{Text: []string{"two"}})
	h := newHarness(t, client, Options{})
	resp := postChat(t, h, `{"message":"first"}`, nil)
	evs, _ := readSSE(t, resp.Body, nil)
	resp.Body.Close()
	var conv agent.ConversationData
	require.NoError(t, json.Unmarshal(evs[0].Data, &conv))

	resp = postChat(t, h, `{"conversation_id":"`+conv.ConversationID+`","message":"second"}`, nil)
	evs2, _ := readSSE(t, resp.Body, nil)
	resp.Body.Close()
	assert.Equal(t, evs[0].Data, evs2[0].Data, "same conversation id")
	second := client.Requests()[1].Messages
	require.Len(t, second, 3, "history includes the first exchange")
	assert.Equal(t, "first", second[0].Text())
}

func TestChatValidation(t *testing.T) {
	h := newHarness(t, fake.New(), Options{MaxBodyBytes: 4 << 10})
	tests := []struct {
		name   string
		body   string
		status int
	}{
		{"empty message", `{"message":"   "}`, http.StatusBadRequest},
		{"unknown field", `{"message":"hi","admin":true}`, http.StatusBadRequest},
		{"bad json", `{`, http.StatusBadRequest},
		{"too long", `{"message":"` + strings.Repeat("a", 2001) + `"}`, http.StatusBadRequest},
		{"too large", `{"message":"` + strings.Repeat("a", 5000) + `"}`, http.StatusRequestEntityTooLarge},
		{"unknown conversation", `{"conversation_id":"c_nope","message":"hi"}`, http.StatusNotFound},
		{"bad conversation id", `{"conversation_id":"../etc","message":"hi"}`, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := postChat(t, h, tt.body, nil)
			resp.Body.Close()
			assert.Equal(t, tt.status, resp.StatusCode)
		})
	}
}

// ctxWatcher is an llm.Client that blocks until its context is cancelled and
// records that it was.
type ctxWatcher struct {
	started   chan struct{}
	cancelled atomic.Bool
}

func (c *ctxWatcher) Stream(ctx context.Context, _ llm.Request) (<-chan llm.Event, error) {
	out := make(chan llm.Event)
	go func() {
		defer close(out)
		select {
		case out <- llm.Event{Type: llm.EventTextDelta, TextDelta: "thinking"}:
		case <-ctx.Done():
		}
		close(c.started)
		<-ctx.Done()
		c.cancelled.Store(true)
	}()
	return out, nil
}

func TestClientDisconnectCancelsLLM(t *testing.T) {
	w := &ctxWatcher{started: make(chan struct{})}
	h := newHarness(t, w, Options{})
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, h.srv.URL+"/api/chat", strings.NewReader(`{"message":"hi"}`))
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	_, _ = readSSE(t, resp.Body, func(e sseEvent) bool { return e.Name == "text_delta" })
	<-w.started
	cancel()
	resp.Body.Close()
	require.Eventually(t, w.cancelled.Load, 2*time.Second, 10*time.Millisecond, "upstream LLM call must be cancelled")
}

func TestHeartbeat(t *testing.T) {
	w := &ctxWatcher{started: make(chan struct{})}
	h := newHarness(t, w, Options{HeartbeatInterval: 20 * time.Millisecond})
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, h.srv.URL+"/api/chat", strings.NewReader(`{"message":"hi"}`))
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	_, beats := readSSE(t, resp.Body, nil)
	assert.GreaterOrEqual(t, beats, 2)
}

func TestBusyConversationReturns409(t *testing.T) {
	h := newHarness(t, fake.New(fake.Response{Text: []string{"x"}}), Options{})
	conv, err := h.store.CreateConversation(context.Background(), "c_busy")
	require.NoError(t, err)
	require.True(t, h.api.tryLock(conv.ID))
	resp := postChat(t, h, `{"conversation_id":"c_busy","message":"hi"}`, nil)
	resp.Body.Close()
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
}

func TestAuth(t *testing.T) {
	h := newHarness(t, fake.New(fake.Response{Text: []string{"x"}}), Options{AppToken: "s3cret"})
	resp, err := http.Get(h.srv.URL + "/api/conversations")
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	req, _ := http.NewRequest(http.MethodGet, h.srv.URL+"/api/conversations", nil)
	req.Header.Set("Authorization", "Bearer s3cret")
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Health check stays open for container probes.
	resp, err = http.Get(h.srv.URL + "/healthz")
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestChatRateLimit(t *testing.T) {
	client := fake.NewFunc(func(int, llm.Request) fake.Response { return fake.Response{Text: []string{"x"}} })
	h := newHarness(t, client, Options{ChatPerMinute: 1})
	codes := map[int]int{}
	for range 12 {
		resp := postChat(t, h, `{"message":"hi"}`, nil)
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		codes[resp.StatusCode]++
	}
	assert.Equal(t, 10, codes[http.StatusOK], "burst of 10")
	assert.Equal(t, 2, codes[http.StatusTooManyRequests])
}

func TestCORS(t *testing.T) {
	h := newHarness(t, fake.New(), Options{CORSOrigin: "http://localhost:5173"})
	req, _ := http.NewRequest(http.MethodOptions, h.srv.URL+"/api/chat", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	req.Header.Set("Access-Control-Request-Method", "POST")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	assert.Equal(t, "http://localhost:5173", resp.Header.Get("Access-Control-Allow-Origin"))

	req.Header.Set("Origin", "http://evil.example")
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Empty(t, resp.Header.Get("Access-Control-Allow-Origin"))
}

func TestMarketStatus(t *testing.T) {
	h := newHarness(t, fake.New(), Options{})
	resp, err := http.Get(h.srv.URL + "/api/market/status")
	require.NoError(t, err)
	defer resp.Body.Close()
	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, true, body["open"], "mock clock is Friday 11:00 ET")
}
