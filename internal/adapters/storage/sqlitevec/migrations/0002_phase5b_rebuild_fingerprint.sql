ALTER TABLE message_embeddings
ADD COLUMN rebuild_fingerprint TEXT NOT NULL DEFAULT '';
