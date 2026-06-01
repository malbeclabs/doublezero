// device-observer samples an Arista cEOS device-under-test during the GRE
// Tunnel Capacity Study.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/malbeclabs/doublezero/tools/stress/device-observer/internal/abort"
	"github.com/malbeclabs/doublezero/tools/stress/device-observer/internal/collector"
	"github.com/malbeclabs/doublezero/tools/stress/device-observer/internal/eapi"
	"github.com/malbeclabs/doublezero/tools/stress/device-observer/internal/loggingtail"
	"github.com/malbeclabs/doublezero/tools/stress/device-observer/internal/promscrape"
	"github.com/malbeclabs/doublezero/tools/stress/device-observer/internal/runlog"
	"github.com/malbeclabs/doublezero/tools/stress/device-observer/internal/sample"
)

// observerConfig is the on-disk schema for observer-config.json. eapi_pass
// is deliberately omitted; the working directory may be archived and
// credentials must not land there.
type observerConfig struct {
	StartedAt       time.Time `json:"started_at"`
	PID             int       `json:"pid"`
	DUTHost         string    `json:"dut_host"`
	EAPIUser        string    `json:"eapi_user"`
	AgentMetricsURL string    `json:"agent_metrics_url"`
	SampleInterval  string    `json:"sample_interval"`
	AbortFile       string    `json:"abort_file"`
	WorkingDir      string    `json:"working_dir"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	var (
		dutHost         = flag.String("dut-host", "", "DUT hostname for eAPI (required)")
		eapiUser        = flag.String("eapi-user", "admin", "eAPI username")
		eapiPass        = flag.String("eapi-pass", "admin", "eAPI password")
		eapiPort        = flag.Int("eapi-port", 80, "eAPI HTTP port")
		agentMetricsURL = flag.String("agent-metrics-url", "", "doublezero-agent Prometheus metrics URL (required)")
		sampleInterval  = flag.Duration("sample-interval", 10*time.Second, "interval between eAPI samples")
		workingDir      = flag.String("working-dir", "", "working directory for observer outputs (required)")
		abortFile       = flag.String("abort-file", "", "path to write the abort sentinel (default <working-dir>/abort)")
	)
	flag.Parse()

	if *dutHost == "" || *agentMetricsURL == "" || *workingDir == "" {
		flag.Usage()
		return errors.New("--dut-host, --agent-metrics-url, and --working-dir are required")
	}
	if *sampleInterval <= 0 {
		return errors.New("--sample-interval must be > 0")
	}
	if *abortFile == "" {
		*abortFile = filepath.Join(*workingDir, "abort")
	}
	absWorking, err := filepath.Abs(*workingDir)
	if err != nil {
		return fmt.Errorf("resolve --working-dir: %w", err)
	}
	absAbort, err := filepath.Abs(*abortFile)
	if err != nil {
		return fmt.Errorf("resolve --abort-file: %w", err)
	}
	// Constrain --abort-file to live under --working-dir so the sentinel
	// is bounded by the orchestrator's archive surface.
	if !strings.HasPrefix(absAbort+string(os.PathSeparator), absWorking+string(os.PathSeparator)) {
		return fmt.Errorf("--abort-file %q must be inside --working-dir %q", absAbort, absWorking)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))

	if err := os.MkdirAll(*workingDir, 0o750); err != nil {
		return fmt.Errorf("create working dir: %w", err)
	}
	if err := writeObserverConfig(*workingDir, observerConfig{
		StartedAt:       time.Now().UTC(),
		PID:             os.Getpid(),
		DUTHost:         *dutHost,
		EAPIUser:        *eapiUser,
		AgentMetricsURL: *agentMetricsURL,
		SampleInterval:  sampleInterval.String(),
		AbortFile:       *abortFile,
		WorkingDir:      *workingDir,
	}); err != nil {
		return fmt.Errorf("write observer config: %w", err)
	}

	client, err := eapi.NewClient(*dutHost, *eapiUser, *eapiPass, *eapiPort)
	if err != nil {
		return err
	}

	collectors := []collector.Collector{
		sample.NewSampler(client, *workingDir, *sampleInterval, logger),
		promscrape.New(*agentMetricsURL, *workingDir, *sampleInterval, logger),
		loggingtail.NewEOS(client, *workingDir, *sampleInterval, logger),
		loggingtail.NewAgent(*workingDir, *sampleInterval, logger),
		runlog.New(*workingDir, *sampleInterval, logger),
		abort.New(*abortFile),
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	logger.Info("device-observer started", "dut_host", *dutHost, "working_dir", *workingDir, "pid", os.Getpid())

	g, gctx := errgroup.WithContext(ctx)
	for _, c := range collectors {
		g.Go(func() error { return c.Run(gctx) })
	}
	if err := g.Wait(); err != nil {
		return fmt.Errorf("collector failed: %w", err)
	}
	return nil
}

func writeObserverConfig(workingDir string, cfg observerConfig) error {
	body, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(workingDir, "observer-config.json"), body, 0o640)
}
