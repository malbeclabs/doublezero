package wheresitup

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/internal/collector"
	"github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/internal/exporter"
	"github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/internal/metrics"
)

const (
	RequestTimeout         = 30 * time.Second // Timeout for job requests
	CreditWarningThreshold = 10000

	// JobExpireAfter is how long WheresItUp is asked to retain a job's results, and
	// jobExpireAfterParam is the same duration in the relative-time form the job API accepts
	// (https://wheresitup.com/docs/?shell#creating-jobs). Keep the two in sync.
	//
	// Past the expiry the API still reports the job with an empty "complete" and a populated
	// "in_progress", which is what a running job looks like in the fields read here, so the
	// value has to comfortably exceed one collection cycle.
	JobExpireAfter      = time.Hour
	jobExpireAfterParam = "1 hour"

	// ExpiryGrace pads the local age cutoff so a job is dropped only once its results are
	// certainly gone.
	ExpiryGrace = time.Minute
)

type Collector struct {
	client           clientInterface
	log              *slog.Logger
	jobWaitTimeout   time.Duration // Duration to wait between job creation and export
	getLocationsFunc func(ctx context.Context) []collector.LocationMatch
	exporter         exporter.Exporter
	env              string
}

type clientInterface interface {
	GetAllSources(ctx context.Context) ([]Source, error)
	GetNearestSources(ctx context.Context, latitude, longitude float64, count int) ([]Source, error)
	GetNearestSourcesForLocations(ctx context.Context, locations []collector.LocationMatch) ([]LocationSourceMatch, error)
	CreateJobWithRequest(ctx context.Context, request any, debug bool) (*JobResponse, error)
	GetJobResults(ctx context.Context, jobID string) (*JobResultResponse, error)
	GetAllJobs(ctx context.Context) ([]JobDetails, error)
	GetCredit(ctx context.Context) (int, error)
}

func NewCollector(logger *slog.Logger, exporter exporter.Exporter, env string, getLocationsFunc func(ctx context.Context) []collector.LocationMatch) *Collector {
	return &Collector{
		client:           NewClient(logger),
		log:              logger,
		jobWaitTimeout:   RequestTimeout, // Default to 30 seconds
		getLocationsFunc: getLocationsFunc,
		exporter:         exporter,
		env:              env,
	}
}

// SetJobWaitTimeout sets the duration to wait between job creation and export.
// Used for testing to avoid waiting 30 seconds.
func (c *Collector) SetJobWaitTimeout(timeout time.Duration) {
	c.jobWaitTimeout = timeout
}

func (c *Collector) InitializeCreditBalance(ctx context.Context) error {
	credit, err := c.client.GetCredit(ctx)
	if err != nil {
		return fmt.Errorf("failed to get Wheresitup credit balance: %w", err)
	}

	metrics.WheresitupCreditBalance.Set(float64(credit))
	c.log.Info("Initialized Wheresitup credit balance metric", slog.Int("credits", credit))

	if credit < CreditWarningThreshold {
		c.log.Warn("Low Wheresitup credit balance", slog.Int("credits", credit), slog.Int("threshold", CreditWarningThreshold))
	}

	return nil
}

func (c *Collector) PrintSources(ctx context.Context, locations []collector.LocationMatch) error {
	if len(locations) == 0 {
		c.log.Warn("Wheresitup - No locations found")
		return collector.ErrNoDevicesFound
	}

	locationMatches, err := c.client.GetNearestSourcesForLocations(ctx, locations)
	if err != nil {
		return collector.NewAPIError("Wheresitup get_nearest_sources", "failed to get nearest sources", err).
			WithContext("location_count", len(locations))
	}

	fmt.Println("\n=== Wheresitup Source Discovery Results ===")
	for _, match := range locationMatches {
		fmt.Printf("\nLocation: %s\n", match.LocationCode)
		fmt.Printf("Coordinates: %.6f, %.6f\n", match.Latitude, match.Longitude)
		fmt.Printf("Nearest Sources (%d):\n", match.SourceCount)

		if match.SourceCount == 0 {
			c.log.Warn("No sources found for location",
				slog.String("location", match.LocationCode))
			fmt.Println("  No sources found")
			continue
		}

		c.log.Debug("Found sources for location",
			slog.String("location", match.LocationCode),
			slog.Int("source_count", match.SourceCount))

		for i, source := range match.NearestSources {
			// Convert string coordinates to float64 for distance calculation
			sourceLat, _ := strconv.ParseFloat(source.Latitude, 64)
			sourceLng, _ := strconv.ParseFloat(source.Longitude, 64)
			result := collector.CalculateDistanceToLocation(sourceLat, sourceLng, match.LocationMatch)
			distance := result.Distance
			if !result.Valid {
				distance = 0
			}

			location := source.Location
			if location == "" {
				location = source.Name
			}

			fmt.Printf("  %d. %s (%s) - %s [ID: %s] - %.2f km away\n",
				i+1, source.Title, source.Name, location, source.ID, distance)
		}
	}

	return nil
}

func (c *Collector) RunJobCreation(ctx context.Context, locations []collector.LocationMatch, dryRun bool, jobIDsFile string) error {
	credit, err := c.client.GetCredit(ctx)
	if err != nil {
		c.log.Warn("Failed to check Wheresitup credit balance",
			slog.String("error", err.Error()))
	} else {
		c.log.Info("Wheresitup credit balance",
			slog.Int("credits", credit))
		metrics.WheresitupCreditBalance.Set(float64(credit))
		if credit < CreditWarningThreshold {
			c.log.Warn("Low Wheresitup credit balance",
				slog.Int("credits", credit),
				slog.Int("threshold", CreditWarningThreshold))
		}
	}

	if len(locations) == 0 {
		c.log.Warn("No locations found")
		return nil
	}

	c.log.Info("Wheresitup - Found locations", slog.Int("location_count", len(locations)))

	locationMatches, err := c.client.GetNearestSourcesForLocations(ctx, locations)
	if err != nil {
		return collector.NewAPIError("get_nearest_sources", "failed to get nearest sources", err).
			WithContext("location_count", len(locations))
	}

	var locationsWithSources []LocationSourceMatch
	for _, location := range locationMatches {
		if location.SourceCount > 0 {
			locationsWithSources = append(locationsWithSources, location)

			// Calculate and record distance to nearest source
			if len(location.NearestSources) > 0 && location.NearestSources[0].Latitude != "" {
				sourceLat, err1 := strconv.ParseFloat(location.NearestSources[0].Latitude, 64)
				sourceLon, err2 := strconv.ParseFloat(location.NearestSources[0].Longitude, 64)
				if err1 == nil && err2 == nil {
					distance := collector.HaversineDistance(
						location.Latitude, location.Longitude,
						sourceLat, sourceLon)
					metrics.DistanceFromExchangeToProbe.WithLabelValues("wheresitup", location.LocationCode).Set(distance)
				}
			}
		}
	}

	if len(locationsWithSources) < 2 {
		return collector.ErrInsufficientSources.WithContext("found_locations", len(locationsWithSources)).
			WithContext("minimum_required", 2)
	}

	c.log.Info(
		"Wheresitup creating ping jobs between locations",
		slog.Int("location_count", len(locationsWithSources)),
		slog.String("expire_after", jobExpireAfterParam))

	jobCreationStart := time.Now()
	jobResponses, err := c.CreateJobsBetweenLocations(ctx, locationsWithSources, dryRun, false)
	jobCreationDuration := time.Since(jobCreationStart)
	metrics.RunDurationSeconds.WithLabelValues("wheresitup", "create_jobs").Observe(jobCreationDuration.Seconds())
	c.log.Info("Wheresitup job creation completed",
		slog.Duration("duration", jobCreationDuration),
		slog.Int("job_count", len(jobResponses)))

	if err != nil {
		return collector.ErrJobCreation.WithContext("location_count", len(locationsWithSources)).
			WithContext("dry_run", dryRun)
	}

	if !dryRun && len(jobResponses) > 0 {
		var newJobIDs []string
		var emptyIDCount int
		for _, jobResponse := range jobResponses {
			if jobResponse.ID != "" {
				newJobIDs = append(newJobIDs, jobResponse.ID)
				c.log.Debug("Wheresitup - Valid job ID created", slog.String("job_id", jobResponse.ID))
			} else {
				emptyIDCount++
				c.log.Warn("Wheresitup - Job response has empty ID",
					slog.String("status", jobResponse.Status),
					slog.String("created", jobResponse.Created))
			}
		}

		if emptyIDCount > 0 {
			c.log.Warn("Wheresitup - Some jobs returned empty IDs",
				slog.Int("empty_id_count", emptyIDCount),
				slog.Int("valid_id_count", len(newJobIDs)))
		}

		if len(newJobIDs) > 0 {
			// Calculate expected circuits: n*(n-1)/2 for n locations
			n := len(locationsWithSources)
			expectedCircuits := n * (n - 1) / 2

			// Generate circuit labels for storage and metrics
			var circuits []string
			for i, sourceLocation := range locationsWithSources {
				for j, targetLocation := range locationsWithSources {
					if i == j || len(sourceLocation.NearestSources) == 0 || len(targetLocation.NearestSources) == 0 {
						continue
					}
					// Only count if source location name comes before target location name alphabetically
					if sourceLocation.LocationCode >= targetLocation.LocationCode {
						continue
					}
					circuit := circuitLabel(sourceLocation.LocationCode, targetLocation.LocationCode)
					circuits = append(circuits, circuit)
					metrics.LatencySamplesPerCollectionIntervalExpected.WithLabelValues("wheresitup", circuit).Add(1)
				}
			}

			c.log.Info("Wheresitup - Added expected samples metrics",
				slog.Int("expected_circuits", expectedCircuits),
				slog.Int("job_count", len(newJobIDs)))

			c.log.Info("Wheresitup - Storing new job IDs and circuits",
				slog.Int("job_count", len(newJobIDs)),
				slog.Int("circuit_count", len(circuits)),
				slog.String("file", jobIDsFile))

			state := NewState(jobIDsFile)
			if err := state.AddJobIDsWithCircuits(newJobIDs, circuits, jobCreationStart); err != nil {
				c.log.Warn("Wheresitup - Failed to store job IDs and circuits",
					slog.String("file", jobIDsFile),
					slog.Int("job_count", len(newJobIDs)),
					slog.Int("circuit_count", len(circuits)),
					slog.String("error", err.Error()))
			} else {
				c.log.Debug("Wheresitup - Successfully stored job IDs and circuits",
					slog.String("file", jobIDsFile),
					slog.Int("job_count", len(newJobIDs)),
					slog.Int("circuit_count", len(circuits)),
					slog.Any("circuits", circuits))
			}
		} else {
			c.log.Warn("Wheresitup - No valid job IDs to store - all job responses had empty IDs")
		}
	}

	return nil
}

func (c *Collector) CreateJobsBetweenLocations(ctx context.Context, locations []LocationSourceMatch, dryRun, debug bool) ([]JobResponse, error) {
	var jobs []JobResponse

	for i, sourceLocation := range locations {
		for j, targetLocation := range locations {
			if i == j || len(sourceLocation.NearestSources) == 0 || len(targetLocation.NearestSources) == 0 {
				continue
			}

			// Only create job if source location name comes before target location name alphabetically
			// This ensures we test each pair only once since ping measures round-trip time
			if sourceLocation.LocationCode >= targetLocation.LocationCode {
				continue // Skip this direction - will be covered by the reverse direction
			}

			// Use the first (nearest) source from each location
			sourceName := sourceLocation.NearestSources[0].Name
			targetName := targetLocation.NearestSources[0].Name

			// Generate target DNS name (city_name.wonderproxy.com)
			targetDNS := fmt.Sprintf("%s.wonderproxy.com", targetName)

			if sourceName == "" {
				c.log.Warn("Empty source name for location, skipping",
					slog.String("location", sourceLocation.LocationCode))
				continue
			}

			jobRequest := map[string]any{
				"uri":     fmt.Sprintf("http://%s", targetDNS),
				"tests":   []string{"ping"},
				"sources": []string{sourceName},
				"options": map[string]any{
					"expire_after": jobExpireAfterParam,
					"label":        fmt.Sprintf("DoubleZero: %s to %s", sourceLocation.LocationCode, targetLocation.LocationCode),
					"timeout":      int(RequestTimeout.Seconds()),
				},
			}

			if dryRun {
				c.log.Debug("Would create job (dry run)",
					slog.String("source_location", sourceLocation.LocationCode),
					slog.String("source_name", sourceName),
					slog.String("target_location", targetLocation.LocationCode),
					slog.String("target_dns", targetDNS))

				requestJSON, _ := json.MarshalIndent(jobRequest, "", "  ")
				c.log.Debug("Request JSON",
					slog.String("json", string(requestJSON)))
			} else {
				c.log.Debug("Creating job",
					slog.String("source_location", sourceLocation.LocationCode),
					slog.String("source_name", sourceName),
					slog.String("target_location", targetLocation.LocationCode),
					slog.String("target_dns", targetDNS))

				jobResponse, err := c.client.CreateJobWithRequest(ctx, jobRequest, debug)
				if err != nil {
					c.log.Warn("Error creating job",
						slog.String("source_location", sourceLocation.LocationCode),
						slog.String("target_location", targetLocation.LocationCode),
						slog.String("error", err.Error()))
					continue
				}

				c.log.Debug("Job creation response",
					slog.String("job_id", jobResponse.ID),
					slog.String("status", jobResponse.Status),
					slog.String("created", jobResponse.Created),
					slog.String("expires", jobResponse.Expires),
					slog.String("source_location", sourceLocation.LocationCode),
					slog.String("target_location", targetLocation.LocationCode))

				if jobResponse.ID == "" {
					c.log.Warn("API returned job response with empty ID",
						slog.String("status", jobResponse.Status),
						slog.String("created", jobResponse.Created),
						slog.String("expires", jobResponse.Expires),
						slog.String("source_location", sourceLocation.LocationCode),
						slog.String("target_location", targetLocation.LocationCode))
				}

				jobs = append(jobs, *jobResponse)
				if debug {
					c.log.Info("Created job",
						slog.String("job_id", jobResponse.ID),
						slog.String("source_location", sourceLocation.LocationCode),
						slog.String("target_location", targetLocation.LocationCode))
				}

				// Add delay to avoid rate limiting
				time.Sleep(CallDelay)
			}
		}
	}

	return jobs, nil
}

func (c *Collector) ListJobs(ctx context.Context) error {
	jobs, err := c.client.GetAllJobs(ctx)
	if err != nil {
		return collector.NewAPIError("get_jobs", "failed to get jobs", err)
	}

	if len(jobs) == 0 {
		fmt.Println("No jobs found.")
		return nil
	}

	fmt.Printf("Found %d jobs:\n", len(jobs))
	fmt.Println("\nJob ID\t\t\t\tLocation A -> Location Z\t\tChecks\t\tCreated\t\t\tExpires\t\t\tURL")
	fmt.Println("------\t\t\t\t------------------------\t\t------\t\t-------\t\t\t-------\t\t\t---")

	for _, job := range jobs {
		locationA, locationZ := c.parseLocationCodesFromJobDetails(job)

		checks := c.extractChecksFromJobDetails(job)

		created := c.formatTimestampFromUnix(job.StartTime)
		expires := c.formatTimestampFromUnix(job.Expiry.Sec)

		fmt.Printf("%s\t%s -> %s\t\t%s\t\t%s\t%s\t%s\n",
			job.ID, locationA, locationZ, checks, created, expires, job.URL)
	}

	return nil
}

func (c *Collector) extractChecksFromJobDetails(job JobDetails) string {
	var allChecks []string

	checkSet := make(map[string]bool)
	for _, service := range job.Services {
		for _, check := range service.Checks {
			if !checkSet[check] {
				checkSet[check] = true
				allChecks = append(allChecks, check)
			}
		}
	}

	if len(allChecks) == 0 {
		return "None"
	}

	return strings.Join(allChecks, ",")
}

func (c *Collector) formatTimestampFromUnix(unixTime int64) string {
	t := time.Unix(unixTime, 0).UTC()
	return t.Format(collector.TimeFormatMicroseconds)
}

type LocationInfo struct {
	LocationCode string
}

// buildLocationMapping creates a mapping from Wheresitup source names to DoubleZero locations
func (c *Collector) buildLocationMapping(ctx context.Context, locations []collector.LocationMatch) (map[string]LocationInfo, error) {
	locationMatches, err := c.client.GetNearestSourcesForLocations(ctx, locations)
	if err != nil {
		return nil, collector.NewAPIError("get_nearest_sources", "failed to get nearest sources", err)
	}

	mapping := make(map[string]LocationInfo)
	for _, locationMatch := range locationMatches {
		if len(locationMatch.NearestSources) == 0 {
			continue
		}

		// We no longer have location pub keys since we're not using devices
		// Just map the source names to location codes
		for _, source := range locationMatch.NearestSources {
			mapping[source.Name] = LocationInfo{
				LocationCode: locationMatch.LocationCode,
			}
		}
	}

	return mapping, nil
}

func (c *Collector) ExportJobResults(ctx context.Context, jobIDsFile string) error {
	state := NewState(jobIDsFile)
	if err := state.Load(); err != nil {
		return err
	}

	// Captured before pruning, which clears Circuits once it empties the job list.
	circuitExpectedSamples := make(map[string]bool)
	for _, circuit := range state.Circuits {
		circuitExpectedSamples[circuit] = true
	}
	c.log.Debug("Wheresitup - Loaded expected circuits",
		slog.Int("circuit_count", len(circuitExpectedSamples)),
		slog.Any("circuits", state.Circuits))

	// Polling an expired job can only return in_progress, and the time it costs is what pushes
	// the next batch past its own expiry, so dropping them stops that loop forming. Persisted
	// here, not with the completed jobs below, so it survives an early return on write failure.
	expiredJobIDs := state.PruneExpired(time.Now())
	if len(expiredJobIDs) > 0 {
		metrics.WheresitupExpiredJobsTotal.Add(float64(len(expiredJobIDs)))
		c.log.Info("Wheresitup - Dropping expired jobs from tracking without polling",
			slog.Int("expired_count", len(expiredJobIDs)),
			slog.Duration("max_job_age", MaxJobAge))
		if err := state.Save(); err != nil {
			c.log.Warn("Wheresitup failed to drop expired job IDs",
				slog.String("file", jobIDsFile),
				slog.Int("expired_count", len(expiredJobIDs)),
				slog.String("error", err.Error()))
		}
	}

	// Newest batch first: the poll set holds several batches when the vendor is slow, and only
	// the newest is certain to still have results, so a pass cut short sheds the stale end.
	jobIDs := make([]string, 0, len(state.Jobs))
	for i := len(state.Jobs) - 1; i >= 0; i-- {
		jobIDs = append(jobIDs, state.Jobs[i].JobID)
	}

	if len(jobIDs) == 0 && len(expiredJobIDs) == 0 {
		c.log.Info("No tracked jobs found to export")
		return nil
	}

	c.log.Info("Found tracked jobs to check", slog.Int("job_count", len(jobIDs)))

	var locationMap map[string]LocationInfo
	if len(jobIDs) > 0 {
		// Only labels the records below, and costs a serviceability program scan plus a
		// source-list fetch per location, so nothing above this point may need it.
		locations := c.getLocationsFunc(ctx)
		var err error
		locationMap, err = c.buildLocationMapping(ctx, locations)
		if err != nil {
			return collector.NewValidationError("build_location_mapping", "failed to build location mapping", err).
				WithContext("location_count", len(locations))
		}

		// GetLocations fails open with an empty slice when the ledger fetch fails, and every
		// record would then be labelled Unknown, dropped by the exporter without an error, and
		// its job removed as completed. Stopping here costs one cycle of polling; going on
		// would consume every resident job's results, which at this retention is about ten
		// cycles' worth.
		if len(locationMap) == 0 {
			c.log.Error("Wheresitup - No location mapping, keeping tracked jobs for the next cycle",
				slog.Int("job_count", len(jobIDs)),
				slog.Int("location_count", len(locations)))
			c.trackMissingSamples(circuitExpectedSamples, nil)
			return collector.NewValidationError("build_location_mapping", "empty location mapping", nil).
				WithContext("job_count", len(jobIDs)).
				WithContext("location_count", len(locations))
		}
	}

	processedCount := 0
	failedCount := 0
	inProgressCount := 0
	var completedJobIDs []string

	records := make([]exporter.Record, 0, len(jobIDs))
	for _, jobID := range jobIDs {
		if ctx.Err() != nil {
			c.log.Info("Wheresitup - Stopping job export early",
				slog.Int("polled_count", processedCount+failedCount+inProgressCount),
				slog.Int("job_count", len(jobIDs)),
				slog.String("reason", ctx.Err().Error()))
			break
		}

		c.log.Debug("Processing job", slog.String("job_id", jobID))

		apiStart := time.Now()
		results, err := c.client.GetJobResults(ctx, jobID)
		metrics.WheresitupAPIResponseDuration.Observe(time.Since(apiStart).Seconds())
		if err != nil {
			// Handle the fact that the wheresitup response section, "complete", "error", and "in_progress"
			// are arrays when empty and maps when populated. In the example below, once the job is complete,
			// the "complete" field will become a map and the "in_progress" field will become an empty array.
			//     "response": {
			//         "complete": [],
			//         "error": [],
			//         "in_progress": {
			//             "<source>": {
			//                 "<test>": "in progress"
			//             }
			//         }
			//     }
			if strings.Contains(err.Error(), "cannot unmarshal array into Go struct field .response.complete") {
				c.log.Debug("Job appears to be in progress (complete field is array), skipping",
					slog.String("job_id", jobID))
				inProgressCount++
				continue
			}
			c.log.Info("Wheresitup failed to get results for job",
				slog.String("job_id", jobID),
				slog.String("error", err.Error()))
			failedCount++
			continue
		}

		if len(results.Response.Complete) == 0 && len(results.Response.InProgress) > 0 {
			c.log.Debug("Wheresitup job still in progress, skipping",
				slog.String("job_id", jobID),
				slog.Int("in_progress_count", len(results.Response.InProgress)))
			inProgressCount++
			continue
		}

		if len(results.Response.Complete) == 0 {
			c.log.Info("Wheresitup job has no completed results, removing from tracking",
				slog.String("job_id", jobID))
			completedJobIDs = append(completedJobIDs, jobID)
			continue
		}

		sourceLocation, targetLocation := c.parseLocationInfoFromJobResults(results, locationMap)

		var minLatency string
		for _, serviceResult := range results.Response.Complete {
			if serviceResult.Ping.Summary.Summary.Min != "" {
				minLatency = serviceResult.Ping.Summary.Summary.Min
				break // Use the first available min latency
			}
		}

		if minLatency == "" {
			c.log.Info("Wheresitup - no min latency found for job",
				slog.String("job_id", jobID))
			completedJobIDs = append(completedJobIDs, jobID)
			continue
		}

		latency, err := parseMillisString(minLatency)
		if err != nil {
			c.log.Info("Wheresitup - failed to parse min latency",
				slog.String("job_id", jobID),
				slog.String("error", err.Error()))
			continue
		}

		completedJobIDs = append(completedJobIDs, jobID)

		records = append(records, exporter.Record{
			DataProvider:       exporter.DataProviderNameWheresitup,
			SourceExchangeCode: sourceLocation,
			TargetExchangeCode: targetLocation,
			Timestamp:          time.Unix(results.Request.StartTime, 0).UTC(),
			RTT:                latency,
		})

		processedCount++

		// Add delay to avoid rate limiting
		time.Sleep(CallDelay)
	}

	// Polling is newest-first and the exporter does not reorder, but the ledger derives each
	// sample's timestamp from its position in the account, so time order has to be restored.
	slices.SortStableFunc(records, func(a, b exporter.Record) int {
		return a.Timestamp.Compare(b.Timestamp)
	})

	circuitActualSamples := make(map[string]int)
	for _, record := range records {
		circuitActualSamples[circuitLabel(record.SourceExchangeCode, record.TargetExchangeCode)]++
	}

	// Write the batch of records with the exporter.
	if len(records) > 0 {
		if err := c.exporter.WriteRecords(ctx, records); err != nil {
			c.log.Warn("Wheresitup failed to write records", "error", err.Error(), "records", len(records))
			return fmt.Errorf("failed to write records: %w", err)
		}
		for circuit, count := range circuitActualSamples {
			metrics.LatencySamplesPerCollectionIntervalActual.WithLabelValues("wheresitup", circuit).Add(float64(count))
		}
		c.log.Info("Wheresitup - Added actual samples metrics",
			slog.Int("actual_samples", len(records)),
			slog.Int("circuits", len(circuitActualSamples)))
	}

	c.trackMissingSamples(circuitExpectedSamples, circuitActualSamples)

	if len(completedJobIDs) > 0 {
		if err := state.RemoveJobIDs(completedJobIDs); err != nil {
			c.log.Warn("Wheresitup failed to remove completed job IDs",
				slog.String("file", jobIDsFile),
				slog.Int("job_count", len(completedJobIDs)),
				slog.String("error", err.Error()))
		}
	}

	// Calculate failure rate and log appropriately
	totalJobs := len(jobIDs)
	pendingJobs := totalJobs - len(completedJobIDs) - failedCount
	var failureRate float64
	if totalJobs > 0 {
		failureRate = float64(failedCount) / float64(totalJobs)
	}

	// Update pending jobs gauge for Prometheus
	metrics.WheresitupPendingJobs.Set(float64(pendingJobs))

	// failureRate is zero unless totalJobs > 0, so this branch already implies it.
	if failureRate > 0.10 {
		// Log error if more than 10% of jobs failed
		c.log.Error("High failure rate for Wheresitup job results",
			slog.Int("processed_count", processedCount),
			slog.Int("failed_count", failedCount),
			slog.Int("in_progress_count", inProgressCount),
			slog.Int("expired_count", len(expiredJobIDs)),
			slog.Int("pending_jobs", pendingJobs),
			slog.Int("total_jobs", totalJobs),
			slog.Int("removed_job_count", len(completedJobIDs)),
			slog.Float64("failure_rate", failureRate))
	} else {
		// Normal info log when failure rate is acceptable
		c.log.Info("Operation completed: Wheresitup export_job_results",
			slog.Int("processed_count", processedCount),
			slog.Int("failed_count", failedCount),
			slog.Int("in_progress_count", inProgressCount),
			slog.Int("expired_count", len(expiredJobIDs)),
			slog.Int("pending_jobs", pendingJobs),
			slog.Int("removed_job_count", len(completedJobIDs)),
			slog.Int("total_jobs", totalJobs),
			slog.Float64("failure_rate", failureRate))
	}

	return nil
}

// trackMissingSamples reports the circuits job creation expected but the pass did not export.
// Called outside any record-count guard, as in ripeatlas: the expected counter was already
// incremented for these circuits, so a cycle that exports nothing is precisely the one that
// has to report them missing.
func (c *Collector) trackMissingSamples(expected map[string]bool, actual map[string]int) {
	missingSamples := 0
	for circuit := range expected {
		if _, exists := actual[circuit]; !exists {
			metrics.LatencySamplesPerCollectionIntervalMissing.WithLabelValues(c.env, circuit, "wheresitup").Add(1)
			missingSamples++
		}
	}
	if missingSamples > 0 {
		c.log.Info("Wheresitup - Tracked missing samples",
			slog.Int("missing_samples", missingSamples),
			slog.Int("expected_circuits", len(expected)),
			slog.Int("actual_circuits", len(actual)))
	}
}

// circuitLabel orders the two exchanges alphabetically, so the expected, actual and missing
// sample metrics all agree on one label per pair.
func circuitLabel(sourceExchange, targetExchange string) string {
	if sourceExchange < targetExchange {
		return fmt.Sprintf("%s → %s", sourceExchange, targetExchange)
	}
	return fmt.Sprintf("%s → %s", targetExchange, sourceExchange)
}

func parseMillisString(s string) (time.Duration, error) {
	ms, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	return time.Duration(ms*1000) * time.Microsecond, nil
}

func (c *Collector) parseLocationCodesFromJobDetails(job JobDetails) (string, string) {
	sourceLocation := parseLocationFromUrl(job.URL)

	var targetLocation string
	if len(job.Services) > 0 {
		targetLocation = job.Services[0].City
	}

	if sourceLocation == "" {
		sourceLocation = "Unknown"
	}
	if targetLocation == "" {
		targetLocation = "Unknown"
	}

	return sourceLocation, targetLocation
}

func (c *Collector) parseLocationInfoFromJobResults(results *JobResultResponse, locationMap map[string]LocationInfo) (string, string) {
	var sourceLocation, targetLocation string

	wheresitupTargetName := parseLocationFromUrl(results.Request.URL)

	var wheresitupSourceName string
	for sourceName := range results.Response.Complete {
		wheresitupSourceName = sourceName // e.g., "secaucus"
		break                             // Use the first (and likely only) source
	}

	// Map Wheresitup names to DoubleZero location info
	if sourceInfo, exists := locationMap[wheresitupSourceName]; exists {
		sourceLocation = sourceInfo.LocationCode
	} else {
		sourceLocation = "Unknown"
	}

	if targetInfo, exists := locationMap[wheresitupTargetName]; exists {
		targetLocation = targetInfo.LocationCode
	} else {
		targetLocation = "Unknown"
	}

	return sourceLocation, targetLocation
}

func parseLocationFromUrl(url string) string {
	if strings.Contains(url, ".wonderproxy.com") {
		urlParts := strings.Split(url, "://")
		if len(urlParts) == 2 {
			hostParts := strings.Split(urlParts[1], ".")
			if len(hostParts) > 0 {
				return hostParts[0]
			}
		}
	}
	return "Unknown"
}

func (c *Collector) Run(ctx context.Context, interval time.Duration, dryRun bool, jobIDsFile, stateDir string) error {
	fullJobIDsPath := filepath.Join(stateDir, jobIDsFile)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			c.log.Info("Stopping Wheresitup job creation")
			return nil
		case <-ticker.C:
			cycleStart := time.Now()
			c.log.Info("Running Wheresitup job creation cycle")
			locations := c.getLocationsFunc(ctx)
			if err := c.RunJobCreation(ctx, locations, dryRun, fullJobIDsPath); err != nil {
				c.log.Error("Operation failed: Wheresitup run_job_creation", slog.String("error", err.Error()))
				metrics.WheresitupJobCreationFailuresTotal.Inc()
			} else {
				metrics.WheresitupJobCreationRunsTotal.Inc()
				// Wait for jobs to start and potentially complete
				c.log.Info("Waiting before exporting Wheresitup job results",
					slog.Int("wait_seconds", int(c.jobWaitTimeout.Seconds())))
				time.Sleep(c.jobWaitTimeout)

				// Export job results
				c.log.Info("Exporting Wheresitup job results")
				exportStart := time.Now()
				if err := c.ExportJobResults(ctx, fullJobIDsPath); err != nil {
					c.log.Error("Operation failed: Wheresitup export_job_results", slog.String("error", err.Error()))
					metrics.CollectionFailuresTotal.WithLabelValues("wheresitup").Inc()
				} else {
					exportDuration := time.Since(exportStart)
					metrics.RunDurationSeconds.WithLabelValues("wheresitup", "export").Observe(exportDuration.Seconds())
					c.log.Info("Wheresitup job export completed",
						slog.Duration("duration", exportDuration))
					metrics.CollectionRunsTotal.WithLabelValues("wheresitup").Inc()
				}
			}

			cycleDuration := time.Since(cycleStart)
			if cycleDuration >= interval {
				c.log.Warn("Wheresitup collection cycle exceeded sampling interval",
					slog.Duration("cycle_duration", cycleDuration),
					slog.Duration("sampling_interval", interval),
					slog.Duration("overrun", cycleDuration-interval))
			}
		}
	}
}
