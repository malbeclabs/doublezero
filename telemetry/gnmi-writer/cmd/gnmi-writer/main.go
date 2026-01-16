package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/lmittmann/tint"
	"github.com/malbeclabs/doublezero/telemetry/gnmi-writer/internal/gnmi"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	flag "github.com/spf13/pflag"
)

var (
	// Set by LDFLAGS
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

const (
	defaultMetricsAddr            = ":2112"
	defaultMetricsShutdownTimeout = 10 * time.Second
)

// BuildInfo is a Prometheus gauge for build metadata.
var BuildInfo = prometheus.NewGaugeVec(
	prometheus.GaugeOpts{
		Namespace: gnmi.MetricsNamespace,
		Name:      "build_info",
		Help:      "Build information for gnmi-writer",
	},
	[]string{"version", "commit", "date"},
)

func init() {
	prometheus.MustRegister(BuildInfo)
}

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

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Start metrics server
	var metricsErrCh <-chan error
	if cfg.MetricsAddr != "" {
		BuildInfo.WithLabelValues(version, commit, date).Set(1)
		metricsErrCh = startMetricsServer(ctx, log, cfg.MetricsAddr, defaultMetricsShutdownTimeout)
	}

	// Create metrics
	consumerMetrics := gnmi.NewConsumerMetrics(prometheus.DefaultRegisterer)
	processorMetrics := gnmi.NewProcessorMetrics(prometheus.DefaultRegisterer)

	// Create consumer
	consumer, err := gnmi.NewKafkaConsumer(
		gnmi.WithKafkaBrokers(cfg.KafkaBrokers),
		gnmi.WithKafkaTopic(cfg.KafkaTopic),
		gnmi.WithKafkaGroup(cfg.KafkaGroup),
		gnmi.WithKafkaAuthType(cfg.KafkaAuthType),
		gnmi.WithKafkaUser(cfg.KafkaUser),
		gnmi.WithKafkaPassword(cfg.KafkaPassword),
		gnmi.WithKafkaTLSDisabled(cfg.KafkaTLSDisabled),
		gnmi.WithKafkaLogger(log),
		gnmi.WithConsumerMetrics(consumerMetrics),
	)
	if err != nil {
		return fmt.Errorf("failed to create consumer: %w", err)
	}

	// Create writer based on output type
	var writer gnmi.RecordWriter
	switch cfg.Output {
	case "stdout":
		writer = gnmi.NewStdoutRecordWriter()
	case "clickhouse":
		chMetrics := gnmi.NewClickhouseMetrics(prometheus.DefaultRegisterer)
		writer, err = gnmi.NewClickhouseRecordWriter(
			gnmi.WithClickhouseAddr(cfg.ClickhouseAddr),
			gnmi.WithClickhouseDB(cfg.ClickhouseDB),
			gnmi.WithClickhouseUser(cfg.ClickhouseUser),
			gnmi.WithClickhousePassword(cfg.ClickhousePassword),
			gnmi.WithClickhouseTLSDisabled(cfg.ClickhouseTLSDisabled),
			gnmi.WithClickhouseLogger(log),
			gnmi.WithClickhouseMetrics(chMetrics),
		)
		if err != nil {
			return fmt.Errorf("failed to create clickhouse writer: %w", err)
		}
	default:
		return fmt.Errorf("unknown output type: %s", cfg.Output)
	}

	// Create and run processor (uses DefaultExtractors automatically)
	processor, err := gnmi.NewProcessor(
		gnmi.WithConsumer(consumer),
		gnmi.WithRecordWriter(writer),
		gnmi.WithProcessorLogger(log),
		gnmi.WithProcessorMetrics(processorMetrics),
	)
	if err != nil {
		return fmt.Errorf("failed to create processor: %w", err)
	}

	log.Info("starting gnmi-writer",
		"output", cfg.Output,
		"kafka_topic", cfg.KafkaTopic,
		"kafka_group", cfg.KafkaGroup,
	)

	errCh := make(chan error, 1)
	go func() {
		errCh <- processor.Run(ctx)
	}()

	for {
		select {
		case err, ok := <-errCh:
			if !ok {
				log.Info("processor stopped")
				return nil
			}
			if err != nil {
				return fmt.Errorf("processor error: %w", err)
			}
		case err, ok := <-metricsErrCh:
			if ok && err != nil {
				return fmt.Errorf("metrics server error: %w", err)
			}
			metricsErrCh = nil
		case <-ctx.Done():
			return nil
		}
	}
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

func startMetricsServer(ctx context.Context, log *slog.Logger, addr string, shutdownTimeout time.Duration) <-chan error {
	errCh := make(chan error, 1)

	go func() {
		defer close(errCh)

		listener, err := net.Listen("tcp", addr)
		if err != nil {
			errCh <- err
			return
		}
		defer listener.Close()

		log.Info("prometheus metrics server listening", "address", listener.Addr().String())

		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.Handler())
		httpSrv := &http.Server{Handler: mux}

		go func() {
			<-ctx.Done()
			sctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
			defer cancel()
			_ = httpSrv.Shutdown(sctx)
		}()

		err = httpSrv.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			return
		}
		if err != nil {
			errCh <- err
		}
	}()

	return errCh
}

// Config holds the application configuration.
type Config struct {
	ShowVersion bool
	Verbose     bool
	MetricsAddr string

	// Output configuration
	Output string // "stdout" or "clickhouse"

	// Kafka configuration
	KafkaBrokers     []string
	KafkaTopic       string
	KafkaGroup       string
	KafkaAuthType    gnmi.KafkaAuthType
	KafkaUser        string
	KafkaPassword    string
	KafkaTLSDisabled bool

	// ClickHouse configuration
	ClickhouseAddr        string
	ClickhouseDB          string
	ClickhouseUser        string
	ClickhousePassword    string
	ClickhouseTLSDisabled bool
}

func getenv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func loadConfig() (Config, error) {
	var cfg Config
	var kafkaAuthType string

	flag.BoolVar(&cfg.ShowVersion, "version", false, "show version and exit")
	flag.BoolVar(&cfg.Verbose, "verbose", false, "verbose mode - show debug logs")
	flag.StringVar(&cfg.MetricsAddr, "metrics-addr", getenv("METRICS_ADDR", defaultMetricsAddr), "address for prometheus metrics (env: METRICS_ADDR)")

	// Output configuration
	flag.StringVar(&cfg.Output, "output", getenv("OUTPUT", "stdout"), "output destination: stdout or clickhouse (env: OUTPUT)")

	// Kafka configuration
	kafkaBrokersStr := getenv("KAFKA_BROKERS", "localhost:9092")
	flag.StringSliceVar(&cfg.KafkaBrokers, "kafka-brokers", strings.Split(kafkaBrokersStr, ","), "kafka broker addresses (env: KAFKA_BROKERS)")
	flag.StringVar(&cfg.KafkaTopic, "kafka-topic", getenv("KAFKA_TOPIC", "gnmi-notifications"), "kafka topic (env: KAFKA_TOPIC)")
	flag.StringVar(&cfg.KafkaGroup, "kafka-group", getenv("KAFKA_GROUP", "gnmi-writer"), "kafka consumer group (env: KAFKA_GROUP)")
	flag.StringVar(&kafkaAuthType, "kafka-auth-type", getenv("KAFKA_AUTH_TYPE", "scram"), "kafka auth type: scram or aws-msk (env: KAFKA_AUTH_TYPE)")
	flag.StringVar(&cfg.KafkaUser, "kafka-user", getenv("KAFKA_USER", ""), "kafka SCRAM username (env: KAFKA_USER)")
	flag.StringVar(&cfg.KafkaPassword, "kafka-password", getenv("KAFKA_PASSWORD", ""), "kafka SCRAM password (env: KAFKA_PASSWORD)")
	flag.BoolVar(&cfg.KafkaTLSDisabled, "kafka-tls-disabled", getenv("KAFKA_TLS_DISABLED", "") == "true", "disable TLS for kafka (env: KAFKA_TLS_DISABLED)")

	// ClickHouse configuration (tables are determined by record types)
	// DZ_ENV sets the database name (devnet, testnet, mainnet-beta), defaults to "default"
	flag.StringVar(&cfg.ClickhouseAddr, "clickhouse-addr", getenv("CLICKHOUSE_ADDR", "localhost:9440"), "clickhouse address (env: CLICKHOUSE_ADDR)")
	flag.StringVar(&cfg.ClickhouseDB, "clickhouse-db", getenv("DZ_ENV", "default"), "clickhouse database (env: DZ_ENV)")
	flag.StringVar(&cfg.ClickhouseUser, "clickhouse-user", getenv("CLICKHOUSE_USER", "default"), "clickhouse username (env: CLICKHOUSE_USER)")
	flag.StringVar(&cfg.ClickhousePassword, "clickhouse-password", getenv("CLICKHOUSE_PASS", ""), "clickhouse password (env: CLICKHOUSE_PASS)")
	flag.BoolVar(&cfg.ClickhouseTLSDisabled, "clickhouse-tls-disabled", getenv("CLICKHOUSE_TLS_DISABLED", "") == "true", "disable TLS for clickhouse (env: CLICKHOUSE_TLS_DISABLED)")

	flag.Parse()

	if cfg.ShowVersion {
		return cfg, nil
	}

	// Parse auth type
	switch strings.ToLower(kafkaAuthType) {
	case "scram":
		cfg.KafkaAuthType = gnmi.KafkaAuthTypeSCRAM
	case "aws-msk":
		cfg.KafkaAuthType = gnmi.KafkaAuthTypeAWSMSK
	default:
		return Config{}, fmt.Errorf("unknown kafka auth type: %s", kafkaAuthType)
	}

	// Validate output
	switch cfg.Output {
	case "stdout", "clickhouse":
		// valid
	default:
		return Config{}, fmt.Errorf("invalid output type: %s (must be stdout or clickhouse)", cfg.Output)
	}

	return cfg, nil
}
