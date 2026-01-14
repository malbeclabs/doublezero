package gm

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/telemetry/global-monitor/internal/dz"
	"github.com/malbeclabs/doublezero/telemetry/global-monitor/internal/geoip"
	"github.com/malbeclabs/doublezero/telemetry/global-monitor/internal/netlink"
	"github.com/malbeclabs/doublezero/telemetry/global-monitor/internal/sol"
	"github.com/stretchr/testify/require"
)

func TestGlobalMonitor_DoubleZeroUserICMPPlanner_getTargets_PublicOnly_WhenNoDZIface(t *testing.T) {
	log := slog.New(slog.NewTextHandler(&strings.Builder{}, nil))
	influx := newFakeWriteAPI()
	geo := &fakeGeoIP{rec: nil}

	p := NewDoubleZeroUserICMPPlanner(log, influx, geo)

	u1 := mkUser(pk(1), "203.0.113.10", "10.0.0.10", "nyc", dz.UserTypeIBRL, solana.PublicKey{})
	u2 := mkUser(pk(2), "203.0.113.11", "10.0.0.11", "sfo", dz.UserTypeIBRL, solana.PublicKey{})

	svc := &dz.ServiceabilityProgramData{
		UsersByPK: map[solana.PublicKey]dz.User{
			u1.PubKey: u1,
			u2.PubKey: u2,
		},
	}

	src := mkSource("eth0", "198.51.100.2", "", nil)

	byUser, err := p.getTargets(svc, src, map[string]netlink.Route{})
	require.NoError(t, err)
	require.Len(t, byUser, 2)

	for userPK, tgts := range byUser {
		require.NotEmpty(t, tgts)
		for _, tgt := range tgts {
			require.Equal(t, "eth0", tgt.Interface())
			u := svc.UsersByPK[userPK]
			require.Equal(t, u.ClientIP.String(), tgt.IP().String())
		}
	}
}

func TestGlobalMonitor_DoubleZeroUserICMPPlanner_getTargets_DZFilters_AndPreflightNoRoute(t *testing.T) {
	log := slog.New(slog.NewTextHandler(&strings.Builder{}, nil))
	influx := newFakeWriteAPI()
	geo := &fakeGeoIP{rec: nil}
	p := NewDoubleZeroUserICMPPlanner(log, influx, geo)

	sourceUser := mkUser(pk(99), "198.51.100.2", "10.255.0.1", "yyz", dz.UserTypeIBRL, solana.PublicKey{})
	src := mkSource("eth0", "198.51.100.2", "dz0", &sourceUser)

	uSameEx := mkUser(pk(1), "203.0.113.10", "10.0.0.10", "yyz", dz.UserTypeIBRL, solana.PublicKey{})
	uMulticast := mkUser(pk(2), "203.0.113.11", "10.0.0.11", "nyc", dz.UserTypeMulticast, solana.PublicKey{})
	uGood := mkUser(pk(3), "203.0.113.12", "10.0.0.12", "nyc", dz.UserTypeIBRL, solana.PublicKey{})

	svc := &dz.ServiceabilityProgramData{
		UsersByPK: map[solana.PublicKey]dz.User{
			uSameEx.PubKey:    uSameEx,
			uMulticast.PubKey: uMulticast,
			uGood.PubKey:      uGood,
		},
	}

	routes := map[string]netlink.Route{
		uGood.DZIP.String(): {},
	}

	byUser, err := p.getTargets(svc, src, routes)
	require.NoError(t, err)

	require.Len(t, byUser, 3)
	require.Len(t, byUser[uGood.PubKey], 2)
	require.Len(t, byUser[uSameEx.PubKey], 1)
	require.Len(t, byUser[uMulticast.PubKey], 1)

	var dzCount int
	for userPK, tgts := range byUser {
		for _, tgt := range tgts {
			if tgt.Interface() == "dz0" {
				dzCount++
				u := svc.UsersByPK[userPK]
				require.Equal(t, u.DZIP.String(), tgt.IP().String())

				cfg := tgt.cfg
				require.NotNil(t, cfg)
				require.NotNil(t, cfg.PreflightFunc)
				reason, ok := cfg.PreflightFunc(context.Background())
				require.True(t, ok)
				require.Equal(t, ProbeFailReason(""), reason)
			}
		}
	}
	require.Equal(t, 1, dzCount)

	{
		tmp, err := NewICMPProbeTarget(log, "dz0", uGood.DZIP, &ICMPProbeTargetConfig{
			PreflightFunc: func(ctx context.Context) (ProbeFailReason, bool) {
				if _, ok := map[string]netlink.Route{}[uGood.DZIP.String()]; !ok {
					return ProbeFailReasonNoRoute, false
				}
				return "", true
			},
		})
		require.NoError(t, err)
		reason, ok := tmp.cfg.PreflightFunc(context.Background())
		require.False(t, ok)
		require.Equal(t, ProbeFailReasonNoRoute, reason)
	}
}

func TestGlobalMonitor_DoubleZeroUserICMPPlanner_BuildPlans_DedupAndPaths(t *testing.T) {
	log := slog.New(slog.NewTextHandler(&strings.Builder{}, nil))
	influx := newFakeWriteAPI()
	geo := &fakeGeoIP{rec: nil}
	p := NewDoubleZeroUserICMPPlanner(log, influx, geo)

	sourceUser := mkUser(pk(99), "198.51.100.2", "10.255.0.1", "yyz", dz.UserTypeIBRL, solana.PublicKey{})
	src := mkSource("eth0", "198.51.100.2", "dz0", &sourceUser)

	u := mkUser(pk(1), "203.0.113.10", "10.0.0.10", "nyc", dz.UserTypeIBRL, pk(42))

	svc := &dz.ServiceabilityProgramData{
		UsersByPK: map[solana.PublicKey]dz.User{u.PubKey: u},
	}
	routes := map[string]netlink.Route{u.DZIP.String(): {}}

	gossip := map[solana.PublicKey]*sol.GossipNode{
		u.ValidatorPK: {GossipIP: u.ClientIP, TPUQUICIP: u.DZIP},
	}
	validators := map[solana.PublicKey]*sol.Validator{
		u.ValidatorPK: {
			VoteAccount: sol.VoteAccount{
				VotePubkey: pk(100),
			},
		},
	}

	byUser, plans, dedup, err := p.BuildPlans(svc, src, routes, gossip, validators)
	require.NoError(t, err)
	require.Len(t, byUser, 1)

	require.Len(t, plans, 2)
	require.Len(t, dedup, 2)

	for _, pl := range plans {
		require.Equal(t, PlanKindDZUserICMP, pl.Kind)
		if strings.Contains(string(pl.ID), "/dz0/") {
			require.Equal(t, ProbePathDoubleZero, pl.Path)
		} else {
			require.Equal(t, ProbePathPublicInternet, pl.Path)
		}
	}
}

func TestGlobalMonitor_DoubleZeroUserICMPPlanner_Record_WritesExpectedInfluxPoints(t *testing.T) {
	log := slog.New(slog.NewTextHandler(&strings.Builder{}, nil))
	influx := newFakeWriteAPI()
	geo := &fakeGeoIP{
		rec: &geoip.Record{
			Country:     "Canada",
			CountryCode: "CA",
			Region:      "ON",
			City:        "Toronto",
			CityID:      123,
			Metro:       "YYZ",
			ASN:         64500,
			ASNOrg:      "Example",
			Latitude:    43.7,
			Longitude:   -79.4,
		},
	}
	p := NewDoubleZeroUserICMPPlanner(log, influx, geo)

	sourceUser := mkUser(pk(99), "198.51.100.2", "10.255.0.1", "yyz", dz.UserTypeIBRL, solana.PublicKey{})
	src := mkSource("eth0", "198.51.100.2", "dz0", &sourceUser)

	u := mkUser(pk(1), "203.0.113.10", "10.0.0.10", "nyc", dz.UserTypeIBRL, pk(42))
	votePK := pk(100)
	gossip := map[solana.PublicKey]*sol.GossipNode{
		u.ValidatorPK: {GossipIP: u.ClientIP, TPUQUICIP: u.DZIP},
	}
	validators := map[solana.PublicKey]*sol.Validator{
		u.ValidatorPK: {
			VoteAccount: sol.VoteAccount{
				VotePubkey: votePK,
			},
		},
	}

	dzT, err := NewICMPProbeTarget(log, "dz0", u.DZIP, &ICMPProbeTargetConfig{})
	require.NoError(t, err)
	pubT, err := NewICMPProbeTarget(log, "eth0", u.ClientIP, &ICMPProbeTargetConfig{})
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
		p.recordResult(src, &u, dzT, res, gossip, validators)

		pts := influx.Points()
		require.Len(t, pts, 1)
		tags := pointTags(pts[0])
		fields := pointFields(pts[0])
		require.Equal(t, string(InfluxTableDoubleZeroUserICMPProbe), pts[0].Name())
		require.Equal(t, ts, pts[0].Time())

		requireTag(t, tags, "probe_type", string(ProbeTypeICMP))
		requireTag(t, tags, "user_pubkey", u.PubKey.String())
		requireTag(t, tags, "probe_path", string(ProbePathDoubleZero))
		requireTag(t, tags, "source_iface", "dz0")
		requireTag(t, tags, "source_ip", src.User.DZIP.String())
		requireTag(t, tags, "target_ip", u.DZIP.String())

		require.Equal(t, true, requireField[bool](t, fields, "probe_ok"))
		require.Contains(t, fields, "probe_rtt_avg_ms")
		require.Contains(t, fields, "probe_packets_sent")
		require.Contains(t, fields, "probe_loss_ratio")

		require.Contains(t, tags, "user_validator_pubkey")
		requireTag(t, tags, "validator_vote_pubkey", votePK.String())
		require.Contains(t, fields, "user_validator_pubkey_in_solana_vote_accounts")
		require.Contains(t, fields, "user_validator_pubkey_in_solana_gossip")

		requireTag(t, tags, "target_geoip_country_code", "CA")
		require.Contains(t, fields, "target_geoip_latitude")
	})

	t.Run("not-ready does not write", func(t *testing.T) {
		influx = newFakeWriteAPI()
		p.influxAPI = influx

		res := &ProbeResult{Timestamp: ts, OK: false, FailReason: ProbeFailReasonNotReady}
		p.recordResult(src, &u, dzT, res, gossip, validators)

		require.Len(t, influx.Points(), 0)
	})

	t.Run("failure writes probe_ok=false + fail_reason", func(t *testing.T) {
		influx = newFakeWriteAPI()
		p.influxAPI = influx

		res := &ProbeResult{Timestamp: ts, OK: false, FailReason: ProbeFailReasonTimeout}
		p.recordResult(src, &u, pubT, res, gossip, validators)

		pts := influx.Points()
		require.Len(t, pts, 1)

		tags := pointTags(pts[0])
		fields := pointFields(pts[0])

		requireTag(t, tags, "probe_path", string(ProbePathPublicInternet))
		requireTag(t, tags, "source_iface", "eth0")
		requireTag(t, tags, "source_ip", src.PublicIP.String())
		requireTag(t, tags, "target_ip", u.ClientIP.String())

		require.Equal(t, false, requireField[bool](t, fields, "probe_ok"))
		require.Contains(t, requireField[string](t, fields, "probe_fail_reason"), string(ProbeFailReasonTimeout))
	})

	t.Run("unknown iface does not write", func(t *testing.T) {
		influx = newFakeWriteAPI()
		p.influxAPI = influx

		weirdT, err := NewICMPProbeTarget(log, "weird0", u.ClientIP, &ICMPProbeTargetConfig{})
		require.NoError(t, err)

		res := &ProbeResult{Timestamp: ts, OK: false, FailReason: ProbeFailReasonTimeout}
		p.recordResult(src, &u, weirdT, res, gossip, validators)

		require.Len(t, influx.Points(), 0)
	})

	t.Run("nil influx api makes Record a no-op", func(t *testing.T) {
		p.influxAPI = nil
		res := &ProbeResult{Timestamp: ts, OK: false, FailReason: ProbeFailReasonTimeout}
		p.recordResult(src, &u, pubT, res, gossip, validators)
	})

	t.Run("validator without vote pubkey does not include validator_vote_pubkey tag", func(t *testing.T) {
		influx = newFakeWriteAPI()
		p.influxAPI = influx

		uNoVote := mkUser(pk(2), "203.0.113.11", "10.0.0.11", "nyc", dz.UserTypeIBRL, pk(43))
		validatorsNoVote := map[solana.PublicKey]*sol.Validator{
			uNoVote.ValidatorPK: {
				VoteAccount: sol.VoteAccount{
					VotePubkey: solana.PublicKey{}, // zero pubkey
				},
			},
		}
		gossipNoVote := map[solana.PublicKey]*sol.GossipNode{
			uNoVote.ValidatorPK: {GossipIP: uNoVote.ClientIP},
		}

		pubTNoVote, err := NewICMPProbeTarget(log, "eth0", uNoVote.ClientIP, &ICMPProbeTargetConfig{})
		require.NoError(t, err)

		res := &ProbeResult{
			Timestamp: ts,
			OK:        true,
			Stats: &ProbeStats{
				PacketsSent: 10, PacketsRecv: 9, PacketsLost: 1, LossRatio: 0.1,
				RTTMin: 10 * time.Millisecond, RTTAvg: 15 * time.Millisecond, RTTStdDev: 2 * time.Millisecond,
			},
		}
		p.recordResult(src, &uNoVote, pubTNoVote, res, gossipNoVote, validatorsNoVote)

		pts := influx.Points()
		require.Len(t, pts, 1)
		tags := pointTags(pts[0])
		require.Contains(t, tags, "user_validator_pubkey")
		require.NotContains(t, tags, "validator_vote_pubkey")
	})
}
