ALTER TABLE deliveries ADD COLUMN reply_all_recipients text[] NOT NULL DEFAULT '{}';
