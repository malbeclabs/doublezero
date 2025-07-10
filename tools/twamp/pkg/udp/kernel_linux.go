package udp

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

type KernelDialer struct{}

func NewKernelDialer() (*KernelDialer, error) {
	return &KernelDialer{}, nil
}

func (d *KernelDialer) Dial(ctx context.Context, ifaceName string, localAddr, remoteAddr *net.UDPAddr) (*net.UDPConn, error) {
	dialer := net.Dialer{
		LocalAddr: localAddr,
		Control: func(network, address string, c syscall.RawConn) error {
			var controlErr error
			err := c.Control(func(fd uintptr) {
				controlErr = syscall.SetsockoptString(int(fd), syscall.SOL_SOCKET, syscall.SO_BINDTODEVICE, ifaceName)
			})
			if err != nil {
				return fmt.Errorf("failed to set socket option: %w", err)
			}
			return controlErr
		},
	}

	conn, err := dialer.DialContext(ctx, "udp", remoteAddr.String())
	if err != nil {
		return nil, fmt.Errorf("failed to dial: %w", err)
	}

	return conn.(*net.UDPConn), nil
}

type KernelTimestampedReader struct {
	log  *slog.Logger
	conn *net.UDPConn
	fd   int
}

func NewKernelTimestampedReader(log *slog.Logger, conn *net.UDPConn) (*KernelTimestampedReader, error) {
	var sysfd int
	rawConn, err := conn.SyscallConn()
	if err != nil {
		return nil, err
	}
	err = rawConn.Control(func(fd uintptr) {
		sysfd = int(fd)
	})
	if err != nil {
		return nil, err
	}

	if err := unix.SetsockoptInt(sysfd, unix.SOL_SOCKET, unix.SO_TIMESTAMPNS, 1); err != nil {
		return nil, fmt.Errorf("failed to set SO_TIMESTAMPNS: %w", err)
	}

	return &KernelTimestampedReader{
		log:  log,
		conn: conn,
		fd:   sysfd,
	}, nil
}

func (c *KernelTimestampedReader) Now() time.Time {
	ts := unix.Timespec{}
	err := unix.ClockGettime(unix.CLOCK_REALTIME, &ts)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(ts.Sec, ts.Nsec)
}

func (c *KernelTimestampedReader) Read(ctx context.Context, buf []byte) (int, time.Time, error) {
	oob := make([]byte, 512)

	for {
		select {
		case <-ctx.Done():
			return 0, time.Time{}, ctx.Err()
		default:
		}

		n, oobn, _, _, err := unix.Recvmsg(c.fd, buf, oob, 0)
		if err != nil {
			if errno, ok := err.(syscall.Errno); ok && isTimeoutErr(errno) {
				// Sleep briefly to avoid busy loop
				time.Sleep(1 * time.Millisecond)
				continue
			}
			return 0, time.Time{}, fmt.Errorf("recvmsg failed: %w", err)
		}

		cmsgs, _ := syscall.ParseSocketControlMessage(oob[:oobn])
		for _, cmsg := range cmsgs {
			if cmsg.Header.Level == syscall.SOL_SOCKET && cmsg.Header.Type == syscall.SO_TIMESTAMPNS {
				if len(cmsg.Data) < int(unsafe.Sizeof(syscall.Timespec{})) {
					continue
				}
				ts := *(*syscall.Timespec)(unsafe.Pointer(&cmsg.Data[0]))
				return n, time.Unix(int64(ts.Sec), int64(ts.Nsec)), nil
			}
		}
		return 0, time.Time{}, fmt.Errorf("no timestamp in control message")
	}
}

func isTimeoutErr(errno syscall.Errno) bool {
	return errno == syscall.EAGAIN || errno == syscall.EWOULDBLOCK
}
