-- +goose Up

-- Account types enum
CREATE TYPE account_type AS ENUM ('domain', 'wallet');

-- Accounts table
CREATE TABLE accounts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_type account_type NOT NULL,
    wallet_address VARCHAR(64) UNIQUE,      -- for wallet users (Solana base58)
    email VARCHAR(255) UNIQUE,              -- for domain users
    email_domain VARCHAR(255),              -- extracted domain for quick lookup
    google_id VARCHAR(255) UNIQUE,          -- Google OAuth subject
    display_name VARCHAR(255),
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_login_at TIMESTAMPTZ
);

CREATE INDEX idx_accounts_wallet_address ON accounts(wallet_address) WHERE wallet_address IS NOT NULL;
CREATE INDEX idx_accounts_email_domain ON accounts(email_domain) WHERE email_domain IS NOT NULL;
CREATE INDEX idx_accounts_google_id ON accounts(google_id) WHERE google_id IS NOT NULL;

-- Auth sessions table
CREATE TABLE auth_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    token_hash VARCHAR(64) NOT NULL UNIQUE, -- SHA256 hash of token
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_auth_sessions_account_id ON auth_sessions(account_id);
CREATE INDEX idx_auth_sessions_expires_at ON auth_sessions(expires_at);

-- Daily usage tracking table
CREATE TABLE usage_daily (
    id SERIAL PRIMARY KEY,
    account_id UUID REFERENCES accounts(id) ON DELETE CASCADE, -- NULL for anonymous
    ip_address INET,                                           -- for anonymous tracking
    date DATE NOT NULL DEFAULT CURRENT_DATE,
    question_count INT NOT NULL DEFAULT 0,
    input_tokens BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT usage_daily_account_date_unique UNIQUE (account_id, date),
    CONSTRAINT usage_daily_ip_date_unique UNIQUE (ip_address, date)
);

-- Partial unique index for anonymous (NULL account_id) IP tracking
CREATE UNIQUE INDEX idx_usage_daily_anon_ip_date
    ON usage_daily(ip_address, date)
    WHERE account_id IS NULL;

CREATE INDEX idx_usage_daily_date ON usage_daily(date);

-- Usage limits configuration table
CREATE TABLE usage_limits (
    id SERIAL PRIMARY KEY,
    account_type account_type,           -- NULL for anonymous
    daily_question_limit INT,            -- NULL for unlimited
    CONSTRAINT usage_limits_type_unique UNIQUE (account_type)
);

-- Insert default limits
INSERT INTO usage_limits (account_type, daily_question_limit) VALUES
    (NULL, 5),           -- anonymous: 5 questions/day
    ('wallet', 50),      -- wallet users: 50 questions/day
    ('domain', NULL);    -- domain users: unlimited

-- Auth nonces table for wallet SIWS (Sign-In With Solana)
CREATE TABLE auth_nonces (
    nonce VARCHAR(64) PRIMARY KEY,
    expires_at TIMESTAMPTZ NOT NULL DEFAULT NOW() + INTERVAL '5 minutes'
);

CREATE INDEX idx_auth_nonces_expires_at ON auth_nonces(expires_at);

-- +goose Down
DROP TABLE IF EXISTS auth_nonces;
DROP TABLE IF EXISTS usage_limits;
DROP TABLE IF EXISTS usage_daily;
DROP TABLE IF EXISTS auth_sessions;
DROP TABLE IF EXISTS accounts;
DROP TYPE IF EXISTS account_type;
