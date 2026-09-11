package main

import (
	"context"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/internal/exporter"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

const testNodeFile = "../../config/nodes-aws.json"

var clickhouseEnvKeys = []string{
	"CLICKHOUSE_ADDR",
	"CLICKHOUSE_DB",
	"CLICKHOUSE_USER",
	"CLICKHOUSE_PASS",
	"CLICKHOUSE_TABLE",
	"CLICKHOUSE_TLS_DISABLED",
	"CLICKHOUSE_RUN_MIGRATIONS",
}

func chDefaults() clickhouseConfig {
	return clickhouseConfig{
		Addr:     "localhost:9440",
		Database: "default",
		Username: "default",
		Password: "",
		Table:    "cloud_region_latency",
	}
}

func TestInternetLatency_Main_LoadClickHouseConfig(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want clickhouseConfig
	}{
		{
			name: "defaults when nothing is set",
			env:  map[string]string{},
			want: chDefaults(),
		},
		{
			name: "env overrides every field",
			env: map[string]string{
				"CLICKHOUSE_ADDR":           "clickhouse.internal:9440",
				"CLICKHOUSE_DB":             "telemetry_mainnet_beta",
				"CLICKHOUSE_USER":           "collector",
				"CLICKHOUSE_PASS":           "secret",
				"CLICKHOUSE_TABLE":          "cloud_region_latency",
				"CLICKHOUSE_TLS_DISABLED":   "true",
				"CLICKHOUSE_RUN_MIGRATIONS": "true",
			},
			want: clickhouseConfig{
				Addr:          "clickhouse.internal:9440",
				Database:      "telemetry_mainnet_beta",
				Username:      "collector",
				Password:      "secret",
				Table:         "cloud_region_latency",
				TLSDisabled:   true,
				RunMigrations: true,
			},
		},
		{
			name: "an empty env value falls back to the default",
			env: map[string]string{
				"CLICKHOUSE_ADDR": "",
				"CLICKHOUSE_DB":   "",
			},
			want: chDefaults(),
		},
		{
			name: "toggles are true only for the literal string true",
			env: map[string]string{
				"CLICKHOUSE_TLS_DISABLED":   "1",
				"CLICKHOUSE_RUN_MIGRATIONS": "yes",
			},
			want: chDefaults(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, key := range clickhouseEnvKeys {
				t.Setenv(key, "")
			}
			for key, value := range tt.env {
				t.Setenv(key, value)
			}
			require.Equal(t, tt.want, loadClickHouseConfig())
		})
	}
}

func TestInternetLatency_Main_Getenv(t *testing.T) {
	t.Setenv("DZ_ILC_TEST_UNSET", "")
	require.Equal(t, "fallback", getenv("DZ_ILC_TEST_UNSET", "fallback"),
		"an empty value must fall back, not be returned as empty")

	t.Setenv("DZ_ILC_TEST_SET", "value")
	require.Equal(t, "value", getenv("DZ_ILC_TEST_SET", "fallback"))
}

func TestInternetLatency_Main_CloudFlagIsTheOnlySwitch(t *testing.T) {
	require.NotNil(t, runCmd.Flags().Lookup("cloud-node-file"),
		"--cloud-node-file must be registered on the run command")
	require.Equal(t, "", runCmd.Flags().Lookup("cloud-node-file").DefValue,
		"cloud mode must be off by default")

	for _, name := range []string{"cloud", "output", "cloud-state-file", "cloud-ripeatlas-tag", "cloud-description-prefix"} {
		require.Nil(t, runCmd.Flags().Lookup(name),
			"flag --%s must not exist: cloud mode is selected by --cloud-node-file alone", name)
	}
}

// The verbs that list, create and stop measurements must act on the same set. list-probes has no
// cloud form because it does proximity discovery, which cloud mode never performs.
func TestInternetLatency_Main_MeasurementVerbsShareTheCloudNodeFile(t *testing.T) {
	for _, cmd := range []*cobra.Command{ripeatlasListMeasurementsCmd, ripeatlasCreateMeasurementsCmd, ripeatlasClearMeasurementsCmd} {
		require.NotNil(t, cmd.Flags().Lookup("cloud-node-file"),
			"--cloud-node-file must be registered on atlas %s", cmd.Use)
	}

	require.Nil(t, ripeatlasListProbesCmd.Flags().Lookup("cloud-node-file"))
}

func TestInternetLatency_Main_NewRipeAtlasCollector(t *testing.T) {
	t.Parallel()

	log := slog.New(slog.DiscardHandler)

	t.Run("a missing node file is an error, not a silent exchange collector", func(t *testing.T) {
		t.Parallel()
		_, err := newRipeAtlasCollector(log, nil, filepath.Join(t.TempDir(), "absent.json"))
		require.Error(t, err)
	})

	t.Run("both modes build a collector", func(t *testing.T) {
		t.Parallel()
		cloud, err := newRipeAtlasCollector(log, nil, testNodeFile)
		require.NoError(t, err)
		require.NotNil(t, cloud)

		exchange, err := newRipeAtlasCollector(log, nil, "")
		require.NoError(t, err)
		require.NotNil(t, exchange)
	})
}

// Validate accepts a RIPE-only config, so a slip in the exchange branch would stop the
// WheresItUp feed with nothing failing.
func TestInternetLatency_Main_NewCollectorConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		nodeFile       string
		wantCloudMode  bool
		wantWheresitup bool
	}{
		{
			name:          "a node file selects cloud mode and drops wheresitup",
			nodeFile:      testNodeFile,
			wantCloudMode: true,
		},
		{
			name:           "no node file keeps both feeds",
			nodeFile:       "",
			wantWheresitup: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg, err := newCollectorConfig(slog.New(slog.DiscardHandler), nil, tt.nodeFile)
			require.NoError(t, err)
			require.NoError(t, cfg.Validate())

			require.NotNil(t, cfg.RipeAtlas, "the RIPE Atlas feed runs in both modes")
			require.Equal(t, tt.wantCloudMode, cfg.CloudMode)
			if tt.wantWheresitup {
				require.NotNil(t, cfg.Wheresitup)
			} else {
				require.Nil(t, cfg.Wheresitup)
			}
		})
	}
}

func TestInternetLatency_Main_RunCloudFlushLoop(t *testing.T) {
	t.Parallel()

	t.Run("a partial batch leaves memory on a tick", func(t *testing.T) {
		t.Parallel()

		inserter := &countingInserter{}
		exp := newTestCloudExporter(t, inserter)
		require.NoError(t, exp.WriteRecords(context.Background(), []exporter.Record{testCloudRecord()}))
		require.Equal(t, 1, exp.Buffered())

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		done := make(chan struct{})
		go func() {
			defer close(done)
			runCloudFlushLoop(ctx, slog.New(slog.DiscardHandler), exp, 5*time.Millisecond)
		}()

		require.Eventually(t, func() bool { return inserter.count() == 1 }, 5*time.Second, 5*time.Millisecond)
		cancel()
		<-done
	})

	t.Run("shutdown leaves the exporter open for the caller to close", func(t *testing.T) {
		t.Parallel()

		inserter := &countingInserter{}
		exp := newTestCloudExporter(t, inserter)
		require.NoError(t, exp.WriteRecords(context.Background(), []exporter.Record{testCloudRecord()}))

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		runCloudFlushLoop(ctx, slog.New(slog.DiscardHandler), exp, time.Hour)

		require.False(t, inserter.isClosed(), "the collector may still be exporting when the flush loop returns")
		require.Equal(t, 1, exp.Buffered())

		require.NoError(t, exp.Close())
		require.Equal(t, 1, inserter.count(), "the last partial batch is sent when the caller closes")
		require.Equal(t, 0, exp.Buffered())
	})
}

func newTestCloudExporter(t *testing.T, inserter exporter.ClickHouseBatchInserter) *exporter.ClickHouseExporter {
	t.Helper()

	exp, err := exporter.NewClickHouseExporter(exporter.ClickHouseExporterConfig{
		Logger:        slog.New(slog.DiscardHandler),
		Inserter:      inserter,
		BatchSize:     500,
		FlushInterval: time.Hour,
	})
	require.NoError(t, err)
	return exp
}

func testCloudRecord() exporter.Record {
	return exporter.Record{
		DataProvider:       exporter.DataProviderNameRIPEAtlas,
		SourceExchangeCode: "us-east-1",
		TargetExchangeCode: "eu-west-1",
		Timestamp:          time.Now().UTC(),
		RTT:                70 * time.Millisecond,
		Cloud: &exporter.CloudInfo{
			SourceCloud:     "aws",
			TargetCloud:     "aws",
			ProbeID:         1003385,
			PacketsSent:     3,
			PacketsReceived: 3,
		},
	}
}

type countingInserter struct {
	mu       sync.Mutex
	inserted int
	closed   bool
}

func (i *countingInserter) Insert(_ context.Context, records []exporter.Record) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.inserted += len(records)
	return nil
}

func (i *countingInserter) Close() error {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.closed = true
	return nil
}

func (i *countingInserter) count() int {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.inserted
}

func (i *countingInserter) isClosed() bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.closed
}
