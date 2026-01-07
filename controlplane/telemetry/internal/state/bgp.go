//go:build linux

package state

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unsafe"

	"github.com/m-lab/tcp-info/inetdiag"
	mtcp "github.com/m-lab/tcp-info/tcp"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/netns"
	"github.com/mdlayher/netlink"
	"golang.org/x/sys/unix"
)

const (
	kindBGPSockets = "bgp-sockets"
	bgpPort        = 179
	tcpEstablished = 1
)

// InetDiagMemInfo mirrors INET_DIAG_MEMINFO.
// These are internal kernel memory accounting values for the socket.
// Units are bytes.
type InetDiagMemInfo struct {
	// Bytes queued in the receive queue.
	Rmem uint32 `json:"rmem"`

	// Bytes queued in the send queue.
	Wmem uint32 `json:"wmem"`

	// Forward allocation (used by some TCP internals).
	Fmem uint32 `json:"fmem"`

	// Total memory allocated for the socket.
	Tmem uint32 `json:"tmem"`
}

// InetDiagSkMemInfo mirrors INET_DIAG_SKMEMINFO.
// This is lower-level socket memory accounting used by ss for diagnostics.
// Units are bytes unless otherwise noted.
type InetDiagSkMemInfo struct {
	// Memory allocated for receive buffers.
	RmemAlloc uint32 `json:"rmem_alloc"`

	// Configured receive buffer size (SO_RCVBUF).
	Rcvbuf uint32 `json:"rcvbuf"`

	// Memory allocated for send buffers.
	WmemAlloc uint32 `json:"wmem_alloc"`

	// Configured send buffer size (SO_SNDBUF).
	Sndbuf uint32 `json:"sndbuf"`

	// Forward allocation for retransmits / internal queues.
	FwdAlloc uint32 `json:"fwd_alloc"`

	// Bytes currently queued for sending.
	WmemQueued uint32 `json:"wmem_queued"`

	// Memory used by socket options.
	Optmem uint32 `json:"optmem"`

	// Number of packets waiting in the backlog queue.
	Backlog uint32 `json:"backlog"`

	// Number of packets dropped due to memory pressure.
	Drops uint32 `json:"drops"`
}

// BGPSocketState represents a single TCP socket associated with BGP
// (local or remote port 179), roughly equivalent to one row of `ss -ti`.
type BGPSocketState struct {
	// Address family: "inet" or "inet6".
	Family string `json:"family"`

	// TCP state (e.g. ESTABLISHED).
	State string `json:"state"`

	// Local socket address.
	LocalIP   string `json:"local_ip"`
	LocalPort uint16 `json:"local_port"`

	// Remote peer address.
	RemoteIP   string `json:"remote_ip"`
	RemotePort uint16 `json:"remote_port"`

	// Whether the kernel provided tcp_info for this socket.
	// Some sockets or kernels may omit this.
	TCPInfoPresent bool `json:"tcp_info_present"`

	// Number of bytes of tcp_info received from the kernel.
	// Useful for debugging kernel / userspace struct mismatches.
	TCPInfoLen int `json:"tcp_info_len,omitempty"`

	// Active congestion control algorithm (e.g. "cubic").
	CongestionControl *string `json:"congestion_control,omitempty"`

	// High-level socket memory accounting.
	MemInfo *InetDiagMemInfo `json:"meminfo,omitempty"`

	// Low-level socket memory accounting.
	SkMemInfo *InetDiagSkMemInfo `json:"skmeminfo,omitempty"`

	// Smoothed round-trip time (RTT), in milliseconds.
	RTTms *float64 `json:"rtt_ms,omitempty"`

	// RTT variance, in milliseconds.
	RTTVarms *float64 `json:"rttvar_ms,omitempty"`

	// Sender congestion window (in packets).
	Cwnd *uint32 `json:"cwnd,omitempty"`

	// Retransmission timeout, in milliseconds.
	RTOms *uint32 `json:"rto_ms,omitempty"`

	// Delayed ACK timeout, in milliseconds.
	ATOms *uint32 `json:"ato_ms,omitempty"`

	// Sender maximum segment size (MSS), in bytes.
	SndMSS *uint32 `json:"snd_mss,omitempty"`

	// Total bytes sent on this connection.
	BytesSent *uint64 `json:"bytes_sent,omitempty"`

	// Total bytes received on this connection.
	BytesReceived *uint64 `json:"bytes_received,omitempty"`

	// Kernel pacing rate, in bytes per second.
	PacingRate *uint64 `json:"pacing_rate_Bps,omitempty"`

	// Kernel delivery rate estimate, in bytes per second.
	DeliveryRate *uint64 `json:"delivery_rate_Bps,omitempty"`

	// Approximate send rate derived as:
	//   cwnd * snd_mss / rtt
	// Matches ss's "send XMbps" field.
	SendRateMbps *float64 `json:"send_Mbps,omitempty"`

	// Pacing rate converted to Mbps.
	PacingRateMbps *float64 `json:"pacing_rate_Mbps,omitempty"`

	// Delivery rate converted to Mbps.
	DeliveryRateMbps *float64 `json:"delivery_rate_Mbps,omitempty"`
}

func (c *Collector) collectBGPStateSnapshot(ctx context.Context) error {
	now := c.cfg.Clock.Now().UTC()

	sockets, err := GetBGPSocketStatsInNamespace(ctx, c.cfg.BGPNamespace)
	if err != nil {
		return fmt.Errorf("failed to get BGP socket stats: %w", err)
	}

	data, err := json.Marshal(sockets)
	if err != nil {
		return fmt.Errorf("failed to marshal BGP state: %w", err)
	}

	snap := StateSnapshot{
		Metadata: StateSnapshotMetadata{
			Kind:      kindBGPSockets,
			Timestamp: now.Format(time.RFC3339),
			Device:    c.cfg.DevicePK.String(),
		},
		Data: json.RawMessage(data),
	}

	c.log.Debug("state: uploading snapshot", "kind", kindBGPSockets, "dataSize", len(data))

	snapJSON, err := json.Marshal(snap)
	if err != nil {
		return fmt.Errorf("failed to marshal state snapshot: %w", err)
	}

	if _, err := c.cfg.StateIngest.UploadSnapshot(ctx, kindBGPSockets, now, snapJSON); err != nil {
		return fmt.Errorf("failed to upload state snapshot: %w", err)
	}

	return nil
}

func GetBGPSocketStatsInNamespace(ctx context.Context, namespace string) ([]BGPSocketState, error) {
	return netns.RunInNamespace(namespace, func() ([]BGPSocketState, error) {
		return GetBGPSocketStats(ctx, namespace)
	})
}

func GetBGPSocketStats(ctx context.Context, namespace string) ([]BGPSocketState, error) {
	conn, err := netlink.Dial(unix.NETLINK_INET_DIAG, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to dial netlink: %w", err)
	}
	defer conn.Close()

	famName := map[uint8]string{inetdiag.AF_INET: "inet", inetdiag.AF_INET6: "inet6"}
	sockets := make([]BGPSocketState, 0, 4)

	for _, fam := range []uint8{inetdiag.AF_INET, inetdiag.AF_INET6} {
		req := inetdiag.NewReqV2(fam, uint8(unix.IPPROTO_TCP), uint32(0xFFFFFFFF))
		req.IDiagExt = 0
		req.IDiagExt |= 1 << (inetdiag.INET_DIAG_INFO - 1)
		req.IDiagExt |= 1 << (inetdiag.INET_DIAG_CONG - 1)
		req.IDiagExt |= 1 << (inetdiag.INET_DIAG_MEMINFO - 1)
		req.IDiagExt |= 1 << (inetdiag.INET_DIAG_SKMEMINFO - 1)

		msgs, err := conn.Execute(netlink.Message{
			Header: netlink.Header{
				Type:  inetdiag.SOCK_DIAG_BY_FAMILY,
				Flags: netlink.Request | netlink.Dump,
			},
			Data: req.Serialize(),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to get inet diag: %w", err)
		}

		for _, nm := range msgs {
			raw, rest := inetdiag.SplitInetDiagMsg(nm.Data)
			idm, err := raw.Parse()
			if err != nil {
				continue
			}
			if idm.IDiagState != uint8(tcpEstablished) {
				continue
			}

			lp := idm.ID.SPort()
			rp := idm.ID.DPort()
			if lp != bgpPort && rp != bgpPort {
				continue
			}

			row := BGPSocketState{
				Family:     famName[fam],
				State:      mtcp.State(idm.IDiagState).String(),
				LocalIP:    idm.ID.SrcIP().String(),
				LocalPort:  lp,
				RemoteIP:   idm.ID.DstIP().String(),
				RemotePort: rp,
			}

			ex, err := parseDiagExtras(rest)
			if err == nil {
				row.CongestionControl = ex.cong
				row.MemInfo = ex.mem
				row.SkMemInfo = ex.skmem
				row.TCPInfoPresent = ex.ti != nil
				row.TCPInfoLen = ex.tiLen

				if ex.ti != nil {
					applyTCPInfo(&row, ex.ti, ex.tiLen)
				}
			}

			sockets = append(sockets, row)
		}
	}

	return sockets, nil
}

type tcpInfoDecoded struct {
	ti    unix.TCPInfo
	tiLen int
	ok    bool
}

// decodeTCPInfoBytes copies min(len(b), sizeof(unix.TCPInfo)) into a unix.TCPInfo.
// tiLen is the number of bytes actually copied (the safe readable prefix).
func decodeTCPInfoBytes(b []byte) tcpInfoDecoded {
	if len(b) == 0 {
		return tcpInfoDecoded{}
	}
	var ti unix.TCPInfo
	sz := int(unsafe.Sizeof(ti))
	n := len(b)
	if n > sz {
		n = sz
	}
	dst := unsafe.Slice((*byte)(unsafe.Pointer(&ti)), sz)
	copy(dst[:n], b[:n])
	return tcpInfoDecoded{ti: ti, tiLen: n, ok: true}
}

func applyTCPInfo(row *BGPSocketState, ti *unix.TCPInfo, tiLen int) {
	if row == nil || ti == nil || tiLen <= 0 {
		return
	}

	var tmp unix.TCPInfo
	offRTT, szRTT := int(unsafe.Offsetof(tmp.Rtt)), int(unsafe.Sizeof(tmp.Rtt))
	offRTTV, szRTTV := int(unsafe.Offsetof(tmp.Rttvar)), int(unsafe.Sizeof(tmp.Rttvar))
	offCWND, szCWND := int(unsafe.Offsetof(tmp.Snd_cwnd)), int(unsafe.Sizeof(tmp.Snd_cwnd))
	offRTO, szRTO := int(unsafe.Offsetof(tmp.Rto)), int(unsafe.Sizeof(tmp.Rto))
	offATO, szATO := int(unsafe.Offsetof(tmp.Ato)), int(unsafe.Sizeof(tmp.Ato))
	offMSS, szMSS := int(unsafe.Offsetof(tmp.Snd_mss)), int(unsafe.Sizeof(tmp.Snd_mss))
	offBS, szBS := int(unsafe.Offsetof(tmp.Bytes_sent)), int(unsafe.Sizeof(tmp.Bytes_sent))
	offBR, szBR := int(unsafe.Offsetof(tmp.Bytes_received)), int(unsafe.Sizeof(tmp.Bytes_received))
	offPR, szPR := int(unsafe.Offsetof(tmp.Pacing_rate)), int(unsafe.Sizeof(tmp.Pacing_rate))
	offDR, szDR := int(unsafe.Offsetof(tmp.Delivery_rate)), int(unsafe.Sizeof(tmp.Delivery_rate))

	has := func(off, sz int) bool {
		if off < 0 || sz < 0 || tiLen < 0 {
			return false
		}
		if off > tiLen {
			return false
		}
		return sz <= tiLen-off
	}

	var rttMs float64
	if has(offRTT, szRTT) && has(offRTTV, szRTTV) {
		rtt := float64(ti.Rtt) / 1000.0
		rttv := float64(ti.Rttvar) / 1000.0
		row.RTTms, row.RTTVarms = &rtt, &rttv
		rttMs = rtt
	}

	var cwnd uint32
	var sndMSS uint32
	if has(offCWND, szCWND) {
		cwnd = ti.Snd_cwnd
		row.Cwnd = &cwnd
	}
	if has(offRTO, szRTO) {
		rtoMs := ti.Rto / 1000
		row.RTOms = &rtoMs
	}
	if has(offATO, szATO) {
		atoMs := ti.Ato / 1000
		row.ATOms = &atoMs
	}
	if has(offMSS, szMSS) {
		sndMSS = ti.Snd_mss
		row.SndMSS = &sndMSS
	}

	if has(offBS, szBS) {
		bs := ti.Bytes_sent
		row.BytesSent = &bs
	}
	if has(offBR, szBR) {
		br := ti.Bytes_received
		row.BytesReceived = &br
	}
	if has(offPR, szPR) {
		pr := ti.Pacing_rate
		row.PacingRate = &pr
		if pr > 0 {
			mbps := bpsToMbpsFloat(float64(pr))
			row.PacingRateMbps = &mbps
		}
	}
	if has(offDR, szDR) {
		dr := ti.Delivery_rate
		row.DeliveryRate = &dr
		if dr > 0 {
			mbps := bpsToMbpsFloat(float64(dr))
			row.DeliveryRateMbps = &mbps
		}
	}

	if row.RTTms != nil && row.Cwnd != nil && row.SndMSS != nil && rttMs > 0 && cwnd > 0 && sndMSS > 0 {
		bps := (float64(cwnd) * float64(sndMSS)) / (rttMs / 1000.0)
		mbps := bpsToMbpsFloat(bps)
		row.SendRateMbps = &mbps
	}
}

func bpsToMbpsFloat(bps float64) float64 { return bps * 8.0 / 1_000_000.0 }

type diagExtras struct {
	ti    *unix.TCPInfo
	tiLen int
	cong  *string
	mem   *InetDiagMemInfo
	skmem *InetDiagSkMemInfo
}

func parseDiagExtras(attrBytes []byte) (diagExtras, error) {
	ad, err := netlink.NewAttributeDecoder(attrBytes)
	if err != nil {
		return diagExtras{}, err
	}

	var out diagExtras
	for ad.Next() {
		switch ad.Type() {
		case inetdiag.INET_DIAG_INFO:
			dec := decodeTCPInfoBytes(ad.Bytes())
			if dec.ok {
				out.ti = &dec.ti
				out.tiLen = dec.tiLen
			}

		case inetdiag.INET_DIAG_CONG:
			s := strings.TrimRight(ad.String(), "\x00")
			if s != "" {
				out.cong = &s
			}

		case inetdiag.INET_DIAG_MEMINFO:
			b := ad.Bytes()
			if len(b) >= 16 {
				out.mem = &InetDiagMemInfo{
					Rmem: binary.LittleEndian.Uint32(b[0:4]),
					Wmem: binary.LittleEndian.Uint32(b[4:8]),
					Fmem: binary.LittleEndian.Uint32(b[8:12]),
					Tmem: binary.LittleEndian.Uint32(b[12:16]),
				}
			}

		case inetdiag.INET_DIAG_SKMEMINFO:
			b := ad.Bytes()
			if len(b) >= 9*4 {
				u := make([]uint32, len(b)/4)
				for i := range u {
					u[i] = binary.LittleEndian.Uint32(b[i*4 : i*4+4])
				}
				out.skmem = &InetDiagSkMemInfo{
					RmemAlloc:  u[0],
					Rcvbuf:     u[1],
					WmemAlloc:  u[2],
					Sndbuf:     u[3],
					FwdAlloc:   u[4],
					WmemQueued: u[5],
					Optmem:     u[6],
					Backlog:    u[7],
					Drops:      u[8],
				}
			}
		}
	}

	if err := ad.Err(); err != nil {
		return diagExtras{}, err
	}
	return out, nil
}
