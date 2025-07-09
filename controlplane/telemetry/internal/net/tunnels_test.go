package net_test

import (
	"net"
	"testing"

	netutil "github.com/malbeclabs/doublezero/controlplane/telemetry/internal/net"
	"github.com/stretchr/testify/require"
)

func TestTelemetry_Net_FindLocalTunnel(t *testing.T) {
	t.Parallel()

	t.Run("success: .0 address should yield .1 peer", func(t *testing.T) {
		t.Parallel()

		tunnelNet := mustCIDR(t, "192.168.0.0/31")
		ifaces := []netutil.Interface{
			{
				Name: "eth0",
				Addrs: []net.Addr{
					mustAddr(t, "192.168.0.0/31"),
				},
			},
		}

		tun, err := netutil.FindLocalTunnel(ifaces, tunnelNet)
		require.NoError(t, err)
		require.Equal(t, "eth0", tun.Interface)
		require.Equal(t, net.IPv4(192, 168, 0, 0).To4(), tun.SourceIP)
		require.Equal(t, net.IPv4(192, 168, 0, 1).To4(), tun.TargetIP)
	})

	t.Run("success: .1 address should yield .0 peer", func(t *testing.T) {
		t.Parallel()

		tunnelNet := mustCIDR(t, "192.168.0.0/31")
		ifaces := []netutil.Interface{
			{
				Name: "eth1",
				Addrs: []net.Addr{
					mustAddr(t, "192.168.0.1/31"),
				},
			},
		}

		tun, err := netutil.FindLocalTunnel(ifaces, tunnelNet)
		require.NoError(t, err)
		require.Equal(t, "eth1", tun.Interface)
		require.Equal(t, net.IPv4(192, 168, 0, 1).To4(), tun.SourceIP)
		require.Equal(t, net.IPv4(192, 168, 0, 0).To4(), tun.TargetIP)
	})

	t.Run("no match for interface with non-/31 mask", func(t *testing.T) {
		t.Parallel()

		tunnelNet := mustCIDR(t, "192.168.0.0/31")
		ifaces := []netutil.Interface{
			{
				Name: "eth2",
				Addrs: []net.Addr{
					mustAddr(t, "192.168.0.5/30"),
				},
			},
		}
		_, err := netutil.FindLocalTunnel(ifaces, tunnelNet)
		require.Error(t, err)
	})

	t.Run("no match for interface outside tunnel net", func(t *testing.T) {
		t.Parallel()

		tunnelNet := mustCIDR(t, "192.168.1.0/31")
		ifaces := []netutil.Interface{
			{
				Name: "eth3",
				Addrs: []net.Addr{
					mustAddr(t, "192.168.0.0/31"),
				},
			},
		}
		_, err := netutil.FindLocalTunnel(ifaces, tunnelNet)
		require.Error(t, err)
	})

	t.Run("no match for IPv6 address", func(t *testing.T) {
		t.Parallel()

		tunnelNet := mustCIDR(t, "2001:db8::/127")
		ifaces := []netutil.Interface{
			{
				Name: "eth4",
				Addrs: []net.Addr{
					mustAddr(t, "2001:db8::/127"),
				},
			},
		}
		_, err := netutil.FindLocalTunnel(ifaces, tunnelNet)
		require.Error(t, err)
	})
}

func mustCIDR(t *testing.T, s string) *net.IPNet {
	t.Helper()
	_, n, err := net.ParseCIDR(s)
	require.NoError(t, err)
	return n
}

func mustAddr(t *testing.T, s string) net.Addr {
	t.Helper()
	ip, ipnet, err := net.ParseCIDR(s)
	require.NoError(t, err)
	ipnet.IP = ip
	return ipnet
}
