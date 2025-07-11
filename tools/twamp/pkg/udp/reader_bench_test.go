//go:build linux
// +build linux

package udp_test

import (
	"context"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/tools/twamp/pkg/udp"
)

func BenchmarkUDP_TimestampedReader_Kernel(b *testing.B) {
	conn, send := setupBenchmarkSocket(b)
	defer conn.Close()
	defer send.Close()

	r, err := udp.NewKernelTimestampedReader(slog.Default(), conn)
	if err != nil {
		b.Skipf("KernelReader unsupported: %v", err)
	}
	benchmarkRTT(b, r, send)
}

func BenchmarkUDP_TimestampedReader_Wallclock(b *testing.B) {
	conn, send := setupBenchmarkSocket(b)
	defer conn.Close()
	defer send.Close()

	r := udp.NewWallclockTimestampedReader(conn)
	benchmarkRTT(b, r, send)
}

func setupBenchmarkSocket(tb testing.TB) (*net.UDPConn, *net.UDPConn) {
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		tb.Fatalf("ResolveUDPAddr: %v", err)
	}
	recv, err := net.ListenUDP("udp", addr)
	if err != nil {
		tb.Fatalf("ListenUDP: %v", err)
	}
	send, err := net.DialUDP("udp", nil, recv.LocalAddr().(*net.UDPAddr))
	if err != nil {
		tb.Fatalf("DialUDP: %v", err)
	}
	return recv, send
}

func benchmarkRTT(b *testing.B, reader udp.TimestampedReader, send *net.UDPConn) {
	buf := make([]byte, 512)
	var sum time.Duration
	var worst time.Duration

	// warm-up
	_, err := send.Write([]byte("warmup"))
	if err != nil {
		b.Fatalf("write failed: %v", err)
	}
	_, _, err = reader.Read(b.Context(), buf)
	if err != nil {
		b.Fatalf("read failed: %v", err)
	}

	b.ResetTimer()
	for range b.N {
		start := reader.Now()

		_, err := send.Write([]byte("ping"))
		if err != nil {
			b.Fatalf("write failed: %v", err)
		}

		ctx, cancel := context.WithTimeout(b.Context(), time.Second)
		_, recvTime, err := reader.Read(ctx, buf)
		cancel()
		if err != nil {
			b.Fatalf("read failed: %v", err)
		}

		delta := recvTime.Sub(start)
		if delta < 0 {
			delta = -delta
		}
		sum += delta
		if delta > worst {
			worst = delta
		}
	}
	b.ReportMetric(float64(sum.Microseconds())/float64(b.N), "avgRTT_us")
	b.ReportMetric(float64(worst.Microseconds()), "worstRTT_us")
}
