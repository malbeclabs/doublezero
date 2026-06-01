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
		force           = flag.Bool("force", false, "overwrite a stale abort sentinel from a previous run")
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

	if err := checkStaleAbort(absAbort, *force); err != nil {
		return err
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

	sampler := sample.NewSampler(client, *workingDir, *sampleInterval, logger)
	scraper := promscrape.New(*agentMetricsURL, *workingDir, *sampleInterval, logger)
	eosTail := loggingtail.NewEOS(client, *workingDir, *sampleInterval, logger)
	agentTail := loggingtail.NewAgent(*workingDir, *sampleInterval, logger)
	runReader := runlog.New(*workingDir, *sampleInterval, logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	g, gctx := errgroup.WithContext(runCtx)

	decider := abort.New(abort.Config{
		AbortFile: absAbort,
		Interval:  *sampleInterval,
		Logger:    logger,
		Sources: abort.Sources{
			PromSnapshot:         scraper.Snapshot,
			AgentSnapshot:        agentTail.Snapshot,
			ProvisionDurations:   runReader.ProvisionDurations,
			DeprovisionDurations: runReader.DeprovisionDurations,
			CPUPercent:           sampler.LatestCPUPercent,
			LedgerHeartbeatPath:  filepath.Join(absWorking, "orchestrator.ledger_heartbeat"),
		},
		OnFire: cancel,
	})

	logger.Info("device-observer started", "dut_host", *dutHost, "working_dir", *workingDir, "pid", os.Getpid())

	for _, c := range []collector.Collector{sampler, scraper, eosTail, agentTail, runReader, decider} {
		g.Go(func() error { return c.Run(gctx) })
	}
	if err := g.Wait(); err != nil {
		return fmt.Errorf("collector failed: %w", err)
	}
	return nil
}

// checkStaleAbort refuses to start when the abort sentinel already
// exists unless --force is passed; with --force, the stale sentinel is
// removed so the decider isn't immediately short-circuited.
func checkStaleAbort(absAbort string, force bool) error {
	if _, err := os.Stat(absAbort); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat abort sentinel: %w", err)
	}
	if !force {
		return fmt.Errorf("stale abort sentinel %s exists; pass --force to overwrite", absAbort)
	}
	if err := os.Remove(absAbort); err != nil {
		return fmt.Errorf("remove stale abort sentinel %s: %w", absAbort, err)
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
