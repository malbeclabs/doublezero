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
	"syscall"
	"time"

	"github.com/gagliardetto/solana-go"

	"github.com/malbeclabs/doublezero/tools/twamp/pkg/signed"
)

const (
	defaultProbePort = 8924
	defaultInterval  = 60 * time.Second
	defaultTimeout   = 2 * time.Second
)

var (
	probeIP     = flag.String("probe-ip", "", "IP address of the GeoProbe to probe (required)")
	probePort   = flag.Uint("probe-port", defaultProbePort, "TWAMP port on the probe")
	probePK     = flag.String("probe-pk", "", "Base58 Ed25519 public key of the GeoProbe's signing authority (required)")
	keypairPath = flag.String("keypair", "", "Path to this target's Ed25519 keypair file for signing outbound message (required)")
	interval    = flag.Duration("interval", defaultInterval, "Interval between probes")
	count       = flag.Uint("count", 0, "Number of probes to send (0 = infinite)")
	timeout     = flag.Duration("timeout", defaultTimeout, "Per-probe timeout")
	logFormat   = flag.String("log-format", "text", "Log format: text or json")
	verbose     = flag.Bool("verbose", false, "Enable debug logging")
	showVersion = flag.Bool("version", false, "Print version and exit")

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
	)

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

	log.Info("sending probes", "target", remoteAddr.String())

	// Probe loop.
	var seq uint32
	for {
		select {
		case <-ctx.Done():
			log.Info("shutdown signal received, exiting gracefully")
			return
		default:
		}

		seq++
		if *count > 0 && seq > uint32(*count) {
			log.Info("completed all probes", "count", *count)
			return
		}

		probeCtx, probeCancel := context.WithTimeout(ctx, *timeout)
		rtt, reply, err := sender.Probe(probeCtx)
		probeCancel()

		if err != nil {
			logProbeError(log, seq, err)
		} else {
			probeSigValid := reply.Probe.Verify()
			replySigValid := reply.Verify()
			logProbeResult(log, seq, rtt, probeSigValid, replySigValid, reply)
		}

		// Wait for next interval (unless this is the last probe).
		if *count > 0 && seq >= uint32(*count) {
			continue
		}
		select {
		case <-ctx.Done():
			log.Info("shutdown signal received, exiting gracefully")
			return
		case <-time.After(*interval):
		}
	}
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

func logProbeResult(log *slog.Logger, seq uint32, rtt time.Duration, probeSigValid, replySigValid bool, reply *signed.ReplyPacket) {
	authorityPK := solana.PublicKeyFromBytes(reply.AuthorityPubkey[:])
	geoprobePK := solana.PublicKeyFromBytes(reply.GeoprobePubkey[:])

	if *logFormat == "json" {
		output := probeOutput{
			Timestamp:      time.Now().UTC().Format(time.RFC3339),
			Seq:            seq,
			RttMs:          float64(rtt.Microseconds()) / 1000.0,
			ProbeSigValid:  probeSigValid,
			ReplySigValid:  replySigValid,
			AuthorityPubkey: authorityPK.String(),
			GeoprobePubkey:  geoprobePK.String(),
		}
		data, err := json.Marshal(output)
		if err != nil {
			log.Error("failed to marshal output", "error", err)
			return
		}
		fmt.Println(string(data))
	} else {
		probeSigStr := "VALID"
		if !probeSigValid {
			probeSigStr = "INVALID"
		}
		replySigStr := "VALID"
		if !replySigValid {
			replySigStr = "INVALID"
		}
		fmt.Printf("[%s] seq=%d rtt=%s probe_sig=%s reflector_sig=%s authority=%s geoprobe=%s\n",
			time.Now().UTC().Format("2006-01-02 15:04:05 MST"),
			seq,
			formatRTT(rtt),
			probeSigStr,
			replySigStr,
			abbreviatePubkey(authorityPK.String()),
			abbreviatePubkey(geoprobePK.String()),
		)
	}
}

func logProbeError(log *slog.Logger, seq uint32, probeErr error) {
	errStr := probeErr.Error()
	if errors.Is(probeErr, context.DeadlineExceeded) {
		errStr = "timeout"
	}

	if *logFormat == "json" {
		output := probeOutput{
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Seq:       seq,
			RttMs:     -1,
			Error:     errStr,
		}
		data, err := json.Marshal(output)
		if err != nil {
			log.Error("failed to marshal output", "error", err)
			return
		}
		fmt.Println(string(data))
	} else {
		fmt.Printf("[%s] seq=%d rtt=%s\n",
			time.Now().UTC().Format("2006-01-02 15:04:05 MST"),
			seq,
			errStr,
		)
	}
}

type probeOutput struct {
	Timestamp       string  `json:"timestamp"`
	Seq             uint32  `json:"seq"`
	RttMs           float64 `json:"rtt_ms"`
	ProbeSigValid   bool    `json:"probe_sig_valid,omitempty"`
	ReplySigValid   bool    `json:"reply_sig_valid,omitempty"`
	AuthorityPubkey string  `json:"authority_pubkey,omitempty"`
	GeoprobePubkey  string  `json:"geoprobe_pubkey,omitempty"`
	Error           string  `json:"error,omitempty"`
}

func formatRTT(d time.Duration) string {
	return fmt.Sprintf("%.3fms", float64(d.Microseconds())/1000.0)
}

func abbreviatePubkey(pk string) string {
	if len(pk) <= 10 {
		return pk
	}
	return pk[:4] + "..." + pk[len(pk)-4:]
}
