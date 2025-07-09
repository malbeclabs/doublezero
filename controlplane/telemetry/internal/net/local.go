package net

import (
	"errors"
	"log/slog"
	"net"
)

var (
	ErrNoMatchingInterface = errors.New("no matching /31 interface found")
)

type Interface struct {
	Name  string
	Addrs []net.Addr
}

type LocalNet interface {
	Interfaces() ([]Interface, error)
}

type localNet struct {
}

func NewLocalNet(log *slog.Logger) LocalNet {
	return &localNet{}
}

func (n *localNet) Interfaces() ([]Interface, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	interfaces := make([]Interface, 0, len(ifaces))
	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			return nil, err
		}
		interfaces = append(interfaces, Interface{Name: iface.Name, Addrs: addrs})
	}
	return interfaces, nil
}

type MockLocalNet struct {
	InterfacesFunc func() ([]Interface, error)
}

func (m *MockLocalNet) Interfaces() ([]Interface, error) {
	return m.InterfacesFunc()
}
