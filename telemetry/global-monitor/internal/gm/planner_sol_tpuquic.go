package gm

import (
	"context"
	"log/slog"
	"net"
	"strconv"
	"time"

	"github.com/gagliardetto/solana-go"
	influxdb2api "github.com/influxdata/influxdb-client-go/v2/api"
	"github.com/influxdata/influxdb-client-go/v2/api/write"
	"github.com/malbeclabs/doublezero/telemetry/global-monitor/internal/dz"
	"github.com/malbeclabs/doublezero/telemetry/global-monitor/internal/netlink"
	"github.com/malbeclabs/doublezero/telemetry/global-monitor/internal/sol"
)

type SolanaValidatorTPUQUICPlanner struct {
	log       *slog.Logger
	influxAPI influxdb2api.WriteAPI

	maxIdleTimeout       time.Duration
	handshakeIdleTimeout time.Duration
	keepAlivePeriod      time.Duration
}

func NewSolanaValidatorTPUQUICPlanner(log *slog.Logger, influxAPI influxdb2api.WriteAPI, maxIdleTimeout time.Duration, handshakeIdleTimeout time.Duration, keepAlivePeriod time.Duration) *SolanaValidatorTPUQUICPlanner {
	return &SolanaValidatorTPUQUICPlanner{
		log:       log,
		influxAPI: influxAPI,

		maxIdleTimeout:       maxIdleTimeout,
		handshakeIdleTimeout: handshakeIdleTimeout,
		keepAlivePeriod:      keepAlivePeriod,
	}
}

func (p *SolanaValidatorTPUQUICPlanner) BuildPlans(
	validators map[solana.PublicKey]*sol.Validator,
	svcData *dz.ServiceabilityProgramData,
	source *Source,
	routes map[string]netlink.Route,
) (map[solana.PublicKey][]*TPUQUICProbeTarget, []ProbePlan, map[ProbeTargetID]ProbeTarget, error) {
	byValidator, err := p.getTargets(validators, svcData, source, routes)
	if err != nil {
		return nil, nil, nil, err
	}

	dedup := make(map[ProbeTargetID]ProbeTarget)
	plans := make([]ProbePlan, 0, 1024)

	for valPK, tgts := range byValidator {
		val := validators[valPK]
		if val == nil {
			continue
		}

		for _, t := range tgts {
			tgt := t
			id := tgt.ID()
			dedup[id] = tgt

			path := ProbePathPublicInternet
			if source.DZIface != "" && tgt.Interface() == source.DZIface {
				path = ProbePathDoubleZero
			}

			var targetUser *dz.User
			ip, _, err := net.SplitHostPort(tgt.Addr())
			if err == nil {
				if u, ok := svcData.UsersByDZIP[ip]; ok {
					targetUser = &u
				}
			}

			plans = append(plans, ProbePlan{
				ID:   id,
				Kind: PlanKindSolValTPUQUIC,
				Path: path,
				Record: func(res *ProbeResult) {
					if p.influxAPI == nil {
						return
					}
					p.recordResult(source, val, tgt, targetUser, res)
				},
			})
		}
	}

	return byValidator, plans, dedup, nil
}

func (p *SolanaValidatorTPUQUICPlanner) getTargets(validators map[solana.PublicKey]*sol.Validator, svcData *dz.ServiceabilityProgramData, source *Source, routes map[string]netlink.Route) (map[solana.PublicKey][]*TPUQUICProbeTarget, error) {
	targets := make(map[solana.PublicKey][]*TPUQUICProbeTarget)

	// Build map of validator pubkey by gossip IP.
	validatorsByTPUQUICIP := make(map[string]map[solana.PublicKey]*sol.Validator)
	validatorsByTPUQUICAddr := make(map[string]map[solana.PublicKey]*sol.Validator)
	for _, val := range validators {
		addr, ok := val.Node.TPUQUICAddr()
		if !ok {
			continue
		}
		if val.Node.TPUQUICIP == nil || val.Node.TPUQUICIP.To4() == nil || val.Node.TPUQUICIP.To4().IsUnspecified() {
			continue
		}
		ip := val.Node.TPUQUICIP.To4().String()

		if _, ok := validatorsByTPUQUICAddr[addr]; !ok {
			validatorsByTPUQUICAddr[addr] = make(map[solana.PublicKey]*sol.Validator)
		}
		validatorsByTPUQUICAddr[addr][val.Node.Pubkey] = val

		if _, ok := validatorsByTPUQUICIP[ip]; !ok {
			validatorsByTPUQUICIP[ip] = make(map[solana.PublicKey]*sol.Validator)
		}
		validatorsByTPUQUICIP[ip][val.Node.Pubkey] = val
	}

	// Build public internet probe targets.
	for addr, vals := range validatorsByTPUQUICAddr {
		for _, val := range vals {
			target, err := NewTPUQUICProbeTarget(p.log, source.PublicIface, addr, &TPUQUICProbeTargetConfig{
				MaxIdleTimeout:       p.maxIdleTimeout,
				HandshakeIdleTimeout: p.handshakeIdleTimeout,
				KeepAlivePeriod:      p.keepAlivePeriod,
			})
			if err != nil {
				p.log.Error("sol/tpuquic: failed to create probe target for public internet", "error", err)
				continue
			}
			targets[val.Node.Pubkey] = append(targets[val.Node.Pubkey], target)
		}
	}

	// If DZ interface is not set, skip building DoubleZero probe targets.
	if source.DZIface == "" {
		return targets, nil
	}

	// Build DoubleZero probe targets.
	for _, user := range svcData.UsersByPK {
		if user.DZIP == nil || user.DZIP.To4() == nil || user.DZIP.To4().IsUnspecified() {
			continue
		}
		vals, ok := validatorsByTPUQUICIP[user.DZIP.String()]
		if !ok || vals == nil {
			continue
		}
		for _, val := range vals {
			addr, ok := val.Node.TPUQUICAddr()
			if !ok {
				continue
			}

			// Exclude users on DZ who are in the same exchange as the source DZD.
			if user.Device.Exchange.Code == source.User.Device.Exchange.Code {
				continue
			}

			target, err := NewTPUQUICProbeTarget(p.log, source.DZIface, addr, &TPUQUICProbeTargetConfig{
				MaxIdleTimeout:       p.maxIdleTimeout,
				HandshakeIdleTimeout: p.handshakeIdleTimeout,
				KeepAlivePeriod:      p.keepAlivePeriod,
				PreflightFunc: func(ctx context.Context) (ProbeFailReason, bool) {
					if _, ok := routes[user.DZIP.String()]; !ok {
						return ProbeFailReasonNoRoute, false
					}
					return "", true
				},
			})
			if err != nil {
				p.log.Error("sol/tpuquic: failed to create probe target for doublezero user", "error", err)
				continue
			}
			targets[val.Node.Pubkey] = append(targets[val.Node.Pubkey], target)
		}
	}

	return targets, nil
}

func (p *SolanaValidatorTPUQUICPlanner) recordResult(source *Source, val *sol.Validator, target *TPUQUICProbeTarget, targetUser *dz.User, result *ProbeResult) {
	if val == nil || val.Node.TPUQUICIP == nil || val.Node.TPUQUICIP.To4() == nil {
		return
	}
	targetIP := val.Node.TPUQUICIP.To4()
	targetIPBlock24 := targetIP.Mask(net.CIDRMask(24, 32))
	targetIface := target.Interface()
	targetAddr := target.Addr()

	tags := map[string]string{
		// Probe tags.
		"probe_type":       string(ProbeTypeTPUQUIC),
		"validator_pubkey": val.Node.Pubkey.String(),

		// Target tags.
		"target_ip":          targetIP.String(),
		"target_ip_block_24": targetIPBlock24.String(),
		"target_port":        strconv.Itoa(int(val.Node.TPUQUICPort)),
		"target_endpoint":    targetAddr,

		// Source tags.
		"source_metro":      source.Metro,
		"source_metro_name": source.MetroName,
		"source_host":       source.Host,
	}
	if !val.VoteAccount.VotePubkey.IsZero() {
		tags["validator_vote_pubkey"] = val.VoteAccount.VotePubkey.String()
	}
	fields := map[string]any{
		"validator_leader_ratio":   val.LeaderRatio,
		"validator_stake_lamports": val.VoteAccount.ActivatedStake,
	}

	// Source tags.
	if source.User != nil && source.User.Device != nil {
		tags["source_dzd_code"] = source.User.Device.Code
		tags["source_dzd_metro_code"] = source.User.Device.Exchange.Code
		tags["source_dzd_metro_name"] = source.User.Device.Exchange.Name
	}

	// Target tags.
	if targetUser != nil {
		tags["target_dzd_code"] = targetUser.Device.Code
		tags["target_dzd_metro_code"] = targetUser.Device.Exchange.Code
		tags["target_dzd_metro_name"] = targetUser.Device.Exchange.Name
	}

	switch targetIface {
	case source.DZIface:
		// Probe tags.
		tags["probe_path"] = string(ProbePathDoubleZero)

		// Source tags.
		tags["source_iface"] = source.DZIface
		tags["source_ip"] = source.User.DZIP.String()
	case source.PublicIface:
		// Probe tags.
		tags["probe_path"] = string(ProbePathPublicInternet)

		// Source tags.
		tags["source_iface"] = source.PublicIface
		tags["source_ip"] = source.PublicIP.String()
	default:
		p.log.Error("sol/tpuquic: unknown source interface while recording solana validator probe result", "interface", targetIface, "pubkey", val.Node.Pubkey.String(), "target", target.ID())
		return
	}

	// Target GeoIP tags and fields.
	if val.GeoIP != nil {
		tags["target_geoip_country"] = val.GeoIP.Country
		tags["target_geoip_country_code"] = val.GeoIP.CountryCode
		tags["target_geoip_region"] = val.GeoIP.Region
		tags["target_geoip_city"] = val.GeoIP.City
		tags["target_geoip_city_id"] = strconv.Itoa(val.GeoIP.CityID)
		tags["target_geoip_metro"] = val.GeoIP.MetroName
		tags["target_geoip_asn"] = strconv.Itoa(int(val.GeoIP.ASN))
		tags["target_geoip_asn_org"] = val.GeoIP.ASNOrg
		fields["target_geoip_latitude"] = val.GeoIP.Latitude
		fields["target_geoip_longitude"] = val.GeoIP.Longitude
	}

	point := write.NewPoint(string(InfluxTableSolanaValidatorTPUQUICProbe), tags, fields, result.Timestamp)

	switch result.FailReason {
	case "":
		// No failure. Proceed to record probe success.
	case ProbeFailReasonNotReady:
		// Probe not ready. Skip recording probe result.
		return
	default:
		point.AddField("probe_ok", false)
		point.AddField("probe_fail_reason", result.FailReason)
		if p.influxAPI != nil {
			p.influxAPI.WritePoint(point)
		}
		return
	}

	if result.Stats == nil {
		p.log.Error("sol/tpuquic: stats are nil while recording solana validator probe result", "pubkey", val.Node.Pubkey.String(), "endpoint", targetAddr, "interface", targetIface)
		return
	}
	stats := result.Stats

	point.AddField("probe_ok", true)
	point.AddField("probe_rtt_avg_ms", float64(stats.RTTAvg.Milliseconds()))
	point.AddField("probe_rtt_latest_ms", float64(stats.RTTAvg.Milliseconds()))
	point.AddField("probe_rtt_min_ms", float64(stats.RTTMin.Milliseconds()))
	point.AddField("probe_rtt_dev_ms", float64(stats.RTTStdDev.Milliseconds()))
	point.AddField("probe_packets_sent", int64(stats.PacketsSent))
	point.AddField("probe_packets_recv", int64(stats.PacketsRecv))
	point.AddField("probe_packets_lost", int64(stats.PacketsLost))
	point.AddField("probe_loss_ratio", float64(stats.LossRatio))

	if p.influxAPI != nil {
		p.influxAPI.WritePoint(point)
	}
}
