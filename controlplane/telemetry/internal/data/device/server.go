package data

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/malbeclabs/doublezero/config"
	datajson "github.com/malbeclabs/doublezero/controlplane/telemetry/internal/data/json"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/data/stats"
)

var (
	ErrInvalidEnvironment = fmt.Errorf("invalid environment")
)

const (
	DefaultMaxPoints = 1000
)

type Server struct {
	log     *slog.Logger
	Mux     *http.ServeMux
	devnet  Provider
	testnet Provider
	mainnet Provider
}

func NewServer(log *slog.Logger, mainnetProvider, testnetProvider, devnetProvider Provider) (*Server, error) {
	s := &Server{
		log:     log,
		Mux:     http.NewServeMux(),
		mainnet: mainnetProvider,
		testnet: testnetProvider,
		devnet:  devnetProvider,
	}
	s.registerRoutes()
	return s, nil
}

func (s *Server) provider(env string) (Provider, error) {
	switch env {
	case config.EnvMainnetBeta:
		return s.mainnet, nil
	case config.EnvMainnet:
		return s.mainnet, nil
	case config.EnvTestnet:
		return s.testnet, nil
	case config.EnvDevnet:
		return s.devnet, nil
	default:
		return nil, ErrInvalidEnvironment
	}
}

func (s *Server) registerRoutes() {
	s.Mux.HandleFunc("/device-link/circuits", s.handleDeviceCircuits)
	s.Mux.HandleFunc("/device-link/link-types", s.handleDeviceLinkTypes)
	s.Mux.HandleFunc("/device-link/circuit-latencies", s.handleDeviceCircuitLatencies)
	s.Mux.HandleFunc("/device-link/summary", s.handlSummary)
}

func (s *Server) handleDeviceLinkTypes(w http.ResponseWriter, r *http.Request) {
	env := r.URL.Query().Get("env")
	s.log.Debug("[/device-link/link-types]", "env", env, "full", r.URL.String())

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

	linkTypesMap := map[string]struct{}{}
	for _, circuit := range circuits {
		linkTypesMap[circuit.Link.LinkType] = struct{}{}
	}

	linkTypes := make([]string, 0, len(linkTypesMap))
	for linkType := range linkTypesMap {
		linkTypes = append(linkTypes, linkType)
	}

	sort.Strings(linkTypes)

	if err := json.NewEncoder(w).Encode(linkTypes); err != nil {
		s.log.Error("failed to encode link types", "error", err)
		http.Error(w, fmt.Sprintf("failed to encode link types: %v", err), http.StatusInternalServerError)
		return
	}
}

func (s *Server) handleDeviceCircuits(w http.ResponseWriter, r *http.Request) {
	env := r.URL.Query().Get("env")
	linkTypes := parseMultiParam(r, "link_type")
	s.log.Debug("[/device-link/circuits]", "env", env, "link_types", linkTypes, "full", r.URL.String())

	// Convert link types to lowercase.
	for i, linkType := range linkTypes {
		linkTypes[i] = strings.ToLower(linkType)
	}

	if len(linkTypes) == 1 && linkTypes[0] == "all" {
		linkTypes = nil
	}

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

	// Filter by link type, if provided.
	if len(linkTypes) > 0 {
		filteredCircuits := make([]Circuit, 0, len(circuits))
		for _, circuit := range circuits {
			if slices.Contains(linkTypes, strings.ToLower(circuit.Link.LinkType)) {
				filteredCircuits = append(filteredCircuits, circuit)
			}
		}
		circuits = filteredCircuits
	}

	if err := json.NewEncoder(w).Encode(circuits); err != nil {
		s.log.Error("failed to encode circuits", "error", err)
		http.Error(w, fmt.Sprintf("failed to encode circuits: %v", err), http.StatusInternalServerError)
		return
	}
}

func (s *Server) handlSummary(w http.ResponseWriter, r *http.Request) {
	env := r.URL.Query().Get("env")
	fromStr := r.URL.Query().Get("from")
	toStr := r.URL.Query().Get("to")
	circuitCodes := parseMultiParam(r, "circuit")
	unit := r.URL.Query().Get("unit")
	linkTypes := parseMultiParam(r, "link_type")
	s.log.Debug("[/device-link/summary]", "env", env, "from", fromStr, "to", toStr, "circuit_codes", circuitCodes, "unit", unit, "link_types", linkTypes, "full", r.URL.String())

	provider, err := s.provider(env)
	if err != nil {
		s.log.Warn("invalid environment", "env", env)
		http.Error(w, fmt.Sprintf("invalid environment %q", env), http.StatusBadRequest)
		return
	}

	// Convert link types to lowercase.
	for i, linkType := range linkTypes {
		linkTypes[i] = strings.ToLower(linkType)
	}
	if len(linkTypes) == 1 && linkTypes[0] == "all" {
		linkTypes = nil
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

	allCircuits, err := provider.GetCircuits(r.Context())
	if err != nil {
		s.log.Error("failed to get circuits", "error", err)
		http.Error(w, fmt.Sprintf("failed to get circuits: %v", err), http.StatusInternalServerError)
		return
	}

	// Filter by link type, if provided.
	if len(linkTypes) > 0 {
		filteredCircuits := make([]Circuit, 0, len(allCircuits))
		for _, circuit := range allCircuits {
			if slices.Contains(linkTypes, strings.ToLower(circuit.Link.LinkType)) {
				filteredCircuits = append(filteredCircuits, circuit)
			}
		}
		allCircuits = filteredCircuits
	}

	if len(circuitCodes) == 0 || (len(circuitCodes) == 1 && circuitCodes[0] == "all") {
		circuitCodes = make([]string, 0, len(allCircuits))
		for _, circuit := range allCircuits {
			circuitCodes = append(circuitCodes, circuit.Code)
		}
	}

	output, err := provider.GetSummaryForCircuits(r.Context(), GetSummaryForCircuitsConfig{
		Circuits: circuitCodes,
		Time:     &TimeRange{From: fromTime, To: toTime},
		Unit:     Unit(unit),
	})
	if err != nil {
		s.log.Error("failed to get summary for circuits", "error", err)
		http.Error(w, fmt.Sprintf("failed to get summary for circuits: %v", err), http.StatusInternalServerError)
		return
	}

	encoder := json.NewEncoder(w)
	if err := encoder.Encode(output); err != nil {
		s.log.Error("failed to encode latencies", "error", err)
		http.Error(w, fmt.Sprintf("failed to encode latencies: %v", err), http.StatusInternalServerError)
		return
	}
}

func (s *Server) handleDeviceCircuitLatencies(w http.ResponseWriter, r *http.Request) {
	env := r.URL.Query().Get("env")
	fromStr := r.URL.Query().Get("from")
	toStr := r.URL.Query().Get("to")
	circuitCodes := parseMultiParam(r, "circuit")
	maxPointsStr := r.URL.Query().Get("max_points")
	intervalStr := r.URL.Query().Get("interval")
	unit := r.URL.Query().Get("unit")
	metrics := parseMultiParam(r, "metrics")
	partStr := r.URL.Query().Get("partition")
	totalPartsStr := r.URL.Query().Get("total_partitions")
	linkTypes := parseMultiParam(r, "link_type")

	s.log.Debug("[/device-link/circuit-latencies]", "env", env, "from", fromStr, "to", toStr, "circuits", circuitCodes, "max_points", maxPointsStr, "interval", intervalStr, "unit", unit, "partition", partStr, "total_partitions", totalPartsStr, "link_types", linkTypes, "full", r.URL.String(), "metrics", metrics)

	provider, err := s.provider(env)
	if err != nil {
		s.log.Warn("invalid environment", "env", env)
		http.Error(w, fmt.Sprintf("invalid environment %q", env), http.StatusBadRequest)
		return
	}

	// Convert link types to lowercase.
	for i, linkType := range linkTypes {
		linkTypes[i] = strings.ToLower(linkType)
	}
	if len(linkTypes) == 1 && linkTypes[0] == "all" {
		linkTypes = nil
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

	if intervalStr != "" && maxPointsStr != "" {
		http.Error(w, "interval and max_points cannot be set at the same time", http.StatusBadRequest)
		return
	}

	var interval time.Duration
	var maxPoints uint64
	if intervalStr != "" {
		interval, err = time.ParseDuration(intervalStr)
		if err != nil {
			s.log.Warn("invalid interval", "interval", intervalStr)
			http.Error(w, "invalid interval", http.StatusBadRequest)
			return
		}
	} else {
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
	}

	allCircuits, err := provider.GetCircuits(r.Context())
	if err != nil {
		s.log.Error("failed to get circuits", "error", err)
		http.Error(w, fmt.Sprintf("failed to get circuits: %v", err), http.StatusInternalServerError)
		return
	}

	// Filter by link type, if provided.
	if len(linkTypes) > 0 {
		filteredCircuits := make([]Circuit, 0, len(allCircuits))
		for _, circuit := range allCircuits {
			if len(linkTypes) == 0 || slices.Contains(linkTypes, strings.ToLower(circuit.Link.LinkType)) {
				filteredCircuits = append(filteredCircuits, circuit)
			}
		}
		allCircuits = filteredCircuits
	}

	if len(circuitCodes) == 0 || (len(circuitCodes) == 1 && circuitCodes[0] == "all") {
		circuitCodes = make([]string, 0, len(allCircuits))
		for _, circuit := range allCircuits {
			circuitCodes = append(circuitCodes, circuit.Code)
		}
	}

	var partOutOfRange bool
	if (partStr != "") != (totalPartsStr != "") {
		http.Error(w, "both partition and total_partitions must be provided together", http.StatusBadRequest)
		return
	}
	if partStr != "" && totalPartsStr != "" {
		part, err1 := strconv.Atoi(partStr)
		tparts, err2 := strconv.Atoi(totalPartsStr)
		if err1 != nil || err2 != nil || tparts <= 0 || part < 0 || part >= tparts {
			http.Error(w, "invalid partition/total_partitions", http.StatusBadRequest)
			return
		}
		sort.Strings(circuitCodes)
		n := len(circuitCodes)
		base := n / tparts
		rem := n % tparts
		start := part*base + min(part, rem)
		size := base
		if part < rem {
			size++
		}
		end := start + size
		if part >= len(circuitCodes) {
			// If the current partition is greater than or equal to the number of circuits, due to a bug
			// in Grafana, we need to return 1 entry in the output with the expected format, so we will
			// return the first entry from the first circuit.
			// This is an ugly hack that we can remove once Grafana fixes the bug.
			// // https://github.com/grafana/grafana-infinity-datasource/issues/705
			partOutOfRange = true
			if len(circuitCodes) > 0 {
				circuitCodes = []string{circuitCodes[0]}
			}
		} else {
			if start > n {
				circuitCodes = nil
			} else {
				if end > n {
					end = n
				}
				circuitCodes = circuitCodes[start:end]
			}
		}
	}

	output := []stats.CircuitLatencyStat{}
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, circuitCode := range circuitCodes {
		wg.Add(1)
		go func(circuitCode string) {
			defer wg.Done()
			series, err := provider.GetCircuitLatencies(r.Context(), GetCircuitLatenciesConfig{
				Circuit:   circuitCode,
				Time:      &TimeRange{From: fromTime, To: toTime},
				Interval:  interval,
				MaxPoints: maxPoints,
				Unit:      Unit(unit),
			})
			if err != nil {
				s.log.Warn("failed to get circuit latencies", "error", err, "circuit", circuitCode)
				return
			}
			s.log.Debug("Got circuit latencies", "circuit", circuitCode, "series", len(series))
			if len(series) == 0 {
				return
			}
			mu.Lock()
			if partOutOfRange {
				// If the current partition is greater than or equal to the number of circuits, due
				// to a bug in Grafana, we need to return 1 entry in the output with the expected
				// format, so we will return the first entry from the first circuit.
				// This is an ugly hack that we can remove once Grafana fixes the bug.
				// https://github.com/grafana/grafana-infinity-datasource/issues/705
				output = append(output, series[0])
			} else {
				output = append(output, series...)
			}
			mu.Unlock()
		}(circuitCode)
	}
	wg.Wait()

	sort.Slice(output, func(i, j int) bool { return output[i].Timestamp < output[j].Timestamp })

	var encoder datajson.Encoder
	if len(metrics) > 0 {
		if !slices.Contains(metrics, "timestamp") {
			metrics = append([]string{"timestamp"}, metrics...)
		}
		if !slices.Contains(metrics, "circuit") {
			metrics = append([]string{"circuit"}, metrics...)
		}
		encoder = datajson.NewFieldFilteringEncoder(w, metrics)
	} else {
		encoder = json.NewEncoder(w)
	}

	if err := encoder.Encode(output); err != nil {
		s.log.Error("failed to encode latencies", "error", err)
		http.Error(w, fmt.Sprintf("failed to encode latencies: %v", err), http.StatusInternalServerError)
		return
	}
}

func parseMultiParam(r *http.Request, name string) []string {
	raw := strings.Trim(r.URL.Query().Get(name), "{}")
	seen := make(map[string]struct{})
	out := []string{}
	for _, v := range strings.Split(raw, ",") {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
