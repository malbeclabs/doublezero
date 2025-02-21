package netlink

import (
	"fmt"
	"net"
)

type EncapType string

const (
	GRE EncapType = "gre"

	TunnelName = "doublezero0"
)

type Tunnel struct {
	Name string
	EncapType
	LocalUnderlay  net.IP
	RemoteUnderlay net.IP
	LocalOverlay   net.IP
	RemoteOverlay  net.IP
}

func NewTunnel(local, remote net.IP, overlayNet string) (*Tunnel, error) {
	tun := &Tunnel{
		Name:           TunnelName,
		EncapType:      GRE,
		LocalUnderlay:  local,
		RemoteUnderlay: remote,
	}

	tunIp, tunNet, err := net.ParseCIDR(overlayNet)
	if err != nil {
		return nil, fmt.Errorf("tunnel: invalid tunnel network specified: %v", err)
	}
	if o, _ := tunNet.Mask.Size(); o != 31 {
		return nil, fmt.Errorf("tunnel: the tunnel network mask must be a /31")
	}
	tun.RemoteOverlay = tunIp.Mask(tunNet.Mask)
	tun.LocalOverlay = tunIp.Mask(tunNet.Mask)
	tun.LocalOverlay[len(tun.LocalOverlay)-1]++
	return tun, nil
}
