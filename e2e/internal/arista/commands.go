package arista

type ShowIPBGPSummary struct {
	VRFs map[string]VRF
}

type VRF struct {
	RouterID string                        `json:"routerId"`
	Peers    map[string]BGPNeighborSummary `json:"peers"`
	VRF      string                        `json:"vrf"`
	ASN      string                        `json:"asn"`
}

type BGPNeighborSummary struct {
	MsgSent             int     `json:"msgSent"`
	InMsgQueue          int     `json:"inMsgQueue"`
	PrefixReceived      int     `json:"prefixReceived"`
	UpDownTime          float64 `json:"upDownTime"`
	Version             int     `json:"version"`
	MsgReceived         int     `json:"msgReceived"`
	PrefixAccepted      int     `json:"prefixAccepted"`
	PeerState           string  `json:"peerState"`
	PeerStateIdleReason string  `json:"peerStateIdleReason,omitempty"`
	OutMsgQueue         int     `json:"outMsgQueue"`
	UnderMaintenance    bool    `json:"underMaintenance"`
	ASN                 string  `json:"asn"`
}

func ShowIPBGPSummaryCmd(vrf string) string {
	if vrf == "" {
		return "show ip bgp summary"
	}
	return "show ip bgp summary vrf " + vrf
}

type ShowIpRoute struct {
	VRFs map[string]ShowIpRouteVRF
}

type ShowIpRouteVRF struct {
	Routes map[string]IpRoute `json:"routes"`
}

type IpRoute struct {
	RouteType string `json:"routeType"`
}

func ShowIpRouteCmd(vrf string) string {
	return "show ip route vrf " + vrf
}

// show ip mroute sparse-mode
//
// "groups": {
//     "233.84.178.0": {
//         "groupSources": {
//             "0.0.0.0": {
//                 "sourceAddress": "0.0.0.0",
//                 "creationTime": 1748646912.0,
//                 "routeFlags": "W",
//                 "rp": "10.0.0.0",
//                 "rpfInterface": "Null0",
//                 "oifList": [
//                     "Tunnel500"
//                 ]
//             }
//         }
//     }
// }

// ShowIPMroute represents the top-level structure containing multicast routing information.
type ShowIPMroute struct {
	Groups map[string]GroupDetail `json:"groups"`
}

type GroupDetail struct {
	GroupSources map[string]SourceDetail `json:"groupSources"`
}

type SourceDetail struct {
	SourceAddress string   `json:"sourceAddress"`
	CreationTime  float64  `json:"creationTime"`
	RouteFlags    string   `json:"routeFlags"`
	RP            string   `json:"rp"`
	RPFInterface  string   `json:"rpfInterface"`
	OIFList       []string `json:"oifList"`
}

func ShowIPMrouteCmd() string {
	return "show ip mroute sparse-mode"
}

// show ip pim neighbor
//
// {
//   "neighbors": {
//     "169.254.0.1": {
//       "address": "169.254.0.1",
//       "intf": "Tunnel500",
//       "creationTime": 1748812548.569936,
//       "lastRefreshTime": 1748812548.5713866,
//       "holdTime": 105,
//       "mode": {
//         "mode": "Sparse",
//         "borderRouter": false
//       },
//       "bfdState": "disabled",
//       "transport": "datagram",
//       "detail": false,
//       "secondaryAddress": [],
//       "maintenanceReceived": null,
//       "maintenanceSent": null
//     }
//   }
// }

// ShowPIMNeighbors represents the top-level structure containing a map of PIM neighbor details.
type ShowPIMNeighbors struct {
	Neighbors map[string]PIMNeighborDetail `json:"neighbors"`
}

// PIMNeighborDetail holds the specific information for a PIM neighbor.
type PIMNeighborDetail struct {
	Address             string          `json:"address"`
	Interface           string          `json:"intf"`
	CreationTime        float64         `json:"creationTime"`
	LastRefreshTime     float64         `json:"lastRefreshTime"`
	HoldTime            int             `json:"holdTime"`
	Mode                PIMNeighborMode `json:"mode"`
	BFDState            string          `json:"bfdState"`
	Transport           string          `json:"transport"`
	Detail              bool            `json:"detail"`
	SecondaryAddresses  []string        `json:"secondaryAddress"`
	MaintenanceReceived string          `json:"maintenanceReceived"`
	MaintenanceSent     string          `json:"maintenanceSent"`
}

// PIMNeighborMode describes the operational mode of the PIM neighbor.
type PIMNeighborMode struct {
	Mode string `json:"mode"`
}

func ShowPIMNeighborsCmd() string {
	return "show ip pim neighbor"
}

// show interfaces <name>
//
// {
//   "interfaces": {
//     "Tunnel500": {
//       "name": "Tunnel500",
//       "lineProtocolStatus": "up",
//       "interfaceStatus": "connected",
//       ...
//     }
//   }
// }

type ShowInterfaces struct {
	Interfaces map[string]InterfaceDetail `json:"interfaces"`
}

type InterfaceDetail struct {
	Name               string `json:"name"`
	LineProtocolStatus string `json:"lineProtocolStatus"`
	InterfaceStatus    string `json:"interfaceStatus"`
}

func ShowInterfacesCmd(name string) string {
	return "show interfaces " + name
}
