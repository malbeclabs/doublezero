package services

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"slices"
	"syscall"
	"time"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/api"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/bgp"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/pim"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
)

type PIMWriter interface {
	Start(conn pim.RawConner, iface string, tunnelAddr net.IP, group []net.IP) error
	Close() error
}

type HeartbeatWriter interface {
	Start(iface string, srcIP net.IP, groups []net.IP, ttl int, interval time.Duration) error
	Close() error
}

type BGPReaderWriter interface {
	AddPeer(*bgp.PeerConfig, []bgp.NLRI) error
	DeletePeer(net.IP) error
	GetPeerStatus(net.IP) bgp.Session
}

type ServiceType uint8

const (
	// ServiceTypeNone is used when no service type is set
	ServiceTypeUnknown ServiceType = iota
	// ServiceTypeUnicast denotes unicast tunnel services
	ServiceTypeUnicast
	// ServiceTypeUnicast denotes multicast tunnel services
	ServiceTypeMulticast
)

func IsUnicastUser(u api.UserType) bool {
	return slices.Contains([]api.UserType{
		api.UserTypeEdgeFiltering,
		api.UserTypeIBRL,
		api.UserTypeIBRLWithAllocatedIP,
	}, u)
}

func IsMulticastUser(u api.UserType) bool {
	return u == api.UserTypeMulticast
}

// createBaseTunnel creates a tunnel interface, adds overlay addressing and brings up the interface.
func createBaseTunnel(nl routing.Netlinker, tun *routing.Tunnel) error {
	if tun.LocalOverlay == nil {
		return fmt.Errorf("missing tunnel local overlay addressing")
	}

	err := nl.TunnelAdd(tun)
	if err != nil {
		if errors.Is(err, routing.ErrTunnelExists) {
			slog.Error("tunnel: tunnel already exists", "tunnel", tun.Name)
		} else {
			return fmt.Errorf("tunnel: could not add tunnel interface: %v", err)
		}
	}
	slog.Info("tunnel: adding address to tunnel interface", "address", tun.LocalOverlay)
	err = nl.TunnelAddrAdd(tun, tun.LocalOverlay.String()+"/31", syscall.RT_SCOPE_LINK)
	if err != nil {
		if errors.Is(err, routing.ErrAddressExists) {
			slog.Error("tunnel: address already present on tunnel")
		} else {
			return fmt.Errorf("error adding addressing to tunnel: %v", err)
		}
	}

	slog.Info("tunnel: bringing up tunnel interface")
	if err = nl.TunnelUp(tun); err != nil {
		return fmt.Errorf("tunnel: error bring up tunnel interface: %v", err)
	}
	return nil
}

func createTunnelWithIP(nl routing.Netlinker, tun *routing.Tunnel, dzIp net.IP) (err error) {
	if err := createBaseTunnel(nl, tun); err != nil {
		return fmt.Errorf("error creating base tunnel: %v", err)
	}

	slog.Info("tunnel: adding dz address to tunnel interface", "dz address", dzIp.String()+"/32")
	err = nl.TunnelAddrAdd(tun, dzIp.String()+"/32", syscall.RT_SCOPE_UNIVERSE)
	if err != nil {
		if errors.Is(err, routing.ErrAddressExists) {
			slog.Error("tunnel: address already present on tunnel")
		} else {
			return fmt.Errorf("error adding addressing to tunnel: %v", err)
		}
	}
	return nil
}
