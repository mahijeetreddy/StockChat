package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mahijeetreddy/stockchat/backend/internal/domain"
	"github.com/mahijeetreddy/stockchat/backend/internal/llm"
	"github.com/mahijeetreddy/stockchat/backend/internal/llm/fake"
	"github.com/mahijeetreddy/stockchat/backend/internal/tools"
)

// stubTool is a configurable read-only tool.
type stubTool struct {
	name  string
	delay map[string]time.Duration // by "symbol" input
	err   error
	calls sync.Map
}

func (s *stubTool) Spec() llm.ToolSpec {
	return llm.ToolSpec{Name: s.name, Description: "stub", InputSchema: json.RawMessage(`{"type":"object","properties":{"symbol":{"type":"string"}}}`)}
}
func (s *stubTool) Mutating() bool { return false }
func (s *stubTool) Execute(ctx context.Context, input json.RawMessage) (tools.Result, error) {
	var in struct{ Symbol string }
	_ = json.Unmarshal(input, &in)
	s.calls.Store(in.Symbol, true)
	if d := s.delay[in.Symbol]; d > 0 {
		select {
		case <-time.After(d):
		case <-ctx.Done():
			return tools.Result{}, ctx.Err()
		}
	}
	if s.err != nil {
		return tools.Result{}, s.err
	}
	ui, _ := tools.NewUIBlock(tools.UIQuoteCard, map[string]string{"symbol": in.Symbol})
	return tools.Result{ForModel: `{"symbol":"` + in.Symbol + `","price":1}`, UI: ui}, nil
}

// stubMutating is a mutating tool that must never Execute inside the loop.
type stubMutating struct{ executed bool }

func (s *stubMutating) Spec() llm.ToolSpec {
	return llm.ToolSpec{Name: "create_alert", Description: "stub", InputSchema: json.RawMessage(`{"type":"object"}`)}
}
func (s *stubMutating) Mutating() bool { return true }
func (s *stubMutating) Execute(context.Context, json.RawMessage) (tools.Result, error) {
	s.executed = true
	return tools.Result{}, nil
}
func (s *stubMutating) Prepare(_ context.Context, in json.RawMessage) (string, json.RawMessage, error) {
	return "Alert when AAPL > $250, one time", in, nil
}

type memActions struct {
	mu      sync.Mutex
	actions []domain.PendingAction
}

func (m *memActions) CreatePendingAction(_ context.Context, a domain.PendingAction) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.actions = append(m.actions, a)
	return nil
}

type recorder struct {
	mu     sync.Mutex
	events []Event
}

func (r *recorder) emit(ev Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, ev)
}

func (r *recorder) types() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.events))
	for i, e := range r.events {
		out[i] = e.Type
	}
	return out
}

func (r *recorder) text() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var sb strings.Builder
	for _, e := range r.events {
		if d, ok := e.Data.(TextDeltaData); ok {
			sb.WriteString(d.Text)
		}
	}
	return sb.String()
}

func newAgent(client llm.Client, ts ...tools.Tool) (*Agent, *MemoryHistory) {
	h := NewMemoryHistory()
	return &Agent{
		LLM:         client,
		Tools:       tools.NewRegistry(ts...),
		History:     h,
		ToolTimeout: time.Second,
		Now:         func() time.Time { return time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC) },
	}, h
}

func run(t *testing.T, a *Agent, ctx context.Context, text string) (*recorder, error) {
	t.Helper()
	rec := &recorder{}
	err := a.Run(ctx, RunInput{ConversationID: "c1", UserText: text}, rec.emit)
	return rec, err
}

// (a) text-only reply
func TestTextOnly(t *testing.T) {
	client := fake.New(fake.Response{Text: []string{"Hi ", "there"}, Usage: llm.Usage{InputTokens: 5, OutputTokens: 2}})
	a, h := newAgent(client)
	rec, err := run(t, a, context.Background(), "hello")
	require.NoError(t, err)
	assert.Equal(t, []string{EventTextDelta, EventTextDelta, EventDone}, rec.types())
	assert.Equal(t, "Hi there", rec.text())
	done := rec.events[2].Data.(DoneData)
	assert.Equal(t, 5, done.Usage.InputTokens)

	msgs, _ := h.LoadMessages(context.Background(), "c1")
	require.Len(t, msgs, 2)
	assert.Equal(t, "hello", msgs[0].Text())
	assert.Equal(t, "Hi there", msgs[1].Text())

	req := client.Requests()[0]
	assert.Contains(t, req.System, "StockChat")
	assert.Contains(t, req.System, "2026-10-03")
}

// (b) one tool call then final text
func TestOneToolThenText(t *testing.T) {
	quote := &stubTool{name: "get_quote"}
	client := fake.New(
		fake.Response{Text: []string{"Let me check. "}, ToolCalls: []fake.ToolCall{{ID: "c1", Name: "get_quote", Input: map[string]string{"symbol": "AAPL"}}}},
		fake.Response{Text: []string{"AAPL is $1."}},
	)
	a, h := newAgent(client, quote)
	rec, err := run(t, a, context.Background(), "price of apple")
	require.NoError(t, err)
	assert.Equal(t, []string{EventTextDelta, EventToolStart, EventToolResult, EventTextDelta, EventDone}, rec.types())

	res := rec.events[2].Data.(ToolResultData)
	assert.True(t, res.OK)
	require.NotNil(t, res.UI)
	assert.Equal(t, tools.UIQuoteCard, res.UI.Type)

	// Second request carries the tool_use and the tool_result.
	second := client.Requests()[1].Messages
	require.Len(t, second, 3)
	assert.Equal(t, "get_quote", second[1].ToolUses()[0].ToolName)
	tr := second[2].Blocks[0]
	assert.Equal(t, llm.BlockToolResult, tr.Type)
	assert.Equal(t, "c1", tr.ToolUseID)
	assert.Contains(t, tr.Content, `"price":1`)

	ui := h.UI("c1")
	require.Len(t, ui, 4) // user, assistant(tool_use), results, assistant(text)
	require.Len(t, ui[2], 1)
	assert.Equal(t, "c1", ui[2][0].CallID)
	assert.NotNil(t, ui[2][0].UI)
}

// (c) parallel tool calls with out-of-order completion keep call order
func TestParallelToolsPreserveOrder(t *testing.T) {
	hist := &stubTool{name: "get_history", delay: map[string]time.Duration{"NVDA": 120 * time.Millisecond, "AMD": 60 * time.Millisecond}}
	client := fake.New(
		fake.Response{ToolCalls: []fake.ToolCall{
			{ID: "a", Name: "get_history", Input: map[string]string{"symbol": "NVDA"}},
			{ID: "b", Name: "get_history", Input: map[string]string{"symbol": "AMD"}},
			{ID: "c", Name: "get_history", Input: map[string]string{"symbol": "INTC"}},
		}},
		fake.Response{Text: []string{"compared"}},
	)
	a, _ := newAgent(client, hist)
	start := time.Now()
	rec, err := run(t, a, context.Background(), "compare")
	require.NoError(t, err)
	assert.Less(t, time.Since(start), 200*time.Millisecond, "calls must run concurrently")

	// Results arrive in completion order: INTC, AMD, NVDA.
	var resultOrder []string
	for _, e := range rec.events {
		if d, ok := e.Data.(ToolResultData); ok {
			resultOrder = append(resultOrder, d.CallID)
		}
	}
	assert.Equal(t, []string{"c", "b", "a"}, resultOrder)

	// History keeps the model's call order.
	blocks := client.Requests()[1].Messages[2].Blocks
	require.Len(t, blocks, 3)
	assert.Equal(t, []string{"a", "b", "c"}, []string{blocks[0].ToolUseID, blocks[1].ToolUseID, blocks[2].ToolUseID})
}

// (d) tool error is surfaced to the model with a safe message
func TestToolErrorSurfaced(t *testing.T) {
	quote := &stubTool{name: "get_quote", err: errors.New("GET https://finnhub.io/api/v1/quote?token=SECRET: 500")}
	client := fake.New(
		fake.Response{ToolCalls: []fake.ToolCall{{ID: "x", Name: "get_quote", Input: map[string]string{"symbol": "AAPL"}}}},
		fake.Response{Text: []string{"Sorry, data is unavailable."}},
	)
	a, _ := newAgent(client, quote)
	rec, err := run(t, a, context.Background(), "price?")
	require.NoError(t, err)
	res := rec.events[1].Data.(ToolResultData)
	assert.False(t, res.OK)
	assert.NotContains(t, res.Error, "SECRET")

	tr := client.Requests()[1].Messages[2].Blocks[0]
	assert.True(t, tr.IsError)
	assert.NotContains(t, tr.Content, "SECRET")
	assert.NotContains(t, tr.Content, "finnhub")
}

func TestUnknownToolAndInvalidInput(t *testing.T) {
	client := fake.New(
		fake.Response{ToolCalls: []fake.ToolCall{{ID: "x", Name: "rm_rf", Input: map[string]string{}}}},
		fake.Response{Text: []string{"ok"}},
	)
	a, _ := newAgent(client)
	rec, err := run(t, a, context.Background(), "do it")
	require.NoError(t, err)
	res := rec.events[1].Data.(ToolResultData)
	assert.False(t, res.OK)
	assert.Contains(t, res.Error, "unknown tool")
}

// (e) iteration cap
func TestIterationCap(t *testing.T) {
	quote := &stubTool{name: "get_quote"}
	client := fake.NewFunc(func(turn int, _ llm.Request) fake.Response {
		return fake.Response{ToolCalls: []fake.ToolCall{{Name: "get_quote", Input: map[string]string{"symbol": "AAPL"}}}}
	})
	a, _ := newAgent(client, quote)
	a.MaxIterations = 3
	rec, err := run(t, a, context.Background(), "loop forever")
	require.ErrorIs(t, err, ErrIterationLimit)
	assert.Len(t, client.Requests(), 3)
	last := rec.events[len(rec.events)-1]
	assert.Equal(t, EventError, last.Type)
	assert.Contains(t, last.Data.(ErrorData).Message, "step limit")
}

// (f) context cancel mid-stream stops the run
func TestCancelMidStream(t *testing.T) {
	client := fake.New(fake.Response{Text: []string{"partial "}, BlockUntilCancel: true})
	a, h := newAgent(client)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	rec := &recorder{}
	go func() { done <- a.Run(ctx, RunInput{ConversationID: "c1", UserText: "hi"}, rec.emit) }()

	require.Eventually(t, func() bool { return rec.text() == "partial " }, time.Second, 5*time.Millisecond)
	cancel()
	select {
	case err := <-done:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("Run did not return after cancel")
	}
	for _, typ := range rec.types() {
		assert.NotEqual(t, EventDone, typ)
	}
	msgs, _ := h.LoadMessages(context.Background(), "c1")
	require.Len(t, msgs, 2, "user message and partial text are kept")
	assert.Equal(t, "partial ", msgs[1].Text())
}

func TestLLMErrorBecomesErrorEvent(t *testing.T) {
	client := fake.New(fake.Response{Err: &llm.Error{Kind: llm.KindRateLimited, UserMsg: "rate limited", Retryable: true}})
	a, _ := newAgent(client)
	rec, err := run(t, a, context.Background(), "hi")
	require.Error(t, err)
	last := rec.events[len(rec.events)-1]
	assert.Equal(t, ErrorData{Message: "rate limited", Retryable: true}, last.Data)
}

// (g) mutating tool creates a pending action instead of executing
func TestMutatingToolStagesAction(t *testing.T) {
	mut := &stubMutating{}
	actions := &memActions{}
	client := fake.New(
		fake.Response{ToolCalls: []fake.ToolCall{{ID: "m1", Name: "create_alert", Input: map[string]any{"symbol": "AAPL", "kind": "price_above", "threshold": 250}}}},
		fake.Response{Text: []string{"Waiting for your confirmation."}},
	)
	a, h := newAgent(client, mut)
	a.Actions = actions
	rec, err := run(t, a, context.Background(), "alert me if AAPL > 250")
	require.NoError(t, err)

	assert.False(t, mut.executed, "mutating tool must not execute inside the loop")
	require.Len(t, actions.actions, 1)
	act := actions.actions[0]
	assert.Equal(t, "create_alert", act.ToolName)
	assert.Equal(t, "c1", act.ConversationID)
	assert.Equal(t, domain.ActionPending, act.Status)
	assert.Equal(t, 15*time.Minute, act.ExpiresAt.Sub(act.CreatedAt))

	assert.Contains(t, rec.types(), EventConfirm)
	tr := client.Requests()[1].Messages[2].Blocks[0]
	assert.Contains(t, tr.Content, "pending_confirmation")
	assert.Contains(t, tr.Content, act.ID)

	ui := h.UI("c1")[2][0]
	require.NotNil(t, ui.Confirm)
	assert.Equal(t, act.ID, ui.Confirm.ActionID)
}

func TestMutatingWithoutActionStoreFails(t *testing.T) {
	client := fake.New(
		fake.Response{ToolCalls: []fake.ToolCall{{ID: "m1", Name: "create_alert", Input: map[string]any{}}}},
		fake.Response{Text: []string{"x"}},
	)
	a, _ := newAgent(client, &stubMutating{})
	rec, err := run(t, a, context.Background(), "alert")
	require.NoError(t, err)
	assert.False(t, rec.events[1].Data.(ToolResultData).OK)
}

func TestTrimHistory(t *testing.T) {
	u := func(s string) llm.Message { return llm.TextMessage(llm.RoleUser, s) }
	as := func(s string) llm.Message { return llm.TextMessage(llm.RoleAssistant, s) }
	tr := llm.Message{Role: llm.RoleUser, Blocks: []llm.ContentBlock{{Type: llm.BlockToolResult}}}
	msgs := []llm.Message{u("1"), as("2"), tr, as("3"), u("4"), as("5")}
	got := TrimHistory(msgs, 4)
	require.Len(t, got, 2)
	assert.Equal(t, "4", got[0].Text(), "never starts at a tool result or assistant turn")
	assert.Len(t, TrimHistory(msgs, 10), 6)
}
