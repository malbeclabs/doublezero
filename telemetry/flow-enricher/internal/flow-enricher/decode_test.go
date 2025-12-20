package enricher

import (
	"net"
	"os"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/layers"
	"github.com/gopacket/gopacket/pcapgo"
	flow "github.com/malbeclabs/doublezero/telemetry/proto/flow/gen/pb-go"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func readPcap(t *testing.T, file string) []byte {
	t.Helper()
	f, err := os.Open(file)
	if err != nil {
		t.Fatalf("failed to open pcap file: %v", err)
	}
	defer f.Close()
	handle, err := pcapgo.NewReader(f)
	if err != nil {
		t.Fatalf("failed to open pcap file: %v", err)
	}

	packetSource := gopacket.NewPacketSource(handle, handle.LinkType())
	for packet := range packetSource.Packets() {
		if udp := packet.Layer(layers.LayerTypeUDP); udp != nil {
			return udp.LayerPayload()
		}
	}
	t.Fatal("no sflow packet found in pcap file")
	return nil
}

func TestDecodeSFlow(t *testing.T) {
	tests := []struct {
		name      string
		input     *flow.FlowSample
		expected  []FlowSample
		expectErr bool
	}{
		{
			name:      "Malformed Packet",
			input:     &flow.FlowSample{FlowPayload: []byte{0x00, 0x01, 0x02}},
			expected:  nil,
			expectErr: true,
		},
		{
			name: "Ingress user traffic",
			input: &flow.FlowSample{
				ReceiveTimestamp: &timestamppb.Timestamp{Seconds: 1625243456, Nanos: 0},
				FlowPayload:      readPcap(t, "./fixtures/sflow_ingress_user_traffic.pcap"),
			},
			expected: []FlowSample{
				{
					EType:          "IPv4",
					Bytes:          1428,
					Packets:        1,
					SamplingRate:   1024,
					SamplerAddress: net.ParseIP("137.174.145.144"),
					InputIfIndex:   8001063,
					OutputIfIndex:  8001134,
					TimeReceivedNs: time.Unix(1625243456, 0),
					SrcAddress:     net.ParseIP("137.174.145.145"),
					DstAddress:     net.ParseIP("137.174.145.147"),
					SrcPort:        47252,
					DstPort:        5001,
					Proto:          "UDP",
					TcpFlags:       0,
					SrcMac:         "0c:42:a1:07:b9:da",
					DstMac:         "c4:ca:2b:4d:f1:f4",
					IpTtl:          64,
					IpFlags:        2,
				},
				{
					EType:          "IPv4",
					Bytes:          1428,
					Packets:        1,
					SamplingRate:   1024,
					SamplerAddress: net.ParseIP("137.174.145.144"),
					InputIfIndex:   8001063,
					OutputIfIndex:  8001134,
					TimeReceivedNs: time.Unix(1625243456, 0),
					SrcAddress:     net.ParseIP("137.174.145.145"),
					DstAddress:     net.ParseIP("137.174.145.147"),
					SrcPort:        47252,
					DstPort:        5001,
					Proto:          "UDP",
					TcpFlags:       0,
					SrcMac:         "0c:42:a1:07:b9:da",
					DstMac:         "c4:ca:2b:4d:f1:f4",
					IpTtl:          64,
					IpFlags:        2,
				},
			},
		},
		{name: "Egress user traffic",
			input: &flow.FlowSample{
				ReceiveTimestamp: &timestamppb.Timestamp{Seconds: 1625243456, Nanos: 0},
				FlowPayload:      readPcap(t, "./fixtures/sflow_egress_user_traffic.pcap"),
			},
			expected: []FlowSample{
				{
					EType:          "MPLSUnicast",
					Bytes:          1428,
					Packets:        1,
					SamplingRate:   1024,
					SamplerAddress: net.ParseIP("137.174.145.144"),
					InputIfIndex:   8001134,
					OutputIfIndex:  8001063,
					TimeReceivedNs: time.Unix(1625243456, 0),
					SrcAddress:     net.ParseIP("137.174.145.147"),
					DstAddress:     net.ParseIP("137.174.145.145"),
					SrcPort:        36115,
					DstPort:        5001,
					Proto:          "UDP",
					TcpFlags:       0,
					SrcMac:         "c4:ca:2b:4d:ea:c3",
					DstMac:         "c4:ca:2b:4d:f1:f4",
					IpTtl:          63,
					IpFlags:        2,
					MplsLabel:      []string{"116386"},
					IpTos:          0,
				},
				{
					EType:          "MPLSUnicast",
					Bytes:          1428,
					Packets:        1,
					SamplingRate:   1024,
					SamplerAddress: net.ParseIP("137.174.145.144"),
					InputIfIndex:   8001134,
					OutputIfIndex:  8001063,
					TimeReceivedNs: time.Unix(1625243456, 0),
					SrcAddress:     net.ParseIP("137.174.145.147"),
					DstAddress:     net.ParseIP("137.174.145.145"),
					SrcPort:        36115,
					DstPort:        5001,
					Proto:          "UDP",
					TcpFlags:       0,
					SrcMac:         "c4:ca:2b:4d:ea:c3",
					DstMac:         "c4:ca:2b:4d:f1:f4",
					IpTtl:          63,
					IpFlags:        2,
					MplsLabel:      []string{"116386"},
					IpTos:          0,
				},
			},
		},
		{name: "Ingress user traffic w/ expanded format",
			input: &flow.FlowSample{
				ReceiveTimestamp: &timestamppb.Timestamp{Seconds: 1625243456, Nanos: 0},
				FlowPayload:      readPcap(t, "./fixtures/sflow_ingress_user_traffic_expanded.pcap"),
			},
			expected: []FlowSample{
				{
					EType:          "IPv4",
					Bytes:          1077,
					Packets:        1,
					SamplingRate:   4096,
					SamplerAddress: net.ParseIP("64.86.249.22"),
					InputIfIndex:   8001147,
					OutputIfIndex:  8001013,
					TimeReceivedNs: time.Unix(1625243456, 0),
					SrcAddress:     net.ParseIP("84.32.71.38"),
					DstAddress:     net.ParseIP("94.158.242.122"),
					SrcPort:        8001,
					DstPort:        8001,
					Proto:          "UDP",
					TcpFlags:       0,
					SrcMac:         "88:e0:f3:85:81:9f",
					DstMac:         "c4:ca:2b:4d:73:97",
					IpTtl:          64,
					IpFlags:        2,
					IpTos:          0,
				},
				{
					EType:          "IPv4",
					Bytes:          1077,
					Packets:        1,
					SamplingRate:   4096,
					SamplerAddress: net.ParseIP("64.86.249.22"),
					InputIfIndex:   8001147,
					OutputIfIndex:  8001013,
					TimeReceivedNs: time.Unix(1625243456, 0),
					SrcAddress:     net.ParseIP("84.32.71.38"),
					DstAddress:     net.ParseIP("216.18.214.178"),
					SrcPort:        8001,
					DstPort:        8000,
					Proto:          "UDP",
					TcpFlags:       0,
					SrcMac:         "88:e0:f3:85:81:9f",
					DstMac:         "c4:ca:2b:4d:73:97",
					IpTtl:          64,
					IpFlags:        2,
					IpTos:          0,
				},
				{
					EType:          "IPv4",
					Bytes:          533,
					Packets:        1,
					SamplingRate:   4096,
					SamplerAddress: net.ParseIP("64.86.249.22"),
					InputIfIndex:   8001147,
					OutputIfIndex:  8001013,
					TimeReceivedNs: time.Unix(1625243456, 0),
					SrcAddress:     net.ParseIP("186.233.187.141"),
					DstAddress:     net.ParseIP("107.155.92.114"),
					SrcPort:        8000,
					DstPort:        8001,
					Proto:          "UDP",
					TcpFlags:       0,
					SrcMac:         "88:e0:f3:85:81:9f",
					DstMac:         "c4:ca:2b:4d:73:97",
					IpTtl:          64,
					IpFlags:        2,
					IpTos:          0,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DecodeSFlow(tt.input)

			if (err != nil) != tt.expectErr {
				t.Fatalf("DecodeSFlow() error = %v, wantErr %v", err, tt.expectErr)
			}

			if !tt.expectErr {
				if len(got) == 0 {
					t.Fatal("DecodeSFlow() returned no samples")
				}
				// The test packet contains multiple samples, we'll check the first one.
				g := got
				expected := tt.expected

				if diff := cmp.Diff(expected, g); diff != "" {
					t.Errorf("DecodeSFlow() mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}
