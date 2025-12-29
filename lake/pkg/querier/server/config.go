package server

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/malbeclabs/doublezero/lake/pkg/querier"
)

type Config struct {
	HTTPListener      net.Listener // HTTP server listener
	PostgresListener  net.Listener // PostgreSQL wire protocol listener (optional)
	ReadHeaderTimeout time.Duration
	ShutdownTimeout   time.Duration
	QuerierConfig     querier.Config

	// PostgreSQL authentication (optional)
	// If empty, authentication is disabled (any username/password accepted)
	// Format: map[username]password
	PostgresAccounts map[string]string // Username -> Password mapping (from POSTGRES_ACCOUNTS env var)
}

// LoadFromEnv loads configuration from environment variables
// POSTGRES_ACCOUNTS format: "user1:pass1,user2:pass2" (comma-separated username:password pairs)
func (cfg *Config) LoadFromEnv() error {
	if cfg.PostgresAccounts == nil {
		cfg.PostgresAccounts = make(map[string]string)
	}

	accountsEnv := os.Getenv("POSTGRES_ACCOUNTS")
	if accountsEnv == "" {
		return nil
	}

	// Parse comma-separated list of username:password pairs
	for _, accountStr := range strings.Split(accountsEnv, ",") {
		accountStr = strings.TrimSpace(accountStr)
		if accountStr == "" {
			continue
		}

		parts := strings.SplitN(accountStr, ":", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid account format in POSTGRES_ACCOUNTS: %q (expected username:password)", accountStr)
		}

		username := strings.TrimSpace(parts[0])
		password := strings.TrimSpace(parts[1])

		if username == "" {
			return fmt.Errorf("username cannot be empty in POSTGRES_ACCOUNTS: %q", accountStr)
		}

		cfg.PostgresAccounts[username] = password
	}

	return nil
}

func (cfg *Config) Validate() error {
	if cfg.HTTPListener == nil {
		return errors.New("http listener is required")
	}
	if err := cfg.QuerierConfig.Validate(); err != nil {
		return err
	}
	return nil
}
