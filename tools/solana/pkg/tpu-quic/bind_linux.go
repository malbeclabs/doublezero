//go:build linux

package tpuquic

import (
	"net"

	"golang.org/x/sys/unix"
)

func bindToDevice(c *net.UDPConn, iface string) error {
	rc, err := c.SyscallConn()
	if err != nil {
		return err
	}
	var serr error
	err = rc.Control(func(fd uintptr) {
		serr = unix.SetsockoptString(int(fd), unix.SOL_SOCKET, unix.SO_BINDTODEVICE, iface)
	})
	if err != nil {
		return err
	}
	return serr
}
