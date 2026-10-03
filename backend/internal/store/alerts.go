package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/mahijeetreddy/stockchat/backend/internal/domain"
)

const alertCols = `id, symbol, kind, threshold, repeat, cooldown_seconds, status, created_at, last_fired_at`

type rowScanner interface{ Scan(dest ...any) error }

func scanAlert(r rowScanner) (domain.Alert, error) {
	var (
		a         domain.Alert
		kind      string
		repeat    int
		cooldown  int64
		created   string
		lastFired sql.NullString
	)
	if err := r.Scan(&a.ID, &a.Symbol, &kind, &a.Threshold, &repeat, &cooldown, &a.Status, &created, &lastFired); err != nil {
		return domain.Alert{}, err
	}
	a.Kind = domain.AlertKind(kind)
	a.Repeat = repeat != 0
	a.Cooldown = time.Duration(cooldown) * time.Second
	var err error
	if a.CreatedAt, err = parseTime(created); err != nil {
		return domain.Alert{}, err
	}
	if a.LastFired, err = parseNullTime(lastFired); err != nil {
		return domain.Alert{}, err
	}
	return a, nil
}

// CreateAlert inserts an active alert and returns it with its ID.
func (s *Store) CreateAlert(ctx context.Context, a domain.Alert) (domain.Alert, error) {
	a.Status = domain.AlertActive
	a.CreatedAt = s.now().UTC()
	repeat := 0
	if a.Repeat {
		repeat = 1
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO alerts (symbol, kind, threshold, repeat, cooldown_seconds, status, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		a.Symbol, string(a.Kind), a.Threshold, repeat, int64(a.Cooldown/time.Second), a.Status, formatTime(a.CreatedAt))
	if err != nil {
		return domain.Alert{}, fmt.Errorf("create alert: %w", err)
	}
	if a.ID, err = res.LastInsertId(); err != nil {
		return domain.Alert{}, fmt.Errorf("alert id: %w", err)
	}
	return a, nil
}

// GetAlert returns one alert (any status).
func (s *Store) GetAlert(ctx context.Context, id int64) (domain.Alert, error) {
	a, err := scanAlert(s.db.QueryRowContext(ctx, `SELECT `+alertCols+` FROM alerts WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Alert{}, ErrNotFound
	}
	if err != nil {
		return domain.Alert{}, fmt.Errorf("get alert: %w", err)
	}
	return a, nil
}

// ListAlerts returns non-deleted alerts (active first, then triggered), newest first.
func (s *Store) ListAlerts(ctx context.Context, activeOnly bool) ([]domain.Alert, error) {
	q := `SELECT ` + alertCols + ` FROM alerts WHERE status != 'deleted'`
	if activeOnly {
		q = `SELECT ` + alertCols + ` FROM alerts WHERE status = 'active'`
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY status = 'active' DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list alerts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []domain.Alert{}
	for rows.Next() {
		a, err := scanAlert(rows)
		if err != nil {
			return nil, fmt.Errorf("scan alert: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// CountActiveAlerts returns the number of active alerts.
func (s *Store) CountActiveAlerts(ctx context.Context) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM alerts WHERE status = 'active'`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count alerts: %w", err)
	}
	return n, nil
}

// DeleteAlert soft-deletes an alert (events keep their reference).
func (s *Store) DeleteAlert(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `UPDATE alerts SET status = 'deleted' WHERE id = ? AND status != 'deleted'`, id)
	if err != nil {
		return fmt.Errorf("delete alert: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// RecordAlertFire stores a firing: inserts an alert_events row, updates
// last_fired_at, and (for one-time alerts) marks the alert triggered. It only
// applies if the alert is still active, so a concurrent delete wins.
func (s *Store) RecordAlertFire(ctx context.Context, a domain.Alert, price float64, at time.Time) (int64, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, false, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	status := domain.AlertActive
	if !a.Repeat {
		status = domain.AlertTriggered
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE alerts SET last_fired_at = ?, status = ? WHERE id = ? AND status = 'active'`,
		formatTime(at), status, a.ID)
	if err != nil {
		return 0, false, fmt.Errorf("update alert: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return 0, false, nil // deleted or already triggered meanwhile
	}
	ev, err := tx.ExecContext(ctx, `INSERT INTO alert_events (alert_id, price, fired_at) VALUES (?, ?, ?)`, a.ID, price, formatTime(at))
	if err != nil {
		return 0, false, fmt.Errorf("insert alert event: %w", err)
	}
	evID, err := ev.LastInsertId()
	if err != nil {
		return 0, false, fmt.Errorf("event id: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, false, fmt.Errorf("commit: %w", err)
	}
	return evID, true, nil
}

// MarkEventDelivered flags an alert event as delivered by the notifier.
func (s *Store) MarkEventDelivered(ctx context.Context, eventID int64) error {
	if _, err := s.db.ExecContext(ctx, `UPDATE alert_events SET delivered = 1 WHERE id = ?`, eventID); err != nil {
		return fmt.Errorf("mark delivered: %w", err)
	}
	return nil
}
