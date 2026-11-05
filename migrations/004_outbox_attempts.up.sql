
ALTER TABLE outbox_events
ADD COLUMN publish_attempts INTEGER NOT NULL DEFAULT 0
CHECK (publish_attempts >= 0);
