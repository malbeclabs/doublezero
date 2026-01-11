package gnmi

import (
	"context"
	"testing"

	gpb "github.com/openconfig/gnmi/proto/gnmi"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/protobuf/proto"
)

// mockKafkaClient implements kafkaClient for testing.
type mockKafkaClient struct {
	fetches kgo.Fetches
	closed  bool
}

func (m *mockKafkaClient) PollFetches(ctx context.Context) kgo.Fetches {
	return m.fetches
}

func (m *mockKafkaClient) CommitUncommittedOffsets(ctx context.Context) error {
	return nil
}

func (m *mockKafkaClient) Close() {
	m.closed = true
}

func TestKafkaConsumer_Consume(t *testing.T) {
	// Create a test notification
	notification := &gpb.Notification{
		Timestamp: 1234567890,
		Prefix: &gpb.Path{
			Target: "device1",
		},
		Update: []*gpb.Update{
			{
				Path: &gpb.Path{
					Elem: []*gpb.PathElem{
						{Name: "interfaces"},
						{Name: "interface", Key: map[string]string{"name": "eth0"}},
						{Name: "state"},
						{Name: "admin-status"},
					},
				},
				Val: &gpb.TypedValue{
					Value: &gpb.TypedValue_StringVal{StringVal: "UP"},
				},
			},
		},
	}

	data, err := proto.Marshal(notification)
	if err != nil {
		t.Fatalf("failed to marshal notification: %v", err)
	}

	// Create mock client with a single record
	mockClient := &mockKafkaClient{
		fetches: kgo.Fetches{
			{
				Topics: []kgo.FetchTopic{
					{
						Topic: "test-topic",
						Partitions: []kgo.FetchPartition{
							{
								Records: []*kgo.Record{
									{Value: data},
								},
							},
						},
					},
				},
			},
		},
	}

	consumer, err := NewKafkaConsumer(
		withKafkaClient(mockClient),
		WithConsumerMetrics(NewConsumerMetrics(prometheus.NewRegistry())),
	)
	if err != nil {
		t.Fatalf("failed to create consumer: %v", err)
	}

	notifications, err := consumer.Consume(context.Background())
	if err != nil {
		t.Fatalf("failed to consume: %v", err)
	}

	if len(notifications) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(notifications))
	}

	if notifications[0].GetPrefix().GetTarget() != "device1" {
		t.Errorf("expected device1, got %s", notifications[0].GetPrefix().GetTarget())
	}
}
