package main

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"math"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gagliardetto/solana-go"

	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/geoprobe"
	"github.com/malbeclabs/doublezero/tools/twamp/pkg/signed"
)

const (
	defaultProbePort = 8924
	defaultInterval  = 30 * time.Second
	defaultTimeout   = 2 * time.Second

	// Illustrative speed-of-light constants for converting RTT to an upper-bound
	// distance estimate. Real-world distance will be shorter because signals
	// travel through fiber (~0.67c) and encounter switching/queuing delays.
	speedOfLightMilesPerMs = 124.0
	nanosecondsPerMs       = 1_000_000.0
	kmPerMile              = 1.60934
)

var (
	probeIP           = flag.String("probe-ip", "", "IP address of the GeoProbe to probe (required)")
	probePort         = flag.Uint("probe-port", defaultProbePort, "TWAMP port on the probe")
	probePK           = flag.String("probe-pk", "", "Base58 Ed25519 public key of the GeoProbe's signing authority (required)")
	keypairPath       = flag.String("keypair", "", "Path to this target's Ed25519 keypair file for signing outbound message (required)")
	interval          = flag.Duration("interval", defaultInterval, "Interval between probe pairs")
	count             = flag.Uint("count", 0, "Number of probe pairs to send (0 = infinite)")
	timeout           = flag.Duration("timeout", defaultTimeout, "Per-probe timeout")
	maxMeasurementAge = flag.Duration("max-measurement-age", 1*time.Hour, "TTL for measurement tracking; best/second-best window")
	logFormat         = flag.String("log-format", "text", "Log format: text or json")
	verbose           = flag.Bool("verbose", false, "Enable debug logging")
	showVersion       = flag.Bool("version", false, "Print version and exit")

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

	if err := validateFlags(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		flag.Usage()
		os.Exit(1)
	}

	log := setupLogger(*logFormat, *verbose)
	log.Info("starting geoprobe-target-sender",
		"version", version,
		"commit", commit,
		"probe_ip", *probeIP,
		"probe_port", *probePort,
		"interval", *interval,
		"timeout", *timeout,
		"count", *count,
		"max_measurement_age", *maxMeasurementAge,
	)

	cache := geoprobe.NewMinCache[measurement](*maxMeasurementAge, func(m measurement) uint64 {
		return m.probeMeasuredRttNs
	})

	// Load target keypair.
	keypair, err := solana.PrivateKeyFromSolanaKeygenFile(*keypairPath)
	if err != nil {
		log.Error("failed to load keypair", "error", err)
		os.Exit(1)
	}
	signer := signed.NewEd25519Signer(ed25519.PrivateKey(keypair))

	// Parse probe public key.
	remotePubkey, err := parsePubkey(*probePK)
	if err != nil {
		log.Error("failed to parse probe-pk", "error", err)
		os.Exit(1)
	}

	log.Info("target identity",
		"pubkey", solana.PublicKeyFromBytes(signer.Public()).String(),
	)
	log.Info("probe identity",
		"pubkey", *probePK,
	)

	// Create sender.
	remoteAddr := &net.UDPAddr{
		IP:   net.ParseIP(*probeIP),
		Port: int(*probePort),
	}
	localAddr := &net.UDPAddr{Port: 0}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	sender, err := signed.NewSender(ctx, "", localAddr, remoteAddr, signer, remotePubkey)
	if err != nil {
		log.Error("failed to create sender", "error", err)
		os.Exit(1)
	}
	defer sender.Close()
	sender.SetLogger(log)

	log.Info("sending paired probes", "target", remoteAddr.String())

	// Paired probe loop.
	var seq uint32
	for {
		select {
		case <-ctx.Done():
			log.Info("shutdown signal received, exiting gracefully")
			return
		default:
		}

		seq++
		log.Debug("starting probe pair iteration", "seq", seq, "target", remoteAddr.String())
		probePair(ctx, log, sender, seq, cache)

		if *count > 0 && seq >= uint32(*count) {
			log.Info("completed all probe pairs", "count", *count)
			return
		}

		select {
		case <-ctx.Done():
			log.Info("shutdown signal received, exiting gracefully")
			return
		case <-time.After(*interval):
		}
	}
}

// probePair sends two probes in quick succession and logs the combined result.
// Both probes are pre-signed before any network I/O so that probe 1 fires
// immediately after reply 0 arrives. Reply 1's SinceLastRxNs gives the
// probe-measured RTT.
func probePair(ctx context.Context, log *slog.Logger, sender signed.Sender, seq uint32, cache *geoprobe.MinCache[measurement]) {
	probeCtx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()

	log.Debug("sending probe pair", "seq", seq)

	result, err := sender.ProbePair(probeCtx)
	if err != nil {
		logProbeError(log, seq, err)
		return
	}

	// Verify both replies (defense-in-depth: LinuxSender already verifies,
	// but we check independently for audit logging).
	reply0ProbeSigValid := result.Reply0.Probe.Verify()
	reply0SigValid := result.Reply0.Verify()
	reply1ProbeSigValid := result.Reply1.Probe.Verify()
	reply1SigValid := result.Reply1.Verify()

	// Probe Measured RTT: Tx-to-Rx interval at the reflector (from reply 1).
	probeMeasuredRttNs := result.Reply1.SinceLastRxNs

	// Target Measured RTT: lower of the two sender-measured RTTs.
	targetMeasuredRtt := min(result.RTT0, result.RTT1)

	log.Debug("probe pair replies received",
		"seq", seq,
		"rtt0_ms", float64(result.RTT0.Microseconds())/1000.0,
		"rtt1_ms", float64(result.RTT1.Microseconds())/1000.0,
		"probe_measured_rtt_ms", float64(probeMeasuredRttNs)/1e6,
		"reply0_probe_sig", reply0ProbeSigValid,
		"reply0_sig", reply0SigValid,
		"reply1_probe_sig", reply1ProbeSigValid,
		"reply1_sig", reply1SigValid)

	m := measurement{
		probeMeasuredRttNs:  probeMeasuredRttNs,
		targetMeasuredRtt:   targetMeasuredRtt,
		seq:                 seq,
		reply:               result.Reply1,
		reply0ProbeSigValid: reply0ProbeSigValid,
		reply0SigValid:      reply0SigValid,
		reply1ProbeSigValid: reply1ProbeSigValid,
		reply1SigValid:      reply1SigValid,
	}

	prevBestRtt, hadPrevBest := cache.BestRttNs()
	cacheResult := cache.Update(m)

	shouldPrint := *verbose || cacheResult == geoprobe.UpdateBest || cacheResult == geoprobe.UpdatePromoted
	if shouldPrint {
		logPairedResult(log, seq, probeMeasuredRttNs, targetMeasuredRtt,
			reply0ProbeSigValid, reply0SigValid, reply1ProbeSigValid, reply1SigValid,
			result.Reply1, cacheResult, prevBestRtt, hadPrevBest)
	}
}

func logPairedResult(log *slog.Logger, seq uint32, probeMeasuredRttNs uint64, targetMeasuredRtt time.Duration, reply0ProbeSigValid, reply0SigValid, reply1ProbeSigValid, reply1SigValid bool, reply *signed.ReplyPacket, cacheResult geoprobe.UpdateResult, prevBestRtt uint64, hadPrevBest bool) {
	authorityPK := solana.PublicKeyFromBytes(reply.AuthorityPubkey[:])
	geoprobePK := solana.PublicKeyFromBytes(reply.GeoprobePubkey[:])
	offsets := parseOffsets(reply.Offsets)

	if *logFormat == "json" {
		distMiles := calculateMaxDistance(reply.RttNs)
		output := probeOutput{
			Timestamp:           time.Now().UTC().Format(time.RFC3339),
			Seq:                 seq,
			ProbeMeasuredRttMs:  float64(probeMeasuredRttNs) / 1e6,
			TargetMeasuredRttMs: float64(targetMeasuredRtt.Microseconds()) / 1000.0,
			Reply0ProbeSigValid: reply0ProbeSigValid,
			Reply0SigValid:      reply0SigValid,
			Reply1ProbeSigValid: reply1ProbeSigValid,
			Reply1SigValid:      reply1SigValid,
			MeasurementSlot:     reply.MeasurementSlot,
			Lat:                 reply.Lat,
			Lng:                 reply.Lng,
			RttNs:               reply.RttNs,
			MaxDistanceMiles:    distMiles,
			MaxDistanceKm:       distMiles * kmPerMile,
			SinceLastRxNs:       reply.SinceLastRxNs,
			AuthorityPubkey:     authorityPK.String(),
			GeoprobePubkey:      geoprobePK.String(),
			Offsets:             offsets,
			CacheUpdate:         cacheResult.String(),
		}
		if (cacheResult == geoprobe.UpdateBest || cacheResult == geoprobe.UpdatePromoted) && hadPrevBest {
			prevMs := float64(prevBestRtt) / 1e6
			output.PreviousBestRttMs = &prevMs
		}
		data, err := json.Marshal(output)
		if err != nil {
			log.Error("failed to marshal output", "error", err)
			return
		}
		fmt.Println(string(data))
	} else {
		text := formatTextResult(seq, probeMeasuredRttNs, targetMeasuredRtt,
			reply0ProbeSigValid, reply0SigValid, reply1ProbeSigValid, reply1SigValid,
			authorityPK, geoprobePK, reply, offsets)
		if *verbose {
			text += fmt.Sprintf("  Cache: %s\n\n", cacheResult.String())
		} else if hadPrevBest {
			text += fmt.Sprintf("  * New best measurement (previous best: %.3fms)\n\n", float64(prevBestRtt)/1e6)
		}
		fmt.Print(text)
	}
}

func logProbeError(log *slog.Logger, seq uint32, probeErr error) {
	errStr := probeErr.Error()
	if errors.Is(probeErr, context.DeadlineExceeded) {
		errStr = "timeout"
	}

	if *logFormat == "json" {
		output := probeOutput{
			Timestamp:           time.Now().UTC().Format(time.RFC3339),
			Seq:                 seq,
			ProbeMeasuredRttMs:  -1,
			TargetMeasuredRttMs: -1,
			Error:               errStr,
		}
		data, err := json.Marshal(output)
		if err != nil {
			log.Error("failed to marshal output", "error", err)
			return
		}
		fmt.Println(string(data))
	} else {
		fmt.Printf("\n[%s] Probe Pair #%d — ERROR\n  %s\n\n", time.Now().UTC().Format("2006-01-02 15:04:05 MST"), seq, errStr)
	}
}

type measurement struct {
	probeMeasuredRttNs  uint64
	targetMeasuredRtt   time.Duration
	seq                 uint32
	reply               *signed.ReplyPacket
	reply0ProbeSigValid bool
	reply0SigValid      bool
	reply1ProbeSigValid bool
	reply1SigValid      bool
}

type probeOutput struct {
	Timestamp           string         `json:"timestamp"`
	Seq                 uint32         `json:"seq"`
	ProbeMeasuredRttMs  float64        `json:"probe_measured_rtt_ms"`
	TargetMeasuredRttMs float64        `json:"target_measured_rtt_ms"`
	Reply0ProbeSigValid bool           `json:"reply0_probe_sig_valid,omitempty"`
	Reply0SigValid      bool           `json:"reply0_sig_valid,omitempty"`
	Reply1ProbeSigValid bool           `json:"reply1_probe_sig_valid,omitempty"`
	Reply1SigValid      bool           `json:"reply1_sig_valid,omitempty"`
	MeasurementSlot     uint64         `json:"measurement_slot,omitempty"`
	Lat                 float64        `json:"lat,omitempty"`
	Lng                 float64        `json:"lng,omitempty"`
	RttNs               uint64         `json:"rtt_ns,omitempty"`
	MaxDistanceMiles    float64        `json:"max_distance_miles,omitempty"`
	MaxDistanceKm       float64        `json:"max_distance_km,omitempty"`
	SinceLastRxNs       uint64         `json:"since_last_rx_ns,omitempty"`
	AuthorityPubkey     string         `json:"authority_pubkey,omitempty"`
	GeoprobePubkey      string         `json:"geoprobe_pubkey,omitempty"`
	Offsets             []offsetOutput `json:"offsets,omitempty"`
	CacheUpdate         string         `json:"cache_update,omitempty"`
	PreviousBestRttMs   *float64       `json:"previous_best_rtt_ms,omitempty"`
	Error               string         `json:"error,omitempty"`
}

type offsetOutput struct {
	AuthorityPubkey  string  `json:"authority_pubkey"`
	SenderPubkey     string  `json:"sender_pubkey"`
	Lat              float64 `json:"lat"`
	Lng              float64 `json:"lng"`
	RttNs            uint64  `json:"rtt_ns"`
	MaxDistanceMiles float64 `json:"max_distance_miles"`
	MaxDistanceKm    float64 `json:"max_distance_km"`
	MeasuredRttNs    uint64  `json:"measured_rtt_ns"`
	SigValid         bool    `json:"sig_valid"`
}

func validateFlags() error {
	if *probeIP == "" {
		return fmt.Errorf("--probe-ip is required")
	}
	if net.ParseIP(*probeIP) == nil {
		return fmt.Errorf("--probe-ip %q is not a valid IP address", *probeIP)
	}
	if *probePK == "" {
		return fmt.Errorf("--probe-pk is required")
	}
	if _, err := parsePubkey(*probePK); err != nil {
		return fmt.Errorf("--probe-pk: %w", err)
	}
	if *keypairPath == "" {
		return fmt.Errorf("--keypair is required")
	}
	if _, err := os.Stat(*keypairPath); os.IsNotExist(err) {
		return fmt.Errorf("--keypair file does not exist: %s", *keypairPath)
	}
	if *probePort == 0 || *probePort > 65535 {
		return fmt.Errorf("--probe-port must be 1-65535")
	}
	if *count > math.MaxUint32 {
		return fmt.Errorf("--count must be <= %d", uint32(math.MaxUint32))
	}
	return nil
}

func parsePubkey(base58Str string) ([32]byte, error) {
	pk, err := solana.PublicKeyFromBase58(base58Str)
	if err != nil {
		return [32]byte{}, fmt.Errorf("invalid base58 public key: %w", err)
	}
	return pk, nil
}

func setupLogger(format string, debug bool) *slog.Logger {
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	opts := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
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

func formatRTT(d time.Duration) string {
	return fmt.Sprintf("%.3fms", float64(d.Microseconds())/1000.0)
}

func formatNsAsMs(ns uint64) string {
	return fmt.Sprintf("%.3fms", float64(ns)/1e6)
}

// calculateMaxDistance returns the theoretical maximum one-way distance in
// miles for a given round-trip time, assuming vacuum speed of light. Actual
// distances will be shorter (see constants above).
func calculateMaxDistance(rttNs uint64) float64 {
	rttMs := float64(rttNs) / (2 * nanosecondsPerMs)
	return rttMs * speedOfLightMilesPerMs
}

func formatTextResult(seq uint32, probeMeasuredRttNs uint64, targetMeasuredRtt time.Duration, reply0ProbeSigValid, reply0SigValid, reply1ProbeSigValid, reply1SigValid bool, authorityPK, geoprobePK solana.PublicKey, reply *signed.ReplyPacket, offsets []offsetOutput) string {
	var sb strings.Builder

	sb.WriteString("\n")
	fmt.Fprintf(&sb, "[%s] Probe Pair #%d\n", time.Now().UTC().Format("2006-01-02 15:04:05 MST"), seq)
	fmt.Fprintf(&sb, "  Probe-Measured RTT:  %s\n", formatNsAsMs(probeMeasuredRttNs))
	fmt.Fprintf(&sb, "  Target-Measured RTT: %s\n", formatRTT(targetMeasuredRtt))
	accumDistMiles := calculateMaxDistance(reply.RttNs)
	accumDistKm := accumDistMiles * kmPerMile
	fmt.Fprintf(&sb, "  Reference Point: %s\n", formatCoordinate(reply.Lat, reply.Lng))
	fmt.Fprintf(&sb, "  Accumulated RTT: %s\n", formatNsAsMs(reply.RttNs))
	fmt.Fprintf(&sb, "  Max Distance: %.0f miles (%.0f km)\n", accumDistMiles, accumDistKm)
	fmt.Fprintf(&sb, "  Measurement Slot: %d\n", reply.MeasurementSlot)
	fmt.Fprintf(&sb, "  Authority: %s\n", authorityPK.String())
	fmt.Fprintf(&sb, "  GeoProbe:  %s\n", geoprobePK.String())
	fmt.Fprintf(&sb, "  Reply 0: sender_sig=%s geoprobe_sig=%s\n", sigMark(reply0ProbeSigValid), sigMark(reply0SigValid))
	fmt.Fprintf(&sb, "  Reply 1: sender_sig=%s geoprobe_sig=%s\n", sigMark(reply1ProbeSigValid), sigMark(reply1SigValid))

	if len(offsets) > 0 {
		sb.WriteString("\n  DZD Reference Chain:\n")
		for i, o := range offsets {
			fmt.Fprintf(&sb, "    [%d] Authority: %s\n", i+1, o.AuthorityPubkey)
			fmt.Fprintf(&sb, "        Sender:    %s\n", o.SenderPubkey)
			fmt.Fprintf(&sb, "        Location:  %s\n", formatCoordinate(o.Lat, o.Lng))
			oDistMiles := calculateMaxDistance(o.RttNs)
			oDistKm := oDistMiles * kmPerMile
			fmt.Fprintf(&sb, "        RTT: %s  Measured RTT: %s\n", formatNsAsMs(o.RttNs), formatNsAsMs(o.MeasuredRttNs))
			fmt.Fprintf(&sb, "        Max Distance: %.0f miles (%.0f km)\n", oDistMiles, oDistKm)
			fmt.Fprintf(&sb, "        Signature: %s\n", sigMark(o.SigValid))
		}
	}

	sb.WriteString("\n")
	return sb.String()
}

func formatCoordinate(lat, lng float64) string {
	latDir := "N"
	if lat < 0 {
		latDir = "S"
	}
	lngDir := "E"
	if lng < 0 {
		lngDir = "W"
	}
	return fmt.Sprintf("%.4f\u00b0%s, %.4f\u00b0%s", math.Abs(lat), latDir, math.Abs(lng), lngDir)
}

func sigMark(valid bool) string {
	if valid {
		return "VALID"
	}
	return "INVALID"
}

func parseOffsets(blobs [][]byte) []offsetOutput {
	if len(blobs) == 0 {
		return nil
	}
	results := make([]offsetOutput, 0, len(blobs))
	for _, blob := range blobs {
		var offset geoprobe.LocationOffset
		if err := offset.Unmarshal(blob); err != nil {
			results = append(results, offsetOutput{SigValid: false})
			continue
		}
		sigValid := geoprobe.VerifyOffsetChain(&offset) == nil
		oDistMiles := calculateMaxDistance(offset.RttNs)
		results = append(results, offsetOutput{
			AuthorityPubkey:  solana.PublicKeyFromBytes(offset.AuthorityPubkey[:]).String(),
			SenderPubkey:     solana.PublicKeyFromBytes(offset.SenderPubkey[:]).String(),
			Lat:              offset.Lat,
			Lng:              offset.Lng,
			RttNs:            offset.RttNs,
			MaxDistanceMiles: oDistMiles,
			MaxDistanceKm:    oDistMiles * kmPerMile,
			MeasuredRttNs:    offset.MeasuredRttNs,
			SigValid:         sigValid,
		})
	}
	return results
}
