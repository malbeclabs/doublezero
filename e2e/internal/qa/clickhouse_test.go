package qa

import (
	"os"
	"testing"

	"github.com/ClickHouse/clickhouse-go/v2"
)

func TestClickhouseConfigFromEnv(t *testing.T) {
	t.Run("returns nil when CLICKHOUSE_ADDR is not set", func(t *testing.T) {
		t.Setenv("CLICKHOUSE_ADDR", "")
		if got := ClickhouseConfigFromEnv(); got != nil {
			t.Errorf("expected nil, got %+v", got)
		}
	})

	t.Run("returns config with defaults when only addr is set", func(t *testing.T) {
		t.Setenv("CLICKHOUSE_ADDR", "clickhouse.example.com:8443")
		os.Unsetenv("CLICKHOUSE_DB")
		os.Unsetenv("CLICKHOUSE_USER")
		os.Unsetenv("CLICKHOUSE_PASS")
		os.Unsetenv("CLICKHOUSE_TLS_DISABLED")

		cfg := ClickhouseConfigFromEnv()
		if cfg == nil {
			t.Fatal("expected non-nil config")
		}
		if cfg.Addr != "clickhouse.example.com:8443" {
			t.Errorf("Addr = %q, want %q", cfg.Addr, "clickhouse.example.com:8443")
		}
		if cfg.DB != "default" {
			t.Errorf("DB = %q, want %q", cfg.DB, "default")
		}
		if cfg.User != "default" {
			t.Errorf("User = %q, want %q", cfg.User, "default")
		}
		if cfg.Pass != "" {
			t.Errorf("Pass = %q, want empty", cfg.Pass)
		}
		if cfg.TLSDisable {
			t.Error("TLSDisable = true, want false")
		}
	})

	t.Run("reads all fields from environment", func(t *testing.T) {
		t.Setenv("CLICKHOUSE_ADDR", "ch.internal:8443")
		t.Setenv("CLICKHOUSE_DB", "qa")
		t.Setenv("CLICKHOUSE_USER", "qauser")
		t.Setenv("CLICKHOUSE_PASS", "s3cr3t")
		t.Setenv("CLICKHOUSE_TLS_DISABLED", "true")

		cfg := ClickhouseConfigFromEnv()
		if cfg == nil {
			t.Fatal("expected non-nil config")
		}
		if cfg.Addr != "ch.internal:8443" {
			t.Errorf("Addr = %q, want %q", cfg.Addr, "ch.internal:8443")
		}
		if cfg.DB != "qa" {
			t.Errorf("DB = %q, want %q", cfg.DB, "qa")
		}
		if cfg.User != "qauser" {
			t.Errorf("User = %q, want %q", cfg.User, "qauser")
		}
		if cfg.Pass != "s3cr3t" {
			t.Errorf("Pass = %q, want %q", cfg.Pass, "s3cr3t")
		}
		if !cfg.TLSDisable {
			t.Error("TLSDisable = false, want true")
		}
	})
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

			if opts.Protocol != clickhouse.HTTP {
				t.Errorf("Protocol = %v, want clickhouse.HTTP (%v)", opts.Protocol, clickhouse.HTTP)
			}
			if len(opts.Addr) != 1 || opts.Addr[0] != tt.wantAddr {
				t.Errorf("Addr = %v, want [%s]", opts.Addr, tt.wantAddr)
			}
			if tt.wantTLS && opts.TLS == nil {
				t.Error("TLS = nil, want non-nil (HTTPS mode)")
			}
			if !tt.wantTLS && opts.TLS != nil {
				t.Error("TLS = non-nil, want nil (plain HTTP mode)")
			}
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
