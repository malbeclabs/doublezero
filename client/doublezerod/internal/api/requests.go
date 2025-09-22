package api

import (
	"encoding/json"
	"fmt"
	"net"
	"slices"

	"github.com/gagliardetto/solana-go"
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
	ProgramID solana.PublicKey `json:"program_id"`
	UserType  UserType         `json:"user_type"`
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
	ProgramID solana.PublicKey `json:"program_id"`
	Results   []*ServiceStatus `json:"results"`
}

type ServiceStatus struct {
	TunnelName       string      `json:"tunnel_name"`
	TunnelSrc        net.IP      `json:"tunnel_src"`
	TunnelDst        net.IP      `json:"tunnel_dst"`
	DoubleZeroIP     net.IP      `json:"doublezero_ip"`
	DoubleZeroStatus bgp.Session `json:"doublezero_status"`
	UserType         UserType    `json:"user_type"`
}

type SetConfigRequest struct {
	LedgerRPCURL string `json:"ledger_rpc_url"`
	ProgramID    string `json:"program_id"`
}

func (r *SetConfigRequest) Validate() error {
	if r.LedgerRPCURL == "" {
		return fmt.Errorf("ledger_rpc_url is required")
	}
	if r.ProgramID == "" {
		return fmt.Errorf("program_id is required")
	}
	return nil
}
