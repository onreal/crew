CREATE TABLE IF NOT EXISTS message_embeddings (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  message_id TEXT NOT NULL UNIQUE,
  session_id TEXT NOT NULL,
  source_text TEXT NOT NULL,
  source_sha256 TEXT NOT NULL,
  embedding_blob BLOB NOT NULL,
  embedding_dimensions INTEGER NOT NULL,
  metadata_json TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY(message_id) REFERENCES messages(id) ON DELETE CASCADE,
  FOREIGN KEY(session_id) REFERENCES sessions(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_message_embeddings_session_id
ON message_embeddings(session_id, updated_at, id);

CREATE TABLE IF NOT EXISTS vector_index_state (
  index_name TEXT PRIMARY KEY,
  provider TEXT NOT NULL,
  status TEXT NOT NULL,
  last_rebuilt_at TEXT,
  last_error TEXT,
  updated_at TEXT NOT NULL
);
