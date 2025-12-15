package netutil

import (
	"fmt"
	"net"
)

func ResolveInterface(name string) (*net.Interface, string, error) {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return nil, "", fmt.Errorf("interface %s not found: %w", name, err)
	}

	addrs, err := iface.Addrs()
	if err != nil {
		return nil, "", fmt.Errorf("failed to list addrs for interface %s: %w", name, err)
	}

	var v6 string
	for _, a := range addrs {
		ipNet, ok := a.(*net.IPNet)
		if !ok || ipNet.IP == nil {
			continue
		}

		ip := ipNet.IP

		// skip loopback
		if ip.IsLoopback() {
			continue
		}

		// prefer IPv4
		if v4 := ip.To4(); v4 != nil {
			return iface, v4.String(), nil
		}

		// remember first non-loopback IPv6 as fallback
		if v6 == "" {
			v6 = ip.String()
		}
	}

	if v6 != "" {
		return iface, v6, nil
	}

	return nil, "", fmt.Errorf("interface %s: no non-loopback IPv4 or IPv6 addresses found", name)
}

func DefaultInterface() (*net.Interface, error) {
	// Pick any routable remote address; no packets are sent.
	conn, err := net.Dial("udp", "8.8.8.8:53")
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)

	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	for _, iface := range ifaces {
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			ipnet, _ := a.(*net.IPNet)
			if ipnet == nil {
				continue
			}
			if ipnet.IP.Equal(localAddr.IP) {
				return &iface, nil
			}
		}
	}

	return nil, fmt.Errorf("default interface not found")
}
