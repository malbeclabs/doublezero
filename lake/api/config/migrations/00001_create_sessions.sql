-- +goose Up
CREATE TABLE IF NOT EXISTS sessions (
    id UUID PRIMARY KEY,
    type VARCHAR(20) NOT NULL CHECK (type IN ('chat', 'query')),
    name VARCHAR(255),
    content JSONB NOT NULL DEFAULT '[]',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    lock_id VARCHAR(36),
    lock_until TIMESTAMPTZ,
    lock_question TEXT
);

CREATE INDEX IF NOT EXISTS idx_sessions_type_updated
ON sessions (type, updated_at DESC);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ language 'plpgsql';
-- +goose StatementEnd

DROP TRIGGER IF EXISTS update_sessions_updated_at ON sessions;
CREATE TRIGGER update_sessions_updated_at
    BEFORE UPDATE ON sessions
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- +goose Down
DROP TRIGGER IF EXISTS update_sessions_updated_at ON sessions;
DROP FUNCTION IF EXISTS update_updated_at_column();
DROP INDEX IF EXISTS idx_sessions_type_updated;
DROP TABLE IF EXISTS sessions;
