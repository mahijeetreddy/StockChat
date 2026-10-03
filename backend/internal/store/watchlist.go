package store

import (
	"context"
	"fmt"
)

// WatchlistSymbols returns watchlist symbols in the order they were added.
func (s *Store) WatchlistSymbols(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT symbol FROM watchlist ORDER BY rowid`)
	if err != nil {
		return nil, fmt.Errorf("list watchlist: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []string{}
	for rows.Next() {
		var sym string
		if err := rows.Scan(&sym); err != nil {
			return nil, fmt.Errorf("scan watchlist: %w", err)
		}
		out = append(out, sym)
	}
	return out, rows.Err()
}

// AddToWatchlist adds a symbol; added is false if it was already present.
func (s *Store) AddToWatchlist(ctx context.Context, symbol string) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO watchlist (symbol, added_at) VALUES (?, ?) ON CONFLICT(symbol) DO NOTHING`,
		symbol, formatTime(s.now()))
	if err != nil {
		return false, fmt.Errorf("add to watchlist: %w", err)
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// RemoveFromWatchlist removes a symbol; removed is false if it wasn't present.
func (s *Store) RemoveFromWatchlist(ctx context.Context, symbol string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM watchlist WHERE symbol = ?`, symbol)
	if err != nil {
		return false, fmt.Errorf("remove from watchlist: %w", err)
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}
