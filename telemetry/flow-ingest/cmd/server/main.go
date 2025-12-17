package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/lmittmann/tint"
	"github.com/malbeclabs/doublezero/telemetry/flow-ingest/internal/kafka"
	"github.com/malbeclabs/doublezero/telemetry/flow-ingest/internal/metrics"
	"github.com/malbeclabs/doublezero/telemetry/flow-ingest/internal/server"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	flag "github.com/spf13/pflag"

	_ "net/http/pprof"
)

var (
	// Set by LDFLAGS
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

const (
	defaultPort        = "6343"
	defaultMetricsAddr = ":8080"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	if cfg.ShowVersion {
		fmt.Printf("version: %s, commit: %s, date: %s\n", version, commit, date)
		return nil
	}

	log := newLogger(cfg.Verbose)

	// Start pprof server
	if cfg.EnablePprof {
		go func() {
			log.Info("starting pprof server", "address", "localhost:6060")
			err := http.ListenAndServe("localhost:6060", nil)
			if err != nil {
				log.Error("failed to start pprof server", "error", err)
			}
		}()
	}

	// Start prometheus metrics server
	if cfg.MetricsAddr != "" {
		metrics.BuildInfo.WithLabelValues(version, commit, date).Set(1)
		go func() {
			listener, err := net.Listen("tcp", cfg.MetricsAddr)
			if err != nil {
				log.Error("Failed to start prometheus metrics server listener", "error", err)
				os.Exit(1)
			}
			log.Info("Prometheus metrics server listening", "address", listener.Addr().String())
			http.Handle("/metrics", promhttp.Handler())
			if err := http.Serve(listener, nil); err != nil {
				log.Error("Failed to start prometheus metrics server", "error", err)
				os.Exit(1)
			}
		}()
	}

	addr, err := net.ResolveUDPAddr("udp", ":"+cfg.Port)
	if err != nil {
		return fmt.Errorf("failed to resolve udp address: %w", err)
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen udp: %w", err)
	}
	log.Info("listening for UDP", "address", conn.LocalAddr())

	healthListener, err := net.Listen("tcp", ":"+cfg.HealthPort)
	if err != nil {
		return fmt.Errorf("failed to listen tcp: %w", err)
	}
	log.Info("listening for TCP for health checks", "address", healthListener.Addr())

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()

	go func() {
		<-ctx.Done()
		_ = healthListener.Close()
	}()

	kafkaClient, err := kafka.NewClient(ctx, &kafka.Config{
		Brokers: cfg.KafkaBrokers,
		AuthIAM: cfg.KafkaAuthIAMEnabled,
	})
	if err != nil {
		return fmt.Errorf("failed to create kafka client: %w", err)
	}

	if err := kafkaClient.EnsureTopic(ctx, cfg.KafkaTopic, cfg.KafkaPartitions, cfg.KafkaReplicationFactor); err != nil {
		return fmt.Errorf("failed to ensure topic exists: %w", err)
	}

	server, err := server.New(&server.Config{
		Logger:         log,
		FlowListener:   conn,
		HealthListener: healthListener,
		KafkaClient:    kafkaClient,
		KafkaTopic:     cfg.KafkaTopic,
	})
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}
	errCh := server.Start(ctx, cancel)
	defer cancel()

	select {
	case <-ctx.Done():
		log.Info("context cancelled, server stopped")
		return nil
	case err := <-errCh:
		return err
	}
}

type Config struct {
	ShowVersion bool
	Verbose     bool
	EnablePprof bool
	MetricsAddr string

	Port       string
	HealthPort string

	KafkaBrokers           []string
	KafkaAuthIAMEnabled    bool
	KafkaTopic             string
	KafkaPartitions        int
	KafkaReplicationFactor int
}

func getenv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}
func getenvBool(key string, def bool) bool {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}
func getenvInt(key string, def int) (int, error) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def, nil
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("invalid %s=%q: %w", key, v, err)
	}
	return i, nil
}
func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func loadConfig() (Config, error) {
	var cfg Config
	var kafkaBrokersCSV string

	flag.BoolVar(&cfg.ShowVersion, "version", false, "show version and exit")
	flag.BoolVar(&cfg.Verbose, "verbose", false, "verbose mode - show debug logs")
	flag.BoolVar(&cfg.EnablePprof, "enable-pprof", false, "enable pprof server")

	flag.StringVar(&cfg.MetricsAddr, "metrics-addr", getenv("METRICS_ADDR", defaultMetricsAddr), "address to listen on for prometheus metrics (env: METRICS_ADDR)")
	flag.StringVar(&cfg.Port, "port", getenv("PORT", defaultPort), "udp listen port (env: PORT)")
	flag.StringVar(&cfg.HealthPort, "health-port", getenv("HEALTH_PORT", ""), "health check port (env: HEALTH_PORT; default: port)")
	flag.StringVar(&cfg.KafkaTopic, "kafka-topic", getenv("KAFKA_TOPIC", ""), "kafka topic (env: KAFKA_TOPIC)")
	flag.BoolVar(&cfg.KafkaAuthIAMEnabled, "kafka-auth-iam-enabled", getenvBool("KAFKA_AUTH_IAM_ENABLED", false), "kafka IAM auth (env: KAFKA_AUTH_IAM_ENABLED)")
	flag.StringVar(&kafkaBrokersCSV, "kafka-brokers", getenv("KAFKA_BROKERS", ""), "kafka brokers csv (env: KAFKA_BROKERS)")

	flag.Parse()

	if cfg.ShowVersion {
		return cfg, nil
	}

	if cfg.HealthPort == "" {
		cfg.HealthPort = cfg.Port
	}
	cfg.KafkaBrokers = splitCSV(kafkaBrokersCSV)

	var err error
	cfg.KafkaPartitions, err = getenvInt("KAFKA_TOPIC_PARTITIONS", 1)
	if err != nil {
		return Config{}, err
	}
	cfg.KafkaReplicationFactor, err = getenvInt("KAFKA_REPLICATION_FACTOR", 1)
	if err != nil {
		return Config{}, err
	}

	if len(cfg.KafkaBrokers) == 0 {
		return Config{}, fmt.Errorf("kafka brokers is empty (set KAFKA_BROKERS or --kafka-brokers)")
	}

	if cfg.KafkaTopic == "" {
		return Config{}, fmt.Errorf("kafka topic is empty (set KAFKA_TOPIC or --kafka-topic)")
	}

	return cfg, nil
}

func newLogger(verbose bool) *slog.Logger {
	logLevel := slog.LevelInfo
	if verbose {
		logLevel = slog.LevelDebug
	}
	return slog.New(tint.NewHandler(os.Stdout, &tint.Options{
		Level: logLevel,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				t := a.Value.Time().UTC()
				a.Value = slog.StringValue(formatRFC3339Millis(t))
			}
			if s, ok := a.Value.Any().(string); ok && s == "" {
				return slog.Attr{}
			}
			return a
		},
	}))
}

func formatRFC3339Millis(t time.Time) string {
	t = t.UTC()
	base := t.Format("2006-01-02T15:04:05")
	ms := t.Nanosecond() / 1_000_000
	return fmt.Sprintf("%s.%03dZ", base, ms)
}
