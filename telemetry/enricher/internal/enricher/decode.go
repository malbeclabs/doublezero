package enricher

import (
	"bytes"
	"log"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	flow "github.com/malbeclabs/doublezero/telemetry/proto/flow/gen/pb-go"
	"github.com/netsampler/goflow2/v2/decoders/sflow"
)

// DecodeSFlow decodes a sFlow flow record from raw bytes.
func DecodeSFlow(sflowSample *flow.FlowSample) ([]FlowSample, error) {
	var samples []FlowSample
	packet := sflow.Packet{}
	err := sflow.DecodeMessageVersion(bytes.NewBuffer(sflowSample.FlowPayload), &packet)
	if err != nil {
		return nil, err
	}
	for _, s := range packet.Samples {
		var sample FlowSample
		sample.TimeReceivedNs = sflowSample.ReceiveTimestamp.AsTime()
		log.Printf("processing sFlow sample: %+v", s)
		var records []sflow.FlowRecord
		switch flowSample := s.(type) {
		case sflow.FlowSample:
			records = flowSample.Records
			sample.InputIfIndex = int(flowSample.Input)
			sample.OutputIfIndex = int(flowSample.Output)
			sample.SamplingRate = int(flowSample.SamplingRate)
			sample.Packets = 1
			log.Printf("flow sample input_if=%d output_if=%d sampling_rate=%d", sample.InputIfIndex, sample.OutputIfIndex, sample.SamplingRate)
		case sflow.ExpandedFlowSample:
			records = flowSample.Records
		}
		for _, record := range records {
			switch record.Header.DataFormat {
			case sflow.FLOW_TYPE_RAW:
				r, ok := record.Data.(sflow.SampledHeader)
				if !ok {
					continue
				}
				// The raw record data is a complete packet.
				// We can parse it with gopacket.
				// The link layer type is typically Ethernet.
				p := gopacket.NewPacket(r.HeaderData, layers.LinkTypeEthernet, gopacket.Default)
				// Get the Ethernet layer
				if ethLayer := p.Layer(layers.LayerTypeEthernet); ethLayer != nil {
					eth, _ := ethLayer.(*layers.Ethernet)
					sample.SrcMac = eth.SrcMAC.String()
					sample.DstMac = eth.DstMAC.String()
					sample.EType = eth.EthernetType.String()
				}
				// Get the IP layer
				if ipLayer := p.Layer(layers.LayerTypeIPv4); ipLayer != nil {
					ip, _ := ipLayer.(*layers.IPv4)
					sample.SrcAddress = ip.SrcIP
					sample.DstAddress = ip.DstIP
					sample.Proto = ip.Protocol.String()
					sample.IpTtl = int(ip.TTL)
					sample.IpTos = int(ip.TOS)
					sample.IpFlags = int(ip.Flags)
					sample.Bytes = int(ip.Length)
				}
				if ip6Layer := p.Layer(layers.LayerTypeIPv6); ip6Layer != nil {
					ip6, _ := ip6Layer.(*layers.IPv6)
					sample.SrcAddress = ip6.SrcIP
					sample.DstAddress = ip6.DstIP
					sample.Proto = ip6.NextHeader.String()
					sample.Ipv6FlowLabel = int(ip6.FlowLabel)
				}
				// Get the transport layer
				if tcpLayer := p.Layer(layers.LayerTypeTCP); tcpLayer != nil {
					tcp, _ := tcpLayer.(*layers.TCP)
					sample.SrcPort = int(tcp.SrcPort)
					sample.DstPort = int(tcp.DstPort)
					sample.TcpFlags = tcpFlags(tcp)
				}
				if udpLayer := p.Layer(layers.LayerTypeUDP); udpLayer != nil {
					udp, _ := udpLayer.(*layers.UDP)
					sample.SrcPort = int(udp.SrcPort)
					sample.DstPort = int(udp.DstPort)
				}

			case sflow.FLOW_TYPE_ETH:
			case sflow.FLOW_TYPE_IPV4:
			case sflow.FLOW_TYPE_IPV6:
			case sflow.FLOW_TYPE_EXT_SWITCH:
				r, ok := record.Data.(*sflow.ExtendedSwitch)
				if !ok {
					continue
				}
				sample.SrcVlan = int(r.SrcVlan)
				sample.DstVlan = int(r.DstVlan)
			case sflow.FLOW_TYPE_EXT_ROUTER:
			case sflow.FLOW_TYPE_EXT_GATEWAY:
			}
		}
		samples = append(samples, sample)
	}
	return samples, nil
}

func tcpFlags(tcp *layers.TCP) int {
	var flags int
	if tcp.FIN {
		flags |= 1
	}
	if tcp.SYN {
		flags |= 2
	}
	if tcp.RST {
		flags |= 4
	}
	if tcp.PSH {
		flags |= 8
	}
	if tcp.ACK {
		flags |= 16
	}
	if tcp.URG {
		flags |= 32
	}
	if tcp.ECE {
		flags |= 64
	}
	if tcp.CWR {
		flags |= 128
	}
	return flags
}
