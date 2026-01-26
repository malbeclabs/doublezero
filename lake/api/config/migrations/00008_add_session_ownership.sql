-- +goose Up

-- Add account ownership to sessions
ALTER TABLE sessions ADD COLUMN account_id UUID REFERENCES accounts(id) ON DELETE CASCADE;
ALTER TABLE sessions ADD COLUMN anonymous_id VARCHAR(64);

-- Index for querying sessions by owner
CREATE INDEX idx_sessions_account_id ON sessions(account_id) WHERE account_id IS NOT NULL;
CREATE INDEX idx_sessions_anonymous_id ON sessions(anonymous_id) WHERE anonymous_id IS NOT NULL;

-- Composite index for efficient filtering by type + owner
CREATE INDEX idx_sessions_type_account ON sessions(type, account_id, updated_at DESC) WHERE account_id IS NOT NULL;
CREATE INDEX idx_sessions_type_anonymous ON sessions(type, anonymous_id, updated_at DESC) WHERE anonymous_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_sessions_type_anonymous;
DROP INDEX IF EXISTS idx_sessions_type_account;
DROP INDEX IF EXISTS idx_sessions_anonymous_id;
DROP INDEX IF EXISTS idx_sessions_account_id;
ALTER TABLE sessions DROP COLUMN IF EXISTS anonymous_id;
ALTER TABLE sessions DROP COLUMN IF EXISTS account_id;
