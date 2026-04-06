CREATE TABLE IF NOT EXISTS sessions (
  id TEXT PRIMARY KEY,
  mode TEXT NOT NULL,
  status TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS agents (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  role TEXT NOT NULL,
  system_prompt TEXT NOT NULL,
  model TEXT NOT NULL,
  tools_json TEXT NOT NULL,
  policies_json TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS workflows (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  entry_step_id TEXT NOT NULL,
  workflow_json TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS messages (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL,
  conversation_id TEXT NOT NULL,
  sender_type TEXT NOT NULL,
  sender_id TEXT NOT NULL,
  recipient_ids_json TEXT NOT NULL,
  channel TEXT NOT NULL,
  kind TEXT NOT NULL,
  body TEXT NOT NULL,
  reply_to TEXT,
  recorded_at TEXT NOT NULL,
  metadata_json TEXT NOT NULL,
  FOREIGN KEY(session_id) REFERENCES sessions(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_messages_session_recorded
ON messages(session_id, recorded_at, id);

CREATE TABLE IF NOT EXISTS outbox_events (
  sequence INTEGER PRIMARY KEY AUTOINCREMENT,
  topic TEXT NOT NULL,
  occurred_at TEXT NOT NULL,
  payload_json TEXT NOT NULL,
  published_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_outbox_events_pending
ON outbox_events(published_at, sequence);

CREATE TABLE IF NOT EXISTS session_stream (
  sequence INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT NOT NULL,
  topic TEXT NOT NULL,
  recorded_at TEXT NOT NULL,
  payload_json TEXT NOT NULL,
  FOREIGN KEY(session_id) REFERENCES sessions(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_session_stream_session_sequence
ON session_stream(session_id, sequence);
