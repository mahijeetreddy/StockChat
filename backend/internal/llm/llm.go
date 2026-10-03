// Package llm defines a provider-neutral chat client interface with tool
// calling and streaming. Concrete clients live in subpackages (gemini, fake).
package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Roles.
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

// Block types.
const (
	BlockText       = "text"
	BlockToolUse    = "tool_use"
	BlockToolResult = "tool_result"
)

// Message is one turn in the conversation.
type Message struct {
	Role   string         `json:"role"`
	Blocks []ContentBlock `json:"blocks"`
}

// ContentBlock is a text, tool_use, or tool_result block.
type ContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	ToolName  string          `json:"tool_name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`   // tool_use
	Content   string          `json:"content,omitempty"` // tool_result (JSON string or text)
	IsError   bool            `json:"is_error,omitempty"`
	// ProviderMeta is opaque provider data that must round-trip with the block
	// (e.g. Gemini thought signatures). Persist it as-is.
	ProviderMeta json.RawMessage `json:"provider_meta,omitempty"`
}

// TextMessage builds a single-text-block message.
func TextMessage(role, text string) Message {
	return Message{Role: role, Blocks: []ContentBlock{{Type: BlockText, Text: text}}}
}

// Text concatenates the message's text blocks.
func (m Message) Text() string {
	var sb strings.Builder
	for _, b := range m.Blocks {
		if b.Type == BlockText {
			sb.WriteString(b.Text)
		}
	}
	return sb.String()
}

// ToolUses returns the message's tool_use blocks.
func (m Message) ToolUses() []ContentBlock {
	var out []ContentBlock
	for _, b := range m.Blocks {
		if b.Type == BlockToolUse {
			out = append(out, b)
		}
	}
	return out
}

// HasToolResults reports whether the message carries tool results.
func (m Message) HasToolResults() bool {
	for _, b := range m.Blocks {
		if b.Type == BlockToolResult {
			return true
		}
	}
	return false
}

// ToolSpec describes a tool to the model.
type ToolSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"` // simple JSON Schema
}

// Request is one model call.
type Request struct {
	System    string
	Messages  []Message
	Tools     []ToolSpec
	MaxTokens int
}

// EventType distinguishes stream events.
type EventType int

// Stream event types.
const (
	EventTextDelta EventType = iota + 1
	EventToolUse
	EventStop
)

// Usage counts tokens for one or more calls.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// Add returns the sum of two usages.
func (u Usage) Add(o Usage) Usage {
	return Usage{InputTokens: u.InputTokens + o.InputTokens, OutputTokens: u.OutputTokens + o.OutputTokens}
}

// Event is one item from a stream. A stream ends with exactly one EventStop
// (possibly carrying Err) and then the channel closes.
type Event struct {
	Type      EventType
	TextDelta string
	// TextMeta is provider metadata to attach to the current text block (e.g. a
	// thought signature that arrived on a text part). May be set with an empty delta.
	TextMeta   json.RawMessage
	ToolUse    *ContentBlock // one complete tool call
	StopReason string
	Usage      Usage
	Err        error
}

// Client streams model responses.
type Client interface {
	Stream(ctx context.Context, req Request) (<-chan Event, error)
}

// ErrorKind classifies LLM errors.
type ErrorKind int

// Error kinds.
const (
	KindOther ErrorKind = iota
	KindRateLimited
	KindUnavailable
	KindBlocked
	KindBadRequest
	KindAuth
)

// Error is a classified LLM failure with a message safe to show users.
type Error struct {
	Kind      ErrorKind
	UserMsg   string
	Retryable bool
	Err       error
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("llm: %s: %v", e.UserMsg, e.Err)
	}
	return "llm: " + e.UserMsg
}

func (e *Error) Unwrap() error { return e.Err }

// UserMessage returns a safe, user-facing description of err and whether a
// retry may help.
func UserMessage(err error) (string, bool) {
	var le *Error
	if errors.As(err, &le) {
		return le.UserMsg, le.Retryable
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "The assistant took too long to respond. Please try again.", true
	}
	return "Something went wrong talking to the assistant. Please try again.", true
}
