-- +goose Up
-- Add columns for distributed workflow claiming
-- This prevents multiple API replicas from resuming the same workflow

ALTER TABLE workflow_runs ADD COLUMN claimed_by TEXT;
ALTER TABLE workflow_runs ADD COLUMN claimed_at TIMESTAMPTZ;

-- Index for finding unclaimed or stale-claimed workflows
CREATE INDEX IF NOT EXISTS idx_workflow_runs_claimable ON workflow_runs(status, claimed_at)
    WHERE status = 'running';

-- +goose Down
DROP INDEX IF EXISTS idx_workflow_runs_claimable;
ALTER TABLE workflow_runs DROP COLUMN IF EXISTS claimed_at;
ALTER TABLE workflow_runs DROP COLUMN IF EXISTS claimed_by;
