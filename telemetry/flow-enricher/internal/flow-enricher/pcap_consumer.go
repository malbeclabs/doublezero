package enricher

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/layers"
	"github.com/gopacket/gopacket/pcap"
	flow "github.com/malbeclabs/doublezero/telemetry/proto/flow/gen/pb-go"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// PcapFlowConsumer implements FlowConsumer for reading sFlow packets from a pcap file.
type PcapFlowConsumer struct {
	pcapPath string
	consumed bool
}

// NewPcapFlowConsumer creates a new PcapFlowConsumer that reads from the given pcap file.
func NewPcapFlowConsumer(pcapPath string) *PcapFlowConsumer {
	return &PcapFlowConsumer{
		pcapPath: pcapPath,
	}
}

// ConsumeFlowRecords reads all sFlow packets from the pcap file and returns them as FlowSamples.
// On the first call, it reads and returns all packets. Subsequent calls return io.EOF.
func (p *PcapFlowConsumer) ConsumeFlowRecords(ctx context.Context) ([]FlowSample, error) {
	if p.consumed {
		return nil, io.EOF
	}
	p.consumed = true

	f, err := os.Open(p.pcapPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open pcap file: %w", err)
	}
	defer f.Close()

	handle, err := pcap.OpenOfflineFile(f)
	if err != nil {
		return nil, fmt.Errorf("failed to parse pcap file: %w", err)
	}
	defer handle.Close()

	var allSamples []FlowSample
	packetSource := gopacket.NewPacketSource(handle, handle.LinkType())

	for packet := range packetSource.Packets() {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		udpLayer := packet.Layer(layers.LayerTypeUDP)
		if udpLayer == nil {
			continue
		}

		payload := udpLayer.LayerPayload()
		if len(payload) == 0 {
			continue
		}

		ts := packet.Metadata().Timestamp

		flowSample := &flow.FlowSample{
			ReceiveTimestamp: timestamppb.New(ts),
			FlowPayload:      payload,
		}

		samples, err := DecodeSFlow(flowSample)
		if err != nil {
			continue
		}

		allSamples = append(allSamples, samples...)
	}

	return allSamples, nil
}

// CommitOffsets is a no-op for pcap files.
func (p *PcapFlowConsumer) CommitOffsets(ctx context.Context) error {
	return nil
}

func (p *PcapFlowConsumer) Close() error {
	return nil
}
