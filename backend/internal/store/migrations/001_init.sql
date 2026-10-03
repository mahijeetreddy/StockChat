-- +goose Up
CREATE TABLE conversations (
  id          TEXT PRIMARY KEY,
  title       TEXT NOT NULL DEFAULT 'New chat',
  created_at  TEXT NOT NULL,
  updated_at  TEXT NOT NULL
);

CREATE TABLE messages (
  id               INTEGER PRIMARY KEY AUTOINCREMENT,
  conversation_id  TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  role             TEXT NOT NULL,         -- user | assistant
  blocks_json      TEXT NOT NULL,         -- []llm.ContentBlock as sent to/from the LLM
  ui_json          TEXT,                  -- []agent.UIEntry rendered for this message (nullable)
  created_at       TEXT NOT NULL
);
CREATE INDEX idx_messages_convo ON messages(conversation_id, id);

CREATE TABLE watchlist (
  symbol     TEXT PRIMARY KEY,
  added_at   TEXT NOT NULL
);

CREATE TABLE alerts (
  id               INTEGER PRIMARY KEY AUTOINCREMENT,
  symbol           TEXT NOT NULL,
  kind             TEXT NOT NULL,
  threshold        REAL NOT NULL,
  repeat           INTEGER NOT NULL DEFAULT 0,
  cooldown_seconds INTEGER NOT NULL DEFAULT 3600,
  status           TEXT NOT NULL DEFAULT 'active',  -- active|triggered|deleted
  created_at       TEXT NOT NULL,
  last_fired_at    TEXT
);
CREATE INDEX idx_alerts_active ON alerts(status, symbol);

CREATE TABLE alert_events (
  id        INTEGER PRIMARY KEY AUTOINCREMENT,
  alert_id  INTEGER NOT NULL REFERENCES alerts(id),
  price     REAL NOT NULL,
  fired_at  TEXT NOT NULL,
  delivered INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE pending_actions (
  id               TEXT PRIMARY KEY,
  conversation_id  TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  tool_name        TEXT NOT NULL,
  call_id          TEXT NOT NULL DEFAULT '',
  input_json       TEXT NOT NULL,
  summary          TEXT NOT NULL,
  status           TEXT NOT NULL DEFAULT 'pending', -- pending|executing|done|failed|cancelled|expired
  result           TEXT NOT NULL DEFAULT '',
  created_at       TEXT NOT NULL,
  expires_at       TEXT NOT NULL
);
CREATE INDEX idx_pending_actions_convo ON pending_actions(conversation_id);

-- +goose Down
DROP TABLE pending_actions;
DROP TABLE alert_events;
DROP TABLE alerts;
DROP TABLE watchlist;
DROP TABLE messages;
DROP TABLE conversations;
