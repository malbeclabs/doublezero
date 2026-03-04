package gm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"runtime"
	"time"

	influxdb2api "github.com/influxdata/influxdb-client-go/v2/api"
	"github.com/jonboulle/clockwork"
	"github.com/malbeclabs/doublezero/telemetry/global-monitor/internal/dz"
	"github.com/malbeclabs/doublezero/telemetry/global-monitor/internal/metrics"
	"github.com/malbeclabs/doublezero/telemetry/global-monitor/internal/netlink"
	"github.com/malbeclabs/doublezero/telemetry/global-monitor/internal/sol"
	"github.com/malbeclabs/doublezero/tools/maxmind/pkg/geoip"
)

type InfluxTable string

const (
	InfluxTableSolanaValidatorTPUQUICProbe InfluxTable = "solana_validator_tpuquic_probe"
	InfluxTableSolanaValidatorICMPProbe    InfluxTable = "solana_validator_icmp_probe"
	InfluxTableDoubleZeroUserICMPProbe     InfluxTable = "doublezero_user_icmp_probe"
)

type ProbeType string

const (
	ProbeTypeTPUQUIC ProbeType = "tpuquic"
	ProbeTypeICMP    ProbeType = "icmp"
	ProbeTypeUnknown ProbeType = "unknown"
)

type ProbePath string

const (
	ProbePathPublicInternet ProbePath = "public_internet"
	ProbePathDoubleZero     ProbePath = "doublezero"
)

func (p ProbePath) String() string {
	return string(p)
}

type RunnerConfig struct {
	Clock          clockwork.Clock
	Solana         *sol.SolanaView
	Serviceability *dz.ServiceabilityView
	Netlinker      netlink.Netlinker
	DZNetworkEnv   string

	// Verbosity configuration.
	VerboseFailures  bool
	VerboseSuccesses bool

	// Source configuration.
	Source *SourceConfig

	// InfluxDB configuration.
	InfluxAPI influxdb2api.WriteAPI

	// GeoIP configuration.
	GeoIP geoip.Resolver

	// Probe configuration.
	ProbeInterval        time.Duration
	ProbeTimeout         time.Duration
	KeepAlivePeriod      time.Duration
	MaxIdleTimeout       time.Duration
	HandshakeIdleTimeout time.Duration
	MaxConcurrency       int
}

func (cfg *RunnerConfig) Validate() error {
	if cfg.Clock == nil {
		return errors.New("clock is required")
	}
	if cfg.Solana == nil {
		return errors.New("solana view is required")
	}
	if cfg.Serviceability == nil {
		return errors.New("serviceability view is required")
	}
	if cfg.Netlinker == nil {
		return errors.New("netlinker is required")
	}
	if cfg.DZNetworkEnv == "" {
		return errors.New("dz network env is required")
	}
	if cfg.Source == nil {
		return errors.New("source config is required")
	}
	if err := cfg.Source.Validate(); err != nil {
		return fmt.Errorf("source config is invalid: %w", err)
	}
	if cfg.ProbeInterval <= 0 {
		return errors.New("probe interval must be greater than 0")
	}
	if cfg.ProbeTimeout <= 0 {
		return errors.New("probe timeout must be greater than 0")
	}
	if cfg.KeepAlivePeriod <= 0 {
		return errors.New("keep alive period must be greater than 0")
	}
	if cfg.MaxIdleTimeout <= 0 {
		return errors.New("max idle timeout must be greater than 0")
	}
	if cfg.HandshakeIdleTimeout <= 0 {
		return errors.New("handshake idle timeout must be greater than 0")
	}
	if cfg.MaxConcurrency <= 0 {
		return errors.New("max concurrency must be greater than 0")
	}
	if cfg.GeoIP == nil {
		return errors.New("geoip resolver is required")
	}
	return nil
}

type Runner struct {
	log     *slog.Logger
	cfg     *RunnerConfig
	targets *TargetSet
}

func NewRunner(log *slog.Logger, cfg *RunnerConfig) (*Runner, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	targets, err := NewTargetSet(log, &TargetSetConfig{
		Clock:            cfg.Clock,
		ProbeTimeout:     cfg.ProbeTimeout,
		MaxConcurrency:   cfg.MaxConcurrency,
		VerboseFailures:  cfg.VerboseFailures,
		VerboseSuccesses: cfg.VerboseSuccesses,
	})
	if err != nil {
		return nil, err
	}
	return &Runner{
		log: log,
		cfg: cfg,

		targets: targets,
	}, nil
}

func (r *Runner) Start(ctx context.Context) <-chan error {
	errCh := make(chan error)
	go func() {
		err := r.Run(ctx)
		if err != nil {
			select {
			case errCh <- err:
			default:
				r.log.Error("runner: error channel is full, skipping error", "error", err)
			}
		}
		close(errCh)
	}()
	return errCh
}

func (r *Runner) Run(ctx context.Context) error {
	source, err := NewSource(ctx, r.log, r.cfg.Source)
	if err != nil {
		r.log.Error("runner: failed to create source", "error", err)
		return err
	}

	r.log.Info("runner: starting",
		"probeInterval", r.cfg.ProbeInterval,
		"probeTimeout", r.cfg.ProbeTimeout,
		"keepAlivePeriod", r.cfg.KeepAlivePeriod,
		"maxIdleTimeout", r.cfg.MaxIdleTimeout,
		"handshakeIdleTimeout", r.cfg.HandshakeIdleTimeout,
		"maxConcurrency", r.cfg.MaxConcurrency,
		"sourcePublicIface", source.PublicIface,
		"sourceDZIface", source.DZIface,
		"sourceMetro", source.Metro,
		"sourceMetroName", source.MetroName,
		"sourceHost", source.Host,
		"sourcePublicIP", source.PublicIP,
		"sourceDZIP", source.User.DZIP,
		"sourceUserPubKey", source.User.PubKey,
	)
	if r.cfg.InfluxAPI == nil {
		r.log.Warn("runner: influx api is not set, skipping telemetry")
	}
	if source.DZIface == "" {
		r.log.Warn("runner: source dz interface is not set, skipping doublezero probing")
	}

	ticker := r.cfg.Clock.NewTicker(r.cfg.ProbeInterval)
	defer ticker.Stop()

	r.tick(ctx)

	for {
		select {
		case <-ctx.Done():
			r.log.Info("runner: context done, stopping", "reason", ctx.Err())
			r.targets.Prune(nil)
			return nil
		case <-ticker.Chan():
			r.tick(ctx)
		}
	}
}

func (r *Runner) tick(ctx context.Context) {
	startedAt := r.cfg.Clock.Now()
	defer func() {
		metrics.TickDuration.Observe(time.Since(startedAt).Seconds())
	}()

	gossipNodes, validators, err := r.cfg.Solana.GetGossipNodesAndValidatorsByNodePubkey(ctx)
	if err != nil {
		r.log.Error("runner: failed to get solana gossip nodes and validators", "error", err)
		metrics.TickTotal.WithLabelValues("solana_err").Inc()
		return
	}

	// Get DoubleZero serviceability program data with users and devices.
	svcData, err := r.cfg.Serviceability.GetProgramData(ctx)
	if err != nil {
		r.log.Error("runner: failed to get program data", "error", err)
		metrics.TickTotal.WithLabelValues("dz_svc_err").Inc()
		return
	}

	// Get source information.
	source, err := NewSource(ctx, r.log, r.cfg.Source)
	if err != nil {
		r.log.Error("runner: failed to create source", "error", err)
		metrics.TickTotal.WithLabelValues("source_err").Inc()
		return
	}

	// Get local kernel BGP routes.
	routes := make(map[string]netlink.Route)
	if source.DZIface != "" {
		routes, err = r.cfg.Netlinker.GetBGPRoutesByDst()
		if err != nil {
			r.log.Error("runner: failed to get BGP routes", "error", err)
			metrics.TickTotal.WithLabelValues("routes_err").Inc()
			return
		}
	}

	// Build plans and deduped targets.
	allTargets := make(map[ProbeTargetID]ProbeTarget)
	allPlans := make([]ProbePlan, 0, 4096)

	// Build solana validator ICMP plans and deduped targets.
	valICMPPlanner := NewSolanaValidatorICMPPlanner(r.log, r.cfg.InfluxAPI, r.cfg.GeoIP)
	_, valICMPPlans, valICMPTargetsDedup, err := valICMPPlanner.BuildPlans(validators, svcData, source, routes)
	if err != nil {
		r.log.Error("runner: failed to get solana validator ICMP targets", "error", err)
		metrics.TickTotal.WithLabelValues("sol_val_icmp_plans_err").Inc()
		return
	}
	maps.Copy(allTargets, valICMPTargetsDedup)
	allPlans = append(allPlans, valICMPPlans...)

	// Build solana validator TPUQUIC plans and deduped targets.
	valTPUQUICPlanner := NewSolanaValidatorTPUQUICPlanner(r.log, r.cfg.InfluxAPI, r.cfg.MaxIdleTimeout, r.cfg.HandshakeIdleTimeout, r.cfg.KeepAlivePeriod)
	_, valTPUPlans, valTPUTargetsDedup, err := valTPUQUICPlanner.BuildPlans(validators, svcData, source, routes)
	if err != nil {
		r.log.Error("runner: failed to get solana validator TPUQUIC targets", "error", err)
		metrics.TickTotal.WithLabelValues("sol_val_tpuquic_plans_err").Inc()
		return
	}
	maps.Copy(allTargets, valTPUTargetsDedup)
	allPlans = append(allPlans, valTPUPlans...)

	// Build doublezero user ICMP plans and deduped targets.
	userPlanner := NewDoubleZeroUserICMPPlanner(r.log, r.cfg.InfluxAPI, r.cfg.GeoIP)
	_, userPlans, userTargetsDedup, err := userPlanner.BuildPlans(svcData, source, routes, gossipNodes, validators)
	if err != nil {
		r.log.Error("runner: failed to get doublezero user ICMP targets", "error", err)
		metrics.TickTotal.WithLabelValues("dz_user_icmp_plans_err").Inc()
		return
	}
	maps.Copy(allTargets, userTargetsDedup)
	allPlans = append(allPlans, userPlans...)

	// Update target set, pruning any targets that are not in the given map.
	r.targets.Update(allTargets)
	metrics.TargetsCurrent.Set(float64(r.targets.Len()))

	// Execute probes for all the targets.
	results, err := r.targets.ExecuteProbes(ctx)
	if err != nil {
		r.log.Error("runner: failed to execute probes", "error", err)
		metrics.TickTotal.WithLabelValues("probes_err").Inc()
		return
	}

	// If InfluxDB is not configured, skip recording probe results.
	if r.cfg.InfluxAPI == nil {
		r.log.Warn("runner: influx api is not set, skipping telemetry")
	}

	// Record and summarize results (driven by plans).
	endAt := r.cfg.Clock.Now()
	summaries := buildSummaries(r.log)

	for _, p := range allPlans {
		res := results[p.ID]
		if res == nil {
			continue
		}

		labels := []string{
			string(p.Kind),
			string(p.Path),
			metricsProbeTypeFromKind(p.Kind),
		}
		switch {
		case res.OK:
			metrics.PlanProbesSuccessTotal.WithLabelValues(labels...).Inc()

		case res.FailReason == ProbeFailReasonNotReady:
			metrics.PlanProbesNotReadyTotal.WithLabelValues(labels...).Inc()

		default:
			metrics.PlanProbesFailTotal.WithLabelValues(
				string(p.Kind),
				string(p.Path),
				metricsProbeTypeFromKind(p.Kind),
				string(res.FailReason),
			).Inc()
		}

		p.Record(res)

		s := summaries[summaryKey{p.Kind, p.Path}]
		if s == nil {
			continue
		}
		s.add(res)
	}

	duration := endAt.Sub(startedAt)

	keys := []summaryKey{
		{PlanKindSolValICMP, ProbePathPublicInternet},
		{PlanKindSolValTPUQUIC, ProbePathPublicInternet},
		{PlanKindDZUserICMP, ProbePathPublicInternet},
	}
	if r.cfg.Source.DZIface != "" {
		keys = append(keys,
			summaryKey{PlanKindSolValICMP, ProbePathDoubleZero},
			summaryKey{PlanKindSolValTPUQUIC, ProbePathDoubleZero},
			summaryKey{PlanKindDZUserICMP, ProbePathDoubleZero},
		)
	}
	for _, k := range keys {
		summaries[k].log(duration)
	}

	r.log.Info("runner: tick", "goroutines", runtime.NumGoroutine(), "targets", r.targets.Len())
	metrics.TickTotal.WithLabelValues("ok").Inc()
}
