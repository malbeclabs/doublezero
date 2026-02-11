package geoprobe

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createTestProgramData(devicePK, locationPK solana.PublicKey, lat, lng float64) *serviceability.ProgramData {
	var devicePKBytes, locationPKBytes [32]byte
	copy(devicePKBytes[:], devicePK[:])
	copy(locationPKBytes[:], locationPK[:])

	return &serviceability.ProgramData{
		Devices: []serviceability.Device{
			{
				PubKey:         devicePKBytes,
				LocationPubKey: locationPKBytes,
				Code:           "test-device",
			},
		},
		Locations: []serviceability.Location{
			{
				PubKey: locationPKBytes,
				Lat:    lat,
				Lng:    lng,
				Code:   "test-location",
			},
		},
	}
}

func TestNewPublisher(t *testing.T) {
	t.Parallel()

	keypair := solana.NewWallet().PrivateKey
	devicePK := solana.NewWallet().PublicKey()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	mockServiceability := &mockServiceabilityClient{}
	mockRPC := &mockRPCClient{}

	t.Run("valid config", func(t *testing.T) {
		cfg := &PublisherConfig{
			Logger:               logger,
			Keypair:              keypair,
			LocalDevicePK:        devicePK,
			ServiceabilityClient: mockServiceability,
			RPCClient:            mockRPC,
		}

		pub, err := NewPublisher(cfg)
		require.NoError(t, err)
		require.NotNil(t, pub)
		assert.NotNil(t, pub.signer)
		assert.NotNil(t, pub.conns)
	})

	t.Run("missing logger", func(t *testing.T) {
		cfg := &PublisherConfig{
			Keypair:              keypair,
			LocalDevicePK:        devicePK,
			ServiceabilityClient: mockServiceability,
			RPCClient:            mockRPC,
		}

		pub, err := NewPublisher(cfg)
		require.Error(t, err)
		require.Nil(t, pub)
		assert.Contains(t, err.Error(), "logger")
	})

	t.Run("missing keypair", func(t *testing.T) {
		cfg := &PublisherConfig{
			Logger:               logger,
			LocalDevicePK:        devicePK,
			ServiceabilityClient: mockServiceability,
			RPCClient:            mockRPC,
		}

		pub, err := NewPublisher(cfg)
		require.Error(t, err)
		require.Nil(t, pub)
		assert.Contains(t, err.Error(), "keypair")
	})

	t.Run("missing device pubkey", func(t *testing.T) {
		cfg := &PublisherConfig{
			Logger:               logger,
			Keypair:              keypair,
			ServiceabilityClient: mockServiceability,
			RPCClient:            mockRPC,
		}

		pub, err := NewPublisher(cfg)
		require.Error(t, err)
		require.Nil(t, pub)
		assert.Contains(t, err.Error(), "device")
	})

	t.Run("missing serviceability client", func(t *testing.T) {
		cfg := &PublisherConfig{
			Logger:        logger,
			Keypair:       keypair,
			LocalDevicePK: devicePK,
			RPCClient:     mockRPC,
		}

		pub, err := NewPublisher(cfg)
		require.Error(t, err)
		require.Nil(t, pub)
		assert.Contains(t, err.Error(), "serviceability")
	})

	t.Run("missing rpc client", func(t *testing.T) {
		cfg := &PublisherConfig{
			Logger:               logger,
			Keypair:              keypair,
			LocalDevicePK:        devicePK,
			ServiceabilityClient: mockServiceability,
		}

		pub, err := NewPublisher(cfg)
		require.Error(t, err)
		require.Nil(t, pub)
		assert.Contains(t, err.Error(), "rpc")
	})
}

func TestPublisher_GetLatLng(t *testing.T) {
	t.Parallel()

	keypair := solana.NewWallet().PrivateKey
	devicePK := solana.NewWallet().PublicKey()
	locationPK := solana.NewWallet().PublicKey()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	mockServiceability := &mockServiceabilityClient{
		programData: createTestProgramData(devicePK, locationPK, 52.3676, 4.9041),
	}
	mockRPC := &mockRPCClient{slot: 12345}

	cfg := &PublisherConfig{
		Logger:               logger,
		Keypair:              keypair,
		LocalDevicePK:        devicePK,
		ServiceabilityClient: mockServiceability,
		RPCClient:            mockRPC,
	}

	pub, err := NewPublisher(cfg)
	require.NoError(t, err)

	ctx := context.Background()

	t.Run("fetches lat/lng from serviceability", func(t *testing.T) {
		lat, lng, err := pub.getLatLng(ctx)
		require.NoError(t, err)
		assert.Equal(t, 52.3676, lat)
		assert.Equal(t, 4.9041, lng)
		assert.False(t, pub.latLngCachedAt.IsZero())
	})

	t.Run("uses cache within TTL", func(t *testing.T) {
		mockServiceability.setProgramData(createTestProgramData(devicePK, locationPK, 1.0, 2.0))

		lat, lng, err := pub.getLatLng(ctx)
		require.NoError(t, err)
		assert.Equal(t, 52.3676, lat)
		assert.Equal(t, 4.9041, lng)
	})
}

func TestPublisher_GetLatLng_CacheExpiry(t *testing.T) {
	t.Parallel()

	keypair := solana.NewWallet().PrivateKey
	devicePK := solana.NewWallet().PublicKey()
	locationPK := solana.NewWallet().PublicKey()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	mockServiceability := &mockServiceabilityClient{
		programData: createTestProgramData(devicePK, locationPK, 52.3676, 4.9041),
	}
	mockRPC := &mockRPCClient{slot: 12345}

	cfg := &PublisherConfig{
		Logger:               logger,
		Keypair:              keypair,
		LocalDevicePK:        devicePK,
		ServiceabilityClient: mockServiceability,
		RPCClient:            mockRPC,
	}

	pub, err := NewPublisher(cfg)
	require.NoError(t, err)

	ctx := context.Background()

	lat, lng, err := pub.getLatLng(ctx)
	require.NoError(t, err)
	assert.Equal(t, 52.3676, lat)
	assert.Equal(t, 4.9041, lng)

	pub.latLngMu.Lock()
	pub.latLngCachedAt = time.Now().Add(-25 * time.Hour)
	pub.latLngMu.Unlock()

	mockServiceability.setProgramData(createTestProgramData(devicePK, locationPK, 50.1109, 8.6821))

	lat, lng, err = pub.getLatLng(ctx)
	require.NoError(t, err)
	assert.Equal(t, 50.1109, lat)
	assert.Equal(t, 8.6821, lng)
}

func TestPublisher_GetLatLng_FallbackOnError(t *testing.T) {
	t.Parallel()

	keypair := solana.NewWallet().PrivateKey
	devicePK := solana.NewWallet().PublicKey()
	locationPK := solana.NewWallet().PublicKey()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	mockServiceability := &mockServiceabilityClient{
		programData: createTestProgramData(devicePK, locationPK, 52.3676, 4.9041),
	}
	mockRPC := &mockRPCClient{slot: 12345}

	cfg := &PublisherConfig{
		Logger:               logger,
		Keypair:              keypair,
		LocalDevicePK:        devicePK,
		ServiceabilityClient: mockServiceability,
		RPCClient:            mockRPC,
	}

	pub, err := NewPublisher(cfg)
	require.NoError(t, err)

	ctx := context.Background()

	lat, lng, err := pub.getLatLng(ctx)
	require.NoError(t, err)
	assert.Equal(t, 52.3676, lat)

	mockServiceability.setError(errors.New("network error"))

	lat, lng, err = pub.getLatLng(ctx)
	require.NoError(t, err)
	assert.Equal(t, 52.3676, lat)
	assert.Equal(t, 4.9041, lng)
}

func TestPublisher_GetLatLng_NoStaleCache(t *testing.T) {
	t.Parallel()

	keypair := solana.NewWallet().PrivateKey
	devicePK := solana.NewWallet().PublicKey()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	mockServiceability := &mockServiceabilityClient{
		err: errors.New("network error"),
	}
	mockRPC := &mockRPCClient{slot: 12345}

	cfg := &PublisherConfig{
		Logger:               logger,
		Keypair:              keypair,
		LocalDevicePK:        devicePK,
		ServiceabilityClient: mockServiceability,
		RPCClient:            mockRPC,
	}

	pub, err := NewPublisher(cfg)
	require.NoError(t, err)

	ctx := context.Background()

	_, _, err = pub.getLatLng(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get program data")
}

func TestPublisher_GetCurrentSlot(t *testing.T) {
	t.Parallel()

	keypair := solana.NewWallet().PrivateKey
	devicePK := solana.NewWallet().PublicKey()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	mockServiceability := &mockServiceabilityClient{}
	mockRPC := &mockRPCClient{slot: 12345}

	cfg := &PublisherConfig{
		Logger:               logger,
		Keypair:              keypair,
		LocalDevicePK:        devicePK,
		ServiceabilityClient: mockServiceability,
		RPCClient:            mockRPC,
	}

	pub, err := NewPublisher(cfg)
	require.NoError(t, err)

	ctx := context.Background()

	t.Run("fetches slot from RPC", func(t *testing.T) {
		slot, err := pub.getCurrentSlot(ctx)
		require.NoError(t, err)
		assert.Equal(t, uint64(12345), slot)
		assert.False(t, pub.slotCachedAt.IsZero())
	})

	t.Run("updates cache on success", func(t *testing.T) {
		mockRPC.setSlot(67890)

		slot, err := pub.getCurrentSlot(ctx)
		require.NoError(t, err)
		assert.Equal(t, uint64(67890), slot)
	})
}

func TestPublisher_GetCurrentSlot_FallbackWindow(t *testing.T) {
	t.Parallel()

	keypair := solana.NewWallet().PrivateKey
	devicePK := solana.NewWallet().PublicKey()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	mockServiceability := &mockServiceabilityClient{}
	mockRPC := &mockRPCClient{slot: 12345}

	cfg := &PublisherConfig{
		Logger:               logger,
		Keypair:              keypair,
		LocalDevicePK:        devicePK,
		ServiceabilityClient: mockServiceability,
		RPCClient:            mockRPC,
	}

	pub, err := NewPublisher(cfg)
	require.NoError(t, err)

	ctx := context.Background()

	slot, err := pub.getCurrentSlot(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(12345), slot)

	mockRPC.setError(errors.New("RPC error"))

	slot, err = pub.getCurrentSlot(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(12345), slot)
}

func TestPublisher_GetCurrentSlot_RejectStaleCache(t *testing.T) {
	t.Parallel()

	keypair := solana.NewWallet().PrivateKey
	devicePK := solana.NewWallet().PublicKey()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	mockServiceability := &mockServiceabilityClient{}
	mockRPC := &mockRPCClient{slot: 12345}

	cfg := &PublisherConfig{
		Logger:               logger,
		Keypair:              keypair,
		LocalDevicePK:        devicePK,
		ServiceabilityClient: mockServiceability,
		RPCClient:            mockRPC,
	}

	pub, err := NewPublisher(cfg)
	require.NoError(t, err)

	ctx := context.Background()

	slot, err := pub.getCurrentSlot(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(12345), slot)

	pub.slotMu.Lock()
	pub.slotCachedAt = time.Now().Add(-6 * time.Minute)
	pub.slotMu.Unlock()

	mockRPC.setError(errors.New("RPC error"))

	_, err = pub.getCurrentSlot(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get slot from RPC")
}

func TestPublisher_AddProbe(t *testing.T) {
	t.Parallel()

	keypair := solana.NewWallet().PrivateKey
	devicePK := solana.NewWallet().PublicKey()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	mockServiceability := &mockServiceabilityClient{}
	mockRPC := &mockRPCClient{}

	cfg := &PublisherConfig{
		Logger:               logger,
		Keypair:              keypair,
		LocalDevicePK:        devicePK,
		ServiceabilityClient: mockServiceability,
		RPCClient:            mockRPC,
	}

	pub, err := NewPublisher(cfg)
	require.NoError(t, err)

	addr := ProbeAddress{Host: "127.0.0.1", Port: 9999}

	t.Run("adds probe successfully", func(t *testing.T) {
		err := pub.AddProbe(context.Background(), addr)
		require.NoError(t, err)

		pub.connsMu.Lock()
		_, exists := pub.conns[addr.String()]
		pub.connsMu.Unlock()
		assert.True(t, exists)
	})

	t.Run("skips duplicate probe", func(t *testing.T) {
		err := pub.AddProbe(context.Background(), addr)
		require.NoError(t, err)
	})
}

func TestPublisher_RemoveProbe(t *testing.T) {
	t.Parallel()

	keypair := solana.NewWallet().PrivateKey
	devicePK := solana.NewWallet().PublicKey()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	mockServiceability := &mockServiceabilityClient{}
	mockRPC := &mockRPCClient{}

	cfg := &PublisherConfig{
		Logger:               logger,
		Keypair:              keypair,
		LocalDevicePK:        devicePK,
		ServiceabilityClient: mockServiceability,
		RPCClient:            mockRPC,
	}

	pub, err := NewPublisher(cfg)
	require.NoError(t, err)

	addr := ProbeAddress{Host: "127.0.0.1", Port: 9999}

	t.Run("removes existing probe", func(t *testing.T) {
		err := pub.AddProbe(context.Background(), addr)
		require.NoError(t, err)

		err = pub.RemoveProbe(addr)
		require.NoError(t, err)

		pub.connsMu.Lock()
		_, exists := pub.conns[addr.String()]
		pub.connsMu.Unlock()
		assert.False(t, exists)
	})

	t.Run("skips non-existent probe", func(t *testing.T) {
		err := pub.RemoveProbe(addr)
		require.NoError(t, err)
	})
}

func TestPublisher_Close(t *testing.T) {
	t.Parallel()

	keypair := solana.NewWallet().PrivateKey
	devicePK := solana.NewWallet().PublicKey()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	mockServiceability := &mockServiceabilityClient{}
	mockRPC := &mockRPCClient{}

	cfg := &PublisherConfig{
		Logger:               logger,
		Keypair:              keypair,
		LocalDevicePK:        devicePK,
		ServiceabilityClient: mockServiceability,
		RPCClient:            mockRPC,
	}

	pub, err := NewPublisher(cfg)
	require.NoError(t, err)

	addr1 := ProbeAddress{Host: "127.0.0.1", Port: 9999}
	addr2 := ProbeAddress{Host: "127.0.0.1", Port: 10000}

	err = pub.AddProbe(context.Background(), addr1)
	require.NoError(t, err)
	err = pub.AddProbe(context.Background(), addr2)
	require.NoError(t, err)

	err = pub.Close()
	require.NoError(t, err)

	pub.connsMu.Lock()
	assert.Len(t, pub.conns, 0)
	pub.connsMu.Unlock()
}

func TestPublisher_Publish(t *testing.T) {
	t.Parallel()

	keypair := solana.NewWallet().PrivateKey
	devicePK := solana.NewWallet().PublicKey()
	locationPK := solana.NewWallet().PublicKey()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	mockServiceability := &mockServiceabilityClient{
		programData: createTestProgramData(devicePK, locationPK, 52.3676, 4.9041),
	}
	mockRPC := &mockRPCClient{slot: 12345}

	cfg := &PublisherConfig{
		Logger:               logger,
		Keypair:              keypair,
		LocalDevicePK:        devicePK,
		ServiceabilityClient: mockServiceability,
		RPCClient:            mockRPC,
	}

	pub, err := NewPublisher(cfg)
	require.NoError(t, err)

	listener, err := NewUDPListener(0)
	require.NoError(t, err)
	defer listener.Close()

	localAddr := listener.LocalAddr().(*net.UDPAddr)
	probeAddr := ProbeAddress{Host: "127.0.0.1", Port: uint16(localAddr.Port)}

	err = pub.AddProbe(context.Background(), probeAddr)
	require.NoError(t, err)

	ctx := context.Background()

	rttData := map[ProbeAddress]uint64{
		probeAddr: 800000,
	}

	doneChan := make(chan struct{})
	go func() {
		offset, _, err := ReceiveOffset(listener)
		require.NoError(t, err)
		assert.Equal(t, uint64(12345), offset.MeasurementSlot)
		assert.Equal(t, 52.3676, offset.Lat)
		assert.Equal(t, 4.9041, offset.Lng)
		assert.Equal(t, uint64(800000), offset.MeasuredRttNs)
		assert.Equal(t, uint64(800000), offset.RttNs)
		assert.Equal(t, uint8(0), offset.NumReferences)

		err = VerifyOffset(offset)
		require.NoError(t, err)
		close(doneChan)
	}()

	err = pub.Publish(ctx, rttData)
	require.NoError(t, err)

	select {
	case <-doneChan:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for offset")
	}
}

func TestPublisher_Publish_EmptyRttData(t *testing.T) {
	t.Parallel()

	keypair := solana.NewWallet().PrivateKey
	devicePK := solana.NewWallet().PublicKey()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	mockServiceability := &mockServiceabilityClient{}
	mockRPC := &mockRPCClient{}

	cfg := &PublisherConfig{
		Logger:               logger,
		Keypair:              keypair,
		LocalDevicePK:        devicePK,
		ServiceabilityClient: mockServiceability,
		RPCClient:            mockRPC,
	}

	pub, err := NewPublisher(cfg)
	require.NoError(t, err)

	ctx := context.Background()

	err = pub.Publish(ctx, nil)
	require.NoError(t, err)

	err = pub.Publish(ctx, map[ProbeAddress]uint64{})
	require.NoError(t, err)
}

func TestPublisher_Publish_LatLngError(t *testing.T) {
	t.Parallel()

	keypair := solana.NewWallet().PrivateKey
	devicePK := solana.NewWallet().PublicKey()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	mockServiceability := &mockServiceabilityClient{
		err: errors.New("serviceability error"),
	}
	mockRPC := &mockRPCClient{slot: 12345}

	cfg := &PublisherConfig{
		Logger:               logger,
		Keypair:              keypair,
		LocalDevicePK:        devicePK,
		ServiceabilityClient: mockServiceability,
		RPCClient:            mockRPC,
	}

	pub, err := NewPublisher(cfg)
	require.NoError(t, err)

	ctx := context.Background()

	rttData := map[ProbeAddress]uint64{
		{Host: "127.0.0.1", Port: 9999}: 800000,
	}

	err = pub.Publish(ctx, rttData)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get lat/lng")
}

func TestPublisher_Publish_SlotError(t *testing.T) {
	t.Parallel()

	keypair := solana.NewWallet().PrivateKey
	devicePK := solana.NewWallet().PublicKey()
	locationPK := solana.NewWallet().PublicKey()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	mockServiceability := &mockServiceabilityClient{
		programData: createTestProgramData(devicePK, locationPK, 52.3676, 4.9041),
	}
	mockRPC := &mockRPCClient{
		err: errors.New("RPC error"),
	}

	cfg := &PublisherConfig{
		Logger:               logger,
		Keypair:              keypair,
		LocalDevicePK:        devicePK,
		ServiceabilityClient: mockServiceability,
		RPCClient:            mockRPC,
	}

	pub, err := NewPublisher(cfg)
	require.NoError(t, err)

	ctx := context.Background()

	rttData := map[ProbeAddress]uint64{
		{Host: "127.0.0.1", Port: 9999}: 800000,
	}

	err = pub.Publish(ctx, rttData)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get current slot")
}

func TestPublisher_Publish_ProbeNotInPool(t *testing.T) {
	t.Parallel()

	keypair := solana.NewWallet().PrivateKey
	devicePK := solana.NewWallet().PublicKey()
	locationPK := solana.NewWallet().PublicKey()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	mockServiceability := &mockServiceabilityClient{
		programData: createTestProgramData(devicePK, locationPK, 52.3676, 4.9041),
	}
	mockRPC := &mockRPCClient{slot: 12345}

	cfg := &PublisherConfig{
		Logger:               logger,
		Keypair:              keypair,
		LocalDevicePK:        devicePK,
		ServiceabilityClient: mockServiceability,
		RPCClient:            mockRPC,
	}

	pub, err := NewPublisher(cfg)
	require.NoError(t, err)

	ctx := context.Background()

	rttData := map[ProbeAddress]uint64{
		{Host: "127.0.0.1", Port: 9999}: 800000,
	}

	err = pub.Publish(ctx, rttData)
	require.NoError(t, err)
}

func TestPublisher_DeviceNotFound(t *testing.T) {
	t.Parallel()

	keypair := solana.NewWallet().PrivateKey
	devicePK := solana.NewWallet().PublicKey()
	otherDevicePK := solana.NewWallet().PublicKey()
	locationPK := solana.NewWallet().PublicKey()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	mockServiceability := &mockServiceabilityClient{
		programData: createTestProgramData(otherDevicePK, locationPK, 52.3676, 4.9041),
	}
	mockRPC := &mockRPCClient{slot: 12345}

	cfg := &PublisherConfig{
		Logger:               logger,
		Keypair:              keypair,
		LocalDevicePK:        devicePK,
		ServiceabilityClient: mockServiceability,
		RPCClient:            mockRPC,
	}

	pub, err := NewPublisher(cfg)
	require.NoError(t, err)

	ctx := context.Background()

	_, _, err = pub.getLatLng(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), fmt.Sprintf("device %s not found", devicePK))
}
