package twamplight_test

import (
	"context"
	"io"
	"log/slog"
	"math"
	"net"
	"runtime"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/go-connections/nat"
	twamplight "github.com/malbeclabs/doublezero/tools/twamp/pkg/light"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

func BenchmarkTWAMP_Linux(b *testing.B) {
	if runtime.GOOS != "linux" {
		b.Skip("Linux-specific benchmark")
	}

	b.Run("linux sender and linux reflector", func(b *testing.B) {
		runBench(b, func(iface string, localAddr, remoteAddr *net.UDPAddr) (twamplight.Sender, error) {
			return twamplight.NewLinuxSender(b.Context(), iface, localAddr, remoteAddr)
		}, startLinuxReflector)
	})

	b.Run("linux sender and basic reflector", func(b *testing.B) {
		runBench(b, func(iface string, localAddr, remoteAddr *net.UDPAddr) (twamplight.Sender, error) {
			return twamplight.NewLinuxSender(b.Context(), iface, localAddr, remoteAddr)
		}, startBasicReflector)
	})

	b.Run("linux sender and container reflector", func(b *testing.B) {
		runBench(b, func(iface string, localAddr, remoteAddr *net.UDPAddr) (twamplight.Sender, error) {
			return twamplight.NewLinuxSender(b.Context(), iface, localAddr, remoteAddr)
		}, startContainerReflector)
	})
}

func BenchmarkTWAMP_Basic(b *testing.B) {
	b.Run("basic sender and basic reflector", func(b *testing.B) {
		log := slog.New(slog.NewTextHandler(io.Discard, nil))

		runBench(b, func(iface string, localAddr, remoteAddr *net.UDPAddr) (twamplight.Sender, error) {
			return twamplight.NewBasicSender(b.Context(), log, iface, localAddr, remoteAddr)
		}, startBasicReflector)
	})

	b.Run("basic sender and container reflector", func(b *testing.B) {
		runBench(b, func(iface string, localAddr, remoteAddr *net.UDPAddr) (twamplight.Sender, error) {
			return twamplight.NewBasicSender(b.Context(), log, iface, localAddr, remoteAddr)
		}, startContainerReflector)
	})
}

func runBench(
	b *testing.B,
	newSender func(iface string, localAddr, remoteAddr *net.UDPAddr) (twamplight.Sender, error),
	newReflector func(ctx context.Context, b *testing.B) *net.UDPAddr,
) {
	ctx, cancel := context.WithCancel(b.Context())
	defer cancel()

	remoteAddr := newReflector(ctx, b)

	sender, err := newSender("", nil, remoteAddr)
	require.NoError(b, err)
	b.Cleanup(func() { sender.Close() })

	var mu sync.Mutex
	var rtts []float64
	var failures int

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			iterCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
			rtt, err := sender.Probe(iterCtx)
			cancel()

			mu.Lock()
			if err != nil {
				failures++
			} else {
				rtts = append(rtts, float64(rtt.Microseconds()))
			}
			mu.Unlock()
		}
	})
	b.StopTimer()

	reportRTTMetrics(b, rtts, failures)
}

func reportRTTMetrics(b *testing.B, rtts []float64, failures int) {
	if len(rtts) == 0 {
		b.Fatalf("no successful probes")
	}
	sort.Float64s(rtts)
	n := len(rtts)

	var sum float64
	for _, rtt := range rtts {
		sum += rtt
	}
	mean := sum / float64(n)
	median := rtts[n/2]
	if n%2 == 0 {
		median = (rtts[n/2-1] + rtts[n/2]) / 2
	}
	p95 := rtts[int(0.95*float64(n))]
	p99 := rtts[int(0.99*float64(n))]

	var deltas []float64
	for i := 1; i < n; i++ {
		deltas = append(deltas, math.Abs(rtts[i]-rtts[i-1]))
	}
	var jitter float64
	if len(deltas) > 0 {
		var deltaSum float64
		for _, d := range deltas {
			deltaSum += d
		}
		deltaMean := deltaSum / float64(len(deltas))
		var sqSum float64
		for _, d := range deltas {
			diff := d - deltaMean
			sqSum += diff * diff
		}
		jitter = math.Sqrt(sqSum / float64(len(deltas)))
	}

	success := n
	total := success + failures
	lossPct := 100 * float64(failures) / float64(total)

	b.ReportMetric(mean, "rtt_mean_µs")
	b.ReportMetric(median, "rtt_median_µs")
	b.ReportMetric(jitter, "rtt_jitter_µs")
	b.ReportMetric(p95, "rtt_p95_µs")
	b.ReportMetric(p99, "rtt_p99_µs")
	b.ReportMetric(float64(success), "success_count")
	b.ReportMetric(float64(failures), "loss_count")
	b.ReportMetric(lossPct, "loss_pct")
}

func startLinuxReflector(ctx context.Context, b *testing.B) *net.UDPAddr {
	b.Helper()

	reflector, err := twamplight.NewLinuxReflector("127.0.0.1:0", 100*time.Millisecond)
	require.NoError(b, err)

	runCtx, runCancel := context.WithCancel(ctx)
	go func() { _ = reflector.Run(runCtx) }()

	b.Cleanup(func() {
		runCancel()
		_ = reflector.Close()
	})

	return reflector.LocalAddr()
}

func startBasicReflector(ctx context.Context, b *testing.B) *net.UDPAddr {
	b.Helper()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	reflector, err := twamplight.NewBasicReflector(log, "127.0.0.1:0", 100*time.Millisecond)
	require.NoError(b, err)

	runCtx, runCancel := context.WithCancel(ctx)
	go func() { _ = reflector.Run(runCtx) }()

	b.Cleanup(func() {
		runCancel()
		_ = reflector.Close()
	})

	return reflector.LocalAddr()
}

func startContainerReflector(ctx context.Context, b *testing.B) *net.UDPAddr {
	b.Helper()

	const containerPort = "862/udp"
	hostPort, err := getAvailableUDPPort()
	require.NoError(b, err)

	req := testcontainers.ContainerRequest{
		Image:        "dz-local/twamp-reflector:dev",
		ExposedPorts: []string{containerPort},
		HostConfigModifier: func(hc *container.HostConfig) {
			hc.PortBindings = nat.PortMap{
				containerPort: []nat.PortBinding{
					{
						HostIP:   "0.0.0.0",
						HostPort: strconv.Itoa(hostPort),
					},
				},
			}
		},
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(b, err)

	b.Cleanup(func() {
		_ = container.Terminate(ctx)
	})

	host, err := container.Host(ctx)
	require.NoError(b, err)

	mapped, err := container.MappedPort(ctx, containerPort)
	require.NoError(b, err)

	hostIP, err := net.LookupIP(host)
	require.NoError(b, err)

	var hostIP4 net.IP
	for _, ip := range hostIP {
		if ip.To4() != nil {
			hostIP4 = ip
			break
		}
	}
	require.NotNil(b, hostIP4, "no IPv4 address found for host %s", host)

	return &net.UDPAddr{IP: hostIP4, Port: mapped.Int()}
}

func getAvailableUDPPort() (int, error) {
	listener, err := net.ListenPacket("udp", ":0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.LocalAddr().(*net.UDPAddr).Port, nil
}
