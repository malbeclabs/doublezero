package routing

import "errors"

var (
	ErrTunnelExists  = errors.New("tunnel already exists")
	ErrAddressExists = errors.New("address already exists")
	ErrRuleExists    = errors.New("ip rule already exists")
	ErrRouteNotFound = errors.New("route not found")
)
