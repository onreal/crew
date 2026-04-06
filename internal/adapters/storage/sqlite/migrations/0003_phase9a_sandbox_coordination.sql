CREATE TABLE IF NOT EXISTS agent_tasks (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL REFERENCES sessions(id),
  conversation_id TEXT NOT NULL,
  requested_by_agent_id TEXT REFERENCES agents(id),
  assigned_agent_id TEXT REFERENCES agents(id),
  assigned_provider TEXT NOT NULL,
  runtime_name TEXT NOT NULL,
  workspace_root TEXT NOT NULL,
  permission_profile TEXT NOT NULL,
  instruction TEXT NOT NULL,
  status TEXT NOT NULL,
  result_summary TEXT NOT NULL DEFAULT '',
  error_message TEXT NOT NULL DEFAULT '',
  artifacts_json TEXT NOT NULL,
  metadata_json TEXT NOT NULL,
  created_at TEXT NOT NULL,
  started_at TEXT,
  completed_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_agent_tasks_session_created
ON agent_tasks(session_id, created_at, id);

CREATE TABLE IF NOT EXISTS agent_handoffs (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL REFERENCES sessions(id),
  conversation_id TEXT NOT NULL,
  source_message_id TEXT REFERENCES messages(id),
  source_task_id TEXT REFERENCES agent_tasks(id),
  task_id TEXT NOT NULL REFERENCES agent_tasks(id),
  from_agent_id TEXT REFERENCES agents(id),
  to_agent_id TEXT REFERENCES agents(id),
  to_provider_class TEXT NOT NULL,
  reason TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_agent_handoffs_session_created
ON agent_handoffs(session_id, created_at, id);
