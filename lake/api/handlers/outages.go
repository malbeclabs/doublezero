package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/malbeclabs/doublezero/lake/api/config"
)

// LinkOutage represents a discrete outage event on a link
type LinkOutage struct {
	ID              string   `json:"id"`
	LinkPK          string   `json:"link_pk"`
	LinkCode        string   `json:"link_code"`
	LinkType        string   `json:"link_type"`
	SideAMetro      string   `json:"side_a_metro"`
	SideZMetro      string   `json:"side_z_metro"`
	ContributorCode string   `json:"contributor_code"`
	OutageType      string   `json:"outage_type"` // "status" or "packet_loss"
	PreviousStatus  *string  `json:"previous_status,omitempty"`
	NewStatus       *string  `json:"new_status,omitempty"`
	ThresholdPct    *float64 `json:"threshold_pct,omitempty"`
	PeakLossPct     *float64 `json:"peak_loss_pct,omitempty"`
	StartedAt       string   `json:"started_at"`
	EndedAt         *string  `json:"ended_at,omitempty"`
	DurationSeconds *int64   `json:"duration_seconds,omitempty"`
	IsOngoing       bool     `json:"is_ongoing"`
}

// LinkOutagesSummary contains aggregate counts for outages
type LinkOutagesSummary struct {
	Total   int            `json:"total"`
	Ongoing int            `json:"ongoing"`
	ByType  map[string]int `json:"by_type"`
}

// LinkOutagesResponse is the API response for link outages
type LinkOutagesResponse struct {
	Outages []LinkOutage       `json:"outages"`
	Summary LinkOutagesSummary `json:"summary"`
}

// parseTimeRange converts a time range string to a duration
func parseTimeRange(rangeStr string) time.Duration {
	switch rangeStr {
	case "3h":
		return 3 * time.Hour
	case "6h":
		return 6 * time.Hour
	case "12h":
		return 12 * time.Hour
	case "24h":
		return 24 * time.Hour
	case "3d":
		return 3 * 24 * time.Hour
	case "7d":
		return 7 * 24 * time.Hour
	case "30d":
		return 30 * 24 * time.Hour
	default:
		return 24 * time.Hour
	}
}

// parseThreshold returns the packet loss threshold percentage
func parseThreshold(thresholdStr string) float64 {
	switch thresholdStr {
	case "1":
		return 1.0
	case "10":
		return 10.0
	default:
		return 1.0
	}
}

// OutageFilter represents a filter for outages (e.g., metro:SAO, link:WAN-LAX-01)
type OutageFilter struct {
	Type  string // device, link, metro, contributor
	Value string
}

// parseOutageFilters parses a comma-separated filter string into OutageFilter structs
func parseOutageFilters(filterStr string) []OutageFilter {
	if filterStr == "" {
		return nil
	}
	var filters []OutageFilter
	for _, f := range strings.Split(filterStr, ",") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		parts := strings.SplitN(f, ":", 2)
		if len(parts) == 2 {
			filters = append(filters, OutageFilter{Type: parts[0], Value: parts[1]})
		}
	}
	return filters
}

// isDefaultOutagesRequest checks if the request matches the default cached parameters
func isDefaultOutagesRequest(r *http.Request) bool {
	q := r.URL.Query()

	// Must be 24h range (default) or no range specified
	rangeParam := q.Get("range")
	if rangeParam != "" && rangeParam != "24h" {
		return false
	}

	// Must be threshold=1 (default) or no threshold specified
	threshold := q.Get("threshold")
	if threshold != "" && threshold != "1" {
		return false
	}

	// Must be type=all (default) or no type specified
	outageType := q.Get("type")
	if outageType != "" && outageType != "all" {
		return false
	}

	// Must not have any filters
	if q.Get("filter") != "" {
		return false
	}

	return true
}

// GetLinkOutages returns discrete outage events for links
func GetLinkOutages(w http.ResponseWriter, r *http.Request) {
	// Check if this is a default request that can be served from cache
	if isDefaultOutagesRequest(r) && statusCache != nil {
		if cached := statusCache.GetOutages(); cached != nil {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Cache", "HIT")
			if err := json.NewEncoder(w).Encode(cached); err != nil {
				log.Printf("Error encoding cached outages response: %v", err)
			}
			return
		}
	}

	// Parse query parameters
	timeRange := r.URL.Query().Get("range")
	if timeRange == "" {
		timeRange = "24h"
	}
	duration := parseTimeRange(timeRange)

	thresholdStr := r.URL.Query().Get("threshold")
	threshold := parseThreshold(thresholdStr)

	outageType := r.URL.Query().Get("type")
	if outageType == "" {
		outageType = "all"
	}

	// Parse filters (format: type:value,type:value)
	filterStr := r.URL.Query().Get("filter")
	filters := parseOutageFilters(filterStr)

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var outages []LinkOutage

	// Fetch status-based outages (drained states)
	if outageType == "all" || outageType == "status" {
		statusOutages, err := fetchStatusOutages(ctx, config.DB, duration, filters)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to fetch status outages: %v", err), http.StatusInternalServerError)
			return
		}
		outages = append(outages, statusOutages...)
	}

	// Fetch packet loss outages
	if outageType == "all" || outageType == "loss" {
		lossOutages, err := fetchPacketLossOutages(ctx, config.DB, duration, threshold, filters)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to fetch packet loss outages: %v", err), http.StatusInternalServerError)
			return
		}
		outages = append(outages, lossOutages...)
	}

	// Fetch no-data outages (links that stopped reporting telemetry)
	if outageType == "all" || outageType == "no_data" {
		noDataOutages, err := fetchNoDataOutages(ctx, config.DB, duration, filters)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to fetch no-data outages: %v", err), http.StatusInternalServerError)
			return
		}
		outages = append(outages, noDataOutages...)
	}

	// Sort by start time (most recent first)
	sort.Slice(outages, func(i, j int) bool {
		return outages[i].StartedAt > outages[j].StartedAt
	})

	// Build summary
	summary := LinkOutagesSummary{
		Total:   len(outages),
		Ongoing: 0,
		ByType:  map[string]int{"status": 0, "packet_loss": 0, "no_data": 0},
	}
	for _, o := range outages {
		if o.IsOngoing {
			summary.Ongoing++
		}
		summary.ByType[o.OutageType]++
	}

	response := LinkOutagesResponse{
		Outages: outages,
		Summary: summary,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// statusChange represents a link status change event
type statusChange struct {
	LinkPK         string
	LinkCode       string
	PreviousStatus string
	NewStatus      string
	ChangedTS      time.Time
	SideAMetro     string
	SideZMetro     string
	LinkType       string
	ContributorCode string
}

func fetchStatusOutages(ctx context.Context, conn driver.Conn, duration time.Duration, filters []OutageFilter) ([]LinkOutage, error) {
	// Check if we need device filtering (requires additional joins)
	needsDeviceJoin := false
	for _, f := range filters {
		if f.Type == "device" {
			needsDeviceJoin = true
			break
		}
	}

	// First, get all currently drained links (ongoing outages) - these should always show
	// regardless of when the outage started
	ongoingOutages, err := fetchCurrentlyDrainedLinks(ctx, conn, filters)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch currently drained links: %w", err)
	}

	// Track which links have ongoing outages so we don't duplicate
	ongoingLinkPKs := make(map[string]bool)
	for _, o := range ongoingOutages {
		// Extract link PK from the outage (we'll need to track this)
		ongoingLinkPKs[o.LinkCode] = true
	}

	// Query for status changes in the time window (for completed outages)
	query := `
		SELECT
			sc.link_pk,
			sc.link_code,
			sc.previous_status,
			sc.new_status,
			sc.changed_ts,
			sc.side_a_metro,
			sc.side_z_metro,
			l.link_type,
			COALESCE(c.code, '') AS contributor_code
		FROM dz_link_status_changes sc
		JOIN dz_links_current l ON sc.link_pk = l.pk
		LEFT JOIN dz_contributors_current c ON l.contributor_pk = c.pk
	`

	if needsDeviceJoin {
		query += `
		LEFT JOIN dz_devices_current da ON l.side_a_pk = da.pk
		LEFT JOIN dz_devices_current dz ON l.side_z_pk = dz.pk
		`
	}

	query += " WHERE sc.changed_ts >= now() - INTERVAL $1 SECOND"

	args := []interface{}{int64(duration.Seconds())}
	argIdx := 2

	// Apply filters
	for _, f := range filters {
		switch f.Type {
		case "metro":
			query += fmt.Sprintf(" AND (sc.side_a_metro = $%d OR sc.side_z_metro = $%d)", argIdx, argIdx)
			args = append(args, f.Value)
			argIdx++
		case "link":
			query += fmt.Sprintf(" AND sc.link_code = $%d", argIdx)
			args = append(args, f.Value)
			argIdx++
		case "contributor":
			query += fmt.Sprintf(" AND c.code = $%d", argIdx)
			args = append(args, f.Value)
			argIdx++
		case "device":
			query += fmt.Sprintf(" AND (da.code = $%d OR dz.code = $%d)", argIdx, argIdx)
			args = append(args, f.Value)
			argIdx++
		}
	}

	query += " ORDER BY sc.link_pk, sc.changed_ts"

	rows, err := conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	var changes []statusChange
	for rows.Next() {
		var sc statusChange
		if err := rows.Scan(
			&sc.LinkPK, &sc.LinkCode, &sc.PreviousStatus, &sc.NewStatus,
			&sc.ChangedTS, &sc.SideAMetro, &sc.SideZMetro, &sc.LinkType, &sc.ContributorCode,
		); err != nil {
			return nil, fmt.Errorf("scan failed: %w", err)
		}
		changes = append(changes, sc)
	}

	// Pair status changes into completed outages (exclude links with ongoing outages)
	completedOutages := pairStatusOutagesCompleted(changes, ongoingLinkPKs)

	// Combine ongoing and completed outages
	allOutages := append(ongoingOutages, completedOutages...)
	return allOutages, nil
}

// fetchCurrentlyDrainedLinks finds all links currently in drained state and when they became drained
func fetchCurrentlyDrainedLinks(ctx context.Context, conn driver.Conn, filters []OutageFilter) ([]LinkOutage, error) {
	// Find links currently in drained state and when they entered that state
	query := `
		WITH current_drained AS (
			SELECT
				l.pk AS link_pk,
				l.code AS link_code,
				l.status AS current_status,
				l.link_type,
				COALESCE(ma.code, '') AS side_a_metro,
				COALESCE(mz.code, '') AS side_z_metro,
				COALESCE(c.code, '') AS contributor_code
			FROM dz_links_current l
			LEFT JOIN dz_devices_current da ON l.side_a_pk = da.pk
			LEFT JOIN dz_devices_current dz ON l.side_z_pk = dz.pk
			LEFT JOIN dz_metros_current ma ON da.metro_pk = ma.pk
			LEFT JOIN dz_metros_current mz ON dz.metro_pk = mz.pk
			LEFT JOIN dz_contributors_current c ON l.contributor_pk = c.pk
			WHERE l.status IN ('soft-drained', 'hard-drained')
	`

	var args []interface{}
	argIdx := 1

	// Apply filters
	for _, f := range filters {
		switch f.Type {
		case "metro":
			query += fmt.Sprintf(" AND (ma.code = $%d OR mz.code = $%d)", argIdx, argIdx)
			args = append(args, f.Value)
			argIdx++
		case "link":
			query += fmt.Sprintf(" AND l.code = $%d", argIdx)
			args = append(args, f.Value)
			argIdx++
		case "contributor":
			query += fmt.Sprintf(" AND c.code = $%d", argIdx)
			args = append(args, f.Value)
			argIdx++
		case "device":
			query += fmt.Sprintf(" AND (da.code = $%d OR dz.code = $%d)", argIdx, argIdx)
			args = append(args, f.Value)
			argIdx++
		}
	}

	query += `
		),
		drain_start AS (
			SELECT
				sc.link_pk,
				sc.previous_status,
				sc.new_status,
				sc.changed_ts,
				ROW_NUMBER() OVER (PARTITION BY sc.link_pk ORDER BY sc.changed_ts DESC) AS rn
			FROM dz_link_status_changes sc
			WHERE sc.link_pk IN (SELECT link_pk FROM current_drained)
			  AND sc.new_status IN ('soft-drained', 'hard-drained')
			  AND sc.previous_status = 'activated'
		)
		SELECT
			cd.link_pk,
			cd.link_code,
			cd.current_status,
			cd.link_type,
			cd.side_a_metro,
			cd.side_z_metro,
			cd.contributor_code,
			COALESCE(ds.previous_status, 'activated') AS previous_status,
			ds.changed_ts AS started_at
		FROM current_drained cd
		LEFT JOIN drain_start ds ON cd.link_pk = ds.link_pk AND ds.rn = 1
	`

	rows, err := conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	var outages []LinkOutage
	outageIDCounter := 0

	for rows.Next() {
		var linkPK, linkCode, currentStatus, linkType, sideAMetro, sideZMetro, contributorCode, previousStatus string
		var startedAt *time.Time

		if err := rows.Scan(&linkPK, &linkCode, &currentStatus, &linkType, &sideAMetro, &sideZMetro, &contributorCode, &previousStatus, &startedAt); err != nil {
			return nil, fmt.Errorf("scan failed: %w", err)
		}

		outageIDCounter++
		prevStatus := previousStatus
		newStatus := currentStatus

		var startedAtStr string
		if startedAt != nil {
			startedAtStr = startedAt.UTC().Format(time.RFC3339)
		} else {
			// If we can't find when it started, use a placeholder
			startedAtStr = "unknown"
		}

		outages = append(outages, LinkOutage{
			ID:              fmt.Sprintf("status-%d", outageIDCounter),
			LinkPK:          linkPK,
			LinkCode:        linkCode,
			LinkType:        linkType,
			SideAMetro:      sideAMetro,
			SideZMetro:      sideZMetro,
			ContributorCode: contributorCode,
			OutageType:      "status",
			PreviousStatus:  &prevStatus,
			NewStatus:       &newStatus,
			StartedAt:       startedAtStr,
			IsOngoing:       true,
		})
	}

	return outages, nil
}

// pairStatusOutagesCompleted pairs status changes into completed outages only
func pairStatusOutagesCompleted(changes []statusChange, excludeLinks map[string]bool) []LinkOutage {
	var outages []LinkOutage
	outageIDCounter := 1000 // Start at 1000 to avoid collision with ongoing outage IDs

	// Group by link
	byLink := make(map[string][]statusChange)
	for _, c := range changes {
		// Skip links that have ongoing outages (they're handled separately)
		if excludeLinks[c.LinkCode] {
			continue
		}
		byLink[c.LinkPK] = append(byLink[c.LinkPK], c)
	}

	for _, linkChanges := range byLink {
		// Sort by time
		sort.Slice(linkChanges, func(i, j int) bool {
			return linkChanges[i].ChangedTS.Before(linkChanges[j].ChangedTS)
		})

		var activeOutage *LinkOutage
		for _, c := range linkChanges {
			isDrainedStatus := c.NewStatus == "soft-drained" || c.NewStatus == "hard-drained"
			wasActivated := c.PreviousStatus == "activated"
			isRecovery := c.NewStatus == "activated" && (c.PreviousStatus == "soft-drained" || c.PreviousStatus == "hard-drained")

			if isDrainedStatus && wasActivated {
				// Start of an outage
				if activeOutage != nil {
					// Close previous outage (shouldn't happen, but handle it)
					outages = append(outages, *activeOutage)
				}
				outageIDCounter++
				prevStatus := c.PreviousStatus
				newStatus := c.NewStatus
				activeOutage = &LinkOutage{
					ID:              fmt.Sprintf("status-%d", outageIDCounter),
					LinkPK:          c.LinkPK,
					LinkCode:        c.LinkCode,
					LinkType:        c.LinkType,
					SideAMetro:      c.SideAMetro,
					SideZMetro:      c.SideZMetro,
					ContributorCode: c.ContributorCode,
					OutageType:      "status",
					PreviousStatus:  &prevStatus,
					NewStatus:       &newStatus,
					StartedAt:       c.ChangedTS.UTC().Format(time.RFC3339),
					IsOngoing:       true,
				}
			} else if isRecovery && activeOutage != nil {
				// End of an outage
				endedAt := c.ChangedTS.UTC().Format(time.RFC3339)
				activeOutage.EndedAt = &endedAt
				activeOutage.IsOngoing = false

				// Calculate duration
				startTime, _ := time.Parse(time.RFC3339, activeOutage.StartedAt)
				durationSecs := int64(c.ChangedTS.Sub(startTime).Seconds())
				activeOutage.DurationSeconds = &durationSecs

				outages = append(outages, *activeOutage)
				activeOutage = nil
			} else if isDrainedStatus && activeOutage != nil {
				// Status change within an outage (e.g., soft-drained -> hard-drained)
				newStatus := c.NewStatus
				activeOutage.NewStatus = &newStatus
			}
		}

		// Don't add incomplete outages here - they're handled by fetchCurrentlyDrainedLinks
		// (activeOutage would be non-nil if there's an ongoing outage, but we've excluded those links)
	}

	return outages
}

// lossBucket represents a 5-minute aggregation of packet loss
type lossBucket struct {
	LinkPK      string
	Bucket      time.Time
	LossPct     float64
	SampleCount uint64
}

func fetchPacketLossOutages(ctx context.Context, conn driver.Conn, duration time.Duration, threshold float64, filters []OutageFilter) ([]LinkOutage, error) {
	// Fetch link metadata (filtering happens here)
	linkMeta, err := fetchLinkMetadata(ctx, conn, filters)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch link metadata: %w", err)
	}

	// First, find all links with CURRENT high packet loss (ongoing outages)
	// This ensures ongoing outages show regardless of time range
	currentLossOutages, err := fetchCurrentHighLossLinks(ctx, conn, threshold, linkMeta)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch current high loss links: %w", err)
	}

	// Track which links have ongoing loss outages
	ongoingLossLinks := make(map[string]bool)
	for _, o := range currentLossOutages {
		ongoingLossLinks[o.LinkCode] = true
	}

	// Query for packet loss buckets within time range (for completed outages)
	query := `
		WITH buckets AS (
			SELECT
				lat.link_pk,
				toStartOfInterval(lat.event_ts, INTERVAL 5 MINUTE) as bucket,
				countIf(lat.loss = true OR lat.rtt_us = 0) * 100.0 / count(*) as loss_pct,
				count(*) as sample_count
			FROM fact_dz_device_link_latency lat
			WHERE lat.event_ts >= now() - INTERVAL $1 SECOND
			  AND lat.link_pk != ''
			GROUP BY lat.link_pk, bucket
			HAVING count(*) >= 3
		)
		SELECT
			b.link_pk,
			b.bucket,
			b.loss_pct,
			b.sample_count
		FROM buckets b
		ORDER BY b.link_pk, b.bucket
	`

	rows, err := conn.Query(ctx, query, int64(duration.Seconds()))
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	var buckets []lossBucket
	for rows.Next() {
		var lb lossBucket
		if err := rows.Scan(&lb.LinkPK, &lb.Bucket, &lb.LossPct, &lb.SampleCount); err != nil {
			return nil, fmt.Errorf("scan failed: %w", err)
		}
		buckets = append(buckets, lb)
	}

	// Detect completed outages within the time range (exclude ongoing ones)
	completedOutages := pairPacketLossOutagesCompleted(buckets, linkMeta, threshold, ongoingLossLinks)

	// Combine ongoing and completed
	allOutages := append(currentLossOutages, completedOutages...)
	return allOutages, nil
}

// fetchCurrentHighLossLinks finds all links currently experiencing packet loss above threshold
func fetchCurrentHighLossLinks(ctx context.Context, conn driver.Conn, threshold float64, linkMeta map[string]linkMetadata) ([]LinkOutage, error) {
	// Get the most recent 10 minutes of data to determine current state
	query := `
		WITH recent_loss AS (
			SELECT
				lat.link_pk,
				countIf(lat.loss = true OR lat.rtt_us = 0) * 100.0 / count(*) as loss_pct,
				count(*) as sample_count,
				max(lat.event_ts) as last_seen
			FROM fact_dz_device_link_latency lat
			WHERE lat.event_ts >= now() - INTERVAL 10 MINUTE
			  AND lat.link_pk != ''
			GROUP BY lat.link_pk
			HAVING count(*) >= 3
		)
		SELECT link_pk, loss_pct, last_seen
		FROM recent_loss
		WHERE loss_pct >= $1
	`

	rows, err := conn.Query(ctx, query, threshold)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	var outages []LinkOutage
	outageIDCounter := 0

	for rows.Next() {
		var linkPK string
		var lossPct float64
		var lastSeen time.Time

		if err := rows.Scan(&linkPK, &lossPct, &lastSeen); err != nil {
			return nil, fmt.Errorf("scan failed: %w", err)
		}

		meta, hasMeta := linkMeta[linkPK]
		if !hasMeta {
			// Link filtered out
			continue
		}

		// Find when this outage started by looking back in history
		startedAt, peakLoss, err := findPacketLossOutageStart(ctx, conn, linkPK, threshold)
		if err != nil {
			// If we can't find the start, use a reasonable default
			startedAt = lastSeen.Add(-10 * time.Minute)
			peakLoss = lossPct
		}

		outageIDCounter++
		thresholdPct := threshold

		outages = append(outages, LinkOutage{
			ID:              fmt.Sprintf("loss-%d", outageIDCounter),
			LinkPK:          linkPK,
			LinkCode:        meta.LinkCode,
			LinkType:        meta.LinkType,
			SideAMetro:      meta.SideAMetro,
			SideZMetro:      meta.SideZMetro,
			ContributorCode: meta.ContributorCode,
			OutageType:      "packet_loss",
			ThresholdPct:    &thresholdPct,
			PeakLossPct:     &peakLoss,
			StartedAt:       startedAt.UTC().Format(time.RFC3339),
			IsOngoing:       true,
		})
	}

	return outages, nil
}

// findPacketLossOutageStart looks back in history to find when the current outage started
func findPacketLossOutageStart(ctx context.Context, conn driver.Conn, linkPK string, threshold float64) (time.Time, float64, error) {
	// Look back up to 30 days to find when loss first went above threshold
	query := `
		WITH buckets AS (
			SELECT
				toStartOfInterval(event_ts, INTERVAL 5 MINUTE) as bucket,
				countIf(loss = true OR rtt_us = 0) * 100.0 / count(*) as loss_pct
			FROM fact_dz_device_link_latency
			WHERE link_pk = $1
			  AND event_ts >= now() - INTERVAL 30 DAY
			GROUP BY bucket
			HAVING count(*) >= 3
			ORDER BY bucket DESC
		),
		with_prev AS (
			SELECT
				bucket,
				loss_pct,
				lagInFrame(loss_pct) OVER (ORDER BY bucket DESC) as next_pct
			FROM buckets
		)
		SELECT bucket, loss_pct
		FROM with_prev
		WHERE loss_pct >= $2 AND (next_pct < $2 OR next_pct IS NULL)
		ORDER BY bucket ASC
		LIMIT 1
	`

	var startBucket time.Time
	var peakLoss float64

	// First find the start
	err := conn.QueryRow(ctx, query, linkPK, threshold).Scan(&startBucket, &peakLoss)
	if err != nil {
		return time.Time{}, 0, err
	}

	// Now find the peak loss during this outage
	peakQuery := `
		SELECT max(loss_pct) as peak_loss FROM (
			SELECT countIf(loss = true OR rtt_us = 0) * 100.0 / count(*) as loss_pct
			FROM fact_dz_device_link_latency
			WHERE link_pk = $1
			  AND event_ts >= $2
			GROUP BY toStartOfInterval(event_ts, INTERVAL 5 MINUTE)
			HAVING count(*) >= 3
		)
	`

	var peak float64
	err = conn.QueryRow(ctx, peakQuery, linkPK, startBucket).Scan(&peak)
	if err == nil && peak > peakLoss {
		peakLoss = peak
	}

	return startBucket, peakLoss, nil
}

// linkMetadata contains link info for enriching outages
type linkMetadata struct {
	LinkPK          string
	LinkCode        string
	LinkType        string
	SideAMetro      string
	SideZMetro      string
	ContributorCode string
}

func fetchLinkMetadata(ctx context.Context, conn driver.Conn, filters []OutageFilter) (map[string]linkMetadata, error) {
	query := `
		SELECT
			l.pk,
			l.code,
			l.link_type,
			COALESCE(ma.code, '') AS side_a_metro,
			COALESCE(mz.code, '') AS side_z_metro,
			COALESCE(c.code, '') AS contributor_code
		FROM dz_links_current l
		LEFT JOIN dz_devices_current da ON l.side_a_pk = da.pk
		LEFT JOIN dz_devices_current dz ON l.side_z_pk = dz.pk
		LEFT JOIN dz_metros_current ma ON da.metro_pk = ma.pk
		LEFT JOIN dz_metros_current mz ON dz.metro_pk = mz.pk
		LEFT JOIN dz_contributors_current c ON l.contributor_pk = c.pk
		WHERE 1=1
	`

	var args []interface{}
	argIdx := 1

	// Apply filters
	for _, f := range filters {
		switch f.Type {
		case "metro":
			query += fmt.Sprintf(" AND (ma.code = $%d OR mz.code = $%d)", argIdx, argIdx)
			args = append(args, f.Value)
			argIdx++
		case "link":
			query += fmt.Sprintf(" AND l.code = $%d", argIdx)
			args = append(args, f.Value)
			argIdx++
		case "contributor":
			query += fmt.Sprintf(" AND c.code = $%d", argIdx)
			args = append(args, f.Value)
			argIdx++
		case "device":
			query += fmt.Sprintf(" AND (da.code = $%d OR dz.code = $%d)", argIdx, argIdx)
			args = append(args, f.Value)
			argIdx++
		}
	}

	rows, err := conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	result := make(map[string]linkMetadata)
	for rows.Next() {
		var lm linkMetadata
		if err := rows.Scan(&lm.LinkPK, &lm.LinkCode, &lm.LinkType, &lm.SideAMetro, &lm.SideZMetro, &lm.ContributorCode); err != nil {
			return nil, fmt.Errorf("scan failed: %w", err)
		}
		result[lm.LinkPK] = lm
	}

	return result, nil
}

// pairPacketLossOutagesCompleted finds completed packet loss outages within the time window
// Links with ongoing outages are excluded (they're handled separately)
func pairPacketLossOutagesCompleted(buckets []lossBucket, linkMeta map[string]linkMetadata, threshold float64, excludeLinks map[string]bool) []LinkOutage {
	var outages []LinkOutage
	outageIDCounter := 1000 // Start high to avoid collision with ongoing IDs

	// Group by link
	byLink := make(map[string][]lossBucket)
	for _, b := range buckets {
		byLink[b.LinkPK] = append(byLink[b.LinkPK], b)
	}

	for linkPK, linkBuckets := range byLink {
		meta, hasMeta := linkMeta[linkPK]
		if !hasMeta {
			// Link not in metadata (filtered out)
			continue
		}

		// Skip links with ongoing outages (handled separately)
		if excludeLinks[meta.LinkCode] {
			continue
		}

		// Sort by time
		sort.Slice(linkBuckets, func(i, j int) bool {
			return linkBuckets[i].Bucket.Before(linkBuckets[j].Bucket)
		})

		var activeOutage *LinkOutage
		var peakLoss float64

		for i, b := range linkBuckets {
			aboveThreshold := b.LossPct >= threshold

			// For the first bucket, check if we're starting in an outage state
			if i == 0 {
				if aboveThreshold {
					// Outage was already in progress at start of window
					outageIDCounter++
					thresholdPct := threshold
					activeOutage = &LinkOutage{
						ID:              fmt.Sprintf("loss-%d", outageIDCounter),
						LinkPK:          linkPK,
						LinkCode:        meta.LinkCode,
						LinkType:        meta.LinkType,
						SideAMetro:      meta.SideAMetro,
						SideZMetro:      meta.SideZMetro,
						ContributorCode: meta.ContributorCode,
						OutageType:      "packet_loss",
						ThresholdPct:    &thresholdPct,
						StartedAt:       b.Bucket.UTC().Format(time.RFC3339),
						IsOngoing:       false, // Will be set properly when it ends
					}
					peakLoss = b.LossPct
				}
				continue
			}

			prevLoss := linkBuckets[i-1].LossPct
			wasAbove := prevLoss >= threshold

			if aboveThreshold && !wasAbove {
				// Start of outage (transition from below to above threshold)
				if activeOutage != nil {
					// Previous outage didn't have a clean end, discard it
					activeOutage = nil
				}
				outageIDCounter++
				thresholdPct := threshold
				activeOutage = &LinkOutage{
					ID:              fmt.Sprintf("loss-%d", outageIDCounter),
					LinkPK:          linkPK,
					LinkCode:        meta.LinkCode,
					LinkType:        meta.LinkType,
					SideAMetro:      meta.SideAMetro,
					SideZMetro:      meta.SideZMetro,
					ContributorCode: meta.ContributorCode,
					OutageType:      "packet_loss",
					ThresholdPct:    &thresholdPct,
					StartedAt:       b.Bucket.UTC().Format(time.RFC3339),
					IsOngoing:       false,
				}
				peakLoss = b.LossPct
			} else if !aboveThreshold && wasAbove && activeOutage != nil {
				// End of outage (use previous bucket as end time since this one is below threshold)
				prevBucket := linkBuckets[i-1]
				endedAt := prevBucket.Bucket.Add(5 * time.Minute).UTC().Format(time.RFC3339)
				activeOutage.EndedAt = &endedAt
				activeOutage.IsOngoing = false
				// Copy peakLoss to avoid pointer aliasing when we reset it
				peak := peakLoss
				activeOutage.PeakLossPct = &peak

				// Calculate duration
				startTime, _ := time.Parse(time.RFC3339, activeOutage.StartedAt)
				endTime := prevBucket.Bucket.Add(5 * time.Minute)
				durationSecs := int64(endTime.Sub(startTime).Seconds())
				activeOutage.DurationSeconds = &durationSecs

				outages = append(outages, *activeOutage)
				activeOutage = nil
				peakLoss = 0
			} else if aboveThreshold && activeOutage != nil {
				// Continuing outage - track peak
				if b.LossPct > peakLoss {
					peakLoss = b.LossPct
				}
			}

			prevLoss = b.LossPct
		}

		// Handle outage that was active at end of time window
		// Since this function only handles completed outages (ongoing links are excluded),
		// if we have an activeOutage here, the link must have recovered but we don't have
		// the exact bucket showing recovery. Use the last bucket's time as the end time.
		if activeOutage != nil && len(linkBuckets) > 0 {
			lastBucket := linkBuckets[len(linkBuckets)-1]
			endedAt := lastBucket.Bucket.Add(5 * time.Minute).UTC().Format(time.RFC3339)
			activeOutage.EndedAt = &endedAt
			activeOutage.IsOngoing = false
			// Copy peakLoss to avoid pointer aliasing
			peak := peakLoss
			activeOutage.PeakLossPct = &peak

			// Calculate duration
			startTime, _ := time.Parse(time.RFC3339, activeOutage.StartedAt)
			endTime := lastBucket.Bucket.Add(5 * time.Minute)
			durationSecs := int64(endTime.Sub(startTime).Seconds())
			activeOutage.DurationSeconds = &durationSecs

			outages = append(outages, *activeOutage)
		}
	}

	return outages
}

// noDataGapThreshold is the minimum gap duration to consider as a "no data" outage
const noDataGapThreshold = 15 * time.Minute

func fetchNoDataOutages(ctx context.Context, conn driver.Conn, duration time.Duration, filters []OutageFilter) ([]LinkOutage, error) {
	// Fetch link metadata (filtering happens here)
	linkMeta, err := fetchLinkMetadata(ctx, conn, filters)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch link metadata: %w", err)
	}

	// First, find links currently not reporting data (ongoing no_data outages)
	currentNoDataOutages, err := fetchCurrentNoDataLinks(ctx, conn, linkMeta)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch current no-data links: %w", err)
	}

	// Track which links have ongoing no-data outages
	ongoingNoDataLinks := make(map[string]bool)
	for _, o := range currentNoDataOutages {
		ongoingNoDataLinks[o.LinkCode] = true
	}

	// Find completed no-data outages (gaps in data within the time range)
	completedNoDataOutages, err := findCompletedNoDataOutages(ctx, conn, duration, linkMeta, ongoingNoDataLinks)
	if err != nil {
		return nil, fmt.Errorf("failed to find completed no-data outages: %w", err)
	}

	allOutages := append(currentNoDataOutages, completedNoDataOutages...)
	return allOutages, nil
}

// fetchCurrentNoDataLinks finds links that have historical data but stopped reporting recently
func fetchCurrentNoDataLinks(ctx context.Context, conn driver.Conn, linkMeta map[string]linkMetadata) ([]LinkOutage, error) {
	// Find links that have data in the last 30 days but no data in the last 15 minutes
	// Exclude links that are currently drained (those show as status outages instead)
	query := `
		WITH link_last_seen AS (
			SELECT
				link_pk,
				max(event_ts) as last_seen
			FROM fact_dz_device_link_latency
			WHERE event_ts >= now() - INTERVAL 30 DAY
			  AND link_pk != ''
			GROUP BY link_pk
		)
		SELECT lls.link_pk, lls.last_seen
		FROM link_last_seen lls
		JOIN dz_links_current l ON lls.link_pk = l.pk
		WHERE lls.last_seen < now() - INTERVAL 15 MINUTE
		  AND lls.last_seen >= now() - INTERVAL 30 DAY
		  AND l.status NOT IN ('soft-drained', 'hard-drained')
	`

	rows, err := conn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	var outages []LinkOutage
	outageIDCounter := 0

	for rows.Next() {
		var linkPK string
		var lastSeen time.Time

		if err := rows.Scan(&linkPK, &lastSeen); err != nil {
			return nil, fmt.Errorf("scan failed: %w", err)
		}

		meta, hasMeta := linkMeta[linkPK]
		if !hasMeta {
			// Link filtered out
			continue
		}

		outageIDCounter++

		// The outage started when we last saw data (plus a small buffer for the expected interval)
		startedAt := lastSeen.Add(5 * time.Minute) // Assume 5-min reporting interval

		outages = append(outages, LinkOutage{
			ID:              fmt.Sprintf("nodata-%d", outageIDCounter),
			LinkPK:          linkPK,
			LinkCode:        meta.LinkCode,
			LinkType:        meta.LinkType,
			SideAMetro:      meta.SideAMetro,
			SideZMetro:      meta.SideZMetro,
			ContributorCode: meta.ContributorCode,
			OutageType:      "no_data",
			StartedAt:       startedAt.UTC().Format(time.RFC3339),
			IsOngoing:       true,
		})
	}

	return outages, nil
}

// drainedPeriod represents a time period when a link was in drained state
type drainedPeriod struct {
	Start time.Time
	End   *time.Time // nil if still drained
}

// fetchDrainedPeriods gets all periods when links were drained within the time range
func fetchDrainedPeriods(ctx context.Context, conn driver.Conn, duration time.Duration) (map[string][]drainedPeriod, error) {
	// Get all status changes within the time range (plus some buffer to catch ongoing drains)
	query := `
		SELECT
			link_pk,
			previous_status,
			new_status,
			changed_ts
		FROM dz_link_status_changes
		WHERE changed_ts >= now() - INTERVAL $1 SECOND
		ORDER BY link_pk, changed_ts
	`

	rows, err := conn.Query(ctx, query, int64(duration.Seconds()))
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	type statusChange struct {
		LinkPK    string
		PrevStatus string
		NewStatus  string
		ChangedTS  time.Time
	}

	var changes []statusChange
	for rows.Next() {
		var sc statusChange
		if err := rows.Scan(&sc.LinkPK, &sc.PrevStatus, &sc.NewStatus, &sc.ChangedTS); err != nil {
			return nil, fmt.Errorf("scan failed: %w", err)
		}
		changes = append(changes, sc)
	}

	// Group by link and build drained periods
	byLink := make(map[string][]statusChange)
	for _, c := range changes {
		byLink[c.LinkPK] = append(byLink[c.LinkPK], c)
	}

	result := make(map[string][]drainedPeriod)
	for linkPK, linkChanges := range byLink {
		var periods []drainedPeriod
		var activeDrain *drainedPeriod

		for _, c := range linkChanges {
			isDrained := c.NewStatus == "soft-drained" || c.NewStatus == "hard-drained"
			isRecovery := c.NewStatus == "activated" && (c.PrevStatus == "soft-drained" || c.PrevStatus == "hard-drained")

			if isDrained && activeDrain == nil {
				activeDrain = &drainedPeriod{Start: c.ChangedTS}
			} else if isRecovery && activeDrain != nil {
				endTime := c.ChangedTS
				activeDrain.End = &endTime
				periods = append(periods, *activeDrain)
				activeDrain = nil
			}
		}

		// If still drained at end, add period with no end time
		if activeDrain != nil {
			periods = append(periods, *activeDrain)
		}

		if len(periods) > 0 {
			result[linkPK] = periods
		}
	}

	return result, nil
}

// gapOverlapsDrainedPeriod checks if a data gap overlaps with any drained period
func gapOverlapsDrainedPeriod(gapStart, gapEnd time.Time, periods []drainedPeriod) bool {
	for _, p := range periods {
		// Check if gap overlaps with this drained period
		// Gap overlaps if: gap starts before period ends AND gap ends after period starts
		periodEnd := time.Now().Add(time.Hour) // Default to future if ongoing
		if p.End != nil {
			periodEnd = *p.End
		}

		if gapStart.Before(periodEnd) && gapEnd.After(p.Start) {
			return true
		}
	}
	return false
}

// findCompletedNoDataOutages finds gaps in data within the time range that later resumed
// Filters out gaps that occurred during drained periods (those are status outages, not no-data)
func findCompletedNoDataOutages(ctx context.Context, conn driver.Conn, duration time.Duration, linkMeta map[string]linkMetadata, excludeLinks map[string]bool) ([]LinkOutage, error) {
	// First, fetch all drained periods to filter out gaps caused by drains
	drainedPeriods, err := fetchDrainedPeriods(ctx, conn, duration)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch drained periods: %w", err)
	}

	// Get the timestamps of data buckets for each link within the time range
	query := `
		SELECT
			link_pk,
			toStartOfInterval(event_ts, INTERVAL 5 MINUTE) as bucket
		FROM fact_dz_device_link_latency
		WHERE event_ts >= now() - INTERVAL $1 SECOND
		  AND link_pk != ''
		GROUP BY link_pk, bucket
		ORDER BY link_pk, bucket
	`

	rows, err := conn.Query(ctx, query, int64(duration.Seconds()))
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	// Group buckets by link
	type bucket struct {
		LinkPK string
		Bucket time.Time
	}
	var buckets []bucket
	for rows.Next() {
		var b bucket
		if err := rows.Scan(&b.LinkPK, &b.Bucket); err != nil {
			return nil, fmt.Errorf("scan failed: %w", err)
		}
		buckets = append(buckets, b)
	}

	// Group by link
	byLink := make(map[string][]time.Time)
	for _, b := range buckets {
		byLink[b.LinkPK] = append(byLink[b.LinkPK], b.Bucket)
	}

	var outages []LinkOutage
	outageIDCounter := 1000 // Start high to avoid collision with ongoing IDs

	for linkPK, linkBuckets := range byLink {
		meta, hasMeta := linkMeta[linkPK]
		if !hasMeta {
			continue
		}

		// Skip links with ongoing no-data outages
		if excludeLinks[meta.LinkCode] {
			continue
		}

		// Sort buckets by time
		sort.Slice(linkBuckets, func(i, j int) bool {
			return linkBuckets[i].Before(linkBuckets[j])
		})

		// Look for gaps > 15 minutes between consecutive buckets
		for i := 1; i < len(linkBuckets); i++ {
			gap := linkBuckets[i].Sub(linkBuckets[i-1])
			if gap >= noDataGapThreshold {
				// Gap started after the previous bucket
				gapStart := linkBuckets[i-1].Add(5 * time.Minute)
				// Gap ended when data resumed
				gapEnd := linkBuckets[i]

				// Skip if this gap overlaps with a drained period
				if periods, hasPeriods := drainedPeriods[linkPK]; hasPeriods {
					if gapOverlapsDrainedPeriod(gapStart, gapEnd, periods) {
						continue
					}
				}

				outageIDCounter++
				gapDuration := int64(gapEnd.Sub(gapStart).Seconds())
				endedAt := gapEnd.UTC().Format(time.RFC3339)

				outages = append(outages, LinkOutage{
					ID:              fmt.Sprintf("nodata-%d", outageIDCounter),
					LinkPK:          linkPK,
					LinkCode:        meta.LinkCode,
					LinkType:        meta.LinkType,
					SideAMetro:      meta.SideAMetro,
					SideZMetro:      meta.SideZMetro,
					ContributorCode: meta.ContributorCode,
					OutageType:      "no_data",
					StartedAt:       gapStart.UTC().Format(time.RFC3339),
					EndedAt:         &endedAt,
					DurationSeconds: &gapDuration,
					IsOngoing:       false,
				})
			}
		}
	}

	return outages, nil
}

// GetLinkOutagesCSV returns outages as CSV for export
func GetLinkOutagesCSV(w http.ResponseWriter, r *http.Request) {
	// Parse query parameters (same as GetLinkOutages)
	timeRange := r.URL.Query().Get("range")
	if timeRange == "" {
		timeRange = "24h"
	}
	duration := parseTimeRange(timeRange)

	thresholdStr := r.URL.Query().Get("threshold")
	threshold := parseThreshold(thresholdStr)

	outageType := r.URL.Query().Get("type")
	if outageType == "" {
		outageType = "all"
	}

	// Parse filters (format: type:value,type:value)
	filterStr := r.URL.Query().Get("filter")
	filters := parseOutageFilters(filterStr)

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var outages []LinkOutage

	if outageType == "all" || outageType == "status" {
		statusOutages, err := fetchStatusOutages(ctx, config.DB, duration, filters)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to fetch status outages: %v", err), http.StatusInternalServerError)
			return
		}
		outages = append(outages, statusOutages...)
	}

	if outageType == "all" || outageType == "loss" {
		lossOutages, err := fetchPacketLossOutages(ctx, config.DB, duration, threshold, filters)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to fetch packet loss outages: %v", err), http.StatusInternalServerError)
			return
		}
		outages = append(outages, lossOutages...)
	}

	// Sort by start time (most recent first)
	sort.Slice(outages, func(i, j int) bool {
		return outages[i].StartedAt > outages[j].StartedAt
	})

	// Generate CSV
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", "attachment; filename=link-outages.csv")

	// Write header
	w.Write([]byte("id,link_code,link_type,side_a_metro,side_z_metro,contributor,outage_type,details,started_at,ended_at,duration_seconds,is_ongoing\n"))

	for _, o := range outages {
		var details string
		if o.OutageType == "status" {
			details = fmt.Sprintf("%s -> %s", strVal(o.PreviousStatus), strVal(o.NewStatus))
		} else {
			details = fmt.Sprintf("peak %.1f%% (threshold %.0f%%)", floatVal(o.PeakLossPct), floatVal(o.ThresholdPct))
		}

		endedAt := ""
		if o.EndedAt != nil {
			endedAt = *o.EndedAt
		}

		durationSecs := ""
		if o.DurationSeconds != nil {
			durationSecs = strconv.FormatInt(*o.DurationSeconds, 10)
		}

		line := fmt.Sprintf("%s,%s,%s,%s,%s,%s,%s,\"%s\",%s,%s,%s,%t\n",
			o.ID, o.LinkCode, o.LinkType, o.SideAMetro, o.SideZMetro,
			o.ContributorCode, o.OutageType, details, o.StartedAt, endedAt,
			durationSecs, o.IsOngoing)
		w.Write([]byte(line))
	}
}

func strVal(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func floatVal(f *float64) float64 {
	if f == nil {
		return 0
	}
	return *f
}

// fetchDefaultOutagesData fetches outages data with default parameters for caching.
// This returns the same data as GetLinkOutages with default params:
// range=24h, threshold=1, type=all, no filters.
func fetchDefaultOutagesData(ctx context.Context) *LinkOutagesResponse {
	duration := 24 * time.Hour
	threshold := 1.0
	var filters []OutageFilter // empty = no filters

	var outages []LinkOutage

	// Fetch status-based outages (drained states)
	statusOutages, err := fetchStatusOutages(ctx, config.DB, duration, filters)
	if err != nil {
		log.Printf("Cache: Failed to fetch status outages: %v", err)
	} else {
		outages = append(outages, statusOutages...)
	}

	// Fetch packet loss outages
	lossOutages, err := fetchPacketLossOutages(ctx, config.DB, duration, threshold, filters)
	if err != nil {
		log.Printf("Cache: Failed to fetch packet loss outages: %v", err)
	} else {
		outages = append(outages, lossOutages...)
	}

	// Fetch no-data outages
	noDataOutages, err := fetchNoDataOutages(ctx, config.DB, duration, filters)
	if err != nil {
		log.Printf("Cache: Failed to fetch no-data outages: %v", err)
	} else {
		outages = append(outages, noDataOutages...)
	}

	// Sort by start time (most recent first)
	sort.Slice(outages, func(i, j int) bool {
		return outages[i].StartedAt > outages[j].StartedAt
	})

	// Build summary
	summary := LinkOutagesSummary{
		Total:   len(outages),
		Ongoing: 0,
		ByType:  map[string]int{"status": 0, "packet_loss": 0, "no_data": 0},
	}
	for _, o := range outages {
		if o.IsOngoing {
			summary.Ongoing++
		}
		summary.ByType[o.OutageType]++
	}

	return &LinkOutagesResponse{
		Outages: outages,
		Summary: summary,
	}
}
