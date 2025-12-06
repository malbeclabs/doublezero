package gm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"strconv"
	"sync"
	"time"

	"github.com/gagliardetto/solana-go"
	influxdb2api "github.com/influxdata/influxdb-client-go/v2/api"
	"github.com/influxdata/influxdb-client-go/v2/api/write"
	"github.com/jonboulle/clockwork"
	"github.com/malbeclabs/doublezero/telemetry/global-monitor/internal/dz"
	"github.com/malbeclabs/doublezero/telemetry/global-monitor/internal/iterutil"
	"github.com/malbeclabs/doublezero/telemetry/global-monitor/internal/netlink"
	"github.com/malbeclabs/doublezero/telemetry/global-monitor/internal/sol"
	tpuquic "github.com/malbeclabs/doublezero/tools/solana/pkg/tpu-quic"
	"github.com/quic-go/quic-go"
)

const (
	ProbeFailReasonNoRoute      = "no-route"
	ProbeFailReasonFailedToDial = "dial-error"
	ProbeFailReasonTimeout      = "timeout"
	ProbeFailReasonOther        = "other"

	ProbePathPublicInternet = "public_internet"
	ProbePathDoubleZero     = "doublezero"

	InfluxTableSolanaValidatorTPUQUICProbe = "solana_validator_tpuquic_probe"
)

type RunnerConfig struct {
	Clock               clockwork.Clock
	Validators          *sol.ValidatorsView
	Serviceability      *dz.ServiceabilityView
	PublicTPUQUICProber *TPUQUICProber
	DZTPUQUICProber     *TPUQUICProber
	Netlinker           netlink.Netlinker
	DZNetworkEnv        string

	// Source configuration.
	SourcePublicIface string
	SourceDZIface     string
	SourceMetro       string
	SourceMetroName   string
	SourceHost        string
	SourcePublicIP    string
	SourceDZIP        string

	// InfluxDB configuration.
	InfluxAPI influxdb2api.WriteAPI

	// Probe configuration.
	ProbeInterval        time.Duration
	ProbeTimeout         time.Duration
	KeepAlivePeriod      time.Duration
	MaxIdleTimeout       time.Duration
	HandshakeIdleTimeout time.Duration
	MaxConcurrency       int
	RedialPeriod         time.Duration
}

func (cfg *RunnerConfig) Validate() error {
	if cfg.Clock == nil {
		return errors.New("clock is required")
	}
	if cfg.Validators == nil {
		return errors.New("validators view is required")
	}
	if cfg.Serviceability == nil {
		return errors.New("serviceability view is required")
	}
	if cfg.PublicTPUQUICProber == nil {
		return errors.New("public tpu quic prober is required")
	}
	if cfg.DZTPUQUICProber == nil {
		return errors.New("dz tpu quic prober is required")
	}
	if cfg.Netlinker == nil {
		return errors.New("netlinker is required")
	}
	if cfg.DZNetworkEnv == "" {
		return errors.New("dz network env is required")
	}
	if cfg.SourcePublicIface == "" {
		return errors.New("source public iface is required")
	}
	if cfg.SourceDZIface == "" {
		return errors.New("source dz iface is required")
	}
	if cfg.SourceMetro == "" {
		return errors.New("source metro is required")
	}
	if cfg.SourceMetroName == "" {
		return errors.New("source metro name is required")
	}
	if cfg.SourceHost == "" {
		return errors.New("source host is required")
	}
	if cfg.SourcePublicIP == "" {
		return errors.New("source public ip is required")
	}
	if cfg.SourceDZIP == "" {
		return errors.New("source dz ip is required")
	}
	if cfg.InfluxAPI == nil {
		return errors.New("influx api is required")
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
	if cfg.RedialPeriod <= 0 {
		return errors.New("redial period must be greater than 0")
	}
	return nil
}

type Runner struct {
	log *slog.Logger
	cfg *RunnerConfig
}

func NewRunner(log *slog.Logger, cfg *RunnerConfig) (*Runner, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Runner{
		log: log,
		cfg: cfg,
	}, nil
}

func (r *Runner) Start(ctx context.Context) {
	go func() {
		r.Run(ctx)
	}()
}

func (r *Runner) Run(ctx context.Context) {
	r.log.Info("runner: starting",
		"probeInterval", r.cfg.ProbeInterval,
		"probeTimeout", r.cfg.ProbeTimeout,
		"keepAlivePeriod", r.cfg.KeepAlivePeriod,
		"maxIdleTimeout", r.cfg.MaxIdleTimeout,
		"handshakeIdleTimeout", r.cfg.HandshakeIdleTimeout,
		"maxConcurrency", r.cfg.MaxConcurrency,
		"redialPeriod", r.cfg.RedialPeriod,
		"sourcePublicIface", r.cfg.SourcePublicIface,
		"sourceDZIface", r.cfg.SourceDZIface,
		"sourceMetro", r.cfg.SourceMetro,
		"sourceMetroName", r.cfg.SourceMetroName,
		"sourceHost", r.cfg.SourceHost,
		"sourcePublicIP", r.cfg.SourcePublicIP,
		"sourceDZIP", r.cfg.SourceDZIP,
	)

	ticker := r.cfg.Clock.NewTicker(r.cfg.ProbeInterval)
	defer ticker.Stop()

	r.tick(ctx)

	for {
		select {
		case <-ctx.Done():
			r.log.Info("runner: context done, stopping", "reason", ctx.Err())
			return
		case <-ticker.Chan():
			r.tick(ctx)
		}
	}
}

func (r *Runner) tick(ctx context.Context) {
	summaryPublic := newTickSummary(r.log, r.cfg.Clock, "public")
	summaryDZ := newTickSummary(r.log, r.cfg.Clock, "doublezero")

	validators, err := r.cfg.Validators.GetValidatorsByNodePubkey(ctx)
	if err != nil {
		r.log.Error("runner: failed to get validators by node pubkey", "error", err)
		return
	}
	validatorsByIP := make(map[string]*sol.Validator)
	for _, val := range validators {
		validatorsByIP[val.Node.IP.String()] = val
	}
	summaryPublic.update(func(s *tickSummary) {
		s.validators = uint64(len(validators))
	})

	// Close connections that are no longer in the validators subset.
	r.cfg.PublicTPUQUICProber.Prune(iterutil.MapFilter(maps.Values(validators), func(val *sol.Validator) (string, bool) {
		addr, ok := val.Node.TPUQUICAddr()
		if !ok {
			return "", false
		}
		return addr, true
	}))

	// Get DoubleZero users from the serviceability program.
	svcData, err := r.cfg.Serviceability.GetProgramData(ctx)
	if err != nil {
		r.log.Error("runner: failed to get program data", "error", err)
		return
	}

	// Find validators on DoubleZero.
	validatorsOnDZByNodePK := make(map[solana.PublicKey]dz.Validator)
	for _, user := range svcData.UsersByPK {
		val, ok := validatorsByIP[user.DZIP.String()]
		if !ok || val == nil {
			continue
		}
		validatorsOnDZByNodePK[val.Node.Pubkey] = dz.Validator{
			User:      user,
			Validator: *val,
		}
	}
	summaryDZ.update(func(s *tickSummary) {
		s.validators = uint64(len(validatorsOnDZByNodePK))
	})

	// Close connections that are no longer on DoubleZero.
	r.cfg.DZTPUQUICProber.Prune(iterutil.MapFilter(maps.Values(validatorsOnDZByNodePK), func(val dz.Validator) (string, bool) {
		addr, ok := val.Validator.Node.TPUQUICAddr()
		if !ok {
			return "", false
		}
		return addr, true
	}))

	// Get current doublezero status.
	sourceStatus, err := dz.GetStatus(ctx)
	if err != nil {
		r.log.Error("runner: failed to get status", "error", err)
		return
	}
	if sourceStatus.NetworkSlug != r.cfg.DZNetworkEnv {
		r.log.Error("runner: network is not mainnet-beta", "network", sourceStatus.NetworkSlug)
		return
	}
	sourceDZDCode := sourceStatus.CurrentDeviceCode

	// Exclude validators on DZ who are in the same exchange as the source DZD.
	sourceDevice, ok := svcData.DevicesByCode[sourceDZDCode]
	if !ok {
		r.log.Error("runner: source device not found", "code", sourceDZDCode)
		return
	}
	if sourceDevice.Exchange == nil {
		r.log.Error("runner: source device exchange not found", "code", sourceDZDCode)
		return
	}
	for _, val := range validatorsOnDZByNodePK {
		if val.User.Device.Exchange.Code == sourceDevice.Exchange.Code {
			delete(validatorsOnDZByNodePK, val.Validator.Node.Pubkey)
		}
	}

	// Get local kernel BGP routes.
	routes, err := r.cfg.Netlinker.GetBGPRoutesByDst()
	if err != nil {
		r.log.Error("runner: failed to get BGP routes", "error", err)
		return
	}

	// Start probing validators over public internet, and validators on DZ over DZ.
	var wg sync.WaitGroup
	sem := make(chan struct{}, r.cfg.MaxConcurrency)
	for _, validator := range validators {
		wg.Add(1)
		sem <- struct{}{}
		go func(val *sol.Validator) {
			defer wg.Done()
			defer func() { <-sem }()

			if val.Node.IP == nil || val.Node.TPUQUICPort == 0 {
				return
			}
			now := r.cfg.Clock.Now()

			// Probe validator over public internet.
			func() {
				r.probeValidatorTPUQUICOverPublicInternet(ctx, val, summaryPublic, now, sourceDevice)
			}()

			// Probe validatos on DZ over DZ.
			func() {
				dzVal, ok := validatorsOnDZByNodePK[val.Node.Pubkey]
				if !ok {
					return
				}
				r.probeValidatorTPUQUICOverDoubleZero(ctx, dzVal, routes, summaryDZ, now, sourceDevice)
			}()
		}(validator)
	}
	wg.Wait()
	close(sem)

	summaryPublic.log()
	summaryDZ.log()
}

func (r *Runner) newInfluxPointForValidatorProbe(table string, probePath string, val *sol.Validator, sourceDevice *dz.Device, now time.Time) *write.Point {
	var sourceDZDCode string
	var sourceDeviceExchangeCode, sourceDeviceExchangeName string
	if sourceDevice != nil {
		sourceDZDCode = sourceDevice.Code
		if sourceDevice.Exchange != nil {
			sourceDeviceExchangeCode = sourceDevice.Exchange.Code
			sourceDeviceExchangeName = sourceDevice.Exchange.Name
		}
	}
	tags := map[string]string{
		"probe_path":       probePath,
		"validator_pubkey": val.Node.Pubkey.String(),
		"target_ip":        val.Node.IP.String(),
		"target_port":      strconv.Itoa(int(val.Node.TPUQUICPort)),

		"source_dzd_code":       sourceDZDCode,
		"source_dzd_metro_code": sourceDeviceExchangeCode,
		"source_dzd_metro_name": sourceDeviceExchangeName,

		"source_metro":      r.cfg.SourceMetro,
		"source_metro_name": r.cfg.SourceMetroName,
		"source_host":       r.cfg.SourceHost,
		"source_ip":         r.cfg.SourcePublicIP,
		"source_iface":      r.cfg.SourcePublicIface,
	}
	fields := map[string]any{
		// TODO(snormore): Deprecate/remove validator_leader_percent.
		"validator_leader_percent": val.LeaderRatio,
		"validator_leader_ratio":   val.LeaderRatio,
	}
	if val.GeoIP != nil {
		tags["target_geoip_country"] = val.GeoIP.Country
		tags["target_geoip_country_code"] = val.GeoIP.CountryCode
		tags["target_geoip_region"] = val.GeoIP.Region
		tags["target_geoip_city"] = val.GeoIP.City
		tags["target_geoip_city_id"] = strconv.Itoa(val.GeoIP.CityID)
		tags["target_geoip_metro"] = val.GeoIP.Metro
		tags["target_geoip_asn"] = strconv.Itoa(int(val.GeoIP.ASN))
		tags["target_geoip_asn_org"] = val.GeoIP.ASNOrg
		fields["target_geoip_latitude"] = val.GeoIP.Latitude
		fields["target_geoip_longitude"] = val.GeoIP.Longitude
	}

	return write.NewPoint(table, tags, fields, now)
}

func (r *Runner) probeValidatorTPUQUICOverPublicInternet(ctx context.Context, val *sol.Validator, summary *tickSummary, now time.Time, sourceDevice *dz.Device) {
	tpuQUICAddr, ok := val.Node.TPUQUICAddr()
	if !ok {
		return
	}

	point := r.newInfluxPointForValidatorProbe(InfluxTableSolanaValidatorTPUQUICProbe, ProbePathPublicInternet, val, sourceDevice, now)

	stats, err := r.cfg.PublicTPUQUICProber.Probe(ctx, tpuQUICAddr, TPUQUICProbeConfig{
		Timeout: r.cfg.ProbeTimeout,
		DialConfig: &tpuquic.DialConfig{
			Interface:            r.cfg.SourcePublicIface,
			KeepAlivePeriod:      r.cfg.KeepAlivePeriod,
			MaxIdleTimeout:       r.cfg.MaxIdleTimeout,
			HandshakeIdleTimeout: r.cfg.HandshakeIdleTimeout,
		},
		DelayAfterDial: 2 * r.cfg.KeepAlivePeriod,
	})
	if err != nil {
		switch {
		case errors.Is(err, context.Canceled):
			r.log.Debug("runner: context cancelled while probing validator tpu quic", "pubkey", val.Node.Pubkey.String(), "address", tpuQUICAddr, "interface", r.cfg.SourcePublicIface)
			return
		case errors.Is(err, ErrStatsNotReady):
			r.log.Debug("runner: stats not ready yet after probing validator tpu quic", "pubkey", val.Node.Pubkey.String(), "address", tpuQUICAddr, "interface", r.cfg.SourcePublicIface, "stats", statsToString(stats))
			summary.update(func(s *tickSummary) {
				s.statsNotReady++
			})
			return
		case errors.Is(err, ErrFailedToDial):
			r.log.Debug("runner: failed to dial validator tpu quic", "pubkey", val.Node.Pubkey.String(), "address", tpuQUICAddr, "interface", r.cfg.SourcePublicIface, "error", err)
			point.AddField("probe_ok", false)
			point.AddField("probe_fail_reason", ProbeFailReasonFailedToDial)
			r.cfg.InfluxAPI.WritePoint(point)
			summary.update(func(s *tickSummary) {
				s.failuresFailedToDial++
				s.failures++
			})
			return
		case errors.Is(err, context.DeadlineExceeded):
			r.log.Debug("runner: timeout while probing validator tpu quic", "pubkey", val.Node.Pubkey.String(), "address", tpuQUICAddr, "interface", r.cfg.SourcePublicIface, "error", err)
			point.AddField("probe_ok", false)
			point.AddField("probe_fail_reason", ProbeFailReasonTimeout)
			r.cfg.InfluxAPI.WritePoint(point)
			summary.update(func(s *tickSummary) {
				s.failuresTimeout++
				s.failures++
			})
			return
		default:
			r.log.Debug("runner: failed to probe validator tpu quic", "error", err, "pubkey", val.Node.Pubkey.String(), "address", tpuQUICAddr, "interface", r.cfg.SourcePublicIface, "error", err)
			point.AddField("probe_ok", false)
			point.AddField("probe_fail_reason", ProbeFailReasonOther)
			r.cfg.InfluxAPI.WritePoint(point)
			summary.update(func(s *tickSummary) {
				s.failuresOther++
				s.failures++
			})
			return
		}
	}

	point.AddField("probe_ok", true)
	// TODO(snormore): Deprecate/remove probe_rtt_smoothed_ms in favor of probe_rtt_avg_ms.
	point.AddField("probe_rtt_smoothed_ms", float64(stats.SmoothedRTT.Milliseconds()))
	point.AddField("probe_rtt_avg_ms", float64(stats.SmoothedRTT.Milliseconds()))
	point.AddField("probe_rtt_latest_ms", float64(stats.LatestRTT.Milliseconds()))
	point.AddField("probe_rtt_min_ms", float64(stats.MinRTT.Milliseconds()))
	point.AddField("probe_rtt_dev_ms", float64(stats.MeanDeviation.Milliseconds()))
	point.AddField("probe_bytes_sent", int64(stats.BytesSent))
	point.AddField("probe_bytes_recv", int64(stats.BytesReceived))
	point.AddField("probe_bytes_lost", int64(stats.BytesLost))
	point.AddField("probe_packets_sent", int64(stats.PacketsSent))
	point.AddField("probe_packets_recv", int64(stats.PacketsReceived))
	point.AddField("probe_packets_lost", int64(stats.PacketsLost))
	if stats.PacketsSent > 0 {
		point.AddField("probe_loss_rate", float64(stats.PacketsLost)/float64(stats.PacketsSent))
	}
	r.cfg.InfluxAPI.WritePoint(point)
	summary.update(func(s *tickSummary) {
		s.successes++
	})
}

func (r *Runner) probeValidatorTPUQUICOverDoubleZero(ctx context.Context, dzVal dz.Validator, routes map[string]netlink.Route, summary *tickSummary, now time.Time, sourceDevice *dz.Device) {
	val := &dzVal.Validator

	tpuQUICAddr, ok := val.Node.TPUQUICAddr()
	if !ok {
		return
	}

	if dzVal.User.Device == nil || dzVal.User.Device.Exchange == nil {
		return
	}
	if dzVal.Validator.Node.IP == nil || dzVal.Validator.Node.TPUQUICPort == 0 {
		return
	}
	dzIP := dzVal.User.DZIP.String()

	point := r.newInfluxPointForValidatorProbe(InfluxTableSolanaValidatorTPUQUICProbe, ProbePathDoubleZero, val, sourceDevice, now)

	// Add DZ-specific tags.
	point.AddTag("probe_path", ProbePathDoubleZero)
	point.AddTag("target_dzd_code", dzVal.User.Device.Code)
	point.AddTag("target_dzd_metro_code", dzVal.User.Device.Exchange.Code)
	point.AddTag("target_dzd_metro_name", dzVal.User.Device.Exchange.Name)

	// Check if there is a route to the validator.
	if _, ok := routes[dzIP]; !ok {
		point.AddField("probe_ok", false)
		point.AddField("probe_fail_reason", ProbeFailReasonNoRoute)
		r.log.Debug("runner: no route to validator tpu quic", "pubkey", val.Node.Pubkey.String(), "address", tpuQUICAddr, "interface", r.cfg.SourceDZIface)
		r.cfg.InfluxAPI.WritePoint(point)
		summary.update(func(s *tickSummary) {
			s.failuresNoRoutes++
			s.failures++
		})
		return
	}

	// Probe the validator.
	stats, err := r.cfg.DZTPUQUICProber.Probe(ctx, tpuQUICAddr, TPUQUICProbeConfig{
		Timeout: r.cfg.ProbeTimeout,
		DialConfig: &tpuquic.DialConfig{
			Interface:            r.cfg.SourceDZIface,
			KeepAlivePeriod:      r.cfg.KeepAlivePeriod,
			MaxIdleTimeout:       r.cfg.MaxIdleTimeout,
			HandshakeIdleTimeout: r.cfg.HandshakeIdleTimeout,
		},
		DelayAfterDial: 2 * r.cfg.KeepAlivePeriod,
	})
	if err != nil {
		switch {
		case errors.Is(err, context.Canceled):
			r.log.Debug("runner: context cancelled while probing validator tpu quic", "pubkey", val.Node.Pubkey.String(), "address", tpuQUICAddr, "interface", r.cfg.SourceDZIface)
			return
		case errors.Is(err, ErrStatsNotReady):
			r.log.Debug("runner: stats not ready yet after probing validator tpu quic", "pubkey", val.Node.Pubkey.String(), "address", tpuQUICAddr, "interface", r.cfg.SourceDZIface, "stats", statsToString(stats))
			summary.update(func(s *tickSummary) {
				s.statsNotReady++
			})
			return
		case errors.Is(err, ErrFailedToDial):
			r.log.Debug("runner: failed to dial validator tpu quic", "pubkey", val.Node.Pubkey.String(), "address", tpuQUICAddr, "interface", r.cfg.SourceDZIface, "error", err)
			point.AddField("probe_ok", false)
			point.AddField("probe_fail_reason", ProbeFailReasonFailedToDial)
			r.cfg.InfluxAPI.WritePoint(point)
			summary.update(func(s *tickSummary) {
				s.failuresFailedToDial++
				s.failures++
			})
			return
		case errors.Is(err, context.DeadlineExceeded):
			r.log.Debug("runner: timeout while probing validator tpu quic", "pubkey", val.Node.Pubkey.String(), "address", tpuQUICAddr, "interface", r.cfg.SourceDZIface, "error", err)
			point.AddField("probe_ok", false)
			point.AddField("probe_fail_reason", ProbeFailReasonTimeout)
			r.cfg.InfluxAPI.WritePoint(point)
			summary.update(func(s *tickSummary) {
				s.failuresTimeout++
				s.failures++
			})
			return
		default:
			r.log.Debug("runner: failed to probe validator tpu quic", "error", err, "pubkey", val.Node.Pubkey.String(), "address", tpuQUICAddr, "interface", r.cfg.SourceDZIface, "error", err)
			point.AddField("probe_ok", false)
			point.AddField("probe_fail_reason", ProbeFailReasonOther)
			r.cfg.InfluxAPI.WritePoint(point)
			summary.update(func(s *tickSummary) {
				s.failuresOther++
				s.failures++
			})
			return
		}
	}

	point.AddTag("source_iface", r.cfg.SourceDZIface)
	point.AddTag("source_ip", r.cfg.SourceDZIP)

	point.AddField("probe_ok", true)
	// TODO(snormore): Deprecate/remove probe_rtt_smoothed_ms in favor of probe_rtt_avg_ms.
	point.AddField("probe_rtt_smoothed_ms", float64(stats.SmoothedRTT.Milliseconds()))
	point.AddField("probe_rtt_avg_ms", float64(stats.SmoothedRTT.Milliseconds()))
	point.AddField("probe_rtt_latest_ms", float64(stats.LatestRTT.Milliseconds()))
	point.AddField("probe_rtt_min_ms", float64(stats.MinRTT.Milliseconds()))
	point.AddField("probe_rtt_dev_ms", float64(stats.MeanDeviation.Milliseconds()))
	point.AddField("probe_bytes_sent", int64(stats.BytesSent))
	point.AddField("probe_bytes_recv", int64(stats.BytesReceived))
	point.AddField("probe_bytes_lost", int64(stats.BytesLost))
	point.AddField("probe_packets_sent", int64(stats.PacketsSent))
	point.AddField("probe_packets_recv", int64(stats.PacketsReceived))
	point.AddField("probe_packets_lost", int64(stats.PacketsLost))
	if stats.PacketsSent > 0 {
		point.AddField("probe_loss_rate", float64(stats.PacketsLost)/float64(stats.PacketsSent))
	}
	r.cfg.InfluxAPI.WritePoint(point)
	summary.update(func(s *tickSummary) {
		s.successes++
	})
}

type tickSummary struct {
	logger    *slog.Logger
	clock     clockwork.Clock
	name      string
	startedAt time.Time

	validators           uint64
	routes               uint64
	successes            uint64
	failures             uint64
	failuresNoRoutes     uint64
	failuresFailedToDial uint64
	failuresTimeout      uint64
	failuresOther        uint64
	statsNotReady        uint64

	mu sync.Mutex
}

func (s *tickSummary) update(fn func(s *tickSummary)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(s)
}

func newTickSummary(log *slog.Logger, clock clockwork.Clock, name string) *tickSummary {
	return &tickSummary{
		logger:    log,
		clock:     clock,
		startedAt: clock.Now(),
		name:      name,
	}
}

func (s *tickSummary) log() {
	attrs := []slog.Attr{
		slog.Duration("duration", s.clock.Now().Sub(s.startedAt)),
		slog.Uint64("successes", s.successes),
		slog.Uint64("failures", s.failures),
	}
	if s.failuresNoRoutes > 0 {
		attrs = append(attrs, slog.Uint64("failuresNoRoutes", s.failuresNoRoutes))
	}
	if s.failuresFailedToDial > 0 {
		attrs = append(attrs, slog.Uint64("failuresFailedToDial", s.failuresFailedToDial))
	}
	if s.failuresTimeout > 0 {
		attrs = append(attrs, slog.Uint64("failuresTimeout", s.failuresTimeout))
	}
	if s.failuresOther > 0 {
		attrs = append(attrs, slog.Uint64("failuresOther", s.failuresOther))
	}
	if s.statsNotReady > 0 {
		attrs = append(attrs, slog.Uint64("statsNotReady", s.statsNotReady))
	}
	attrs = append(attrs, slog.Uint64("validators", s.validators))
	if s.routes > 0 {
		attrs = append(attrs, slog.Uint64("routes", s.routes))
	}
	s.logger.LogAttrs(
		context.Background(),
		slog.LevelInfo,
		fmt.Sprintf("runner: tick summary (%s)", s.name),
		attrs...,
	)
}

func statsToString(stats *quic.ConnectionStats) string {
	return fmt.Sprintf("rttMin: %s, rttLatest: %s, rttSmoothed: %s, rttDev: %s, sentBytes: %d, sentPackets: %d, recvBytes: %d, recvPackets: %d, lostBytes: %d, lostPackets: %d",
		stats.MinRTT.String(),
		stats.LatestRTT.String(),
		stats.SmoothedRTT.String(),
		stats.MeanDeviation.String(),
		int64(stats.BytesSent),
		int64(stats.PacketsSent),
		int64(stats.BytesReceived),
		int64(stats.PacketsReceived),
		int64(stats.BytesLost),
		int64(stats.PacketsLost),
	)
}
