package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mahijeetreddy/stockchat/backend/internal/agent"
	"github.com/mahijeetreddy/stockchat/backend/internal/llm"
)

// DefaultTitle is the title of a conversation before its first message.
const DefaultTitle = "New chat"

// Conversation is a chat thread.
type Conversation struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// StoredMessage is a persisted message with its UI entries.
type StoredMessage struct {
	ID        int64              `json:"id"`
	Role      string             `json:"role"`
	Blocks    []llm.ContentBlock `json:"blocks"`
	UI        []agent.UIEntry    `json:"ui,omitempty"`
	CreatedAt time.Time          `json:"created_at"`
}

var _ agent.History = (*Store)(nil)

// CreateConversation inserts a new conversation.
func (s *Store) CreateConversation(ctx context.Context, id string) (Conversation, error) {
	now := s.now().UTC()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO conversations (id, title, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		id, DefaultTitle, formatTime(now), formatTime(now))
	if err != nil {
		return Conversation{}, fmt.Errorf("create conversation: %w", err)
	}
	return Conversation{ID: id, Title: DefaultTitle, CreatedAt: now, UpdatedAt: now}, nil
}

// GetConversation returns one conversation.
func (s *Store) GetConversation(ctx context.Context, id string) (Conversation, error) {
	var c Conversation
	var created, updated string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, title, created_at, updated_at FROM conversations WHERE id = ?`, id).
		Scan(&c.ID, &c.Title, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Conversation{}, ErrNotFound
	}
	if err != nil {
		return Conversation{}, fmt.Errorf("get conversation: %w", err)
	}
	if c.CreatedAt, err = parseTime(created); err != nil {
		return Conversation{}, err
	}
	if c.UpdatedAt, err = parseTime(updated); err != nil {
		return Conversation{}, err
	}
	return c, nil
}

// ListConversations returns conversations, most recently updated first.
func (s *Store) ListConversations(ctx context.Context, limit int) ([]Conversation, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, title, created_at, updated_at FROM conversations ORDER BY updated_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list conversations: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []Conversation{}
	for rows.Next() {
		var c Conversation
		var created, updated string
		if err := rows.Scan(&c.ID, &c.Title, &created, &updated); err != nil {
			return nil, fmt.Errorf("scan conversation: %w", err)
		}
		if c.CreatedAt, err = parseTime(created); err != nil {
			return nil, err
		}
		if c.UpdatedAt, err = parseTime(updated); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// DeleteConversation deletes a conversation and (via cascade) its messages.
func (s *Store) DeleteConversation(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM conversations WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete conversation: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// AppendMessage implements agent.History. The first plain user message also
// becomes the conversation title.
func (s *Store) AppendMessage(ctx context.Context, convoID string, m llm.Message, ui []agent.UIEntry) (int64, error) {
	blocks, err := json.Marshal(m.Blocks)
	if err != nil {
		return 0, fmt.Errorf("marshal blocks: %w", err)
	}
	var uiJSON sql.NullString
	if len(ui) > 0 {
		b, err := json.Marshal(ui)
		if err != nil {
			return 0, fmt.Errorf("marshal ui: %w", err)
		}
		uiJSON = sql.NullString{String: string(b), Valid: true}
	}
	now := formatTime(s.now())

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx,
		`INSERT INTO messages (conversation_id, role, blocks_json, ui_json, created_at) VALUES (?, ?, ?, ?, ?)`,
		convoID, m.Role, string(blocks), uiJSON, now)
	if err != nil {
		return 0, fmt.Errorf("insert message: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("message id: %w", err)
	}
	if m.Role == llm.RoleUser && !m.HasToolResults() && !strings.HasPrefix(m.Text(), "[system note") {
		if _, err := tx.ExecContext(ctx,
			`UPDATE conversations SET title = ? WHERE id = ? AND title = ?`, MakeTitle(m.Text()), convoID, DefaultTitle); err != nil {
			return 0, fmt.Errorf("set title: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE conversations SET updated_at = ? WHERE id = ?`, now, convoID); err != nil {
		return 0, fmt.Errorf("touch conversation: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return id, nil
}

// MakeTitle derives a short title from the first user message.
func MakeTitle(text string) string {
	t := strings.Join(strings.Fields(text), " ")
	if utf8.RuneCountInString(t) > 60 {
		r := []rune(t)
		t = strings.TrimSpace(string(r[:57])) + "…"
	}
	if t == "" {
		return DefaultTitle
	}
	return t
}

// LoadMessages implements agent.History.
func (s *Store) LoadMessages(ctx context.Context, convoID string) ([]llm.Message, error) {
	stored, err := s.Messages(ctx, convoID)
	if err != nil {
		return nil, err
	}
	out := make([]llm.Message, len(stored))
	for i, m := range stored {
		out[i] = llm.Message{Role: m.Role, Blocks: m.Blocks}
	}
	return out, nil
}

// Messages returns all messages of a conversation with their UI entries.
func (s *Store) Messages(ctx context.Context, convoID string) ([]StoredMessage, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, role, blocks_json, ui_json, created_at FROM messages WHERE conversation_id = ? ORDER BY id`, convoID)
	if err != nil {
		return nil, fmt.Errorf("query messages: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []StoredMessage{}
	for rows.Next() {
		var (
			m       StoredMessage
			blocks  string
			ui      sql.NullString
			created string
		)
		if err := rows.Scan(&m.ID, &m.Role, &blocks, &ui, &created); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		if err := json.Unmarshal([]byte(blocks), &m.Blocks); err != nil {
			return nil, fmt.Errorf("decode blocks of message %d: %w", m.ID, err)
		}
		if ui.Valid && ui.String != "" {
			if err := json.Unmarshal([]byte(ui.String), &m.UI); err != nil {
				return nil, fmt.Errorf("decode ui of message %d: %w", m.ID, err)
			}
		}
		if m.CreatedAt, err = parseTime(created); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
