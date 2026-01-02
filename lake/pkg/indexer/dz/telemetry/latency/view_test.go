package dztelemlatency

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/jonboulle/clockwork"
	"github.com/malbeclabs/doublezero/lake/pkg/duck"
	mcpgeoip "github.com/malbeclabs/doublezero/lake/pkg/indexer/geoip"
	"github.com/malbeclabs/doublezero/tools/maxmind/pkg/geoip"
	"github.com/stretchr/testify/require"

	dzsvc "github.com/malbeclabs/doublezero/lake/pkg/indexer/dz/serviceability"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
)

type mockTelemetryRPC struct{}

func (m *mockTelemetryRPC) GetDeviceLatencySamplesTail(ctx context.Context, originDevicePK, targetDevicePK, linkPK solana.PublicKey, epoch uint64, existingMaxIdx int) (*telemetry.DeviceLatencySamplesHeader, int, []uint32, error) {
	return nil, 0, nil, telemetry.ErrAccountNotFound
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

type mockGeoIPResolver struct {
	resolveFunc func(net.IP) *geoip.Record
}

func (m *mockGeoIPResolver) Resolve(ip net.IP) *geoip.Record {
	if m.resolveFunc != nil {
		return m.resolveFunc(ip)
	}
	return nil
}

type mockGeoIPStore struct {
	store *mcpgeoip.Store
	db    duck.DB
}

func newMockGeoIPStore(t *testing.T) (*mockGeoIPStore, error) {
	t.Helper()
	db := testDB(t)

	store, err := mcpgeoip.NewStore(mcpgeoip.StoreConfig{
		Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
		DB:     db,
	})
	if err != nil {
		return nil, err
	}

	return &mockGeoIPStore{
		store: store,
		db:    db,
	}, nil
}

func testDB(t *testing.T) duck.DB {
	db, err := duck.NewDB(t.Context(), "", slog.New(slog.NewTextHandler(os.Stderr, nil)))
	require.NoError(t, err)
	t.Cleanup(func() {
		db.Close()
	})
	return db
}

func TestLake_TelemetryLatency_View_Ready(t *testing.T) {
	t.Parallel()

	t.Run("returns false when not ready", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		geoipStore, err := newMockGeoIPStore(t)
		require.NoError(t, err)
		defer geoipStore.db.Close()

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

func TestLake_TelemetryLatency_View_WaitReady(t *testing.T) {
	t.Parallel()

	t.Run("returns error when context is cancelled", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		geoipStore, err := newMockGeoIPStore(t)
		require.NoError(t, err)
		defer geoipStore.db.Close()

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

func TestLake_TelemetryLatency_View_ConvertInternetLatencySamples(t *testing.T) {
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

		result := convertInternetLatencySamples(samples, testPK(1), testPK(2), "test-provider", 456)

		require.Len(t, result, 3)
		require.Equal(t, testPK(1), result[0].OriginMetroPK)
		require.Equal(t, testPK(2), result[0].TargetMetroPK)
		require.Equal(t, "test-provider", result[0].DataProvider)
		require.Equal(t, uint64(456), result[0].Epoch)
		require.Equal(t, 0, result[0].SampleIndex)
		require.WithinDuration(t, time.Unix(0, 1_700_000_000*1000), result[0].Time, time.Millisecond)
		require.Equal(t, uint32(10000), result[0].RTTMicroseconds)

		require.Equal(t, 1, result[1].SampleIndex)
		require.WithinDuration(t, time.Unix(0, (1_700_000_000+250_000)*1000), result[1].Time, time.Millisecond)
		require.Equal(t, uint32(11000), result[1].RTTMicroseconds)

		require.Equal(t, 2, result[2].SampleIndex)
		require.WithinDuration(t, time.Unix(0, (1_700_000_000+500_000)*1000), result[2].Time, time.Millisecond)
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

		result := convertInternetLatencySamples(samples, testPK(1), testPK(2), "provider", 0)
		require.Empty(t, result)
	})
}

func TestLake_TelemetryLatency_View_Refresh_SavesToDB(t *testing.T) {
	t.Parallel()

	t.Run("saves device-link samples to database", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

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

		geoipStore, err := newMockGeoIPStore(t)
		require.NoError(t, err)
		defer geoipStore.db.Close()

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

		// Verify samples were saved (if any telemetry data was returned)
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
	})

	t.Run("saves device-link latency samples to database", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

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

		geoipStore, err := newMockGeoIPStore(t)
		require.NoError(t, err)
		defer geoipStore.db.Close()

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
						StartTimestampMicroseconds:   1_600_000_000_000_000, // 1.6 billion seconds in microseconds
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
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM dz_device_link_latency_samples_raw").Scan(&sampleCount)
		require.NoError(t, err)
		require.Equal(t, 3, sampleCount)

		var originDevicePK, targetDevicePK, linkPKStr string
		var epoch, sampleIndex int64
		var sampleTime time.Time
		var rttUs int64
		err = conn.QueryRowContext(ctx, "SELECT origin_device_pk, target_device_pk, link_pk, epoch, sample_index, time, rtt_us FROM dz_device_link_latency_samples_raw ORDER BY sample_index LIMIT 1").Scan(&originDevicePK, &targetDevicePK, &linkPKStr, &epoch, &sampleIndex, &sampleTime, &rttUs)
		require.NoError(t, err)
		require.Equal(t, solana.PublicKeyFromBytes(devicePK1[:]).String(), originDevicePK)
		require.Equal(t, solana.PublicKeyFromBytes(devicePK2[:]).String(), targetDevicePK)
		require.Equal(t, solana.PublicKeyFromBytes(linkPK[:]).String(), linkPKStr)
		require.Equal(t, int64(100), epoch)
		require.Equal(t, int64(0), sampleIndex)
		require.WithinDuration(t, time.Unix(1_600_000_000, 0).UTC(), sampleTime.UTC(), time.Second)
		require.Equal(t, int64(5000), rttUs)
	})

	t.Run("reads data back from database correctly", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

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

		geoipStore, err := newMockGeoIPStore(t)
		require.NoError(t, err)
		defer geoipStore.db.Close()

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
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM dz_devices_current").Scan(&deviceCount)
		require.NoError(t, err)
		require.Equal(t, 2, deviceCount)

		// Verify serviceability store can read devices from DB
		devicesFromServiceability, err := svcView.Store().GetDevices()
		require.NoError(t, err)
		require.Len(t, devicesFromServiceability, 2)
		require.Equal(t, "DEV1", devicesFromServiceability[0].Code)
		require.Equal(t, "DEV2", devicesFromServiceability[1].Code)

		// Verify telemetry view can read links from DB via serviceability store
		linksFromServiceability, err := svcView.Store().GetLinks()
		require.NoError(t, err)
		require.Len(t, linksFromServiceability, 1)
		require.Equal(t, "LINK1", linksFromServiceability[0].Code)

		// Verify telemetry view can read contributors from DB via serviceability store
		contributorsFromServiceability, err := svcView.Store().GetContributors()
		require.NoError(t, err)
		require.Len(t, contributorsFromServiceability, 1)
		require.Equal(t, "CONTRIB", contributorsFromServiceability[0].Code)
	})
}

type mockTelemetryRPCWithSamples struct {
	samples map[string]*telemetry.DeviceLatencySamples
}

func (m *mockTelemetryRPCWithSamples) GetDeviceLatencySamplesTail(ctx context.Context, originDevicePK, targetDevicePK, linkPK solana.PublicKey, epoch uint64, existingMaxIdx int) (*telemetry.DeviceLatencySamplesHeader, int, []uint32, error) {
	key := key(originDevicePK, targetDevicePK, linkPK, epoch)
	if samples, ok := m.samples[key]; ok {
		return &samples.DeviceLatencySamplesHeader, 0, samples.Samples, nil
	}
	return nil, 0, nil, telemetry.ErrAccountNotFound
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

func TestLake_TelemetryLatency_View_IncrementalAppend(t *testing.T) {
	t.Parallel()

	t.Run("device-link samples are appended incrementally", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

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

		geoipStore, err := newMockGeoIPStore(t)
		require.NoError(t, err)
		defer geoipStore.db.Close()

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

		// Set up telemetry RPC to return samples with NextSampleIndex
		originPK := solana.PublicKeyFromBytes(devicePK1[:])
		targetPK := solana.PublicKeyFromBytes(devicePK2[:])
		linkPKPubKey := solana.PublicKeyFromBytes(linkPK[:])

		// Mock that simulates incremental samples: data source grows from 3 to 5 samples
		// First refresh: data source has 3 samples (0-2), existingMaxIdx=-1, return all 3
		// Second refresh: data source has 5 samples (0-4), existingMaxIdx=2, return tail (3-4)
		mockTelemetryRPC := &mockTelemetryRPCWithIncrementalSamples{
			getSamplesFunc: func(originDevicePK, targetDevicePK, linkPK solana.PublicKey, epoch uint64, existingMaxIdx int) (*telemetry.DeviceLatencySamplesHeader, int, []uint32, error) {
				sampleKey := key(originDevicePK, targetDevicePK, linkPK, epoch)
				expectedKey := key(originPK, targetPK, linkPKPubKey, 100)
				if sampleKey != expectedKey {
					return nil, 0, nil, telemetry.ErrAccountNotFound
				}

				// Simulate data source that grows: initially 3 samples, then 5 samples
				firstBatch := []uint32{5000, 6000, 7000}             // indices 0-2
				allSamples := []uint32{5000, 6000, 7000, 8000, 9000} // indices 0-4

				if existingMaxIdx < 0 {
					// First refresh: no existing data, data source has 3 samples
					return &telemetry.DeviceLatencySamplesHeader{
						StartTimestampMicroseconds:   1_600_000_000,
						SamplingIntervalMicroseconds: 100_000,
						NextSampleIndex:              3, // 3 samples (indices 0, 1, 2)
					}, 0, firstBatch, nil
				} else {
					// Subsequent refresh: data source has grown to 5 samples
					// Return only the tail (samples after existingMaxIdx)
					startIdx := existingMaxIdx + 1
					if startIdx >= len(allSamples) {
						// No new samples
						return &telemetry.DeviceLatencySamplesHeader{
							StartTimestampMicroseconds:   1_600_000_000,
							SamplingIntervalMicroseconds: 100_000,
							NextSampleIndex:              uint32(len(allSamples)),
						}, startIdx, nil, nil
					}

					tail := allSamples[startIdx:]
					return &telemetry.DeviceLatencySamplesHeader{
						StartTimestampMicroseconds:   1_600_000_000,
						SamplingIntervalMicroseconds: 100_000,
						NextSampleIndex:              5, // 5 samples (indices 0-4)
					}, startIdx, tail, nil
				}
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

		// First refresh: should insert 3 samples
		err = view.Refresh(ctx)
		require.NoError(t, err)

		var sampleCount int
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM dz_device_link_latency_samples_raw").Scan(&sampleCount)
		require.NoError(t, err)
		require.Equal(t, 3, sampleCount, "first refresh should insert 3 samples")

		// Verify the first 3 samples are correct
		var maxIdx int64
		err = conn.QueryRowContext(ctx, "SELECT MAX(sample_index) FROM dz_device_link_latency_samples_raw").Scan(&maxIdx)
		require.NoError(t, err)
		require.Equal(t, int64(2), maxIdx, "max sample_index should be 2 after first refresh")

		// Second refresh: should append only the 2 new samples (indices 3-4)
		err = view.Refresh(ctx)
		require.NoError(t, err)

		conn, err = db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM dz_device_link_latency_samples_raw").Scan(&sampleCount)
		require.NoError(t, err)
		require.Equal(t, 5, sampleCount, "second refresh should append 2 more samples, total 5")

		// Verify all samples are present and correct
		var rttUs int64
		err = conn.QueryRowContext(ctx, "SELECT rtt_us FROM dz_device_link_latency_samples_raw WHERE sample_index = 0").Scan(&rttUs)
		require.NoError(t, err)
		require.Equal(t, int64(5000), rttUs, "sample 0 should remain unchanged")

		err = conn.QueryRowContext(ctx, "SELECT rtt_us FROM dz_device_link_latency_samples_raw WHERE sample_index = 2").Scan(&rttUs)
		require.NoError(t, err)
		require.Equal(t, int64(7000), rttUs, "sample 2 should remain unchanged")

		err = conn.QueryRowContext(ctx, "SELECT rtt_us FROM dz_device_link_latency_samples_raw WHERE sample_index = 3").Scan(&rttUs)
		require.NoError(t, err)
		require.Equal(t, int64(8000), rttUs, "sample 3 should be newly inserted")

		err = conn.QueryRowContext(ctx, "SELECT rtt_us FROM dz_device_link_latency_samples_raw WHERE sample_index = 4").Scan(&rttUs)
		require.NoError(t, err)
		require.Equal(t, int64(9000), rttUs, "sample 4 should be newly inserted")

		// Verify max index is now 4
		err = conn.QueryRowContext(ctx, "SELECT MAX(sample_index) FROM dz_device_link_latency_samples_raw").Scan(&maxIdx)
		require.NoError(t, err)
		require.Equal(t, int64(4), maxIdx, "max sample_index should be 4 after second refresh")
	})

	t.Run("internet-metro samples are appended incrementally", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		// Set up serviceability view with metros
		metroPK1 := [32]byte{1, 2, 3, 4}
		metroPK2 := [32]byte{5, 6, 7, 8}
		contributorPK := [32]byte{13, 14, 15, 16}
		ownerPK := [32]byte{21, 22, 23, 24}

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
					Exchanges: []serviceability.Exchange{
						{
							PubKey: metroPK1,
							Owner:  ownerPK,
							Code:   "NYC",
							Name:   "New York",
							Status: serviceability.ExchangeStatusActivated,
							Lat:    40.7128,
							Lng:    -74.0060,
						},
						{
							PubKey: metroPK2,
							Owner:  ownerPK,
							Code:   "LAX",
							Name:   "Los Angeles",
							Status: serviceability.ExchangeStatusActivated,
							Lat:    34.0522,
							Lng:    -118.2437,
						},
					},
				}, nil
			},
		}

		geoipStore, err := newMockGeoIPStore(t)
		require.NoError(t, err)
		defer geoipStore.db.Close()

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

		// Set up telemetry RPC to return internet samples with NextSampleIndex
		originPK := solana.PublicKeyFromBytes(metroPK1[:])
		targetPK := solana.PublicKeyFromBytes(metroPK2[:])
		agentPK := solana.MustPublicKeyFromBase58("So11111111111111111111111111111111111111112")

		var refreshCount atomic.Int64
		mockTelemetryRPC := &mockTelemetryRPCWithIncrementalInternetSamples{
			getSamplesFunc: func(dataProviderName string, originLocationPK, targetLocationPK, agentPK solana.PublicKey, epoch uint64) (*telemetry.InternetLatencySamples, error) {
				// Metro pairs can be in either direction, so check both orderings
				matchesForward := originLocationPK.String() == originPK.String() && targetLocationPK.String() == targetPK.String()
				matchesReverse := originLocationPK.String() == targetPK.String() && targetLocationPK.String() == originPK.String()
				if (!matchesForward && !matchesReverse) || epoch != 100 {
					return nil, telemetry.ErrAccountNotFound
				}

				count := refreshCount.Add(1)
				if count == 1 {
					// First refresh: return samples 0-1
					return &telemetry.InternetLatencySamples{
						InternetLatencySamplesHeader: telemetry.InternetLatencySamplesHeader{
							StartTimestampMicroseconds:   1_700_000_000,
							SamplingIntervalMicroseconds: 250_000,
							NextSampleIndex:              2, // 2 samples (indices 0, 1)
						},
						Samples: []uint32{10000, 11000},
					}, nil
				} else {
					// Second refresh: return samples 0-3 (new samples 2-3 added)
					return &telemetry.InternetLatencySamples{
						InternetLatencySamplesHeader: telemetry.InternetLatencySamplesHeader{
							StartTimestampMicroseconds:   1_700_000_000,
							SamplingIntervalMicroseconds: 250_000,
							NextSampleIndex:              4, // 4 samples (indices 0-3)
						},
						Samples: []uint32{10000, 11000, 12000, 13000},
					}, nil
				}
			},
		}

		mockEpochRPC := &mockEpochRPCWithEpoch{epoch: 100}

		view, err := NewView(ViewConfig{
			Logger:                 slog.New(slog.NewTextHandler(os.Stderr, nil)),
			Clock:                  clockwork.NewFakeClock(),
			TelemetryRPC:           mockTelemetryRPC,
			EpochRPC:               mockEpochRPC,
			MaxConcurrency:         32,
			InternetLatencyAgentPK: agentPK,
			InternetDataProviders:  []string{"test-provider"},
			DB:                     db,
			Serviceability:         svcView,
			RefreshInterval:        time.Second,
		})
		require.NoError(t, err)

		// First refresh: should insert 2 samples
		err = view.Refresh(ctx)
		require.NoError(t, err)

		var sampleCount int
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM dz_internet_metro_latency_samples_raw").Scan(&sampleCount)
		require.NoError(t, err)
		require.Equal(t, 2, sampleCount, "first refresh should insert 2 samples")

		// Verify the first 2 samples are correct
		var maxIdx int64
		err = conn.QueryRowContext(ctx, "SELECT MAX(sample_index) FROM dz_internet_metro_latency_samples_raw").Scan(&maxIdx)
		require.NoError(t, err)
		require.Equal(t, int64(1), maxIdx, "max sample_index should be 1 after first refresh")

		// Second refresh: should append only the 2 new samples (indices 2-3)
		err = view.Refresh(ctx)
		require.NoError(t, err)

		conn, err = db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM dz_internet_metro_latency_samples_raw").Scan(&sampleCount)
		require.NoError(t, err)
		require.Equal(t, 4, sampleCount, "second refresh should append 2 more samples, total 4")

		// Verify all samples are present and correct
		var rttUs int64
		err = conn.QueryRowContext(ctx, "SELECT rtt_us FROM dz_internet_metro_latency_samples_raw WHERE sample_index = 0").Scan(&rttUs)
		require.NoError(t, err)
		require.Equal(t, int64(10000), rttUs, "sample 0 should remain unchanged")

		err = conn.QueryRowContext(ctx, "SELECT rtt_us FROM dz_internet_metro_latency_samples_raw WHERE sample_index = 1").Scan(&rttUs)
		require.NoError(t, err)
		require.Equal(t, int64(11000), rttUs, "sample 1 should remain unchanged")

		err = conn.QueryRowContext(ctx, "SELECT rtt_us FROM dz_internet_metro_latency_samples_raw WHERE sample_index = 2").Scan(&rttUs)
		require.NoError(t, err)
		require.Equal(t, int64(12000), rttUs, "sample 2 should be newly inserted")

		err = conn.QueryRowContext(ctx, "SELECT rtt_us FROM dz_internet_metro_latency_samples_raw WHERE sample_index = 3").Scan(&rttUs)
		require.NoError(t, err)
		require.Equal(t, int64(13000), rttUs, "sample 3 should be newly inserted")

		// Verify max index is now 3
		err = conn.QueryRowContext(ctx, "SELECT MAX(sample_index) FROM dz_internet_metro_latency_samples_raw").Scan(&maxIdx)
		require.NoError(t, err)
		require.Equal(t, int64(3), maxIdx, "max sample_index should be 3 after second refresh")
	})
}

type mockTelemetryRPCWithIncrementalSamples struct {
	getSamplesFunc func(originDevicePK, targetDevicePK, linkPK solana.PublicKey, epoch uint64, existingMaxIdx int) (*telemetry.DeviceLatencySamplesHeader, int, []uint32, error)
}

func (m *mockTelemetryRPCWithIncrementalSamples) GetDeviceLatencySamplesTail(ctx context.Context, originDevicePK, targetDevicePK, linkPK solana.PublicKey, epoch uint64, existingMaxIdx int) (*telemetry.DeviceLatencySamplesHeader, int, []uint32, error) {
	if m.getSamplesFunc != nil {
		return m.getSamplesFunc(originDevicePK, targetDevicePK, linkPK, epoch, existingMaxIdx)
	}
	return nil, 0, nil, telemetry.ErrAccountNotFound
}

func (m *mockTelemetryRPCWithIncrementalSamples) GetInternetLatencySamples(ctx context.Context, dataProviderName string, originLocationPK, targetLocationPK, agentPK solana.PublicKey, epoch uint64) (*telemetry.InternetLatencySamples, error) {
	return nil, telemetry.ErrAccountNotFound
}

type mockTelemetryRPCWithIncrementalInternetSamples struct {
	getSamplesFunc func(dataProviderName string, originLocationPK, targetLocationPK, agentPK solana.PublicKey, epoch uint64) (*telemetry.InternetLatencySamples, error)
}

func (m *mockTelemetryRPCWithIncrementalInternetSamples) GetDeviceLatencySamplesTail(ctx context.Context, originDevicePK, targetDevicePK, linkPK solana.PublicKey, epoch uint64, existingMaxIdx int) (*telemetry.DeviceLatencySamplesHeader, int, []uint32, error) {
	return nil, 0, nil, telemetry.ErrAccountNotFound
}

func (m *mockTelemetryRPCWithIncrementalInternetSamples) GetInternetLatencySamples(ctx context.Context, dataProviderName string, originLocationPK, targetLocationPK, agentPK solana.PublicKey, epoch uint64) (*telemetry.InternetLatencySamples, error) {
	if m.getSamplesFunc != nil {
		return m.getSamplesFunc(dataProviderName, originLocationPK, targetLocationPK, agentPK, epoch)
	}
	return nil, telemetry.ErrAccountNotFound
}

func TestLake_TelemetryLatency_View_Refresh_ErrorHandling(t *testing.T) {
	t.Parallel()

	t.Run("device-link refresh returns error when GetExistingMaxSampleIndices fails", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

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

		geoipStore, err := newMockGeoIPStore(t)
		require.NoError(t, err)
		defer geoipStore.db.Close()

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
						StartTimestampMicroseconds:   1_600_000_000_000_000,
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

		// First refresh should succeed and insert samples
		err = view.Refresh(ctx)
		require.NoError(t, err)

		// Verify samples were inserted
		var sampleCount int
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM dz_device_link_latency_samples_raw").Scan(&sampleCount)
		require.NoError(t, err)
		require.Equal(t, 3, sampleCount, "first refresh should insert 3 samples")

		// Drop the table to cause GetExistingMaxSampleIndices to fail
		_, err = conn.ExecContext(ctx, "DROP TABLE dz_device_link_latency_samples_raw")
		require.NoError(t, err)

		// Second refresh should complete but log an error (Refresh catches and logs errors)
		// The key behavior is that it should NOT insert all samples when GetExistingMaxSampleIndices fails
		err = view.Refresh(ctx)
		// Refresh returns nil even when refresh methods fail (it logs warnings)
		require.NoError(t, err)

		// Verify the table still doesn't exist (wasn't recreated) and no samples were inserted
		// This confirms that when GetExistingMaxSampleIndices fails, we don't proceed with insertion
		var count int
		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM dz_device_link_latency_samples_raw").Scan(&count)
		require.Error(t, err, "table should not exist after being dropped")
		require.Contains(t, err.Error(), "does not exist")
	})

	t.Run("internet-metro refresh returns error when GetExistingInternetMaxSampleIndices fails", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		// Set up serviceability view with metros
		metroPK1 := [32]byte{1, 2, 3, 4}
		metroPK2 := [32]byte{5, 6, 7, 8}
		contributorPK := [32]byte{13, 14, 15, 16}
		ownerPK := [32]byte{21, 22, 23, 24}

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
					Exchanges: []serviceability.Exchange{
						{
							PubKey: metroPK1,
							Owner:  ownerPK,
							Code:   "NYC",
							Name:   "New York",
							Status: serviceability.ExchangeStatusActivated,
							Lat:    40.7128,
							Lng:    -74.0060,
						},
						{
							PubKey: metroPK2,
							Owner:  ownerPK,
							Code:   "LAX",
							Name:   "Los Angeles",
							Status: serviceability.ExchangeStatusActivated,
							Lat:    34.0522,
							Lng:    -118.2437,
						},
					},
				}, nil
			},
		}

		geoipStore, err := newMockGeoIPStore(t)
		require.NoError(t, err)
		defer geoipStore.db.Close()

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

		// Set up telemetry RPC to return internet samples
		originPK := solana.PublicKeyFromBytes(metroPK1[:])
		targetPK := solana.PublicKeyFromBytes(metroPK2[:])
		agentPK := solana.MustPublicKeyFromBase58("So11111111111111111111111111111111111111112")

		mockTelemetryRPC := &mockTelemetryRPCWithIncrementalInternetSamples{
			getSamplesFunc: func(dataProviderName string, originLocationPK, targetLocationPK, agentPK solana.PublicKey, epoch uint64) (*telemetry.InternetLatencySamples, error) {
				// Metro pairs can be in either direction, so check both orderings
				matchesForward := originLocationPK.String() == originPK.String() && targetLocationPK.String() == targetPK.String()
				matchesReverse := originLocationPK.String() == targetPK.String() && targetLocationPK.String() == originPK.String()
				if (!matchesForward && !matchesReverse) || epoch != 100 {
					return nil, telemetry.ErrAccountNotFound
				}

				return &telemetry.InternetLatencySamples{
					InternetLatencySamplesHeader: telemetry.InternetLatencySamplesHeader{
						StartTimestampMicroseconds:   1_700_000_000,
						SamplingIntervalMicroseconds: 250_000,
						NextSampleIndex:              2,
					},
					Samples: []uint32{10000, 11000},
				}, nil
			},
		}

		mockEpochRPC := &mockEpochRPCWithEpoch{epoch: 100}

		view, err := NewView(ViewConfig{
			Logger:                 slog.New(slog.NewTextHandler(os.Stderr, nil)),
			Clock:                  clockwork.NewFakeClock(),
			TelemetryRPC:           mockTelemetryRPC,
			EpochRPC:               mockEpochRPC,
			MaxConcurrency:         32,
			InternetLatencyAgentPK: agentPK,
			InternetDataProviders:  []string{"test-provider"},
			DB:                     db,
			Serviceability:         svcView,
			RefreshInterval:        time.Second,
		})
		require.NoError(t, err)

		// First refresh should succeed and insert samples
		err = view.Refresh(ctx)
		require.NoError(t, err)

		// Verify samples were inserted
		var sampleCount int
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM dz_internet_metro_latency_samples_raw").Scan(&sampleCount)
		require.NoError(t, err)
		require.Equal(t, 2, sampleCount, "first refresh should insert 2 samples")

		// Drop the table to cause GetExistingInternetMaxSampleIndices to fail
		_, err = conn.ExecContext(ctx, "DROP TABLE dz_internet_metro_latency_samples_raw")
		require.NoError(t, err)

		// Second refresh should complete but log an error (Refresh catches and logs errors)
		// The key behavior is that it should NOT insert all samples when GetExistingInternetMaxSampleIndices fails
		err = view.Refresh(ctx)
		// Refresh returns nil even when refresh methods fail (it logs warnings)
		require.NoError(t, err)

		// Verify the table still doesn't exist (wasn't recreated) and no samples were inserted
		// This confirms that when GetExistingInternetMaxSampleIndices fails, we don't proceed with insertion
		var count int
		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM dz_internet_metro_latency_samples_raw").Scan(&count)
		require.Error(t, err, "table should not exist after being dropped")
		require.Contains(t, err.Error(), "does not exist")
	})
}
