package solbalance

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/prometheus/client_golang/prometheus"
	io_prometheus_client "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockRPCClient struct {
	balances map[solana.PublicKey]uint64
	err      error
}

func (m *mockRPCClient) GetBalance(ctx context.Context, pubkey solana.PublicKey, commitment solanarpc.CommitmentType) (*solanarpc.GetBalanceResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	balance, ok := m.balances[pubkey]
	if !ok {
		balance = 0
	}
	return &solanarpc.GetBalanceResult{
		Value: balance,
	}, nil
}

func TestTick_UpdatesMetrics(t *testing.T) {
	// Reset metrics before test
	MetricBalanceLamports.Reset()
	MetricBalanceSOL.Reset()
	MetricErrors.Reset()

	// Use well-known Solana program addresses for testing
	pubkey1 := solana.SystemProgramID // 11111111111111111111111111111111
	pubkey2 := solana.TokenProgramID  // TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA

	mockClient := &mockRPCClient{
		balances: map[solana.PublicKey]uint64{
			pubkey1: 500_000_000,   // 0.5 SOL
			pubkey2: 1_000_000_000, // 1.0 SOL
		},
	}

	cfg := &Config{
		Logger:    slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})),
		Interval:  1,
		RPCClient: mockClient,
		Accounts: map[string]solana.PublicKey{
			"debt_accountant":    pubkey1,
			"rewards_accountant": pubkey2,
		},
		Threshold: 0.1,
	}

	watcher, err := NewSolBalanceWatcher(cfg)
	require.NoError(t, err)

	err = watcher.Tick(context.Background())
	require.NoError(t, err)

	// Verify metrics
	debtLamports := getGaugeValue(t, MetricBalanceLamports, "debt_accountant")
	assert.Equal(t, float64(500_000_000), debtLamports)

	debtSOL := getGaugeValue(t, MetricBalanceSOL, "debt_accountant")
	assert.Equal(t, 0.5, debtSOL)

	rewardsLamports := getGaugeValue(t, MetricBalanceLamports, "rewards_accountant")
	assert.Equal(t, float64(1_000_000_000), rewardsLamports)

	rewardsSOL := getGaugeValue(t, MetricBalanceSOL, "rewards_accountant")
	assert.Equal(t, 1.0, rewardsSOL)
}

func TestTick_HandlesRPCErrors(t *testing.T) {
	// Reset metrics before test
	MetricBalanceLamports.Reset()
	MetricBalanceSOL.Reset()
	MetricErrors.Reset()

	pubkey1 := solana.SystemProgramID

	mockClient := &mockRPCClient{
		err: errors.New("rpc error"),
	}

	cfg := &Config{
		Logger:    slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})),
		Interval:  1,
		RPCClient: mockClient,
		Accounts: map[string]solana.PublicKey{
			"debt_accountant": pubkey1,
		},
		Threshold: 0.1,
	}

	watcher, err := NewSolBalanceWatcher(cfg)
	require.NoError(t, err)

	// Tick should not return error even if RPC fails
	err = watcher.Tick(context.Background())
	require.NoError(t, err)

	// Verify error metric was incremented
	errorCount := getCounterValue(t, MetricErrors, MetricErrorTypeGetBalance)
	assert.Equal(t, float64(1), errorCount)
}

func TestValidate(t *testing.T) {
	pubkey := solana.SystemProgramID
	mockClient := &mockRPCClient{}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	tests := []struct {
		name    string
		cfg     *Config
		wantErr string
	}{
		{
			name: "valid config",
			cfg: &Config{
				Logger:    logger,
				Interval:  1,
				RPCClient: mockClient,
				Accounts:  map[string]solana.PublicKey{"test": pubkey},
			},
			wantErr: "",
		},
		{
			name: "missing logger",
			cfg: &Config{
				Interval:  1,
				RPCClient: mockClient,
				Accounts:  map[string]solana.PublicKey{"test": pubkey},
			},
			wantErr: "logger is required",
		},
		{
			name: "invalid interval",
			cfg: &Config{
				Logger:    logger,
				Interval:  0,
				RPCClient: mockClient,
				Accounts:  map[string]solana.PublicKey{"test": pubkey},
			},
			wantErr: "interval must be greater than 0",
		},
		{
			name: "missing rpc client",
			cfg: &Config{
				Logger:   logger,
				Interval: 1,
				Accounts: map[string]solana.PublicKey{"test": pubkey},
			},
			wantErr: "rpc client is required",
		},
		{
			name: "empty accounts",
			cfg: &Config{
				Logger:    logger,
				Interval:  1,
				RPCClient: mockClient,
				Accounts:  map[string]solana.PublicKey{},
			},
			wantErr: "at least one account is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr == "" {
				assert.NoError(t, err)
			} else {
				assert.EqualError(t, err, tt.wantErr)
			}
		})
	}
}

func getGaugeValue(t *testing.T, vec *prometheus.GaugeVec, labelValue string) float64 {
	t.Helper()
	gauge, err := vec.GetMetricWithLabelValues(labelValue)
	require.NoError(t, err)
	var m io_prometheus_client.Metric
	err = gauge.Write(&m)
	require.NoError(t, err)
	return m.GetGauge().GetValue()
}

func getCounterValue(t *testing.T, vec *prometheus.CounterVec, labelValue string) float64 {
	t.Helper()
	counter, err := vec.GetMetricWithLabelValues(labelValue)
	require.NoError(t, err)
	var m io_prometheus_client.Metric
	err = counter.Write(&m)
	require.NoError(t, err)
	return m.GetCounter().GetValue()
}
