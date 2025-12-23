package dztelem

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/require"

	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
	dzsvc "github.com/malbeclabs/doublezero/tools/mcp/internal/dz/serviceability"
)

type mockTelemetryRPC struct{}

func (m *mockTelemetryRPC) GetDeviceLatencySamples(ctx context.Context, originDevicePK, targetDevicePK, linkPK solana.PublicKey, epoch uint64) (*telemetry.DeviceLatencySamples, error) {
	return nil, telemetry.ErrAccountNotFound
}

func (m *mockTelemetryRPC) GetInternetLatencySamples(ctx context.Context, dataProviderName string, originLocationPK, targetLocationPK, agentPK solana.PublicKey, epoch uint64) (*telemetry.InternetLatencySamples, error) {
	return nil, telemetry.ErrAccountNotFound
}

type mockEpochRPC struct{}

func (m *mockEpochRPC) GetEpochInfo(ctx context.Context, commitment solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
	return &solanarpc.GetEpochInfoResult{
		Epoch: 100,
	}, nil
}

type MockServiceabilityRPC struct {
	getProgramDataFunc func(context.Context) (*serviceability.ProgramData, error)
}

func (m *MockServiceabilityRPC) GetProgramData(ctx context.Context) (*serviceability.ProgramData, error) {
	if m.getProgramDataFunc != nil {
		return m.getProgramDataFunc(ctx)
	}
	return &serviceability.ProgramData{}, nil
}

func TestMCP_Telemetry_View_Ready(t *testing.T) {
	t.Parallel()

	t.Run("returns false when not ready", func(t *testing.T) {
		t.Parallel()

		db, err := sql.Open("duckdb", "")
		require.NoError(t, err)
		defer db.Close()

		svcView, err := dzsvc.NewView(dzsvc.ViewConfig{
			Logger:            slog.New(slog.NewTextHandler(os.Stderr, nil)),
			Clock:             clockwork.NewFakeClock(),
			ServiceabilityRPC: &MockServiceabilityRPC{},
			RefreshInterval:   time.Second,
			DB:                db,
		})
		require.NoError(t, err)

		view, err := NewView(ViewConfig{
			Logger:                 slog.New(slog.NewTextHandler(os.Stderr, nil)),
			Clock:                  clockwork.NewFakeClock(),
			TelemetryRPC:           &mockTelemetryRPC{},
			EpochRPC:               &mockEpochRPC{},
			MaxConcurrency:         32,
			InternetLatencyAgentPK: solana.MustPublicKeyFromBase58("So11111111111111111111111111111111111111112"),
			InternetDataProviders:  []string{"test-provider"},
			DB:                     db,
			Serviceability:         svcView,
			RefreshInterval:        time.Second,
		})
		require.NoError(t, err)

		require.False(t, view.Ready(), "view should not be ready before first refresh")
	})
}

func TestMCP_Telemetry_View_WaitReady(t *testing.T) {
	t.Parallel()

	t.Run("returns error when context is cancelled", func(t *testing.T) {
		t.Parallel()

		db, err := sql.Open("duckdb", "")
		require.NoError(t, err)
		defer db.Close()

		svcView, err := dzsvc.NewView(dzsvc.ViewConfig{
			Logger:            slog.New(slog.NewTextHandler(os.Stderr, nil)),
			Clock:             clockwork.NewFakeClock(),
			ServiceabilityRPC: &MockServiceabilityRPC{},
			RefreshInterval:   time.Second,
			DB:                db,
		})
		require.NoError(t, err)

		view, err := NewView(ViewConfig{
			Logger:                 slog.New(slog.NewTextHandler(os.Stderr, nil)),
			Clock:                  clockwork.NewFakeClock(),
			TelemetryRPC:           &mockTelemetryRPC{},
			EpochRPC:               &mockEpochRPC{},
			MaxConcurrency:         32,
			InternetLatencyAgentPK: solana.MustPublicKeyFromBase58("So11111111111111111111111111111111111111112"),
			InternetDataProviders:  []string{"test-provider"},
			DB:                     db,
			Serviceability:         svcView,
			RefreshInterval:        time.Second,
		})
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err = view.WaitReady(ctx)
		require.Error(t, err)
		require.Contains(t, err.Error(), "context cancelled")
	})
}

func TestMCP_Telemetry_View_ConvertDeviceLatencySamples(t *testing.T) {
	t.Parallel()

	t.Run("converts onchain device latency samples to domain types", func(t *testing.T) {
		t.Parallel()

		samples := &telemetry.DeviceLatencySamples{
			DeviceLatencySamplesHeader: telemetry.DeviceLatencySamplesHeader{
				StartTimestampMicroseconds:   1_600_000_000,
				SamplingIntervalMicroseconds: 100_000,
			},
			Samples: []uint32{5000, 6000, 7000},
		}

		result := convertDeviceLatencySamples(samples, "DEV001 → DEV002", 123)

		require.Len(t, result, 3)
		require.Equal(t, "DEV001 → DEV002", result[0].CircuitCode)
		require.Equal(t, uint64(123), result[0].Epoch)
		require.Equal(t, 0, result[0].SampleIndex)
		require.Equal(t, uint64(1_600_000_000), result[0].TimestampMicroseconds)
		require.Equal(t, uint32(5000), result[0].RTTMicroseconds)

		require.Equal(t, 1, result[1].SampleIndex)
		require.Equal(t, uint64(1_600_000_000+100_000), result[1].TimestampMicroseconds)
		require.Equal(t, uint32(6000), result[1].RTTMicroseconds)

		require.Equal(t, 2, result[2].SampleIndex)
		require.Equal(t, uint64(1_600_000_000+200_000), result[2].TimestampMicroseconds)
		require.Equal(t, uint32(7000), result[2].RTTMicroseconds)
	})

	t.Run("handles empty samples", func(t *testing.T) {
		t.Parallel()

		samples := &telemetry.DeviceLatencySamples{
			DeviceLatencySamplesHeader: telemetry.DeviceLatencySamplesHeader{
				StartTimestampMicroseconds:   1_600_000_000,
				SamplingIntervalMicroseconds: 100_000,
			},
			Samples: []uint32{},
		}

		result := convertDeviceLatencySamples(samples, "TEST", 0)
		require.Empty(t, result)
	})
}

func TestMCP_Telemetry_View_ConvertInternetLatencySamples(t *testing.T) {
	t.Parallel()

	t.Run("converts onchain internet latency samples to domain types", func(t *testing.T) {
		t.Parallel()

		samples := &telemetry.InternetLatencySamples{
			InternetLatencySamplesHeader: telemetry.InternetLatencySamplesHeader{
				StartTimestampMicroseconds:   1_700_000_000,
				SamplingIntervalMicroseconds: 250_000,
			},
			Samples: []uint32{10000, 11000, 12000},
		}

		result := convertInternetLatencySamples(samples, "NYC → LAX", "test-provider", 456)

		require.Len(t, result, 3)
		require.Equal(t, "NYC → LAX", result[0].CircuitCode)
		require.Equal(t, "test-provider", result[0].DataProvider)
		require.Equal(t, uint64(456), result[0].Epoch)
		require.Equal(t, 0, result[0].SampleIndex)
		require.Equal(t, uint64(1_700_000_000), result[0].TimestampMicroseconds)
		require.Equal(t, uint32(10000), result[0].RTTMicroseconds)

		require.Equal(t, 1, result[1].SampleIndex)
		require.Equal(t, uint64(1_700_000_000+250_000), result[1].TimestampMicroseconds)
		require.Equal(t, uint32(11000), result[1].RTTMicroseconds)

		require.Equal(t, 2, result[2].SampleIndex)
		require.Equal(t, uint64(1_700_000_000+500_000), result[2].TimestampMicroseconds)
		require.Equal(t, uint32(12000), result[2].RTTMicroseconds)
	})

	t.Run("handles empty samples", func(t *testing.T) {
		t.Parallel()

		samples := &telemetry.InternetLatencySamples{
			InternetLatencySamplesHeader: telemetry.InternetLatencySamplesHeader{
				StartTimestampMicroseconds:   1_700_000_000,
				SamplingIntervalMicroseconds: 250_000,
			},
			Samples: []uint32{},
		}

		result := convertInternetLatencySamples(samples, "TEST", "provider", 0)
		require.Empty(t, result)
	})
}

func TestMCP_Telemetry_View_Refresh_SavesToDB(t *testing.T) {
	t.Parallel()

	t.Run("saves device-link circuits to database", func(t *testing.T) {
		t.Parallel()

		db, err := sql.Open("duckdb", "")
		require.NoError(t, err)
		defer db.Close()

		// First, set up serviceability view with devices and links
		devicePK1 := [32]byte{1, 2, 3, 4}
		devicePK2 := [32]byte{5, 6, 7, 8}
		linkPK := [32]byte{9, 10, 11, 12}
		contributorPK := [32]byte{13, 14, 15, 16}
		metroPK := [32]byte{17, 18, 19, 20}
		ownerPK := [32]byte{21, 22, 23, 24}
		publicIP1 := [4]byte{192, 168, 1, 1}
		publicIP2 := [4]byte{192, 168, 1, 2}
		tunnelNet := [5]byte{10, 0, 0, 0, 24}

		svcMockRPC := &MockServiceabilityRPC{
			getProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
				return &serviceability.ProgramData{
					Contributors: []serviceability.Contributor{
						{
							PubKey: contributorPK,
							Owner:  ownerPK,
							Code:   "CONTRIB",
						},
					},
					Devices: []serviceability.Device{
						{
							PubKey:            devicePK1,
							Owner:             ownerPK,
							Status:            serviceability.DeviceStatusActivated,
							DeviceType:        serviceability.DeviceDeviceTypeHybrid,
							Code:              "DEV1",
							PublicIp:          publicIP1,
							ContributorPubKey: contributorPK,
							ExchangePubKey:    metroPK,
						},
						{
							PubKey:            devicePK2,
							Owner:             ownerPK,
							Status:            serviceability.DeviceStatusActivated,
							DeviceType:        serviceability.DeviceDeviceTypeHybrid,
							Code:              "DEV2",
							PublicIp:          publicIP2,
							ContributorPubKey: contributorPK,
							ExchangePubKey:    metroPK,
						},
					},
					Links: []serviceability.Link{
						{
							PubKey:            linkPK,
							Owner:             ownerPK,
							Status:            serviceability.LinkStatusActivated,
							Code:              "LINK1",
							TunnelNet:         tunnelNet,
							ContributorPubKey: contributorPK,
							SideAPubKey:       devicePK1,
							SideZPubKey:       devicePK2,
							SideAIfaceName:    "eth0",
							SideZIfaceName:    "eth1",
							LinkType:          serviceability.LinkLinkTypeWAN,
							DelayNs:           1000000,
							JitterNs:          50000,
						},
					},
				}, nil
			},
		}

		svcView, err := dzsvc.NewView(dzsvc.ViewConfig{
			Logger:            slog.New(slog.NewTextHandler(os.Stderr, nil)),
			Clock:             clockwork.NewFakeClock(),
			ServiceabilityRPC: svcMockRPC,
			RefreshInterval:   time.Second,
			DB:                db,
		})
		require.NoError(t, err)

		ctx := context.Background()
		err = svcView.Refresh(ctx)
		require.NoError(t, err)

		// Now set up telemetry view
		mockTelemetryRPC := &mockTelemetryRPC{}
		mockEpochRPC := &mockEpochRPC{}

		view, err := NewView(ViewConfig{
			Logger:                 slog.New(slog.NewTextHandler(os.Stderr, nil)),
			Clock:                  clockwork.NewFakeClock(),
			TelemetryRPC:           mockTelemetryRPC,
			EpochRPC:               mockEpochRPC,
			MaxConcurrency:         32,
			InternetLatencyAgentPK: solana.MustPublicKeyFromBase58("So11111111111111111111111111111111111111112"),
			InternetDataProviders:  []string{"test-provider"},
			DB:                     db,
			Serviceability:         svcView,
			RefreshInterval:        time.Second,
		})
		require.NoError(t, err)

		err = view.Refresh(ctx)
		require.NoError(t, err)

		// Verify circuits were saved
		var circuitCount int
		err = db.QueryRow("SELECT COUNT(*) FROM dz_device_link_circuits").Scan(&circuitCount)
		require.NoError(t, err)
		require.Equal(t, 2, circuitCount) // Forward and reverse circuits

		var code, originDevicePK, targetDevicePK, linkPKStr, linkCode, linkType, contributorCode string
		var committedRTT, committedJitter float64
		err = db.QueryRow("SELECT code, origin_device_pk, target_device_pk, link_pk, link_code, link_type, contributor_code, committed_rtt, committed_jitter FROM dz_device_link_circuits LIMIT 1").Scan(&code, &originDevicePK, &targetDevicePK, &linkPKStr, &linkCode, &linkType, &contributorCode, &committedRTT, &committedJitter)
		require.NoError(t, err)
		require.Contains(t, code, "DEV1")
		require.Contains(t, code, "DEV2")
		require.Equal(t, solana.PublicKeyFromBytes(linkPK[:]).String(), linkPKStr)
		require.Equal(t, "LINK1", linkCode)
		require.Equal(t, "WAN", linkType)
		require.Equal(t, "CONTRIB", contributorCode)
		require.InDelta(t, 1000.0, committedRTT, 0.1)  // 1000000 ns / 1000 = 1000 us
		require.InDelta(t, 50.0, committedJitter, 0.1) // 50000 ns / 1000 = 50 us
	})

	t.Run("saves device-link latency samples to database", func(t *testing.T) {
		t.Parallel()

		db, err := sql.Open("duckdb", "")
		require.NoError(t, err)
		defer db.Close()

		// Set up serviceability view
		devicePK1 := [32]byte{1, 2, 3, 4}
		devicePK2 := [32]byte{5, 6, 7, 8}
		linkPK := [32]byte{9, 10, 11, 12}
		contributorPK := [32]byte{13, 14, 15, 16}
		metroPK := [32]byte{17, 18, 19, 20}
		ownerPK := [32]byte{21, 22, 23, 24}
		publicIP1 := [4]byte{192, 168, 1, 1}
		publicIP2 := [4]byte{192, 168, 1, 2}
		tunnelNet := [5]byte{10, 0, 0, 0, 24}

		svcMockRPC := &MockServiceabilityRPC{
			getProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
				return &serviceability.ProgramData{
					Contributors: []serviceability.Contributor{
						{
							PubKey: contributorPK,
							Owner:  ownerPK,
							Code:   "CONTRIB",
						},
					},
					Devices: []serviceability.Device{
						{
							PubKey:            devicePK1,
							Owner:             ownerPK,
							Status:            serviceability.DeviceStatusActivated,
							DeviceType:        serviceability.DeviceDeviceTypeHybrid,
							Code:              "DEV1",
							PublicIp:          publicIP1,
							ContributorPubKey: contributorPK,
							ExchangePubKey:    metroPK,
						},
						{
							PubKey:            devicePK2,
							Owner:             ownerPK,
							Status:            serviceability.DeviceStatusActivated,
							DeviceType:        serviceability.DeviceDeviceTypeHybrid,
							Code:              "DEV2",
							PublicIp:          publicIP2,
							ContributorPubKey: contributorPK,
							ExchangePubKey:    metroPK,
						},
					},
					Links: []serviceability.Link{
						{
							PubKey:            linkPK,
							Owner:             ownerPK,
							Status:            serviceability.LinkStatusActivated,
							Code:              "LINK1",
							TunnelNet:         tunnelNet,
							ContributorPubKey: contributorPK,
							SideAPubKey:       devicePK1,
							SideZPubKey:       devicePK2,
							SideAIfaceName:    "eth0",
							SideZIfaceName:    "eth1",
							LinkType:          serviceability.LinkLinkTypeWAN,
							DelayNs:           1000000,
							JitterNs:          50000,
						},
					},
				}, nil
			},
		}

		svcView, err := dzsvc.NewView(dzsvc.ViewConfig{
			Logger:            slog.New(slog.NewTextHandler(os.Stderr, nil)),
			Clock:             clockwork.NewFakeClock(),
			ServiceabilityRPC: svcMockRPC,
			RefreshInterval:   time.Second,
			DB:                db,
		})
		require.NoError(t, err)

		ctx := context.Background()
		err = svcView.Refresh(ctx)
		require.NoError(t, err)

		// Set up telemetry RPC to return samples
		originPK := solana.PublicKeyFromBytes(devicePK1[:])
		targetPK := solana.PublicKeyFromBytes(devicePK2[:])
		linkPKPubKey := solana.PublicKeyFromBytes(linkPK[:])

		mockTelemetryRPC := &mockTelemetryRPCWithSamples{
			samples: map[string]*telemetry.DeviceLatencySamples{
				key(originPK, targetPK, linkPKPubKey, 100): {
					DeviceLatencySamplesHeader: telemetry.DeviceLatencySamplesHeader{
						StartTimestampMicroseconds:   1_600_000_000,
						SamplingIntervalMicroseconds: 100_000,
					},
					Samples: []uint32{5000, 6000, 7000},
				},
			},
		}

		mockEpochRPC := &mockEpochRPCWithEpoch{epoch: 100}

		view, err := NewView(ViewConfig{
			Logger:                 slog.New(slog.NewTextHandler(os.Stderr, nil)),
			Clock:                  clockwork.NewFakeClock(),
			TelemetryRPC:           mockTelemetryRPC,
			EpochRPC:               mockEpochRPC,
			MaxConcurrency:         32,
			InternetLatencyAgentPK: solana.MustPublicKeyFromBase58("So11111111111111111111111111111111111111112"),
			InternetDataProviders:  []string{"test-provider"},
			DB:                     db,
			Serviceability:         svcView,
			RefreshInterval:        time.Second,
		})
		require.NoError(t, err)

		err = view.Refresh(ctx)
		require.NoError(t, err)

		// Verify samples were saved
		var sampleCount int
		err = db.QueryRow("SELECT COUNT(*) FROM dz_device_link_latency_samples").Scan(&sampleCount)
		require.NoError(t, err)
		require.Equal(t, 3, sampleCount)

		var circuitCode string
		var epoch, sampleIndex int64
		var timestampUs, rttUs int64
		err = db.QueryRow("SELECT circuit_code, epoch, sample_index, timestamp_us, rtt_us FROM dz_device_link_latency_samples ORDER BY sample_index LIMIT 1").Scan(&circuitCode, &epoch, &sampleIndex, &timestampUs, &rttUs)
		require.NoError(t, err)
		require.Contains(t, circuitCode, "DEV1")
		require.Contains(t, circuitCode, "DEV2")
		require.Equal(t, int64(100), epoch)
		require.Equal(t, int64(0), sampleIndex)
		require.Equal(t, int64(1_600_000_000), timestampUs)
		require.Equal(t, int64(5000), rttUs)
	})

	t.Run("reads data back from database correctly", func(t *testing.T) {
		t.Parallel()

		db, err := sql.Open("duckdb", "")
		require.NoError(t, err)
		defer db.Close()

		// Set up serviceability view
		devicePK1 := [32]byte{1, 2, 3, 4}
		devicePK2 := [32]byte{5, 6, 7, 8}
		linkPK := [32]byte{9, 10, 11, 12}
		contributorPK := [32]byte{13, 14, 15, 16}
		metroPK := [32]byte{17, 18, 19, 20}
		ownerPK := [32]byte{21, 22, 23, 24}
		publicIP1 := [4]byte{192, 168, 1, 1}
		publicIP2 := [4]byte{192, 168, 1, 2}
		tunnelNet := [5]byte{10, 0, 0, 0, 24}

		svcMockRPC := &MockServiceabilityRPC{
			getProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
				return &serviceability.ProgramData{
					Contributors: []serviceability.Contributor{
						{
							PubKey: contributorPK,
							Owner:  ownerPK,
							Code:   "CONTRIB",
						},
					},
					Devices: []serviceability.Device{
						{
							PubKey:            devicePK1,
							Owner:             ownerPK,
							Status:            serviceability.DeviceStatusActivated,
							DeviceType:        serviceability.DeviceDeviceTypeHybrid,
							Code:              "DEV1",
							PublicIp:          publicIP1,
							ContributorPubKey: contributorPK,
							ExchangePubKey:    metroPK,
						},
						{
							PubKey:            devicePK2,
							Owner:             ownerPK,
							Status:            serviceability.DeviceStatusActivated,
							DeviceType:        serviceability.DeviceDeviceTypeHybrid,
							Code:              "DEV2",
							PublicIp:          publicIP2,
							ContributorPubKey: contributorPK,
							ExchangePubKey:    metroPK,
						},
					},
					Links: []serviceability.Link{
						{
							PubKey:            linkPK,
							Owner:             ownerPK,
							Status:            serviceability.LinkStatusActivated,
							Code:              "LINK1",
							TunnelNet:         tunnelNet,
							ContributorPubKey: contributorPK,
							SideAPubKey:       devicePK1,
							SideZPubKey:       devicePK2,
							SideAIfaceName:    "eth0",
							SideZIfaceName:    "eth1",
							LinkType:          serviceability.LinkLinkTypeWAN,
							DelayNs:           1000000,
							JitterNs:          50000,
						},
					},
				}, nil
			},
		}

		svcView, err := dzsvc.NewView(dzsvc.ViewConfig{
			Logger:            slog.New(slog.NewTextHandler(os.Stderr, nil)),
			Clock:             clockwork.NewFakeClock(),
			ServiceabilityRPC: svcMockRPC,
			RefreshInterval:   time.Second,
			DB:                db,
		})
		require.NoError(t, err)

		ctx := context.Background()
		err = svcView.Refresh(ctx)
		require.NoError(t, err)

		// Verify we can read devices back by querying the database directly
		var deviceCount int
		err = db.QueryRow("SELECT COUNT(*) FROM dz_devices").Scan(&deviceCount)
		require.NoError(t, err)
		require.Equal(t, 2, deviceCount)

		// Set up telemetry view and verify it can read from DB
		mockTelemetryRPC := &mockTelemetryRPC{}
		mockEpochRPC := &mockEpochRPC{}

		view, err := NewView(ViewConfig{
			Logger:                 slog.New(slog.NewTextHandler(os.Stderr, nil)),
			Clock:                  clockwork.NewFakeClock(),
			TelemetryRPC:           mockTelemetryRPC,
			EpochRPC:               mockEpochRPC,
			MaxConcurrency:         32,
			InternetLatencyAgentPK: solana.MustPublicKeyFromBase58("So11111111111111111111111111111111111111112"),
			InternetDataProviders:  []string{"test-provider"},
			DB:                     db,
			Serviceability:         svcView,
			RefreshInterval:        time.Second,
		})
		require.NoError(t, err)

		// Verify telemetry view can read devices from DB
		devicesFromTelemetry, err := view.getDevicesFromDB()
		require.NoError(t, err)
		require.Len(t, devicesFromTelemetry, 2)
		require.Equal(t, "DEV1", devicesFromTelemetry[0].Code)
		require.Equal(t, "DEV2", devicesFromTelemetry[1].Code)

		// Verify telemetry view can read links from DB
		linksFromTelemetry, err := view.getLinksFromDB()
		require.NoError(t, err)
		require.Len(t, linksFromTelemetry, 1)
		require.Equal(t, "LINK1", linksFromTelemetry[0].Code)

		// Verify telemetry view can read contributors from DB
		contributorsFromTelemetry, err := view.getContributorsFromDB()
		require.NoError(t, err)
		require.Len(t, contributorsFromTelemetry, 1)
		require.Equal(t, "CONTRIB", contributorsFromTelemetry[0].Code)
	})
}

type mockTelemetryRPCWithSamples struct {
	samples map[string]*telemetry.DeviceLatencySamples
}

func (m *mockTelemetryRPCWithSamples) GetDeviceLatencySamples(ctx context.Context, originDevicePK, targetDevicePK, linkPK solana.PublicKey, epoch uint64) (*telemetry.DeviceLatencySamples, error) {
	key := key(originDevicePK, targetDevicePK, linkPK, epoch)
	if samples, ok := m.samples[key]; ok {
		return samples, nil
	}
	return nil, telemetry.ErrAccountNotFound
}

func (m *mockTelemetryRPCWithSamples) GetInternetLatencySamples(ctx context.Context, dataProviderName string, originLocationPK, targetLocationPK, agentPK solana.PublicKey, epoch uint64) (*telemetry.InternetLatencySamples, error) {
	return nil, telemetry.ErrAccountNotFound
}

type mockEpochRPCWithEpoch struct {
	epoch uint64
}

func (m *mockEpochRPCWithEpoch) GetEpochInfo(ctx context.Context, commitment solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
	return &solanarpc.GetEpochInfoResult{
		Epoch: m.epoch,
	}, nil
}

func key(origin, target, link solana.PublicKey, epoch uint64) string {
	return fmt.Sprintf("%s:%s:%s:%d", origin.String(), target.String(), link.String(), epoch)
}
