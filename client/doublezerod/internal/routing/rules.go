//go:build linux

package routing

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
)

var (
	DzTableSpecific = 100
	DzTableDefault  = 101
)

type IPRule struct {
	Priority int
	Table    int
	SrcNet   *net.IPNet
	DstNet   *net.IPNet
}

// NewIPRule creates a new linux IP rule. The priority and table value ranges must be within
// 100 - 200 for no good reason other than we can't insert rules just anywhere and stomp
// on other system rules.
func NewIPRule(priority, table int, srcnet, dstnet string) (*IPRule, error) {
	if priority < 100 || priority > 200 {
		return nil, fmt.Errorf("priority not in range of 100 to 200: %d", priority)
	}
	if table < 100 || table > 200 {
		return nil, fmt.Errorf("table number not in range of 100 to 200: %d", table)
	}
	_, src, err := net.ParseCIDR(srcnet)
	if err != nil {
		return nil, fmt.Errorf("error parsing source network: %v", err)
	}
	_, dst, err := net.ParseCIDR(dstnet)
	if err != nil {
		return nil, fmt.Errorf("error parsing destination network: %v", err)
	}
	return &IPRule{
		Priority: priority,
		Table:    table,
		SrcNet:   src,
		DstNet:   dst,
	}, nil
}

func (r *IPRule) String() string {
	return fmt.Sprintf("priority: %d, table: %d, src: %s, dst: %s", r.Priority, r.Table, r.SrcNet, r.DstNet)
}

func CreateIPRules(nl Netlinker, rules []*IPRule) error {
	for _, rule := range rules {
		slog.Info("tunnel: adding ip rule", "rule", rule)
		err := nl.RuleAdd(rule)
		if err != nil {
			if errors.Is(err, ErrRuleExists) {
				slog.Error("tunnel: rule already exists", "rule", rule)
			} else {
				return fmt.Errorf("error adding ip rule: %v", err)
			}
		}
	}
	return nil
}
