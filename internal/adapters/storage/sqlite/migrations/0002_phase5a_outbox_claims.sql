ALTER TABLE outbox_events ADD COLUMN claim_token TEXT;
ALTER TABLE outbox_events ADD COLUMN claim_deadline TEXT;

ALTER TABLE session_stream ADD COLUMN outbox_sequence INTEGER;

CREATE INDEX IF NOT EXISTS idx_outbox_events_claimable
ON outbox_events(published_at, claim_deadline, sequence);

CREATE UNIQUE INDEX IF NOT EXISTS idx_session_stream_outbox_sequence_unique
ON session_stream(outbox_sequence)
WHERE outbox_sequence IS NOT NULL;
