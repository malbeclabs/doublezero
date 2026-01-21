-- +goose Up
-- Add unified steps column to track execution order of thinking and query events
ALTER TABLE workflow_runs ADD COLUMN IF NOT EXISTS steps JSONB NOT NULL DEFAULT '[]';

-- +goose Down
ALTER TABLE workflow_runs DROP COLUMN IF EXISTS steps;
