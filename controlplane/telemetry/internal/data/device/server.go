package data

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/data/stats"
)

var (
	ErrInvalidEnvironment = fmt.Errorf("invalid environment")
)

const (
	EnvTestnet = "testnet"
	EnvDevnet  = "devnet"

	DefaultMaxPoints = 1000
)

type Server struct {
	log     *slog.Logger
	Mux     *http.ServeMux
	devnet  Provider
	testnet Provider
}

func NewServer(log *slog.Logger, testnetProvider, devnetProvider Provider) (*Server, error) {
	s := &Server{
		log:     log,
		Mux:     http.NewServeMux(),
		testnet: testnetProvider,
		devnet:  devnetProvider,
	}
	s.registerRoutes()
	return s, nil
}

func (s *Server) provider(env string) (Provider, error) {
	switch env {
	case EnvTestnet:
		return s.testnet, nil
	case EnvDevnet:
		return s.devnet, nil
	default:
		return nil, ErrInvalidEnvironment
	}
}

func (s *Server) registerRoutes() {
	s.Mux.HandleFunc("/device-link/circuits", s.handleDeviceCircuits)
	s.Mux.HandleFunc("/device-link/circuit-latencies", s.handleDeviceCircuitLatencies)
}

func (s *Server) handleDeviceCircuits(w http.ResponseWriter, r *http.Request) {
	env := r.URL.Query().Get("env")
	s.log.Debug("[/device-link/circuits]", "env", env, "full", r.URL.String())

	provider, err := s.provider(env)
	if err != nil {
		s.log.Warn("invalid environment", "env", env)
		http.Error(w, fmt.Sprintf("invalid environment %q", env), http.StatusBadRequest)
		return
	}

	circuits, err := provider.GetCircuits(r.Context())
	if err != nil {
		s.log.Error("failed to get circuits", "error", err)
		http.Error(w, fmt.Sprintf("failed to get circuits: %v", err), http.StatusInternalServerError)
		return
	}

	if err := json.NewEncoder(w).Encode(circuits); err != nil {
		s.log.Error("failed to encode circuits", "error", err)
		http.Error(w, fmt.Sprintf("failed to encode circuits: %v", err), http.StatusInternalServerError)
		return
	}
}

func (s *Server) handleDeviceCircuitLatencies(w http.ResponseWriter, r *http.Request) {
	env := r.URL.Query().Get("env")
	fromStr := r.URL.Query().Get("from")
	toStr := r.URL.Query().Get("to")
	circuits := parseMultiParam(r, "circuit")
	maxPointsStr := r.URL.Query().Get("max_points")
	unit := r.URL.Query().Get("unit")

	s.log.Debug("[/device-link/circuit-latencies]", "env", env, "from", fromStr, "to", toStr, "circuits", circuits, "max_points", maxPointsStr, "unit", unit, "full", r.URL.String())

	provider, err := s.provider(env)
	if err != nil {
		s.log.Warn("invalid environment", "env", env)
		http.Error(w, fmt.Sprintf("invalid environment %q", env), http.StatusBadRequest)
		return
	}

	if unit == "" {
		unit = string(UnitMicrosecond)
	}
	switch Unit(unit) {
	case UnitMillisecond, UnitMicrosecond:
	default:
		http.Error(w, "invalid unit (must be ms or us)", http.StatusBadRequest)
		return
	}

	fromTime, errFrom := time.Parse(time.RFC3339, fromStr)
	toTime, errTo := time.Parse(time.RFC3339, toStr)
	if errFrom != nil || errTo != nil {
		s.log.Warn("invalid from/to", "from", fromStr, "to", toStr)
		http.Error(w, "invalid from/to", http.StatusBadRequest)
		return
	}

	var maxPoints uint64
	if maxPointsStr == "" {
		maxPoints = DefaultMaxPoints
	} else {
		maxPoints, err = strconv.ParseUint(maxPointsStr, 10, 32)
		if err != nil || maxPoints == 0 {
			s.log.Warn("invalid max_points", "max_points", maxPointsStr)
			http.Error(w, "invalid max_points", http.StatusBadRequest)
			return
		}
	}

	output := []stats.CircuitLatencyStat{}
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, circuitCode := range circuits {
		wg.Add(1)
		go func(circuitCode string) {
			defer wg.Done()
			series, err := provider.GetCircuitLatencies(r.Context(), GetCircuitLatenciesConfig{
				Circuit: circuitCode,
				Time: &TimeRange{
					From: fromTime,
					To:   toTime,
				},
				MaxPoints: maxPoints,
				Unit:      Unit(unit),
			})
			if err != nil {
				s.log.Warn("failed to get circuit latencies", "error", err, "circuit", circuitCode)
				return
			}
			mu.Lock()
			output = append(output, series...)
			mu.Unlock()
		}(circuitCode)
	}
	wg.Wait()

	sort.Slice(output, func(i, j int) bool {
		return output[i].Timestamp < output[j].Timestamp
	})

	if err := json.NewEncoder(w).Encode(output); err != nil {
		s.log.Error("failed to encode latencies", "error", err)
		http.Error(w, fmt.Sprintf("failed to encode latencies: %v", err), http.StatusInternalServerError)
		return
	}
}

func parseMultiParam(r *http.Request, name string) []string {
	valueStr := strings.Trim(r.URL.Query().Get(name), "{}")
	values := strings.Split(valueStr, ",")
	params := []string{}
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		params = append(params, value)
	}
	return params
}
