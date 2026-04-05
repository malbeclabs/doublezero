//go:build linux

package main

import (
	"context"
	"crypto/ed25519"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/config"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/geoprobe"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/metrics"
	geolocation "github.com/malbeclabs/doublezero/sdk/geolocation/go"
	twamplight "github.com/malbeclabs/doublezero/tools/twamp/pkg/light"
	"github.com/malbeclabs/doublezero/tools/twamp/pkg/signed"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	defaultTWAMPListenPort       = 8925
	defaultSignedTWAMPListenPort = 8924
	defaultUDPListenPort         = 8923
	defaultProbeInterval         = 30 * time.Second
	defaultTWAMPSenderTimeout    = 1 * time.Second
	defaultTWAMPReflectorTimeout = 1 * time.Second
	defaultMaxOffsetAge          = 1 * time.Hour
	defaultEvictionInterval      = 30 * time.Minute
	defaultVerifyInterval        = 29 * time.Second
	discoveryInterval            = 60 * time.Second
)

var (
	env                        = flag.String("env", "", "The network environment to use (devnet, testnet, mainnet-beta).")
	ledgerRPCURL               = flag.String("ledger-rpc-url", "", "The url of the ledger RPC. If env is provided, this flag is ignored.")
	keypairPath                = flag.String("keypair", "", "The path to the probe's Ed25519 keypair file for signing messages.")
	geoProbePubkeyStr          = flag.String("geoprobe-pubkey", "", "The geoprobe device's public key (base58). Identifies this specific probe in offsets. Should Match DZ Ledger")
	additionalParent           = flag.String("additional-parent", "", "Trusted parent DZD in the format devicekey,metricskey (base58 pubkeys).")
	additionalTargets          = flag.String("additional-targets", "", "Comma-separated list of target addresses (host or host:offset_port:twamp_port) to measure and send composite offsets.")
	additionalIcmpTargets      = flag.String("additional-icmp-targets", "", "Comma-separated list of ICMP target addresses (host:offset_port) to measure via ICMP echo and send composite offsets.")
	twampListenPort            = flag.Uint("twamp-listen-port", defaultTWAMPListenPort, "Port for TWAMP reflector.")
	signedTWAMPListenPort      = flag.Uint("signed-twamp-port", defaultSignedTWAMPListenPort, "Port for Signed TWAMP reflector for inbound probing.")
	allowedPubkeysFlag         = flag.String("allowed-pubkeys", "", "Comma-separated base58 Ed25519 pubkeys always authorized for signed TWAMP probes in inbound probing.")
	udpListenPort              = flag.Uint("udp-listen-port", defaultUDPListenPort, "Port for receiving DZD offset datagrams.")
	probeInterval              = flag.Duration("probe-interval", defaultProbeInterval, "Interval between measurement cycles.")
	twampSenderTimeout         = flag.Duration("twamp-sender-timeout", defaultTWAMPSenderTimeout, "Timeout for TWAMP probes to targets.")
	maxOffsetAge               = flag.Duration("max-offset-age", defaultMaxOffsetAge, "TTL for cached DZD offsets.")
	verifyInterval             = flag.Duration("verify-interval", defaultVerifyInterval, "Minimum time between signature verifications per sender for the signed TWAMP reflector in inbound probing.")
	geolocationProgramIDStr    = flag.String("geolocation-program-id", "", "Geolocation program ID (base58). If env is provided, this is derived automatically.")
	serviceabilityProgramIDStr = flag.String("serviceability-program-id", "", "Serviceability program ID (base58). If env is provided, this is derived automatically.")
	verbose                    = flag.Bool("verbose", false, "Enable verbose logging.")
	showVersion                = flag.Bool("version", false, "Print the version and exit.")
	metricsEnable              = flag.Bool("metrics-enable", false, "Enable prometheus metrics.")
	metricsAddr                = flag.String("metrics-addr", ":8080", "Address to listen on for prometheus metrics.")
	// Set by LDFLAGS
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// parentDZD represents a trusted parent DZD that sends offsets to this probe.
type parentDZD struct {
	pubkey          [32]byte // Parent pubkey (appears as SenderPubkey in received offsets)
	authorityPubkey [32]byte // Authority pubkey (used to sign offsets)
}

// parentState holds the current parent authorities with thread-safe access.
type parentState struct {
	mu          sync.RWMutex
	authorities map[[32]byte][32]byte // parent pubkey → authority pubkey
}

func (s *parentState) getAuthority(senderPubkey [32]byte) ([32]byte, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	auth, ok := s.authorities[senderPubkey]
	return auth, ok
}

func (s *parentState) update(authorities map[[32]byte][32]byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.authorities = authorities
}

// parseParentDZD parses the --additional-parent flag value.
// Format: "parentkey,authoritykey"
func parseParentDZD(s string) (*parentDZD, error) {
	if s == "" {
		return nil, nil
	}

	parts := strings.Split(s, ",")
	if len(parts) != 2 {
		return nil, fmt.Errorf("expected format parentkey,authoritykey, got %q", s)
	}

	parentStr := strings.TrimSpace(parts[0])
	authorityStr := strings.TrimSpace(parts[1])

	parentPK, err := solana.PublicKeyFromBase58(parentStr)
	if err != nil {
		return nil, fmt.Errorf("invalid parent pubkey %q: %w", parentStr, err)
	}

	authorityPK, err := solana.PublicKeyFromBase58(authorityStr)
	if err != nil {
		return nil, fmt.Errorf("invalid authority pubkey %q: %w", authorityStr, err)
	}

	var pubkeyBytes, authorityBytes [32]byte
	copy(pubkeyBytes[:], parentPK[:])
	copy(authorityBytes[:], authorityPK[:])

	return &parentDZD{
		pubkey:          pubkeyBytes,
		authorityPubkey: authorityBytes,
	}, nil
}

// parseAllowedPubkeys parses a comma-separated list of base58 Ed25519 public keys.
func parseAllowedPubkeys(s string) ([][32]byte, error) {
	if s == "" {
		return nil, nil
	}

	parts := strings.Split(s, ",")
	keys := make([][32]byte, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		pk, err := solana.PublicKeyFromBase58(p)
		if err != nil {
			return nil, fmt.Errorf("invalid pubkey %q: %w", p, err)
		}
		var key [32]byte
		copy(key[:], pk[:])
		keys = append(keys, key)
	}
	return keys, nil
}

// offsetCache stores recent DZD offsets keyed by sender (device) pubkey.
type offsetCache struct {
	mu      sync.RWMutex
	entries map[[32]byte]*cachedSender
	maxAge  time.Duration
}

type cachedOffset struct {
	offset     geoprobe.LocationOffset
	receivedAt time.Time
}

func (co *cachedOffset) expired(maxAge time.Duration) bool {
	return co == nil || time.Since(co.receivedAt) > maxAge
}

type cachedSender struct {
	best   *cachedOffset // lowest RTT seen in the full maxAge window
	backup *cachedOffset // lowest RTT seen in the recent half-maxAge window
}

func newOffsetCache(maxAge time.Duration) *offsetCache {
	return &offsetCache{
		entries: make(map[[32]byte]*cachedSender),
		maxAge:  maxAge,
	}
}

func (c *offsetCache) Put(offset *geoprobe.LocationOffset) {
	c.mu.Lock()
	defer c.mu.Unlock()

	sender, ok := c.entries[offset.SenderPubkey]
	if !ok {
		sender = &cachedSender{}
		c.entries[offset.SenderPubkey] = sender
	}

	now := time.Now()
	entry := &cachedOffset{
		offset:     *offset,
		receivedAt: now,
	}

	// If best is expired, promote backup to best (if backup is non-expired), then clear backup.
	if sender.best.expired(c.maxAge) {
		if !sender.backup.expired(c.maxAge) {
			sender.best = sender.backup
		} else {
			sender.best = nil
		}
		sender.backup = nil
	}

	// If best is nil (empty after promotion attempt), just set it.
	if sender.best == nil {
		sender.best = entry
		return
	}

	if offset.RttNs <= sender.best.offset.RttNs {
		// New offset is better than or equal to best: replace best.
		sender.best = entry
	} else {
		// New offset has higher RTT than best, consider it for second-best.
		// Second-best must have a half of a MaxAge left to live, so that if
		// it gets promoted, it could hold for a meaningful amount of time.
		halfMaxAge := c.maxAge / 2
		if sender.backup.expired(c.maxAge) || offset.RttNs <= sender.backup.offset.RttNs {
			sender.backup = entry
		} else if time.Since(sender.backup.receivedAt) > halfMaxAge {
			// Backup is stale (older than half-maxAge): replace to keep it fresh.
			sender.backup = entry
		}
	}
}

func (c *offsetCache) Get(senderPubkey [32]byte) *geoprobe.LocationOffset {
	c.mu.RLock()
	defer c.mu.RUnlock()
	sender, ok := c.entries[senderPubkey]
	if !ok {
		return nil
	}
	if !sender.best.expired(c.maxAge) {
		result := sender.best.offset
		return &result
	}
	if !sender.backup.expired(c.maxAge) {
		result := sender.backup.offset
		return &result
	}
	return nil
}

// GetBest returns the non-expired offset with the shortest RttNs.
func (c *offsetCache) GetBest() *geoprobe.LocationOffset {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var best *geoprobe.LocationOffset
	for _, sender := range c.entries {
		for _, entry := range []*cachedOffset{sender.best, sender.backup} {
			if entry.expired(c.maxAge) {
				continue
			}
			if best == nil || entry.offset.RttNs < best.RttNs {
				e := entry.offset
				best = &e
			}
		}
	}
	return best
}

// Evict removes expired entries.
func (c *offsetCache) Evict() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	evicted := 0
	for key, sender := range c.entries {
		if sender.best.expired(c.maxAge) && sender.backup.expired(c.maxAge) {
			delete(c.entries, key)
			evicted++
		}
	}
	return evicted
}

func marshalBestOffset(cache *offsetCache) [][]byte {
	best := cache.GetBest()
	if best == nil {
		return nil
	}
	data, err := best.Marshal()
	if err != nil {
		return nil
	}
	return [][]byte{data}
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
	if *geoProbePubkeyStr == "" {
		log.Error("Missing required flag", "flag", "geoprobe-pubkey")
		flag.Usage()
		os.Exit(1)
	}

	// We need an RPC URL for slot fetching.
	if *env == "" && *ledgerRPCURL == "" {
		log.Error("Missing required flag: either --env or --ledger-rpc-url must be provided")
		flag.Usage()
		os.Exit(1)
	}

	var networkConfig *config.NetworkConfig
	if *env != "" {
		var err error
		networkConfig, err = config.NetworkConfigForEnv(*env)
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

	// Parse geoprobe device pubkey.
	geoProbePubkey, err := solana.PublicKeyFromBase58(*geoProbePubkeyStr)
	if err != nil {
		log.Error("Failed to parse geoprobe-pubkey", "error", err)
		os.Exit(1)
	}

	// Parse parent.
	var parents []parentDZD
	if *additionalParent != "" {
		parent, err := parseParentDZD(*additionalParent)
		if err != nil {
			log.Error("Failed to parse additional-parent", "error", err)
			os.Exit(1)
		}
		if parent != nil {
			parents = append(parents, *parent)
		}
	}

	// Build initial parent authority map from CLI args.
	cliParentAuthorities := make(map[[32]byte][32]byte, len(parents))
	for _, p := range parents {
		cliParentAuthorities[p.pubkey] = p.authorityPubkey
	}

	pState := &parentState{
		authorities: make(map[[32]byte][32]byte, len(cliParentAuthorities)),
	}
	for k, v := range cliParentAuthorities {
		pState.authorities[k] = v
	}

	// Derive geolocation and serviceability program IDs from --env if not explicit.
	var geolocationProgramID, serviceabilityProgramID solana.PublicKey
	if *geolocationProgramIDStr != "" {
		pk, err := solana.PublicKeyFromBase58(*geolocationProgramIDStr)
		if err != nil {
			log.Error("Failed to parse geolocation-program-id", "error", err)
			os.Exit(1)
		}
		geolocationProgramID = pk
	} else if networkConfig != nil {
		geolocationProgramID = networkConfig.GeolocationProgramID
	}
	if *serviceabilityProgramIDStr != "" {
		pk, err := solana.PublicKeyFromBase58(*serviceabilityProgramIDStr)
		if err != nil {
			log.Error("Failed to parse serviceability-program-id", "error", err)
			os.Exit(1)
		}
		serviceabilityProgramID = pk
	} else if networkConfig != nil {
		serviceabilityProgramID = networkConfig.ServiceabilityProgramID
	}

	parentDiscoveryEnabled := !geolocationProgramID.IsZero() && !serviceabilityProgramID.IsZero()

	if len(cliParentAuthorities) == 0 && !parentDiscoveryEnabled {
		log.Warn("No trusted parents configured and parent discovery disabled -- will not accept offsets until parents are added")
	}

	// Parse targets.
	targets, err := geoprobe.ParseProbeAddresses(*additionalTargets)
	if err != nil {
		log.Error("Failed to parse additional-targets", "error", err)
		os.Exit(1)
	}

	icmpTargets, err := geoprobe.ParseICMPProbeAddresses(*additionalIcmpTargets)
	if err != nil {
		log.Error("Failed to parse additional-icmp-targets", "error", err)
		os.Exit(1)
	}

	// Parse allowed pubkeys for signed TWAMP reflector.
	allowedKeys, err := parseAllowedPubkeys(*allowedPubkeysFlag)
	if err != nil {
		log.Error("Failed to parse allowed-pubkeys", "error", err)
		os.Exit(1)
	}

	log.Info("Starting geoprobe agent",
		"version", version,
		"cliParents", len(parents),
		"parentDiscovery", parentDiscoveryEnabled,
		"targets", len(targets),
		"icmpTargets", len(icmpTargets),
		"probeInterval", *probeInterval,
		"maxOffsetAge", *maxOffsetAge,
		"twampListenPort", *twampListenPort,
		"signedTWAMPListenPort", *signedTWAMPListenPort,
		"allowedPubkeys", len(allowedKeys),
		"udpListenPort", *udpListenPort,
		"authority_pubkey", keypair.PublicKey(),
		"geoprobe_pubkey", geoProbePubkey,
	)

	// Set up prometheus metrics server if enabled.
	if *metricsEnable {
		metrics.GeoProbeBuildInfo.WithLabelValues(version, commit, date).Set(1)
		go func() {
			listener, err := net.Listen("tcp", *metricsAddr)
			if err != nil {
				log.Error("Failed to start prometheus metrics server listener", "error", err)
				return
			}
			log.Info("Prometheus metrics server listening", "address", listener.Addr().String())
			mux := http.NewServeMux()
			mux.Handle("/metrics", promhttp.Handler())
			if err := http.Serve(listener, mux); err != nil {
				log.Error("Failed to start prometheus metrics server", "error", err)
			}
		}()
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Set up TWAMP reflector.
	reflector, err := twamplight.NewReflector(log, fmt.Sprintf("0.0.0.0:%d", *twampListenPort), defaultTWAMPReflectorTimeout)
	if err != nil {
		log.Error("Failed to create TWAMP reflector", "error", err)
		os.Exit(1)
	}

	// Set up Signed TWAMP reflector.
	signedSigner := signed.NewEd25519Signer(ed25519.PrivateKey(keypair))
	var geoprobePubkeyBytes [32]byte
	copy(geoprobePubkeyBytes[:], geoProbePubkey[:])
	signedReflector, err := signed.NewReflector(
		fmt.Sprintf("0.0.0.0:%d", *signedTWAMPListenPort),
		defaultTWAMPReflectorTimeout,
		signedSigner,
		geoprobePubkeyBytes,
		allowedKeys,
		*verifyInterval,
	)
	if err != nil {
		log.Error("Failed to create Signed TWAMP reflector", "error", err)
		os.Exit(1)
	}
	if *verbose {
		signedReflector.SetLogger(log)
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

	// Set up ICMP pinger for outbound ICMP targets.
	icmpPinger, err := geoprobe.NewICMPPinger(&geoprobe.ICMPPingerConfig{
		Logger:       log,
		ProbeTimeout: *twampSenderTimeout,
		BatchSize:    geoprobe.ICMPDefaultBatchSize,
		StaggerDelay: geoprobe.ICMPDefaultStaggerDelay,
	})
	if err != nil {
		log.Error("Failed to create ICMP pinger (CAP_NET_RAW may be missing)", "error", err)
		os.Exit(1)
	}
	defer icmpPinger.Close()
	for _, target := range icmpTargets {
		if err := icmpPinger.AddProbe(target); err != nil {
			log.Warn("Failed to add ICMP target probe", "target", target, "error", err)
		}
	}

	// Set up signer and RPC client.
	signer, err := geoprobe.NewOffsetSigner(keypair, geoProbePubkey)
	if err != nil {
		log.Error("Failed to create offset signer", "error", err)
		os.Exit(1)
	}
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

	errCh := make(chan error, 4)

	// Run TWAMP reflector.
	go func() {
		if err := reflector.Run(ctx); err != nil {
			errCh <- fmt.Errorf("TWAMP reflector: %w", err)
		}
	}()

	// Run Signed TWAMP reflector.
	go func() {
		if err := signedReflector.Run(ctx); err != nil {
			errCh <- fmt.Errorf("signed TWAMP reflector: %w", err)
		}
	}()

	// Run UDP offset listener.
	go func() {
		runOffsetListener(ctx, log, offsetListener, cache, pState, signedReflector)
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
					signedReflector.SetOffsets(marshalBestOffset(cache))
				}
			}
		}
	}()

	// Shared counter: parent discovery writes the GeoProbe target_update_count on each
	// poll; target discovery reads it to skip expensive full scans when unchanged.
	// Both discoveries run sequentially in a single goroutine to guarantee the
	// counter is updated before target discovery reads it.
	var probeTargetUpdateCount atomic.Uint32

	targetUpdateCh := make(chan geoprobe.TargetUpdate, 1)
	inboundKeyCh := make(chan geoprobe.InboundKeyUpdate, 1)
	parentUpdateCh := make(chan geoprobe.ParentUpdate, 1)
	icmpTargetUpdateCh := make(chan geoprobe.ICMPTargetUpdate, 1)

	// Build parent discovery if program IDs are configured.
	var pd *geoprobe.ParentDiscovery
	if parentDiscoveryEnabled {
		geoProbeClient := geoprobe.NewRPCGeoProbeClient(rpcClient, geolocationProgramID)
		deviceResolver := geoprobe.NewRPCDeviceResolver(rpcClient, serviceabilityProgramID)

		var pdErr error
		pd, pdErr = geoprobe.NewParentDiscovery(&geoprobe.ParentDiscoveryConfig{
			GeoProbePubkey:         geoProbePubkey,
			Client:                 geoProbeClient,
			Resolver:               deviceResolver,
			CLIParents:             cliParentAuthorities,
			Logger:                 log,
			ProbeTargetUpdateCount: &probeTargetUpdateCount,
		})
		if pdErr != nil {
			log.Error("Failed to create parent discovery", "error", pdErr)
			os.Exit(1)
		}

		// Consume parent updates: update parentState for OffsetListener validation.
		// Parent authority keys are NOT added to the signed TWAMP reflector — parent
		// DZDs use the unsigned reflector. The signed reflector's allowlist comes
		// from target discovery (inbound targets) and CLI --allowed-pubkeys only.
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case update := <-parentUpdateCh:
					pState.update(update.Authorities)
					metrics.GeoProbeParentsDiscovered.Set(float64(len(update.Authorities)))
					log.Info("Updated parent authorities from discovery",
						"totalParents", len(update.Authorities))
				}
			}
		}()
	}

	// Build target discovery if geolocation program ID is configured.
	var td *geoprobe.TargetDiscovery
	if !geolocationProgramID.IsZero() {
		geolocationUserClient := geolocation.New(log, rpcClient, geolocationProgramID)
		var tdErr error
		td, tdErr = geoprobe.NewTargetDiscovery(&geoprobe.TargetDiscoveryConfig{
			GeoProbePubkey:         geoProbePubkey,
			Client:                 geolocationUserClient,
			CLITargets:             targets,
			CLIIcmpTargets:         icmpTargets,
			CLIAllowedKeys:         allowedKeys,
			Logger:                 log,
			ProbeTargetUpdateCount: &probeTargetUpdateCount,
		})
		if tdErr != nil {
			log.Error("Failed to create target discovery", "error", tdErr)
			os.Exit(1)
		}
	}

	// Run parent and target discovery sequentially in a single goroutine so that
	// parent discovery always updates probeTargetUpdateCount before target
	// discovery reads it.
	go func() {
		tick := func() {
			if pd != nil {
				start := time.Now()
				pd.Tick(ctx, parentUpdateCh)
				metrics.GeoProbeParentDiscoveryDuration.Observe(time.Since(start).Seconds())
			}
			if td != nil {
				start := time.Now()
				td.Tick(ctx, targetUpdateCh, inboundKeyCh, icmpTargetUpdateCh)
				metrics.GeoProbeTargetDiscoveryDuration.Observe(time.Since(start).Seconds())
			}
		}

		tick()

		discoveryTicker := time.NewTicker(discoveryInterval)
		defer discoveryTicker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-discoveryTicker.C:
				tick()
			}
		}
	}()

	// Run main measurement loop. This runs regardless of whether trusted parents
	// are configured at startup, since they may be added dynamically at runtime.
	go func() {
		ml := &measurementLoop{
			ctx:                ctx,
			log:                log,
			pinger:             pinger,
			icmpPinger:         icmpPinger,
			cache:              cache,
			signer:             signer,
			senderConn:         senderConn,
			getCurrentSlot:     getCurrentSlot,
			signedReflector:    signedReflector,
			cliAllowedKeys:     allowedKeys,
			targets:            targets,
			icmpTargets:        icmpTargets,
			targetUpdateCh:     targetUpdateCh,
			icmpTargetUpdateCh: icmpTargetUpdateCh,
			inboundKeyCh:       inboundKeyCh,
		}
		if err := ml.run(); err != nil {
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
	parents *parentState,
	signedReflector signed.Reflector,
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
			metrics.GeoProbeErrors.WithLabelValues(metrics.GeoProbeErrorTypeOffsetReceive).Inc()
			continue
		}

		senderPK := solana.PublicKeyFromBytes(offset.SenderPubkey[:]).String()
		authorityPK := solana.PublicKeyFromBytes(offset.AuthorityPubkey[:]).String()

		log.Debug("received UDP offset packet", "from", addr, "sender_pubkey", senderPK, "authority_pubkey", authorityPK)

		// Verify the sender is a known parent and the authority matches.
		expectedAuthority, knownParent := parents.getAuthority(offset.SenderPubkey)
		if !knownParent {
			log.Debug("Rejecting offset from unknown parent", "sender_pubkey", senderPK, "addr", addr)
			metrics.GeoProbeOffsetsRejected.WithLabelValues(metrics.GeoProbeRejectUnknownParent).Inc()
			continue
		}
		if expectedAuthority != offset.AuthorityPubkey {
			log.Warn("Rejecting offset with wrong authority for parent",
				"sender_pubkey", senderPK,
				"expected_authority", solana.PublicKeyFromBytes(expectedAuthority[:]).String(),
				"actual_authority", authorityPK,
				"addr", addr)
			metrics.GeoProbeOffsetsRejected.WithLabelValues(metrics.GeoProbeRejectWrongAuthority).Inc()
			continue
		}

		// Verify signature chain (top-level and all references).
		if err := geoprobe.VerifyOffsetChain(offset); err != nil {
			log.Warn("Offset signature verification failed", "authority_pubkey", authorityPK, "addr", addr, "error", err)
			metrics.GeoProbeOffsetsRejected.WithLabelValues(metrics.GeoProbeRejectInvalidSignature).Inc()
			continue
		}

		log.Debug("signature verification successful", "authority_pubkey", authorityPK)

		cache.Put(offset)
		signedReflector.SetOffsets(marshalBestOffset(cache))
		metrics.GeoProbeOffsetsReceived.Inc()

		log.Debug("Cached DZD offset",
			"authority_pubkey", authorityPK,
			"sender_pubkey", senderPK,
			"addr", addr,
			"target_ip", geoprobe.FormatTargetIP(offset.TargetIP),
			"rtt_ns", offset.RttNs,
			"measured_rtt_ns", offset.MeasuredRttNs,
			"lat", offset.Lat,
			"lng", offset.Lng,
			"slot", offset.MeasurementSlot)
	}
}

type measurementLoop struct {
	ctx             context.Context
	log             *slog.Logger
	pinger          *geoprobe.Pinger
	icmpPinger      *geoprobe.ICMPPinger
	cache           *offsetCache
	signer          *geoprobe.OffsetSigner
	senderConn      *net.UDPConn
	getCurrentSlot  func(ctx context.Context) (uint64, error)
	signedReflector signed.Reflector
	cliAllowedKeys  [][32]byte

	targets     []geoprobe.ProbeAddress
	icmpTargets []geoprobe.ProbeAddress

	targetUpdateCh     <-chan geoprobe.TargetUpdate
	icmpTargetUpdateCh <-chan geoprobe.ICMPTargetUpdate
	inboundKeyCh       <-chan geoprobe.InboundKeyUpdate
}

func (ml *measurementLoop) reconcileTargets(
	oldTargets []geoprobe.ProbeAddress,
	newTargets []geoprobe.ProbeAddress,
	addProbe func(geoprobe.ProbeAddress) error,
	removeProbe func(geoprobe.ProbeAddress) error,
	measureOne func(geoprobe.ProbeAddress) (uint64, bool),
) ([]geoprobe.ProbeAddress, map[geoprobe.ProbeAddress]uint64) {
	oldSet := make(map[string]geoprobe.ProbeAddress, len(oldTargets))
	for _, t := range oldTargets {
		oldSet[t.String()] = t
	}
	newSet := make(map[string]geoprobe.ProbeAddress, len(newTargets))
	for _, t := range newTargets {
		newSet[t.String()] = t
	}

	var newlyAdded []geoprobe.ProbeAddress
	for key, addr := range newSet {
		if _, exists := oldSet[key]; !exists {
			if err := addProbe(addr); err != nil {
				ml.log.Warn("Failed to add discovered target probe", "target", addr, "error", err)
			} else {
				newlyAdded = append(newlyAdded, addr)
			}
		}
	}
	for key, addr := range oldSet {
		if _, exists := newSet[key]; !exists {
			if err := removeProbe(addr); err != nil {
				ml.log.Warn("Failed to remove stale target probe", "target", addr, "error", err)
			}
		}
	}

	var rttData map[geoprobe.ProbeAddress]uint64
	if len(newlyAdded) > 0 {
		rttData = make(map[geoprobe.ProbeAddress]uint64, len(newlyAdded))
		for _, addr := range newlyAdded {
			if rttNs, ok := measureOne(addr); ok {
				rttData[addr] = rttNs
			}
		}
	}
	return newTargets, rttData
}

func (ml *measurementLoop) run() error {
	measureTicker := time.NewTicker(*probeInterval)
	defer measureTicker.Stop()

	for {
		select {
		case <-ml.ctx.Done():
			return nil

		case <-measureTicker.C:
			ml.runCycle()

		case update := <-ml.targetUpdateCh:
			newTargets, rttData := ml.reconcileTargets(
				ml.targets,
				update.Targets,
				func(addr geoprobe.ProbeAddress) error { return ml.pinger.AddProbe(ml.ctx, addr) },
				ml.pinger.RemoveProbe,
				func(addr geoprobe.ProbeAddress) (uint64, bool) { return ml.pinger.MeasureOne(ml.ctx, addr) },
			)
			ml.targets = newTargets
			metrics.GeoProbeTargetsDiscovered.Set(float64(len(ml.targets)))
			ml.log.Info("Updated targets from discovery", "totalTargets", len(ml.targets))
			if len(rttData) > 0 {
				sendCompositeOffsets(ml.ctx, ml.log, rttData, ml.cache, ml.signer, ml.senderConn, ml.getCurrentSlot)
			}

		case icmpUpdate := <-ml.icmpTargetUpdateCh:
			newTargets, rttData := ml.reconcileTargets(
				ml.icmpTargets,
				icmpUpdate.Targets,
				func(addr geoprobe.ProbeAddress) error { return ml.icmpPinger.AddProbe(addr) },
				ml.icmpPinger.RemoveProbe,
				func(addr geoprobe.ProbeAddress) (uint64, bool) { return ml.icmpPinger.MeasureOne(ml.ctx, addr) },
			)
			ml.icmpTargets = newTargets
			metrics.GeoProbeIcmpTargetsDiscovered.Set(float64(len(ml.icmpTargets)))
			ml.log.Info("Updated ICMP targets from discovery", "totalIcmpTargets", len(ml.icmpTargets))
			if len(rttData) > 0 {
				sendCompositeOffsets(ml.ctx, ml.log, rttData, ml.cache, ml.signer, ml.senderConn, ml.getCurrentSlot)
			}

		case keyUpdate := <-ml.inboundKeyCh:
			ml.signedReflector.SetAuthorizedKeys(keyUpdate.Keys)
			ml.log.Info("Updated signed TWAMP authorized keys from discovery",
				"totalKeys", len(keyUpdate.Keys),
				"cliKeys", len(ml.cliAllowedKeys),
				"discoveredKeys", len(keyUpdate.Keys)-len(ml.cliAllowedKeys))
		}
	}
}

func (ml *measurementLoop) runCycle() {
	if len(ml.targets) == 0 && len(ml.icmpTargets) == 0 {
		ml.log.Debug("No targets configured, skipping measurement cycle")
		return
	}

	ml.log.Debug("Starting measurement cycle", "targets", len(ml.targets), "icmpTargets", len(ml.icmpTargets))
	start := time.Now()
	defer func() {
		metrics.GeoProbeMeasurementCycleDuration.Observe(time.Since(start).Seconds())
	}()

	rttData := make(map[geoprobe.ProbeAddress]uint64)

	if len(ml.targets) > 0 {
		twampResults, err := ml.pinger.MeasureAll(ml.ctx)
		if err != nil {
			ml.log.Error("Failed to measure TWAMP targets", "error", err)
			metrics.GeoProbeErrors.WithLabelValues(metrics.GeoProbeErrorTypeMeasurementCycle).Inc()
		} else {
			for k, v := range twampResults {
				rttData[k] = v
			}
		}
	}

	if len(ml.icmpTargets) > 0 {
		icmpStart := time.Now()
		icmpResults, err := ml.icmpPinger.MeasureAll(ml.ctx)
		metrics.GeoProbeIcmpMeasurementCycleDuration.Observe(time.Since(icmpStart).Seconds())
		if err != nil {
			ml.log.Error("Failed to measure ICMP targets", "error", err)
			metrics.GeoProbeErrors.WithLabelValues(metrics.GeoProbeErrorTypeIcmpMeasurementCycle).Inc()
		} else {
			for k, v := range icmpResults {
				rttData[k] = v
			}
		}
	}

	if len(rttData) == 0 {
		ml.log.Warn("No successful target measurements in cycle")
		return
	}

	for addr, rttNs := range rttData {
		ml.log.Debug("target measurement result", "target", addr.Host, "rtt_ms", float64(rttNs)/1000000.0)
	}

	sent := sendCompositeOffsets(ml.ctx, ml.log, rttData, ml.cache, ml.signer, ml.senderConn, ml.getCurrentSlot)

	ml.log.Info("Completed measurement cycle",
		"measured", len(rttData),
		"sent", sent,
		"total_targets", len(ml.targets),
		"total_icmp_targets", len(ml.icmpTargets))
}

func sendCompositeOffsets(
	ctx context.Context,
	log *slog.Logger,
	rttData map[geoprobe.ProbeAddress]uint64,
	cache *offsetCache,
	signer *geoprobe.OffsetSigner,
	senderConn *net.UDPConn,
	getCurrentSlot func(ctx context.Context) (uint64, error),
) int {
	dzdOffset := cache.GetBest()
	if dzdOffset == nil {
		log.Warn("No valid DZD offsets in cache, skipping composite generation")
		return 0
	}

	slot, err := getCurrentSlot(ctx)
	if err != nil {
		log.Error("Failed to get current slot", "error", err)
		metrics.GeoProbeErrors.WithLabelValues(metrics.GeoProbeErrorTypeSlotFetch).Inc()
		return 0
	}

	log.Debug("fetched current slot", "slot", slot)

	sentCount := 0
	for addr, measuredRttNs := range rttData {
		compositeOffset := geoprobe.LocationOffset{
			Version:         geoprobe.LocationOffsetVersion,
			MeasurementSlot: slot,
			MeasuredRttNs:   measuredRttNs,
			Lat:             dzdOffset.Lat,
			Lng:             dzdOffset.Lng,
			RttNs:           dzdOffset.RttNs + measuredRttNs,
			TargetIP:        geoprobe.IPToTargetIP(addr.Host),
			NumReferences:   1,
			References:      []geoprobe.LocationOffset{*dzdOffset},
		}

		if err := signer.SignOffset(&compositeOffset); err != nil {
			log.Error("Failed to sign composite offset", "target", addr, "error", err)
			metrics.GeoProbeErrors.WithLabelValues(metrics.GeoProbeErrorTypeSignOffset).Inc()
			continue
		}

		targetAddr := &net.UDPAddr{IP: net.ParseIP(addr.Host), Port: int(addr.Port)}
		if err := geoprobe.SendOffset(senderConn, targetAddr, &compositeOffset); err != nil {
			log.Error("Failed to send composite offset", "target", addr, "error", err)
			metrics.GeoProbeErrors.WithLabelValues(metrics.GeoProbeErrorTypeSendOffset).Inc()
			continue
		}

		sentCount++
		metrics.GeoProbeCompositeOffsetsSent.Inc()
		log.Debug("Sent composite offset to target",
			"target", addr,
			"slot", slot,
			"measured_rtt_ns", measuredRttNs,
			"total_rtt_ns", compositeOffset.RttNs,
			"lat", compositeOffset.Lat,
			"lng", compositeOffset.Lng,
			"ref_authority_pubkey", solana.PublicKeyFromBytes(dzdOffset.AuthorityPubkey[:]).String(),
			"ref_sender_pubkey", solana.PublicKeyFromBytes(dzdOffset.SenderPubkey[:]).String())
	}

	return sentCount
}
