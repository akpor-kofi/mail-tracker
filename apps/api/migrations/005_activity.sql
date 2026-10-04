ALTER TABLE deliveries ADD COLUMN confirmed_sent_at timestamptz;
ALTER TABLE deliveries ADD COLUMN instrumentation_version integer NOT NULL DEFAULT 1;
ALTER TABLE deliveries ADD COLUMN activity_count integer NOT NULL DEFAULT 0;
ALTER TABLE deliveries ADD COLUMN activity_capped boolean NOT NULL DEFAULT false;
CREATE TABLE tracked_links (
 id uuid PRIMARY KEY, delivery_id uuid NOT NULL REFERENCES deliveries(id) ON DELETE CASCADE,
 token_hash bytea NOT NULL UNIQUE, destination text NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
 logging_enabled boolean NOT NULL DEFAULT true
);
CREATE TABLE activity_events (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 delivery_id uuid NOT NULL REFERENCES deliveries(id) ON DELETE CASCADE,
 kind text NOT NULL, source text NOT NULL DEFAULT 'unknown', occurred_at timestamptz NOT NULL DEFAULT now(),
 link_id uuid REFERENCES tracked_links(id) ON DELETE SET NULL,
 session_id uuid, metadata jsonb NOT NULL DEFAULT '{}', dedupe_key text,
 UNIQUE(delivery_id,dedupe_key)
);
CREATE INDEX activity_delivery_time ON activity_events(delivery_id,occurred_at DESC,id DESC);
INSERT INTO activity_events(delivery_id,kind,source,occurred_at)
 SELECT delivery_id,'image_request','legacy',occurred_at FROM open_events;
CREATE TABLE activity_outbox (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY, owner_id text NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);

UPDATE deliveries SET activity_capped=true WHERE recorded_open_count>=1000;
