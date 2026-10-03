package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/mahijeetreddy/stockchat/backend/internal/domain"
	"github.com/mahijeetreddy/stockchat/backend/internal/store"
	"github.com/mahijeetreddy/stockchat/backend/internal/tools"
)

type actionResponse struct {
	ActionID string `json:"action_id"`
	Status   string `json:"status"`
	Result   string `json:"result"`
	Error    string `json:"error,omitempty"`
}

// handleConfirmAction executes a pending mutating action. This HTTP call is the
// only way a mutating tool runs; the LLM cannot trigger it.
func (s *Server) handleConfirmAction(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !idRe.MatchString(id) {
		writeError(w, http.StatusBadRequest, "invalid action id")
		return
	}
	a, err := s.store.ClaimPendingAction(r.Context(), id)
	if !s.actionError(w, id, a, err) {
		return
	}

	status, result := domain.ActionDone, ""
	tool, ok := s.tools.Get(a.ToolName)
	if !ok || !tool.Mutating() {
		status, result = store.ActionFailed, "This action is no longer supported."
	} else {
		// Finish even if the client disconnects mid-request.
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 15*time.Second)
		res, err := tool.Execute(ctx, a.Input)
		cancel()
		if err != nil {
			s.log.Warn("confirmed action failed", "action", id, "tool", a.ToolName, "err", err)
			status, result = store.ActionFailed, "Couldn't apply: "+strings.TrimPrefix(tools.SafeMessage(err), "invalid input: ")
		} else {
			result = res.ForModel
		}
	}
	if err := s.store.FinishPendingAction(context.WithoutCancel(r.Context()), id, status, result); err != nil {
		s.log.Error("finish pending action", "action", id, "err", err)
	}
	verb := "confirmed"
	if status != domain.ActionDone {
		verb = "confirmed (but it failed)"
	}
	s.addNote(a.ConversationID, fmt.Sprintf("[system note: the user %s action %s (%s). Result: %s]", verb, id, a.Summary, result))
	writeJSON(w, http.StatusOK, actionResponse{ActionID: id, Status: status, Result: result})
}

func (s *Server) handleCancelAction(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !idRe.MatchString(id) {
		writeError(w, http.StatusBadRequest, "invalid action id")
		return
	}
	a, err := s.store.CancelPendingAction(r.Context(), id)
	if !s.actionError(w, id, a, err) {
		return
	}
	s.addNote(a.ConversationID, fmt.Sprintf("[system note: the user cancelled action %s (%s). Nothing was changed.]", id, a.Summary))
	writeJSON(w, http.StatusOK, actionResponse{ActionID: id, Status: domain.ActionCancelled, Result: "Cancelled"})
}

// actionError writes 404/409/500 for claim/cancel failures; true means proceed.
func (s *Server) actionError(w http.ResponseWriter, id string, a domain.PendingAction, err error) bool {
	switch {
	case err == nil:
		return true
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "action not found")
	case errors.Is(err, store.ErrConflict):
		msg := "This action was already handled."
		if a.Status == domain.ActionExpired {
			msg = "This action expired. Ask again to set it up."
		}
		writeJSON(w, http.StatusConflict, actionResponse{ActionID: id, Status: a.Status, Result: a.Result, Error: msg})
	default:
		s.log.Error("pending action", "action", id, "err", err)
		writeError(w, http.StatusInternalServerError, "couldn't process the action")
	}
	return false
}

func (s *Server) handleWatchlist(w http.ResponseWriter, r *http.Request) {
	syms, err := s.store.WatchlistSymbols(r.Context())
	if err != nil {
		s.log.Error("watchlist", "err", err)
		writeError(w, http.StatusInternalServerError, "couldn't load watchlist")
		return
	}
	quotes, _ := tools.FetchQuotes(r.Context(), s.market, syms, s.now())
	writeJSON(w, http.StatusOK, map[string]any{"symbols": syms, "quotes": quotes})
}

func (s *Server) handleListAlerts(w http.ResponseWriter, r *http.Request) {
	alerts, err := s.store.ListAlerts(r.Context(), false)
	if err != nil {
		s.log.Error("list alerts", "err", err)
		writeError(w, http.StatusInternalServerError, "couldn't load alerts")
		return
	}
	views := make([]tools.AlertView, len(alerts))
	for i, a := range alerts {
		views[i] = tools.ViewAlert(a)
	}
	writeJSON(w, http.StatusOK, map[string]any{"alerts": views})
}

// handleDeleteAlert deletes directly: the user clicked a trash icon, which is
// itself an explicit user action (no LLM involved).
func (s *Server) handleDeleteAlert(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusBadRequest, "invalid alert id")
		return
	}
	if err := s.store.DeleteAlert(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "alert not found")
			return
		}
		s.log.Error("delete alert", "err", err)
		writeError(w, http.StatusInternalServerError, "couldn't delete alert")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
