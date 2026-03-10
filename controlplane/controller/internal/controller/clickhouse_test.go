package controller

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/ClickHouse/clickhouse-go/v2"
)

// logEntry captures a single log call for test assertions.
type logEntry struct {
	Level   slog.Level
	Message string
}

// capturingHandler is a slog.Handler that records log entries.
type capturingHandler struct {
	entries []logEntry
}

func (h *capturingHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (h *capturingHandler) Handle(_ context.Context, r slog.Record) error {
	h.entries = append(h.entries, logEntry{Level: r.Level, Message: r.Message})
	return nil
}
func (h *capturingHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *capturingHandler) WithGroup(_ string) slog.Handler      { return h }

func TestClickhouseWriterLogEscalation(t *testing.T) {
	h := &capturingHandler{}
	logger := slog.New(h)
	cw := &ClickhouseWriter{log: logger}

	testErr := errors.New("connection reset")

	// First few errors should be WARN
	for range consecutiveErrorThreshold {
		cw.recordFlushError("error sending clickhouse batch", testErr)
	}

	// Verify first errors are WARN, last one crosses threshold and is ERROR
	for i, entry := range h.entries {
		if i < consecutiveErrorThreshold-1 {
			if entry.Level != slog.LevelWarn {
				t.Errorf("entry[%d]: got level %v, want WARN", i, entry.Level)
			}
		} else {
			if entry.Level != slog.LevelError {
				t.Errorf("entry[%d]: got level %v, want ERROR", i, entry.Level)
			}
		}
	}

	// Reset counter (simulating a successful flush)
	cw.consecutiveErrors = 0
	h.entries = nil

	// Next error after reset should be WARN again
	cw.recordFlushError("error sending clickhouse batch", testErr)
	if len(h.entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(h.entries))
	}
	if h.entries[0].Level != slog.LevelWarn {
		t.Errorf("after reset: got level %v, want WARN", h.entries[0].Level)
	}
}

func TestBuildClickhouseOptions(t *testing.T) {
	tests := []struct {
		name       string
		addr       string
		db         string
		user       string
		pass       string
		disableTLS bool
		wantAddr   string
		wantTLS    bool
	}{
		{
			name:       "production defaults (HTTPS)",
			addr:       "clickhouse.example.com:8443",
			db:         "mydb",
			user:       "admin",
			pass:       "secret",
			disableTLS: false,
			wantAddr:   "clickhouse.example.com:8443",
			wantTLS:    true,
		},
		{
			name:       "TLS disabled (plain HTTP for local dev)",
			addr:       "localhost:8123",
			db:         "default",
			user:       "default",
			pass:       "",
			disableTLS: true,
			wantAddr:   "localhost:8123",
			wantTLS:    false,
		},
		{
			name:       "strips https:// scheme prefix",
			addr:       "https://clickhouse.example.com:8443",
			db:         "default",
			user:       "default",
			pass:       "",
			disableTLS: false,
			wantAddr:   "clickhouse.example.com:8443",
			wantTLS:    true,
		},
		{
			name:       "strips http:// scheme prefix",
			addr:       "http://localhost:8123",
			db:         "default",
			user:       "default",
			pass:       "",
			disableTLS: true,
			wantAddr:   "localhost:8123",
			wantTLS:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := buildClickhouseOptions(tt.addr, tt.db, tt.user, tt.pass, tt.disableTLS)

			// Core fix: protocol must always be HTTP (not native TCP)
			if opts.Protocol != clickhouse.HTTP {
				t.Errorf("Protocol = %v, want clickhouse.HTTP (%v)", opts.Protocol, clickhouse.HTTP)
			}

			// Verify address scheme stripping
			if len(opts.Addr) != 1 || opts.Addr[0] != tt.wantAddr {
				t.Errorf("Addr = %v, want [%s]", opts.Addr, tt.wantAddr)
			}

			// Verify TLS configuration
			if tt.wantTLS && opts.TLS == nil {
				t.Error("TLS = nil, want non-nil (HTTPS mode)")
			}
			if !tt.wantTLS && opts.TLS != nil {
				t.Error("TLS = non-nil, want nil (plain HTTP mode)")
			}

			// Verify auth
			if opts.Auth.Database != tt.db {
				t.Errorf("Auth.Database = %q, want %q", opts.Auth.Database, tt.db)
			}
			if opts.Auth.Username != tt.user {
				t.Errorf("Auth.Username = %q, want %q", opts.Auth.Username, tt.user)
			}
			if opts.Auth.Password != tt.pass {
				t.Errorf("Auth.Password = %q, want %q", opts.Auth.Password, tt.pass)
			}
		})
	}
}
