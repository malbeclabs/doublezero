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

	// API configuration
	APIBaseURL string
	WebBaseURL string // Base URL for web UI (for session links)

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

	// Load API configuration
	cfg.APIBaseURL = os.Getenv("API_BASE_URL")
	if cfg.APIBaseURL == "" {
		cfg.APIBaseURL = "http://localhost:8080"
	}

	// Load web UI configuration (optional - if not set, "View on web" links won't be shown)
	cfg.WebBaseURL = os.Getenv("WEB_BASE_URL")

	return cfg, nil
}
