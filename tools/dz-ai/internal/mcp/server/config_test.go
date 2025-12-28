package server

import (
	"context"
	"log/slog"
	"net"
	"os"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/jonboulle/clockwork"
	"github.com/malbeclabs/doublezero/lake/pkg/duck"
	"github.com/malbeclabs/doublezero/lake/pkg/indexer"
	"github.com/malbeclabs/doublezero/lake/pkg/querier"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
	"github.com/malbeclabs/doublezero/tools/maxmind/pkg/geoip"
	"github.com/stretchr/testify/require"
)

type mockServiceabilityRPC struct{}

func (m *mockServiceabilityRPC) GetProgramData(ctx context.Context) (*serviceability.ProgramData, error) {
	return nil, nil
}

type mockTelemetryRPC struct{}

func (m *mockTelemetryRPC) GetDeviceLatencySamplesTail(ctx context.Context, originDevicePK, targetDevicePK, linkPK solana.PublicKey, epoch uint64, existingMaxIdx int) (*telemetry.DeviceLatencySamplesHeader, int, []uint32, error) {
	return nil, 0, nil, telemetry.ErrAccountNotFound
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

type mockGeoIPResolver struct{}

func (m *mockGeoIPResolver) Resolve(ip net.IP) *geoip.Record {
	return nil
}

type mockInfluxDBClient struct{}

func (m *mockInfluxDBClient) QuerySQL(ctx context.Context, sqlQuery string) ([]map[string]any, error) {
	return nil, nil
}

func (m *mockInfluxDBClient) Close() error {
	return nil
}

func testLogger(t *testing.T) *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, nil))
}

func testDB(t *testing.T) duck.DB {
	db, err := duck.NewDB(t.Context(), "", slog.New(slog.NewTextHandler(os.Stderr, nil)))
	require.NoError(t, err)
	t.Cleanup(func() {
		db.Close()
	})
	return db
}

func testIndexer(t *testing.T) *indexer.Indexer {
	indexer, err := indexer.New(t.Context(), indexer.Config{
		Logger:                       slog.Default(),
		Clock:                        clockwork.NewFakeClock(),
		DB:                           testDB(t),
		RefreshInterval:              30 * time.Second,
		MaxConcurrency:               32,
		ServiceabilityRPC:            &mockServiceabilityRPC{},
		TelemetryRPC:                 &mockTelemetryRPC{},
		DZEpochRPC:                   &mockEpochRPC{},
		SolanaRPC:                    &mockSolanaRPC{},
		InternetLatencyAgentPK:       solana.MustPublicKeyFromBase58("So11111111111111111111111111111111111111112"),
		InternetDataProviders:        []string{"test-provider"},
		GeoIPResolver:                &mockGeoIPResolver{},
		DeviceUsageInfluxClient:      &mockInfluxDBClient{},
		DeviceUsageInfluxBucket:      "test-bucket",
		DeviceUsageInfluxQueryWindow: 1 * time.Hour,
		ReadyIncludesDeviceUsage:     false,
	})
	require.NoError(t, err)
	return indexer
}

func testQuerier(t *testing.T, idx *indexer.Indexer) *querier.Querier {
	querier, err := querier.New(querier.Config{
		Logger:  testLogger(t),
		DB:      testDB(t),
		Schemas: idx.Schemas(),
	})
	require.NoError(t, err)
	return querier
}

func validConfig(t *testing.T) Config {
	idx := testIndexer(t)
	return Config{
		Version:    "test",
		ListenAddr: "localhost:8080",
		Logger:     slog.Default(),
		Indexer:    idx,
		Querier:    testQuerier(t, idx),
	}
}

func TestAI_MCP_Server_Config_Validate(t *testing.T) {
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
			name: "missing querier",
			modify: func(c *Config) {
				c.Querier = nil
			},
			wantErr: true,
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

			cfg := validConfig(t)
			tt.modify(&cfg)
			err := cfg.Validate()
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.NotZero(t, cfg.ReadHeaderTimeout, "Config.Validate() should set default read header timeout")
				require.NotZero(t, cfg.ShutdownTimeout, "Config.Validate() should set default shutdown timeout")
			}
		})
	}
}
