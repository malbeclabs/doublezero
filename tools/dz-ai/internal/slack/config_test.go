package slack

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAI_Slack_LoadFromEnv(t *testing.T) {
	// Save original env vars
	originalEnv := map[string]string{}
	envVars := []string{
		"SLACK_BOT_TOKEN",
		"SLACK_APP_TOKEN",
		"SLACK_SIGNING_SECRET",
		"ANTHROPIC_API_KEY",
		"MCP_URL",
		"MCP_TOKEN",
	}

	for _, key := range envVars {
		originalEnv[key] = os.Getenv(key)
		os.Unsetenv(key)
	}

	t.Cleanup(func() {
		// Restore original env vars
		for key, value := range originalEnv {
			if value != "" {
				os.Setenv(key, value)
			} else {
				os.Unsetenv(key)
			}
		}
	})

	tests := []struct {
		name            string
		setupEnv        func()
		modeFlag        string
		httpAddrFlag    string
		metricsAddrFlag string
		verbose         bool
		enablePprof     bool
		wantErr         bool
		errContains     string
		checkConfig     func(*testing.T, *Config)
	}{
		{
			name: "socket mode with all required vars",
			setupEnv: func() {
				os.Setenv("SLACK_BOT_TOKEN", "xoxb-test")
				os.Setenv("SLACK_APP_TOKEN", "xapp-test")
				os.Setenv("ANTHROPIC_API_KEY", "sk-test")
				os.Setenv("MCP_URL", "http://localhost:8080")
			},
			modeFlag: "socket",
			checkConfig: func(t *testing.T, cfg *Config) {
				require.Equal(t, ModeSocket, cfg.Mode)
				require.Equal(t, "xoxb-test", cfg.BotToken)
				require.Equal(t, "xapp-test", cfg.AppToken)
				require.Equal(t, "sk-test", cfg.AnthropicAPIKey)
				require.Equal(t, "http://localhost:8080", cfg.MCPEndpoint)
			},
		},
		{
			name: "http mode with all required vars",
			setupEnv: func() {
				os.Setenv("SLACK_BOT_TOKEN", "xoxb-test")
				os.Setenv("SLACK_SIGNING_SECRET", "secret")
				os.Setenv("ANTHROPIC_API_KEY", "sk-test")
				os.Setenv("MCP_URL", "http://localhost:8080")
			},
			modeFlag: "http",
			checkConfig: func(t *testing.T, cfg *Config) {
				require.Equal(t, ModeHTTP, cfg.Mode)
				require.Equal(t, "secret", cfg.SigningSecret)
			},
		},
		{
			name: "auto-detect socket mode",
			setupEnv: func() {
				os.Setenv("SLACK_BOT_TOKEN", "xoxb-test")
				os.Setenv("SLACK_APP_TOKEN", "xapp-test")
				os.Setenv("ANTHROPIC_API_KEY", "sk-test")
				os.Setenv("MCP_URL", "http://localhost:8080")
			},
			modeFlag: "",
			checkConfig: func(t *testing.T, cfg *Config) {
				require.Equal(t, ModeSocket, cfg.Mode)
			},
		},
		{
			name: "auto-detect http mode",
			setupEnv: func() {
				os.Setenv("SLACK_BOT_TOKEN", "xoxb-test")
				os.Setenv("SLACK_SIGNING_SECRET", "secret")
				os.Setenv("ANTHROPIC_API_KEY", "sk-test")
				os.Setenv("MCP_URL", "http://localhost:8080")
			},
			modeFlag: "",
			checkConfig: func(t *testing.T, cfg *Config) {
				require.Equal(t, ModeHTTP, cfg.Mode)
			},
		},
		{
			name: "missing bot token",
			setupEnv: func() {
				os.Setenv("ANTHROPIC_API_KEY", "sk-test")
				os.Setenv("MCP_URL", "http://localhost:8080")
			},
			modeFlag:    "socket",
			wantErr:     true,
			errContains: "SLACK_BOT_TOKEN is required",
		},
		{
			name: "missing app token for socket mode",
			setupEnv: func() {
				os.Setenv("SLACK_BOT_TOKEN", "xoxb-test")
				os.Setenv("ANTHROPIC_API_KEY", "sk-test")
				os.Setenv("MCP_URL", "http://localhost:8080")
			},
			modeFlag:    "socket",
			wantErr:     true,
			errContains: "SLACK_APP_TOKEN is required for socket mode",
		},
		{
			name: "missing signing secret for http mode",
			setupEnv: func() {
				os.Setenv("SLACK_BOT_TOKEN", "xoxb-test")
				os.Setenv("ANTHROPIC_API_KEY", "sk-test")
				os.Setenv("MCP_URL", "http://localhost:8080")
			},
			modeFlag:    "http",
			wantErr:     true,
			errContains: "SLACK_SIGNING_SECRET is required for HTTP mode",
		},
		{
			name: "missing anthropic key",
			setupEnv: func() {
				os.Setenv("SLACK_BOT_TOKEN", "xoxb-test")
				os.Setenv("SLACK_APP_TOKEN", "xapp-test")
				os.Setenv("MCP_URL", "http://localhost:8080")
			},
			modeFlag:    "socket",
			wantErr:     true,
			errContains: "ANTHROPIC_API_KEY is required",
		},
		{
			name: "missing MCP endpoint",
			setupEnv: func() {
				os.Setenv("SLACK_BOT_TOKEN", "xoxb-test")
				os.Setenv("SLACK_APP_TOKEN", "xapp-test")
				os.Setenv("ANTHROPIC_API_KEY", "sk-test")
				os.Unsetenv("MCP_URL") // Ensure it's unset
			},
			modeFlag:    "socket",
			wantErr:     true,
			errContains: "MCP endpoint is required",
		},
		{
			name: "invalid mode",
			setupEnv: func() {
				os.Setenv("SLACK_BOT_TOKEN", "xoxb-test")
				os.Setenv("SLACK_APP_TOKEN", "xapp-test")
				os.Setenv("ANTHROPIC_API_KEY", "sk-test")
				os.Setenv("MCP_URL", "http://localhost:8080")
			},
			modeFlag:    "invalid",
			wantErr:     true,
			errContains: "mode must be 'socket' or 'http'",
		},
		{
			name: "flags are set correctly",
			setupEnv: func() {
				os.Setenv("SLACK_BOT_TOKEN", "xoxb-test")
				os.Setenv("SLACK_APP_TOKEN", "xapp-test")
				os.Setenv("ANTHROPIC_API_KEY", "sk-test")
				os.Setenv("MCP_URL", "http://localhost:8080")
			},
			modeFlag:        "socket",
			httpAddrFlag:    "0.0.0.0:3000",
			metricsAddrFlag: "0.0.0.0:8080",
			verbose:         true,
			enablePprof:     true,
			checkConfig: func(t *testing.T, cfg *Config) {
				require.Equal(t, "0.0.0.0:3000", cfg.HTTPAddr)
				require.Equal(t, "0.0.0.0:8080", cfg.MetricsAddr)
				require.True(t, cfg.Verbose)
				require.True(t, cfg.EnablePprof)
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			// Don't run subtests in parallel - they modify shared environment variables
			// Clean up env before each test
			for _, key := range envVars {
				os.Unsetenv(key)
			}

			if tt.setupEnv != nil {
				tt.setupEnv()
			}

			cfg, err := LoadFromEnv(tt.modeFlag, tt.httpAddrFlag, tt.metricsAddrFlag, tt.verbose, tt.enablePprof)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					require.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
				require.NotNil(t, cfg)
				if tt.checkConfig != nil {
					tt.checkConfig(t, cfg)
				}
			}
		})
	}
}
