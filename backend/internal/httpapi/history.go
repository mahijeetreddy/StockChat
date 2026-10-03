package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/mahijeetreddy/stockchat/backend/internal/market"
	"github.com/mahijeetreddy/stockchat/backend/internal/tools"
)

// handleHistory serves a price_chart payload so the UI's range tabs can
// switch ranges without another chat turn. It reuses the get_history tool, so
// validation and output are identical to what the assistant gets.
func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	tool, ok := s.tools.Get("get_history")
	if !ok {
		writeError(w, http.StatusNotFound, "history is unavailable")
		return
	}
	input, _ := json.Marshal(map[string]string{
		"symbol": chi.URLParam(r, "symbol"),
		"range":  strings.ToUpper(r.URL.Query().Get("range")),
	})
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	res, err := tool.Execute(ctx, input)
	if err != nil {
		var ie *tools.InputError
		switch {
		case errors.As(err, &ie):
			writeError(w, http.StatusBadRequest, ie.Msg)
		case errors.Is(err, market.ErrNotFound):
			writeError(w, http.StatusNotFound, tools.SafeMessage(err))
		case errors.Is(err, market.ErrRateLimited):
			writeError(w, http.StatusTooManyRequests, tools.SafeMessage(err))
		default:
			writeError(w, http.StatusBadGateway, tools.SafeMessage(err))
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, max-age=30")
	_, _ = w.Write(res.UI.Data)
}
