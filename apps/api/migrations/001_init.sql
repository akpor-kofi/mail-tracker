CREATE TABLE IF NOT EXISTS mailboxes (
  id uuid PRIMARY KEY, owner_id text NOT NULL, google_sub text NOT NULL,
  email text NOT NULL, encrypted_refresh_token bytea NOT NULL,
  connected_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(owner_id, google_sub)
);
CREATE TABLE IF NOT EXISTS oauth_states (
  state_hash bytea PRIMARY KEY, owner_id text NOT NULL, verifier text NOT NULL,
  expires_at timestamptz NOT NULL
);
CREATE TABLE IF NOT EXISTS pairing_codes (
  code_hash bytea PRIMARY KEY, mailbox_id uuid NOT NULL REFERENCES mailboxes(id) ON DELETE CASCADE,
  expires_at timestamptz NOT NULL, used_at timestamptz
);
CREATE TABLE IF NOT EXISTS addon_pairs (
  google_sub text PRIMARY KEY, mailbox_id uuid NOT NULL REFERENCES mailboxes(id) ON DELETE CASCADE,
  paired_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS local_drafts (
  id uuid PRIMARY KEY, owner_id text NOT NULL, mailbox_id uuid NOT NULL REFERENCES mailboxes(id) ON DELETE CASCADE,
  content jsonb NOT NULL, updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS attachments (
  id uuid PRIMARY KEY, owner_id text NOT NULL, path text NOT NULL,
  filename text NOT NULL, content_type text NOT NULL, size_bytes bigint NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS conversations (
  id uuid PRIMARY KEY, owner_id text NOT NULL, mailbox_id uuid NOT NULL REFERENCES mailboxes(id) ON DELETE CASCADE,
  subject text NOT NULL, mode text NOT NULL, status text NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(), idempotency_key text,
  UNIQUE(owner_id, idempotency_key)
);
CREATE TABLE IF NOT EXISTS deliveries (
  id uuid PRIMARY KEY, conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  recipients text[] NOT NULL, pixel_token_hash bytea NOT NULL UNIQUE,
  status text NOT NULL, gmail_message_id text, gmail_thread_id text, rfc_message_id text,
  error text, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS open_events (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  delivery_id uuid NOT NULL REFERENCES deliveries(id) ON DELETE CASCADE,
  occurred_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS open_events_delivery_idx ON open_events(delivery_id, occurred_at);
CREATE INDEX IF NOT EXISTS conversations_owner_idx ON conversations(owner_id, updated_at DESC);
