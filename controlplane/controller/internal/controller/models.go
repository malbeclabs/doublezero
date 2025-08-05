package controller

import (
	"net"

	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
)

var (
	// maximum number of tunnels to provision on a given device
	maxTunnelSlots     = 64
	startUserTunnelNum = 500
)

type Device struct {
	PubKey                string
	PublicIP              net.IP
	Vpn4vLoopbackIP       net.IP
	Ip4vLoopbackIP        net.IP
	Tunnels               []*Tunnel
	TunnelSlots           int
	Interfaces            []serviceability.Interface
	Vpn4vLoopbackIntfName string
	Ip4vLoopbackIntfName  string
}

func NewDevice(ip net.IP, publicKey string) *Device {
	tunnels := []*Tunnel{}
	for i := 0; i < maxTunnelSlots; i++ {
		id := startUserTunnelNum + i
		tunnel := &Tunnel{
			Id:        id,
			Allocated: false,
		}
		tunnels = append(tunnels, tunnel)
	}
	return &Device{
		PublicIP:    ip,
		PubKey:      publicKey,
		Tunnels:     tunnels,
		TunnelSlots: maxTunnelSlots,
	}
}

func (d *Device) findTunnel(id int) *Tunnel {
	for _, tunnel := range d.Tunnels {
		if tunnel.Id == id {
			return tunnel
		}
	}
	return nil
}

type Tunnel struct {
	Id                    int
	UnderlaySrcIP         net.IP
	UnderlayDstIP         net.IP
	OverlaySrcIP          net.IP // This needs to be derived based on the tunnel net
	OverlayDstIP          net.IP // This needs to be derived based on the tunnel net
	DzIp                  net.IP
	PubKey                string
	Allocated             bool
	IsMulticast           bool
	MulticastBoundaryList []net.IP
	MulticastSubscribers  []net.IP
	MulticastPublishers   []net.IP
}

type BgpPeer struct {
	PeerIP   net.IP
	PeerName string
}

type templateData struct {
	Device                   *Device
	Vpnv4BgpPeers            []BgpPeer
	Ipv4BgpPeers             []BgpPeer
	UnknownBgpPeers          []net.IP
	MulticastGroupBlock      string
	NoHardware               bool
	TelemetryTWAMPListenPort int
}
