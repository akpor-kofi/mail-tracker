ALTER TABLE mailboxes ADD COLUMN granted_scopes text[] NOT NULL DEFAULT '{}';
ALTER TABLE mailboxes ADD COLUMN sync_enabled boolean NOT NULL DEFAULT false;
CREATE TABLE mailbox_sync (
 mailbox_id uuid PRIMARY KEY REFERENCES mailboxes(id) ON DELETE CASCADE,
 history_id text NOT NULL DEFAULT '', last_success_at timestamptz,
 status text NOT NULL DEFAULT 'not_started', error text NOT NULL DEFAULT ''
);
CREATE TABLE synced_messages (
 id uuid PRIMARY KEY, mailbox_id uuid NOT NULL REFERENCES mailboxes(id) ON DELETE CASCADE,
 gmail_id text NOT NULL, thread_id text NOT NULL, subject text NOT NULL, sender text NOT NULL,
 snippet text NOT NULL, direction text NOT NULL, occurred_at timestamptz NOT NULL,
 UNIQUE(mailbox_id,gmail_id)
);
CREATE TABLE goals (
 id uuid PRIMARY KEY, owner_id text NOT NULL, name text NOT NULL,
 window_days integer NOT NULL DEFAULT 30 CHECK(window_days BETWEEN 1 AND 365), created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE conversions (
 id uuid PRIMARY KEY, goal_id uuid NOT NULL REFERENCES goals(id), delivery_id uuid REFERENCES deliveries(id) ON DELETE CASCADE,
 owner_id text NOT NULL, source text NOT NULL, external_id text, occurred_at timestamptz NOT NULL,
 recorded_at timestamptz NOT NULL DEFAULT now(), reversed_at timestamptz,
 UNIQUE(owner_id,source,external_id)
);
CREATE TABLE reminders (
 id uuid PRIMARY KEY, owner_id text NOT NULL, conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
 due_at timestamptz NOT NULL, done_at timestamptz, created_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE tracked_links ADD COLUMN attribution_hash bytea UNIQUE;
ALTER TABLE conversions ADD COLUMN value numeric(18,2);
ALTER TABLE conversions ADD COLUMN currency text;
ALTER TABLE conversions ADD COLUMN request_hash bytea;
ALTER TABLE activity_events ADD COLUMN received_at timestamptz NOT NULL DEFAULT now();
