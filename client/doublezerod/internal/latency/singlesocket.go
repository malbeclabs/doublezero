package latency

import (
	"context"
	"log/slog"
	"math"
	"net"
	"sync/atomic"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

const (
	icmpProtoNum = 1 // IANA protocol number for ICMPv4
	pingCount    = 3
	pingTimeout  = 10 * time.Second
	pingInterval = time.Second
	pingPayload  = 56 // bytes, same as UdpPing
)

// SingleSocketPing probes all targets using a single shared ICMP raw socket.
// This avoids per-target socket creation and eliminates kernel-level ICMP
// response fan-out contention that inflates RTT measurements under concurrency.
func SingleSocketPing(ctx context.Context, targets []ProbeTarget) []LatencyResult {
	if len(targets) == 0 {
		return nil
	}

	// ICMP Echo ID is a 16-bit field; we use it to identify the target index
	// when matching replies, so we can't support more targets than that.
	if len(targets) > math.MaxUint16 {
		slog.Error("latency: too many targets for single-socket ICMP probe", "count", len(targets), "max", math.MaxUint16)
		results := make([]LatencyResult, len(targets))
		for i, t := range targets {
			results[i] = LatencyResult{Device: t.Device, InterfaceName: t.InterfaceName, IP: t.IP, Reachable: false}
		}
		return results
	}

	conn, err := icmp.ListenPacket("ip4:icmp", "")
	if err != nil {
		slog.Error("latency: failed to open ICMP socket", "error", err)
		results := make([]LatencyResult, len(targets))
		for i, t := range targets {
			results[i] = LatencyResult{Device: t.Device, InterfaceName: t.InterfaceName, IP: t.IP, Reachable: false}
		}
		return results
	}
	defer conn.Close()

	states := make([]probeState, len(targets))
	for i, t := range targets {
		states[i].target = t
	}

	payload := make([]byte, pingPayload)

	// Set read deadline for the entire probe session.
	conn.SetReadDeadline(time.Now().Add(pingTimeout)) //nolint:errcheck

	totalExpected := len(targets) * pingCount
	var totalReceived atomic.Int32

	// Start reader goroutine BEFORE sending so replies are timestamped
	// on arrival, not when dequeued after all sends complete.
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		buf := make([]byte, 1500)
		for totalReceived.Load() < int32(totalExpected) {
			n, peer, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			receiveTime := time.Now()

			msg, err := icmp.ParseMessage(icmpProtoNum, buf[:n])
			if err != nil {
				continue
			}
			if msg.Type != ipv4.ICMPTypeEchoReply {
				continue
			}

			echo, ok := msg.Body.(*icmp.Echo)
			if !ok {
				continue
			}

			seq := echo.Seq
			if seq < 0 || seq >= pingCount {
				continue
			}

			// The echo ID carries the target index from the send side; verify
			// the peer IP matches to guard against kernel ID mangling or
			// stray replies from another process's pings.
			idx := echo.ID
			if idx < 0 || idx >= len(states) {
				continue
			}
			peerAddr, ok := peer.(*net.IPAddr)
			if !ok || !states[idx].target.IP.Equal(peerAddr.IP) {
				continue
			}

			if states[idx].rtts[seq].got {
				continue
			}
			sentNanos := states[idx].rtts[seq].sent.Load()
			if sentNanos == 0 {
				// Reply observed before the send time was recorded. Shouldn't
				// happen since the sender records sent before calling WriteTo,
				// but guard so we never compute a garbage RTT.
				continue
			}
			states[idx].rtts[seq].got = true
			states[idx].rtts[seq].rtt = receiveTime.Sub(time.Unix(0, sentNanos))
			totalReceived.Add(1)
		}
	}()

	// Send pings in rounds with interval between each round.
	for seq := 0; seq < pingCount; seq++ {
		for i, t := range targets {
			msg := &icmp.Message{
				Type: ipv4.ICMPTypeEcho,
				Code: 0,
				Body: &icmp.Echo{
					ID:   i,
					Seq:  seq,
					Data: payload,
				},
			}
			b, err := msg.Marshal(nil)
			if err != nil {
				slog.Error("latency: failed to marshal ICMP message", "target", t.IP, "error", err)
				continue
			}
			// Record send time atomically so the reader goroutine observes it
			// with proper memory synchronization.
			states[i].rtts[seq].sent.Store(time.Now().UnixNano())
			_, err = conn.WriteTo(b, &net.IPAddr{IP: t.IP})
			if err != nil {
				slog.Error("latency: failed to send ICMP echo", "target", t.IP, "error", err)
			}
		}

		if seq < pingCount-1 {
			select {
			case <-ctx.Done():
				conn.Close()
				<-readerDone
				return buildResults(states)
			case <-time.After(pingInterval):
			}
		}
	}

	// Wait for reader to finish (hits read deadline or all received).
	<-readerDone

	return buildResults(states)
}

type rttEntry struct {
	sent atomic.Int64 // UnixNano of send time; 0 means not yet sent
	rtt  time.Duration
	got  bool
}

type probeState struct {
	target ProbeTarget
	rtts   [pingCount]rttEntry
}

func buildResults(states []probeState) []LatencyResult {
	results := make([]LatencyResult, len(states))
	for i := range states {
		s := &states[i]
		var received int
		var total, minRtt, maxRtt time.Duration
		minRtt = time.Hour // sentinel

		for j := range s.rtts {
			r := &s.rtts[j]
			if r.got {
				received++
				total += r.rtt
				if r.rtt < minRtt {
					minRtt = r.rtt
				}
				if r.rtt > maxRtt {
					maxRtt = r.rtt
				}
			}
		}

		result := LatencyResult{
			Device:        s.target.Device,
			InterfaceName: s.target.InterfaceName,
			IP:            s.target.IP,
		}

		if received > 0 {
			result.Reachable = true
			result.Min = int64(minRtt)
			result.Max = int64(maxRtt)
			result.Avg = int64(total / time.Duration(received))
			result.Loss = float64(pingCount-received) / float64(pingCount) * 100
		} else {
			result.Reachable = false
			result.Loss = 100
		}

		results[i] = result
	}
	return results
}
