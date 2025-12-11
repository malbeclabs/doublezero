package enricher

import (
	"log"
	"net"
	"os"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/layers"
	"github.com/gopacket/gopacket/pcap"
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
	handle, err := pcap.OpenOfflineFile(f)
	if err != nil {
		t.Fatalf("failed to open pcap file: %v", err)
	}
	defer handle.Close()

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
			name: "Valid IPv4 Sampled Flow from bytes",
			input: &flow.FlowSample{
				ReceiveTimestamp: &timestamppb.Timestamp{Seconds: 1625243456, Nanos: 0},
				FlowPayload:      readPcap(t, "./fixtures/sflow_single.pcap"),
			},
			expected: []FlowSample{
				{
					EType:          "IPv4",
					Bytes:          1428,
					Packets:        1,
					SamplingRate:   1024,
					InputIfIndex:   8001063,
					OutputIfIndex:  8001134,
					TimeReceivedNs: time.Unix(1625243456, 0),
					SrcAddress:     net.ParseIP("137.174.145.145"),
					DstAddress:     net.ParseIP("137.174.145.147"),
					SrcPort:        41306,
					DstPort:        5001,
					Proto:          "UDP",
					TcpFlags:       0,
					SrcMac:         "0c:42:a1:07:b9:da",
					DstMac:         "c4:ca:2b:4d:f1:f4",
					IpTtl:          64,
					IpFlags:        2,
				},
			},
			expectErr: false,
		},
		{
			name:      "Malformed Packet",
			input:     &flow.FlowSample{FlowPayload: []byte{0x00, 0x01, 0x02}},
			expected:  nil,
			expectErr: true,
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
				g := got[0]
				log.Printf("Decoded Flow Sample: %+v", g)
				expected := tt.expected[0]

				if diff := cmp.Diff(expected, g); diff != "" {
					t.Errorf("DecodeSFlow() mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}
