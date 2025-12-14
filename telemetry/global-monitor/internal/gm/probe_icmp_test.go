package gm

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	probing "github.com/prometheus-community/pro-bing"
	"github.com/stretchr/testify/require"
)

func TestGlobalMonitor_ICMPProbeTarget_NewICMPProbeTarget_validation(t *testing.T) {
	t.Parallel()

	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	ip := net.ParseIP("127.0.0.1")

	_, err := NewICMPProbeTarget(nil, "lo", ip, &ICMPProbeTargetConfig{})
	require.Error(t, err)

	_, err = NewICMPProbeTarget(log, "", ip, &ICMPProbeTargetConfig{})
	require.Error(t, err)

	_, err = NewICMPProbeTarget(log, "lo", nil, &ICMPProbeTargetConfig{})
	require.Error(t, err)

	got, err := NewICMPProbeTarget(log, "lo", ip, nil)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.NotNil(t, got.cfg)
}

func TestGlobalMonitor_ICMPProbeTarget_ID_Interface_IP(t *testing.T) {
	t.Parallel()

	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	ip := net.ParseIP("192.0.2.10")
	target, err := NewICMPProbeTarget(log, "eth0", ip, &ICMPProbeTargetConfig{})
	require.NoError(t, err)

	require.Equal(t, ProbeTargetID("icmp/eth0/192.0.2.10"), target.ID())
	require.Equal(t, "eth0", target.Interface())
	require.True(t, target.IP().Equal(ip))
}

func TestGlobalMonitor_ICMPProbeTarget_Probe_preflight_short_circuits(t *testing.T) {
	t.Parallel()

	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	ip := net.ParseIP("203.0.113.9")

	called := 0
	cfg := &ICMPProbeTargetConfig{
		PreflightFunc: func(ctx context.Context) (ProbeFailReason, bool) {
			called++
			return ProbeFailReasonNotReady, false
		},
	}

	target, err := NewICMPProbeTarget(log, "eth0", ip, cfg)
	require.NoError(t, err)

	res, err := target.Probe(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, called)
	require.NotNil(t, res)
	require.Equal(t, ProbeFailReasonNotReady, res.FailReason)
	require.False(t, res.OK)
	require.Nil(t, res.Stats)
}

func TestGlobalMonitor_ICMPProbeTarget_icmpStatsNotReady(t *testing.T) {
	t.Parallel()

	require.True(t, icmpStatsNotReady(nil))

	require.True(t, icmpStatsNotReady(&probing.Statistics{PacketsSent: 0}))

	require.True(t, icmpStatsNotReady(&probing.Statistics{
		PacketsSent: 3,
		PacketsRecv: 1,
		AvgRtt:      0,
	}))

	require.False(t, icmpStatsNotReady(&probing.Statistics{
		PacketsSent: 3,
		PacketsRecv: 0,
		AvgRtt:      0,
	}))

	require.False(t, icmpStatsNotReady(&probing.Statistics{
		PacketsSent: 3,
		PacketsRecv: 3,
		AvgRtt:      10 * time.Millisecond,
	}))
}

func TestGlobalMonitor_ICMPProbeTarget_Probe_localhost_ok(t *testing.T) {
	t.Parallel()

	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	iface := loopbackIfaceName(t)
	requireICMPProbeCapable(t, log, iface)

	cfg := &ICMPProbeTargetConfig{} // exercise default Count/Interval mutation
	target, err := NewICMPProbeTarget(log, iface, net.ParseIP("127.0.0.1"), cfg)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		res, err := target.Probe(ctx)
		return err == nil && res != nil && res.OK && res.Stats != nil &&
			res.Stats.PacketsSent > 0 && res.Stats.PacketsRecv > 0
	}, 12*time.Second, 250*time.Millisecond)

	require.Equal(t, defaultICMPCount, cfg.Count)
	require.Equal(t, defaultICMPInterval, cfg.Interval)
}

func TestGlobalMonitor_ICMPProbeTarget_Probe_timeout_maps_to_fail_reason(t *testing.T) {
	t.Parallel()

	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	iface := loopbackIfaceName(t)
	requireICMPProbeCapable(t, log, iface)

	cfg := &ICMPProbeTargetConfig{Count: 1000, Interval: time.Second}
	target, err := NewICMPProbeTarget(log, iface, net.ParseIP("127.0.0.1"), cfg)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		res, err := target.Probe(ctx)
		return err == nil && res != nil &&
			res.FailReason == ProbeFailReasonTimeout &&
			errors.Is(res.FailError, context.DeadlineExceeded)
	}, 6*time.Second, 200*time.Millisecond)
}

func requireICMPProbeCapable(t *testing.T, log *slog.Logger, iface string) {
	t.Helper()

	cfg := &ICMPProbeTargetConfig{Count: 1, Interval: 10 * time.Millisecond}
	target, err := NewICMPProbeTarget(log, iface, net.ParseIP("127.0.0.1"), cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	res, err := target.Probe(ctx)
	if err != nil {
		t.Skipf("icmp probe check errored: %v", err)
		return
	}
	if res == nil {
		t.Skip("icmp probe check returned nil result")
		return
	}

	// If we can run but stats came back "not ready", that's fine: it still proves we could open/run ICMP.
	if res.OK || res.FailReason == ProbeFailReasonNotReady {
		return
	}

	if isICMPPermissionError(res.FailError) {
		t.Skipf("icmp probe not permitted on this runner: %v", res.FailError)
		return
	}

	// Any other failure: don't skip; let the test fail loudly so we notice regressions.
	require.True(t, res.OK, "icmp probe capability check failed unexpectedly: reason=%v err=%v", res.FailReason, res.FailError)
}

func isICMPPermissionError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "operation not permitted"),
		strings.Contains(msg, "permission denied"),
		strings.Contains(msg, "socket: permission denied"),
		strings.Contains(msg, "must be root"),
		strings.Contains(msg, "requires privileged"):
		return true
	default:
		return errors.Is(err, os.ErrPermission)
	}
}

func loopbackIfaceName(t *testing.T) string {
	t.Helper()
	ifaces, err := net.Interfaces()
	require.NoError(t, err)
	for _, itf := range ifaces {
		if itf.Flags&net.FlagLoopback != 0 && itf.Flags&net.FlagUp != 0 {
			return itf.Name
		}
	}
	t.Fatalf("no up loopback interface found")
	return ""
}
