package telemetry

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/stretchr/testify/require"

	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/geoprobe"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	telemetryprog "github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
)

type mockReflector struct{}

func (m *mockReflector) Run(ctx context.Context) error {
	return nil
}

func (m *mockReflector) Close() error {
	return nil
}

func (m *mockReflector) LocalAddr() *net.UDPAddr {
	return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0}
}

type mockPeerDiscovery struct{}

func (m *mockPeerDiscovery) Run(ctx context.Context) error {
	return nil
}

func (m *mockPeerDiscovery) GetPeers() []*Peer {
	return nil
}

type mockTelemetryProgramClient struct{}

func (m *mockTelemetryProgramClient) InitializeDeviceLatencySamples(ctx context.Context, config telemetryprog.InitializeDeviceLatencySamplesInstructionConfig) (solana.Signature, *rpc.GetTransactionResult, error) {
	return solana.Signature{}, nil, nil
}

func (m *mockTelemetryProgramClient) WriteDeviceLatencySamples(ctx context.Context, config telemetryprog.WriteDeviceLatencySamplesInstructionConfig) (solana.Signature, *rpc.GetTransactionResult, error) {
	return solana.Signature{}, nil, nil
}

func validBaseConfig(keypair solana.PrivateKey) Config {
	return Config{
		TWAMPReflector:          &mockReflector{},
		PeerDiscovery:           &mockPeerDiscovery{},
		TelemetryProgramClient:  &mockTelemetryProgramClient{},
		LocalDevicePK:           solana.NewWallet().PublicKey(),
		ProbeInterval:           10 * time.Second,
		SubmissionInterval:      30 * time.Second,
		TWAMPSenderTimeout:      5 * time.Second,
		SenderTTL:               60 * time.Second,
		SubmitterMaxConcurrency: 4,
		GetCurrentEpochFunc: func(ctx context.Context) (uint64, error) {
			return 1, nil
		},
		Keypair: keypair,
	}
}

func TestConfig_Validate_GeoprobeFields(t *testing.T) {
	keypair, err := solana.NewRandomPrivateKey()
	require.NoError(t, err)

	rpcClient := rpc.New("https://api.mainnet-beta.solana.com")
	serviceabilityClient := &serviceability.Client{}

	testCases := []struct {
		name        string
		modify      func(*Config)
		expectError string
	}{
		{
			name:        "valid config without geoprobe",
			modify:      func(c *Config) {},
			expectError: "",
		},
		{
			name: "valid config with geoprobe",
			modify: func(c *Config) {
				c.InitialChildGeoProbes = []geoprobe.ProbeAddress{{Host: "192.0.2.1", Port: 8080}}
				c.ServiceabilityProgramClient = serviceabilityClient
				c.RPCClient = rpcClient
			},
			expectError: "",
		},
		{
			name: "geoprobe enabled but missing serviceability client",
			modify: func(c *Config) {
				c.InitialChildGeoProbes = []geoprobe.ProbeAddress{{Host: "192.0.2.1", Port: 8080}}
				c.RPCClient = rpcClient
			},
			expectError: "serviceability client is required when geoprobe is enabled",
		},
		{
			name: "geoprobe enabled but missing rpc client",
			modify: func(c *Config) {
				c.InitialChildGeoProbes = []geoprobe.ProbeAddress{{Host: "192.0.2.1", Port: 8080}}
				c.ServiceabilityProgramClient = serviceabilityClient
			},
			expectError: "rpc client is required when geoprobe is enabled",
		},
		{
			name: "geoprobe enabled but missing keypair",
			modify: func(c *Config) {
				c.InitialChildGeoProbes = []geoprobe.ProbeAddress{{Host: "192.0.2.1", Port: 8080}}
				c.ServiceabilityProgramClient = serviceabilityClient
				c.RPCClient = rpcClient
				c.Keypair = nil
			},
			expectError: "keypair is required when geoprobe is enabled",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validBaseConfig(keypair)
			tc.modify(&cfg)
			err := cfg.Validate()
			if tc.expectError == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.expectError)
			}
		})
	}
}
