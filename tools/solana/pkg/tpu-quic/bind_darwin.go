//go:build darwin

package tpuquic

import (
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

func bindToDevice(c *net.UDPConn, ifaceName string) error {
	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return fmt.Errorf("lookup interface %q: %w", ifaceName, err)
	}
	if iface.Index == 0 {
		return fmt.Errorf("interface %q has index 0", ifaceName)
	}

	laddr := c.LocalAddr()
	ua, ok := laddr.(*net.UDPAddr)
	if !ok || ua.IP == nil {
		return fmt.Errorf("unexpected LocalAddr type %T", laddr)
	}

	isV4 := ua.IP.To4() != nil

	rc, err := c.SyscallConn()
	if err != nil {
		return err
	}

	var serr error
	err = rc.Control(func(fd uintptr) {
		if isV4 {
			serr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_BOUND_IF, iface.Index)
		} else {
			serr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IPV6, unix.IPV6_BOUND_IF, iface.Index)
		}
	})
	if err != nil {
		return err
	}
	return serr
}
