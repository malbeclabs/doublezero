package e2e_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/redpanda"
	"github.com/twmb/franz-go/pkg/kgo"
	goproto "google.golang.org/protobuf/proto"

	"github.com/malbeclabs/doublezero/telemetry/flow-ingest/internal/kafka"
	"github.com/malbeclabs/doublezero/telemetry/flow-ingest/internal/server"
	flowproto "github.com/malbeclabs/doublezero/telemetry/proto/flow/gen/pb-go"
)

func TestTelemetry_FlowIngest_E2E_ProducesToKafka(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	brokers := startRedpanda(t, ctx)
	topic := "flow-test-" + sanitizeTopicSuffix(t.Name())

	kc := newKafkaClient(t, ctx, brokers)
	requireTopic(t, ctx, kc, topic)

	flowListener, healthListener := newListeners(t)
	log := newTestLogger()

	srv := newServer(t, log, flowListener, healthListener, kc, topic)

	runCtx, runCancel := context.WithCancel(ctx)
	defer runCancel()
	errCh := srv.Start(runCtx, runCancel)

	consumer := newConsumerNoGroup(t, brokers, topic)
	defer consumer.Close()

	wantDatagram := buildSFlowV5DatagramWithFlowSample()
	sendUDP(t, flowListener.LocalAddr().(*net.UDPAddr), wantDatagram)

	var got flowproto.FlowSample
	require.Eventually(t, func() bool {
		v, ok := pollOneValue(ctx, consumer)
		if !ok {
			return false
		}
		require.NoError(t, goproto.Unmarshal(v, &got))
		return true
	}, 10*time.Second, 50*time.Millisecond)

	require.NotNil(t, got.ReceiveTimestamp)
	require.True(t, bytes.Equal(got.FlowPayload, wantDatagram))

	runCancel()
	select {
	case err := <-errCh:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("server did not stop")
	}
}

func TestTelemetry_FlowIngest_E2E_NoFlowSample_NoKafkaRecord(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	brokers := startRedpanda(t, ctx)
	topic := "flow-test-" + sanitizeTopicSuffix(t.Name())

	kc := newKafkaClient(t, ctx, brokers)
	requireTopic(t, ctx, kc, topic)

	flowListener, healthListener := newListeners(t)
	srv := newServer(t, newTestLogger(), flowListener, healthListener, kc, topic)

	runCtx, runCancel := context.WithCancel(ctx)
	defer runCancel()
	_ = srv.Start(runCtx, runCancel)

	consumer := newConsumerNoGroup(t, brokers, topic)
	defer consumer.Close()

	sendUDP(t, flowListener.LocalAddr().(*net.UDPAddr), buildSFlowV5DatagramWithNoSamples())

	require.Never(t, func() bool {
		_, ok := pollOneValue(ctx, consumer)
		return ok
	}, 1200*time.Millisecond, 50*time.Millisecond)
}

func TestTelemetry_FlowIngest_E2E_InvalidSFlow_NoKafkaRecord(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	brokers := startRedpanda(t, ctx)
	topic := "flow-test-" + sanitizeTopicSuffix(t.Name())

	kc := newKafkaClient(t, ctx, brokers)
	requireTopic(t, ctx, kc, topic)

	flowListener, healthListener := newListeners(t)
	srv := newServer(t, newTestLogger(), flowListener, healthListener, kc, topic)

	runCtx, runCancel := context.WithCancel(ctx)
	defer runCancel()
	_ = srv.Start(runCtx, runCancel)

	consumer := newConsumerNoGroup(t, brokers, topic)
	defer consumer.Close()

	sendUDP(t, flowListener.LocalAddr().(*net.UDPAddr), []byte{0x01, 0x02, 0x03})

	require.Never(t, func() bool {
		_, ok := pollOneValue(ctx, consumer)
		return ok
	}, 1200*time.Millisecond, 50*time.Millisecond)
}

func TestTelemetry_FlowIngest_E2E_HealthAcceptsAndCloses(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	brokers := startRedpanda(t, ctx)
	kc := newKafkaClient(t, ctx, brokers)

	flowListener, healthListener := newListeners(t)
	srv := newServer(t, slog.New(slog.NewTextHandler(io.Discard, nil)), flowListener, healthListener, kc, "ignored")

	runCtx, runCancel := context.WithCancel(ctx)
	defer runCancel()
	_ = srv.Start(runCtx, runCancel)

	c, err := net.Dial("tcp", healthListener.Addr().String())
	require.NoError(t, err)
	defer c.Close()

	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	var b [1]byte
	_, rerr := c.Read(b[:])
	require.Error(t, rerr)
}

func TestTelemetry_FlowIngest_E2E_NPackets_NRecords(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	brokers := startRedpanda(t, ctx)
	topic := "flow-test-" + sanitizeTopicSuffix(t.Name())

	kc := newKafkaClient(t, ctx, brokers)
	requireTopic(t, ctx, kc, topic)

	flowListener, healthListener := newListeners(t)
	srv := newServer(t, newTestLogger(), flowListener, healthListener, kc, topic)

	runCtx, runCancel := context.WithCancel(ctx)
	defer runCancel()
	_ = srv.Start(runCtx, runCancel)

	consumer := newConsumerNoGroup(t, brokers, topic)
	defer consumer.Close()

	dst := flowListener.LocalAddr().(*net.UDPAddr)

	const N = 50
	want := make(map[string]struct{}, N)

	udpConn, err := net.DialUDP("udp", nil, dst)
	require.NoError(t, err)
	defer udpConn.Close()

	for i := 0; i < N; i++ {
		d := buildSFlowV5DatagramWithFlowSampleMarker(uint32(i + 1))
		want[string(d)] = struct{}{}
		_, err := udpConn.Write(d)
		require.NoError(t, err)
	}

	got := make(map[string]struct{}, N)

	require.Eventually(t, func() bool {
		pctx, cancel := context.WithTimeout(ctx, 400*time.Millisecond)
		defer cancel()

		fetches := consumer.PollFetches(pctx)
		for _, e := range fetches.Errors() {
			if errors.Is(e.Err, context.DeadlineExceeded) || errors.Is(e.Err, context.Canceled) {
				continue
			}
			t.Fatalf("consume error: %v", e)
		}

		iter := fetches.RecordIter()
		for !iter.Done() {
			r := iter.Next()
			var fs flowproto.FlowSample
			if err := goproto.Unmarshal(r.Value, &fs); err != nil {
				t.Fatalf("unmarshal FlowSample: %v", err)
			}
			if fs.ReceiveTimestamp == nil {
				t.Fatalf("ReceiveTimestamp is nil")
			}
			k := string(fs.FlowPayload)
			if _, ok := want[k]; !ok {
				t.Fatalf("unexpected FlowPayload (%d bytes)", len(fs.FlowPayload))
			}
			got[k] = struct{}{}
		}
		return len(got) == N
	}, 45*time.Second, 25*time.Millisecond)

	require.Equal(t, N, len(got))
}

func startRedpanda(t *testing.T, parent context.Context) []string {
	t.Helper()

	startCtx, cancel := context.WithTimeout(parent, 3*time.Minute)
	defer cancel()

	const maxAttempts = 5
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		rp, err := redpanda.Run(startCtx, "redpandadata/redpanda:v24.2.6")
		if err != nil {
			lastErr = err
			if isRetryableContainerStartErr(err) && attempt < maxAttempts {
				t.Logf("redpanda container start attempt %d failed: %v", attempt, err)
				time.Sleep(time.Duration(attempt) * time.Second)
				continue
			}
			require.NoError(t, err)
		}

		t.Cleanup(func() { _ = rp.Terminate(context.Background()) })

		broker, err := rp.KafkaSeedBroker(startCtx)
		if err != nil {
			lastErr = err
			if isRetryableContainerStartErr(err) && attempt < maxAttempts {
				t.Logf("redpanda broker fetch attempt %d failed: %v", attempt, err)
				_ = rp.Terminate(context.Background())
				time.Sleep(time.Duration(attempt) * time.Second)
				continue
			}
			require.NoError(t, err)
		}

		return []string{broker}
	}

	t.Fatalf("failed to start redpanda after %d attempts: %v", maxAttempts, lastErr)
	return nil
}

func isRetryableContainerStartErr(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "wait until ready") ||
		strings.Contains(s, "mapped port") ||
		strings.Contains(s, "timeout") ||
		strings.Contains(s, "context deadline exceeded") ||
		strings.Contains(s, "TLS handshake") ||
		strings.Contains(s, "connection refused") ||
		strings.Contains(s, "/containers/") && strings.Contains(s, "json") ||
		strings.Contains(s, "Get \"http")
}

func newKafkaClient(t *testing.T, ctx context.Context, brokers []string) *kafka.Client {
	t.Helper()
	kc, err := kafka.NewClient(ctx, &kafka.Config{Brokers: brokers, AuthIAM: false})
	require.NoError(t, err)
	t.Cleanup(kc.Close)
	return kc
}

func requireTopic(t *testing.T, ctx context.Context, kc *kafka.Client, topic string) {
	t.Helper()
	err := kc.EnsureTopic(ctx, topic, 1, 1)
	if err != nil {
		require.NoError(t, err)
	}
}

func newListeners(t *testing.T) (*net.UDPConn, net.Listener) {
	t.Helper()
	flowListener, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	require.NoError(t, err)
	t.Cleanup(func() { _ = flowListener.Close() })

	healthListener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = healthListener.Close() })

	return flowListener, healthListener
}

func newServer(t *testing.T, log *slog.Logger, flowListener *net.UDPConn, healthListener net.Listener, kc *kafka.Client, topic string) *server.Server {
	t.Helper()
	srv, err := server.New(&server.Config{
		Logger:         log,
		FlowListener:   flowListener,
		HealthListener: healthListener,
		KafkaClient:    kc,
		KafkaTopic:     topic,
		ReadTimeout:    100 * time.Millisecond,
		WorkerCount:    1,
	})
	require.NoError(t, err)
	return srv
}

func newConsumerNoGroup(t *testing.T, brokers []string, topic string) *kgo.Client {
	t.Helper()
	c, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ConsumeTopics(topic),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
	)
	require.NoError(t, err)
	return c
}

func pollOneValue(ctx context.Context, c *kgo.Client) ([]byte, bool) {
	pctx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
	defer cancel()

	fetches := c.PollFetches(pctx)
	if len(fetches.Errors()) > 0 {
		return nil, false
	}
	iter := fetches.RecordIter()
	if iter.Done() {
		return nil, false
	}
	r := iter.Next()
	v := make([]byte, len(r.Value))
	copy(v, r.Value)
	return v, true
}

func sendUDP(t *testing.T, dst *net.UDPAddr, payload []byte) {
	t.Helper()
	sender, err := net.DialUDP("udp", nil, dst)
	require.NoError(t, err)
	defer sender.Close()
	_, err = sender.Write(payload)
	require.NoError(t, err)
}

func newTestLogger() *slog.Logger {
	log := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{}))
	if os.Getenv("TEST_LOG") != "" {
		log = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
	return log
}

func buildSFlowV5DatagramWithFlowSample() []byte {
	return buildSFlowV5DatagramWithFlowSampleMarker(1)
}

func buildSFlowV5DatagramWithFlowSampleMarker(marker uint32) []byte {
	var out bytes.Buffer
	w32 := func(v uint32) { _ = binary.Write(&out, binary.BigEndian, v) }

	w32(5)
	w32(1)
	out.Write([]byte{1, 2, 3, 4})
	w32(0)
	w32(marker) // datagram sequence
	w32(100)
	w32(1)

	var sample bytes.Buffer
	sw32 := func(v uint32) { _ = binary.Write(&sample, binary.BigEndian, v) }

	sw32(marker) // sample sequence
	sw32(0)
	sw32(1000)
	sw32(1)
	sw32(0)
	sw32(0)
	sw32(0)
	sw32(1)

	var rec bytes.Buffer
	rw32 := func(v uint32) { _ = binary.Write(&rec, binary.BigEndian, v) }

	rw32(1)
	rw32(64)
	rw32(0)

	ethIPv4UDP := []byte{
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
		0x00, 0x11, 0x22, 0x33, 0x44, 0x55,
		0x08, 0x00,
		0x45, 0x00, 0x00, 0x1c, 0x00, 0x00, 0x00, 0x00, 0x40, 0x11, 0x00, 0x00,
		0x0a, 0x00, 0x00, 0x01,
		0x0a, 0x00, 0x00, 0x02,
		0x00, 0x35, 0x00, 0x35, 0x00, 0x08, 0x00, 0x00,
	}
	// Put marker into the last 4 bytes so each payload can be made unique.
	binary.BigEndian.PutUint32(ethIPv4UDP[len(ethIPv4UDP)-4:], marker)

	rw32(uint32(len(ethIPv4UDP)))
	rec.Write(ethIPv4UDP)
	for rec.Len()%4 != 0 {
		rec.WriteByte(0)
	}

	sw32(1)                 // record header: format=1, enterprise=0
	sw32(uint32(rec.Len())) // record length
	sample.Write(rec.Bytes())

	w32(1) // flow sample
	w32(uint32(sample.Len()))
	out.Write(sample.Bytes())

	return out.Bytes()
}

func buildSFlowV5DatagramWithNoSamples() []byte {
	var out bytes.Buffer
	w32 := func(v uint32) { _ = binary.Write(&out, binary.BigEndian, v) }

	w32(5)
	w32(1)
	out.Write([]byte{1, 2, 3, 4})
	w32(0)
	w32(1)
	w32(100)
	w32(0)

	return out.Bytes()
}

func sanitizeTopicSuffix(s string) string {
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			b = append(b, c)
		} else {
			b = append(b, '-')
		}
	}
	return string(b)
}
