package controller

import (
	"testing"

	"github.com/ClickHouse/clickhouse-go/v2"
)

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
