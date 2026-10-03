package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/oklog/ulid/v2"

	"github.com/mahijeetreddy/stockchat/backend/internal/agent"
	"github.com/mahijeetreddy/stockchat/backend/internal/llm"
	"github.com/mahijeetreddy/stockchat/backend/internal/store"
)

type chatRequest struct {
	ConversationID string `json:"conversation_id"`
	Message        string `json:"message"`
	TimeZone       string `json:"timezone"`
}

// handleChat runs one agent turn and streams events as SSE.
func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	var req chatRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	msg := strings.TrimSpace(req.Message)
	if msg == "" {
		writeError(w, http.StatusBadRequest, "message is required")
		return
	}
	if utf8.RuneCountInString(msg) > s.opts.MaxMessageRunes {
		writeError(w, http.StatusBadRequest, "message is too long")
		return
	}
	loc := s.opts.DefaultLocation
	if req.TimeZone != "" && len(req.TimeZone) <= 64 {
		if l, err := time.LoadLocation(req.TimeZone); err == nil {
			loc = l
		}
	}

	ctx := r.Context()
	convoID := req.ConversationID
	if convoID == "" {
		convoID = "c_" + ulid.Make().String()
		if _, err := s.store.CreateConversation(ctx, convoID); err != nil {
			s.log.Error("create conversation", "err", err)
			writeError(w, http.StatusInternalServerError, "couldn't create conversation")
			return
		}
	} else {
		if !idRe.MatchString(convoID) {
			writeError(w, http.StatusBadRequest, "invalid conversation_id")
			return
		}
		if _, err := s.store.GetConversation(ctx, convoID); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				writeError(w, http.StatusNotFound, "conversation not found")
				return
			}
			s.log.Error("get conversation", "err", err)
			writeError(w, http.StatusInternalServerError, "couldn't load conversation")
			return
		}
	}
	if !s.tryLock(convoID) {
		writeError(w, http.StatusConflict, "a reply is already in progress for this conversation")
		return
	}
	defer s.unlock(convoID)

	sse := newSSEWriter(w)
	stop := make(chan struct{})
	defer close(stop)
	go sse.heartbeat(s.opts.HeartbeatInterval, stop)

	_ = sse.event(agent.EventConversation, agent.ConversationData{ConversationID: convoID})

	err := s.agent.Run(ctx, agent.RunInput{ConversationID: convoID, UserText: msg, Location: loc}, func(ev agent.Event) {
		// Write errors mean the client went away; ctx cancellation stops the run.
		_ = sse.event(ev.Type, ev.Data)
	})
	switch {
	case err == nil:
	case ctx.Err() != nil:
		s.log.Info("chat turn cancelled (client disconnected)", "convo", convoID, "req_id", middleware.GetReqID(ctx))
	default:
		s.log.Warn("chat turn ended with error", "convo", convoID, "err", err, "req_id", middleware.GetReqID(ctx))
	}
}

func (s *Server) handleListConversations(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListConversations(r.Context(), 100)
	if err != nil {
		s.log.Error("list conversations", "err", err)
		writeError(w, http.StatusInternalServerError, "couldn't list conversations")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"conversations": list})
}

func (s *Server) handleGetConversation(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !idRe.MatchString(id) {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	c, err := s.store.GetConversation(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "conversation not found")
		return
	}
	if err != nil {
		s.log.Error("get conversation", "err", err)
		writeError(w, http.StatusInternalServerError, "couldn't load conversation")
		return
	}
	msgs, err := s.store.Messages(r.Context(), id)
	if err != nil {
		s.log.Error("load messages", "err", err)
		writeError(w, http.StatusInternalServerError, "couldn't load messages")
		return
	}
	s.refreshConfirmations(r, msgs)
	writeJSON(w, http.StatusOK, map[string]any{"conversation": c, "messages": publicMessages(msgs)})
}

func (s *Server) handleDeleteConversation(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !idRe.MatchString(id) {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	err := s.store.DeleteConversation(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "conversation not found")
		return
	}
	if err != nil {
		s.log.Error("delete conversation", "err", err)
		writeError(w, http.StatusInternalServerError, "couldn't delete conversation")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// publicBlock is the client-facing view of a content block. Provider metadata
// (thought signatures) stays server-side.
type publicBlock struct {
	Type      string `json:"type"`
	Text      string `json:"text,omitempty"`
	ToolUseID string `json:"tool_use_id,omitempty"`
	ToolName  string `json:"tool_name,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
}

type publicMessage struct {
	ID        int64           `json:"id"`
	Role      string          `json:"role"`
	Blocks    []publicBlock   `json:"blocks"`
	UI        []agent.UIEntry `json:"ui,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

func publicMessages(msgs []store.StoredMessage) []publicMessage {
	out := make([]publicMessage, 0, len(msgs))
	for _, m := range msgs {
		pm := publicMessage{ID: m.ID, Role: m.Role, UI: m.UI, CreatedAt: m.CreatedAt, Blocks: []publicBlock{}}
		for _, b := range m.Blocks {
			pb := publicBlock{Type: b.Type, ToolUseID: b.ToolUseID, ToolName: b.ToolName, IsError: b.IsError}
			if b.Type == llm.BlockText {
				pb.Text = b.Text
			}
			pm.Blocks = append(pm.Blocks, pb)
		}
		out = append(out, pm)
	}
	return out
}

// refreshConfirmations updates persisted confirm cards with the current status
// of their pending action (set in M6; no-op until actions exist).
func (s *Server) refreshConfirmations(r *http.Request, msgs []store.StoredMessage) {
	for i := range msgs {
		for j := range msgs[i].UI {
			c := msgs[i].UI[j].Confirm
			if c == nil {
				continue
			}
			a, err := s.store.GetPendingAction(r.Context(), c.ActionID)
			if err != nil {
				continue
			}
			status := a.Status
			if status == "pending" && s.now().After(a.ExpiresAt) {
				status = "expired"
			}
			c.Status, c.Result = status, a.Result
		}
	}
}
