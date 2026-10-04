CREATE TABLE documents (
 id uuid PRIMARY KEY, owner_id text NOT NULL, filename text NOT NULL, content_type text NOT NULL,
 object_key text NOT NULL UNIQUE, size_bytes bigint NOT NULL, checksum text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX documents_owner ON documents(owner_id,created_at DESC);
CREATE TABLE document_shares (
 id uuid PRIMARY KEY, document_id uuid NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
 delivery_id uuid REFERENCES deliveries(id) ON DELETE CASCADE, token_hash bytea NOT NULL UNIQUE,
 expires_at timestamptz NOT NULL, revoked_at timestamptz, allow_download boolean NOT NULL DEFAULT true,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE viewer_sessions (
 id uuid PRIMARY KEY, share_id uuid NOT NULL REFERENCES document_shares(id) ON DELETE CASCADE,
 secret_hash bytea NOT NULL, expires_at timestamptz NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
 active_ms bigint NOT NULL DEFAULT 0, loaded boolean NOT NULL DEFAULT false
);
CREATE TABLE viewer_events (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY, session_id uuid NOT NULL REFERENCES viewer_sessions(id) ON DELETE CASCADE,
 batch_id uuid NOT NULL, kind text NOT NULL, active_ms integer NOT NULL DEFAULT 0, pages integer[] NOT NULL DEFAULT '{}',
 occurred_at timestamptz NOT NULL DEFAULT now(), UNIQUE(session_id,batch_id)
);
CREATE TABLE document_uploads (object_key text PRIMARY KEY, expires_at timestamptz NOT NULL DEFAULT now()+interval '1 hour');
