package geoprobe

import (
	"bytes"
	"context"
	"sync"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
)

type mockServiceabilityClient struct {
	mu          sync.Mutex
	devicePK    solana.PublicKey
	locationPK  solana.PublicKey
	programData *serviceability.ProgramData
	err         error
}

func newMockServiceabilityClient() *mockServiceabilityClient {
	return &mockServiceabilityClient{}
}

func (m *mockServiceabilityClient) GetProgramData(ctx context.Context) (*serviceability.ProgramData, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.err != nil {
		return nil, m.err
	}

	if m.programData != nil {
		return m.programData, nil
	}

	devicePK := m.devicePK
	if devicePK.IsZero() {
		devicePK = solana.NewWallet().PublicKey()
	}

	locationPK := m.locationPK
	if locationPK.IsZero() {
		locationPK = solana.NewWallet().PublicKey()
	}

	var locationPKBytes [32]byte
	copy(locationPKBytes[:], locationPK.Bytes())

	location := serviceability.Location{
		PubKey: locationPKBytes,
		Lat:    37.7749,
		Lng:    -122.4194,
		Code:   "test-location",
	}

	var devicePKBytes [32]byte
	copy(devicePKBytes[:], devicePK.Bytes())

	var locationPKBytesForDevice [32]uint8
	copy(locationPKBytesForDevice[:], locationPK.Bytes())

	device := serviceability.Device{
		PubKey:         devicePKBytes,
		LocationPubKey: locationPKBytesForDevice,
		Code:           "test-device",
	}

	return &serviceability.ProgramData{
		Locations: []serviceability.Location{location},
		Devices:   []serviceability.Device{device},
	}, nil
}

func (m *mockServiceabilityClient) ProgramID() solana.PublicKey {
	return solana.MustPublicKeyFromBase58("11111111111111111111111111111111")
}

func (m *mockServiceabilityClient) setError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.err = err
}

func (m *mockServiceabilityClient) setProgramData(data *serviceability.ProgramData) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.programData = data
}

func (m *mockServiceabilityClient) setDevicePK(pk solana.PublicKey) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.devicePK = pk
}

type mockRPCClient struct {
	mu   sync.Mutex
	slot uint64
	err  error
}

func newMockRPCClient() *mockRPCClient {
	return &mockRPCClient{
		slot: 12345,
	}
}

func (m *mockRPCClient) GetSlot(ctx context.Context, commitment solanarpc.CommitmentType) (uint64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.err != nil {
		return 0, m.err
	}

	return m.slot, nil
}

func (m *mockRPCClient) setSlot(slot uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.slot = slot
}

func (m *mockRPCClient) setError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.err = err
}

// tamperOffset modifies an offset's data without updating the signature.
func tamperOffset(offset *LocationOffset) {
	offset.MeasuredRttNs = offset.MeasuredRttNs + 1
}

func offsetSignaturesEqual(a, b *LocationOffset) bool {
	return bytes.Equal(a.Signature[:], b.Signature[:])
}
