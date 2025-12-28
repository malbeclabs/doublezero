// Package server provides the gRPC server for the multicast relay service.
package server

import (
	"fmt"
	"log/slog"
	"net"
	"sync/atomic"

	"github.com/malbeclabs/doublezero/mcastrelay/internal/multicast"
	pb "github.com/malbeclabs/doublezero/mcastrelay/proto/relay/gen/pb-go"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Server is the gRPC server that relays multicast packets to subscribers.
type Server struct {
	pb.UnimplementedRelayServiceServer

	log           *slog.Logger
	listener      *multicast.Listener
	grpc          *grpc.Server
	channelBuffer int

	subscriberCount atomic.Int64
}

// Config holds configuration for the gRPC server.
type Config struct {
	Logger        *slog.Logger
	Listener      *multicast.Listener
	ChannelBuffer int // Buffer size for per-subscriber channels
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Logger:        slog.Default(),
		ChannelBuffer: 256,
	}
}

// New creates a new gRPC relay server.
func New(cfg *Config) (*Server, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Listener == nil {
		return nil, fmt.Errorf("multicast listener is required")
	}
	if cfg.ChannelBuffer <= 0 {
		cfg.ChannelBuffer = 256
	}

	s := &Server{
		log:           cfg.Logger,
		listener:      cfg.Listener,
		grpc:          grpc.NewServer(),
		channelBuffer: cfg.ChannelBuffer,
	}

	pb.RegisterRelayServiceServer(s.grpc, s)

	return s, nil
}

// Serve starts the gRPC server. It blocks until the server is stopped.
func (s *Server) Serve(lis net.Listener) error {
	s.log.Info("gRPC server starting", "address", lis.Addr().String())
	return s.grpc.Serve(lis)
}

// Stop gracefully stops the gRPC server.
func (s *Server) Stop() {
	s.log.Info("gRPC server stopping")
	s.grpc.GracefulStop()
}

// SubscriberCount returns the current number of active subscribers.
func (s *Server) SubscriberCount() int64 {
	return s.subscriberCount.Load()
}

// Subscribe implements the RelayService Subscribe RPC.
// It streams multicast packets to the client until the client disconnects
// or the server shuts down.
func (s *Server) Subscribe(req *pb.SubscribeRequest, stream pb.RelayService_SubscribeServer) error {
	ctx := stream.Context()

	// Create a buffered channel for this subscriber
	packets := make(chan multicast.Packet, s.channelBuffer)

	// Subscribe to the multicast listener
	unsubscribe := s.listener.Subscribe(packets)
	defer unsubscribe()

	s.subscriberCount.Add(1)
	defer s.subscriberCount.Add(-1)

	s.log.Info("client subscribed", "subscribers", s.subscriberCount.Load())

	for {
		select {
		case <-ctx.Done():
			s.log.Info("client disconnected", "error", ctx.Err())
			return nil
		case pkt, ok := <-packets:
			if !ok {
				return nil
			}

			msg := &pb.PayloadMessage{
				Payload:    pkt.Data,
				ReceivedAt: timestamppb.New(pkt.ReceivedAt),
			}

			if err := stream.Send(msg); err != nil {
				s.log.Error("failed to send message", "error", err)
				return err
			}
		}
	}
}

// GRPCServer returns the underlying grpc.Server for testing or custom configuration.
func (s *Server) GRPCServer() *grpc.Server {
	return s.grpc
}
