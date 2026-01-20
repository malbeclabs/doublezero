-- +goose Up
-- Remove all updated_at triggers - timestamps are now set explicitly in application code
-- This avoids the bug where the sessions trigger referenced columns that don't exist in workflow_runs

DROP TRIGGER IF EXISTS update_workflow_runs_updated_at ON workflow_runs;
DROP TRIGGER IF EXISTS update_sessions_updated_at ON sessions;
DROP FUNCTION IF EXISTS update_updated_at_column();

-- +goose Down
-- Recreate the original simple trigger (not the "smart" one that caused issues)
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ language 'plpgsql';
-- +goose StatementEnd

CREATE TRIGGER update_sessions_updated_at
    BEFORE UPDATE ON sessions
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_workflow_runs_updated_at
    BEFORE UPDATE ON workflow_runs
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
