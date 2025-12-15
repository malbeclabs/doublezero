//go:build !linux

package netlink

import (
	"errors"
)

func NewNetlinker() Netlinker {
	return &unimplementedNetlink{}
}

type unimplementedNetlink struct{}

func (d *unimplementedNetlink) GetBGPRoutesByDst() (map[string]Route, error) {
	return nil, errors.New("unimplemented")
}
