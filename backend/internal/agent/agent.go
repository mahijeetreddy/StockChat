// Package agent runs the LLM tool-calling loop for one chat turn.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	"golang.org/x/sync/errgroup"

	"github.com/mahijeetreddy/stockchat/backend/internal/domain"
	"github.com/mahijeetreddy/stockchat/backend/internal/llm"
	"github.com/mahijeetreddy/stockchat/backend/internal/tools"
)

// History persists conversation messages. The SQLite store implements it; so
// does MemoryHistory for CLIs and tests.
type History interface {
	LoadMessages(ctx context.Context, conversationID string) ([]llm.Message, error)
	AppendMessage(ctx context.Context, conversationID string, m llm.Message, ui []UIEntry) (int64, error)
}

// ActionStore records pending mutating actions awaiting user confirmation.
type ActionStore interface {
	CreatePendingAction(ctx context.Context, a domain.PendingAction) error
}

// Agent runs chat turns.
type Agent struct {
	LLM           llm.Client
	Tools         *tools.Registry
	History       History
	Actions       ActionStore // nil disables mutating tools
	MaxIterations int
	MaxTokens     int
	ToolTimeout   time.Duration
	ToolParallel  int
	ActionTTL     time.Duration
	MaxHistory    int // messages sent to the model (older ones are trimmed)
	Now           func() time.Time
	Logger        *slog.Logger
}

// RunInput is one user turn.
type RunInput struct {
	ConversationID string
	UserText       string
	Location       *time.Location // user's time zone for the system prompt
}

// ErrIterationLimit is returned when the model keeps calling tools.
var ErrIterationLimit = errors.New("agent: iteration limit reached")

func (a *Agent) defaults() {
	if a.MaxIterations <= 0 {
		a.MaxIterations = 6
	}
	if a.MaxTokens <= 0 {
		a.MaxTokens = 1024
	}
	if a.ToolTimeout <= 0 {
		a.ToolTimeout = 10 * time.Second
	}
	if a.ToolParallel <= 0 {
		a.ToolParallel = 5
	}
	if a.ActionTTL <= 0 {
		a.ActionTTL = 15 * time.Minute
	}
	if a.MaxHistory <= 0 {
		a.MaxHistory = 40
	}
	if a.Now == nil {
		a.Now = time.Now
	}
	if a.Logger == nil {
		a.Logger = slog.Default()
	}
}

// Run executes one chat turn, emitting events as it goes. emit is called from
// a single goroutine at a time. Errors are also reported via an "error" event;
// the returned error is for logging.
func (a *Agent) Run(ctx context.Context, in RunInput, emitFn func(Event)) error {
	a.defaults()
	var emitMu sync.Mutex
	emit := func(ev Event) {
		emitMu.Lock()
		defer emitMu.Unlock()
		emitFn(ev)
	}
	fail := func(err error, msg string, retryable bool) error {
		emit(Event{Type: EventError, Data: ErrorData{Message: msg, Retryable: retryable}})
		return err
	}
	// Persistence must survive client disconnects so history stays consistent.
	persistCtx := context.WithoutCancel(ctx)

	history, err := a.History.LoadMessages(ctx, in.ConversationID)
	if err != nil {
		return fail(fmt.Errorf("load history: %w", err), "Couldn't load the conversation.", true)
	}
	userMsg := llm.TextMessage(llm.RoleUser, in.UserText)
	if _, err := a.History.AppendMessage(persistCtx, in.ConversationID, userMsg, nil); err != nil {
		return fail(fmt.Errorf("save user message: %w", err), "Couldn't save your message.", true)
	}
	history = append(history, userMsg)

	system := SystemPrompt(a.Now(), in.Location)
	var usage llm.Usage

	for range a.MaxIterations {
		req := llm.Request{System: system, Messages: TrimHistory(history, a.MaxHistory), Tools: a.Tools.Specs(), MaxTokens: a.MaxTokens}
		assistant, calls, u, err := a.consume(ctx, req, emit)
		usage = usage.Add(u)
		if err != nil {
			// Keep any partial text (without dangling tool calls) for context.
			if t := assistant.Text(); t != "" {
				_, _ = a.History.AppendMessage(persistCtx, in.ConversationID, llm.TextMessage(llm.RoleAssistant, t), nil)
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			msg, retry := llm.UserMessage(err)
			return fail(err, msg, retry)
		}
		if len(assistant.Blocks) == 0 {
			return fail(errors.New("empty model response"), "The assistant returned an empty response. Please try again.", true)
		}
		msgID, err := a.History.AppendMessage(persistCtx, in.ConversationID, assistant, nil)
		if err != nil {
			return fail(fmt.Errorf("save assistant message: %w", err), "Couldn't save the reply.", true)
		}
		history = append(history, assistant)

		if len(calls) == 0 {
			emit(Event{Type: EventDone, Data: DoneData{MessageID: strconv.FormatInt(msgID, 10), Usage: usage}})
			return nil
		}

		results, ui := a.executeTools(ctx, in.ConversationID, calls, emit)
		resultsMsg := llm.Message{Role: llm.RoleUser, Blocks: results}
		if _, err := a.History.AppendMessage(persistCtx, in.ConversationID, resultsMsg, ui); err != nil {
			return fail(fmt.Errorf("save tool results: %w", err), "Couldn't save tool results.", true)
		}
		history = append(history, resultsMsg)
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return fail(ErrIterationLimit, "I couldn't finish within the step limit; try a simpler question.", false)
}

// consume reads one model stream, forwarding text deltas, and returns the
// assistant message plus its tool calls in emitted order.
func (a *Agent) consume(ctx context.Context, req llm.Request, emit func(Event)) (llm.Message, []llm.ContentBlock, llm.Usage, error) {
	msg := llm.Message{Role: llm.RoleAssistant}
	var (
		calls []llm.ContentBlock
		usage llm.Usage
	)
	stream, err := a.LLM.Stream(ctx, req)
	if err != nil {
		return msg, nil, usage, err
	}
	// curText indexes the text block being appended to (-1 when the last block
	// is not text), so text after a tool call starts a new block.
	curText := -1
	for ev := range stream {
		switch ev.Type {
		case llm.EventTextDelta:
			if curText < 0 {
				msg.Blocks = append(msg.Blocks, llm.ContentBlock{Type: llm.BlockText})
				curText = len(msg.Blocks) - 1
			}
			msg.Blocks[curText].Text += ev.TextDelta
			if len(ev.TextMeta) > 0 {
				msg.Blocks[curText].ProviderMeta = ev.TextMeta
			}
			if ev.TextDelta != "" {
				emit(Event{Type: EventTextDelta, Data: TextDeltaData{Text: ev.TextDelta}})
			}
		case llm.EventToolUse:
			if ev.ToolUse == nil {
				continue
			}
			call := *ev.ToolUse
			if len(call.Input) == 0 {
				call.Input = json.RawMessage(`{}`)
			}
			msg.Blocks = append(msg.Blocks, call)
			calls = append(calls, call)
			curText = -1
		case llm.EventStop:
			usage = ev.Usage
			if ev.Err != nil {
				return msg, calls, usage, ev.Err
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return msg, calls, usage, err
	}
	// Drop empty text blocks (e.g. a bare signature with no text and no calls).
	kept := msg.Blocks[:0]
	for _, b := range msg.Blocks {
		if b.Type == llm.BlockText && b.Text == "" && len(b.ProviderMeta) == 0 {
			continue
		}
		kept = append(kept, b)
	}
	msg.Blocks = kept
	if len(calls) == 0 && strings.TrimSpace(msg.Text()) == "" {
		msg.Blocks = nil
	}
	return msg, calls, usage, nil
}

// executeTools runs calls concurrently (bounded) and returns tool_result
// blocks and UI entries in the original call order.
func (a *Agent) executeTools(ctx context.Context, convoID string, calls []llm.ContentBlock, emit func(Event)) ([]llm.ContentBlock, []UIEntry) {
	results := make([]llm.ContentBlock, len(calls))
	ui := make([]UIEntry, len(calls))

	for i, c := range calls {
		label := a.Tools.Label(c.ToolName, c.Input)
		ui[i] = UIEntry{CallID: c.ToolUseID, Name: c.ToolName, Label: label}
		emit(Event{Type: EventToolStart, Data: ToolStartData{CallID: c.ToolUseID, Name: c.ToolName, Label: label}})
	}

	var g errgroup.Group
	g.SetLimit(a.ToolParallel)
	for i, c := range calls {
		g.Go(func() error {
			res, entry := a.runOne(ctx, convoID, c, emit)
			results[i] = res
			entry.Label = ui[i].Label
			ui[i] = entry
			return nil
		})
	}
	_ = g.Wait()
	return results, ui
}

// runOne executes or stages a single call and emits its result event.
func (a *Agent) runOne(ctx context.Context, convoID string, call llm.ContentBlock, emit func(Event)) (llm.ContentBlock, UIEntry) {
	block := llm.ContentBlock{Type: llm.BlockToolResult, ToolUseID: call.ToolUseID, ToolName: call.ToolName}
	entry := UIEntry{CallID: call.ToolUseID, Name: call.ToolName}

	failWith := func(msg string) (llm.ContentBlock, UIEntry) {
		block.IsError, block.Content = true, msg
		entry.OK, entry.Error = false, msg
		emit(Event{Type: EventToolResult, Data: ToolResultData{CallID: call.ToolUseID, Name: call.ToolName, OK: false, Error: msg}})
		return block, entry
	}

	tool, ok := a.Tools.Get(call.ToolName)
	if !ok {
		return failWith(fmt.Sprintf("unknown tool %q", call.ToolName))
	}

	if tool.Mutating() {
		return a.stageAction(ctx, convoID, call, tool, block, entry, emit, failWith)
	}

	tctx, cancel := context.WithTimeout(ctx, a.ToolTimeout)
	defer cancel()
	start := time.Now()
	res, err := tool.Execute(tctx, call.Input)
	if err != nil {
		a.Logger.Warn("tool failed", "tool", call.ToolName, "err", err, "dur", time.Since(start))
		return failWith(tools.SafeMessage(err))
	}
	a.Logger.Debug("tool ok", "tool", call.ToolName, "dur", time.Since(start))
	block.Content = res.ForModel
	entry.OK, entry.UI = true, res.UI
	emit(Event{Type: EventToolResult, Data: ToolResultData{CallID: call.ToolUseID, Name: call.ToolName, OK: true, UI: res.UI}})
	return block, entry
}

// stageAction validates a mutating call and records it as a pending action.
// Nothing changes until the user confirms through the HTTP API.
func (a *Agent) stageAction(ctx context.Context, convoID string, call llm.ContentBlock, tool tools.Tool,
	block llm.ContentBlock, entry UIEntry, emit func(Event), failWith func(string) (llm.ContentBlock, UIEntry),
) (llm.ContentBlock, UIEntry) {
	if a.Actions == nil {
		return failWith("this action is not available right now")
	}
	prep := tool.(tools.Preparer) // guaranteed by NewRegistry
	tctx, cancel := context.WithTimeout(ctx, a.ToolTimeout)
	defer cancel()
	summary, normalized, err := prep.Prepare(tctx, call.Input)
	if err != nil {
		return failWith(tools.SafeMessage(err))
	}
	now := a.Now()
	action := domain.PendingAction{
		ID:             "act_" + ulid.Make().String(),
		ConversationID: convoID,
		ToolName:       call.ToolName,
		CallID:         call.ToolUseID,
		Input:          normalized,
		Summary:        summary,
		Status:         domain.ActionPending,
		CreatedAt:      now,
		ExpiresAt:      now.Add(a.ActionTTL),
	}
	if err := a.Actions.CreatePendingAction(context.WithoutCancel(ctx), action); err != nil {
		a.Logger.Error("create pending action", "err", err)
		return failWith("couldn't stage the action; please try again")
	}
	forModel, _ := json.Marshal(map[string]string{
		"status":    "pending_confirmation",
		"action_id": action.ID,
		"summary":   summary,
		"note":      "Not done yet. The user must click Confirm in the UI. Tell them it is waiting for their confirmation.",
	})
	confirm := &ConfirmData{ActionID: action.ID, CallID: call.ToolUseID, Summary: summary, Tool: call.ToolName, Input: normalized, Status: domain.ActionPending}
	block.Content = string(forModel)
	entry.OK, entry.Confirm = true, confirm
	emit(Event{Type: EventToolResult, Data: ToolResultData{CallID: call.ToolUseID, Name: call.ToolName, OK: true}})
	emit(Event{Type: EventConfirm, Data: *confirm})
	return block, entry
}

// TrimHistory keeps at most maxMsgs messages, starting at a plain user message so
// the model never sees tool results without the calls that produced them.
func TrimHistory(msgs []llm.Message, maxMsgs int) []llm.Message {
	if len(msgs) <= maxMsgs {
		return msgs
	}
	start := len(msgs) - maxMsgs
	for start < len(msgs) && (msgs[start].Role != llm.RoleUser || msgs[start].HasToolResults()) {
		start++
	}
	if start >= len(msgs) {
		return msgs[len(msgs)-1:]
	}
	return msgs[start:]
}

// MemoryHistory is an in-memory History for CLIs and tests.
type MemoryHistory struct {
	mu    sync.Mutex
	msgs  map[string][]llm.Message
	ui    map[string][][]UIEntry
	seqID int64
}

// NewMemoryHistory returns an empty MemoryHistory.
func NewMemoryHistory() *MemoryHistory {
	return &MemoryHistory{msgs: map[string][]llm.Message{}, ui: map[string][][]UIEntry{}}
}

// LoadMessages implements History.
func (h *MemoryHistory) LoadMessages(_ context.Context, id string) ([]llm.Message, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]llm.Message(nil), h.msgs[id]...), nil
}

// AppendMessage implements History.
func (h *MemoryHistory) AppendMessage(_ context.Context, id string, m llm.Message, ui []UIEntry) (int64, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.seqID++
	h.msgs[id] = append(h.msgs[id], m)
	h.ui[id] = append(h.ui[id], ui)
	return h.seqID, nil
}

// UI returns the stored UI entries per message (tests).
func (h *MemoryHistory) UI(id string) [][]UIEntry {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([][]UIEntry(nil), h.ui[id]...)
}
