package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mahijeetreddy/stockchat/backend/internal/agent"
	"github.com/mahijeetreddy/stockchat/backend/internal/llm"
	"github.com/mahijeetreddy/stockchat/backend/internal/tools"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestOpenRunsMigrationsIdempotently(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "x.db")
	s, err := Open(context.Background(), path)
	require.NoError(t, err)
	require.NoError(t, s.Close())
	s, err = Open(context.Background(), path) // second run is a no-op
	require.NoError(t, err)
	require.NoError(t, s.Close())
}

func TestConversationLifecycle(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	clock := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	s.SetClock(func() time.Time { return clock })

	_, err := s.CreateConversation(ctx, "c1")
	require.NoError(t, err)
	clock = clock.Add(time.Minute)
	_, err = s.CreateConversation(ctx, "c2")
	require.NoError(t, err)

	// Messages: user, assistant tool call, tool results with UI, final text.
	ui, _ := tools.NewUIBlock(tools.UIQuoteCard, map[string]any{"symbol": "AAPL", "price": 1.5})
	clock = clock.Add(time.Minute)
	msgs := []struct {
		m  llm.Message
		ui []agent.UIEntry
	}{
		{llm.TextMessage(llm.RoleUser, "What's Apple trading at?"), nil},
		{llm.Message{Role: llm.RoleAssistant, Blocks: []llm.ContentBlock{{
			Type: llm.BlockToolUse, ToolUseID: "call_1", ToolName: "get_quote", Input: json.RawMessage(`{"symbol":"AAPL"}`),
			ProviderMeta: json.RawMessage(`{"thought_signature":"c2ln"}`),
		}}}, nil},
		{llm.Message{Role: llm.RoleUser, Blocks: []llm.ContentBlock{{Type: llm.BlockToolResult, ToolUseID: "call_1", ToolName: "get_quote", Content: `{"price":1.5}`}}},
			[]agent.UIEntry{{CallID: "call_1", Name: "get_quote", OK: true, UI: ui}}},
		{llm.TextMessage(llm.RoleAssistant, "AAPL is $1.50."), nil},
	}
	for _, x := range msgs {
		_, err := s.AppendMessage(ctx, "c1", x.m, x.ui)
		require.NoError(t, err)
	}

	list, err := s.ListConversations(ctx, 0)
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, "c1", list[0].ID, "most recently updated first")
	assert.Equal(t, "What's Apple trading at?", list[0].Title)
	assert.Equal(t, DefaultTitle, list[1].Title)

	loaded, err := s.LoadMessages(ctx, "c1")
	require.NoError(t, err)
	require.Len(t, loaded, 4)
	assert.Equal(t, `{"thought_signature":"c2ln"}`, string(loaded[1].Blocks[0].ProviderMeta), "provider meta round-trips")
	assert.Equal(t, `{"symbol":"AAPL"}`, string(loaded[1].Blocks[0].Input))

	stored, err := s.Messages(ctx, "c1")
	require.NoError(t, err)
	require.Len(t, stored[2].UI, 1)
	assert.Equal(t, tools.UIQuoteCard, stored[2].UI[0].UI.Type)
	assert.JSONEq(t, `{"symbol":"AAPL","price":1.5}`, string(stored[2].UI[0].UI.Data))

	require.NoError(t, s.DeleteConversation(ctx, "c1"))
	assert.ErrorIs(t, s.DeleteConversation(ctx, "c1"), ErrNotFound)
	_, err = s.GetConversation(ctx, "c1")
	assert.ErrorIs(t, err, ErrNotFound)
	stored, err = s.Messages(ctx, "c1")
	require.NoError(t, err)
	assert.Empty(t, stored, "messages cascade-deleted")
}

func TestForeignKeyEnforced(t *testing.T) {
	s := newStore(t)
	_, err := s.AppendMessage(context.Background(), "missing", llm.TextMessage(llm.RoleUser, "x"), nil)
	assert.Error(t, err)
}

func TestMakeTitle(t *testing.T) {
	assert.Equal(t, "hello world", MakeTitle("  hello \n world "))
	long := MakeTitle("Compare NVDA, AMD, and INTC over the last six months and tell me which moved most")
	assert.LessOrEqual(t, len([]rune(long)), 58)
	assert.Equal(t, DefaultTitle, MakeTitle("   "))
}
