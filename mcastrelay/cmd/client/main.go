package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lmittmann/tint"
	"github.com/malbeclabs/doublezero/mcastrelay/internal/shred"
	pb "github.com/malbeclabs/doublezero/mcastrelay/proto/relay/gen/pb-go"
	flag "github.com/spf13/pflag"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var (
	// Set by LDFLAGS
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

type config struct {
	ServerAddr  string
	Verbose     bool
	ShowVersion bool
	ShowRaw     bool
	MaxShreds   int
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := parseFlags()

	if cfg.ShowVersion {
		fmt.Printf("mcastrelay-client version: %s, commit: %s, date: %s\n", version, commit, date)
		return nil
	}

	log := newLogger(cfg.Verbose)

	// Setup context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		log.Info("received shutdown signal", "signal", sig)
		cancel()
	}()

	// Connect to the server
	log.Info("connecting to server", "address", cfg.ServerAddr)

	conn, err := grpc.NewClient(
		cfg.ServerAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	defer conn.Close()

	client := pb.NewRelayServiceClient(conn)

	// Subscribe to the stream
	log.Info("subscribing to shred stream")

	stream, err := client.Subscribe(ctx, &pb.SubscribeRequest{})
	if err != nil {
		return fmt.Errorf("failed to subscribe: %w", err)
	}

	log.Info("connected and subscribed, waiting for shreds...")
	fmt.Println()

	// Track statistics
	stats := &shredStats{}

	// Receive and decode shreds
	for {
		msg, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				log.Info("stream closed by server")
				break
			}
			if ctx.Err() != nil {
				log.Info("shutting down")
				break
			}
			return fmt.Errorf("stream error: %w", err)
		}

		stats.totalReceived++

		// Decode the shred
		s, err := shred.Decode(msg.Payload)
		if err != nil {
			stats.decodeErrors++
			if cfg.Verbose {
				log.Warn("failed to decode shred",
					"error", err,
					"size", len(msg.Payload),
				)
			}
			continue
		}

		// Update statistics
		stats.update(s)

		// Print shred info
		printShred(s, msg, cfg, stats)

		// Check if we've reached max shreds
		if cfg.MaxShreds > 0 && stats.totalReceived >= cfg.MaxShreds {
			log.Info("reached max shreds limit", "count", cfg.MaxShreds)
			break
		}
	}

	// Print final statistics
	fmt.Println()
	printStats(stats)

	return nil
}

type shredStats struct {
	totalReceived  int
	decodeErrors   int
	dataShreds     int
	codeShreds     int
	blocksComplete int
	currentSlot    uint64
	slotsSeeen     map[uint64]bool
}

func (s *shredStats) update(sh *shred.Shred) {
	if s.slotsSeeen == nil {
		s.slotsSeeen = make(map[uint64]bool)
	}

	s.slotsSeeen[sh.Slot] = true

	if sh.Slot > s.currentSlot {
		s.currentSlot = sh.Slot
	}

	switch sh.Type {
	case shred.ShredTypeData:
		s.dataShreds++
		if sh.BlockComplete {
			s.blocksComplete++
		}
	case shred.ShredTypeCode:
		s.codeShreds++
	}
}

func printShred(s *shred.Shred, msg *pb.PayloadMessage, cfg *config, stats *shredStats) {
	// Calculate latency if we have a received timestamp
	var latencyStr string
	if msg.ReceivedAt != nil {
		latency := time.Since(msg.ReceivedAt.AsTime())
		latencyStr = fmt.Sprintf(" latency=%v", latency.Round(time.Microsecond))
	}

	// Print based on verbosity
	if cfg.Verbose {
		fmt.Printf("#%d %s%s\n", stats.totalReceived, s.String(), latencyStr)
	} else {
		fmt.Printf("#%d %s%s\n", stats.totalReceived, s.Summary(), latencyStr)
	}

	// Show raw hex if requested
	if cfg.ShowRaw && len(s.Payload) > 0 {
		maxBytes := 64
		if len(s.Payload) < maxBytes {
			maxBytes = len(s.Payload)
		}
		fmt.Printf("    payload[0:%d]: %x\n", maxBytes, s.Payload[:maxBytes])
	}
}

func printStats(stats *shredStats) {
	fmt.Println("=== Statistics ===")
	fmt.Printf("Total received:   %d\n", stats.totalReceived)
	fmt.Printf("Decode errors:    %d\n", stats.decodeErrors)
	fmt.Printf("Data shreds:      %d\n", stats.dataShreds)
	fmt.Printf("Code shreds:      %d\n", stats.codeShreds)
	fmt.Printf("Blocks complete:  %d\n", stats.blocksComplete)
	fmt.Printf("Unique slots:     %d\n", len(stats.slotsSeeen))
	fmt.Printf("Latest slot:      %d\n", stats.currentSlot)
}

func parseFlags() *config {
	cfg := &config{}

	flag.StringVarP(&cfg.ServerAddr, "server", "s", "localhost:50051", "mcastrelay server address (host:port)")
	flag.BoolVarP(&cfg.Verbose, "verbose", "v", false, "Enable verbose output")
	flag.BoolVar(&cfg.ShowVersion, "version", false, "Show version and exit")
	flag.BoolVar(&cfg.ShowRaw, "raw", false, "Show raw payload hex (first 64 bytes)")
	flag.IntVarP(&cfg.MaxShreds, "count", "c", 0, "Exit after receiving N shreds (0 = unlimited)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "mcastrelay-client - Subscribe to Solana shred stream via gRPC\n\n")
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  mcastrelay-client [flags]\n\n")
		fmt.Fprintf(os.Stderr, "Examples:\n")
		fmt.Fprintf(os.Stderr, "  mcastrelay-client -s localhost:50051\n")
		fmt.Fprintf(os.Stderr, "  mcastrelay-client -s 192.168.1.100:50051 -v\n")
		fmt.Fprintf(os.Stderr, "  mcastrelay-client -s relay.example.com:50051 -c 100\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
	}

	flag.Parse()
	return cfg
}

func newLogger(verbose bool) *slog.Logger {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}

	return slog.New(tint.NewHandler(os.Stderr, &tint.Options{
		Level:      level,
		TimeFormat: time.RFC3339,
	}))
}
