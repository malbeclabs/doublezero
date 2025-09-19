package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

const (
	initialSupply = 10_000_000_000
)

type RpcClient interface {
	GetTotalSupply(ctx context.Context) (float64, error)
}

type ApiServer struct {
	totalSupply     float64
	estimatedSupply map[string]float64 // date -> estimated supply
	rpcClient       RpcClient
	httpServer      *http.Server
	shutdownCh      chan struct{}
	logger          *slog.Logger
	mu              sync.RWMutex
	listenAddr      string
}

type Option func(*ApiServer)

// WithRpcClient sets the solana RPC client for the ApiServer.
func WithRpcClient(client RpcClient) Option {
	return func(s *ApiServer) {
		s.rpcClient = client
	}
}

// WithEstimatedSupply sets the estimated supply by day. The map keys are dates in "YYYY-MM-DD" format.
func WithEstimatedSupply(supplyMap map[string]float64) Option {
	return func(s *ApiServer) {
		s.estimatedSupply = supplyMap
	}
}

func WithLogger(logger *slog.Logger) Option {
	return func(s *ApiServer) {
		s.logger = logger
	}
}

func WithListenAddr(addr string) Option {
	return func(s *ApiServer) {
		s.listenAddr = addr
	}
}

func NewApiServer(opts ...Option) (*ApiServer, error) {
	s := &ApiServer{
		totalSupply:     0,
		estimatedSupply: make(map[string]float64),
		shutdownCh:      make(chan struct{}),
		logger:          slog.Default(),
		listenAddr:      ":8080", // Default listen address
	}
	for _, opt := range opts {
		opt(s)
	}
	if s.rpcClient == nil {
		return nil, fmt.Errorf("RpcClient is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	totalSupply, err := s.rpcClient.GetTotalSupply(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get total supply from RPC: %w", err)
	}
	s.totalSupply = totalSupply
	return s, nil
}

// handleGetCirculatingSupply handles HTTP requests to get the circulating supply.
func (s *ApiServer) handleGetCirculatingSupply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	supply, err := s.GetCirculatingSupply()
	if err != nil {
		s.logger.Error("Error getting circulating supply", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprintf(w, "%.1f", supply)
}

// GetCirculatingSupply calculates and returns the current circulating supply.
func (s *ApiServer) GetCirculatingSupply() (float64, error) {
	date := time.Now().Format("2006-01-02")
	estimatedSupply, ok := s.estimatedSupply[date]
	if !ok {
		return 0, fmt.Errorf("estimated supply for date %s not found", date)
	}

	s.mu.RLock()
	totalSupply := s.totalSupply
	s.mu.RUnlock()

	circulatingSupply := estimatedSupply + (totalSupply - initialSupply)
	return circulatingSupply, nil
}

func (s *ApiServer) Run() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/supply", s.handleGetCirculatingSupply)

	s.httpServer = &http.Server{
		Addr:    s.listenAddr,
		Handler: mux,
	}

	// periodically fetch total supply from Solana RPC and update s.totalSupply
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				totalSupply, err := s.rpcClient.GetTotalSupply(ctx)
				cancel()

				if err != nil {
					s.logger.Error("Error fetching total supply from RPC", "error", err)
					continue
				}
				s.mu.Lock()
				s.totalSupply = totalSupply
				s.mu.Unlock()
				s.logger.Info("Updated total supply", "supply", totalSupply)
			case <-s.shutdownCh:
				s.logger.Info("Stopping periodic total supply fetcher.")
				return
			}
		}
	}()

	s.logger.Info("API server starting", "address", s.httpServer.Addr)
	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("could not listen on %s: %w", s.httpServer.Addr, err)
	}
	return nil
}

func (s *ApiServer) Shutdown() error {
	s.logger.Info("Shutting down server...")
	close(s.shutdownCh)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return s.httpServer.Shutdown(ctx)
}
