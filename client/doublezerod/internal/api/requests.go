package api

import (
	"encoding/json"
	"fmt"
	"net"
	"slices"
	"strings"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/bgp"
)

type UserType int

const (
	UserTypeUnknown UserType = iota
	UserTypeIBRL
	UserTypeIBRLWithAllocatedIP
	UserTypeEdgeFiltering
	UserTypeMulticast
)

type userTypes []UserType

var ValidUserTypes = userTypes{
	UserTypeIBRL,
	UserTypeIBRLWithAllocatedIP,
	UserTypeEdgeFiltering,
	UserTypeMulticast,
}

func (u UserType) String() string {
	return [...]string{
		"Unknown",
		"IBRL",
		"IBRLWithAllocatedIP",
		"EdgeFiltering",
		"Multicast",
	}[u]
}

func (u UserType) FromString(userType string) UserType {
	return map[string]UserType{
		"Unknown":             UserTypeUnknown,
		"IBRL":                UserTypeIBRL,
		"IBRLWithAllocatedIP": UserTypeIBRLWithAllocatedIP,
		"EdgeFiltering":       UserTypeEdgeFiltering,
		"Multicast":           UserTypeMulticast,
	}[userType]
}

func (u UserType) MarshalJSON() ([]byte, error) {
	return json.Marshal(u.String())
}

func (u *UserType) UnmarshalJSON(b []byte) error {
	var s string
	err := json.Unmarshal(b, &s)

	if err != nil {
		return err
	}
	*u = u.FromString(s)
	return nil
}

type RemoveRequest struct {
	UserType UserType `json:"user_type"`
}

func (r *RemoveRequest) Validate() error {
	if !slices.Contains(ValidUserTypes, r.UserType) {
		return fmt.Errorf("invalid user type: %s", r.UserType)
	}
	return nil
}

type ProvisionRequest struct {
	UserType           UserType     `json:"user_type"`
	TunnelSrc          net.IP       `json:"tunnel_src"`
	TunnelDst          net.IP       `json:"tunnel_dst"`
	TunnelNet          *net.IPNet   `json:"tunnel_net"`
	DoubleZeroIP       net.IP       `json:"doublezero_ip"`
	DoubleZeroPrefixes []*net.IPNet `json:"doublezero_prefixes"`
	MulticastSubGroups []net.IP     `json:"mcast_sub_groups"`
	MulticastPubGroups []net.IP     `json:"mcast_pub_groups"`
	BgpLocalAsn        uint32       `json:"bgp_local_asn"`
	BgpRemoteAsn       uint32       `json:"bgp_remote_asn"`
}

// Equal reports whether two ProvisionRequests describe the same desired state.
func (p *ProvisionRequest) Equal(other *ProvisionRequest) bool {
	if p == nil || other == nil {
		return p == other
	}
	if p.UserType != other.UserType {
		return false
	}
	if !p.TunnelSrc.Equal(other.TunnelSrc) || !p.TunnelDst.Equal(other.TunnelDst) {
		return false
	}
	if !ipNetsEqual(p.TunnelNet, other.TunnelNet) {
		return false
	}
	if !p.DoubleZeroIP.Equal(other.DoubleZeroIP) {
		return false
	}
	if p.BgpLocalAsn != other.BgpLocalAsn || p.BgpRemoteAsn != other.BgpRemoteAsn {
		return false
	}
	if !ipSlicesEqual(p.MulticastPubGroups, other.MulticastPubGroups) {
		return false
	}
	if !ipSlicesEqual(p.MulticastSubGroups, other.MulticastSubGroups) {
		return false
	}
	return ipNetSlicesEqual(p.DoubleZeroPrefixes, other.DoubleZeroPrefixes)
}

// Diff returns a human-readable summary of fields that differ between two
// ProvisionRequests. Returns an empty string if they are equal.
func (p *ProvisionRequest) Diff(other *ProvisionRequest) string {
	if p == nil && other == nil {
		return ""
	}
	if p == nil {
		return "current is nil"
	}
	if other == nil {
		return "new is nil"
	}

	var diffs []string
	if p.UserType != other.UserType {
		diffs = append(diffs, fmt.Sprintf("UserType: %s -> %s", p.UserType, other.UserType))
	}
	if !p.TunnelSrc.Equal(other.TunnelSrc) {
		diffs = append(diffs, fmt.Sprintf("TunnelSrc: %s -> %s", p.TunnelSrc, other.TunnelSrc))
	}
	if !p.TunnelDst.Equal(other.TunnelDst) {
		diffs = append(diffs, fmt.Sprintf("TunnelDst: %s -> %s", p.TunnelDst, other.TunnelDst))
	}
	if !ipNetsEqual(p.TunnelNet, other.TunnelNet) {
		diffs = append(diffs, fmt.Sprintf("TunnelNet: %s -> %s", p.TunnelNet, other.TunnelNet))
	}
	if !p.DoubleZeroIP.Equal(other.DoubleZeroIP) {
		diffs = append(diffs, fmt.Sprintf("DoubleZeroIP: %s -> %s", p.DoubleZeroIP, other.DoubleZeroIP))
	}
	if p.BgpLocalAsn != other.BgpLocalAsn {
		diffs = append(diffs, fmt.Sprintf("BgpLocalAsn: %d -> %d", p.BgpLocalAsn, other.BgpLocalAsn))
	}
	if p.BgpRemoteAsn != other.BgpRemoteAsn {
		diffs = append(diffs, fmt.Sprintf("BgpRemoteAsn: %d -> %d", p.BgpRemoteAsn, other.BgpRemoteAsn))
	}
	if !ipSlicesEqual(p.MulticastPubGroups, other.MulticastPubGroups) {
		diffs = append(diffs, fmt.Sprintf("MulticastPubGroups: %v -> %v", p.MulticastPubGroups, other.MulticastPubGroups))
	}
	if !ipSlicesEqual(p.MulticastSubGroups, other.MulticastSubGroups) {
		diffs = append(diffs, fmt.Sprintf("MulticastSubGroups: %v -> %v", p.MulticastSubGroups, other.MulticastSubGroups))
	}
	if !ipNetSlicesEqual(p.DoubleZeroPrefixes, other.DoubleZeroPrefixes) {
		diffs = append(diffs, fmt.Sprintf("DoubleZeroPrefixes: count %d -> %d", len(p.DoubleZeroPrefixes), len(other.DoubleZeroPrefixes)))
	}

	if len(diffs) == 0 {
		return ""
	}
	return fmt.Sprintf("[%s]", strings.Join(diffs, ", "))
}

func ipNetsEqual(a, b *net.IPNet) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.IP.Equal(b.IP) && fmt.Sprint(a.Mask) == fmt.Sprint(b.Mask)
}

func ipSlicesEqual(a, b []net.IP) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !a[i].Equal(b[i]) {
			return false
		}
	}
	return true
}

func ipNetSlicesEqual(a, b []*net.IPNet) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !ipNetsEqual(a[i], b[i]) {
			return false
		}
	}
	return true
}

func (p *ProvisionRequest) Validate() error {
	// TODO: tunneldst cannot be nil
	// TODO: tunnelnet cannot be nil
	// TODO: doublezeroip cannot be nil
	// TODO: doublezeroprefixes cannot be nil
	if p.BgpLocalAsn == 0 {
		p.BgpLocalAsn = 65000
	}
	if p.BgpRemoteAsn == 0 {
		p.BgpRemoteAsn = 65001
	}
	return nil
}

func (p *ProvisionRequest) UnmarshalJSON(data []byte) error {
	type Alias ProvisionRequest
	alias := &struct {
		TunnelNet          string   `json:"tunnel_net"`
		DoubleZeroPrefixes []string `json:"doublezero_prefixes"`
		*Alias
	}{
		Alias: (*Alias)(p),
	}
	if err := json.Unmarshal(data, &alias); err != nil {
		return err
	}
	_, nn, err := net.ParseCIDR(alias.TunnelNet)
	if err != nil {
		return fmt.Errorf("error parsing cider address %s: %v", alias.TunnelNet, err)
	}
	p.TunnelNet = nn
	for _, prefix := range alias.DoubleZeroPrefixes {
		_, nn, err := net.ParseCIDR(prefix)
		if err != nil {
			return err
		}
		p.DoubleZeroPrefixes = append(p.DoubleZeroPrefixes, nn)
	}

	return nil
}

func (p *ProvisionRequest) MarshalJSON() ([]byte, error) {
	type Alias ProvisionRequest
	dzp := []string{}
	for _, prefix := range p.DoubleZeroPrefixes {
		dzp = append(dzp, prefix.String())
	}
	return json.Marshal(&struct {
		TunnelNet          string   `json:"tunnel_net"`
		DoubleZeroPrefixes []string `json:"doublezero_prefixes"`
		*Alias
	}{
		TunnelNet:          p.TunnelNet.String(),
		DoubleZeroPrefixes: dzp,
		Alias:              (*Alias)(p),
	})
}

type StatusRequest struct {
}

type StatusResponse struct {
	TunnelName       string      `json:"tunnel_name"`
	TunnelSrc        net.IP      `json:"tunnel_src"`
	TunnelDst        net.IP      `json:"tunnel_dst"`
	DoubleZeroIP     net.IP      `json:"doublezero_ip"`
	DoubleZeroStatus bgp.Session `json:"doublezero_status"`
	UserType         UserType    `json:"user_type"`
}
