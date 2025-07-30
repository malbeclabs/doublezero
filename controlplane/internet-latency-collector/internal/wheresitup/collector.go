package wheresitup

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/internal/collector"
	"github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/internal/exporter"
)

const (
	RequestTimeout         = 30 * time.Second // Timeout for job requests
	CreditWarningThreshold = 10000
	ExpireAfter            = "10 minutes" // Relative time like "1 hour" - https://wheresitup.com/docs/?shell#creating-jobs
)

type Collector struct {
	client           clientInterface
	log              *slog.Logger
	jobWaitTimeout   time.Duration // Duration to wait between job creation and export
	getLocationsFunc func(ctx context.Context) []collector.LocationMatch
	exporter         exporter.Exporter
}

type clientInterface interface {
	GetAllSources(ctx context.Context) ([]Source, error)
	GetNearestSources(ctx context.Context, latitude, longitude float64, count int) ([]Source, error)
	GetNearestSourcesForLocations(ctx context.Context, locations []collector.LocationMatch) ([]LocationSourceMatch, error)
	CreateJob(ctx context.Context, url string) (string, error)
	CreateJobWithRequest(ctx context.Context, request any, debug bool) (*JobResponse, error)
	GetJobResults(ctx context.Context, jobID string) (*JobResultResponse, error)
	GetAllJobs(ctx context.Context) ([]JobDetails, error)
	GetCredit(ctx context.Context) (int, error)
}

func NewCollector(logger *slog.Logger, exporter exporter.Exporter, getLocationsFunc func(ctx context.Context) []collector.LocationMatch) *Collector {
	return &Collector{
		client:           NewClient(logger),
		log:              logger,
		jobWaitTimeout:   RequestTimeout, // Default to 30 seconds
		getLocationsFunc: getLocationsFunc,
		exporter:         exporter,
	}
}

// SetJobWaitTimeout sets the duration to wait between job creation and export.
// Used for testing to avoid waiting 30 seconds.
func (c *Collector) SetJobWaitTimeout(timeout time.Duration) {
	c.jobWaitTimeout = timeout
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
		if credit < CreditWarningThreshold {
			c.log.Warn("Low Wheresitup credit balance",
				slog.Int("credits", credit),
				slog.Int("threshold", CreditWarningThreshold))
		}
	}

	expireAfter := "1 hour"
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
		}
	}

	if len(locationsWithSources) < 2 {
		return collector.ErrInsufficientSources.WithContext("found_locations", len(locationsWithSources)).
			WithContext("minimum_required", 2)
	}

	c.log.Info(
		"Wheresitup creating ping jobs between locations",
		slog.Int("location_count", len(locationsWithSources)),
		slog.String("expire_after", expireAfter))

	jobResponses, err := c.CreateJobsBetweenLocations(ctx, locationsWithSources, dryRun, false)
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

			c.log.Info("Wheresitup - Storing new job IDs",
				slog.Int("job_count", len(newJobIDs)),
				slog.String("file", jobIDsFile))

			state := NewState(jobIDsFile)
			if err := state.AddJobIDs(newJobIDs); err != nil {
				c.log.Warn("Wheresitup - Failed to store job IDs",
					slog.String("file", jobIDsFile),
					slog.Int("job_count", len(newJobIDs)),
					slog.String("error", err.Error()))
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
					"expire_after": ExpireAfter,
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

func (c *Collector) ExportJobResults(ctx context.Context, jobIDsFile, outputDir string) error {
	locations := c.getLocationsFunc(ctx)
	locationMap, err := c.buildLocationMapping(ctx, locations)
	if err != nil {
		return collector.NewValidationError("build_location_mapping", "failed to build location mapping", err).
			WithContext("location_count", len(locations))
	}

	state := NewState(jobIDsFile)
	if err := state.Load(); err != nil {
		return err
	}
	jobIDs := state.GetJobIDs()

	if len(jobIDs) == 0 {
		c.log.Info("No tracked jobs found to export")
		return nil
	}

	c.log.Info("Found tracked jobs to check", slog.Int("job_count", len(jobIDs)))

	processedCount := 0
	var completedJobIDs []string

	records := make([]exporter.Record, 0, len(jobIDs))
	for _, jobID := range jobIDs {
		c.log.Debug("Processing job", slog.String("job_id", jobID))

		results, err := c.client.GetJobResults(ctx, jobID)
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
				continue
			}
			c.log.Warn("Wheresitup failed to get results for job",
				slog.String("job_id", jobID),
				slog.String("error", err.Error()))
			continue
		}

		if len(results.Response.Complete) == 0 && len(results.Response.InProgress) > 0 {
			c.log.Debug("Wheresitup job still in progress, skipping",
				slog.String("job_id", jobID),
				slog.Int("in_progress_count", len(results.Response.InProgress)))
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
			c.log.Warn("Wheresitup - no min latency found for job",
				slog.String("job_id", jobID))
			completedJobIDs = append(completedJobIDs, jobID)
			continue
		}

		latency, err := parseMillisString(minLatency)
		if err != nil {
			c.log.Warn("Wheresitup - failed to parse min latency",
				slog.String("job_id", jobID),
				slog.String("error", err.Error()))
			continue
		}

		records = append(records, exporter.Record{
			DataProvider:       exporter.DataProviderNameWheresitup,
			SourceLocationCode: sourceLocation,
			TargetLocationCode: targetLocation,
			Timestamp:          time.Unix(results.Request.StartTime, 0).UTC(),
			RTT:                latency,
		})

		completedJobIDs = append(completedJobIDs, jobID)
		processedCount++

		// Add delay to avoid rate limiting
		time.Sleep(CallDelay)
	}

	// Write the batch of records with the exporter.
	if len(records) > 0 {
		if err := c.exporter.WriteRecords(ctx, records); err != nil {
			c.log.Warn("Wheresitup failed to write records", "error", err.Error(), "records", len(records))
			return fmt.Errorf("failed to write records: %w", err)
		}
	}

	if len(completedJobIDs) > 0 {
		if err := state.RemoveJobIDs(completedJobIDs); err != nil {
			c.log.Warn("Wheresitup failed to remove completed job IDs",
				slog.String("file", jobIDsFile),
				slog.Int("job_count", len(completedJobIDs)),
				slog.String("error", err.Error()))
		}
	}

	c.log.Info("Operation completed: Wheresitup export_job_results",
		slog.Int("processed_count", processedCount),
		slog.Int("removed_job_count", len(completedJobIDs)))

	return nil
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

func (c *Collector) Run(ctx context.Context, interval time.Duration, dryRun bool, jobIDsFile, stateDir, outputDir string) error {
	fullJobIDsPath := filepath.Join(stateDir, jobIDsFile)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			c.log.Info("Stopping Wheresitup job creation")
			return nil
		case <-ticker.C:
			c.log.Info("Running Wheresitup job creation cycle")
			locations := c.getLocationsFunc(ctx)
			if err := c.RunJobCreation(ctx, locations, dryRun, fullJobIDsPath); err != nil {
				c.log.Error("Operation failed: Wheresitup run_job_creation", slog.String("error", err.Error()))
			} else {
				// Wait for jobs to start and potentially complete
				c.log.Info("Waiting before exporting Wheresitup job results",
					slog.Int("wait_seconds", int(c.jobWaitTimeout.Seconds())))
				time.Sleep(c.jobWaitTimeout)

				// Export job results
				c.log.Info("Exporting Wheresitup job results")
				if err := c.ExportJobResults(ctx, fullJobIDsPath, outputDir); err != nil {
					c.log.Error("Operation failed: Wheresitup export_job_results", slog.String("error", err.Error()))
				}
			}
		}
	}
}
