//go:build linux

package uping

import (
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"net"
	"os"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

const defaultListenerTimeout = 1 * time.Second

// ListenerConfig defines how the ICMP listener should bind and behave.
// Interface + IP pin the socket to a specific kernel network interface and address; Timeout bounds poll().
type ListenerConfig struct {
	Logger    *slog.Logger
	Interface string        // required: Linux ifname (e.g. "eth0")
	IP        net.IP        // required: IPv4 address on Interface
	Timeout   time.Duration // per-iteration poll timeout; 0 -> default
}

func (cfg *ListenerConfig) Validate() error {
	if cfg.Interface == "" {
		return fmt.Errorf("interface is required")
	}
	if cfg.IP == nil || cfg.IP.To4() == nil {
		return fmt.Errorf("IP must be an IPv4 address")
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = defaultListenerTimeout
	}
	if cfg.Timeout <= 0 {
		return fmt.Errorf("timeout must be greater than 0")
	}
	return nil
}

// Listener exposes a blocking receive/reply loop until ctx is done.
type Listener interface {
	Listen(ctx context.Context) error
}

type listener struct {
	log     *slog.Logger
	cfg     ListenerConfig
	iface   *net.Interface
	ifIndex int
	src4    net.IP // local IPv4 we will answer for (and source from)
}

func NewListener(cfg ListenerConfig) (Listener, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	ifi, err := net.InterfaceByName(cfg.Interface)
	if err != nil {
		return nil, fmt.Errorf("lookup interface %q: %w", cfg.Interface, err)
	}
	return &listener{log: cfg.Logger, cfg: cfg, iface: ifi, ifIndex: ifi.Index, src4: cfg.IP.To4()}, nil
}

// ipid is a process-wide increasing IPv4 Identification field for HDRINCL packets.
var ipid uint32

func nextIPID() uint16 { return uint16(atomic.AddUint32(&ipid, 1)) }

// onesComplement16 computes the Internet checksum over b (used for IPv4 header and ICMP).
func onesComplement16(b []byte) uint16 {
	var sum uint32
	for i := 0; i+1 < len(b); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(b[i:]))
	}
	if len(b)%2 == 1 {
		sum += uint32(b[len(b)-1]) << 8
	}
	for (sum >> 16) != 0 {
		sum = (sum & 0xFFFF) + (sum >> 16)
	}
	return ^uint16(sum)
}

func (l *listener) Listen(ctx context.Context) error {
	// Instance tag helps spot duplicate listeners (pid/object address).
	inst := fmt.Sprintf("%d/%p", os.Getpid(), l)
	if l.log != nil {
		l.log.Info("uping/recv: starting listener", "inst", inst, "iface", l.cfg.Interface, "src", l.src4)
	}

	// Raw ICMPv4 socket; requires CAP_NET_RAW. We both read requests and send replies on it.
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_RAW, unix.IPPROTO_ICMP)
	if err != nil {
		return fmt.Errorf("socket: %w", err)
	}
	defer unix.Close(fd)

	// Pin the socket to the given interface for both RX and TX routing.
	if err := unix.SetsockoptString(fd, unix.SOL_SOCKET, unix.SO_BINDTODEVICE, l.iface.Name); err != nil {
		return fmt.Errorf("bind-to-device %q: %w", l.iface.Name, err)
	}
	// Ask the kernel to attach IP_PKTINFO cmsgs so we can verify ingress ifindex.
	if err := unix.SetsockoptInt(fd, unix.IPPROTO_IP, unix.IP_PKTINFO, 1); err != nil {
		return fmt.Errorf("setsockopt IP_PKTINFO: %w", err)
	}
	// We will craft the IPv4 header ourselves to force source/dst and keep TX on the interface.
	if err := unix.SetsockoptInt(fd, unix.IPPROTO_IP, unix.IP_HDRINCL, 1); err != nil {
		return fmt.Errorf("setsockopt IP_HDRINCL: %w", err)
	}
	if err := unix.SetNonblock(fd, true); err != nil {
		return fmt.Errorf("set nonblock: %w", err)
	}

	// eventfd used to interrupt poll() on ctx cancellation without races.
	efd, err := unix.Eventfd(0, unix.EFD_NONBLOCK|unix.EFD_CLOEXEC)
	if err != nil {
		return fmt.Errorf("eventfd: %w", err)
	}
	defer unix.Close(efd)
	go func() {
		<-ctx.Done()
		var one [8]byte
		binary.LittleEndian.PutUint64(one[:], 1)
		_, _ = unix.Write(efd, one[:])
	}()

	// Reusable buffers for payload and control messages (IP_PKTINFO).
	buf := make([]byte, 65535)
	oob := make([]byte, unix.CmsgSpace(unix.SizeofInet4Pktinfo))
	pfds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}, {Fd: int32(efd), Events: unix.POLLIN}}

	for {
		// Use the smaller of ctx deadline or fallback timeout to bound poll().
		timeout := pollTimeoutMs(ctx, l.cfg.Timeout)

		nready, err := unix.Poll(pfds, timeout)
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			return fmt.Errorf("poll: %w", err)
		}
		// Wake from ctx cancellation.
		if pfds[1].Revents&unix.POLLIN != 0 {
			var tmp [8]byte
			_, _ = unix.Read(efd, tmp[:])
			return nil
		}
		// Timeout or nothing interesting on the socket.
		if nready == 0 || pfds[0].Revents&(unix.POLLIN|unix.POLLERR|unix.POLLHUP) == 0 {
			continue
		}

		// Recvmsg with OOB to get IP_PKTINFO (ingress ifindex).
		n, oobn, _, _, err := unix.Recvmsg(fd, buf, oob, 0)
		if err != nil {
			if err == unix.EAGAIN || err == unix.EWOULDBLOCK || err == unix.EINTR {
				continue
			}
			if ctx.Err() != nil {
				return nil
			}
			if l.log != nil {
				l.log.Debug("uping/recv: recvmsg error", "err", err)
			}
			continue
		}
		// Basic IPv4 sanity: IHL/Version before deeper parsing.
		if n < 20 || buf[0]>>4 != 4 {
			continue
		}

		// Enforce ingress interface: drop if IP_PKTINFO missing or ifindex mismatched.
		inIfidx := 0
		if oobn > 0 {
			cms, _ := unix.ParseSocketControlMessage(oob[:oobn])
			for _, cm := range cms {
				if cm.Header.Level == unix.IPPROTO_IP && cm.Header.Type == unix.IP_PKTINFO && len(cm.Data) >= unix.SizeofInet4Pktinfo {
					var pi unix.Inet4Pktinfo
					copy((*[unix.SizeofInet4Pktinfo]byte)(unsafe.Pointer(&pi))[:], cm.Data[:unix.SizeofInet4Pktinfo])
					inIfidx = int(pi.Ifindex)
					break
				}
			}
		}
		if inIfidx == 0 || inIfidx != l.ifIndex {
			continue
		}

		ipv := buf[:n]
		ihl := int(ipv[0]&0x0F) * 4
		// Must be ICMPv4 (proto=1) and large enough for ICMP header.
		if ihl < 20 || n < ihl+8 || ipv[9] != 1 {
			continue
		}

		// Only answer traffic addressed to our configured source IP.
		dst := net.IP(ipv[16:20]).To4()
		if !dst.Equal(l.src4) {
			continue
		}

		src := net.IP(ipv[12:16]).To4()
		icmp := ipv[ihl:]
		// Echo request (type 8), validate header length.
		if len(icmp) < 8 || icmp[0] != 8 {
			continue
		}
		// Drop corrupt ICMPs early.
		if onesComplement16(icmp) != 0 {
			continue
		}

		id := binary.BigEndian.Uint16(icmp[4:6])
		seq := binary.BigEndian.Uint16(icmp[6:8])
		payload := icmp[8:]

		// Build ICMP echo-reply (type 0), mirror id/seq/payload.
		reply := make([]byte, 8+len(payload))
		reply[0] = 0
		reply[1] = 0
		binary.BigEndian.PutUint16(reply[4:], id)
		binary.BigEndian.PutUint16(reply[6:], seq)
		copy(reply[8:], payload)
		binary.BigEndian.PutUint16(reply[2:], onesComplement16(reply))

		// Build minimal IPv4 header (HDRINCL). Copy DSCP/ECN from request.
		ip := make([]byte, 20)
		ip[0] = 0x45                                              // Version=4, IHL=5
		ip[1] = ipv[1]                                            // DSCP/ECN from request
		binary.BigEndian.PutUint16(ip[2:], uint16(20+len(reply))) // Total Length
		binary.BigEndian.PutUint16(ip[4:], nextIPID())            // Identification
		ip[6], ip[7] = 0, 0                                       // Flags/Fragment offset
		ip[8] = 64                                                // TTL
		ip[9] = 1                                                 // Protocol = ICMP
		copy(ip[12:16], l.src4)                                   // Source = our configured IP
		copy(ip[16:20], src)                                      // Dest = original sender
		binary.BigEndian.PutUint16(ip[10:], onesComplement16(ip)) // Header checksum

		// Send the full packet; SO_BINDTODEVICE keeps egress on the bound interface.
		pkt := append(ip, reply...)
		var dstSA unix.SockaddrInet4
		copy(dstSA.Addr[:], src)
		if err := unix.Sendto(fd, pkt, 0, &dstSA); err == nil {
			if l.log != nil {
				l.log.Info("uping/recv: replied", "inst", inst, "dst", src.String(), "id", id, "seq", seq, "iface", l.iface.Name, "src", l.src4)
			}
		} else if l.log != nil {
			l.log.Debug("uping/recv: HDRINCL send failed", "err", err, "iface", l.iface.Name, "src", l.src4)
		}
	}
}

// pollTimeoutMs returns a millisecond poll() timeout derived from ctx deadline
// or falls back to the provided duration. -1 means “infinite” for poll().
func pollTimeoutMs(ctx context.Context, fallback time.Duration) int {
	if dl, ok := ctx.Deadline(); ok {
		rem := time.Until(dl)
		if rem <= 0 {
			return 0
		}
		const max = int(^uint32(0) >> 1)
		if rem > (1<<31-1)*time.Millisecond {
			return max
		}
		return int(rem / time.Millisecond)
	}
	if fallback > 0 {
		const max = int(^uint32(0) >> 1)
		if fallback > (1<<31-1)*time.Millisecond {
			return max
		}
		return int(fallback / time.Millisecond)
	}
	return -1
}
