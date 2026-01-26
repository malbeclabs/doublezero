-- +goose Up
-- Fix the updated_at trigger to only update when content or name actually changes
-- This prevents timestamp corruption when syncing unchanged sessions

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    -- Only update timestamp if content or name actually changed
    IF NEW.content IS DISTINCT FROM OLD.content OR NEW.name IS DISTINCT FROM OLD.name THEN
        NEW.updated_at = NOW();
    END IF;
    RETURN NEW;
END;
$$ language 'plpgsql';
-- +goose StatementEnd

-- +goose Down
-- Revert to the original trigger that always updates
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ language 'plpgsql';
-- +goose StatementEnd
