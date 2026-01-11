package slack

import (
	"fmt"
	"os"
)

// Mode represents the Slack bot operation mode
type Mode string

const (
	ModeSocket Mode = "socket" // Development mode using Socket Mode
	ModeHTTP   Mode = "http"   // Production mode using HTTP events
)

// Config holds all configuration for the Slack bot
type Config struct {
	// Bot configuration
	BotToken      string
	AppToken      string
	SigningSecret string
	Mode          Mode
	BotUserID     string

	// Anthropic configuration
	AnthropicAPIKey string

	// ClickHouse configuration
	ClickhouseAddr     string
	ClickhouseDatabase string
	ClickhouseUsername string
	ClickhousePassword string

	// Server configuration
	HTTPAddr    string
	MetricsAddr string

	// Feature flags
	Verbose     bool
	EnablePprof bool
}

// LoadFromEnv loads configuration from environment variables and flags
func LoadFromEnv(modeFlag, httpAddrFlag, metricsAddrFlag string, verbose, enablePprof bool) (*Config, error) {
	cfg := &Config{
		HTTPAddr:    httpAddrFlag,
		MetricsAddr: metricsAddrFlag,
		Verbose:     verbose,
		EnablePprof: enablePprof,
	}

	// Load bot token
	cfg.BotToken = os.Getenv("SLACK_BOT_TOKEN")
	if cfg.BotToken == "" {
		return nil, fmt.Errorf("SLACK_BOT_TOKEN is required")
	}

	// Determine mode
	cfg.Mode = Mode(modeFlag)
	if cfg.Mode == "" {
		// Auto-detect: socket mode if app token is set, otherwise HTTP mode
		if os.Getenv("SLACK_APP_TOKEN") != "" {
			cfg.Mode = ModeSocket
		} else {
			cfg.Mode = ModeHTTP
		}
	}

	if cfg.Mode != ModeSocket && cfg.Mode != ModeHTTP {
		return nil, fmt.Errorf("mode must be 'socket' or 'http', got: %s", cfg.Mode)
	}

	// Load mode-specific tokens
	if cfg.Mode == ModeSocket {
		cfg.AppToken = os.Getenv("SLACK_APP_TOKEN")
		if cfg.AppToken == "" {
			return nil, fmt.Errorf("SLACK_APP_TOKEN is required for socket mode")
		}
	} else {
		cfg.SigningSecret = os.Getenv("SLACK_SIGNING_SECRET")
		if cfg.SigningSecret == "" {
			return nil, fmt.Errorf("SLACK_SIGNING_SECRET is required for HTTP mode")
		}
	}

	// Load Anthropic API key
	cfg.AnthropicAPIKey = os.Getenv("ANTHROPIC_API_KEY")
	if cfg.AnthropicAPIKey == "" {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY is required")
	}

	// Load ClickHouse configuration
	cfg.ClickhouseAddr = os.Getenv("CLICKHOUSE_ADDR")
	if cfg.ClickhouseAddr == "" {
		return nil, fmt.Errorf("CLICKHOUSE_ADDR is required (use --clickhouse-addr flag or CLICKHOUSE_ADDR env var)")
	}
	cfg.ClickhouseDatabase = os.Getenv("CLICKHOUSE_DATABASE")
	if cfg.ClickhouseDatabase == "" {
		return nil, fmt.Errorf("CLICKHOUSE_DATABASE is required (use --clickhouse-database flag or CLICKHOUSE_DATABASE env var)")
	}
	cfg.ClickhouseUsername = os.Getenv("CLICKHOUSE_USERNAME")
	if cfg.ClickhouseUsername == "" {
		return nil, fmt.Errorf("CLICKHOUSE_USERNAME is required (use --clickhouse-username flag or CLICKHOUSE_USERNAME env var)")
	}
	cfg.ClickhousePassword = os.Getenv("CLICKHOUSE_PASSWORD")

	return cfg, nil
}
