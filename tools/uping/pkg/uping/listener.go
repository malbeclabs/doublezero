//go:build linux

package uping

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
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

func (l *listener) Listen(ctx context.Context) error {
	// Instance tag helps spot duplicate listeners (pid/object address).
	inst := fmt.Sprintf("%d/%p", os.Getpid(), l)
	if l.log != nil {
		l.log.Info("uping/recv: starting listener", "inst", inst, "iface", l.cfg.Interface, "src", l.src4)
	}

	// Raw ICMPv4 via net.IPConn so we can pin to device and use control messages.
	ipc, err := net.ListenIP("ip4:icmp", &net.IPAddr{IP: l.src4})
	if err != nil {
		return fmt.Errorf("ListenIP: %w", err)
	}
	defer ipc.Close()

	// Wrap in ipv4.PacketConn so we can enable control messages (interface, dst).
	ip4c := ipv4.NewPacketConn(ipc)
	defer ip4c.Close()
	if err := ip4c.SetControlMessage(ipv4.FlagInterface|ipv4.FlagDst, true); err != nil {
		return fmt.Errorf("SetControlMessage: %w", err)
	}

	// Pin the socket to the given interface for both RX and TX routing.
	if err := bindToDevice(ipc, l.iface.Name); err != nil {
		return fmt.Errorf("bind-to-device %q: %w", l.iface.Name, err)
	}

	// Interrupt blocking reads immediately on ctx cancellation.
	go func() {
		<-ctx.Done()
		_ = ipc.SetReadDeadline(time.Now().Add(-time.Hour))
	}()

	buf := make([]byte, 65535)

	for {
		// Use the smaller of ctx deadline or fallback timeout to bound reads.
		if ms := pollTimeoutMs(ctx, l.cfg.Timeout); ms < 0 {
			_ = ipc.SetReadDeadline(time.Time{})
		} else {
			_ = ipc.SetReadDeadline(time.Now().Add(time.Duration(ms) * time.Millisecond))
		}

		n, cm, raddr, err := ip4c.ReadFrom(buf)
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			if ctx.Err() != nil {
				return nil
			}
			continue
		}
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if l.log != nil {
				l.log.Debug("uping/recv: read error", "err", err)
			}
			continue
		}

		// Enforce ingress interface and destination.
		if cm == nil || cm.IfIndex != l.ifIndex || !cm.Dst.Equal(l.src4) {
			continue
		}

		m, err := icmp.ParseMessage(1, buf[:n])
		if err != nil {
			continue
		}
		if m.Type != ipv4.ICMPTypeEcho {
			continue
		}
		echo, ok := m.Body.(*icmp.Echo)
		if !ok || echo == nil {
			continue
		}

		// Build ICMP echo-reply (type 0), mirror id/seq/payload.
		reply := &icmp.Message{
			Type: ipv4.ICMPTypeEchoReply,
			Code: 0,
			Body: &icmp.Echo{ID: echo.ID, Seq: echo.Seq, Data: echo.Data},
		}
		wb, err := reply.Marshal(nil)
		if err != nil {
			continue
		}

		// Send the reply; SO_BINDTODEVICE keeps egress on the bound interface.
		dst := raddr.(*net.IPAddr)
		if _, err := ip4c.WriteTo(wb, &ipv4.ControlMessage{IfIndex: l.ifIndex, Src: l.src4}, dst); err == nil {
			if l.log != nil {
				l.log.Info("uping/recv: replied", "inst", inst, "dst", dst.IP.String(), "id", echo.ID, "seq", echo.Seq, "iface", l.iface.Name, "src", l.src4)
			}
		} else if l.log != nil {
			l.log.Debug("uping/recv: write failed", "err", err, "iface", l.iface.Name, "src", l.src4)
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
