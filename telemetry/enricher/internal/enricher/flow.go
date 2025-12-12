package enricher

import (
	"net"
	"time"
)

/*
	{
	    "type": "SFLOW_5",
	    "time_received_ns": 1740523285033865700,
	    "sequence_num": 3421,
	    "sampling_rate": 1024,
	    "sampler_address": "204.16.241.241",
	    "time_flow_start_ns": 1740523285033865700,
	    "time_flow_end_ns": 1740523285033865700,
	    "bytes": 113,
	    "packets": 1,
	    "src_addr": "64.130.54.3",
	    "dst_addr": "204.16.241.243",
	    "etype": "IPv4",
	    "proto": "GRE",
	    "src_port": 0,
	    "dst_port": 0,
	    "in_if": 8001014,
	    "out_if": 1073741823,
	    "src_mac": "2c:dd:e9:27:64:77",
	    "dst_mac": "c4:ca:2b:4d:c5:c7",
	    "src_vlan": 0,
	    "dst_vlan": 0,
	    "vlan_id": 0,
	    "ip_tos": 0,
	    "forwarding_status": 0,
	    "ip_ttl": 61,
	    "ip_flags": 2,
	    "tcp_flags": 0,
	    "icmp_type": 0,
	    "icmp_code": 0,
	    "ipv6_flow_label": 0,
	    "fragment_id": 49677,
	    "fragment_offset": 0,
	    "src_as": 0,
	    "dst_as": 0,
	    "next_hop": "",
	    "next_hop_as": 0,
	    "src_net": "0.0.0.0/0",
	    "dst_net": "0.0.0.0/0",
	    "bgp_next_hop": "",
	    "bgp_communities": [],
	    "as_path": [],
	    "mpls_ttl": [],
	    "mpls_label": [],
	    "mpls_ip": [],
	    "observation_domain_id": 0,
	    "observation_point_id": 0,
	    "layer_stack": [
	        "Ethernet",
	        "IPv4",
	        "GRE",
	        "IPv4",
	        "TCP"
	    ],
	    "layer_size": [
	        14,
	        20,
	        4,
	        20,
	        24
	    ],
	    "ipv6_routing_header_addresses": [],
	    "ipv6_routing_header_seg_left": 0
	}
*/

// FlowSample represents an enriched flow record
type FlowSample struct {
	Type                       string    `json:"type"`
	TimeReceivedNs             time.Time `json:"time_received_ns"`
	SequenceNum                int       `json:"sequence_num"`
	SamplingRate               int       `json:"sampling_rate"`
	SamplerAddress             net.IP    `json:"sampler_address"`
	TimeFlowStartNs            int64     `json:"time_flow_start_ns"`
	TimeFlowEndNs              int64     `json:"time_flow_end_ns"`
	Bytes                      int       `json:"bytes"`
	Packets                    int       `json:"packets"`
	SrcAddress                 net.IP    `json:"src_addr"`
	DstAddress                 net.IP    `json:"dst_addr"`
	EType                      string    `json:"etype"`
	Proto                      string    `json:"proto"`
	SrcPort                    int       `json:"src_port"`
	DstPort                    int       `json:"dst_port"`
	InputIfIndex               int       `json:"in_if"`
	OutputIfIndex              int       `json:"out_if"`
	SrcMac                     string    `json:"src_mac"`
	DstMac                     string    `json:"dst_mac"`
	SrcVlan                    int       `json:"src_vlan"`
	DstVlan                    int       `json:"dst_vlan"`
	VlanId                     int       `json:"vlan_id"`
	IpTos                      int       `json:"ip_tos"`
	ForwardingStatus           int       `json:"forwarding_status"`
	IpTtl                      int       `json:"ip_ttl"`
	IpFlags                    int       `json:"ip_flags"`
	TcpFlags                   int       `json:"tcp_flags"`
	IcmpType                   int       `json:"icmp_type"`
	IcmpCode                   int       `json:"icmp_code"`
	Ipv6FlowLabel              int       `json:"ipv6_flow_label"`
	FragmentId                 int       `json:"fragment_id"`
	FragmentOffset             int       `json:"fragment_offset"`
	SrcAs                      int       `json:"src_as"`
	DstAs                      int       `json:"dst_as"`
	NextHop                    net.IP    `json:"next_hop"`
	NextHopAs                  int       `json:"next_hop_as"`
	SrcNet                     string    `json:"src_net"`
	DstNet                     string    `json:"dst_net"`
	BgpNextHop                 net.IP    `json:"bgp_next_hop"`
	BgpCommunities             []string  `json:"bgp_communities"`
	AsPath                     []int     `json:"as_path"`
	MplsTtl                    []int     `json:"mpls_ttl"`
	MplsLabel                  []string  `json:"mpls_label"`
	MplsIp                     []string  `json:"mpls_ip"`
	ObservationDomainId        int       `json:"observation_domain_id"`
	ObservationPointId         int       `json:"observation_point_id"`
	LayerStack                 []string  `json:"layer_stack"`
	LayerSize                  []int     `json:"layer_size"`
	Ipv6RoutingHeaderAddresses []net.IP  `json:"ipv6_routing_header_addresses"`
	Ipv6RoutingHeaderSegLeft   int       `json:"ipv6_routing_header_seg_left"`

	// New enriched fields should be inserted below this comment.
	// Fields above are the default fields sent via Goflow.
	InputInterface  string `json:"in_ifname"`
	OutputInterface string `json:"out_ifname"`
}
