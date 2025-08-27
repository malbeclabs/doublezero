package telemetry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
)

type refreshableTelemetryClient struct {
	log         *slog.Logger
	rpc         telemetry.RPCClient
	programID   solana.PublicKey
	keypairPath string

	mu              sync.RWMutex
	signer          solana.PrivateKey
	client          *telemetry.Client
	lastKeyMtime    time.Time
	lastStatTime    time.Time
	minStatInterval time.Duration
}

type TelemetryClientOption func(*refreshableTelemetryClient)

func WithMinStatInterval(d time.Duration) TelemetryClientOption {
	return func(c *refreshableTelemetryClient) { c.minStatInterval = d }
}

func NewTelemetryClient(log *slog.Logger, rpc telemetry.RPCClient, programID solana.PublicKey, signerKeypairPath string, opts ...TelemetryClientOption) (*refreshableTelemetryClient, error) {
	c := &refreshableTelemetryClient{
		log:             log,
		rpc:             rpc,
		programID:       programID,
		keypairPath:     signerKeypairPath,
		minStatInterval: 5 * time.Second,
	}
	for _, opt := range opts {
		opt(c)
	}
	if err := c.maybeRefresh(); err != nil {
		return nil, fmt.Errorf("failed to refresh signer keypair: %w", err)
	}
	return c, nil
}

func (c *refreshableTelemetryClient) maybeRefresh() error {
	// throttle stats if we already have a client
	c.mu.RLock()
	hasClient := c.client != nil
	tooSoon := time.Since(c.lastStatTime) < c.minStatInterval
	c.mu.RUnlock()
	if hasClient && tooSoon {
		return nil
	}

	fi, err := os.Stat(c.keypairPath)
	if err != nil {
		return fmt.Errorf("stat signer keypair: %w", err)
	}
	newMtime := fi.ModTime()

	c.mu.RLock()
	mtCached := c.lastKeyMtime
	hasSigner := len(c.signer) != 0
	c.mu.RUnlock()

	// unchanged mtime → only bump lastStatTime
	if hasSigner && !newMtime.After(mtCached) {
		c.mu.Lock()
		c.lastStatTime = time.Now()
		c.mu.Unlock()
		return nil
	}

	// load key outside write lock
	key, err := solana.PrivateKeyFromSolanaKeygenFile(c.keypairPath)
	if err != nil {
		return fmt.Errorf("failed to load signer keypair: %w", err)
	}
	newPK := key.PublicKey()

	c.mu.Lock()
	defer c.mu.Unlock()

	// mark stat time under lock
	c.lastStatTime = time.Now()

	// TOCTOU guard: state may have changed since our RLock snapshot
	hasSigner = len(c.signer) != 0
	if hasSigner && !newMtime.After(c.lastKeyMtime) && c.signer.PublicKey().Equals(newPK) {
		return nil
	}

	// log the change
	oldStr := "<none>"
	if hasSigner {
		oldStr = c.signer.PublicKey().String()
	}
	newStr := newPK.String()

	c.log.Info("Detected signer keypair change; updating telemetry client",
		slog.String("old", oldStr),
		slog.String("new", newStr),
		slog.Time("mtime", newMtime),
	)

	// swap in new signer/client
	c.signer = key
	c.lastKeyMtime = newMtime
	c.client = telemetry.New(c.log, c.rpc, &c.signer, c.programID)
	return nil
}

func (c *refreshableTelemetryClient) GetSignerPublicKey() (solana.PublicKey, error) {
	if err := c.maybeRefresh(); err != nil {
		return solana.PublicKey{}, fmt.Errorf("refresh signer keypair: %w", err)
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if len(c.signer) == 0 {
		return solana.PublicKey{}, errors.New("no signer available")
	}
	return c.signer.PublicKey(), nil
}

func (c *refreshableTelemetryClient) InitializeDeviceLatencySamples(ctx context.Context, cfg telemetry.InitializeDeviceLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
	if err := c.maybeRefresh(); err != nil {
		return solana.Signature{}, nil, err
	}
	cli, err := c.getClient()
	if err != nil {
		return solana.Signature{}, nil, err
	}
	return cli.InitializeDeviceLatencySamples(ctx, cfg)
}

func (c *refreshableTelemetryClient) WriteDeviceLatencySamples(ctx context.Context, cfg telemetry.WriteDeviceLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
	if err := c.maybeRefresh(); err != nil {
		return solana.Signature{}, nil, err
	}
	cli, err := c.getClient()
	if err != nil {
		return solana.Signature{}, nil, err
	}
	return cli.WriteDeviceLatencySamples(ctx, cfg)
}

func (c *refreshableTelemetryClient) getClient() (*telemetry.Client, error) {
	c.mu.RLock()
	cli := c.client
	c.mu.RUnlock()
	if cli == nil {
		return nil, fmt.Errorf("telemetry client not initialized")
	}
	return cli, nil
}
