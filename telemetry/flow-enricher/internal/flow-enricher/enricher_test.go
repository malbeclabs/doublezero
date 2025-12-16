package enricher

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	flow "github.com/malbeclabs/doublezero/telemetry/proto/flow/gen/pb-go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type MockFlowConsumer struct {
	mu              sync.Mutex
	SamplesToReturn [][]FlowSample
	FetchCount      int
	CommitCalled    bool
	CloseCalled     bool
	ConsumeError    error
	CommitError     error
}

func (m *MockFlowConsumer) ConsumeFlowRecords(ctx context.Context) ([]FlowSample, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ConsumeError != nil {
		return nil, m.ConsumeError
	}
	if len(m.SamplesToReturn) == 0 || m.FetchCount >= len(m.SamplesToReturn) {
		// Block to simulate waiting for new messages, respecting context cancellation.
		<-ctx.Done()
		return nil, ctx.Err()
	}
	samples := m.SamplesToReturn[m.FetchCount]
	m.FetchCount++
	return samples, nil
}

func (m *MockFlowConsumer) CommitOffsets(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.CommitCalled = true
	return m.CommitError
}

func (m *MockFlowConsumer) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.CloseCalled = true
	return nil
}

type MockClicker struct {
	mu              sync.Mutex
	ReceivedSamples []FlowSample
	InsertError     error
}

func (m *MockClicker) BatchInsert(ctx context.Context, samples []FlowSample) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.InsertError != nil {
		return m.InsertError
	}
	m.ReceivedSamples = append(m.ReceivedSamples, samples...)
	return nil
}

func TestEnricher(t *testing.T) {
	payload := readPcap(t, "./fixtures/sflow_ingress_user_traffic.pcap")
	rawKafkaMessage := &flow.FlowSample{
		ReceiveTimestamp: &timestamppb.Timestamp{Seconds: 1625243456},
		FlowPayload:      payload,
	}

	expectedSamples, err := DecodeSFlow(rawKafkaMessage)
	require.NoError(t, err, "DecodeSFlow failed during test setup")
	require.NotEmpty(t, expectedSamples, "DecodeSFlow should produce samples from the fixture")

	mockConsumer := &MockFlowConsumer{
		SamplesToReturn: [][]FlowSample{expectedSamples},
	}
	mockWriter := &MockClicker{}

	reg := prometheus.NewRegistry()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	enricher := NewEnricher(
		WithFlowConsumer(mockConsumer),
		WithClickhouseWriter(mockWriter),
		WithLogger(logger),
		WithEnricherMetrics(NewEnricherMetrics(reg)),
	)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := enricher.Run(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("Enricher.Run returned an unexpected error: %v", err)
		}
	}()

	require.Eventually(t, func() bool {
		mockWriter.mu.Lock()
		defer mockWriter.mu.Unlock()
		return len(mockWriter.ReceivedSamples) >= len(expectedSamples)
	}, 3*time.Second, 50*time.Millisecond, "ClickHouse writer did not receive the expected number of samples")

	cancel()
	wg.Wait()

	mockConsumer.mu.Lock()
	require.True(t, mockConsumer.CommitCalled, "Expected CommitOffsets to be called")
	require.True(t, mockConsumer.CloseCalled, "Expected Close to be called")
	mockConsumer.mu.Unlock()

	mockWriter.mu.Lock()
	defer mockWriter.mu.Unlock()
	require.Equal(t, expectedSamples, mockWriter.ReceivedSamples, "The records received by the writer do not match the records generated from the pcap")
}

func TestEnricherMetrics(t *testing.T) {
	tests := []struct {
		name                    string
		mockConsumer            *MockFlowConsumer
		mockWriter              *MockClicker
		expectedFlowsProcessed  float64
		expectedClickhouseErrs  float64
		expectedKafkaCommitErrs float64
	}{
		{
			name: "Successful run increments FlowsProcessedTotal",
			mockConsumer: &MockFlowConsumer{
				SamplesToReturn: [][]FlowSample{{{SrcAddress: net.IP("1.1.1.1")}, {SrcAddress: net.IP("2.2.2.2")}}},
			},
			mockWriter:             &MockClicker{},
			expectedFlowsProcessed: 2,
		},
		{
			name: "ClickHouse insert error increments ClickhouseInsertErrors",
			mockConsumer: &MockFlowConsumer{
				SamplesToReturn: [][]FlowSample{{{SrcAddress: net.IP("1.1.1.1")}, {SrcAddress: net.IP("2.2.2.2")}}},
			},
			mockWriter: &MockClicker{
				InsertError: errors.New("clickhouse failed"),
			},
			expectedClickhouseErrs: 1,
		},
		{
			name: "Kafka commit error increments KafkaCommitErrors",
			mockConsumer: &MockFlowConsumer{
				SamplesToReturn: [][]FlowSample{{{SrcAddress: net.IP("1.1.1.1")}, {SrcAddress: net.IP("2.2.2.2")}}},
				CommitError:     errors.New("kafka commit failed"),
			},
			mockWriter:              &MockClicker{},
			expectedKafkaCommitErrs: 1,
		},
		{
			name: "No samples processed does not increment metrics",
			mockConsumer: &MockFlowConsumer{
				SamplesToReturn: [][]FlowSample{},
			},
			mockWriter: &MockClicker{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := prometheus.NewRegistry()
			metrics := NewEnricherMetrics(reg)

			enricher := NewEnricher(
				WithFlowConsumer(tt.mockConsumer),
				WithClickhouseWriter(tt.mockWriter),
				WithEnricherMetrics(metrics),
			)

			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()

			go func() {
				_ = enricher.Run(ctx)
			}()

			<-ctx.Done()

			require.Equal(t, tt.expectedFlowsProcessed, testutil.ToFloat64(metrics.FlowsProcessedTotal), "FlowsProcessedTotal mismatch")
			require.Equal(t, tt.expectedClickhouseErrs, testutil.ToFloat64(metrics.ClickhouseInsertErrors), "ClickhouseInsertErrors mismatch")
			require.Equal(t, tt.expectedKafkaCommitErrs, testutil.ToFloat64(metrics.KafkaCommitErrors), "KafkaCommitErrors mismatch")
		})
	}
}
