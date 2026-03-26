package latency

import (
	"context"
	"log/slog"
	"net"
	"sync"
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

	// Build a lookup from destination IP to list of target indices (multiple
	// targets can share an IP in theory, but typically they don't).
	ipToIndices := make(map[string][]int, len(targets))
	for i, t := range targets {
		ipToIndices[t.IP.String()] = append(ipToIndices[t.IP.String()], i)
	}

	payload := make([]byte, pingPayload)

	// Send all pings in rounds: round 0, wait 1s, round 1, wait 1s, round 2.
	for seq := 0; seq < pingCount; seq++ {
		for i, t := range targets {
			msg := &icmp.Message{
				Type: ipv4.ICMPTypeEcho,
				Code: 0,
				Body: &icmp.Echo{
					ID:   i, // encode target index
					Seq:  seq,
					Data: payload,
				},
			}
			b, err := msg.Marshal(nil)
			if err != nil {
				slog.Error("latency: failed to marshal ICMP message", "target", t.IP, "error", err)
				continue
			}
			states[i].rtts[seq].sent = time.Now()
			_, err = conn.WriteTo(b, &net.IPAddr{IP: t.IP})
			if err != nil {
				slog.Error("latency: failed to send ICMP echo", "target", t.IP, "error", err)
			}
		}

		if seq < pingCount-1 {
			select {
			case <-ctx.Done():
				return buildResults(states)
			case <-time.After(pingInterval):
			}
		}
	}

	// Collect responses until timeout or all received.
	deadline := time.Now().Add(pingTimeout)
	conn.SetReadDeadline(deadline) //nolint:errcheck

	totalExpected := len(targets) * pingCount
	totalReceived := 0

	buf := make([]byte, 1500)
	var mu sync.Mutex

	// Read responses in a loop.
	for totalReceived < totalExpected && time.Now().Before(deadline) {
		n, peer, err := conn.ReadFrom(buf)
		if err != nil {
			// Timeout or closed — done reading.
			break
		}

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

		// Route reply by source IP to the matching target(s).
		peerIP := peer.String()
		indices, ok := ipToIndices[peerIP]
		if !ok {
			continue
		}

		mu.Lock()
		for _, idx := range indices {
			if !states[idx].rtts[seq].got {
				states[idx].rtts[seq].got = true
				states[idx].rtts[seq].rtt = time.Since(states[idx].rtts[seq].sent)
				totalReceived++
			}
		}
		mu.Unlock()
	}

	return buildResults(states)
}

type rttEntry struct {
	sent time.Time
	rtt  time.Duration
	got  bool
}

type probeState struct {
	target ProbeTarget
	rtts   [pingCount]rttEntry
}

func buildResults(states []probeState) []LatencyResult {
	results := make([]LatencyResult, len(states))
	for i, s := range states {
		var received int
		var total, minRtt, maxRtt time.Duration
		minRtt = time.Hour // sentinel

		for _, r := range s.rtts {
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
