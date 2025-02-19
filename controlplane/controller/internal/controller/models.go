package controller

import "net"

var (
	// maximum number of tunnels to provision on a given device
	maxTunnelSlots     = 20
	startUserTunnelNum = 500
)

type Device struct {
	PubKey          string
	PublicIP        net.IP
	Tunnels         []*Tunnel
	TunnelSlots     int
	UnknownBgpPeers []net.IP
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
		PublicIP:        ip,
		PubKey:          publicKey,
		Tunnels:         tunnels,
		TunnelSlots:     maxTunnelSlots,
		UnknownBgpPeers: []net.IP{},
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
	Id            int
	UnderlaySrcIP net.IP
	UnderlayDstIP net.IP
	OverlaySrcIP  net.IP // This needs to be derived based on the tunnel net
	OverlayDstIP  net.IP // This needs to be derived based on the tunnel net
	DzIp          net.IP
	PubKey        string
	Allocated     bool
}
