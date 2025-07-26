package ripeatlas

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/internal/collector"
)

const (
	TimestampFileName = "ripe_atlas_timestamps.json"
)

// CallDelay is defined in client.go to avoid duplication

type clientInterface interface {
	GetProbesInRadius(ctx context.Context, latitude, longitude float64, radiusKm int) ([]Probe, error)
	GetProbesForLocations(ctx context.Context, locations []LocationProbeMatch) ([]LocationProbeMatch, error)
	CreateMeasurement(ctx context.Context, request MeasurementRequest) (*MeasurementResponse, error)
	GetAllMeasurements(ctx context.Context) ([]Measurement, error)
	GetMeasurementResultsIncremental(ctx context.Context, measurementID int, startTimestamp int64) ([]any, error)
	StopMeasurement(ctx context.Context, measurementID int) error
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
	client clientInterface
	log    *slog.Logger
}

type MeasurementSpec struct {
	SourceLocation     string
	TargetLocation     string
	SourceLocationCode string
	TargetLocationCode string
	SourceProbe        Probe
	TargetProbe        Probe
}

func NewCollector(logger *slog.Logger) *Collector {
	return &Collector{
		client: NewClient(logger),
		log:    logger,
	}
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

func parseProbeIDsFromDescription(description string) (sourceProbe int, targetProbe int, locationA, locationZ string) {
	// Split by " to "
	parts := strings.Split(description, " to ")
	if len(parts) != 2 {
		return 0, 0, "", ""
	}

	// Extract source probe from left part: "DoubleZero LocationA probe 123"
	leftPart := parts[0]
	if idx := strings.LastIndex(leftPart, " probe "); idx != -1 {
		// Extract probe ID
		probeIDStr := leftPart[idx+7:] // Skip " probe "
		if sourceID, err := strconv.Atoi(probeIDStr); err == nil {
			sourceProbe = sourceID
		}

		// Extract location A
		beforeProbe := leftPart[:idx]
		locationA = strings.TrimPrefix(beforeProbe, "DoubleZero ")
		locationA = strings.TrimSpace(locationA)
	}

	// Extract target probe from right part: "LocationZ probe 456"
	rightPart := parts[1]
	if idx := strings.LastIndex(rightPart, " probe "); idx != -1 {
		// Extract probe ID
		probeIDStr := rightPart[idx+7:] // Skip " probe "
		if targetID, err := strconv.Atoi(probeIDStr); err == nil {
			targetProbe = targetID
		}

		// Extract location Z
		locationZ = rightPart[:idx]
		locationZ = strings.TrimSpace(locationZ)
	}

	return sourceProbe, targetProbe, locationA, locationZ
}

func (c *Collector) parseLatencyFromResult(result any) (float64, string) {
	// Parse the result JSON to extract latency data
	resultMap, ok := result.(map[string]any)
	if !ok {
		return 0, ""
	}

	// Extract timestamp
	timestamp := ""
	if ts, ok := resultMap["timestamp"].(float64); ok {
		timestamp = time.Unix(int64(ts), 0).UTC().Format(collector.TimeFormatMicroseconds)
	}

	// Extract latency from ping results
	if resultArray, ok := resultMap["result"].([]any); ok {
		for _, pingResult := range resultArray {
			if pingMap, ok := pingResult.(map[string]any); ok {
				if rtt, ok := pingMap["rtt"].(float64); ok && rtt > 0 {
					return rtt, timestamp
				}
			}
		}
	}

	return 0, timestamp
}

func (c *Collector) ClearAllMeasurements(ctx context.Context) error {
	c.log.Info("Retrieving all measurements")

	measurements, err := c.client.GetAllMeasurements(ctx)
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
	measurements, err := c.client.GetAllMeasurements(ctx)
	if err != nil {
		return collector.NewAPIError("get_measurements", "failed to get measurements", err)
	}

	fmt.Println("ID,Description,Target,Status,Type")

	for _, measurement := range measurements {
		description := collector.EscapeCSVField(measurement.Description)

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

func (c *Collector) ExportMeasurementResults(ctx context.Context, stateDir, outputDir string) error {
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return fmt.Errorf("failed to create state directory: %w", err)
	}
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	timestampFile := filepath.Join(stateDir, TimestampFileName)

	measurementState := NewMeasurementState(timestampFile)
	if err := measurementState.Load(); err != nil {
		return err
	}

	csvExporter, err := collector.NewCSVExporter(c.log, "ripe_atlas_measurements", outputDir)
	if err != nil {
		return err
	}
	defer csvExporter.Close()

	measurements, err := c.client.GetAllMeasurements(ctx)
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

	header := []string{"location_a", "location_z", "timestamp", "latency"}
	if err := csvExporter.WriteHeader(header); err != nil {
		return err
	}

	recordCount := 0

	for _, measurement := range activeMeasurements {
		count, err := c.exportSingleMeasurementResults(ctx, measurement, outputDir, measurementState, csvExporter)
		if err != nil {
			c.log.Warn("Failed to export measurement results",
				slog.Int("measurement_id", measurement.ID),
				slog.String("description", measurement.Description),
				slog.String("error", err.Error()))
			continue // Skip this measurement if export fails
		} else {
			recordCount += count
		}
	}

	if err := measurementState.Save(); err != nil {
		c.log.Warn("Failed to save timestamps", slog.String("error", err.Error()))
		return err
	}
	c.log.Debug("Updated timestamp tracking file", slog.String("file", timestampFile))

	c.log.Info("Successfully exported measurement results",
		slog.String("file", csvExporter.GetFilename()),
		slog.Int("records_written", recordCount))

	return nil
}

func (c *Collector) exportSingleMeasurementResults(ctx context.Context, measurement Measurement, outputDir string, measurementState *MeasurementState, csvExporter *collector.CSVExporter) (int, error) {
	lastTimestamp, exists := measurementState.GetLastTimestamp(measurement.ID)
	recordCount := 0

	c.log.Debug("Processing measurement",
		slog.Int("measurement_id", measurement.ID),
		slog.String("description", measurement.Description),
		slog.Bool("has_timestamp", exists),
		slog.Int64("last_timestamp", lastTimestamp))

	// Parse location codes from description (new format has codes instead of names)
	_, _, locationA, locationZ := parseProbeIDsFromDescription(measurement.Description)

	if locationA == "" {
		locationA = "Unknown"
	}
	if locationZ == "" {
		locationZ = "Unknown"
	}

	// Get measurement results with optional start timestamp
	results, err := c.client.GetMeasurementResultsIncremental(ctx, measurement.ID, lastTimestamp)
	if err != nil {
		c.log.Warn("Failed to get results for measurement",
			slog.Int("measurement_id", measurement.ID),
			slog.String("error", err.Error()))
		return 0, err
	}

	if len(results) == 0 {
		c.log.Debug("No new results for measurement", slog.Int("measurement_id", measurement.ID))
		return 0, nil
	}

	c.log.Debug("Retrieved new results for measurement",
		slog.Int("measurement_id", measurement.ID),
		slog.Int("result_count", len(results)))

	var maxTimestamp int64
	processedResults := 0

	// Process results and write to CSV
	for _, result := range results {
		// Parse latency from result
		latency, timestampStr := c.parseLatencyFromResult(result)
		if latency > 0 {
			// Parse the timestamp
			resultMap, ok := result.(map[string]any)
			if ok {
				if ts, ok := resultMap["timestamp"].(float64); ok {
					timestamp := int64(ts)
					if timestamp > maxTimestamp {
						maxTimestamp = timestamp
					}
				}
			}

			record := []string{
				locationA,
				locationZ,
				timestampStr,
				fmt.Sprintf("%.2f", latency),
			}

			csvExporter.WriteRecordWithWarning(record)
			processedResults++
			recordCount++
		}
	}

	// Update the timestamp tracker with the newest timestamp seen
	if maxTimestamp > lastTimestamp {
		measurementState.UpdateTimestamp(measurement.ID, maxTimestamp)
		c.log.Debug("Updated timestamp for measurement",
			slog.Int("measurement_id", measurement.ID),
			slog.Int64("new_timestamp", maxTimestamp),
			slog.Int("processed_results", processedResults))
	}

	return recordCount, nil
}

func (c *Collector) RunRipeAtlasMeasurementCreation(ctx context.Context, dryRun bool, probesPerLocation int, outputDir string, stateDir string) error {
	c.log.Info("Running RIPE Atlas measurement creation")

	locations := collector.GetLocations(ctx, c.log)
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

	// Configure measurements between locations
	if err := c.configureMeasurements(ctx, locationMatches, dryRun, probesPerLocation, outputDir, stateDir); err != nil {
		return collector.NewAPIError("configure_measurements", "failed to configure measurements", err).
			WithContext("location_count", len(locationMatches))
	}

	c.log.Info("Operation completed: ripe_atlas_measurement_cycle")
	return nil
}

func (c *Collector) configureMeasurements(ctx context.Context, locationMatches []LocationProbeMatch, dryRun bool, probesPerLocation int, outputDir string, stateDir string) error {
	// Step 1: Generate the list of measurements we want
	wantedMeasurements := c.generateWantedMeasurements(locationMatches, probesPerLocation)

	// Step 2: Get all existing measurements
	existingMeasurements, err := c.client.GetAllMeasurements(ctx)
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

	// Step 3: Build a map of existing measurements for easy lookup
	existingMap := make(map[string]Measurement)
	for _, m := range doubleZeroMeasurements {
		sourceProbe, targetProbe, _, _ := parseProbeIDsFromDescription(m.Description)
		if sourceProbe > 0 && targetProbe > 0 {
			key := fmt.Sprintf("%d->%d", sourceProbe, targetProbe)
			existingMap[key] = m
		}
	}

	// Step 4: Determine what to create and what to remove
	toCreate := []MeasurementSpec{}
	for _, wanted := range wantedMeasurements {
		key := fmt.Sprintf("%d->%d", wanted.SourceProbe.ID, wanted.TargetProbe.ID)
		if _, exists := existingMap[key]; !exists {
			toCreate = append(toCreate, wanted)
		}
	}

	toRemove := []Measurement{}
	wantedMap := make(map[string]bool)
	for _, wanted := range wantedMeasurements {
		key := fmt.Sprintf("%d->%d", wanted.SourceProbe.ID, wanted.TargetProbe.ID)
		wantedMap[key] = true
	}

	for key, measurement := range existingMap {
		if !wantedMap[key] {
			toRemove = append(toRemove, measurement)
		}
	}

	// Step 5: Log the changes
	c.log.Info("Measurement configuration summary",
		slog.Int("wanted", len(wantedMeasurements)),
		slog.Int("existing", len(doubleZeroMeasurements)),
		slog.Int("to_create", len(toCreate)),
		slog.Int("to_remove", len(toRemove)))

	// Step 6: Remove unwanted measurements
	if len(toRemove) > 0 {
		timestampFile := filepath.Join(stateDir, TimestampFileName)

		measurementState := NewMeasurementState(timestampFile)
		if err := measurementState.Load(); err != nil {
			return err
		}

		// Create CSV exporter
		csvExporter, err := collector.NewCSVExporter(c.log, "ripe_atlas_measurements", outputDir)
		if err != nil {
			return err
		}
		defer csvExporter.Close()

		for _, measurement := range toRemove {
			if dryRun {
				c.log.Info("Would remove measurement (dry run)",
					slog.Int("measurement_id", measurement.ID),
					slog.String("description", measurement.Description))
			} else {
				// Export results before removing
				if _, err := c.exportSingleMeasurementResults(ctx, measurement, outputDir, measurementState, csvExporter); err != nil {
					c.log.Warn("Failed to export measurement results before removal",
						slog.Int("measurement_id", measurement.ID),
						slog.String("error", err.Error()))
					// Continue with removal even if export fails
				}

				c.log.Info("Removing measurement",
					slog.Int("measurement_id", measurement.ID),
					slog.String("description", measurement.Description))
				if err := c.client.StopMeasurement(ctx, measurement.ID); err != nil {
					c.log.Warn("Failed to stop measurement",
						slog.Int("measurement_id", measurement.ID),
						slog.String("error", err.Error()))
				}
				time.Sleep(CallDelay) // Rate limiting
			}
		}
	}

	// Step 7: Create new measurements
	for _, spec := range toCreate {
		if dryRun {
			c.log.Info("Would create measurement (dry run)",
				slog.String("source_location", spec.SourceLocation),
				slog.Int("source_probe", spec.SourceProbe.ID),
				slog.String("target_location", spec.TargetLocation),
				slog.Int("target_probe", spec.TargetProbe.ID))
		} else {
			description := fmt.Sprintf("DoubleZero %s probe %d to %s probe %d",
				spec.SourceLocationCode, spec.SourceProbe.ID,
				spec.TargetLocationCode, spec.TargetProbe.ID)

			measurementRequest := MeasurementRequest{
				Definitions: []MeasurementDefinition{
					{
						Type:           "ping",
						AF:             4,
						Interval:       120,
						Packets:        1,
						Size:           1280,
						PacketInterval: 1000, // Delay between packets; only matters when Packets > 1
						Target:         spec.TargetProbe.Address,
						Description:    description,
					},
				},
				Probes: []MeasurementProbe{
					{
						Value:     spec.SourceProbe.ID,
						Type:      "probes",
						Requested: 1,
					},
				},
			}

			response, err := c.client.CreateMeasurement(ctx, measurementRequest)
			if err != nil {
				c.log.Warn("Failed to create measurement",
					slog.String("source_location", spec.SourceLocation),
					slog.Int("source_probe", spec.SourceProbe.ID),
					slog.String("target_location", spec.TargetLocation),
					slog.Int("target_probe", spec.TargetProbe.ID),
					slog.String("error", err.Error()))
			} else {
				c.log.Info("Created measurement",
					slog.Int("measurement_id", response.Measurements[0]),
					slog.String("description", description))
			}
			time.Sleep(CallDelay)
		}
	}

	return nil
}

func (c *Collector) generateWantedMeasurements(locationMatches []LocationProbeMatch, probesPerLocation int) []MeasurementSpec {
	var wantedMeasurements []MeasurementSpec

	// Sort locations alphabetically by location code to ensure deterministic ordering
	sortedLocations := make([]LocationProbeMatch, len(locationMatches))
	copy(sortedLocations, locationMatches)
	sort.Slice(sortedLocations, func(i, j int) bool {
		return sortedLocations[i].LocationCode < sortedLocations[j].LocationCode
	})

	// We want a matrix of measurements between all location pairs
	for i, sourceLocation := range sortedLocations {
		if len(sourceLocation.NearbyProbes) == 0 {
			continue
		}

		sourceProbes := getNearestProbesSorted(sourceLocation.NearbyProbes,
			sourceLocation.Latitude, sourceLocation.Longitude, probesPerLocation)

		for j, targetLocation := range sortedLocations {
			// Since ping measures round-trip time, we only need A->B, not B->A
			if i >= j || len(targetLocation.NearbyProbes) == 0 {
				continue
			}

			targetProbes := getNearestProbesSorted(targetLocation.NearbyProbes,
				targetLocation.Latitude, targetLocation.Longitude, probesPerLocation)

			// Create measurements between the closest probes
			// Use min to handle cases where one location has fewer probes
			measurementCount := min(len(sourceProbes), len(targetProbes), probesPerLocation)

			for k := 0; k < measurementCount; k++ {
				sourceProbe := sourceProbes[k]
				targetProbe := targetProbes[k]

				if targetProbe.Address == "" {
					continue
				}

				wantedMeasurements = append(wantedMeasurements, MeasurementSpec{
					SourceLocation:     sourceLocation.LocationCode,
					TargetLocation:     targetLocation.LocationCode,
					SourceLocationCode: sourceLocation.LocationCode,
					TargetLocationCode: targetLocation.LocationCode,
					SourceProbe:        sourceProbe,
					TargetProbe:        targetProbe,
				})
			}
		}
	}

	return wantedMeasurements
}

func (c *Collector) Run(ctx context.Context, dryRun bool, probesPerLocation int, stateDir, outputDir string, measurementInterval, exportInterval time.Duration) error {
	// Validate intervals
	if measurementInterval <= 0 {
		return fmt.Errorf("RIPE Atlas measurement interval must be positive, got %v", measurementInterval)
	}
	if exportInterval <= 0 {
		return fmt.Errorf("RIPE Atlas export interval must be positive, got %v", exportInterval)
	}

	var wg sync.WaitGroup

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
				if err := c.RunRipeAtlasMeasurementCreation(ctx, dryRun, probesPerLocation, outputDir, stateDir); err != nil {
					c.log.Error("Operation failed: create_ripeatlas_measurements", slog.String("error", err.Error()))
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
				if err := c.ExportMeasurementResults(ctx, stateDir, outputDir); err != nil {
					c.log.Warn("Failed to export RIPE Atlas measurements", slog.String("error", err.Error()))
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
