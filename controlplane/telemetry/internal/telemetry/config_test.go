package telemetry

import (
	"context"
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/stretchr/testify/require"

	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/geoprobe"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
)

type mockReflector struct{}

func (m *mockReflector) Start(ctx context.Context) error {
	return nil
}

func (m *mockReflector) Stop() error {
	return nil
}

type mockPeerDiscovery struct{}

func (m *mockPeerDiscovery) Start(ctx context.Context) error {
	return nil
}

func (m *mockPeerDiscovery) Stop() error {
	return nil
}

type mockTelemetryProgramClient struct{}

func (m *mockTelemetryProgramClient) Start(ctx context.Context) error {
	return nil
}

func (m *mockTelemetryProgramClient) Stop() error {
	return nil
}

func TestConfig_Validate_GeoprobeFields(t *testing.T) {
	keypair, err := solana.NewRandomPrivateKey()
	require.NoError(t, err)

	rpcClient := rpc.New("https://api.mainnet-beta.solana.com")
	serviceabilityClient := &serviceability.Client{}

	testCases := []struct {
		name        string
		config      Config
		expectError string
	}{
		{
			name: "valid config without geoprobe",
			config: Config{
				Reflector:              &mockReflector{},
				PeerDiscovery:          &mockPeerDiscovery{},
				TelemetryProgramClient: &mockTelemetryProgramClient{},
				Keypair:                keypair,
			},
			expectError: "",
		},
		{
			name: "valid config with geoprobe",
			config: Config{
				Reflector:                   &mockReflector{},
				PeerDiscovery:               &mockPeerDiscovery{},
				TelemetryProgramClient:      &mockTelemetryProgramClient{},
				Keypair:                     keypair,
				InitialChildGeoProbes:       []geoprobe.Address{{Host: "192.0.2.1", Port: 8080}},
				ServiceabilityProgramClient: serviceabilityClient,
				RPCClient:                   rpcClient,
			},
			expectError: "",
		},
		{
			name: "geoprobe enabled but missing serviceability client",
			config: Config{
				Reflector:              &mockReflector{},
				PeerDiscovery:          &mockPeerDiscovery{},
				TelemetryProgramClient: &mockTelemetryProgramClient{},
				Keypair:                keypair,
				InitialChildGeoProbes:  []geoprobe.Address{{Host: "192.0.2.1", Port: 8080}},
				RPCClient:              rpcClient,
			},
			expectError: "serviceability client is required when geoprobe is enabled",
		},
		{
			name: "geoprobe enabled but missing rpc client",
			config: Config{
				Reflector:                   &mockReflector{},
				PeerDiscovery:               &mockPeerDiscovery{},
				TelemetryProgramClient:      &mockTelemetryProgramClient{},
				Keypair:                     keypair,
				InitialChildGeoProbes:       []geoprobe.Address{{Host: "192.0.2.1", Port: 8080}},
				ServiceabilityProgramClient: serviceabilityClient,
			},
			expectError: "rpc client is required when geoprobe is enabled",
		},
		{
			name: "geoprobe enabled but missing keypair",
			config: Config{
				Reflector:                   &mockReflector{},
				PeerDiscovery:               &mockPeerDiscovery{},
				TelemetryProgramClient:      &mockTelemetryProgramClient{},
				InitialChildGeoProbes:       []geoprobe.Address{{Host: "192.0.2.1", Port: 8080}},
				ServiceabilityProgramClient: serviceabilityClient,
				RPCClient:                   rpcClient,
			},
			expectError: "keypair is required when geoprobe is enabled",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.config.Validate()
			if tc.expectError == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.expectError)
			}
		})
	}
}
