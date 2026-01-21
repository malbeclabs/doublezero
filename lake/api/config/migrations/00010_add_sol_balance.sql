-- +goose Up
ALTER TABLE accounts ADD COLUMN sol_balance BIGINT;
ALTER TABLE accounts ADD COLUMN sol_balance_updated_at TIMESTAMPTZ;

-- Update wallet limit from 50 to 10 (basic tier without SOL balance)
UPDATE usage_limits SET daily_question_limit = 10 WHERE account_type = 'wallet';

-- +goose Down
UPDATE usage_limits SET daily_question_limit = 50 WHERE account_type = 'wallet';
ALTER TABLE accounts DROP COLUMN IF EXISTS sol_balance_updated_at;
ALTER TABLE accounts DROP COLUMN IF EXISTS sol_balance;
