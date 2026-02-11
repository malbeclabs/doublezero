package main

import (
	"context"
	"encoding/json"
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

	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/geoprobe"
	twamplight "github.com/malbeclabs/doublezero/tools/twamp/pkg/light"
)

const (
	defaultTWAMPPort         = 862
	defaultUDPPort           = 8923
	defaultTWAMPTimeout      = 1 * time.Second
	defaultRateLimit         = 10
	maxReferenceDepth        = 5
	speedOfLightMilesPerMs   = 124.0
	speedOfLightKmPerMs      = speedOfLightMilesPerMs * 1.60934
	nanosecondsPerMs         = 1000000.0
	rateLimitCleanupInterval = 5 * time.Minute
	rateLimitEntryTTL        = 10 * time.Minute
)

var (
	twampPort       = flag.Uint("twamp-port", defaultTWAMPPort, "Port to listen for TWAMP probes")
	udpPort         = flag.Uint("udp-port", defaultUDPPort, "Port to listen for LocationOffset UDP datagrams")
	logFormat       = flag.String("log-format", "text", "Log format: text or json")
	verifySignature = flag.Bool("verify-signatures", true, "Verify Ed25519 signatures on received offsets")
	rateLimit       = flag.Uint("rate-limit", defaultRateLimit, "Maximum packets per second per source IP")
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

	log := setupLogger(*logFormat)
	log.Info("starting geoprobe-target",
		"version", version,
		"commit", commit,
		"date", date,
		"twamp_port", *twampPort,
		"udp_port", *udpPort,
		"verify_signatures", *verifySignature,
		"rate_limit", *rateLimit,
		"max_reference_depth", maxReferenceDepth,
	)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	errCh := make(chan error, 2)

	limiter := newRateLimiter(*rateLimit)
	go limiter.cleanup(ctx)

	go runTWAMPReflector(ctx, log, *twampPort, errCh)
	go runUDPListener(ctx, log, *udpPort, *verifySignature, limiter, errCh)

	select {
	case err := <-errCh:
		log.Error("component failed", "error", err)
		os.Exit(1)
	case <-ctx.Done():
		log.Info("shutdown signal received, exiting gracefully")
	}
}

func setupLogger(format string) *slog.Logger {
	var handler slog.Handler
	opts := &slog.HandlerOptions{
		Level: slog.LevelInfo,
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

func runUDPListener(ctx context.Context, log *slog.Logger, port uint, verifySignatures bool, limiter *rateLimiter, errCh chan<- error) {
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
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				if err := conn.SetReadDeadline(time.Now().Add(1 * time.Second)); err != nil {
					errCh <- fmt.Errorf("failed to set read deadline: %w", err)
					return
				}
				continue
			}
			log.Warn("failed to receive offset", "error", err, "from", addr)
			continue
		}

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

		handleOffset(log, offset, addr, verifySignatures)
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

func handleOffset(log *slog.Logger, offset *geoprobe.LocationOffset, addr *net.UDPAddr, verifySignatures bool) {
	signatureValid := true
	var verifyError error

	if verifySignatures {
		verifyError = geoprobe.VerifyOffsetChain(offset)
		signatureValid = verifyError == nil
	}

	output := formatLocationOffset(offset, addr, signatureValid, verifyError)

	if *logFormat == "json" {
		data, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			log.Error("failed to marshal offset output", "error", err)
			return
		}
		fmt.Println(string(data))
	} else {
		fmt.Print(formatTextOutput(output))
	}

	log.Info("received LocationOffset",
		"from", addr,
		"probe_pubkey", output.ProbePubkey,
		"rtt_ms", output.RttMs,
		"max_distance_miles", output.MaxDistanceMiles,
		"signature_valid", signatureValid,
	)
}

type OffsetOutput struct {
	Timestamp         string             `json:"timestamp"`
	SourceAddr        string             `json:"source_addr"`
	ProbePubkey       string             `json:"probe_pubkey"`
	ReferencePoint    CoordinateOutput   `json:"reference_point"`
	RttMs             float64            `json:"rtt_ms"`
	MeasuredRttMs     float64            `json:"measured_rtt_ms"`
	MaxDistanceMiles  float64            `json:"max_distance_miles"`
	MaxDistanceKm     float64            `json:"max_distance_km"`
	MeasurementSlot   uint64             `json:"measurement_slot"`
	SignatureValid    bool               `json:"signature_valid"`
	SignatureError    string             `json:"signature_error,omitempty"`
	DZDReferenceChain []ReferenceOutput  `json:"dzd_reference_chain"`
}

type CoordinateOutput struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Formatted string  `json:"formatted"`
}

type ReferenceOutput struct {
	Pubkey         string           `json:"pubkey"`
	Location       CoordinateOutput `json:"location"`
	RttMs          float64          `json:"rtt_ms"`
	MeasuredRttMs  float64          `json:"measured_rtt_ms"`
}

func formatLocationOffset(offset *geoprobe.LocationOffset, addr *net.UDPAddr, signatureValid bool, verifyError error) OffsetOutput {
	rttMs := float64(offset.RttNs) / nanosecondsPerMs
	measuredRttMs := float64(offset.MeasuredRttNs) / nanosecondsPerMs
	maxDistanceMiles := calculateMaxDistance(offset.RttNs)
	maxDistanceKm := maxDistanceMiles * 1.60934

	output := OffsetOutput{
		Timestamp:      time.Now().UTC().Format("2006-01-02 15:04:05 MST"),
		SourceAddr:     addr.String(),
		ProbePubkey:    formatPubkey(offset.Pubkey[:]),
		ReferencePoint: formatCoordinate(offset.Lat, offset.Lng),
		RttMs:          rttMs,
		MeasuredRttMs:  measuredRttMs,
		MaxDistanceMiles: maxDistanceMiles,
		MaxDistanceKm:    maxDistanceKm,
		MeasurementSlot:  offset.MeasurementSlot,
		SignatureValid:   signatureValid,
	}

	if verifyError != nil {
		output.SignatureError = verifyError.Error()
	}

	for _, ref := range offset.References {
		refRttMs := float64(ref.RttNs) / nanosecondsPerMs
		refMeasuredRttMs := float64(ref.MeasuredRttNs) / nanosecondsPerMs
		output.DZDReferenceChain = append(output.DZDReferenceChain, ReferenceOutput{
			Pubkey:        formatPubkey(ref.Pubkey[:]),
			Location:      formatCoordinate(ref.Lat, ref.Lng),
			RttMs:         refRttMs,
			MeasuredRttMs: refMeasuredRttMs,
		})
	}

	return output
}

func formatTextOutput(output OffsetOutput) string {
	var sb strings.Builder

	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("[%s] Received LocationOffset from Probe\n", output.Timestamp))
	sb.WriteString(fmt.Sprintf("  Probe: %s\n", output.ProbePubkey))
	sb.WriteString(fmt.Sprintf("  Reference Point: %s\n", output.ReferencePoint.Formatted))
	sb.WriteString(fmt.Sprintf("  RTT to Target: %.2fms\n", output.RttMs))
	sb.WriteString(fmt.Sprintf("  Max Distance: %.0f miles (%.0f km)\n", output.MaxDistanceMiles, output.MaxDistanceKm))
	sb.WriteString(fmt.Sprintf("  Measurement Slot: %d\n", output.MeasurementSlot))
	sb.WriteString("\n")

	if len(output.DZDReferenceChain) > 0 {
		sb.WriteString("  DZD Reference Chain:\n")
		for i, ref := range output.DZDReferenceChain {
			sb.WriteString(fmt.Sprintf("    [%d] DZD: %s\n", i+1, ref.Pubkey))
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
	rttMs := float64(rttNs) / nanosecondsPerMs
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

func formatPubkey(pubkey []byte) string {
	if len(pubkey) < 8 {
		return fmt.Sprintf("%x", pubkey)
	}
	prefix := fmt.Sprintf("%x", pubkey[:4])
	suffix := fmt.Sprintf("%x", pubkey[len(pubkey)-4:])
	return fmt.Sprintf("%s...%s", prefix, suffix)
}
