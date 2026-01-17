package gm

import (
	"context"
	"log/slog"
	"net"
	"strconv"

	"github.com/gagliardetto/solana-go"
	influxdb2api "github.com/influxdata/influxdb-client-go/v2/api"
	"github.com/influxdata/influxdb-client-go/v2/api/write"
	"github.com/malbeclabs/doublezero/telemetry/global-monitor/internal/dz"
	"github.com/malbeclabs/doublezero/telemetry/global-monitor/internal/netlink"
	"github.com/malbeclabs/doublezero/telemetry/global-monitor/internal/sol"
	"github.com/malbeclabs/doublezero/tools/maxmind/pkg/geoip"
)

type DoubleZeroUserICMPPlanner struct {
	log       *slog.Logger
	influxAPI influxdb2api.WriteAPI
	geoIP     geoip.Resolver
}

func NewDoubleZeroUserICMPPlanner(log *slog.Logger, influxAPI influxdb2api.WriteAPI, geoIP geoip.Resolver) *DoubleZeroUserICMPPlanner {
	return &DoubleZeroUserICMPPlanner{
		log:       log,
		influxAPI: influxAPI,
		geoIP:     geoIP,
	}
}

func (p *DoubleZeroUserICMPPlanner) BuildPlans(
	svcData *dz.ServiceabilityProgramData,
	source *Source,
	routes map[string]netlink.Route,
	gossipNodes map[solana.PublicKey]*sol.GossipNode,
	validators map[solana.PublicKey]*sol.Validator,
) (map[solana.PublicKey][]*ICMPProbeTarget, []ProbePlan, map[ProbeTargetID]ProbeTarget, error) {
	byUser, err := p.getTargets(svcData, source, routes)
	if err != nil {
		return nil, nil, nil, err
	}

	dedup := make(map[ProbeTargetID]ProbeTarget)
	plans := make([]ProbePlan, 0, 1024)

	for userPK, tgts := range byUser {
		u, ok := svcData.UsersByPK[userPK]
		if !ok {
			continue
		}
		user := u

		for _, t := range tgts {
			tgt := t
			id := tgt.ID()
			dedup[id] = tgt

			path := ProbePathPublicInternet
			if source.DZIface != "" && tgt.Interface() == source.DZIface {
				path = ProbePathDoubleZero
			}

			plans = append(plans, ProbePlan{
				ID:   id,
				Kind: PlanKindDZUserICMP,
				Path: path,
				Record: func(res *ProbeResult) {
					if p.influxAPI == nil {
						return
					}
					p.recordResult(source, &user, tgt, res, gossipNodes, validators)
				},
			})
		}
	}

	return byUser, plans, dedup, nil
}

func (p *DoubleZeroUserICMPPlanner) getTargets(svcData *dz.ServiceabilityProgramData, source *Source, routes map[string]netlink.Route) (map[solana.PublicKey][]*ICMPProbeTarget, error) {
	targets := make(map[solana.PublicKey][]*ICMPProbeTarget)

	// Build map of user pubkey by client IP for public internet probing.
	usersByClientIP := make(map[string]map[solana.PublicKey]*dz.User)
	for _, user := range svcData.UsersByPK {
		if user.ClientIP == nil || user.ClientIP.To4() == nil || user.ClientIP.To4().IsUnspecified() {
			continue
		}
		if _, ok := usersByClientIP[user.ClientIP.To4().String()]; !ok {
			usersByClientIP[user.ClientIP.To4().String()] = make(map[solana.PublicKey]*dz.User)
		}
		usersByClientIP[user.ClientIP.To4().String()][user.PubKey] = &user
	}

	// Build public internet probe targets.
	for _, users := range usersByClientIP {
		for _, user := range users {
			target, err := NewICMPProbeTarget(p.log, source.PublicIface, user.ClientIP, nil)
			if err != nil {
				p.log.Error("dz/icmp: failed to create probe target for public internet", "error", err)
				continue
			}
			targets[user.PubKey] = append(targets[user.PubKey], target)
		}
	}

	// If DZ interface is not set, skip building DoubleZero probe targets.
	if source.DZIface == "" {
		return targets, nil
	}

	// Build map of user by DZIP for doublezero probing.
	usersByDZIP := make(map[string]map[solana.PublicKey]*dz.User)
	for _, user := range svcData.UsersByPK {
		if user.DZIP == nil || user.DZIP.To4() == nil || user.DZIP.To4().IsUnspecified() {
			continue
		}
		if _, ok := usersByDZIP[user.DZIP.String()]; !ok {
			usersByDZIP[user.DZIP.String()] = make(map[solana.PublicKey]*dz.User)
		}
		usersByDZIP[user.DZIP.String()][user.PubKey] = &user
	}

	// Build DoubleZero probe targets.
	for _, users := range usersByDZIP {
		for _, user := range users {
			// Exclude users on DZ who are in the same exchange as the source DZD.
			if user.Device.Exchange.Code == source.User.Device.Exchange.Code {
				continue
			}

			// Exclude multicast users.
			if user.UserType == dz.UserTypeMulticast {
				continue
			}

			target, err := NewICMPProbeTarget(p.log, source.DZIface, user.DZIP, &ICMPProbeTargetConfig{
				PreflightFunc: func(ctx context.Context) (ProbeFailReason, bool) {
					if _, ok := routes[user.DZIP.String()]; !ok {
						return ProbeFailReasonNoRoute, false
					}
					return "", true
				},
			})
			if err != nil {
				p.log.Error("dz/icmp: failed to create probe target for doublezero user", "error", err)
				continue
			}
			targets[user.PubKey] = append(targets[user.PubKey], target)
		}
	}

	return targets, nil
}

func (p *DoubleZeroUserICMPPlanner) recordResult(source *Source, user *dz.User, target *ICMPProbeTarget, res *ProbeResult, gossipNodes map[solana.PublicKey]*sol.GossipNode, validators map[solana.PublicKey]*sol.Validator) {
	if user == nil {
		return
	}

	tags := map[string]string{
		// Probe tags.
		"probe_type":  string(ProbeTypeICMP),
		"user_pubkey": user.PubKey.String(),

		// Source tags.
		"source_metro":      source.Metro,
		"source_metro_name": source.MetroName,
		"source_host":       source.Host,
	}
	fields := map[string]any{}

	// Source tags.
	if source.User != nil {
		tags["source_user_pubkey"] = source.User.PubKey.String()
		if source.User.Device != nil {
			tags["source_dzd_code"] = source.User.Device.Code
			tags["source_dzd_metro_code"] = source.User.Device.Exchange.Code
			tags["source_dzd_metro_name"] = source.User.Device.Exchange.Name
		}
	}

	// Target tags.
	tags["target_dzd_code"] = user.Device.Code
	tags["target_dzd_metro_code"] = user.Device.Exchange.Code
	tags["target_dzd_metro_name"] = user.Device.Exchange.Name

	iface := target.Interface()
	switch iface {
	case source.DZIface:
		// Probe tags.
		tags["probe_path"] = string(ProbePathDoubleZero)

		// Source tags.
		tags["source_iface"] = source.DZIface
		tags["source_ip"] = source.User.DZIP.String()

		// Target tags.
		tags["target_ip"] = user.DZIP.String()
		tags["target_ip_block_24"] = user.DZIP.To4().Mask(net.CIDRMask(24, 32)).String()
	case source.PublicIface:
		// Probe tags.
		tags["probe_path"] = string(ProbePathPublicInternet)

		// Source tags.
		tags["source_iface"] = source.PublicIface
		tags["source_ip"] = source.PublicIP.String()

		// Target tags.
		tags["target_ip"] = user.ClientIP.String()
		tags["target_ip_block_24"] = user.ClientIP.To4().Mask(net.CIDRMask(24, 32)).String()
	default:
		p.log.Error("dz/icmp: unknown source interface while recording doublezero user probe result", "interface", iface, "pubkey", user.PubKey.String(), "target", target.ID())
		return
	}

	// Derive target GeoIP tags and fields.
	geoIP := p.geoIP.Resolve(user.ClientIP)
	if geoIP != nil {
		tags["target_geoip_country"] = geoIP.Country
		tags["target_geoip_country_code"] = geoIP.CountryCode
		tags["target_geoip_region"] = geoIP.Region
		tags["target_geoip_city"] = geoIP.City
		tags["target_geoip_city_id"] = strconv.Itoa(geoIP.CityID)
		tags["target_geoip_metro"] = geoIP.MetroName
		tags["target_geoip_asn"] = strconv.Itoa(int(geoIP.ASN))
		tags["target_geoip_asn_org"] = geoIP.ASNOrg
		fields["target_geoip_latitude"] = geoIP.Latitude
		fields["target_geoip_longitude"] = geoIP.Longitude
	}

	// Derive solana-specific tags and fields.
	if !user.ValidatorPK.IsZero() {
		// Is the user validator pubkey in the solana vote accounts?
		tags["user_validator_pubkey"] = user.ValidatorPK.String()
		val, ok := validators[user.ValidatorPK]
		fields["user_validator_pubkey_in_solana_vote_accounts"] = ok

		// Add validator vote pubkey if validator exists
		if ok && val != nil && !val.VoteAccount.VotePubkey.IsZero() {
			tags["validator_vote_pubkey"] = val.VoteAccount.VotePubkey.String()
		}

		// Is the user validator pubkey in the solana gossip?
		_, ok = gossipNodes[user.ValidatorPK]
		fields["user_validator_pubkey_in_solana_gossip"] = ok
	}
	targetIP := target.IP()
	if targetIP != nil && targetIP.To4() != nil && !targetIP.To4().IsUnspecified() {
		// Is the target IP in the solana gossip?
		solNodesByGossipIP := make(map[string]*sol.GossipNode)
		for _, solNode := range gossipNodes {
			ip := solNode.GossipIP
			if ip == nil || ip.To4() == nil || ip.To4().IsUnspecified() {
				continue
			}
			solNodesByGossipIP[ip.To4().String()] = solNode
		}
		_, ok := solNodesByGossipIP[targetIP.String()]
		fields["target_ip_in_solana_gossip"] = ok

		// Is the target IP in the solana gossip as a TPUQUIC IP?
		solNodesByTPUQUICIP := make(map[string]*sol.GossipNode)
		for _, solNode := range gossipNodes {
			ip := solNode.TPUQUICIP
			if ip == nil || ip.To4() == nil || ip.To4().IsUnspecified() {
				continue
			}
			solNodesByTPUQUICIP[ip.To4().String()] = solNode
		}
		_, ok = solNodesByTPUQUICIP[targetIP.String()]
		fields["target_ip_in_solana_gossip_as_tpuquic"] = ok
	}

	point := write.NewPoint(string(InfluxTableDoubleZeroUserICMPProbe), tags, fields, res.Timestamp)

	switch res.FailReason {
	case "":
		// No failure. Proceed to record probe success.
	case ProbeFailReasonNotReady:
		// Probe not ready. Skip recording probe result.
		return
	default:
		point.AddField("probe_ok", false)
		point.AddField("probe_fail_reason", res.FailReason)
		if p.influxAPI != nil {
			p.influxAPI.WritePoint(point)
		}
		return
	}

	if res.Stats == nil {
		p.log.Error("dz/icmp: stats are nil while recording doublezero user probe result", "pubkey", user.PubKey.String(), "endpoint", user.DZIP.String(), "interface", iface)
		return
	}
	stats := res.Stats

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
