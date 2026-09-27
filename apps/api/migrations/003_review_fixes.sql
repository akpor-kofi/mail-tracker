ALTER TABLE conversations ADD COLUMN request_hash bytea;

CREATE TABLE pending_attachment_deletions (
  path text PRIMARY KEY,
  queued_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX pending_attachment_deletions_queue_idx ON pending_attachment_deletions(queued_at, path);

CREATE INDEX attachments_created_idx ON attachments(created_at);

ALTER TABLE deliveries
  ADD COLUMN recorded_open_count integer NOT NULL DEFAULT 0,
  ADD COLUMN last_open_recorded_at timestamptz;

CREATE INDEX IF NOT EXISTS deliveries_conversation_idx ON deliveries(conversation_id);

WITH ranked AS (
  SELECT id, row_number() OVER (PARTITION BY delivery_id ORDER BY occurred_at DESC, id DESC) AS position
  FROM open_events
)
DELETE FROM open_events e USING ranked r WHERE e.id = r.id AND r.position > 1000;

UPDATE deliveries d SET
  recorded_open_count = (SELECT count(*) FROM open_events e WHERE e.delivery_id = d.id),
  last_open_recorded_at = (SELECT max(occurred_at) FROM open_events e WHERE e.delivery_id = d.id);
