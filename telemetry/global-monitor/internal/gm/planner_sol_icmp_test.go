package gm

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/telemetry/global-monitor/internal/dz"
	"github.com/malbeclabs/doublezero/telemetry/global-monitor/internal/netlink"
	"github.com/malbeclabs/doublezero/telemetry/global-monitor/internal/sol"
	"github.com/malbeclabs/doublezero/tools/maxmind/pkg/geoip"
	"github.com/stretchr/testify/require"
)

func TestGlobalMonitor_SolanaValidatorICMPPlanner_getTargets_PublicOnly_WhenNoDZIface(t *testing.T) {
	log := slog.New(slog.NewTextHandler(&strings.Builder{}, nil))
	influx := newFakeWriteAPI()
	geo := &fakeGeoIP{rec: nil}
	p := NewSolanaValidatorICMPPlanner(log, influx, geo)

	v1 := mkValidator(pk(1), "203.0.113.10")
	v2 := mkValidator(pk(2), "203.0.113.11")
	validators := map[solana.PublicKey]*sol.Validator{
		v1.Node.Pubkey: v1,
		v2.Node.Pubkey: v2,
	}

	src := mkSource("eth0", "198.51.100.2", "", nil)

	byVal, err := p.getTargets(validators, &dz.ServiceabilityProgramData{UsersByPK: map[solana.PublicKey]dz.User{}}, src, map[string]netlink.Route{})
	require.NoError(t, err)
	require.Len(t, byVal, 2)

	for pk, tgts := range byVal {
		require.NotEmpty(t, tgts)
		for _, tgt := range tgts {
			require.Equal(t, "eth0", tgt.Interface())
			require.Equal(t, validators[pk].Node.GossipIP.String(), tgt.IP().String())
		}
	}
}

func TestGlobalMonitor_SolanaValidatorICMPPlanner_getTargets_DZFilters_AndPreflightNoRoute(t *testing.T) {
	log := slog.New(slog.NewTextHandler(&strings.Builder{}, nil))
	influx := newFakeWriteAPI()
	geo := &fakeGeoIP{rec: nil}
	p := NewSolanaValidatorICMPPlanner(log, influx, geo)

	sourceUser := mkUser(pk(99), "198.51.100.2", "10.255.0.1", "yyz", dz.UserTypeIBRL, solana.PublicKey{})
	src := mkSource("eth0", "198.51.100.2", "dz0", &sourceUser)

	val := mkValidator(pk(1), "203.0.113.10")
	validators := map[solana.PublicKey]*sol.Validator{
		val.Node.Pubkey: val,
	}

	uSameEx := mkUser(pk(10), "198.51.100.10", val.Node.GossipIP.String(), "yyz", dz.UserTypeIBRL, solana.PublicKey{})
	uGood := mkUser(pk(11), "198.51.100.11", val.Node.GossipIP.String(), "nyc", dz.UserTypeIBRL, solana.PublicKey{})
	svc := &dz.ServiceabilityProgramData{
		UsersByPK: map[solana.PublicKey]dz.User{
			uSameEx.PubKey: uSameEx,
			uGood.PubKey:   uGood,
		},
	}

	routes := map[string]netlink.Route{val.Node.GossipIP.String(): {}}

	byVal, err := p.getTargets(validators, svc, src, routes)
	require.NoError(t, err)
	require.Len(t, byVal, 1)

	tgts := byVal[val.Node.Pubkey]
	require.Len(t, tgts, 2)

	var pub, dzT *ICMPProbeTarget
	for _, t0 := range tgts {
		if t0.Interface() == "eth0" {
			pub = t0
		}
		if t0.Interface() == "dz0" {
			dzT = t0
		}
	}
	require.NotNil(t, pub)
	require.NotNil(t, dzT)
	require.Equal(t, val.Node.GossipIP.String(), pub.IP().String())
	require.Equal(t, val.Node.GossipIP.String(), dzT.IP().String())

	require.NotNil(t, dzT.cfg)
	require.NotNil(t, dzT.cfg.PreflightFunc)
	reason, ok := dzT.cfg.PreflightFunc(context.Background())
	require.True(t, ok)
	require.Equal(t, ProbeFailReason(""), reason)

	tmp, err := NewICMPProbeTarget(log, "dz0", val.Node.GossipIP, &ICMPProbeTargetConfig{
		PreflightFunc: func(ctx context.Context) (ProbeFailReason, bool) {
			if _, ok := map[string]netlink.Route{}[val.Node.GossipIP.String()]; !ok {
				return ProbeFailReasonNoRoute, false
			}
			return "", true
		},
	})
	require.NoError(t, err)
	reason, ok = tmp.cfg.PreflightFunc(context.Background())
	require.False(t, ok)
	require.Equal(t, ProbeFailReasonNoRoute, reason)
}

func TestGlobalMonitor_SolanaValidatorICMPPlanner_BuildPlans_DedupAndPaths_TargetUserLookup(t *testing.T) {
	log := slog.New(slog.NewTextHandler(&strings.Builder{}, nil))
	influx := newFakeWriteAPI()
	geo := &fakeGeoIP{rec: nil}
	p := NewSolanaValidatorICMPPlanner(log, influx, geo)

	sourceUser := mkUser(pk(99), "198.51.100.2", "10.255.0.1", "yyz", dz.UserTypeIBRL, solana.PublicKey{})
	src := mkSource("eth0", "198.51.100.2", "dz0", &sourceUser)

	val := mkValidator(pk(1), "203.0.113.10")
	val.GeoIP = nil
	validators := map[solana.PublicKey]*sol.Validator{
		val.Node.Pubkey: val,
	}

	uTarget := mkUser(pk(7), "198.51.100.7", val.Node.GossipIP.String(), "nyc", dz.UserTypeIBRL, solana.PublicKey{})
	svc := &dz.ServiceabilityProgramData{
		UsersByPK:   map[solana.PublicKey]dz.User{uTarget.PubKey: uTarget},
		UsersByDZIP: map[string]dz.User{uTarget.DZIP.String(): uTarget},
	}
	routes := map[string]netlink.Route{val.Node.GossipIP.String(): {}}

	byVal, plans, dedup, err := p.BuildPlans(validators, svc, src, routes)
	require.NoError(t, err)
	require.Len(t, byVal, 1)

	require.Len(t, plans, 2)
	require.Len(t, dedup, 2)

	for _, pl := range plans {
		require.Equal(t, PlanKindSolValICMP, pl.Kind)
		if strings.Contains(string(pl.ID), "/dz0/") {
			require.Equal(t, ProbePathDoubleZero, pl.Path)
		} else {
			require.Equal(t, ProbePathPublicInternet, pl.Path)
		}
	}
}

func TestGlobalMonitor_SolanaValidatorICMPPlanner_Record_WritesExpectedInfluxPoints(t *testing.T) {
	log := slog.New(slog.NewTextHandler(&strings.Builder{}, nil))
	influx := newFakeWriteAPI()
	geo := &fakeGeoIP{rec: nil}
	p := NewSolanaValidatorICMPPlanner(log, influx, geo)

	sourceUser := mkUser(pk(99), "198.51.100.2", "10.255.0.1", "yyz", dz.UserTypeIBRL, solana.PublicKey{})
	src := mkSource("eth0", "198.51.100.2", "dz0", &sourceUser)

	val := mkValidator(pk(1), "203.0.113.10")
	val.LeaderRatio = 0.42
	val.VoteAccount.VotePubkey = pk(100)
	val.GeoIP = &geoip.Record{
		Country:     "Canada",
		CountryCode: "CA",
		Region:      "ON",
		City:        "Toronto",
		CityID:      123,
		MetroName:   "Yorkton",
		ASN:         64500,
		ASNOrg:      "Example",
		Latitude:    43.7,
		Longitude:   -79.4,
	}
	uTarget := mkUser(pk(7), "198.51.100.7", "10.0.0.7", "nyc", dz.UserTypeIBRL, solana.PublicKey{})

	pubT, err := NewICMPProbeTarget(log, "eth0", val.Node.GossipIP, &ICMPProbeTargetConfig{})
	require.NoError(t, err)
	dzT, err := NewICMPProbeTarget(log, "dz0", val.Node.GossipIP, &ICMPProbeTargetConfig{})
	require.NoError(t, err)

	ts := time.Unix(1700000000, 0)

	t.Run("success writes probe_ok=true + stats fields", func(t *testing.T) {
		influx = newFakeWriteAPI()
		p.influxAPI = influx

		res := &ProbeResult{
			Timestamp: ts,
			OK:        true,
			Stats: &ProbeStats{
				PacketsSent: 10, PacketsRecv: 9, PacketsLost: 1, LossRatio: 0.1,
				RTTMin: 10 * time.Millisecond, RTTAvg: 15 * time.Millisecond, RTTStdDev: 2 * time.Millisecond,
			},
		}
		p.recordResult(src, val, dzT, &uTarget, res)

		pts := influx.Points()
		require.Len(t, pts, 1)
		tags := pointTags(pts[0])
		fields := pointFields(pts[0])
		require.Equal(t, string(InfluxTableSolanaValidatorICMPProbe), pts[0].Name())
		require.Equal(t, ts, pts[0].Time())

		requireTag(t, tags, "probe_type", string(ProbeTypeICMP))
		requireTag(t, tags, "validator_pubkey", val.Node.Pubkey.String())
		requireTag(t, tags, "validator_vote_pubkey", val.VoteAccount.VotePubkey.String())
		requireTag(t, tags, "probe_path", string(ProbePathDoubleZero))
		requireTag(t, tags, "source_iface", "dz0")
		requireTag(t, tags, "source_ip", src.User.DZIP.String())
		requireTag(t, tags, "target_ip", val.Node.GossipIP.To4().String())

		requireTag(t, tags, "target_dzd_code", uTarget.Device.Code)
		requireTag(t, tags, "target_dzd_metro_code", uTarget.Device.Exchange.Code)

		require.Equal(t, true, requireField[bool](t, fields, "probe_ok"))
		require.Contains(t, fields, "probe_rtt_avg_ms")
		require.Contains(t, fields, "probe_packets_sent")
		require.Contains(t, fields, "probe_loss_ratio")

		require.Contains(t, tags, "target_geoip_country_code")
		require.Contains(t, fields, "target_geoip_latitude")
		require.Contains(t, fields, "validator_leader_ratio")
	})

	t.Run("not-ready does not write", func(t *testing.T) {
		influx = newFakeWriteAPI()
		p.influxAPI = influx

		res := &ProbeResult{Timestamp: ts, OK: false, FailReason: ProbeFailReasonNotReady}
		p.recordResult(src, val, pubT, &uTarget, res)

		require.Len(t, influx.Points(), 0)
	})

	t.Run("failure writes probe_ok=false + fail_reason", func(t *testing.T) {
		influx = newFakeWriteAPI()
		p.influxAPI = influx

		res := &ProbeResult{Timestamp: ts, OK: false, FailReason: ProbeFailReasonTimeout}
		p.recordResult(src, val, pubT, &uTarget, res)

		pts := influx.Points()
		require.Len(t, pts, 1)

		tags := pointTags(pts[0])
		fields := pointFields(pts[0])

		requireTag(t, tags, "probe_path", string(ProbePathPublicInternet))
		requireTag(t, tags, "source_iface", "eth0")
		requireTag(t, tags, "source_ip", src.PublicIP.String())
		requireTag(t, tags, "target_ip", val.Node.GossipIP.To4().String())

		require.Equal(t, false, requireField[bool](t, fields, "probe_ok"))
		require.Contains(t, requireField[string](t, fields, "probe_fail_reason"), string(ProbeFailReasonTimeout))
	})

	t.Run("unknown iface does not write", func(t *testing.T) {
		influx = newFakeWriteAPI()
		p.influxAPI = influx

		weirdT, err := NewICMPProbeTarget(log, "weird0", val.Node.GossipIP, &ICMPProbeTargetConfig{})
		require.NoError(t, err)

		res := &ProbeResult{Timestamp: ts, OK: false, FailReason: ProbeFailReasonTimeout}
		p.recordResult(src, val, weirdT, &uTarget, res)

		require.Len(t, influx.Points(), 0)
	})

	t.Run("nil influx api makes Record a no-op", func(t *testing.T) {
		p.influxAPI = nil
		res := &ProbeResult{Timestamp: ts, OK: false, FailReason: ProbeFailReasonTimeout}
		p.recordResult(src, val, pubT, &uTarget, res)
	})

	t.Run("zero vote pubkey is not included in tags", func(t *testing.T) {
		influx = newFakeWriteAPI()
		p.influxAPI = influx

		valNoVote := mkValidator(pk(2), "203.0.113.20")
		valNoVote.VoteAccount.VotePubkey = solana.PublicKey{} // zero pubkey
		pubTNoVote, err := NewICMPProbeTarget(log, "eth0", valNoVote.Node.GossipIP, &ICMPProbeTargetConfig{})
		require.NoError(t, err)

		res := &ProbeResult{
			Timestamp: ts,
			OK:        true,
			Stats: &ProbeStats{
				PacketsSent: 10, PacketsRecv: 9, PacketsLost: 1, LossRatio: 0.1,
				RTTMin: 10 * time.Millisecond, RTTAvg: 15 * time.Millisecond, RTTStdDev: 2 * time.Millisecond,
			},
		}
		p.recordResult(src, valNoVote, pubTNoVote, nil, res)

		pts := influx.Points()
		require.Len(t, pts, 1)
		tags := pointTags(pts[0])
		requireTag(t, tags, "validator_pubkey", valNoVote.Node.Pubkey.String())
		require.NotContains(t, tags, "validator_vote_pubkey")
	})
}
