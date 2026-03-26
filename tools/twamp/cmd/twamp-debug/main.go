//go:build linux

// twamp-debug is a diagnostic tool for checking kernel timestamping support
// and measuring TWAMP probe quality on Arista switches.
//
// It probes an existing TWAMP reflector and reports:
//   - SO_TIMESTAMPING capability (TX/RX software/hardware)
//   - SO_TIMESTAMPNS support (what the current sender uses)
//   - Comparison of userspace vs kernel vs hardware timestamps
//   - RTT statistics (min/max/mean/p50/p99/stddev/jitter)
//   - Per-probe breakdown showing all available timestamp sources
//   - ethtool timestamping capabilities for the socket's interface
package main

import (
	"context"
	"encoding/binary"
	"flag"
	"fmt"
	"math"
	"net"
	"os"
	"os/signal"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"

	twamplight "github.com/malbeclabs/doublezero/tools/twamp/pkg/light"
)

// SO_TIMESTAMPING flags — not all are in x/sys/unix.
const (
	SOF_TIMESTAMPING_TX_HARDWARE  = 1 << 0
	SOF_TIMESTAMPING_TX_SOFTWARE  = 1 << 1
	SOF_TIMESTAMPING_RX_HARDWARE  = 1 << 2
	SOF_TIMESTAMPING_RX_SOFTWARE  = 1 << 3
	SOF_TIMESTAMPING_SOFTWARE     = 1 << 4
	SOF_TIMESTAMPING_RAW_HARDWARE = 1 << 6
	SOF_TIMESTAMPING_OPT_CMSG     = 1 << 10
	SOF_TIMESTAMPING_OPT_TSONLY   = 1 << 11
	SOF_TIMESTAMPING_OPT_ID       = 1 << 14
	SOF_TIMESTAMPING_TX_SCHED     = 1 << 8

	// SCM_TIMESTAMPING is the cmsg type for SO_TIMESTAMPING.
	SCM_TIMESTAMPING = 37 // unix.SCM_TIMESTAMPING

	// SO_TIMESTAMPING socket option.
	SO_TIMESTAMPING = 37 // unix.SO_TIMESTAMPING
)

type probeResult struct {
	seq             uint32
	userspaceSend   time.Time
	userspaceRecv   time.Time
	kernelRecvNS    time.Time // SO_TIMESTAMPNS
	softwareRecv    time.Time // SO_TIMESTAMPING software
	hardwareRecv    time.Time // SO_TIMESTAMPING hardware
	hasKernelNS     bool
	hasSoftwareRecv bool
	hasHardwareRecv bool
	rttUserspace    time.Duration
	rttKernelNS     time.Duration
	rttSoftware     time.Duration
	rttHardware     time.Duration
}

func main() {
	remoteAddr := flag.String("remote-addr", "", "Remote reflector address (host:port)")
	localAddr := flag.String("local-addr", "", "Source address (host:port), optional")
	iface := flag.String("iface", "", "Bind to interface (SO_BINDTODEVICE), optional")
	count := flag.Int("count", 20, "Number of probes to send")
	interval := flag.Duration("interval", 100*time.Millisecond, "Interval between probes")
	timeout := flag.Duration("timeout", 2*time.Second, "Per-probe timeout")
	flag.Parse()

	if *remoteAddr == "" {
		fmt.Fprintf(os.Stderr, "Usage: twamp-debug -remote-addr host:port [-local-addr host:port] [-iface eth0] [-count 20] [-interval 100ms]\n")
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	remoteUDP := resolveAddr(*remoteAddr)
	var localUDP *net.UDPAddr
	if *localAddr != "" {
		localUDP = resolveAddr(*localAddr)
	}

	fmt.Println("=== TWAMP Timestamp Debug Tool ===")
	fmt.Printf("Target: %s\n", remoteUDP)
	if localUDP != nil {
		fmt.Printf("Source: %s\n", localUDP)
	}
	if *iface != "" {
		fmt.Printf("Interface: %s\n", *iface)
	}
	fmt.Println()

	// Phase 1: Check timestamp capabilities by sending real probes.
	fmt.Println("--- Timestamp Capabilities ---")
	checkCapabilities(*iface, localUDP, remoteUDP)
	fmt.Println()

	// Phase 2: Probe with all timestamp sources.
	fmt.Println("--- Probing ---")
	results := probeWithAllTimestamps(ctx, *iface, localUDP, remoteUDP, *count, *interval, *timeout)
	fmt.Println()

	if len(results) == 0 {
		fmt.Println("No successful probes.")
		os.Exit(1)
	}

	// Phase 4: Per-probe breakdown.
	fmt.Println("--- Per-Probe Timestamps ---")
	printPerProbe(results)
	fmt.Println()

	// Phase 5: Statistics.
	fmt.Println("--- RTT Statistics ---")
	printStats(results)
}

// checkCapabilities sends real probes to test each timestamp type and reports
// whether the kernel actually delivered the cmsg.
func checkCapabilities(iface string, local, remote *net.UDPAddr) {
	const testProbes = 3
	const testTimeout = 2 * time.Second

	type tsTest struct {
		name    string
		comment string
		// setupSockopt configures the socket before probing.
		setupSockopt func(fd int) error
		// checkCmsgs inspects the received cmsgs and returns true if the
		// timestamp of interest was delivered.
		checkCmsgs func(oob []byte) bool
	}

	tests := []tsTest{
		{
			name:    "SO_TIMESTAMPNS",
			comment: "kernel RX nanosecond timestamp (current method)",
			setupSockopt: func(fd int) error {
				return unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_TIMESTAMPNS, 1)
			},
			checkCmsgs: func(oob []byte) bool {
				cmsgs, err := syscall.ParseSocketControlMessage(oob)
				if err != nil {
					return false
				}
				for _, cmsg := range cmsgs {
					if cmsg.Header.Level == syscall.SOL_SOCKET && cmsg.Header.Type == syscall.SO_TIMESTAMPNS {
						return true
					}
				}
				return false
			},
		},
		{
			name:    "SO_TIMESTAMPING RX software",
			comment: "kernel software RX timestamp via SO_TIMESTAMPING",
			setupSockopt: func(fd int) error {
				return unix.SetsockoptInt(fd, unix.SOL_SOCKET, SO_TIMESTAMPING,
					SOF_TIMESTAMPING_RX_SOFTWARE|SOF_TIMESTAMPING_SOFTWARE|SOF_TIMESTAMPING_OPT_CMSG)
			},
			checkCmsgs: func(oob []byte) bool {
				cmsgs, err := syscall.ParseSocketControlMessage(oob)
				if err != nil {
					return false
				}
				tsSize := int(unsafe.Sizeof(syscall.Timespec{}))
				for _, cmsg := range cmsgs {
					if cmsg.Header.Level == syscall.SOL_SOCKET && cmsg.Header.Type == SCM_TIMESTAMPING && len(cmsg.Data) >= 3*tsSize {
						sw := readTimespec(cmsg.Data[0:tsSize])
						if sw.Sec != 0 || sw.Nsec != 0 {
							return true
						}
					}
				}
				return false
			},
		},
		{
			name:    "SO_TIMESTAMPING RX hardware",
			comment: "NIC hardware RX timestamp",
			setupSockopt: func(fd int) error {
				return unix.SetsockoptInt(fd, unix.SOL_SOCKET, SO_TIMESTAMPING,
					SOF_TIMESTAMPING_RX_HARDWARE|SOF_TIMESTAMPING_RAW_HARDWARE|SOF_TIMESTAMPING_OPT_CMSG)
			},
			checkCmsgs: func(oob []byte) bool {
				cmsgs, err := syscall.ParseSocketControlMessage(oob)
				if err != nil {
					return false
				}
				tsSize := int(unsafe.Sizeof(syscall.Timespec{}))
				for _, cmsg := range cmsgs {
					if cmsg.Header.Level == syscall.SOL_SOCKET && cmsg.Header.Type == SCM_TIMESTAMPING && len(cmsg.Data) >= 3*tsSize {
						hw := readTimespec(cmsg.Data[2*tsSize : 3*tsSize])
						if hw.Sec != 0 || hw.Nsec != 0 {
							return true
						}
					}
				}
				return false
			},
		},
		{
			name:    "SO_TIMESTAMPING TX sched",
			comment: "kernel TX timestamp at qdisc enqueue",
			setupSockopt: func(fd int) error {
				return unix.SetsockoptInt(fd, unix.SOL_SOCKET, SO_TIMESTAMPING,
					SOF_TIMESTAMPING_TX_SCHED|SOF_TIMESTAMPING_SOFTWARE|SOF_TIMESTAMPING_OPT_CMSG)
			},
			checkCmsgs: nil, // handled via TX path
		},
		{
			name:    "SO_TIMESTAMPING TX software",
			comment: "kernel software TX timestamp",
			setupSockopt: func(fd int) error {
				return unix.SetsockoptInt(fd, unix.SOL_SOCKET, SO_TIMESTAMPING,
					SOF_TIMESTAMPING_TX_SOFTWARE|SOF_TIMESTAMPING_SOFTWARE|SOF_TIMESTAMPING_OPT_CMSG)
			},
			checkCmsgs: func(oob []byte) bool {
				// TX timestamps come back on the error queue.
				oobBuf := make([]byte, 1024)
				buf := make([]byte, 1500)
				// Non-blocking peek at the error queue.
				_, oobn, _, _, err := unix.Recvmsg(-1, buf, oobBuf, unix.MSG_ERRQUEUE|unix.MSG_DONTWAIT)
				// This is a placeholder — the actual fd isn't available here.
				// We'll handle TX specially below.
				_ = oobn
				_ = err
				return false
			},
		},
		{
			name:    "SO_TIMESTAMPING TX hardware",
			comment: "NIC hardware TX timestamp",
			setupSockopt: func(fd int) error {
				return unix.SetsockoptInt(fd, unix.SOL_SOCKET, SO_TIMESTAMPING,
					SOF_TIMESTAMPING_TX_HARDWARE|SOF_TIMESTAMPING_RAW_HARDWARE|SOF_TIMESTAMPING_OPT_CMSG)
			},
			checkCmsgs: nil, // handled via TX path
		},
	}

	raddr := &unix.SockaddrInet4{Port: remote.Port}
	copy(raddr.Addr[:], remote.IP.To4())

	for _, test := range tests {
		isTX := strings.Contains(test.name, "TX")
		delivered := false
		sockoptErr := false

		for probe := 0; probe < testProbes && !delivered; probe++ {
			fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM|unix.SOCK_NONBLOCK, unix.IPPROTO_UDP)
			if err != nil {
				continue
			}

			if iface != "" {
				_ = unix.SetsockoptString(fd, unix.SOL_SOCKET, unix.SO_BINDTODEVICE, iface)
			}
			if local != nil {
				sa := &unix.SockaddrInet4{Port: 0} // ephemeral port per test
				copy(sa.Addr[:], local.IP.To4())
				_ = unix.Bind(fd, sa)
			}

			if err := test.setupSockopt(fd); err != nil {
				sockoptErr = true
				unix.Close(fd)
				break
			}

			epfd, err := unix.EpollCreate1(0)
			if err != nil {
				unix.Close(fd)
				continue
			}

			ev := &unix.EpollEvent{Events: unix.EPOLLIN, Fd: int32(fd)}
			_ = unix.EpollCtl(epfd, unix.EPOLL_CTL_ADD, fd, ev)

			// Send a probe.
			pkt := twamplight.NewPacket(uint32(probe + 1))
			buf := make([]byte, 1500)
			_ = pkt.Marshal(buf)
			_ = unix.Sendto(fd, buf[:twamplight.PacketSize], 0, raddr)

			if isTX {
				// TX timestamps arrive on the error queue after send.
				time.Sleep(10 * time.Millisecond)
				oobBuf := make([]byte, 1024)
				txBuf := make([]byte, 1500)
				_, oobn, _, _, err := unix.Recvmsg(fd, txBuf, oobBuf, unix.MSG_ERRQUEUE|unix.MSG_DONTWAIT)
				if err == nil && oobn > 0 {
					cmsgs, err := syscall.ParseSocketControlMessage(oobBuf[:oobn])
					if err == nil {
						tsSize := int(unsafe.Sizeof(syscall.Timespec{}))
						for _, cmsg := range cmsgs {
							if cmsg.Header.Level == syscall.SOL_SOCKET && cmsg.Header.Type == SCM_TIMESTAMPING && len(cmsg.Data) >= 3*tsSize {
								sw := readTimespec(cmsg.Data[0:tsSize])
								hw := readTimespec(cmsg.Data[2*tsSize : 3*tsSize])
								if sw.Sec != 0 || sw.Nsec != 0 || hw.Sec != 0 || hw.Nsec != 0 {
									delivered = true
								}
							}
						}
					}
				}
			} else {
				// RX timestamps: wait for the reflected packet.
				events := make([]unix.EpollEvent, 1)
				deadline := time.Now().Add(testTimeout)
				for time.Now().Before(deadline) {
					remaining := int(time.Until(deadline).Milliseconds())
					if remaining <= 0 {
						break
					}
					n, err := unix.EpollWait(epfd, events, remaining)
					if err != nil || n == 0 {
						break
					}
					oob := make([]byte, 1024)
					n, oobn, _, _, err := unix.Recvmsg(fd, buf, oob, 0)
					if err != nil {
						if err == syscall.EAGAIN {
							continue
						}
						break
					}
					if n != twamplight.PacketSize {
						continue
					}
					if _, err := twamplight.UnmarshalPacket(buf[:n]); err != nil {
						continue
					}
					if test.checkCmsgs(oob[:oobn]) {
						delivered = true
					}
					break
				}
			}

			unix.Close(epfd)
			unix.Close(fd)
		}

		status := "no"
		detail := ""
		if sockoptErr {
			status = "no"
			detail = " (setsockopt rejected)"
		} else if delivered {
			status = "yes"
			detail = fmt.Sprintf(" (verified with %d test probes)", testProbes)
		} else {
			detail = " (setsockopt accepted, but no timestamp delivered)"
		}
		fmt.Printf("  %-32s %s%s\n", test.name+":", status, detail)
		fmt.Printf("    %s\n", test.comment)
	}
}

func probeWithAllTimestamps(ctx context.Context, iface string, local, remote *net.UDPAddr, count int, interval, timeout time.Duration) []probeResult {
	// Create a raw socket with both SO_TIMESTAMPNS and SO_TIMESTAMPING.
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM|unix.SOCK_NONBLOCK, unix.IPPROTO_UDP)
	if err != nil {
		fmt.Printf("socket: %v\n", err)
		return nil
	}
	defer unix.Close(fd)

	if iface != "" {
		if err := unix.SetsockoptString(fd, unix.SOL_SOCKET, unix.SO_BINDTODEVICE, iface); err != nil {
			fmt.Printf("SO_BINDTODEVICE: %v\n", err)
			return nil
		}
	}

	if local != nil {
		sa := &unix.SockaddrInet4{Port: local.Port}
		copy(sa.Addr[:], local.IP.To4())
		if err := unix.Bind(fd, sa); err != nil {
			fmt.Printf("bind: %v\n", err)
			return nil
		}
	}

	epfd, err := unix.EpollCreate1(0)
	if err != nil {
		fmt.Printf("epoll_create1: %v\n", err)
		return nil
	}
	defer unix.Close(epfd)

	event := &unix.EpollEvent{Events: unix.EPOLLIN, Fd: int32(fd)}
	if err := unix.EpollCtl(epfd, unix.EPOLL_CTL_ADD, fd, event); err != nil {
		fmt.Printf("epoll_ctl: %v\n", err)
		return nil
	}

	// Enable SO_TIMESTAMPNS.
	if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_TIMESTAMPNS, 1); err != nil {
		fmt.Printf("SO_TIMESTAMPNS: %v (continuing without)\n", err)
	}

	// Enable SO_TIMESTAMPING for RX software + hardware.
	tsFlags := SOF_TIMESTAMPING_RX_SOFTWARE |
		SOF_TIMESTAMPING_RX_HARDWARE |
		SOF_TIMESTAMPING_SOFTWARE |
		SOF_TIMESTAMPING_RAW_HARDWARE |
		SOF_TIMESTAMPING_OPT_CMSG
	if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, SO_TIMESTAMPING, tsFlags); err != nil {
		fmt.Printf("SO_TIMESTAMPING: %v (continuing without)\n", err)
	}

	raddr := &unix.SockaddrInet4{Port: remote.Port}
	copy(raddr.Addr[:], remote.IP.To4())

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	buf := make([]byte, 1500)
	oob := make([]byte, 1024) // Larger OOB for multiple cmsg types.
	events := make([]unix.EpollEvent, 1)

	var results []probeResult
	var seq uint32

	for i := 0; i < count; i++ {
		select {
		case <-ctx.Done():
			return results
		default:
		}

		seq++
		pkt := twamplight.NewPacket(seq)
		if err := pkt.Marshal(buf); err != nil {
			fmt.Printf("  probe %d: marshal error: %v\n", seq, err)
			continue
		}

		sendTime := time.Now()
		if err := unix.Sendto(fd, buf[:twamplight.PacketSize], 0, raddr); err != nil {
			fmt.Printf("  probe %d: sendto error: %v\n", seq, err)
			continue
		}

		deadline := time.Now().Add(timeout)
		var result probeResult
		result.seq = seq
		result.userspaceSend = sendTime
		got := false

		for {
			remaining := int(time.Until(deadline).Milliseconds())
			if remaining <= 0 {
				break
			}

			n, err := unix.EpollWait(epfd, events, remaining)
			if err != nil && err != syscall.EINTR {
				break
			}
			if n == 0 {
				break
			}

			n, oobn, _, _, err := unix.Recvmsg(fd, buf, oob, 0)
			if err != nil {
				if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK {
					continue
				}
				break
			}
			result.userspaceRecv = time.Now()

			if n != twamplight.PacketSize {
				continue
			}
			recvPkt, err := twamplight.UnmarshalPacket(buf[:n])
			if err != nil || recvPkt.Seq != pkt.Seq || recvPkt.Sec != pkt.Sec || recvPkt.Frac != pkt.Frac {
				continue
			}

			// Parse all control messages.
			parseCmsgs(oob[:oobn], &result)

			result.rttUserspace = result.userspaceRecv.Sub(sendTime)
			if result.hasKernelNS {
				result.rttKernelNS = result.kernelRecvNS.Sub(sendTime)
				if result.rttKernelNS < 0 {
					result.rttKernelNS = 0
				}
			}
			if result.hasSoftwareRecv {
				result.rttSoftware = result.softwareRecv.Sub(sendTime)
				if result.rttSoftware < 0 {
					result.rttSoftware = 0
				}
			}
			if result.hasHardwareRecv {
				result.rttHardware = result.hardwareRecv.Sub(sendTime)
				if result.rttHardware < 0 {
					result.rttHardware = 0
				}
			}

			got = true
			break
		}

		if got {
			results = append(results, result)
			fmt.Printf("  probe %d: userspace=%v", seq, result.rttUserspace.Round(time.Microsecond))
			if result.hasKernelNS {
				fmt.Printf("  kernel_ns=%v", result.rttKernelNS.Round(time.Microsecond))
			}
			if result.hasSoftwareRecv {
				fmt.Printf("  sw=%v", result.rttSoftware.Round(time.Microsecond))
			}
			if result.hasHardwareRecv {
				fmt.Printf("  hw=%v", result.rttHardware.Round(time.Microsecond))
			}
			fmt.Println()
		} else {
			fmt.Printf("  probe %d: timeout\n", seq)
		}

		if i < count-1 {
			select {
			case <-ctx.Done():
				return results
			case <-time.After(interval):
			}
		}
	}

	return results
}

func parseCmsgs(oob []byte, result *probeResult) {
	cmsgs, err := syscall.ParseSocketControlMessage(oob)
	if err != nil {
		return
	}

	for _, cmsg := range cmsgs {
		switch {
		// SO_TIMESTAMPNS — single timespec.
		case cmsg.Header.Level == syscall.SOL_SOCKET && cmsg.Header.Type == syscall.SO_TIMESTAMPNS:
			if len(cmsg.Data) >= int(unsafe.Sizeof(syscall.Timespec{})) {
				ts := *(*syscall.Timespec)(unsafe.Pointer(&cmsg.Data[0]))
				result.kernelRecvNS = time.Unix(int64(ts.Sec), int64(ts.Nsec))
				result.hasKernelNS = true
			}

		// SCM_TIMESTAMPING — three timespecs: [software, deprecated, hardware].
		case cmsg.Header.Level == syscall.SOL_SOCKET && cmsg.Header.Type == SCM_TIMESTAMPING:
			tsSize := int(unsafe.Sizeof(syscall.Timespec{}))
			if len(cmsg.Data) >= 3*tsSize {
				// Software timestamp (index 0).
				sw := readTimespec(cmsg.Data[0:tsSize])
				if sw.Sec != 0 || sw.Nsec != 0 {
					result.softwareRecv = time.Unix(int64(sw.Sec), int64(sw.Nsec))
					result.hasSoftwareRecv = true
				}
				// Hardware timestamp (index 2).
				hw := readTimespec(cmsg.Data[2*tsSize : 3*tsSize])
				if hw.Sec != 0 || hw.Nsec != 0 {
					result.hardwareRecv = time.Unix(int64(hw.Sec), int64(hw.Nsec))
					result.hasHardwareRecv = true
				}
			}
		}
	}
}

func readTimespec(data []byte) syscall.Timespec {
	var ts syscall.Timespec
	ts.Sec = int64(binary.LittleEndian.Uint64(data[0:8]))
	ts.Nsec = int64(binary.LittleEndian.Uint64(data[8:16]))
	return ts
}

func printPerProbe(results []probeResult) {
	// Show timestamp deltas between sources.
	hasKernel := false
	hasSW := false
	hasHW := false
	for _, r := range results {
		if r.hasKernelNS {
			hasKernel = true
		}
		if r.hasSoftwareRecv {
			hasSW = true
		}
		if r.hasHardwareRecv {
			hasHW = true
		}
	}

	// Header.
	fmt.Printf("  %-6s %-14s", "seq", "userspace")
	if hasKernel {
		fmt.Printf(" %-14s %-14s", "kernel_ns", "kern-user")
	}
	if hasSW {
		fmt.Printf(" %-14s %-14s", "sw_ts", "sw-user")
	}
	if hasHW {
		fmt.Printf(" %-14s %-14s", "hw_ts", "hw-user")
	}
	fmt.Println()

	for _, r := range results {
		fmt.Printf("  %-6d %-14s", r.seq, r.rttUserspace.Round(time.Microsecond))
		if hasKernel {
			if r.hasKernelNS {
				delta := r.rttKernelNS - r.rttUserspace
				fmt.Printf(" %-14s %-14s", r.rttKernelNS.Round(time.Microsecond), delta.Round(time.Microsecond))
			} else {
				fmt.Printf(" %-14s %-14s", "-", "-")
			}
		}
		if hasSW {
			if r.hasSoftwareRecv {
				delta := r.rttSoftware - r.rttUserspace
				fmt.Printf(" %-14s %-14s", r.rttSoftware.Round(time.Microsecond), delta.Round(time.Microsecond))
			} else {
				fmt.Printf(" %-14s %-14s", "-", "-")
			}
		}
		if hasHW {
			if r.hasHardwareRecv {
				delta := r.rttHardware - r.rttUserspace
				fmt.Printf(" %-14s %-14s", r.rttHardware.Round(time.Microsecond), delta.Round(time.Microsecond))
			} else {
				fmt.Printf(" %-14s %-14s", "-", "-")
			}
		}
		fmt.Println()
	}
}

func printStats(results []probeResult) {
	type source struct {
		name string
		rtts []float64 // nanoseconds
	}

	sources := []source{{name: "userspace"}}
	for _, r := range results {
		sources[0].rtts = append(sources[0].rtts, float64(r.rttUserspace.Nanoseconds()))
	}

	if results[0].hasKernelNS {
		s := source{name: "kernel_ns (SO_TIMESTAMPNS)"}
		for _, r := range results {
			if r.hasKernelNS {
				s.rtts = append(s.rtts, float64(r.rttKernelNS.Nanoseconds()))
			}
		}
		sources = append(sources, s)
	}
	if results[0].hasSoftwareRecv {
		s := source{name: "software (SO_TIMESTAMPING)"}
		for _, r := range results {
			if r.hasSoftwareRecv {
				s.rtts = append(s.rtts, float64(r.rttSoftware.Nanoseconds()))
			}
		}
		sources = append(sources, s)
	}
	if results[0].hasHardwareRecv {
		s := source{name: "hardware (SO_TIMESTAMPING)"}
		for _, r := range results {
			if r.hasHardwareRecv {
				s.rtts = append(s.rtts, float64(r.rttHardware.Nanoseconds()))
			}
		}
		sources = append(sources, s)
	}

	for _, s := range sources {
		if len(s.rtts) == 0 {
			continue
		}

		sorted := make([]float64, len(s.rtts))
		copy(sorted, s.rtts)
		sort.Float64s(sorted)

		n := len(sorted)
		minV := sorted[0]
		maxV := sorted[n-1]
		p50 := percentile(sorted, 50)
		p99 := percentile(sorted, 99)

		sum := 0.0
		for _, v := range sorted {
			sum += v
		}
		mean := sum / float64(n)

		variance := 0.0
		for _, v := range sorted {
			d := v - mean
			variance += d * d
		}
		stddev := math.Sqrt(variance / float64(n))

		// Jitter = mean absolute difference between consecutive samples.
		jitterSum := 0.0
		for i := 1; i < len(s.rtts); i++ {
			jitterSum += math.Abs(s.rtts[i] - s.rtts[i-1])
		}
		jitter := jitterSum / float64(len(s.rtts)-1)

		fmt.Printf("  %s (%d samples):\n", s.name, n)
		fmt.Printf("    min=%s  max=%s  mean=%s\n",
			fmtNs(minV), fmtNs(maxV), fmtNs(mean))
		fmt.Printf("    p50=%s  p99=%s\n",
			fmtNs(p50), fmtNs(p99))
		fmt.Printf("    stddev=%s  jitter=%s\n",
			fmtNs(stddev), fmtNs(jitter))
		fmt.Println()
	}

	// Loss rate.
	fmt.Printf("  Probes sent: target %d, received: %d, loss: %.1f%%\n",
		results[len(results)-1].seq, len(results),
		100.0*(1.0-float64(len(results))/float64(results[len(results)-1].seq)))
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := p / 100.0 * float64(len(sorted)-1)
	lower := int(idx)
	upper := lower + 1
	if upper >= len(sorted) {
		return sorted[len(sorted)-1]
	}
	frac := idx - float64(lower)
	return sorted[lower]*(1-frac) + sorted[upper]*frac
}

func fmtNs(ns float64) string {
	d := time.Duration(ns)
	return d.Round(time.Microsecond).String()
}

func resolveAddr(addr string) *net.UDPAddr {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid address %s: %v\n", addr, err)
		os.Exit(1)
	}
	port, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid port %s: %v\n", portStr, err)
		os.Exit(1)
	}
	ips, err := net.LookupIP(host)
	if err != nil || len(ips) == 0 {
		fmt.Fprintf(os.Stderr, "Error: failed to resolve %s: %v\n", host, err)
		os.Exit(1)
	}
	return &net.UDPAddr{IP: ips[0], Port: int(port)}
}
