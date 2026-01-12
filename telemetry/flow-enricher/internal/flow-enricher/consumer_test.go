package enricher

import (
	"context"
	"io"
	"log/slog"
	"testing"

	flow "github.com/malbeclabs/doublezero/telemetry/proto/flow/gen/pb-go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// mockKafkaClient implements kafkaClient for testing.
type mockKafkaClient struct {
	fetches kgo.Fetches
}

func (m *mockKafkaClient) PollFetches(ctx context.Context) kgo.Fetches {
	return m.fetches
}

func (m *mockKafkaClient) CommitUncommittedOffsets(ctx context.Context) error {
	return nil
}

func (m *mockKafkaClient) Close() {}

// createTestFetches creates kgo.Fetches with the given records.
// This simulates what Kafka returns when polling for messages.
func createTestFetches(records []*kgo.Record) kgo.Fetches {
	return kgo.Fetches{
		kgo.Fetch{
			Topics: []kgo.FetchTopic{
				{
					Topic: "test-topic",
					Partitions: []kgo.FetchPartition{
						{
							Partition: 0,
							Records:   records,
						},
					},
				},
			},
		},
	}
}

// TestConsumeFlowRecords_BatchAccumulation is a regression test for a bug where
// ConsumeFlowRecords was overwriting samples instead of appending them when
// processing multiple Kafka records in a batch.
func TestConsumeFlowRecords_BatchAccumulation(t *testing.T) {
	// Load the expanded pcap fixture which contains 3 samples per sFlow packet
	// readPcap returns a single packet from the pcap
	payload := readPcap(t, "./fixtures/sflow_ingress_user_traffic_expanded.pcap")

	// Create multiple Kafka records with different timestamps
	// Each record will decode to 3 flow samples
	flowSample1 := &flow.FlowSample{
		ReceiveTimestamp: &timestamppb.Timestamp{Seconds: 1000},
		FlowPayload:      payload,
	}
	flowSample2 := &flow.FlowSample{
		ReceiveTimestamp: &timestamppb.Timestamp{Seconds: 2000},
		FlowPayload:      payload,
	}
	flowSample3 := &flow.FlowSample{
		ReceiveTimestamp: &timestamppb.Timestamp{Seconds: 3000},
		FlowPayload:      payload,
	}

	// Marshal to proto bytes (as they would be in Kafka)
	data1, err := proto.Marshal(flowSample1)
	require.NoError(t, err)
	data2, err := proto.Marshal(flowSample2)
	require.NoError(t, err)
	data3, err := proto.Marshal(flowSample3)
	require.NoError(t, err)

	// Create Kafka records
	records := []*kgo.Record{
		{Value: data1},
		{Value: data2},
		{Value: data3},
	}

	// Create mock client that returns these records
	mockClient := &mockKafkaClient{
		fetches: createTestFetches(records),
	}

	// Create consumer with mock client
	reg := prometheus.NewRegistry()
	consumer, err := NewKafkaFlowConsumer(
		withKafkaClient(mockClient),
		WithFlowConsumerMetrics(NewFlowConsumerMetrics(reg)),
		WithKafkaLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
	)
	require.NoError(t, err)

	// Call the actual ConsumeFlowRecords method
	samples, err := consumer.ConsumeFlowRecords(context.Background())
	require.NoError(t, err)

	// Each sFlow packet in this fixture contains 3 samples
	// With 3 Kafka records, we should have 9 total samples
	expectedSamplesPerRecord := 3
	expectedTotalSamples := len(records) * expectedSamplesPerRecord

	require.Len(t, samples, expectedTotalSamples,
		"Expected %d samples (3 records × %d samples each), got %d. "+
			"This indicates samples are being overwritten instead of appended.",
		expectedTotalSamples, expectedSamplesPerRecord, len(samples))

	// Verify samples from all records are present by checking timestamps
	timestampCounts := make(map[int64]int)
	for _, s := range samples {
		ts := s.TimeReceivedNs.Unix()
		timestampCounts[ts]++
	}

	require.Equal(t, expectedSamplesPerRecord, timestampCounts[1000],
		"Expected %d samples from record 1 (timestamp 1000)", expectedSamplesPerRecord)
	require.Equal(t, expectedSamplesPerRecord, timestampCounts[2000],
		"Expected %d samples from record 2 (timestamp 2000)", expectedSamplesPerRecord)
	require.Equal(t, expectedSamplesPerRecord, timestampCounts[3000],
		"Expected %d samples from record 3 (timestamp 3000)", expectedSamplesPerRecord)
}

// TestConsumeFlowRecords_EmptyFetch verifies handling of empty fetches.
func TestConsumeFlowRecords_EmptyFetch(t *testing.T) {
	mockClient := &mockKafkaClient{
		fetches: kgo.Fetches{}, // Empty
	}

	reg := prometheus.NewRegistry()
	consumer, err := NewKafkaFlowConsumer(
		withKafkaClient(mockClient),
		WithFlowConsumerMetrics(NewFlowConsumerMetrics(reg)),
		WithKafkaLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
	)
	require.NoError(t, err)

	samples, err := consumer.ConsumeFlowRecords(context.Background())
	require.NoError(t, err)
	require.Nil(t, samples)
}

// TestConsumeFlowRecords_SingleRecord verifies a single record is processed correctly.
func TestConsumeFlowRecords_SingleRecord(t *testing.T) {
	payload := readPcap(t, "./fixtures/sflow_ingress_user_traffic_expanded.pcap")

	flowSample := &flow.FlowSample{
		ReceiveTimestamp: &timestamppb.Timestamp{Seconds: 1625243456},
		FlowPayload:      payload,
	}
	data, err := proto.Marshal(flowSample)
	require.NoError(t, err)

	records := []*kgo.Record{{Value: data}}
	mockClient := &mockKafkaClient{
		fetches: createTestFetches(records),
	}

	reg := prometheus.NewRegistry()
	consumer, err := NewKafkaFlowConsumer(
		withKafkaClient(mockClient),
		WithFlowConsumerMetrics(NewFlowConsumerMetrics(reg)),
		WithKafkaLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
	)
	require.NoError(t, err)

	samples, err := consumer.ConsumeFlowRecords(context.Background())
	require.NoError(t, err)
	require.Len(t, samples, 3, "Expected 3 samples from the expanded sFlow fixture")
}
