package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/mahijeetreddy/stockchat/backend/internal/agent"
	"github.com/mahijeetreddy/stockchat/backend/internal/domain"
)

// Extra pending-action statuses used while/after executing.
const (
	ActionExecuting = "executing"
	ActionFailed    = "failed"
)

var _ agent.ActionStore = (*Store)(nil)

// CreatePendingAction implements agent.ActionStore.
func (s *Store) CreatePendingAction(ctx context.Context, a domain.PendingAction) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO pending_actions (id, conversation_id, tool_name, call_id, input_json, summary, status, created_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.ConversationID, a.ToolName, a.CallID, string(a.Input), a.Summary, domain.ActionPending,
		formatTime(a.CreatedAt), formatTime(a.ExpiresAt))
	if err != nil {
		return fmt.Errorf("create pending action: %w", err)
	}
	return nil
}

// GetPendingAction returns one action.
func (s *Store) GetPendingAction(ctx context.Context, id string) (domain.PendingAction, error) {
	var (
		a                domain.PendingAction
		input            string
		created, expires string
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, conversation_id, tool_name, call_id, input_json, summary, status, result, created_at, expires_at
		 FROM pending_actions WHERE id = ?`, id).
		Scan(&a.ID, &a.ConversationID, &a.ToolName, &a.CallID, &input, &a.Summary, &a.Status, &a.Result, &created, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.PendingAction{}, ErrNotFound
	}
	if err != nil {
		return domain.PendingAction{}, fmt.Errorf("get pending action: %w", err)
	}
	a.Input = []byte(input)
	if a.CreatedAt, err = parseTime(created); err != nil {
		return domain.PendingAction{}, err
	}
	if a.ExpiresAt, err = parseTime(expires); err != nil {
		return domain.PendingAction{}, err
	}
	return a, nil
}

// ClaimPendingAction atomically moves a pending, unexpired action to
// "executing" so it can run exactly once. It returns ErrConflict if the action
// was already handled or has expired (expired actions are marked as such).
func (s *Store) ClaimPendingAction(ctx context.Context, id string) (domain.PendingAction, error) {
	now := s.now()
	res, err := s.db.ExecContext(ctx,
		`UPDATE pending_actions SET status = ? WHERE id = ? AND status = ? AND expires_at > ?`,
		ActionExecuting, id, domain.ActionPending, formatTime(now))
	if err != nil {
		return domain.PendingAction{}, fmt.Errorf("claim pending action: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 1 {
		return s.GetPendingAction(ctx, id)
	}
	a, err := s.GetPendingAction(ctx, id)
	if err != nil {
		return domain.PendingAction{}, err
	}
	if a.Status == domain.ActionPending && !now.Before(a.ExpiresAt) {
		_ = s.FinishPendingAction(ctx, id, domain.ActionExpired, "")
		a.Status = domain.ActionExpired
	}
	return a, fmt.Errorf("action is %s: %w", a.Status, ErrConflict)
}

// FinishPendingAction records the final status and a short result.
func (s *Store) FinishPendingAction(ctx context.Context, id, status, result string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE pending_actions SET status = ?, result = ? WHERE id = ?`, status, result, id)
	if err != nil {
		return fmt.Errorf("finish pending action: %w", err)
	}
	return nil
}

// CancelPendingAction cancels a pending action. ErrConflict if not pending.
func (s *Store) CancelPendingAction(ctx context.Context, id string) (domain.PendingAction, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE pending_actions SET status = ?, result = 'Cancelled' WHERE id = ? AND status = ? AND expires_at > ?`,
		domain.ActionCancelled, id, domain.ActionPending, formatTime(s.now()))
	if err != nil {
		return domain.PendingAction{}, fmt.Errorf("cancel pending action: %w", err)
	}
	a, gerr := s.GetPendingAction(ctx, id)
	if gerr != nil {
		return domain.PendingAction{}, gerr
	}
	if n, _ := res.RowsAffected(); n == 1 {
		return a, nil
	}
	if a.Status == domain.ActionPending {
		_ = s.FinishPendingAction(ctx, id, domain.ActionExpired, "")
		a.Status = domain.ActionExpired
	}
	return a, fmt.Errorf("action is %s: %w", a.Status, ErrConflict)
}

// ExpirePendingActions marks overdue pending actions expired.
func (s *Store) ExpirePendingActions(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE pending_actions SET status = ? WHERE status = ? AND expires_at <= ?`,
		domain.ActionExpired, domain.ActionPending, formatTime(s.now()))
	if err != nil {
		return 0, fmt.Errorf("expire pending actions: %w", err)
	}
	return res.RowsAffected()
}

// Now returns the store's current time (consistent with expiry checks).
func (s *Store) Now() time.Time { return s.now() }
