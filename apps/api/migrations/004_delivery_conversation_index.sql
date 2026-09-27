-- Existing installations have already applied 003, so they need a new migration.
CREATE INDEX IF NOT EXISTS deliveries_conversation_idx ON deliveries(conversation_id);
