package geoip

import (
	"net"

	"github.com/malbeclabs/doublezero/tools/maxmind/pkg/geoip"
)

func MaybeResolveAddr(resolver geoip.Resolver, addr string) *geoip.Record {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return nil
	}
	return resolver.Resolve(ip)
}
