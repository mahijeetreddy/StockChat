// Package fake provides scripted llm.Client implementations for tests and
// offline development.
package fake

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/mahijeetreddy/stockchat/backend/internal/llm"
)

// ToolCall is a scripted tool call.
type ToolCall struct {
	ID    string // optional; generated if empty
	Name  string
	Input any // marshalled to JSON
}

// Response is one scripted model turn.
type Response struct {
	Text      []string // streamed as separate deltas
	ToolCalls []ToolCall
	Err       error         // delivered on the stop event
	Delay     time.Duration // wait between events
	// BlockUntilCancel makes the stream hang after emitting Text until ctx is done.
	BlockUntilCancel bool
	Usage            llm.Usage
}

// Responder decides a response from the request (turn is 0-based).
type Responder func(turn int, req llm.Request) Response

// Client replays a script or calls a Responder. It records every request.
type Client struct {
	mu        sync.Mutex
	script    []Response
	responder Responder
	requests  []llm.Request
	callSeq   int
}

var _ llm.Client = (*Client)(nil)

// New returns a client that replays responses in order.
func New(script ...Response) *Client { return &Client{script: script} }

// NewFunc returns a client driven by a Responder.
func NewFunc(r Responder) *Client { return &Client{responder: r} }

// Requests returns a copy of the recorded requests.
func (c *Client) Requests() []llm.Request {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]llm.Request(nil), c.requests...)
}

// Stream implements llm.Client.
func (c *Client) Stream(ctx context.Context, req llm.Request) (<-chan llm.Event, error) {
	c.mu.Lock()
	turn := len(c.requests)
	// Copy messages so later mutation by the caller doesn't affect recordings.
	req.Messages = append([]llm.Message(nil), req.Messages...)
	c.requests = append(c.requests, req)
	var resp Response
	switch {
	case c.responder != nil:
		c.mu.Unlock()
		resp = c.responder(turn, req)
		c.mu.Lock()
	case turn < len(c.script):
		resp = c.script[turn]
	default:
		c.mu.Unlock()
		return nil, fmt.Errorf("fake llm: no scripted response for turn %d", turn)
	}
	calls := make([]llm.ContentBlock, 0, len(resp.ToolCalls))
	for _, tc := range resp.ToolCalls {
		c.callSeq++
		id := tc.ID
		if id == "" {
			id = fmt.Sprintf("call_%d", c.callSeq)
		}
		input, err := json.Marshal(tc.Input)
		if err != nil {
			c.mu.Unlock()
			return nil, fmt.Errorf("fake llm: marshal input: %w", err)
		}
		if tc.Input == nil {
			input = json.RawMessage(`{}`)
		}
		calls = append(calls, llm.ContentBlock{Type: llm.BlockToolUse, ToolUseID: id, ToolName: tc.Name, Input: input})
	}
	c.mu.Unlock()

	out := make(chan llm.Event)
	go func() {
		defer close(out)
		send := func(ev llm.Event) bool {
			if resp.Delay > 0 {
				select {
				case <-time.After(resp.Delay):
				case <-ctx.Done():
					return false
				}
			}
			select {
			case out <- ev:
				return true
			case <-ctx.Done():
				return false
			}
		}
		for _, t := range resp.Text {
			if !send(llm.Event{Type: llm.EventTextDelta, TextDelta: t}) {
				return
			}
		}
		if resp.BlockUntilCancel {
			<-ctx.Done()
			return
		}
		for i := range calls {
			if !send(llm.Event{Type: llm.EventToolUse, ToolUse: &calls[i]}) {
				return
			}
		}
		send(llm.Event{Type: llm.EventStop, StopReason: "STOP", Usage: resp.Usage, Err: resp.Err})
	}()
	return out, nil
}
