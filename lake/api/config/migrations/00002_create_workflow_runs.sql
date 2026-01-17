-- +goose Up
CREATE TABLE IF NOT EXISTS workflow_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    status VARCHAR(20) NOT NULL DEFAULT 'running'
        CHECK (status IN ('running', 'completed', 'failed', 'cancelled')),
    user_question TEXT NOT NULL,

    -- Checkpoint state (updated after each iteration)
    iteration INT NOT NULL DEFAULT 0,
    messages JSONB NOT NULL DEFAULT '[]',
    thinking_steps JSONB NOT NULL DEFAULT '[]',
    executed_queries JSONB NOT NULL DEFAULT '[]',
    final_answer TEXT,

    -- Metrics
    llm_calls INT NOT NULL DEFAULT 0,
    input_tokens INT NOT NULL DEFAULT 0,
    output_tokens INT NOT NULL DEFAULT 0,

    -- Timestamps
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,

    -- Error tracking
    error TEXT
);

CREATE INDEX IF NOT EXISTS idx_workflow_runs_session ON workflow_runs(session_id);
CREATE INDEX IF NOT EXISTS idx_workflow_runs_status ON workflow_runs(status) WHERE status = 'running';

-- Reuse the update_updated_at_column trigger function from sessions
DROP TRIGGER IF EXISTS update_workflow_runs_updated_at ON workflow_runs;
CREATE TRIGGER update_workflow_runs_updated_at
    BEFORE UPDATE ON workflow_runs
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- +goose Down
DROP TRIGGER IF EXISTS update_workflow_runs_updated_at ON workflow_runs;
DROP INDEX IF EXISTS idx_workflow_runs_status;
DROP INDEX IF EXISTS idx_workflow_runs_session;
DROP TABLE IF EXISTS workflow_runs;
