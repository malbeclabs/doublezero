package server

import (
	"context"
	"database/sql"
	"log/slog"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/jonboulle/clockwork"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
	"github.com/stretchr/testify/require"
)

type mockServiceabilityRPC struct{}

func (m *mockServiceabilityRPC) GetProgramData(ctx context.Context) (*serviceability.ProgramData, error) {
	return nil, nil
}

type mockTelemetryRPC struct{}

func (m *mockTelemetryRPC) GetDeviceLatencySamples(ctx context.Context, originDevicePK, targetDevicePK, linkPK solana.PublicKey, epoch uint64) (*telemetry.DeviceLatencySamples, error) {
	return nil, nil
}

func (m *mockTelemetryRPC) GetInternetLatencySamples(ctx context.Context, dataProviderName string, originLocationPK, targetLocationPK, agentPK solana.PublicKey, epoch uint64) (*telemetry.InternetLatencySamples, error) {
	return nil, nil
}

type mockEpochRPC struct{}

func (m *mockEpochRPC) GetEpochInfo(ctx context.Context, commitment solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
	return nil, nil
}

type mockSolanaRPC struct{}

func (m *mockSolanaRPC) GetEpochInfo(ctx context.Context, commitment solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
	return nil, nil
}

func (m *mockSolanaRPC) GetLeaderSchedule(ctx context.Context) (solanarpc.GetLeaderScheduleResult, error) {
	return nil, nil
}

func (m *mockSolanaRPC) GetClusterNodes(ctx context.Context) ([]*solanarpc.GetClusterNodesResult, error) {
	return nil, nil
}

func (m *mockSolanaRPC) GetVoteAccounts(ctx context.Context, opts *solanarpc.GetVoteAccountsOpts) (*solanarpc.GetVoteAccountsResult, error) {
	return nil, nil
}

type mockDB struct{}

func (m *mockDB) Exec(query string, args ...any) (sql.Result, error) { return nil, nil }
func (m *mockDB) Query(query string, args ...any) (*sql.Rows, error) { return nil, nil }
func (m *mockDB) Begin() (*sql.Tx, error)                            { return nil, nil }
func (m *mockDB) Close() error                                       { return nil }

func validConfig() Config {
	return Config{
		Version:                "test",
		ListenAddr:             "localhost:8080",
		Logger:                 slog.Default(),
		Clock:                  clockwork.NewFakeClock(),
		ServiceabilityRPC:      &mockServiceabilityRPC{},
		TelemetryRPC:           &mockTelemetryRPC{},
		DZEpochRPC:             &mockEpochRPC{},
		SolanaRPC:              &mockSolanaRPC{},
		DB:                     &mockDB{},
		RefreshInterval:        30 * time.Second,
		MaxConcurrency:         32,
		InternetLatencyAgentPK: solana.MustPublicKeyFromBase58("So11111111111111111111111111111111111111112"),
		InternetDataProviders:  []string{"test-provider"},
	}
}

func TestMCP_Server_Config_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		modify  func(*Config)
		wantErr bool
	}{
		{
			name:    "valid config",
			modify:  func(c *Config) {},
			wantErr: false,
		},
		{
			name: "missing logger",
			modify: func(c *Config) {
				c.Logger = nil
			},
			wantErr: true,
		},
		{
			name: "missing serviceability rpc",
			modify: func(c *Config) {
				c.ServiceabilityRPC = nil
			},
			wantErr: true,
		},
		{
			name: "missing telemetry rpc",
			modify: func(c *Config) {
				c.TelemetryRPC = nil
			},
			wantErr: true,
		},
		{
			name: "missing epoch rpc",
			modify: func(c *Config) {
				c.DZEpochRPC = nil
			},
			wantErr: true,
		},
		{
			name: "missing db",
			modify: func(c *Config) {
				c.DB = nil
			},
			wantErr: true,
		},
		{
			name: "invalid max concurrency",
			modify: func(c *Config) {
				c.MaxConcurrency = 0
			},
			wantErr: true,
		},
		{
			name: "invalid refresh interval",
			modify: func(c *Config) {
				c.RefreshInterval = 0
			},
			wantErr: true,
		},
		{
			name: "zero internet latency agent pk",
			modify: func(c *Config) {
				c.InternetLatencyAgentPK = solana.PublicKey{}
			},
			wantErr: true,
		},
		{
			name: "empty internet data providers",
			modify: func(c *Config) {
				c.InternetDataProviders = nil
			},
			wantErr: true,
		},
		{
			name: "sets default clock",
			modify: func(c *Config) {
				c.Clock = nil
			},
			wantErr: false,
		},
		{
			name: "sets default read header timeout",
			modify: func(c *Config) {
				c.ReadHeaderTimeout = 0
			},
			wantErr: false,
		},
		{
			name: "sets default shutdown timeout",
			modify: func(c *Config) {
				c.ShutdownTimeout = 0
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := validConfig()
			tt.modify(&cfg)
			err := cfg.Validate()
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.NotNil(t, cfg.Clock, "Config.Validate() should set default clock")
				require.NotZero(t, cfg.ReadHeaderTimeout, "Config.Validate() should set default read header timeout")
				require.NotZero(t, cfg.ShutdownTimeout, "Config.Validate() should set default shutdown timeout")
			}
		})
	}
}
