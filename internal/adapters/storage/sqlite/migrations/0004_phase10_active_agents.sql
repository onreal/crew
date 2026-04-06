ALTER TABLE agents ADD COLUMN active INTEGER NOT NULL DEFAULT 1;

UPDATE agents
SET active = 1
WHERE active IS NULL;

CREATE INDEX IF NOT EXISTS idx_agents_active_id
ON agents(active, id);
