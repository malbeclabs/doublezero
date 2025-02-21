package bgp

import "fmt"

type NLRI struct {
	AsPath       []uint32
	NextHop      string
	Prefix       string
	PrefixLength uint8
}

func NewNLRI(aspath []uint32, nexthop, prefix string, prefixlen uint8) (NLRI, error) {
	if prefixlen > 32 {
		return NLRI{}, fmt.Errorf("invalid prefix length specified: %d", prefixlen)
	}
	return NLRI{
		AsPath:       aspath,
		NextHop:      nexthop,
		Prefix:       prefix,
		PrefixLength: prefixlen,
	}, nil
}
