package telemetry_test

import (
	"context"
	"flag"
	"log/slog"
	"maps"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/lmittmann/tint"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/telemetry"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	sdktelemetry "github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
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

func newTestAccountKey() telemetry.AccountKey {
	return telemetry.AccountKey{
		OriginDevicePK: solana.PublicKey{1},
		TargetDevicePK: solana.PublicKey{2},
		LinkPK:         solana.PublicKey{3},
		Epoch:          42,
	}
}

type mockServiceabilityProgramClient struct {
	devices []serviceability.Device
	links   []serviceability.Link

	loadFn func(c *mockServiceabilityProgramClient) error

	mu sync.RWMutex
}

func newMockServiceabilityProgramClient(loadFn func(c *mockServiceabilityProgramClient) error) *mockServiceabilityProgramClient {
	return &mockServiceabilityProgramClient{
		loadFn: loadFn,
	}
}

func (c *mockServiceabilityProgramClient) Load(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.loadFn(c)
}

func (c *mockServiceabilityProgramClient) GetDevices() []serviceability.Device {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.devices
}

func (c *mockServiceabilityProgramClient) GetLinks() []serviceability.Link {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.links
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
	accounts map[telemetry.AccountKey][]telemetry.Sample

	mu sync.RWMutex
}

func newMemoryTelemetryProgramClient() *memoryTelemetryProgramClient {
	return &memoryTelemetryProgramClient{
		accounts: make(map[telemetry.AccountKey][]telemetry.Sample),
	}
}

func (c *memoryTelemetryProgramClient) InitializeDeviceLatencySamples(ctx context.Context, config sdktelemetry.InitializeDeviceLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	accountKey := telemetry.AccountKey{
		OriginDevicePK: config.OriginDevicePK,
		TargetDevicePK: config.TargetDevicePK,
		LinkPK:         config.LinkPK,
		Epoch:          config.Epoch,
	}

	c.accounts[accountKey] = make([]telemetry.Sample, 0)

	return solana.Signature{}, nil, nil
}

func (c *memoryTelemetryProgramClient) WriteDeviceLatencySamples(ctx context.Context, config sdktelemetry.WriteDeviceLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	accountKey := telemetry.AccountKey{
		OriginDevicePK: config.OriginDevicePK,
		TargetDevicePK: config.TargetDevicePK,
		LinkPK:         config.LinkPK,
		Epoch:          config.Epoch,
	}

	if _, ok := c.accounts[accountKey]; !ok {
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
	c.accounts[accountKey] = append(c.accounts[accountKey], samples...)

	return solana.Signature{}, nil, nil
}

func (c *memoryTelemetryProgramClient) GetAccounts(t *testing.T) map[telemetry.AccountKey][]telemetry.Sample {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return maps.Clone(c.accounts)
}

func (c *memoryTelemetryProgramClient) GetSamples(t *testing.T, key telemetry.AccountKey) []telemetry.Sample {
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
	peers map[string]*telemetry.Peer

	mu sync.RWMutex
}

func newMockPeerDiscovery() *mockPeerDiscovery {
	return &mockPeerDiscovery{
		peers: make(map[string]*telemetry.Peer),
	}
}

func (p *mockPeerDiscovery) Run(ctx context.Context) error {
	<-ctx.Done()

	return nil
}

func (p *mockPeerDiscovery) GetPeers() map[string]*telemetry.Peer {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return maps.Clone(p.peers)
}

func (p *mockPeerDiscovery) UpdatePeers(t *testing.T, peers map[string]*telemetry.Peer) {
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
