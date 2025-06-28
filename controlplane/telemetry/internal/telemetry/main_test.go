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
	"github.com/lmittmann/tint"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/telemetry"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
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
	samples []telemetry.Sample

	mu sync.RWMutex
}

func newMockTelemetryProgramClient() *mockTelemetryProgramClient {
	return &mockTelemetryProgramClient{}
}

func (c *mockTelemetryProgramClient) AddSamples(ctx context.Context, samples []telemetry.Sample) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.samples = append(c.samples, samples...)
	return nil
}

func (c *mockTelemetryProgramClient) GetSamples(t *testing.T) []telemetry.Sample {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.samples
}

func (c *mockTelemetryProgramClient) ClearSamples() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.samples = nil
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
