package main

import (
	"context"
	"encoding/json"
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
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/geoprobe"
	twamplight "github.com/malbeclabs/doublezero/tools/twamp/pkg/light"
)

const (
	defaultTWAMPPort         = 8925
	defaultUDPPort           = 8923
	defaultTWAMPTimeout      = 1 * time.Second
	defaultRateLimit         = 10
	maxReferenceDepth        = 5
	speedOfLightMilesPerMs   = 124.0
	nanosecondsPerMs         = 1000000.0
	rateLimitCleanupInterval = 5 * time.Minute
	rateLimitEntryTTL        = 10 * time.Minute

	// dzSlotDuration is the nominal DoubleZero Ledger slot time.
	dzSlotDuration = 400 * time.Millisecond

	// RFC-16's replay bound, derived from the offset stream instead of a ledger
	// clock: geoprobe-target holds no RPC connection by design, so the only
	// time reference it has is the highest MeasurementSlot a signing key has
	// proven. A replay can repeat that floor but cannot push it forward without
	// the corresponding private key.
	//
	// maxSlotRegression is how far below the floor an offer may sit and still be
	// accepted. A probe re-reads the slot from a load-balanced RPC pool, so a
	// lagging finalized replica can hand it a slot slightly behind the last one;
	// without slack that would silently stop the sender's ingestion.
	maxSlotRegression = uint64(2 * time.Minute / dzSlotDuration)

	// maxFloorStall is how long the floor may stand still before repeats stop
	// counting as live. A healthy sender stamps from geoprobe.SlotCacheTTL, so
	// the floor advances every ~5m and the steady-state replay window is ~7m.
	// The stall bound only governs the degraded case where the sending probe
	// rides out its own RPC outage on a frozen cached slot: six refresh periods
	// keeps ingesting its genuine measurements, because a dropped measurement
	// is lost permanently rather than deferred.
	maxFloorStall = 6 * geoprobe.SlotCacheTTL

	// floorEntryTTL is how long a silent sender's floor is remembered. Only
	// total silence expires it — any offer, accepted or rejected, keeps it
	// alive — so this bounds the map, not the replay window. Kept off
	// -max-offset-age, which tunes the display cache and would otherwise let a
	// cache setting shorten a security bound.
	floorEntryTTL = 2 * maxFloorStall
)

// Machine-readable rejection reasons, logged as the "reason" field so an
// operator can alert on a sender's measurements going missing. geoprobe-target
// exposes no prometheus metrics, so these log lines are the only signal.
const (
	rejectSlotRegressed = "slot_regressed"
	rejectFloorStalled  = "floor_stalled"
)

// signatureUnverifiedMarker records in signature_error that no verification ran
// at all, distinguishing a -verify-signatures=false row from a historical row
// whose verification genuinely failed.
const signatureUnverifiedMarker = "signature verification disabled"

var (
	twampPort       = flag.Uint("twamp-port", defaultTWAMPPort, "Port to listen for TWAMP probes")
	udpPort         = flag.Uint("udp-port", defaultUDPPort, "Port to listen for LocationOffset UDP datagrams")
	logFormat       = flag.String("log-format", "text", "Log format: text or json")
	verifySignature = flag.Bool("verify-signatures", true, "Verify Ed25519 signatures on received offsets")
	rateLimit       = flag.Uint("rate-limit", defaultRateLimit, "Maximum packets per second per source IP (0 disables rate limiting)")
	maxOffsetAge    = flag.Duration("max-offset-age", 1*time.Hour, "TTL for cached offsets; best/second-best tracking window")
	verbose         = flag.Bool("verbose", false, "Enable verbose logging")
	showVersion     = flag.Bool("version", false, "Print version and exit")

	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	flag.Parse()

	if *showVersion {
		fmt.Printf("version: %s, commit: %s, date: %s\n", version, commit, date)
		os.Exit(0)
	}

	if *twampPort == 0 || *twampPort > 65535 {
		fmt.Fprintf(os.Stderr, "invalid twamp-port: must be 1-65535\n")
		os.Exit(1)
	}
	if *udpPort == 0 || *udpPort > 65535 {
		fmt.Fprintf(os.Stderr, "invalid udp-port: must be 1-65535\n")
		os.Exit(1)
	}

	log := setupLogger(*logFormat, *verbose)
	log.Info("starting geoprobe-target",
		"version", version,
		"commit", commit,
		"date", date,
		"twamp_port", *twampPort,
		"udp_port", *udpPort,
		"verify_signatures", *verifySignature,
		"rate_limit", *rateLimit,
		"max_reference_depth", maxReferenceDepth,
		"max_offset_age", *maxOffsetAge,
		"max_slot_regression", maxSlotRegression,
		"max_floor_stall", maxFloorStall,
		"floor_entry_ttl", floorEntryTTL,
	)

	// Keyed by SenderPubkey (geoprobe identity). Each geoprobe is an independent
	// measurement stream. Uses RttNs (accumulated from the DZD root of trust),
	// not MeasuredRttNs (single hop), because the geolocation constraint is the
	// total distance from the DZD's known coordinates to this target.
	caches := geoprobe.NewMinCacheMap[[32]byte, geoprobe.LocationOffset](*maxOffsetAge, func(o geoprobe.LocationOffset) uint64 {
		return o.RttNs
	})

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	var chWriter *geoprobe.ClickhouseWriter
	if chCfg := geoprobe.ClickhouseConfigFromEnv(); chCfg != nil {
		log.Info("clickhouse enabled", "addr", chCfg.Addr, "db", chCfg.Database)
		chWriter = geoprobe.NewClickhouseWriter(*chCfg, log)
		go chWriter.Run(ctx)
	}

	errCh := make(chan error, 2)

	limiter := newRateLimiter(*rateLimit)
	if *rateLimit > 0 {
		go limiter.cleanup(ctx)
	}
	floor := newSlotFloor(floorEntryTTL)
	go sweepCaches(ctx, caches, floor)

	go runTWAMPReflector(ctx, log, *twampPort, errCh)
	go runUDPListener(ctx, log, *udpPort, *verifySignature, limiter, chWriter, caches, floor, errCh)

	select {
	case err := <-errCh:
		log.Error("component failed", "error", err)
		os.Exit(1)
	case <-ctx.Done():
		log.Info("shutdown signal received, exiting gracefully")
	}
}

func setupLogger(format string, debug bool) *slog.Logger {
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	var handler slog.Handler
	opts := &slog.HandlerOptions{
		Level: level,
	}

	switch format {
	case "json":
		handler = slog.NewJSONHandler(os.Stdout, opts)
	case "text":
		handler = slog.NewTextHandler(os.Stdout, opts)
	default:
		fmt.Fprintf(os.Stderr, "invalid log-format: %s (use 'text' or 'json')\n", format)
		os.Exit(1)
	}

	return slog.New(handler)
}

type rateLimiterEntry struct {
	tokens     uint
	lastUpdate time.Time
}

type rateLimiter struct {
	maxTokens uint
	entries   map[string]*rateLimiterEntry
	mu        sync.RWMutex
}

func newRateLimiter(maxTokens uint) *rateLimiter {
	return &rateLimiter{
		maxTokens: maxTokens,
		entries:   make(map[string]*rateLimiterEntry),
	}
}

func (rl *rateLimiter) allow(ip string) bool {
	if rl.maxTokens == 0 {
		return true
	}
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	entry, exists := rl.entries[ip]

	if !exists {
		rl.entries[ip] = &rateLimiterEntry{
			tokens:     rl.maxTokens - 1,
			lastUpdate: now,
		}
		return true
	}

	elapsed := now.Sub(entry.lastUpdate).Seconds()
	tokensToAdd := uint(elapsed * float64(rl.maxTokens))

	if tokensToAdd > 0 {
		entry.tokens += tokensToAdd
		if entry.tokens > rl.maxTokens {
			entry.tokens = rl.maxTokens
		}
		entry.lastUpdate = now
	}

	if entry.tokens > 0 {
		entry.tokens--
		return true
	}

	return false
}

func (rl *rateLimiter) cleanup(ctx context.Context) {
	ticker := time.NewTicker(rateLimitCleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			rl.mu.Lock()
			now := time.Now()
			for ip, entry := range rl.entries {
				if now.Sub(entry.lastUpdate) > rateLimitEntryTTL {
					delete(rl.entries, ip)
				}
			}
			rl.mu.Unlock()
		}
	}
}

type floorEntry struct {
	slot       uint64
	advancedAt time.Time
	lastSeen   time.Time
}

// slotFloor bounds offset replay without a ledger clock. MeasurementSlot is
// inside the signed payload, so the highest slot a key has proven is a lower
// bound on real time that a replay can repeat but cannot advance.
//
// Floors are keyed by AuthorityPubkey, the key VerifyOffsetChain actually
// checks the signature against — not by SenderPubkey, which is signed but
// unauthenticated. Anyone can mint a keypair and stamp a real geoprobe's
// SenderPubkey, so a SenderPubkey-keyed floor would let one datagram carrying
// a huge slot lock that geoprobe out of the table permanently. In production a
// probe signs its own offsets, so the two keys move together for real senders.
type slotFloor struct {
	mu      sync.Mutex
	entries map[[32]byte]*floorEntry
	ttl     time.Duration
	nowFunc func() time.Time // for testing; defaults to time.Now
}

func newSlotFloor(ttl time.Duration) *slotFloor {
	return &slotFloor{
		entries: make(map[[32]byte]*floorEntry),
		ttl:     ttl,
		nowFunc: time.Now,
	}
}

// accept reports whether an offer may be ingested, advancing the signing key's
// floor when it does. On rejection it returns a reason token plus the floor
// state, for the log line.
//
// Callers verify the signature chain first, so in the deployed configuration
// only a proven slot moves a floor. With -verify-signatures=false nothing is
// checked and the floor is fed unverified slots along with everything else.
//
// A never-seen key seeds its floor from its own first offer, so one stale
// capture is accepted per key per process restart; the live stream raises the
// floor past it within minutes. A rejected offer still refreshes lastSeen, so a
// sustained replay cannot outlive the entry and reseed from itself.
func (f *slotFloor) accept(authority [32]byte, slot uint64) (ok bool, reason string, floorSlot uint64, floorAge time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()

	now := f.nowFunc()
	entry, exists := f.entries[authority]
	if !exists {
		f.entries[authority] = &floorEntry{slot: slot, advancedAt: now, lastSeen: now}
		return true, "", slot, 0
	}
	entry.lastSeen = now

	age := now.Sub(entry.advancedAt)
	switch {
	case slot > entry.slot:
		entry.slot = slot
		entry.advancedAt = now
		age = 0
	case entry.slot-slot > maxSlotRegression:
		return false, rejectSlotRegressed, entry.slot, age
	case age > maxFloorStall:
		// The floor has not moved in maxFloorStall, so repeats no longer
		// evidence a live sender. Only a strictly higher slot does.
		return false, rejectFloorStalled, entry.slot, age
	}

	return true, "", entry.slot, age
}

// sweep drops keys silent for ttl, bounding the map the same way the offset
// caches are bounded.
func (f *slotFloor) sweep() {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := f.nowFunc()
	for k, entry := range f.entries {
		if now.Sub(entry.lastSeen) > f.ttl {
			delete(f.entries, k)
		}
	}
}

func sweepCaches(ctx context.Context, caches *geoprobe.MinCacheMap[[32]byte, geoprobe.LocationOffset], floor *slotFloor) {
	ticker := time.NewTicker(rateLimitCleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			caches.Sweep()
			floor.sweep()
		}
	}
}

func runTWAMPReflector(ctx context.Context, log *slog.Logger, port uint, errCh chan<- error) {
	addr := fmt.Sprintf("0.0.0.0:%d", port)
	reflector, err := twamplight.NewReflector(log, addr, defaultTWAMPTimeout)
	if err != nil {
		errCh <- fmt.Errorf("failed to create TWAMP reflector: %w", err)
		return
	}
	defer reflector.Close()

	log.Info("TWAMP reflector started", "addr", reflector.LocalAddr())

	if err := reflector.Run(ctx); err != nil {
		if ctx.Err() == nil {
			errCh <- fmt.Errorf("TWAMP reflector error: %w", err)
		}
	}
}

func runUDPListener(ctx context.Context, log *slog.Logger, port uint, verifySignatures bool, limiter *rateLimiter, chWriter *geoprobe.ClickhouseWriter, caches *geoprobe.MinCacheMap[[32]byte, geoprobe.LocationOffset], floor *slotFloor, errCh chan<- error) {
	conn, err := geoprobe.NewUDPListener(int(port))
	if err != nil {
		errCh <- fmt.Errorf("failed to create UDP listener: %w", err)
		return
	}
	defer conn.Close()

	log.Info("UDP listener started", "port", port, "verify_signatures", verifySignatures)

	if err := conn.SetReadDeadline(time.Now().Add(1 * time.Second)); err != nil {
		errCh <- fmt.Errorf("failed to set read deadline: %w", err)
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		offset, addr, err := geoprobe.ReceiveOffset(conn)
		if err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				if err := conn.SetReadDeadline(time.Now().Add(1 * time.Second)); err != nil {
					errCh <- fmt.Errorf("failed to set read deadline: %w", err)
					return
				}
				continue
			}
			log.Warn("failed to receive offset", "error", err, "from", addr)
			continue
		}

		log.Debug("received UDP packet", "from", addr, "sender_pubkey", solana.PublicKeyFromBytes(offset.SenderPubkey[:]).String(), "authority_pubkey", solana.PublicKeyFromBytes(offset.AuthorityPubkey[:]).String())

		if err := conn.SetReadDeadline(time.Now().Add(1 * time.Second)); err != nil {
			errCh <- fmt.Errorf("failed to set read deadline: %w", err)
			return
		}

		sourceIP := addr.IP.String()
		if !limiter.allow(sourceIP) {
			log.Warn("rate limit exceeded",
				"from", addr,
				"limit", limiter.maxTokens,
			)
			continue
		}

		depth := countReferenceDepth(offset)
		if depth > maxReferenceDepth {
			log.Warn("reference chain too deep",
				"from", addr,
				"depth", depth,
				"max", maxReferenceDepth,
			)
			continue
		}

		handleOffset(log, offset, addr, verifySignatures, chWriter, caches, floor)
	}
}

func countReferenceDepth(offset *geoprobe.LocationOffset) int {
	if len(offset.References) == 0 {
		return 0
	}
	maxDepth := 0
	for i := range offset.References {
		depth := countReferenceDepth(&offset.References[i])
		if depth > maxDepth {
			maxDepth = depth
		}
	}
	return maxDepth + 1
}

func handleOffset(log *slog.Logger, offset *geoprobe.LocationOffset, addr *net.UDPAddr, verifySignatures bool, chWriter *geoprobe.ClickhouseWriter, caches *geoprobe.MinCacheMap[[32]byte, geoprobe.LocationOffset], floor *slotFloor) {
	// signature_valid is an assertion the row carries into the public
	// location_offsets table, so it may only be true when a check actually ran.
	// With -verify-signatures=false nothing is checked: record false and say
	// why, rather than asserting a verification that did not happen.
	signatureValid := false
	signatureError := ""

	if verifySignatures {
		// Until the chain verifies, a LocationOffset is just an attacker-chosen
		// UDP datagram. Drop it instead of recording it: every row written to
		// location_offsets is aggregated by the public lake explorer without
		// filtering on signature_valid, so persisting forgeries would publish
		// them.
		if err := geoprobe.VerifyOffsetChain(offset); err != nil {
			log.Warn("dropping offset with invalid signature chain",
				"from", addr,
				"authority_pubkey", solana.PublicKeyFromBytes(offset.AuthorityPubkey[:]).String(),
				"sender_pubkey", solana.PublicKeyFromBytes(offset.SenderPubkey[:]).String(),
				"error", err)
			return
		}
		signatureValid = true
		log.Debug("signature verification complete", "authority_pubkey", solana.PublicKeyFromBytes(offset.AuthorityPubkey[:]).String(), "valid", true)
	} else {
		signatureError = signatureUnverifiedMarker
	}

	// A valid signature never expires, so a captured offset stays verifiable
	// forever. The slot floor is what stops it being replayed into the table.
	if ok, reason, floorSlot, floorAge := floor.accept(offset.AuthorityPubkey, offset.MeasurementSlot); !ok {
		log.Warn("dropping offset outside slot floor",
			"reason", reason,
			"from", addr,
			"authority_pubkey", solana.PublicKeyFromBytes(offset.AuthorityPubkey[:]).String(),
			"sender_pubkey", solana.PublicKeyFromBytes(offset.SenderPubkey[:]).String(),
			"offset_slot", offset.MeasurementSlot,
			"floor_slot", floorSlot,
			"floor_age_seconds", floorAge.Seconds())
		return
	}

	if chWriter != nil {
		rawBytes, err := offset.Marshal()
		if err != nil {
			log.Error("failed to marshal offset for clickhouse", "error", err)
		} else {
			row := geoprobe.OffsetRowFromLocationOffset(offset, addr.String(), signatureValid, signatureError, rawBytes)
			chWriter.Record(row)
		}
	}

	cache := caches.Get(offset.SenderPubkey)
	info := cache.Update(*offset)

	output := formatLocationOffset(offset, addr, signatureValid, signatureError)

	if *verbose || info.Changed() {
		if *logFormat == "json" {
			output.CacheUpdate = info.Result.String()
			if info.Promoted {
				output.CacheUpdate = "promoted+" + info.Result.String()
			}
			if info.Changed() && info.HadPrevBest {
				prevMs := float64(info.PrevBestRttNs) / nanosecondsPerMs
				output.PreviousBestRttMs = &prevMs
			}
			data, err := json.MarshalIndent(output, "", "  ")
			if err != nil {
				log.Error("failed to marshal offset output", "error", err)
				return
			}
			fmt.Println(string(data))
		} else {
			text := formatTextOutput(output)
			if *verbose {
				text += fmt.Sprintf("  Cache: result=%s promoted=%v\n\n", info.Result.String(), info.Promoted)
			} else if info.Promoted {
				// A promotion implies the old best expired, so there is no
				// non-stale previous best RTT to report.
				text += "  * Backup promoted to best\n\n"
			} else if info.Result == geoprobe.UpdateBest && info.HadPrevBest {
				text += fmt.Sprintf("  * New best measurement (previous best: %.3fms)\n\n", float64(info.PrevBestRttNs)/nanosecondsPerMs)
			}
			fmt.Print(text)
		}
	}

	log.Info("received LocationOffset",
		"from", addr,
		"authority_pubkey", output.AuthorityPubkey,
		"sender_pubkey", output.SenderPubkey,
		"target_ip", output.TargetIP,
		"rtt_ms", output.RttMs,
		"max_distance_miles", output.MaxDistanceMiles,
		"signature_valid", signatureValid,
		"cache_result", info.Result.String(),
		"cache_promoted", info.Promoted,
	)
	log.Debug("offset processed successfully", "from", addr, "authority_pubkey", solana.PublicKeyFromBytes(offset.AuthorityPubkey[:]).String(), "rtt_ms", float64(offset.RttNs)/1000000.0)
}

type OffsetOutput struct {
	Timestamp         string            `json:"timestamp"`
	SourceAddr        string            `json:"source_addr"`
	AuthorityPubkey   string            `json:"authority_pubkey"`
	SenderPubkey      string            `json:"sender_pubkey"`
	TargetIP          string            `json:"target_ip"`
	ReferencePoint    CoordinateOutput  `json:"reference_point"`
	RttMs             float64           `json:"rtt_ms"`
	MeasuredRttMs     float64           `json:"measured_rtt_ms"`
	MaxDistanceMiles  float64           `json:"max_distance_miles"`
	MaxDistanceKm     float64           `json:"max_distance_km"`
	MeasurementSlot   uint64            `json:"measurement_slot"`
	SignatureValid    bool              `json:"signature_valid"`
	SignatureError    string            `json:"signature_error,omitempty"`
	DZDReferenceChain []ReferenceOutput `json:"dzd_reference_chain"`
	CacheUpdate       string            `json:"cache_update,omitempty"`
	PreviousBestRttMs *float64          `json:"previous_best_rtt_ms,omitempty"`
}

type CoordinateOutput struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Formatted string  `json:"formatted"`
}

type ReferenceOutput struct {
	AuthorityPubkey string           `json:"authority_pubkey"`
	SenderPubkey    string           `json:"sender_pubkey"`
	TargetIP        string           `json:"target_ip"`
	Location        CoordinateOutput `json:"location"`
	RttMs           float64          `json:"rtt_ms"`
	MeasuredRttMs   float64          `json:"measured_rtt_ms"`
}

func formatLocationOffset(offset *geoprobe.LocationOffset, addr *net.UDPAddr, signatureValid bool, signatureError string) OffsetOutput {
	rttMs := float64(offset.RttNs) / nanosecondsPerMs
	measuredRttMs := float64(offset.MeasuredRttNs) / nanosecondsPerMs
	maxDistanceMiles := calculateMaxDistance(offset.RttNs)
	maxDistanceKm := maxDistanceMiles * 1.60934

	output := OffsetOutput{
		Timestamp:        time.Now().UTC().Format("2006-01-02 15:04:05 MST"),
		SourceAddr:       addr.String(),
		AuthorityPubkey:  solana.PublicKeyFromBytes(offset.AuthorityPubkey[:]).String(),
		SenderPubkey:     solana.PublicKeyFromBytes(offset.SenderPubkey[:]).String(),
		TargetIP:         geoprobe.FormatTargetIP(offset.TargetIP),
		ReferencePoint:   formatCoordinate(offset.Lat, offset.Lng),
		RttMs:            rttMs,
		MeasuredRttMs:    measuredRttMs,
		MaxDistanceMiles: maxDistanceMiles,
		MaxDistanceKm:    maxDistanceKm,
		MeasurementSlot:  offset.MeasurementSlot,
		SignatureValid:   signatureValid,
	}

	output.SignatureError = signatureError

	for _, ref := range offset.References {
		refRttMs := float64(ref.RttNs) / nanosecondsPerMs
		refMeasuredRttMs := float64(ref.MeasuredRttNs) / nanosecondsPerMs
		output.DZDReferenceChain = append(output.DZDReferenceChain, ReferenceOutput{
			AuthorityPubkey: solana.PublicKeyFromBytes(ref.AuthorityPubkey[:]).String(),
			SenderPubkey:    solana.PublicKeyFromBytes(ref.SenderPubkey[:]).String(),
			TargetIP:        geoprobe.FormatTargetIP(ref.TargetIP),
			Location:        formatCoordinate(ref.Lat, ref.Lng),
			RttMs:           refRttMs,
			MeasuredRttMs:   refMeasuredRttMs,
		})
	}

	return output
}

func formatTextOutput(output OffsetOutput) string {
	var sb strings.Builder

	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("[%s] Received LocationOffset from Probe\n", output.Timestamp))
	sb.WriteString(fmt.Sprintf("  Authority: %s\n", output.AuthorityPubkey))
	sb.WriteString(fmt.Sprintf("  Sender:    %s\n", output.SenderPubkey))
	sb.WriteString(fmt.Sprintf("  Target IP: %s\n", output.TargetIP))
	sb.WriteString(fmt.Sprintf("  Reference Point: %s\n", output.ReferencePoint.Formatted))
	sb.WriteString(fmt.Sprintf("  RTT to Target: %.2fms\n", output.RttMs))
	sb.WriteString(fmt.Sprintf("  Measured RTT:  %.2fms\n", output.MeasuredRttMs))
	sb.WriteString(fmt.Sprintf("  Max Distance: %.0f miles (%.0f km)\n", output.MaxDistanceMiles, output.MaxDistanceKm))
	sb.WriteString(fmt.Sprintf("  Measurement Slot: %d\n", output.MeasurementSlot))
	sb.WriteString("\n")

	if len(output.DZDReferenceChain) > 0 {
		sb.WriteString("  DZD Reference Chain:\n")
		for i, ref := range output.DZDReferenceChain {
			sb.WriteString(fmt.Sprintf("    [%d] Authority: %s\n", i+1, ref.AuthorityPubkey))
			sb.WriteString(fmt.Sprintf("        Sender:    %s\n", ref.SenderPubkey))
			sb.WriteString(fmt.Sprintf("        Location: %s\n", ref.Location.Formatted))
			sb.WriteString(fmt.Sprintf("        DZD→Probe RTT: %.2fms\n", ref.MeasuredRttMs))
		}
		sb.WriteString("\n")
	}

	if output.SignatureValid {
		sb.WriteString("  Signature: VALID ✓\n")
		if len(output.DZDReferenceChain) > 0 {
			sb.WriteString("  Chain Verification: VALID ✓\n")
		}
	} else {
		sb.WriteString("  Signature: INVALID ✗\n")
		if output.SignatureError != "" {
			sb.WriteString(fmt.Sprintf("  Error: %s\n", output.SignatureError))
		}
	}
	sb.WriteString("\n")

	return sb.String()
}

func calculateMaxDistance(rttNs uint64) float64 {
	rttMs := float64(rttNs) / (2 * nanosecondsPerMs)
	return rttMs * speedOfLightMilesPerMs
}

func formatCoordinate(lat, lng float64) CoordinateOutput {
	latDir := "N"
	if lat < 0 {
		latDir = "S"
		lat = -lat
	}

	lngDir := "E"
	if lng < 0 {
		lngDir = "W"
		lng = -lng
	}

	formatted := fmt.Sprintf("%.4f°%s, %.4f°%s", lat, latDir, lng, lngDir)

	if latDir == "S" {
		lat = -lat
	}
	if lngDir == "W" {
		lng = -lng
	}

	return CoordinateOutput{
		Latitude:  lat,
		Longitude: lng,
		Formatted: formatted,
	}
}
