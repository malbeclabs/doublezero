package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/config"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/geoprobe"
	twamplight "github.com/malbeclabs/doublezero/tools/twamp/pkg/light"
)

const (
	defaultTWAMPListenPort       = 862
	defaultUDPListenPort         = 8923
	defaultProbeInterval         = 5 * time.Minute
	defaultTWAMPSenderTimeout    = 1 * time.Second
	defaultTWAMPReflectorTimeout = 1 * time.Second
	defaultMaxOffsetAge          = 1 * time.Hour
	defaultEvictionInterval      = 30 * time.Minute
	discoveryInterval            = 60 * time.Second
)

var (
	env                = flag.String("env", "", "The network environment to use (devnet, testnet, mainnet-beta).")
	ledgerRPCURL       = flag.String("ledger-rpc-url", "", "The url of the ledger RPC. If env is provided, this flag is ignored.")
	keypairPath        = flag.String("keypair", "", "The path to the probe's Ed25519 keypair file.")
	additionalParents  = flag.String("additional-parents", "", "Comma-separated list of trusted parent DZD pubkeys (base58).")
	additionalTargets  = flag.String("additional-targets", "", "Comma-separated list of target addresses (host:port) to measure and send composite offsets.")
	twampListenPort    = flag.Uint("twamp-listen-port", defaultTWAMPListenPort, "Port for TWAMP reflector.")
	udpListenPort      = flag.Uint("udp-listen-port", defaultUDPListenPort, "Port for receiving DZD offset datagrams.")
	probeInterval      = flag.Duration("probe-interval", defaultProbeInterval, "Interval between measurement cycles.")
	twampSenderTimeout = flag.Duration("twamp-sender-timeout", defaultTWAMPSenderTimeout, "Timeout for TWAMP probes to targets.")
	maxOffsetAge       = flag.Duration("max-offset-age", defaultMaxOffsetAge, "TTL for cached DZD offsets.")
	verbose            = flag.Bool("verbose", false, "Enable verbose logging.")
	showVersion        = flag.Bool("version", false, "Print the version and exit.")
	// Set by LDFLAGS
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// parentDZD represents a trusted parent DZD that sends offsets to this probe.
type parentDZD struct {
	pubkey [32]byte
}

// parseParentDZDs parses the --additional-parents flag value.
// Format: "pubkey,pubkey"
func parseParentDZDs(s string) ([]parentDZD, error) {
	if s == "" {
		return nil, nil
	}

	parts := strings.Split(s, ",")
	parents := make([]parentDZD, 0, len(parts))
	seen := make(map[[32]byte]bool)

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		pk, err := solana.PublicKeyFromBase58(part)
		if err != nil {
			return nil, fmt.Errorf("invalid pubkey %q: %w", part, err)
		}

		var pubkeyBytes [32]byte
		copy(pubkeyBytes[:], pk[:])

		if seen[pubkeyBytes] {
			continue
		}
		seen[pubkeyBytes] = true

		parents = append(parents, parentDZD{
			pubkey: pubkeyBytes,
		})
	}

	return parents, nil
}

// offsetCache stores recent DZD offsets keyed by DZD pubkey.
type offsetCache struct {
	mu      sync.RWMutex
	entries map[[32]byte]*cachedOffset
	maxAge  time.Duration
}

type cachedOffset struct {
	offset     geoprobe.LocationOffset
	receivedAt time.Time
}

func newOffsetCache(maxAge time.Duration) *offsetCache {
	return &offsetCache{
		entries: make(map[[32]byte]*cachedOffset),
		maxAge:  maxAge,
	}
}

func (c *offsetCache) Put(offset *geoprobe.LocationOffset) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[offset.Pubkey] = &cachedOffset{
		offset:     *offset,
		receivedAt: time.Now(),
	}
}

func (c *offsetCache) Get(pubkey [32]byte) *geoprobe.LocationOffset {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.entries[pubkey]
	if !ok {
		return nil
	}
	if time.Since(entry.receivedAt) > c.maxAge {
		return nil
	}
	result := entry.offset
	return &result
}

// GetBest returns the non-expired offset with the shortest RttNs.
func (c *offsetCache) GetBest() *geoprobe.LocationOffset {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var best *geoprobe.LocationOffset
	for _, entry := range c.entries {
		if time.Since(entry.receivedAt) > c.maxAge {
			continue
		}
		if best == nil || entry.offset.RttNs < best.RttNs {
			e := entry.offset
			best = &e
		}
	}
	return best
}

// Evict removes expired entries.
func (c *offsetCache) Evict() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	evicted := 0
	for key, entry := range c.entries {
		if time.Since(entry.receivedAt) > c.maxAge {
			delete(c.entries, key)
			evicted++
		}
	}
	return evicted
}

func main() {
	flag.Parse()

	if *showVersion {
		fmt.Printf("version: %s, commit: %s, date: %s\n", version, commit, date)
		os.Exit(0)
	}

	logLevel := slog.LevelInfo
	if *verbose {
		logLevel = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level:     logLevel,
		AddSource: true,
	}))

	// Validate required flags.
	if *keypairPath == "" {
		log.Error("Missing required flag", "flag", "keypair")
		flag.Usage()
		os.Exit(1)
	}

	// We need an RPC URL for slot fetching.
	if *env == "" && *ledgerRPCURL == "" {
		log.Error("Missing required flag: either --env or --ledger-rpc-url must be provided")
		flag.Usage()
		os.Exit(1)
	}

	if *env != "" {
		networkConfig, err := config.NetworkConfigForEnv(*env)
		if err != nil {
			log.Error("Failed to get network config", "error", err)
			flag.Usage()
			os.Exit(1)
		}
		*ledgerRPCURL = networkConfig.LedgerPublicRPCURL
	}

	// Load keypair.
	if _, err := os.Stat(*keypairPath); os.IsNotExist(err) {
		log.Error("Keypair file does not exist", "path", *keypairPath)
		os.Exit(1)
	}
	keypair, err := solana.PrivateKeyFromSolanaKeygenFile(*keypairPath)
	if err != nil {
		log.Error("Failed to load keypair", "error", err)
		os.Exit(1)
	}

	// Parse parents.
	parents, err := parseParentDZDs(*additionalParents)
	if err != nil {
		log.Error("Failed to parse additional-parents", "error", err)
		os.Exit(1)
	}

	// Build trusted pubkey set.
	trustedPubkeys := make(map[[32]byte]bool, len(parents))
	for _, p := range parents {
		trustedPubkeys[p.pubkey] = true
	}

	if len(trustedPubkeys) == 0 {
		log.Warn("No trusted parents configured -- will not accept offsets until parents are added")
	}

	// Parse targets.
	targets, err := geoprobe.ParseProbeAddresses(*additionalTargets)
	if err != nil {
		log.Error("Failed to parse additional-targets", "error", err)
		os.Exit(1)
	}

	log.Info("Starting geoprobe agent",
		"version", version,
		"parents", len(parents),
		"targets", len(targets),
		"probeInterval", *probeInterval,
		"maxOffsetAge", *maxOffsetAge,
		"twampListenPort", *twampListenPort,
		"udpListenPort", *udpListenPort,
		"pubkey", keypair.PublicKey(),
	)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Set up TWAMP reflector.
	reflector, err := twamplight.NewReflector(log, fmt.Sprintf("0.0.0.0:%d", *twampListenPort), defaultTWAMPReflectorTimeout)
	if err != nil {
		log.Error("Failed to create TWAMP reflector", "error", err)
		os.Exit(1)
	}

	// Set up UDP listener for receiving DZD offsets.
	offsetListener, err := geoprobe.NewUDPListener(int(*udpListenPort))
	if err != nil {
		log.Error("Failed to create UDP listener", "error", err)
		os.Exit(1)
	}
	defer offsetListener.Close()

	// Set up offset cache.
	cache := newOffsetCache(*maxOffsetAge)

	// Set up pinger for targets.
	pinger := geoprobe.NewPinger(&geoprobe.PingerConfig{
		Logger:       log,
		ProbeTimeout: *twampSenderTimeout,
		Interval:     *probeInterval,
	})
	defer pinger.Close()

	// Add probes for targets.
	for _, target := range targets {
		if err := pinger.AddProbe(ctx, target); err != nil {
			log.Warn("Failed to add target probe", "target", target, "error", err)
		}
	}

	// Set up signer and RPC client.
	signer := geoprobe.NewOffsetSigner(keypair)
	rpcClient := solanarpc.New(*ledgerRPCURL)

	// Slot cache for reducing RPC load.
	var (
		slotMu       sync.RWMutex
		cachedSlot   uint64
		slotCachedAt time.Time
	)

	getCurrentSlot := func(ctx context.Context) (uint64, error) {
		slotMu.RLock()
		if !slotCachedAt.IsZero() && time.Since(slotCachedAt) < 5*time.Minute {
			s := cachedSlot
			slotMu.RUnlock()
			return s, nil
		}
		slotMu.RUnlock()

		slot, err := rpcClient.GetSlot(ctx, solanarpc.CommitmentFinalized)
		if err != nil {
			slotMu.RLock()
			if !slotCachedAt.IsZero() {
				s := cachedSlot
				slotMu.RUnlock()
				log.Warn("Failed to fetch current slot, using stale cache",
					"error", err,
					"cached_slot", s,
					"cache_age", time.Since(slotCachedAt))
				return s, nil
			}
			slotMu.RUnlock()
			return 0, fmt.Errorf("failed to get slot from RPC: %w", err)
		}

		slotMu.Lock()
		cachedSlot = slot
		slotCachedAt = time.Now()
		slotMu.Unlock()

		return slot, nil
	}

	// Set up UDP sender for composite offsets.
	senderConn, err := geoprobe.NewUDPConn()
	if err != nil {
		log.Error("Failed to create UDP sender connection", "error", err)
		os.Exit(1)
	}
	defer senderConn.Close()

	errCh := make(chan error, 3)

	// Run TWAMP reflector.
	go func() {
		if err := reflector.Run(ctx); err != nil {
			errCh <- fmt.Errorf("TWAMP reflector: %w", err)
		}
	}()

	// Run UDP offset listener.
	go func() {
		runOffsetListener(ctx, log, offsetListener, cache, trustedPubkeys)
	}()

	// Run eviction goroutine.
	go func() {
		evictionTicker := time.NewTicker(defaultEvictionInterval)
		defer evictionTicker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-evictionTicker.C:
				if evicted := cache.Evict(); evicted > 0 {
					log.Debug("Evicted expired offsets", "count", evicted)
				}
			}
		}
	}()

	// Run main measurement loop. This runs regardless of whether trusted parents
	// are configured at startup, since they may be added dynamically at runtime.
	go func() {
		if err := runMeasurementLoop(ctx, log, pinger, cache, signer, senderConn, targets, getCurrentSlot); err != nil {
			errCh <- fmt.Errorf("measurement loop: %w", err)
		}
	}()

	select {
	case <-ctx.Done():
		log.Info("Geoprobe agent shutting down")
	case err := <-errCh:
		log.Error("Geoprobe agent exited with error", "error", err)
		cancel()
		os.Exit(1)
	}
}

func runOffsetListener(
	ctx context.Context,
	log *slog.Logger,
	conn *net.UDPConn,
	cache *offsetCache,
	trustedPubkeys map[[32]byte]bool,
) {
	log.Info("Starting offset listener", "addr", conn.LocalAddr().String())

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if err := conn.SetReadDeadline(time.Now().Add(1 * time.Second)); err != nil {
			log.Warn("Failed to set read deadline", "error", err)
			continue
		}

		offset, addr, err := geoprobe.ReceiveOffset(conn)
		if err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				continue
			}
			if ctx.Err() != nil {
				return
			}
			log.Warn("Failed to receive offset", "error", err)
			continue
		}

		// Verify the offset comes from a trusted parent.
		if !trustedPubkeys[offset.Pubkey] {
			pubkey := solana.PublicKeyFromBytes(offset.Pubkey[:])
			log.Debug("Rejecting offset from untrusted pubkey", "pubkey", pubkey, "addr", addr)
			continue
		}

		// Verify signature chain (top-level and all references).
		if err := geoprobe.VerifyOffsetChain(offset); err != nil {
			pubkey := solana.PublicKeyFromBytes(offset.Pubkey[:])
			log.Warn("Offset signature verification failed", "pubkey", pubkey, "addr", addr, "error", err)
			continue
		}

		cache.Put(offset)

		pubkey := solana.PublicKeyFromBytes(offset.Pubkey[:])
		log.Debug("Cached DZD offset",
			"pubkey", pubkey,
			"addr", addr,
			"rtt_ns", offset.RttNs,
			"measured_rtt_ns", offset.MeasuredRttNs,
			"lat", offset.Lat,
			"lng", offset.Lng,
			"slot", offset.MeasurementSlot)
	}
}

func runMeasurementLoop(
	ctx context.Context,
	log *slog.Logger,
	pinger *geoprobe.Pinger,
	cache *offsetCache,
	signer *geoprobe.OffsetSigner,
	senderConn *net.UDPConn,
	targets []geoprobe.ProbeAddress,
	getCurrentSlot func(ctx context.Context) (uint64, error),
) error {
	measureTicker := time.NewTicker(*probeInterval)
	defer measureTicker.Stop()

	discoveryTicker := time.NewTicker(discoveryInterval)
	defer discoveryTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil

		case <-measureTicker.C:
			runMeasurementCycle(ctx, log, pinger, cache, signer, senderConn, targets, getCurrentSlot)

		case <-discoveryTicker.C:
			// TODO: Check for new targets (stub for future implementation).
			// When implemented, new targets will be added to the pinger and
			// receive an immediate one-off probe. Subsequent probes happen
			// on the next regular measurement tick.
			// log.Debug("Target discovery tick (no-op)")
		}
	}
}

func runMeasurementCycle(
	ctx context.Context,
	log *slog.Logger,
	pinger *geoprobe.Pinger,
	cache *offsetCache,
	signer *geoprobe.OffsetSigner,
	senderConn *net.UDPConn,
	targets []geoprobe.ProbeAddress,
	getCurrentSlot func(ctx context.Context) (uint64, error),
) {
	if len(targets) == 0 {
		log.Debug("No targets configured, skipping measurement cycle")
		return
	}

	log.Debug("Starting measurement cycle", "targets", len(targets))

	rttData, err := pinger.MeasureAll(ctx)
	if err != nil {
		log.Error("Failed to measure targets", "error", err)
		return
	}

	if len(rttData) == 0 {
		log.Warn("No successful target measurements in cycle")
		return
	}

	dzdOffset := cache.GetBest()
	if dzdOffset == nil {
		log.Warn("No valid DZD offsets in cache, skipping composite generation")
		return
	}

	slot, err := getCurrentSlot(ctx)
	if err != nil {
		log.Error("Failed to get current slot", "error", err)
		return
	}

	sentCount := 0
	for addr, measuredRttNs := range rttData {
		compositeOffset := geoprobe.LocationOffset{
			MeasurementSlot: slot,
			MeasuredRttNs:   measuredRttNs,
			Lat:             dzdOffset.Lat,
			Lng:             dzdOffset.Lng,
			RttNs:           dzdOffset.RttNs + measuredRttNs,
			NumReferences:   1,
			References:      []geoprobe.LocationOffset{*dzdOffset},
		}

		if err := signer.SignOffset(&compositeOffset); err != nil {
			log.Error("Failed to sign composite offset", "target", addr, "error", err)
			continue
		}

		targetAddr := &net.UDPAddr{IP: net.ParseIP(addr.Host), Port: int(addr.Port)}
		if err := geoprobe.SendOffset(senderConn, targetAddr, &compositeOffset); err != nil {
			log.Error("Failed to send composite offset", "target", addr, "error", err)
			continue
		}

		sentCount++
		log.Debug("Sent composite offset to target",
			"target", addr,
			"slot", slot,
			"measured_rtt_ns", measuredRttNs,
			"total_rtt_ns", compositeOffset.RttNs,
			"lat", compositeOffset.Lat,
			"lng", compositeOffset.Lng,
			"ref_pubkey", solana.PublicKeyFromBytes(dzdOffset.Pubkey[:]))
	}

	log.Info("Completed measurement cycle",
		"measured", len(rttData),
		"sent", sentCount,
		"total_targets", len(targets))
}
