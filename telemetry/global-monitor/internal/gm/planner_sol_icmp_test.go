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
	geo := &fakeGeoIP{rec: nil}
	p := NewSolanaValidatorICMPPlanner(log, nil, geo)

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
	geo := &fakeGeoIP{rec: nil}
	p := NewSolanaValidatorICMPPlanner(log, nil, geo)

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
	geo := &fakeGeoIP{rec: nil}
	p := NewSolanaValidatorICMPPlanner(log, nil, geo)

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

func TestGlobalMonitor_SolanaValidatorICMPPlanner_Record_WritesExpectedClickHouseRows(t *testing.T) {
	log := slog.New(slog.NewTextHandler(&strings.Builder{}, nil))
	ch := newFakeProbeWriter()
	geo := &fakeGeoIP{rec: nil}
	p := NewSolanaValidatorICMPPlanner(log, ch, geo)

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

	dzT, err := NewICMPProbeTarget(log, "dz0", val.Node.GossipIP, &ICMPProbeTargetConfig{})
	require.NoError(t, err)
	pubT, err := NewICMPProbeTarget(log, "eth0", val.Node.GossipIP, &ICMPProbeTargetConfig{})
	require.NoError(t, err)

	ts := time.Unix(1700000000, 0)

	t.Run("success writes row with stats", func(t *testing.T) {
		ch := newFakeProbeWriter()
		p.chWriter = ch

		res := &ProbeResult{
			Timestamp: ts,
			OK:        true,
			Stats: &ProbeStats{
				PacketsSent: 10, PacketsRecv: 9, PacketsLost: 1, LossRatio: 0.1,
				RTTMin: 10 * time.Millisecond, RTTAvg: 15 * time.Millisecond, RTTStdDev: 2 * time.Millisecond,
			},
		}
		p.recordResult(src, val, dzT, &uTarget, res)

		rows := ch.SolICMPRows()
		require.Len(t, rows, 1)
		row := rows[0]

		require.Equal(t, ts, row.Timestamp)
		require.Equal(t, string(ProbeTypeICMP), row.ProbeType)
		require.Equal(t, string(ProbePathDoubleZero), row.ProbePath)
		require.Equal(t, val.Node.Pubkey.String(), row.ValidatorPubkey)
		require.Equal(t, val.VoteAccount.VotePubkey.String(), row.ValidatorVotePubkey)
		require.Equal(t, val.Node.GossipIP.To4().String(), row.TargetIP)
		require.Equal(t, "dz0", row.SourceIface)
		require.Equal(t, src.User.DZIP.String(), row.SourceIP)
		require.Equal(t, uTarget.Device.Code, row.TargetDZDCode)
		require.Equal(t, uTarget.Device.Exchange.Code, row.TargetDZDMetroCode)

		require.True(t, row.ProbeOK)
		require.Empty(t, row.ProbeFailReason)
		require.InDelta(t, 15.0, row.ProbeRTTAvgMs, 1)
		require.Equal(t, int64(10), row.ProbePacketsSent)
		require.InDelta(t, 0.1, row.ProbeLossRatio, 0.01)

		require.Equal(t, "CA", row.TargetGeoIPCountryCode)
		require.InDelta(t, 43.7, row.TargetGeoIPLatitude, 0.01)
		require.InDelta(t, 0.42, row.ValidatorLeaderRatio, 0.01)
	})

	t.Run("not-ready does not write", func(t *testing.T) {
		ch := newFakeProbeWriter()
		p.chWriter = ch

		res := &ProbeResult{Timestamp: ts, OK: false, FailReason: ProbeFailReasonNotReady}
		p.recordResult(src, val, pubT, &uTarget, res)

		require.Len(t, ch.SolICMPRows(), 0)
	})

	t.Run("failure writes row with probe_ok=false", func(t *testing.T) {
		ch := newFakeProbeWriter()
		p.chWriter = ch

		res := &ProbeResult{Timestamp: ts, OK: false, FailReason: ProbeFailReasonTimeout}
		p.recordResult(src, val, pubT, &uTarget, res)

		rows := ch.SolICMPRows()
		require.Len(t, rows, 1)
		row := rows[0]

		require.False(t, row.ProbeOK)
		require.Equal(t, string(ProbeFailReasonTimeout), row.ProbeFailReason)
		require.Equal(t, string(ProbePathPublicInternet), row.ProbePath)
		require.Equal(t, "eth0", row.SourceIface)
		require.Equal(t, src.PublicIP.String(), row.SourceIP)
		require.Zero(t, row.ProbeRTTAvgMs)
	})

	t.Run("unknown iface does not write", func(t *testing.T) {
		ch := newFakeProbeWriter()
		p.chWriter = ch

		weirdT, err := NewICMPProbeTarget(log, "weird0", val.Node.GossipIP, &ICMPProbeTargetConfig{})
		require.NoError(t, err)

		res := &ProbeResult{Timestamp: ts, OK: false, FailReason: ProbeFailReasonTimeout}
		p.recordResult(src, val, weirdT, &uTarget, res)

		require.Len(t, ch.SolICMPRows(), 0)
	})

	t.Run("zero vote pubkey writes empty validator_vote_pubkey", func(t *testing.T) {
		ch := newFakeProbeWriter()
		p.chWriter = ch

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

		rows := ch.SolICMPRows()
		require.Len(t, rows, 1)
		require.Equal(t, valNoVote.Node.Pubkey.String(), rows[0].ValidatorPubkey)
		require.Empty(t, rows[0].ValidatorVotePubkey)
	})

	t.Run("nil chWriter does not panic", func(t *testing.T) {
		p.chWriter = nil
		res := &ProbeResult{Timestamp: ts, OK: false, FailReason: ProbeFailReasonTimeout}
		p.recordResult(src, val, pubT, &uTarget, res)
	})
}
