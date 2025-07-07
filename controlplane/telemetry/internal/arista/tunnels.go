package arista

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"

	aristapb "github.com/malbeclabs/doublezero/controlplane/proto/arista/gen/pb-go/arista/EosSdkRpc"
)

const (
	IPInterfaceInterfaceStatusConnected = "connected"
	IPInterfaceLineProtocolStatusUp     = "up"
)

type IPInterfacesBriefResponse struct {
	Interfaces map[string]IPInterfaceBrief `json:"interfaces"`
}

type IPInterfaceBrief struct {
	Name               string             `json:"name"`
	InterfaceStatus    string             `json:"interfaceStatus"`
	LineProtocolStatus string             `json:"lineProtocolStatus"`
	InterfaceAddress   IPInterfaceAddress `json:"interfaceAddress"`
}

type IPInterfaceAddress struct {
	IPAddr IPInterfaceAddressIPAddr `json:"ipAddr"`
}

type IPInterfaceAddressIPAddr struct {
	Address string `json:"address"`
	MaskLen uint8  `json:"maskLen"`
}

// GetLocalTunnelTargetIPs returns the local tunnel target IPs from the Arista EAPI client.
//
// Example "show ip interface brief" JSON response from the Arista EAPI client:
//
//	{
//	  "interfaces": {
//	    "Ethernet1": {
//	      "name": "Ethernet1",
//	      "interfaceStatus": "connected",
//	      "lineProtocolStatus": "up",
//	      "mtu": 1500,
//	      "ipv4Routable240": false,
//	      "ipv4Routable0": false,
//	      "interfaceAddress": {
//	        "ipAddr": {
//	          "address": "10.157.67.16",
//	          "maskLen": 24
//	        }
//	      },
//	      "nonRoutableClassEIntf": false
//	    },
//	    "Loopback0": {
//	      "name": "Loopback0",
//	      "interfaceStatus": "connected",
//	      "lineProtocolStatus": "up",
//	      "mtu": 65535,
//	      "ipv4Routable240": false,
//	      "ipv4Routable0": false,
//	      "interfaceAddress": {
//	        "ipAddr": {
//	          "address": "8.8.8.8",
//	          "maskLen": 32
//	        }
//	      },
//	      "nonRoutableClassEIntf": false
//	    },
//	    "Management0": {
//	      "name": "Management0",
//	      "interfaceStatus": "connected",
//	      "lineProtocolStatus": "up",
//	      "mtu": 1500,
//	      "ipv4Routable240": false,
//	      "ipv4Routable0": false,
//	      "interfaceAddress": {
//	        "ipAddr": {
//	          "address": "172.27.0.7",
//	          "maskLen": 16
//	        }
//	      },
//	      "nonRoutableClassEIntf": false
//	    },
//	    "Tunnel1": {
//	      "name": "Tunnel1",
//	      "interfaceStatus": "connected",
//	      "lineProtocolStatus": "up",
//	      "mtu": 1476,
//	      "ipv4Routable240": false,
//	      "ipv4Routable0": false,
//	      "interfaceAddress": {
//	        "ipAddr": {
//	          "address": "172.16.0.1",
//	          "maskLen": 31
//	        }
//	      },
//	      "nonRoutableClassEIntf": false
//	    }
//	  }
//	}
func GetLocalTunnelTargetIPs(ctx context.Context, log *slog.Logger, client aristapb.EapiMgrServiceClient) ([]net.IP, error) {
	response, err := client.RunShowCmd(ctx, &aristapb.RunShowCmdRequest{
		Command: "show ip interface brief",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get local tunnel ips: %w", err)
	}

	if response.Response == nil {
		return nil, fmt.Errorf("no response from arista eapi")
	}

	if !response.Response.Success {
		return nil, fmt.Errorf("error from arista eapi: code=%d, message=%s", response.Response.ErrorCode, response.Response.ErrorMessage)
	}

	ip4s := make([]net.IP, 0)
	for _, respJSON := range response.Response.Responses {
		var resp IPInterfacesBriefResponse
		err := json.Unmarshal([]byte(respJSON), &resp)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal response: %w", err)
		}
		for _, iface := range resp.Interfaces {
			if iface.InterfaceStatus != IPInterfaceInterfaceStatusConnected || iface.LineProtocolStatus != IPInterfaceLineProtocolStatusUp {
				continue
			}
			if iface.InterfaceAddress.IPAddr.MaskLen != 31 {
				continue
			}
			ip4 := net.ParseIP(iface.InterfaceAddress.IPAddr.Address).To4()
			if ip4 != nil {
				peerIP, err := getPeerIPIn31(iface.InterfaceAddress.IPAddr.Address)
				if err != nil {
					log.Warn("Failed to get peer ip in /31", "error", err, "ip", iface.InterfaceAddress.IPAddr.Address, "name", iface.Name, "maskLen", iface.InterfaceAddress.IPAddr.MaskLen)
					continue
				}
				ip4s = append(ip4s, peerIP.To4())
			}
		}
	}

	return ip4s, nil
}

func getPeerIPIn31(ipStr string) (net.IP, error) {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return nil, fmt.Errorf("invalid IP: %s", ipStr)
	}
	ip = ip.To4()
	if ip == nil {
		return nil, fmt.Errorf("not an IPv4 address: %s", ipStr)
	}
	ipInt := uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3])
	peerInt := ipInt ^ 1
	peerIP := net.IPv4(byte(peerInt>>24), byte(peerInt>>16), byte(peerInt>>8), byte(peerInt))
	return peerIP, nil
}
