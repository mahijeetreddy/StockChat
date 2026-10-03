package agent

import (
	"encoding/json"

	"github.com/mahijeetreddy/stockchat/backend/internal/llm"
	"github.com/mahijeetreddy/stockchat/backend/internal/tools"
)

// SSE event names (PLAN.md section 10).
const (
	EventConversation = "conversation"
	EventTextDelta    = "text_delta"
	EventToolStart    = "tool_start"
	EventToolResult   = "tool_result"
	EventConfirm      = "confirmation_required"
	EventError        = "error"
	EventDone         = "done"
)

// Event is something the agent reports to the client while running.
type Event struct {
	Type string
	Data any // one of the *Data structs below; JSON-encoded on the wire
}

// ConversationData is the payload of "conversation".
type ConversationData struct {
	ConversationID string `json:"conversation_id"`
}

// TextDeltaData is the payload of "text_delta".
type TextDeltaData struct {
	Text string `json:"text"`
}

// ToolStartData is the payload of "tool_start".
type ToolStartData struct {
	CallID string `json:"call_id"`
	Name   string `json:"name"`
	Label  string `json:"label"`
}

// ToolResultData is the payload of "tool_result". UI comes from Go structs only.
type ToolResultData struct {
	CallID string         `json:"call_id"`
	Name   string         `json:"name"`
	OK     bool           `json:"ok"`
	Error  string         `json:"error,omitempty"`
	UI     *tools.UIBlock `json:"ui,omitempty"`
}

// ConfirmData is the payload of "confirmation_required".
type ConfirmData struct {
	ActionID string          `json:"action_id"`
	CallID   string          `json:"call_id"`
	Summary  string          `json:"summary"`
	Tool     string          `json:"tool"`
	Input    json.RawMessage `json:"input"`
	Status   string          `json:"status"`
	Result   string          `json:"result,omitempty"`
}

// ErrorData is the payload of "error".
type ErrorData struct {
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

// DoneData is the payload of "done".
type DoneData struct {
	MessageID string    `json:"message_id"`
	Usage     llm.Usage `json:"usage"`
}

// UIEntry is persisted with a tool-results message so a reloaded conversation
// re-renders the same chips, cards, and confirmation prompts.
type UIEntry struct {
	CallID  string         `json:"call_id"`
	Name    string         `json:"name"`
	Label   string         `json:"label,omitempty"`
	OK      bool           `json:"ok"`
	Error   string         `json:"error,omitempty"`
	UI      *tools.UIBlock `json:"ui,omitempty"`
	Confirm *ConfirmData   `json:"confirm,omitempty"`
}
