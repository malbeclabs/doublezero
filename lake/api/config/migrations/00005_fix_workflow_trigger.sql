-- +goose Up
-- Create a separate trigger function for workflow_runs that doesn't reference session-specific columns

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION update_workflow_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ language 'plpgsql';
-- +goose StatementEnd

-- Update workflow_runs to use its own trigger function
DROP TRIGGER IF EXISTS update_workflow_runs_updated_at ON workflow_runs;
CREATE TRIGGER update_workflow_runs_updated_at
    BEFORE UPDATE ON workflow_runs
    FOR EACH ROW
    EXECUTE FUNCTION update_workflow_updated_at_column();

-- +goose Down
-- Revert to using the shared trigger function
DROP TRIGGER IF EXISTS update_workflow_runs_updated_at ON workflow_runs;
CREATE TRIGGER update_workflow_runs_updated_at
    BEFORE UPDATE ON workflow_runs
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

DROP FUNCTION IF EXISTS update_workflow_updated_at_column();
