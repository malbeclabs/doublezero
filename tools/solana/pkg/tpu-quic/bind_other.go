//go:build !linux && !darwin

package tpuquic

import (
	"fmt"
	"net"
	"runtime"
)

func bindToDevice(c *net.UDPConn, iface string) error {
	return fmt.Errorf("binding to device is not implemented for platform %s", runtime.GOOS)
}
