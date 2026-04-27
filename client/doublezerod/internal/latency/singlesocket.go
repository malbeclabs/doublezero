package latency

import (
	"context"
	"log/slog"
	"net"
	"sync"
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

			// Match by echo.ID = the target index we set when sending. Raw
			// ICMP sockets preserve the ID byte-for-byte, so this filters out
			// stray echo replies (e.g. responses to other processes' pings on
			// the host) that would otherwise be misattributed to one of our
			// slots and produce bogus RTTs.
			idx := echo.ID
			if idx < 0 || idx >= len(targets) {
				continue
			}
			if peer.String() != targets[idx].IP.String() {
				continue
			}

			// Lock pairs with the sender's write of `.sent` below. The kernel
			// roundtrip provides a logical happens-before, but the race
			// detector can't see across socket I/O — explicit synchronization
			// is required.
			states[idx].mu.Lock()
			if states[idx].rtts[seq].got {
				states[idx].mu.Unlock()
				continue
			}
			sent := states[idx].rtts[seq].sent
			if sent.IsZero() {
				// Reply arrived before the matching send was recorded —
				// shouldn't happen given the echo.ID match, but defensive:
				// computing receiveTime.Sub(zero) would saturate to
				// MaxDuration and poison the result.
				states[idx].mu.Unlock()
				continue
			}
			states[idx].rtts[seq].got = true
			states[idx].rtts[seq].rtt = receiveTime.Sub(sent)
			states[idx].mu.Unlock()
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
			states[i].mu.Lock()
			states[i].rtts[seq].sent = time.Now()
			states[i].mu.Unlock()
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
	sent time.Time
	rtt  time.Duration
	got  bool
}

type probeState struct {
	target ProbeTarget
	// mu protects concurrent access to rtts[*].sent between the sender (main)
	// goroutine and the reader goroutine. The other rtts fields are only
	// written by the reader and read by buildResults after readerDone closes.
	mu   sync.Mutex
	rtts [pingCount]rttEntry
}

func buildResults(states []probeState) []LatencyResult {
	results := make([]LatencyResult, len(states))
	// Iterate by index so we don't copy probeState (which contains a Mutex).
	// Safe to read rtts without locking: this is called only after
	// readerDone closes, which establishes happens-before with the reader
	// goroutine, and the sender goroutine has long since finished.
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
