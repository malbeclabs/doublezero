package telemetry_test

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"maps"
	"net"
	"os"
	"slices"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/lmittmann/tint"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/telemetry"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	sdktelemetry "github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	log *slog.Logger
)

// TestMain sets up the test environment with a global logger.
func TestMain(m *testing.M) {
	flag.Parse()
	verbose := false
	if vFlag := flag.Lookup("test.v"); vFlag != nil && vFlag.Value.String() == "true" {
		verbose = true
	}
	logLevel := slog.LevelInfo
	if verbose {
		logLevel = slog.LevelDebug
	}
	log = slog.New(tint.NewHandler(os.Stdout, &tint.Options{
		Level:      logLevel,
		TimeFormat: time.RFC3339,
		AddSource:  true,
	}))

	os.Exit(m.Run())
}

func newTestSample() telemetry.Sample {
	return telemetry.Sample{
		Timestamp: time.Unix(123, 456),
		RTT:       42 * time.Millisecond,
		Loss:      false,
	}
}

func newTestPartitionKey() telemetry.PartitionKey {
	return telemetry.PartitionKey{
		OriginDevicePK: solana.PublicKey{1},
		TargetDevicePK: solana.PublicKey{2},
		LinkPK:         solana.PublicKey{3},
		Epoch:          42,
	}
}

type mockServiceabilityProgramClient struct {
	GetProgramDataFunc func(ctx context.Context) (*serviceability.ProgramData, error)
}

func (c *mockServiceabilityProgramClient) GetProgramData(ctx context.Context) (*serviceability.ProgramData, error) {
	return c.GetProgramDataFunc(ctx)
}

type mockTelemetryProgramClient struct {
	InitializeDeviceLatencySamplesFunc func(ctx context.Context, config sdktelemetry.InitializeDeviceLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error)
	WriteDeviceLatencySamplesFunc      func(ctx context.Context, config sdktelemetry.WriteDeviceLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error)
	GetDeviceLatencySamplesFunc        func(ctx context.Context, originDevicePK solana.PublicKey, targetDevicePK solana.PublicKey, linkPK solana.PublicKey, epoch uint64) (*sdktelemetry.DeviceLatencySamples, error)
}

func (c *mockTelemetryProgramClient) InitializeDeviceLatencySamples(ctx context.Context, config sdktelemetry.InitializeDeviceLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
	return c.InitializeDeviceLatencySamplesFunc(ctx, config)
}

func (c *mockTelemetryProgramClient) WriteDeviceLatencySamples(ctx context.Context, config sdktelemetry.WriteDeviceLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
	return c.WriteDeviceLatencySamplesFunc(ctx, config)
}

func (c *mockTelemetryProgramClient) GetDeviceLatencySamples(ctx context.Context, originDevicePK solana.PublicKey, targetDevicePK solana.PublicKey, linkPK solana.PublicKey, epoch uint64) (*sdktelemetry.DeviceLatencySamples, error) {
	return c.GetDeviceLatencySamplesFunc(ctx, originDevicePK, targetDevicePK, linkPK, epoch)
}

type memoryTelemetryProgramClient struct {
	accounts map[telemetry.PartitionKey][]telemetry.Sample

	mu sync.RWMutex
}

func newMemoryTelemetryProgramClient() *memoryTelemetryProgramClient {
	return &memoryTelemetryProgramClient{
		accounts: make(map[telemetry.PartitionKey][]telemetry.Sample),
	}
}

func (c *memoryTelemetryProgramClient) InitializeDeviceLatencySamples(ctx context.Context, config sdktelemetry.InitializeDeviceLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	partitionKey := telemetry.PartitionKey{
		OriginDevicePK: config.OriginDevicePK,
		TargetDevicePK: config.TargetDevicePK,
		LinkPK:         config.LinkPK,
		Epoch:          *config.Epoch,
	}

	c.accounts[partitionKey] = make([]telemetry.Sample, 0)

	return solana.Signature{}, nil, nil
}

func (c *memoryTelemetryProgramClient) WriteDeviceLatencySamples(ctx context.Context, config sdktelemetry.WriteDeviceLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	partitionKey := telemetry.PartitionKey{
		OriginDevicePK: config.OriginDevicePK,
		TargetDevicePK: config.TargetDevicePK,
		LinkPK:         config.LinkPK,
		Epoch:          *config.Epoch,
	}

	if _, ok := c.accounts[partitionKey]; !ok {
		return solana.Signature{}, nil, sdktelemetry.ErrAccountNotFound
	}

	samples := make([]telemetry.Sample, len(config.Samples))
	for i, sample := range config.Samples {
		samples[i] = telemetry.Sample{
			Timestamp: time.Now(),
			RTT:       time.Duration(sample) * time.Microsecond,
			Loss:      sample == 0,
		}
	}
	c.accounts[partitionKey] = append(c.accounts[partitionKey], samples...)

	return solana.Signature{}, nil, nil
}

func (c *memoryTelemetryProgramClient) GetAccounts(t *testing.T) map[telemetry.PartitionKey][]telemetry.Sample {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return maps.Clone(c.accounts)
}

func (c *memoryTelemetryProgramClient) GetSamples(t *testing.T, key telemetry.PartitionKey) []telemetry.Sample {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return append([]telemetry.Sample{}, c.accounts[key]...)
}

func (c *memoryTelemetryProgramClient) ClearSamples() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.accounts = nil
}

type mockPeerDiscovery struct {
	peers []*telemetry.Peer

	mu sync.RWMutex
}

func newMockPeerDiscovery() *mockPeerDiscovery {
	return &mockPeerDiscovery{
		peers: make([]*telemetry.Peer, 0),
	}
}

func (p *mockPeerDiscovery) Run(ctx context.Context) error {
	<-ctx.Done()

	return nil
}

func (p *mockPeerDiscovery) GetPeers() []*telemetry.Peer {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return slices.Clone(p.peers)
}

func (p *mockPeerDiscovery) UpdatePeers(t *testing.T, peers []*telemetry.Peer) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.peers = peers
}

// stringToPubkey coerces a string to a solana.PublicKey by copying up to 32 bytes of the string
// and zero-padding the rest if necessary.
//
// This is useful for testing purposes only where we want to use a string as a pubkey.
func stringToPubkey(s string) solana.PublicKey {
	var b [32]byte
	copy(b[:], s) // copies up to 32 bytes; extra bytes are ignored, rest are zero-padded
	return solana.PublicKeyFromBytes(b[:])
}

func requireUnorderedEqual[T fmt.Stringer](t *testing.T, expected, actual []T) {
	t.Helper()
	sort.Slice(expected, func(i, j int) bool {
		return expected[i].String() < expected[j].String()
	})
	sort.Slice(actual, func(i, j int) bool {
		return actual[i].String() < actual[j].String()
	})
	assert.Equal(t, expected, actual)
}

func loopbackInterface(t *testing.T) string {
	t.Helper()

	ifaces, err := net.Interfaces()
	require.NoError(t, err)
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 {
			return iface.Name
		}
	}
	return ""
}
