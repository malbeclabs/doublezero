package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/malbeclabs/doublezero/telemetry/flow-ingest/internal/metrics"
	flowproto "github.com/malbeclabs/doublezero/telemetry/proto/flow/gen/pb-go"
	"github.com/netsampler/goflow2/v2/decoders/sflow"
	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	defaultReadTimeout       = 250 * time.Millisecond
	defaultWorkerCount       = 0 // 0 => runtime.NumCPU()
	defaultBufferSizePackets = 1024
	defaultBufferSizeBytes   = 65535

	// Health endpoint: best-effort backoff (does not kill ingest on transient failures).
	healthBaseBackoff = 50 * time.Millisecond
	healthMaxBackoff  = 2 * time.Second

	// Non-timeout UDP read errors: keep running but avoid tight loops.
	readErrBackoff = 10 * time.Millisecond
)

type KafkaClient interface {
	Produce(ctx context.Context, record *kgo.Record, fn func(*kgo.Record, error))
}

type Server struct {
	log *slog.Logger
	cfg *Config
}

type packet struct {
	addr *net.UDPAddr
	data []byte
}

func New(cfg *Config) (*Server, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("failed to validate config: %w", err)
	}
	return &Server{log: cfg.Logger, cfg: cfg}, nil
}

func (s *Server) Start(ctx context.Context, cancel context.CancelFunc) <-chan error {
	errCh := make(chan error, 1)
	go func() {
		defer close(errCh)
		if err := s.Run(ctx); err != nil {
			errCh <- err
			cancel()
		}
	}()
	return errCh
}

func (s *Server) Run(parentCtx context.Context) error {
	s.log.Info("starting flow ingest server",
		"flowListener", s.cfg.FlowListener.LocalAddr().String(),
		"healthListener", s.cfg.HealthListener.Addr().String(),
		"kafkaTopic", s.cfg.KafkaTopic,
		"readTimeout", s.cfg.ReadTimeout,
		"workerCount", s.cfg.WorkerCount,
		"bufferSizePackets", s.cfg.BufferSizePackets,
		"bufferSizeBytes", s.cfg.BufferSizeBytes,
	)

	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	go func() {
		<-ctx.Done()
		if s.cfg.HealthListener != nil {
			_ = s.cfg.HealthListener.Close()
		}
		if s.cfg.FlowListener != nil {
			_ = s.cfg.FlowListener.Close()
		}
	}()

	packets := make(chan packet, s.cfg.BufferSizePackets)

	var workers sync.WaitGroup
	for i := 0; i < s.cfg.WorkerCount; i++ {
		workers.Add(1)
		go func(id int) {
			defer workers.Done()
			s.ingestWorker(ctx, id, packets)
		}(i)
	}

	errCh := make(chan error, 2)
	go func() { errCh <- s.healthLoop(ctx) }()
	go func() { errCh <- s.readLoop(ctx, packets) }()

	e1 := <-errCh
	cancel()
	e2 := <-errCh

	close(packets)
	workers.Wait()

	if e1 != nil {
		return e1
	}
	if e2 != nil {
		return e2
	}
	s.log.Info("server stopped")
	return nil
}

func (s *Server) readLoop(ctx context.Context, out chan<- packet) error {
	buf := make([]byte, s.cfg.BufferSizeBytes)

	for {
		metrics.PacketQueueDepth.Set(float64(len(out)))

		if err := s.cfg.FlowListener.SetReadDeadline(s.cfg.Clock.Now().Add(s.cfg.ReadTimeout)); err != nil {
			if isClosedNetErr(err) {
				metrics.UDPSetDeadlineErrs.WithLabelValues("closed").Inc()
			} else {
				metrics.UDPSetDeadlineErrs.WithLabelValues("other").Inc()
			}
			if ctx.Err() != nil {
				return nil
			}
			if isClosedNetErr(err) {
				s.log.Debug("flow listener closed on set read deadline, exiting", "error", err)
				return nil
			}
			return fmt.Errorf("set read deadline failed: %w", err)
		}

		n, remote, err := s.cfg.FlowListener.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if isClosedNetErr(err) {
				s.log.Debug("flow listener closed on read from udp, exiting", "error", err)
				metrics.UDPReadErrs.WithLabelValues("closed").Inc()
				return nil
			}
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				metrics.UDPReadErrs.WithLabelValues("timeout").Inc()
				continue
			}
			metrics.UDPReadErrs.WithLabelValues("other").Inc()
			s.log.Warn("read error", "error", err)
			select {
			case <-s.cfg.Clock.After(readErrBackoff):
			case <-ctx.Done():
				return nil
			}
			continue
		}

		metrics.UDPPackets.Inc()
		metrics.UDPBytes.Add(float64(n))

		data := make([]byte, n)
		copy(data, buf[:n])

		select {
		case out <- packet{addr: remote, data: data}:
		case <-ctx.Done():
			return nil
		}
	}
}

func (s *Server) ingestWorker(ctx context.Context, id int, in <-chan packet) {
	metrics.WorkersRunning.Inc()
	defer metrics.WorkersRunning.Dec()

	s.log.Info("worker started", "worker", id)
	for {
		select {
		case <-ctx.Done():
			s.log.Info("worker exiting on context cancel", "worker", id)
			return
		case p, ok := <-in:
			if !ok {
				s.log.Info("worker channel closed, exiting", "worker", id)
				return
			}
			s.ingestPacket(ctx, id, p)
		}
	}
}

func (s *Server) ingestPacket(ctx context.Context, workerID int, p packet) {
	var msg sflow.Packet
	if err := sflow.DecodeMessageVersion(bytes.NewBuffer(p.data), &msg); err != nil {
		metrics.FlowDecodeErrs.Inc()
		s.log.Error("sflow decode error", "error", err, "worker", workerID)
		return
	}

	hasFlowSample := slices.ContainsFunc(msg.Samples, func(s any) bool {
		switch s.(type) {
		case sflow.FlowSample, sflow.ExpandedFlowSample:
			return true
		}
		return false
	})
	if !hasFlowSample {
		metrics.PacketsWithoutFlowSample.Inc()
		return
	}
	metrics.PacketsWithFlowSample.Inc()

	now := s.cfg.Clock.Now().UTC()
	sample := &flowproto.FlowSample{
		ReceiveTimestamp: timestamppb.New(now),
		FlowPayload:      p.data,
	}

	payload, err := proto.Marshal(sample)
	if err != nil {
		s.log.Error("proto marshal error", "error", err, "worker", workerID)
		return
	}

	rec := &kgo.Record{Topic: s.cfg.KafkaTopic, Value: payload}
	metrics.FlowKafkaProduceInflight.Inc()
	s.cfg.KafkaClient.Produce(ctx, rec, func(r *kgo.Record, err error) {
		defer metrics.FlowKafkaProduceInflight.Dec()
		if err != nil {
			metrics.FlowKafkaProduceOutcomes.WithLabelValues("error").Inc()
			s.log.Error("failed to produce record",
				"error", err, "worker", workerID,
				"topic", r.Topic, "partition", r.Partition, "offset", r.Offset,
			)
			return
		}
		metrics.FlowKafkaProduceOutcomes.WithLabelValues("ok").Inc()
		s.log.Debug("produced record",
			"worker", workerID, "topic", r.Topic, "partition", r.Partition, "offset", r.Offset,
		)
	})

	s.log.Debug("ingested sflow packet", "worker", workerID, "source", p.addr.String())
}

func (s *Server) healthLoop(ctx context.Context) error {
	backoff := healthBaseBackoff
	for {
		c, err := s.cfg.HealthListener.Accept()
		if err == nil {
			metrics.HealthAccept.Inc()
			backoff = healthBaseBackoff
			_ = c.Close()
			continue
		}

		if ctx.Err() != nil {
			return nil
		}

		closed := isClosedNetErr(err)
		metrics.HealthAcceptErrs.WithLabelValues(map[bool]string{true: "closed", false: "other"}[closed]).Inc()
		s.log.Warn("health accept error; continuing", "error", err, "closedClassified", closed, "backoff", backoff)

		select {
		case <-s.cfg.Clock.After(backoff):
		case <-ctx.Done():
			return nil
		}
		backoff *= 2
		if backoff > healthMaxBackoff {
			backoff = healthMaxBackoff
		}
	}
}

func isClosedNetErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, net.ErrClosed) {
		return true
	}
	if errors.Is(err, io.EOF) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "use of closed network connection") ||
		strings.Contains(msg, "bad file descriptor")
}
