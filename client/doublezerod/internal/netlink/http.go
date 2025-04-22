package netlink

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"

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

type ProvisionRequest struct {
	UserType           UserType     `json:"user_type"`
	TunnelSrc          net.IP       `json:"tunnel_src"`
	TunnelDst          net.IP       `json:"tunnel_dst"`
	TunnelNet          *net.IPNet   `json:"tunnel_net"`
	DoubleZeroIP       net.IP       `json:"doublezero_ip"`
	DoubleZeroPrefixes []*net.IPNet `json:"doublezero_prefixes"`
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

/*
ServeProvision handles local provisioning of a double zero tunnel. The following is an example payload:

	`{
		"user_type": "IBRL"						[required]
		"tunnel_src": "1.1.1.1", 					[optional]
		"tunnel_dst": "2.2.2.2", 					[required]
		"tunnel_net": "10.1.1.0/31",					[required]
		"doublezero_ip": "10.0.0.0",					[required]
		"doublezero_prefixes": ["10.0.0.0/24"], 			[required]
		"bgp_local_asn": 65000,						[optional]
		"bgp_remote_asn": 65001						[optional]
	}`,
*/
func (n *NetlinkManager) ServeProvision(w http.ResponseWriter, r *http.Request) {
	var p ProvisionRequest
	err := json.NewDecoder(r.Body).Decode(&p)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"status": "error", "description": "malformed provision request: %v"}`, err)))
		return
	}

	if err = p.Validate(); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"status": "error", "description": "invalid request: %v"}`, err)))
		return
	}

	err = n.Provision(p)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"status": "error", "description": "malformed stuff: %v"}`, err)))
		return
	}

	_, _ = w.Write([]byte(`{"status": "ok"}`))
}

func (n *NetlinkManager) ServeRemove(w http.ResponseWriter, r *http.Request) {
	err := n.Remove()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"status": "error", "description": "error during tunnel removal: %v"}`, err)))
		return
	}

	_, _ = w.Write([]byte(`{"status": "ok"}`))
}

type StatusRequest struct {
}

type StatusResponse struct {
	TunnelName       string      `json:"tunnel_name"`
	TunnelSrc        net.IP      `json:"tunnel_src"`
	TunnelDst        net.IP      `json:"tunnel_dst"`
	DoubleZeroIP     net.IP      `json:"doublezero_ip"`
	DoubleZeroStatus bgp.Session `json:"doublezero_status"`
}

func (n *NetlinkManager) ServeStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	status, err := n.Status()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"status": "error", "description": "error while getting status: %v"}`, err)))
		return
	}
	if status == nil {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"doublezero_status": {"session_status": "disconnected"}}`))
		return
	}
	if err = json.NewEncoder(w).Encode(status); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"status": "error", "description": "error while encoding status: %v"}`, err)))
		return
	}
}
