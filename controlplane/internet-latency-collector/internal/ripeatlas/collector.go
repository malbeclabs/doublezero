package ripeatlas

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/internal/collector"
	"github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/internal/exporter"
	"github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/internal/metrics"
)

const (
	TimestampFileName = "ripe_atlas_timestamps.json"
)

// CallDelay is defined in client.go to avoid duplication

type clientInterface interface {
	GetProbesInRadius(ctx context.Context, latitude, longitude float64, radiusKm int) ([]Probe, error)
	GetProbesForLocations(ctx context.Context, locations []LocationProbeMatch) ([]LocationProbeMatch, error)
	CreateMeasurement(ctx context.Context, request MeasurementRequest) (*MeasurementResponse, error)
	GetAllMeasurements(ctx context.Context, env string) ([]Measurement, error)
	GetMeasurementResultsIncremental(ctx context.Context, measurementID int, startTimestamp int64) ([]any, error)
	StopMeasurement(ctx context.Context, measurementID int) error
	GetCreditBalance(ctx context.Context) (float64, error)
}

type LocationProbeMatch struct {
	collector.LocationMatch
	NearbyProbes []Probe
	ProbeCount   int
}

type ProbeDistance struct {
	Probe    Probe
	Distance float64
}

type MeasurementInfo struct {
	ID          int
	SourceProbe int // The ID of the measurement's "source" probe (the probe associated with location_a)
	TargetProbe int // The ID of the measurement's "target" probe (the probe associated with location_z)
	LocationA   string
	LocationZ   string
	Description string
	Target      string
	Status      string
}

type Collector struct {
	client           clientInterface
	log              *slog.Logger
	exporter         exporter.Exporter
	getLocationsFunc func(ctx context.Context) []collector.LocationMatch
	env              string
	probeToLocation  map[int]string // Maps probe IDs to location codes
	mu               sync.RWMutex   // Protects probeToLocation map
}

type MeasurementSpec struct {
	TargetLocation     string
	TargetLocationCode string
	TargetProbe        Probe
	SourceSpecs        []SourceSpec
}

type SourceSpec struct {
	LocationCode string
	Probe        Probe
}

func NewCollector(logger *slog.Logger, exporter exporter.Exporter, env string, getLocationsFunc func(ctx context.Context) []collector.LocationMatch) *Collector {
	return &Collector{
		client:           NewClient(logger),
		log:              logger,
		exporter:         exporter,
		getLocationsFunc: getLocationsFunc,
		env:              env,
		probeToLocation:  make(map[int]string),
	}
}

func (c *Collector) InitializeCreditBalance(ctx context.Context) error {
	balance, err := c.client.GetCreditBalance(ctx)
	if err != nil {
		return fmt.Errorf("failed to get RIPE Atlas credit balance: %w", err)
	}

	metrics.RipeatlasCreditBalance.Set(balance)
	c.log.Info("Initialized RIPE Atlas credit balance metric", slog.Float64("balance", balance))

	return nil
}

func (c *Collector) InitializeMeasurementMetrics(stateDir string) error {
	timestampFile := filepath.Join(stateDir, TimestampFileName)
	measurementState := NewMeasurementState(timestampFile)

	if err := measurementState.Load(); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("failed to load measurement state: %w", err)
		}
		// File doesn't exist yet, no measurements to track
		metrics.RipeatlasTotalMeasurements.Set(0)
		metrics.RipeatlasProbesPerMeasurement.Set(0)
		metrics.RipeatlasExpectedDailyCredits.Set(0)
		metrics.RipeatlasExpectedDailyResults.Set(0)
		c.log.Info("No measurement state file found, initializing metrics to zero")
		return nil
	}

	// Count total measurements and determine probes per location
	totalMeasurements := 0
	probesPerLocation := 0
	for _, meta := range measurementState.tracker.Metadata {
		if len(meta.Sources) > 0 {
			totalMeasurements++
			// All measurements should have the same number of sources (n-1 where n is number of exchanges)
			if probesPerLocation == 0 {
				probesPerLocation = len(meta.Sources)
			}
		}
	}

	metrics.RipeatlasTotalMeasurements.Set(float64(totalMeasurements))
	metrics.RipeatlasProbesPerMeasurement.Set(float64(probesPerLocation))
	c.log.Info("Initialized RIPE Atlas measurement metrics",
		slog.Int("total_measurements", totalMeasurements),
		slog.Int("probes_per_location", probesPerLocation))

	return nil
}

func calculateAndSortProbeDistances(probes []Probe, lat, lng float64) []ProbeDistance {
	// Convert to CoordinatesGetter slice
	var sources []collector.CoordinatesGetter
	for _, probe := range probes {
		sources = append(sources, probe)
	}

	// Use generic function
	sourceDistances := collector.CalculateAndSortSourceDistances(sources, lat, lng)

	// Convert back to ProbeDistance
	var probeDistances []ProbeDistance
	for _, sd := range sourceDistances {
		probeDistances = append(probeDistances, ProbeDistance{
			Probe:    sd.Source.(Probe),
			Distance: sd.Distance,
		})
	}

	return probeDistances
}

func getNearestProbesSorted(probes []Probe, latitude, longitude float64, maxCount int) []Probe {
	// Use the shared generic function directly
	return collector.GetNearestSourcesSorted(probes, latitude, longitude, maxCount)
}

func filterValidProbes(probes []Probe) []Probe {
	var validProbes []Probe
	for _, probe := range probes {
		if probe.Address != "" && collector.IsInternetRoutable(probe.Address) {
			validProbes = append(validProbes, probe)
		}
	}
	return validProbes
}

func filterResponsiveProbes(probes []Probe, measurementState *MeasurementState) []Probe {
	var responsiveProbes []Probe
	for _, probe := range probes {
		if !measurementState.IsProbeUnresponsive(probe.ID) && probe.Address != "" {
			responsiveProbes = append(responsiveProbes, probe)
		}
	}
	return responsiveProbes
}

func (c *Collector) parseLatencyFromResult(result any) (time.Duration, time.Time, int) {
	resultMap, ok := result.(map[string]any)
	if !ok {
		return 0 * time.Millisecond, time.Time{}, 0
	}

	probeID := 0
	if prb, ok := resultMap["prb_id"].(float64); ok {
		probeID = int(prb)
	}

	timestamp := time.Time{}
	if ts, ok := resultMap["timestamp"].(float64); ok {
		timestamp = time.Unix(int64(ts), 0).UTC()
	}

	if resultArray, ok := resultMap["result"].([]any); ok {
		for _, pingResult := range resultArray {
			if pingMap, ok := pingResult.(map[string]any); ok {
				if rtt, ok := pingMap["rtt"].(float64); ok && rtt > 0 {
					return time.Duration(rtt*1000) * time.Microsecond, timestamp, probeID
				}
			}
		}
	}

	return 0, timestamp, probeID
}

func (c *Collector) ClearAllMeasurements(ctx context.Context) error {
	c.log.Info("Retrieving all measurements")

	measurements, err := c.client.GetAllMeasurements(ctx, c.env)
	if err != nil {
		return collector.NewAPIError("get_measurements", "failed to get measurements", err)
	}

	if len(measurements) == 0 {
		c.log.Info("No measurements found to clear")
		return nil
	}

	c.log.Info("Found measurements to clear", slog.Int("count", len(measurements)))

	successCount := 0
	errorCount := 0

	for _, measurement := range measurements {
		// Skip already stopped measurements
		if measurement.Status.Name == "Stopped" {
			c.log.Debug("Skipping measurement - already stopped", slog.Int("measurement_id", measurement.ID))
			continue
		}

		// Only clear DoubleZero measurements to avoid affecting other measurements
		if !strings.Contains(measurement.Description, "DoubleZero") {
			c.log.Debug("Skipping measurement - not a DoubleZero measurement",
				slog.Int("measurement_id", measurement.ID),
				slog.String("description", measurement.Description))
			continue
		}

		c.log.Info("Stopping measurement",
			slog.Int("measurement_id", measurement.ID),
			slog.String("description", measurement.Description))

		if err := c.client.StopMeasurement(context.Background(), measurement.ID); err != nil {
			c.log.Warn("Error stopping measurement",
				slog.Int("measurement_id", measurement.ID),
				slog.String("error", err.Error()))
			errorCount++
		} else {
			c.log.Info("Successfully stopped measurement",
				slog.Int("measurement_id", measurement.ID))
			successCount++
		}

		// Add a small delay to avoid rate limiting
		time.Sleep(CallDelay)
	}

	c.log.Info("Clear measurements completed",
		slog.Int("successful", successCount),
		slog.Int("errors", errorCount))

	if errorCount > 0 {
		return collector.NewAPIError("process_measurements", "failed to process measurements", nil).
			WithContext("error_count", errorCount)
	}

	return nil
}

func (c *Collector) ListMeasurements(ctx context.Context) error {
	measurements, err := c.client.GetAllMeasurements(ctx, c.env)
	if err != nil {
		return collector.NewAPIError("get_measurements", "failed to get measurements", err)
	}

	fmt.Println("ID,Description,Target,Status,Type")

	for _, measurement := range measurements {
		description := exporter.EscapeCSVField(measurement.Description)

		fmt.Printf("%d,%s,%s,%s,%s\n",
			measurement.ID,
			description,
			measurement.Target,
			measurement.Status.Name,
			measurement.Type)
	}

	return nil
}

// ListAtlasProbes displays a list of nearby RIPE Atlas probes for the given locations
func (c *Collector) ListAtlasProbes(ctx context.Context, locations []collector.LocationMatch) error {
	if len(locations) == 0 {
		c.log.Warn("No locations found")
		return collector.ErrNoDevicesFound
	}

	// Convert LocationMatch to LocationProbeMatch
	var locationProbeMatches []LocationProbeMatch
	for _, loc := range locations {
		locationProbeMatches = append(locationProbeMatches, LocationProbeMatch{
			LocationMatch: loc,
			NearbyProbes:  []Probe{},
			ProbeCount:    0,
		})
	}
	fmt.Printf("Found %d locations\n", len(locations))

	locationMatches, err := c.client.GetProbesForLocations(ctx, locationProbeMatches)
	if err != nil {
		return collector.NewAPIError("get_probes_for_locations", "failed to get probes for locations", err).
			WithContext("location_count", len(locations))
	}

	fmt.Println("\n=== RIPE Atlas Probe Discovery Results ===")
	for _, match := range locationMatches {
		fmt.Printf("\nLocation: %s\n", match.LocationCode)
		fmt.Printf("Coordinates: %.6f, %.6f\n", match.Latitude, match.Longitude)

		if match.ProbeCount == 0 {
			fmt.Println("Nearby Probes (0):")
			fmt.Println("  No probes found")
			continue
		}

		probeDistances := calculateAndSortProbeDistances(match.NearbyProbes, match.Latitude, match.Longitude)

		maxProbes := 15
		if len(probeDistances) < maxProbes {
			maxProbes = len(probeDistances)
		}

		fmt.Printf("Nearby Probes (showing closest %d of %d):\n", maxProbes, len(probeDistances))

		for i := 0; i < maxProbes; i++ {
			probe := probeDistances[i].Probe
			distance := probeDistances[i].Distance

			fmt.Printf("  %d. %s [ID: %d] - ASN: %d - %.2f km away\n",
				i+1, probe.Address, probe.ID, probe.ASN, distance)

			if probe.AddressV6 != "" {
				fmt.Printf("      IPv6: %s\n", probe.AddressV6)
			}

			fmt.Printf("      Status: %s, Type: %s\n",
				probe.Status.Name, probe.Type)
		}
	}

	return nil
}

func (c *Collector) ExportMeasurementResults(ctx context.Context, stateDir string) error {
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return fmt.Errorf("failed to create state directory: %w", err)
	}

	timestampFile := filepath.Join(stateDir, TimestampFileName)

	measurementState := NewMeasurementState(timestampFile)
	if err := measurementState.Load(); err != nil {
		return err
	}

	measurements, err := c.client.GetAllMeasurements(ctx, c.env)
	if err != nil {
		return collector.NewAPIError("get_measurements", "failed to get measurements", err)
	}

	if len(measurements) == 0 {
		c.log.Info("No measurements found to export")
		return nil
	}

	// Filter for active DoubleZero measurements
	var activeMeasurements []Measurement
	for _, measurement := range measurements {
		if strings.Contains(measurement.Description, "DoubleZero") && measurement.Status.Name != "Stopped" {
			activeMeasurements = append(activeMeasurements, measurement)
		}
	}

	if len(activeMeasurements) == 0 {
		c.log.Info("No active DoubleZero measurements found to export")
		return nil
	}

	c.log.Info("Found active DoubleZero measurements to export", slog.Int("count", len(activeMeasurements)))

	// Calculate expected samples based on unique exchange pairs (upper triangular matrix)
	// Build a set of all exchanges that have probes
	exchangesWithProbes := make(map[string]bool)
	for _, measurement := range activeMeasurements {
		if meta, hasMeta := measurementState.GetMetadata(measurement.ID); hasMeta {
			// Target exchange has a probe
			exchangesWithProbes[meta.TargetLocation] = true
			// Source exchanges have probes
			for _, source := range meta.Sources {
				exchangesWithProbes[source.LocationCode] = true
			}
		}
	}

	// Convert to sorted slice for consistent ordering
	var exchanges []string
	for exchange := range exchangesWithProbes {
		exchanges = append(exchanges, exchange)
	}
	sort.Strings(exchanges)

	// Count unique pairs (upper triangular matrix) and track per circuit
	expectedSamples := 0
	circuitExpectedSamples := make(map[string]int)
	for i := 0; i < len(exchanges); i++ {
		for j := i + 1; j < len(exchanges); j++ {
			// Both exchanges have probes, so this pair should have a measurement
			expectedSamples++
			// Verify exchanges are sorted as expected
			if exchanges[i] > exchanges[j] {
				c.log.Warn("Exchanges not in expected sorted order",
					slog.String("exchange_i", exchanges[i]),
					slog.String("exchange_j", exchanges[j]))
			}
			circuit := fmt.Sprintf("%s → %s", exchanges[i], exchanges[j])
			circuitExpectedSamples[circuit] = 1
		}
	}

	// Track expected samples metric per circuit
	for circuit, count := range circuitExpectedSamples {
		metrics.LatencySamplesPerCollectionIntervalExpected.WithLabelValues("ripeatlas", circuit).Add(float64(count))
	}

	if expectedSamples > 0 {
		c.log.Info("RIPE Atlas - Added expected samples metrics",
			slog.Int("total_expected_samples", expectedSamples),
			slog.Int("exchanges_with_probes", len(exchanges)),
			slog.Int("circuits", len(circuitExpectedSamples)))
	}

	recordCount := 0
	circuitActualSamples := make(map[string]int)

	for _, measurement := range activeMeasurements {
		count, records, err := c.exportSingleMeasurementResults(ctx, measurement, measurementState)
		if err != nil {
			c.log.Warn("Failed to export measurement results",
				slog.Int("measurement_id", measurement.ID),
				slog.String("description", measurement.Description),
				slog.String("error", err.Error()))
			continue // Skip this measurement if export fails
		} else {
			recordCount += count
			// Track actual samples per circuit
			for _, record := range records {
				// Create circuit label with alphabetically sorted exchanges
				var circuit string
				if record.SourceExchangeCode < record.TargetExchangeCode {
					circuit = fmt.Sprintf("%s → %s", record.SourceExchangeCode, record.TargetExchangeCode)
				} else {
					circuit = fmt.Sprintf("%s → %s", record.TargetExchangeCode, record.SourceExchangeCode)
				}
				circuitActualSamples[circuit]++
			}
		}
	}

	if err := measurementState.Save(); err != nil {
		c.log.Warn("Failed to save timestamps", slog.String("error", err.Error()))
		return err
	}
	c.log.Debug("Updated timestamp tracking file", slog.String("file", timestampFile))

	// Track actual samples metric per circuit
	for circuit, count := range circuitActualSamples {
		metrics.LatencySamplesPerCollectionIntervalActual.WithLabelValues("ripeatlas", circuit).Add(float64(count))
	}

	if recordCount > 0 {
		c.log.Info("RIPE Atlas - Added actual samples metrics",
			slog.Int("total_actual_samples", recordCount),
			slog.Int("circuits", len(circuitActualSamples)))
	}

	// Track missing samples metric for circuits that were expected but not received
	missingSamples := 0
	var missingCircuits []string
	for circuit := range circuitExpectedSamples {
		if _, exists := circuitActualSamples[circuit]; !exists {
			metrics.LatencySamplesPerCollectionIntervalMissing.WithLabelValues(c.env, circuit, "ripeatlas").Add(1)
			missingSamples++
			missingCircuits = append(missingCircuits, circuit)
		}
	}
	if missingSamples > 0 {
		sort.Strings(missingCircuits)
		c.log.Info("RIPE Atlas - Tracked missing samples",
			slog.Int("missing_samples", missingSamples),
			slog.Int("expected_circuits", len(circuitExpectedSamples)),
			slog.Int("actual_circuits", len(circuitActualSamples)),
			slog.Any("missing_circuits", missingCircuits))
	}

	c.log.Info("Successfully exported measurement results",
		slog.Int("records_written", recordCount))

	return nil
}

func (c *Collector) exportSingleMeasurementResults(ctx context.Context, measurement Measurement, measurementState *MeasurementState) (int, []exporter.Record, error) {
	lastTimestampUnix, exists := measurementState.GetLastTimestamp(measurement.ID)
	lastTimestamp := time.Unix(lastTimestampUnix, 0)

	c.log.Debug("Processing measurement",
		slog.Int("measurement_id", measurement.ID),
		slog.String("description", measurement.Description),
		slog.Bool("has_timestamp", exists),
		slog.Time("last_timestamp", lastTimestamp))

	meta, hasMeta := measurementState.GetMetadata(measurement.ID)
	if !hasMeta {
		c.log.Warn("No metadata found for measurement",
			slog.Int("measurement_id", measurement.ID))
		return 0, nil, nil
	}

	targetLocation := meta.TargetLocation
	probeToLocationLocal := make(map[int]string)
	for _, source := range meta.Sources {
		probeToLocationLocal[source.ProbeID] = source.LocationCode
	}

	// Get measurement results with optional start timestamp
	results, err := c.client.GetMeasurementResultsIncremental(ctx, measurement.ID, lastTimestampUnix)
	if err != nil {
		c.log.Warn("Failed to get results for measurement",
			slog.Int("measurement_id", measurement.ID),
			slog.String("error", err.Error()))
		return 0, nil, err
	}

	if len(results) == 0 {
		c.log.Debug("No new results for measurement",
			slog.Int("measurement_id", measurement.ID),
			slog.String("target_location", meta.TargetLocation),
			slog.Int64("query_start_timestamp", lastTimestampUnix))
		return 0, nil, nil
	}

	c.log.Info("Retrieved new results for measurement",
		slog.Int("measurement_id", measurement.ID),
		slog.String("target_location", meta.TargetLocation),
		slog.Int("result_count", len(results)),
		slog.Int64("query_start_timestamp", lastTimestampUnix))

	var maxTimestamp time.Time
	processedResults := 0

	// Process results - use slice to preserve all samples
	var records []exporter.Record
	for _, result := range results {
		// Parse latency from result (now also returns probe ID)
		latency, timestamp, probeID := c.parseLatencyFromResult(result)
		if latency > 0 {
			if timestamp.After(maxTimestamp) {
				maxTimestamp = timestamp
			}

			sourceLocation := "Unknown"
			if loc, ok := probeToLocationLocal[probeID]; ok {
				sourceLocation = loc
			}

			records = append(records, exporter.Record{
				DataProvider: exporter.DataProviderNameRIPEAtlas,
				// Source and target are swapped here to match alphabetical ordering expected by downstream systems.
				// We alphabetically sort the measurements by exchange code, but from the perspective of the measurement
				// we are pinging from a remote probe to our target exchange.
				SourceExchangeCode: targetLocation,
				TargetExchangeCode: sourceLocation,
				Timestamp:          timestamp,
				RTT:                latency,
			})

			processedResults++
		}
	}

	// Write the batch of records with the exporter.
	if len(records) > 0 {
		if err := c.exporter.WriteRecords(ctx, records); err != nil {
			c.log.Warn("RIPE Atlas failed to write records", "error", err.Error(), "records", len(records))
			return 0, nil, fmt.Errorf("failed to write records: %w", err)
		}
	}

	// Update the timestamp tracker with the newest timestamp seen
	if maxTimestamp.After(lastTimestamp) {
		measurementState.UpdateTimestamp(measurement.ID, maxTimestamp.Unix())
		c.log.Info("Updated timestamp for measurement",
			slog.Int("measurement_id", measurement.ID),
			slog.String("target_location", meta.TargetLocation),
			slog.Time("old_timestamp", lastTimestamp),
			slog.Time("new_timestamp", maxTimestamp),
			slog.Int("raw_results", len(results)),
			slog.Int("valid_latencies", processedResults),
			slog.Int("exported_records", len(records)))
	} else if len(records) > 0 {
		c.log.Warn("Exported records but timestamp not updated (old results?)",
			slog.Int("measurement_id", measurement.ID),
			slog.String("target_location", meta.TargetLocation),
			slog.Time("last_timestamp", lastTimestamp),
			slog.Time("max_result_timestamp", maxTimestamp),
			slog.Int("exported_records", len(records)))
	}

	return len(records), records, nil
}

func (c *Collector) RunRipeAtlasMeasurementCreation(ctx context.Context, dryRun bool, probesPerLocation int, stateDir string, samplingInterval time.Duration) error {
	c.log.Info("Running RIPE Atlas measurement creation")

	locations := c.getLocationsFunc(ctx)
	if len(locations) == 0 {
		c.log.Warn("No locations found for RIPE Atlas measurements")
		return collector.ErrNoDevicesFound
	}

	c.log.Info("Operation started: ripe_atlas_measurement_cycle",
		slog.Int("probes_per_location", probesPerLocation),
		slog.Bool("dry_run", dryRun),
		slog.Int("location_count", len(locations)))

	// Convert LocationMatch to LocationProbeMatch
	var locationProbeMatches []LocationProbeMatch
	for _, loc := range locations {
		locationProbeMatches = append(locationProbeMatches, LocationProbeMatch{
			LocationMatch: loc,
			NearbyProbes:  []Probe{},
			ProbeCount:    0,
		})
	}
	c.log.Info("Found locations", slog.Int("location_count", len(locations)))

	// Get probes for all locations
	locationMatches, err := c.client.GetProbesForLocations(ctx, locationProbeMatches)
	if err != nil {
		return collector.NewAPIError("get_probes_for_locations", "failed to get probes for locations", err).
			WithContext("location_count", len(locations))
	}

	c.log.Info("Found probes for locations", slog.Int("locations_with_probes", len(locationMatches)))

	c.mu.Lock()
	c.probeToLocation = make(map[int]string)
	for _, match := range locationMatches {
		if len(match.NearbyProbes) > 0 {
			// Map the closest probe to this location
			nearestProbes := getNearestProbesSorted(match.NearbyProbes,
				match.Latitude, match.Longitude, probesPerLocation)
			if len(nearestProbes) > 0 {
				c.probeToLocation[nearestProbes[0].ID] = match.LocationCode

				// Calculate and record distance to nearest probe
				nearestProbe := nearestProbes[0]
				distance := collector.HaversineDistance(
					match.Latitude, match.Longitude,
					nearestProbe.Geometry.Coordinates[1], nearestProbe.Geometry.Coordinates[0])
				metrics.DistanceFromExchangeToProbe.WithLabelValues("ripeatlas", match.LocationCode).Set(distance)
			}
		}
	}
	c.mu.Unlock()

	// Configure measurements between locations
	if err := c.configureMeasurements(ctx, locationMatches, dryRun, probesPerLocation, stateDir, samplingInterval); err != nil {
		return collector.NewAPIError("configure_measurements", "failed to configure measurements", err).
			WithContext("location_count", len(locationMatches))
	}

	c.log.Info("Operation completed: ripe_atlas_measurement_cycle")
	return nil
}

func (c *Collector) configureMeasurements(ctx context.Context, locationMatches []LocationProbeMatch, dryRun bool, probesPerLocation int, stateDir string, samplingInterval time.Duration) error {
	// Step 1: Load state first to get unresponsive probes list
	timestampFile := filepath.Join(stateDir, TimestampFileName)
	measurementState := NewMeasurementState(timestampFile)
	if err := measurementState.Load(); err != nil {
		c.log.Warn("Failed to load measurement state", slog.String("error", err.Error()))
	}

	// Step 2: Generate the list of measurements we want, skipping unresponsive probes
	wantedMeasurements := c.generateWantedMeasurements(locationMatches, probesPerLocation, measurementState)

	// Step 2: Get all existing measurements
	existingMeasurements, err := c.client.GetAllMeasurements(ctx, c.env)
	if err != nil {
		c.log.Warn("Failed to get existing measurements", slog.String("error", err.Error()))
		existingMeasurements = []Measurement{}
	}

	// Filter for DoubleZero measurements only
	var doubleZeroMeasurements []Measurement
	for _, m := range existingMeasurements {
		if strings.HasPrefix(m.Description, "DoubleZero ") && m.Status.Name != "Stopped" {
			doubleZeroMeasurements = append(doubleZeroMeasurements, m)
		}
	}

	// Step 3: Build map of existing measurements by target location
	existingByTarget := make(map[string]Measurement)
	for _, m := range doubleZeroMeasurements {
		// Format: "DoubleZero [env] to TARGET probe Y"
		parts := strings.Split(m.Description, " to ")
		if len(parts) == 2 {
			targetPart := parts[1]
			// Extract location code (before " probe")
			if idx := strings.Index(targetPart, " probe"); idx != -1 {
				targetLocation := targetPart[:idx]
				existingByTarget[targetLocation] = m
			}
		}
	}

	// Step 4: Detect unresponsive probes (no exports after 1 hour)
	currentTime := time.Now().Unix()
	probeTimeout := currentTime - 3600 // 1 hours (3660 seconds)
	newUnresponsiveProbes := 0
	for _, measurement := range doubleZeroMeasurements {
		if meta, hasMeta := measurementState.GetMetadata(measurement.ID); hasMeta {
			// Check if measurement is stale - either never exported, or last export was too long ago
			isStale := false
			var reason string

			if meta.LastExportAt == 0 && meta.CreatedAt > 0 && meta.CreatedAt < probeTimeout {
				// Case 1: Created > 1 hour ago but never exported
				isStale = true
				reason = "never_exported"
			} else if meta.LastExportAt > 0 && meta.LastExportAt < probeTimeout {
				// Case 2: Last export was > 1 hour ago
				isStale = true
				reason = "no_recent_exports"
			}

			if isStale {
				c.log.Warn("Marking probe as unresponsive - no exports after 1 hour",
					slog.Int("measurement_id", measurement.ID),
					slog.Int("probe_id", meta.TargetProbeID),
					slog.String("target_location", meta.TargetLocation),
					slog.String("reason", reason),
					slog.Time("created_at", time.Unix(meta.CreatedAt, 0)),
					slog.Time("last_export_at", time.Unix(meta.LastExportAt, 0)))
				measurementState.AddUnresponsiveProbe(meta.TargetProbeID)
				newUnresponsiveProbes++
			}
		}
	}

	// Save state if we detected any new unresponsive probes
	if newUnresponsiveProbes > 0 {
		if err := measurementState.Save(); err != nil {
			c.log.Warn("Failed to save measurement state after detecting unresponsive probes", slog.String("error", err.Error()))
		} else {
			c.log.Info("Saved unresponsive probe state",
				slog.Int("new_unresponsive_probes", newUnresponsiveProbes))
		}
	}

	// Step 5: Determine what to create and what to remove
	toCreate := []MeasurementSpec{}
	wantedTargets := make(map[string]bool)
	wantedByTarget := make(map[string]MeasurementSpec)

	// Build map of wanted measurements by target
	for _, wanted := range wantedMeasurements {
		wantedTargets[wanted.TargetLocationCode] = true
		wantedByTarget[wanted.TargetLocationCode] = wanted
	}

	// Check each wanted measurement
	for _, wanted := range wantedMeasurements {
		existing, exists := existingByTarget[wanted.TargetLocationCode]
		if !exists {
			// No existing measurement for this target, create it
			toCreate = append(toCreate, wanted)
		} else {
			// Check if existing measurement has the correct target and source probes
			meta, hasMeta := measurementState.GetMetadata(existing.ID)
			if !hasMeta {
				// No metadata, can't verify sources, recreate
				toCreate = append(toCreate, wanted)
				continue
			}

			// Check if target probe has changed
			targetProbeChanged := meta.TargetProbeID != wanted.TargetProbe.ID
			if targetProbeChanged {
				c.log.Info("Measurement has outdated target probe, marking for recreation",
					slog.Int("measurement_id", existing.ID),
					slog.String("target", wanted.TargetLocationCode),
					slog.Int("existing_probe_id", meta.TargetProbeID),
					slog.Int("wanted_probe_id", wanted.TargetProbe.ID))
				toCreate = append(toCreate, wanted)
				continue
			}

			// Compare source probe locations
			existingSources := make(map[string]bool)
			for _, source := range meta.Sources {
				existingSources[source.LocationCode] = true
			}

			wantedSources := make(map[string]bool)
			for _, source := range wanted.SourceSpecs {
				wantedSources[source.LocationCode] = true
			}

			// Check if source sets match
			sourcesMatch := len(existingSources) == len(wantedSources)
			if sourcesMatch {
				for loc := range wantedSources {
					if !existingSources[loc] {
						sourcesMatch = false
						break
					}
				}
			}

			if !sourcesMatch {
				// Sources don't match, need to recreate measurement
				c.log.Info("Measurement has outdated source probes, marking for recreation",
					slog.Int("measurement_id", existing.ID),
					slog.String("target", wanted.TargetLocationCode),
					slog.Int("existing_sources", len(existingSources)),
					slog.Int("wanted_sources", len(wantedSources)))
				toCreate = append(toCreate, wanted)
			}
		}
	}

	// Remove measurements for targets we no longer want, measurements without metadata, or measurements with outdated target/source probes
	toRemove := []Measurement{}
	measurementsToRecreate := make(map[string]bool)

	// First, identify measurements that need recreation due to outdated target or source probes
	for _, wanted := range wantedMeasurements {
		if existing, exists := existingByTarget[wanted.TargetLocationCode]; exists {
			meta, hasMeta := measurementState.GetMetadata(existing.ID)
			if hasMeta {
				// Check if target probe has changed
				if meta.TargetProbeID != wanted.TargetProbe.ID {
					measurementsToRecreate[wanted.TargetLocationCode] = true
					continue
				}

				// Check if sources match
				existingSources := make(map[string]bool)
				for _, source := range meta.Sources {
					existingSources[source.LocationCode] = true
				}

				wantedSources := make(map[string]bool)
				for _, source := range wanted.SourceSpecs {
					wantedSources[source.LocationCode] = true
				}

				sourcesMatch := len(existingSources) == len(wantedSources)
				if sourcesMatch {
					for loc := range wantedSources {
						if !existingSources[loc] {
							sourcesMatch = false
							break
						}
					}
				}

				if !sourcesMatch {
					measurementsToRecreate[wanted.TargetLocationCode] = true
				}
			}
		}
	}

	for _, measurement := range doubleZeroMeasurements {
		// Check if measurement has metadata
		_, hasMetadata := measurementState.GetMetadata(measurement.ID)
		if !hasMetadata {
			c.log.Info("Marking measurement for removal due to missing metadata",
				slog.Int("measurement_id", measurement.ID),
				slog.String("description", measurement.Description))
			toRemove = append(toRemove, measurement)
			continue
		}

		parts := strings.Split(measurement.Description, " to ")
		if len(parts) == 2 {
			targetPart := parts[1]
			if idx := strings.Index(targetPart, " probe"); idx != -1 {
				targetLocation := targetPart[:idx]
				if !wantedTargets[targetLocation] {
					toRemove = append(toRemove, measurement)
				} else if measurementsToRecreate[targetLocation] {
					// This measurement needs to be recreated due to outdated target or source probes
					toRemove = append(toRemove, measurement)
				}
			}
		}
	}

	// Step 6: Log the changes
	c.log.Info("Measurement configuration summary",
		slog.Int("wanted", len(wantedMeasurements)),
		slog.Int("existing", len(doubleZeroMeasurements)),
		slog.Int("to_create", len(toCreate)),
		slog.Int("to_remove", len(toRemove)))

	// Step 7: Remove unwanted measurements
	if len(toRemove) > 0 {

		for _, measurement := range toRemove {
			if dryRun {
				c.log.Info("Would remove measurement (dry run)",
					slog.Int("measurement_id", measurement.ID),
					slog.String("description", measurement.Description))
			} else {
				// Export results before removing (only if exporter is available)
				if c.exporter != nil {
					if _, _, err := c.exportSingleMeasurementResults(ctx, measurement, measurementState); err != nil {
						c.log.Warn("Failed to export measurement results before removal",
							slog.Int("measurement_id", measurement.ID),
							slog.String("error", err.Error()))
						// Continue with removal even if export fails
					}
				}

				c.log.Info("Removing measurement",
					slog.Int("measurement_id", measurement.ID),
					slog.String("description", measurement.Description))
				if err := c.client.StopMeasurement(ctx, measurement.ID); err != nil {
					c.log.Warn("Failed to stop measurement",
						slog.Int("measurement_id", measurement.ID),
						slog.String("error", err.Error()))
				}
				// Always remove metadata for measurements we're removing,
				// even if the API call fails (measurement might already be stopped)
				measurementState.RemoveMetadata(measurement.ID)
				metrics.RipeatlasTotalMeasurements.Dec()
				time.Sleep(CallDelay) // Rate limiting
			}
		}

		// Save updated state after removals
		if err := measurementState.Save(); err != nil {
			c.log.Warn("Failed to save measurement state after removals", slog.String("error", err.Error()))
		}
	}

	// Step 7b: Clean up orphaned metadata (measurements in state file but not in API)
	activeIDs := make(map[int]bool)
	for _, m := range doubleZeroMeasurements {
		activeIDs[m.ID] = true
	}

	orphanedIDs := []int{}
	for id := range measurementState.tracker.Metadata {
		if !activeIDs[id] {
			orphanedIDs = append(orphanedIDs, id)
		}
	}

	if len(orphanedIDs) > 0 {
		c.log.Info("Cleaning up orphaned metadata entries",
			slog.Int("count", len(orphanedIDs)))
		for _, id := range orphanedIDs {
			if meta, hasMeta := measurementState.GetMetadata(id); hasMeta {
				ageSeconds := currentTime - meta.CreatedAt
				c.log.Info("Removing orphaned metadata",
					slog.Int("measurement_id", id),
					slog.String("target_location", meta.TargetLocation),
					slog.Int("target_probe_id", meta.TargetProbeID),
					slog.Time("created_at", time.Unix(meta.CreatedAt, 0)),
					slog.Int64("age_seconds", ageSeconds))
			} else {
				c.log.Info("Removing orphaned metadata (no metadata found)",
					slog.Int("measurement_id", id))
			}
			measurementState.RemoveMetadata(id)
		}
		if err := measurementState.Save(); err != nil {
			c.log.Warn("Failed to save state after orphan cleanup", slog.String("error", err.Error()))
		}
	}

	// Step 8: Create new measurements
	for _, spec := range toCreate {
		if dryRun {
			c.log.Info("Would create measurement (dry run)",
				slog.String("target_location", spec.TargetLocation),
				slog.Int("target_probe", spec.TargetProbe.ID),
				slog.Int("source_count", len(spec.SourceSpecs)))
		} else {
			// Use simplified description without source list
			var description string
			if c.env != "" {
				description = fmt.Sprintf("DoubleZero [%s] to %s probe %d",
					c.env, spec.TargetLocationCode, spec.TargetProbe.ID)
			} else {
				description = fmt.Sprintf("DoubleZero to %s probe %d",
					spec.TargetLocationCode, spec.TargetProbe.ID)
			}

			// Build tags including environment if set
			var tags []string
			if c.env != "" {
				tags = append(tags, c.env)
			}
			tags = append(tags, "doublezero")

			var probes []MeasurementProbe
			for _, source := range spec.SourceSpecs {
				probes = append(probes, MeasurementProbe{
					Value:     source.Probe.ID,
					Type:      "probes",
					Requested: 1,
				})
			}

			measurementRequest := MeasurementRequest{
				Definitions: []MeasurementDefinition{
					{
						Type:           "ping",
						AF:             4,
						Interval:       int(samplingInterval.Seconds()),
						Packets:        1,
						PacketInterval: 1000, // Delay between packets; only matters when Packets > 1
						Target:         spec.TargetProbe.Address,
						Description:    description,
						Tags:           tags,
					},
				},
				Probes: probes,
			}

			response, err := c.client.CreateMeasurement(ctx, measurementRequest)
			if err != nil {
				c.log.Warn("Failed to create measurement",
					slog.String("target_location", spec.TargetLocation),
					slog.Int("target_probe", spec.TargetProbe.ID),
					slog.Int("source_count", len(spec.SourceSpecs)),
					slog.String("error", err.Error()))
			} else {
				measurementID := response.Measurements[0]
				c.log.Info("Created measurement",
					slog.Int("measurement_id", measurementID),
					slog.String("description", description))

				sources := make([]SourceProbeMeta, len(spec.SourceSpecs))
				for i, source := range spec.SourceSpecs {
					sources[i] = SourceProbeMeta{
						LocationCode: source.LocationCode,
						ProbeID:      source.Probe.ID,
					}
				}

				meta := MeasurementMeta{
					TargetLocation: spec.TargetLocationCode,
					TargetProbeID:  spec.TargetProbe.ID,
					Sources:        sources,
					CreatedAt:      time.Now().Unix(),
				}

				measurementState.SetMetadata(measurementID, meta)
				if err := measurementState.Save(); err != nil {
					c.log.Warn("Failed to save measurement metadata", slog.String("error", err.Error()))
				} else {
					// Update metrics
					metrics.RipeatlasTotalMeasurements.Inc()
					metrics.RipeatlasProbesPerMeasurement.Set(float64(len(sources)))
				}
			}
			time.Sleep(CallDelay)
		}
	}

	// Step 9: Calculate and update expected daily credits metric
	// Use API-provided estimates when available
	totalProbeCount := 0
	expectedDailyResults := 0.0
	expectedDailyCredits := 0.0

	// Build maps for API-provided values
	measurementAPIData := make(map[int]Measurement)
	for _, m := range doubleZeroMeasurements {
		measurementAPIData[m.ID] = m
	}

	for id := range measurementState.tracker.Metadata {
		if meta, exists := measurementState.tracker.Metadata[id]; exists {
			probeCount := len(meta.Sources)
			totalProbeCount += probeCount

			// Use API-provided estimates if available
			if apiData, ok := measurementAPIData[id]; ok {
				if apiData.EstimatedResultsPerDay > 0 {
					expectedDailyResults += apiData.EstimatedResultsPerDay
				} else {
					// Fallback calculation if API doesn't provide estimate
					samplesPerDay := (24 * time.Hour) / samplingInterval
					expectedDailyResults += float64(probeCount) * float64(samplesPerDay)
				}

				if apiData.CreditsPerResult > 0 && apiData.EstimatedResultsPerDay > 0 {
					expectedDailyCredits += apiData.CreditsPerResult * apiData.EstimatedResultsPerDay
				} else {
					// Fallback: assume 2 credits per result
					samplesPerDay := (24 * time.Hour) / samplingInterval
					expectedDailyCredits += 2.0 * float64(probeCount) * float64(samplesPerDay)
				}
			} else {
				// No API data available, use fallback calculation
				samplesPerDay := (24 * time.Hour) / samplingInterval
				expectedDailyResults += float64(probeCount) * float64(samplesPerDay)
				expectedDailyCredits += 2.0 * float64(probeCount) * float64(samplesPerDay)
			}
		}
	}

	metrics.RipeatlasExpectedDailyResults.Set(expectedDailyResults)
	metrics.RipeatlasExpectedDailyCredits.Set(expectedDailyCredits)

	c.log.Info("Updated expected daily metrics",
		slog.Float64("expected_daily_results", expectedDailyResults),
		slog.Float64("expected_daily_credits", expectedDailyCredits),
		slog.Int("total_probe_count", totalProbeCount),
		slog.Int("measurement_count", len(measurementState.tracker.Metadata)),
		slog.String("source", "api_estimates_with_fallback"))

	return nil
}

func (c *Collector) generateWantedMeasurements(locationMatches []LocationProbeMatch, probesPerLocation int, measurementState *MeasurementState) []MeasurementSpec {
	var wantedMeasurements []MeasurementSpec

	// Get list of unresponsive probes to skip
	unresponsiveProbes := measurementState.GetUnresponsiveProbes()
	c.log.Info("Generating wanted measurements",
		slog.Int("unresponsive_probe_count", len(unresponsiveProbes)))

	// Sort locations alphabetically by location code to ensure deterministic ordering
	sortedLocations := make([]LocationProbeMatch, len(locationMatches))
	copy(sortedLocations, locationMatches)
	sort.Slice(sortedLocations, func(i, j int) bool {
		return sortedLocations[i].LocationCode < sortedLocations[j].LocationCode
	})

	// Create one measurement per target location
	// Each measurement will ping from all other locations' probes to this target
	for targetIdx, targetLocation := range sortedLocations {
		if len(targetLocation.NearbyProbes) == 0 {
			continue
		}

		responsiveProbes := filterResponsiveProbes(targetLocation.NearbyProbes, measurementState)
		if len(responsiveProbes) == 0 {
			c.log.Warn("No responsive probes found for location",
				slog.String("location", targetLocation.LocationCode))
			continue
		}

		targetProbes := getNearestProbesSorted(responsiveProbes,
			targetLocation.Latitude, targetLocation.Longitude, probesPerLocation)
		if len(targetProbes) == 0 {
			continue
		}
		targetProbe := targetProbes[0]

		// Collect source probes from all other locations
		// Since we're iterating in alphabetical order and only need to measure once between any pair,
		// we only include sources from locations that come after this target in the alphabet
		var sourceSpecs []SourceSpec
		for sourceIdx, sourceLocation := range sortedLocations {
			// Skip if this is the target location itself
			if sourceIdx == targetIdx {
				continue
			}

			// Only include sources that come after the target alphabetically
			// This ensures we only measure once between any pair
			if sourceIdx < targetIdx {
				continue
			}

			if len(sourceLocation.NearbyProbes) == 0 {
				continue
			}

			responsiveSourceProbes := filterResponsiveProbes(sourceLocation.NearbyProbes, measurementState)
			if len(responsiveSourceProbes) == 0 {
				continue
			}

			sourceProbes := getNearestProbesSorted(responsiveSourceProbes,
				sourceLocation.Latitude, sourceLocation.Longitude, probesPerLocation)
			if len(sourceProbes) > 0 {
				sourceSpecs = append(sourceSpecs, SourceSpec{
					LocationCode: sourceLocation.LocationCode,
					Probe:        sourceProbes[0],
				})
			}
		}

		if len(sourceSpecs) > 0 {
			wantedMeasurements = append(wantedMeasurements, MeasurementSpec{
				TargetLocation:     targetLocation.LocationCode,
				TargetLocationCode: targetLocation.LocationCode,
				TargetProbe:        targetProbe,
				SourceSpecs:        sourceSpecs,
			})
		}
	}

	return wantedMeasurements
}

func (c *Collector) Run(ctx context.Context, dryRun bool, probesPerLocation int, stateDir string, samplingInterval, measurementInterval, exportInterval time.Duration) error {
	// Validate intervals
	if samplingInterval <= 0 {
		return fmt.Errorf("RIPE Atlas sampling interval must be positive, got %v", samplingInterval)
	}
	if measurementInterval <= 0 {
		return fmt.Errorf("RIPE Atlas measurement interval must be positive, got %v", measurementInterval)
	}
	if exportInterval <= 0 {
		return fmt.Errorf("RIPE Atlas export interval must be positive, got %v", exportInterval)
	}

	var wg sync.WaitGroup

	// Run measurement creation immediately on startup
	c.log.Info("Running initial RIPE Atlas measurement creation")
	managementStart := time.Now()
	if err := c.RunRipeAtlasMeasurementCreation(ctx, dryRun, probesPerLocation, stateDir, samplingInterval); err != nil {
		c.log.Error("Initial measurement creation failed", slog.String("error", err.Error()))
		metrics.RipeatlasMeasurementManagementFailuresTotal.Inc()
		// Continue running even if initial creation fails
	} else {
		managementDuration := time.Since(managementStart)
		metrics.RunDurationSeconds.WithLabelValues("ripeatlas", "manage_measurements").Observe(managementDuration.Seconds())
		c.log.Info("RIPE Atlas measurement management completed",
			slog.Duration("duration", managementDuration))
		metrics.RipeatlasMeasurementManagementRunsTotal.Inc()
	}

	// Measurement management
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Create ticker with configurable interval
		ticker := time.NewTicker(measurementInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				c.log.Info("Stopping RIPE Atlas measurement creation")
				return
			case <-ticker.C:
				managementStart := time.Now()
				if err := c.RunRipeAtlasMeasurementCreation(ctx, dryRun, probesPerLocation, stateDir, samplingInterval); err != nil {
					c.log.Error("Operation failed: create_ripeatlas_measurements", slog.String("error", err.Error()))
					metrics.RipeatlasMeasurementManagementFailuresTotal.Inc()
				} else {
					managementDuration := time.Since(managementStart)
					metrics.RunDurationSeconds.WithLabelValues("ripeatlas", "manage_measurements").Observe(managementDuration.Seconds())
					c.log.Info("RIPE Atlas measurement management completed",
						slog.Duration("duration", managementDuration))
					metrics.RipeatlasMeasurementManagementRunsTotal.Inc()
				}
			}
		}
	}()

	// Data export
	wg.Add(1)
	go func() {
		defer wg.Done()
		c.log.Info("Starting RIPE Atlas export")
		// Create ticker with configurable interval
		ticker := time.NewTicker(exportInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				c.log.Info("Stopping RIPE Atlas export")
				return
			case <-ticker.C:
				exportStart := time.Now()
				if err := c.ExportMeasurementResults(ctx, stateDir); err != nil {
					c.log.Warn("Failed to export RIPE Atlas measurements", slog.String("error", err.Error()))
					metrics.CollectionFailuresTotal.WithLabelValues("ripeatlas").Inc()
				} else {
					exportDuration := time.Since(exportStart)
					metrics.RunDurationSeconds.WithLabelValues("ripeatlas", "export").Observe(exportDuration.Seconds())
					c.log.Info("RIPE Atlas export completed",
						slog.Duration("duration", exportDuration))
					metrics.CollectionRunsTotal.WithLabelValues("ripeatlas").Inc()
				}

				if balance, err := c.client.GetCreditBalance(ctx); err != nil {
					c.log.Warn("Failed to get RIPE Atlas credit balance", slog.String("error", err.Error()))
				} else {
					metrics.RipeatlasCreditBalance.Set(balance)
				}
			}
		}
	}()

	// Wait for context cancellation
	<-ctx.Done()
	c.log.Info("RIPE Atlas collector shutting down")

	// Wait for all goroutines to complete
	wg.Wait()

	return nil
}
