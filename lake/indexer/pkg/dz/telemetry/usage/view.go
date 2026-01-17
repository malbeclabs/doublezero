package dztelemusage

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/InfluxCommunity/influxdb3-go/v2/influxdb3"
	"github.com/jonboulle/clockwork"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/metrics"
)

// InfluxDBClient is an interface for querying InfluxDB 3 with SQL
type InfluxDBClient interface {
	// QuerySQL executes a SQL query and returns results as a slice of maps
	QuerySQL(ctx context.Context, sqlQuery string) ([]map[string]any, error)
	// Close closes the client and releases resources
	Close() error
}

// SDKInfluxDBClient implements InfluxDBClient using the official InfluxDB 3 Go SDK
type SDKInfluxDBClient struct {
	client *influxdb3.Client
}

// NewSDKInfluxDBClient creates a new SDK-based InfluxDB client
func NewSDKInfluxDBClient(host, token, database string) (*SDKInfluxDBClient, error) {
	client, err := influxdb3.New(influxdb3.ClientConfig{
		Host:     host,
		Token:    token,
		Database: database,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create InfluxDB client: %w", err)
	}
	return &SDKInfluxDBClient{client: client}, nil
}

func (c *SDKInfluxDBClient) QuerySQL(ctx context.Context, sqlQuery string) ([]map[string]any, error) {
	iterator, err := c.client.Query(ctx, sqlQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}

	var results []map[string]any
	for iterator.Next() {
		value := iterator.Value()
		row := make(map[string]any)
		for k, v := range value {
			row[k] = v
		}
		results = append(results, row)
	}

	if err := iterator.Err(); err != nil {
		return nil, fmt.Errorf("error iterating results: %w", err)
	}

	return results, nil
}

func (c *SDKInfluxDBClient) Close() error {
	if c.client != nil {
		err := c.client.Close()
		if err != nil {
			if isExpectedCloseError(err) {
				return nil
			}
		}
		return err
	}
	return nil
}

func isExpectedCloseError(err error) bool {
	errStr := err.Error()
	return strings.Contains(errStr, "connection is closing") ||
		strings.Contains(errStr, "code = Canceled") ||
		strings.Contains(errStr, "grpc: the client connection is closing")
}

type ViewConfig struct {
	Logger          *slog.Logger
	Clock           clockwork.Clock
	InfluxDB        InfluxDBClient
	Bucket          string
	ClickHouse      clickhouse.Client
	RefreshInterval time.Duration
	QueryWindow     time.Duration // How far back to query from InfluxDB
}

func (cfg *ViewConfig) Validate() error {
	if cfg.Logger == nil {
		return errors.New("logger is required")
	}
	if cfg.ClickHouse == nil {
		return errors.New("clickhouse connection is required")
	}
	if cfg.InfluxDB == nil {
		return errors.New("influxdb client is required")
	}
	if cfg.Bucket == "" {
		return errors.New("influxdb bucket is required")
	}
	if cfg.RefreshInterval <= 0 {
		return errors.New("refresh interval must be greater than 0")
	}
	if cfg.QueryWindow <= 0 {
		cfg.QueryWindow = 1 * time.Hour // Default to 1 hour window
	}
	if cfg.Clock == nil {
		cfg.Clock = clockwork.NewRealClock()
	}
	return nil
}

type View struct {
	log       *slog.Logger
	cfg       ViewConfig
	store     *Store
	readyOnce sync.Once
	readyCh   chan struct{}
	refreshMu sync.Mutex // prevents concurrent refreshes
}

func NewView(cfg ViewConfig) (*View, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	store, err := NewStore(StoreConfig{
		Logger:     cfg.Logger,
		ClickHouse: cfg.ClickHouse,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create store: %w", err)
	}

	v := &View{
		log:     cfg.Logger,
		cfg:     cfg,
		store:   store,
		readyCh: make(chan struct{}),
	}

	return v, nil
}

func (v *View) Start(ctx context.Context) {
	go func() {
		v.log.Info("telemetry/usage: starting refresh loop", "interval", v.cfg.RefreshInterval)

		if err := v.Refresh(ctx); err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			v.log.Error("telemetry/usage: initial refresh failed", "error", err)
		}
		ticker := v.cfg.Clock.NewTicker(v.cfg.RefreshInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.Chan():
				if err := v.Refresh(ctx); err != nil {
					if errors.Is(err, context.Canceled) {
						return
					}
					v.log.Error("telemetry/usage: refresh failed", "error", err)
				}
			}
		}
	}()
}

func (v *View) Refresh(ctx context.Context) error {
	v.refreshMu.Lock()
	defer v.refreshMu.Unlock()

	refreshStart := time.Now()
	v.log.Debug("telemetry/usage: refresh started", "start_time", refreshStart)
	defer func() {
		duration := time.Since(refreshStart)
		v.log.Info("telemetry/usage: refresh completed", "duration", duration.String())
		metrics.ViewRefreshDuration.WithLabelValues("telemetry-usage").Observe(duration.Seconds())
		if err := recover(); err != nil {
			metrics.ViewRefreshTotal.WithLabelValues("telemetry-usage", "error").Inc()
			panic(err)
		}
	}()

	maxTime, err := v.store.GetMaxTimestamp(ctx)
	if err != nil {
		metrics.ViewRefreshTotal.WithLabelValues("telemetry-usage", "error").Inc()
		return fmt.Errorf("failed to get max timestamp: %w", err)
	}
	if maxTime != nil {
		v.log.Debug("telemetry/usage: found max timestamp", "max_time", maxTime.UTC())
	} else {
		v.log.Debug("telemetry/usage: no existing data, performing initial refresh")
	}

	now := v.cfg.Clock.Now()
	queryWindowStart := now.Add(-v.cfg.QueryWindow)
	var queryStart time.Time

	if maxTime != nil {
		if maxTime.After(queryWindowStart) {
			// Include a small overlap (5 minutes) to catch late-arriving data with past timestamps
			overlap := 5 * time.Minute
			queryStart = maxTime.Add(-overlap)
			newDataWindow := now.Sub(*maxTime)
			totalQueryWindow := now.Sub(queryStart)
			v.log.Debug("telemetry/usage: incremental refresh (data within query window)",
				"maxTime", maxTime.UTC(),
				"queryStart", queryStart.UTC(),
				"now", now.UTC(),
				"newDataWindow", newDataWindow,
				"totalQueryWindow", totalQueryWindow,
				"overlap", overlap)
		} else {
			queryStart = queryWindowStart
			age := now.Sub(*maxTime)
			v.log.Debug("telemetry/usage: data exists but too old, starting from query window",
				"maxTime", maxTime.UTC(),
				"queryStart", queryStart.UTC(),
				"now", now.UTC(),
				"dataAge", age)
		}
	} else {
		queryStart = queryWindowStart
		v.log.Debug("telemetry/usage: initial full refresh", "from", queryStart, "to", now)
	}

	// Always try ClickHouse first; only query InfluxDB if ClickHouse returns 0 baselines
	var baselines *CounterBaselines
	v.log.Debug("telemetry/usage: querying baselines from clickhouse")
	chStart := time.Now()
	chBaselines, err := v.queryBaselineCountersFromClickHouse(ctx, queryStart)
	chDuration := time.Since(chStart)
	if err != nil {
		v.log.Warn("telemetry/usage: failed to query baseline counters from clickhouse", "error", err, "duration", chDuration.String())
		return fmt.Errorf("failed to query baseline counters from clickhouse: %w", err)
	} else {
		totalKeys := v.countUniqueBaselineKeys(chBaselines)
		if totalKeys > 0 {
			// ClickHouse has baseline data, use it
			v.log.Info("telemetry/usage: queried baselines from clickhouse", "unique_keys", totalKeys, "duration", chDuration.String())
			baselines = chBaselines
		} else {
			v.log.Debug("telemetry/usage: no baseline data in clickhouse (0 rows), will query influxdb", "duration", chDuration.String())
		}
	}

	if baselines == nil {
		v.log.Debug("telemetry/usage: querying baselines from influxdb (clickhouse returned 0 baselines)")
		baselineCtx, baselineCancel := context.WithTimeout(ctx, 120*time.Second)
		defer baselineCancel()

		influxStart := time.Now()
		baselines, err = v.queryBaselineCounters(baselineCtx, queryStart)
		influxDuration := time.Since(influxStart)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return err
			}
			if errors.Is(err, context.DeadlineExceeded) {
				v.log.Warn("telemetry/usage: baseline query timed out, proceeding without baselines", "duration", influxDuration.String())
			} else {
				v.log.Warn("telemetry/usage: failed to query baseline counters from InfluxDB, proceeding without baselines", "error", err, "duration", influxDuration.String())
			}
			baselines = &CounterBaselines{
				InDiscards:  make(map[string]*int64),
				InErrors:    make(map[string]*int64),
				InFCSErrors: make(map[string]*int64),
				OutDiscards: make(map[string]*int64),
				OutErrors:   make(map[string]*int64),
			}
		} else {
			totalKeys := v.countUniqueBaselineKeys(baselines)
			v.log.Info("telemetry/usage: queried baselines from influxdb", "unique_keys", totalKeys, "duration", influxDuration.String())
		}
	}

	if baselines == nil {
		baselines = &CounterBaselines{
			InDiscards:  make(map[string]*int64),
			InErrors:    make(map[string]*int64),
			InFCSErrors: make(map[string]*int64),
			OutDiscards: make(map[string]*int64),
			OutErrors:   make(map[string]*int64),
		}
	}

	// Query InfluxDB for interface usage data
	// Convert times to UTC for InfluxDB query (InfluxDB stores times in UTC)
	queryStartUTC := queryStart.UTC()
	nowUTC := now.UTC()
	usage, err := v.queryInfluxDB(ctx, queryStartUTC, nowUTC, baselines)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return err
		}
		metrics.ViewRefreshTotal.WithLabelValues("telemetry-usage", "error").Inc()
		return fmt.Errorf("failed to query influxdb: %w", err)
	}

	v.log.Info("telemetry/usage: queried influxdb", "rows", len(usage), "from", queryStart, "to", now)

	if len(usage) == 0 {
		v.log.Warn("telemetry/usage: no data returned from influxdb query", "from", queryStart, "to", now)
		v.readyOnce.Do(func() {
			close(v.readyCh)
			v.log.Info("telemetry/usage: view is now ready (no data)")
		})
		metrics.ViewRefreshTotal.WithLabelValues("telemetry-usage", "success").Inc()
		return nil
	}

	insertStart := time.Now()
	if err := v.store.InsertInterfaceUsage(ctx, usage); err != nil {
		metrics.ViewRefreshTotal.WithLabelValues("telemetry-usage", "error").Inc()
		return fmt.Errorf("failed to insert interface usage data to clickhouse: %w", err)
	}
	insertDuration := time.Since(insertStart)
	v.log.Info("telemetry/usage: inserted data to clickhouse", "rows", len(usage), "duration", insertDuration.String())

	v.readyOnce.Do(func() {
		close(v.readyCh)
		v.log.Info("telemetry/usage: view is now ready")
	})

	metrics.ViewRefreshTotal.WithLabelValues("telemetry-usage", "success").Inc()
	return nil
}

// LinkInfo holds link information for a device/interface
type LinkInfo struct {
	LinkPK   string
	LinkSide string // "A" or "Z"
}

// CounterBaselines holds the last known counter values before the query window
// Key format: "device_pk:intf"
// Only sparse counters (errors/discards) need baselines; non-sparse counters use the first row as baseline
type CounterBaselines struct {
	InDiscards  map[string]*int64
	InErrors    map[string]*int64
	InFCSErrors map[string]*int64
	OutDiscards map[string]*int64
	OutErrors   map[string]*int64
}

func (v *View) queryInfluxDB(ctx context.Context, startTime, endTime time.Time, baselines *CounterBaselines) ([]InterfaceUsage, error) {
	// InfluxDB uses dzd_pubkey as a tag, which we extract and map to device_pk.
	v.log.Debug("telemetry/usage: executing main influxdb query", "from", startTime.UTC(), "to", endTime.UTC())
	queryStart := time.Now()
	sqlQuery := fmt.Sprintf(`
		SELECT
			time,
			dzd_pubkey,
			host,
			intf,
			model_name,
			serial_number,
			"carrier-transitions",
			"in-broadcast-pkts",
			"in-discards",
			"in-errors",
			"in-fcs-errors",
			"in-multicast-pkts",
			"in-octets",
			"in-pkts",
			"in-unicast-pkts",
			"out-broadcast-pkts",
			"out-discards",
			"out-errors",
			"out-multicast-pkts",
			"out-octets",
			"out-pkts",
			"out-unicast-pkts"
		FROM "intfCounters"
		WHERE time >= '%s' AND time < '%s'
	`, startTime.UTC().Format(time.RFC3339Nano), endTime.UTC().Format(time.RFC3339Nano))

	rows, err := v.cfg.InfluxDB.QuerySQL(ctx, sqlQuery)
	queryDuration := time.Since(queryStart)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, err
		}
		return nil, fmt.Errorf("failed to execute SQL query: %w", err)
	}
	v.log.Debug("telemetry/usage: main influxdb query completed", "rows", len(rows), "duration", queryDuration.String())

	// Baselines are already provided from Refresh() - use them as-is

	// Sort rows by time to ensure proper forward-fill
	sortStart := time.Now()
	sort.Slice(rows, func(i, j int) bool {
		timeI := extractStringFromRow(rows[i], "time")
		timeJ := extractStringFromRow(rows[j], "time")
		if timeI == nil || timeJ == nil {
			return false
		}
		ti, errI := time.Parse(time.RFC3339Nano, *timeI)
		if errI != nil {
			ti, _ = time.Parse(time.RFC3339, *timeI)
		}
		tj, errJ := time.Parse(time.RFC3339Nano, *timeJ)
		if errJ != nil {
			tj, _ = time.Parse(time.RFC3339, *timeJ)
		}
		return ti.Before(tj)
	})
	sortDuration := time.Since(sortStart)
	v.log.Debug("telemetry/usage: sorted rows", "rows", len(rows), "duration", sortDuration.String())

	// Build link lookup map from dz_links_current table
	linkLookup, err := v.buildLinkLookup(ctx)
	if err != nil {
		v.log.Warn("telemetry/usage: failed to build link lookup map, proceeding without link information", "error", err)
		linkLookup = make(map[string]LinkInfo)
	} else {
		v.log.Debug("telemetry/usage: built link lookup map", "links", len(linkLookup))
	}

	// Convert rows to InterfaceUsage, tracking last known values per device/interface
	// We need to process in time order to properly forward-fill nulls
	convertStart := time.Now()
	usage, err := v.convertRowsToUsage(rows, baselines, linkLookup)
	convertDuration := time.Since(convertStart)
	if err != nil {
		return nil, fmt.Errorf("failed to convert rows: %w", err)
	}
	v.log.Debug("telemetry/usage: converted rows to usage data", "usage_records", len(usage), "duration", convertDuration.String())

	return usage, nil
}

// buildLinkLookup builds a map from "device_pk:intf" to LinkInfo by querying the dz_links_history table
func (v *View) buildLinkLookup(ctx context.Context) (map[string]LinkInfo, error) {
	lookup := make(map[string]LinkInfo)

	conn, err := v.cfg.ClickHouse.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get ClickHouse connection: %w", err)
	}

	// Query current links from history table using ROW_NUMBER for latest row per entity
	query := `
		WITH ranked AS (
			SELECT
				*,
				ROW_NUMBER() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
			FROM dim_dz_links_history
		)
		SELECT
			pk,
			side_a_pk,
			side_a_iface_name,
			side_z_pk,
			side_z_iface_name
		FROM ranked
		WHERE rn = 1 AND is_deleted = 0`
	rows, err := conn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query links: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var linkPK, sideAPK, sideAIface, sideZPK, sideZIface *string
		if err := rows.Scan(&linkPK, &sideAPK, &sideAIface, &sideZPK, &sideZIface); err != nil {
			return nil, fmt.Errorf("failed to scan link row: %w", err)
		}

		// Add side A mapping
		if sideAPK != nil && sideAIface != nil && *sideAPK != "" && *sideAIface != "" {
			key := fmt.Sprintf("%s:%s", *sideAPK, *sideAIface)
			linkPKVal := ""
			if linkPK != nil {
				linkPKVal = *linkPK
			}
			lookup[key] = LinkInfo{LinkPK: linkPKVal, LinkSide: "A"}
		}

		// Add side Z mapping
		if sideZPK != nil && sideZIface != nil && *sideZPK != "" && *sideZIface != "" {
			key := fmt.Sprintf("%s:%s", *sideZPK, *sideZIface)
			linkPKVal := ""
			if linkPK != nil {
				linkPKVal = *linkPK
			}
			lookup[key] = LinkInfo{LinkPK: linkPKVal, LinkSide: "Z"}
		}
	}

	return lookup, nil
}

// convertRowsToUsage converts rows to InterfaceUsage, using baselines only for the first null
// and forward-filling with the last known value for subsequent nulls
// For non-sparse counters, the first row per device/interface is used as baseline and not stored
func (v *View) convertRowsToUsage(rows []map[string]any, baselines *CounterBaselines, linkLookup map[string]LinkInfo) ([]InterfaceUsage, error) {
	// Track last known values per device/interface for each counter
	// Key: "device_pk:intf", Value: map of counter name to last value
	lastKnownValues := make(map[string]map[string]*int64)
	// Track whether we've seen the first row for each device/interface
	// For non-sparse counters, we skip storing the first row and use it as baseline
	firstRowSeen := make(map[string]bool)
	// Track last time per device/interface for computing delta_duration
	lastTime := make(map[string]time.Time)

	var usage []InterfaceUsage
	totalRows := len(rows)
	logInterval := totalRows / 10 // Log every 10% progress
	if logInterval < 100 {
		logInterval = 100 // But at least every 100 rows
	}

	for i, row := range rows {
		// Log progress periodically
		if i > 0 && i%logInterval == 0 {
			v.log.Debug("telemetry/usage: converting rows", "progress", fmt.Sprintf("%d/%d (%.1f%%)", i, totalRows, float64(i)/float64(totalRows)*100))
		}
		u := &InterfaceUsage{}

		// Extract time (required)
		timeStr := extractStringFromRow(row, "time")
		if timeStr == nil {
			continue // Skip rows without time
		}

		// Try multiple time formats that InfluxDB might return
		// InfluxDB SDK returns time in format: "2006-01-02 15:04:05.999999999 +0000 UTC"
		var t time.Time
		var err error
		timeFormats := []string{
			time.RFC3339Nano,
			time.RFC3339,
			"2006-01-02 15:04:05.999999999 -0700 UTC", // InfluxDB format with timezone
			"2006-01-02 15:04:05.999999999 +0700 UTC",
			"2006-01-02 15:04:05.999999999 +0000 UTC",
			"2006-01-02 15:04:05.999999 -0700 UTC",
			"2006-01-02 15:04:05.999999 +0700 UTC",
			"2006-01-02 15:04:05.999999 +0000 UTC",
			"2006-01-02 15:04:05.999 -0700 UTC",
			"2006-01-02 15:04:05.999 +0700 UTC",
			"2006-01-02 15:04:05.999 +0000 UTC",
			"2006-01-02 15:04:05 -0700 UTC",
			"2006-01-02 15:04:05 +0700 UTC",
			"2006-01-02 15:04:05 +0000 UTC",
		}

		parsed := false
		for _, format := range timeFormats {
			t, err = time.Parse(format, *timeStr)
			if err == nil {
				parsed = true
				break
			}
		}

		if !parsed {
			continue // Skip rows with invalid time
		}
		u.Time = t

		// Extract string fields
		u.DevicePK = extractStringFromRow(row, "dzd_pubkey")
		u.Host = extractStringFromRow(row, "host")
		u.Intf = extractStringFromRow(row, "intf")
		u.ModelName = extractStringFromRow(row, "model_name")
		u.SerialNumber = extractStringFromRow(row, "serial_number")

		// Extract tunnel ID from interface name if it starts with "Tunnel"
		if u.Intf != nil {
			u.UserTunnelID = extractTunnelIDFromInterface(*u.Intf)
		}

		// Build key for tracking
		var key string
		if u.DevicePK != nil && u.Intf != nil {
			key = fmt.Sprintf("%s:%s", *u.DevicePK, *u.Intf)
		} else {
			// Can't track without key, just extract what we can
			key = ""
		}

		if key != "" {
			if linkInfo, ok := linkLookup[key]; ok {
				u.LinkPK = &linkInfo.LinkPK
				u.LinkSide = &linkInfo.LinkSide
			}
		}

		if key != "" && lastKnownValues[key] == nil {
			lastKnownValues[key] = make(map[string]*int64)
			// Pre-populate sparse counter baselines for forward-filling
			if baselines != nil {
				if val := baselines.InDiscards[key]; val != nil {
					lastKnownValues[key]["in-discards"] = val
				}
				if val := baselines.InErrors[key]; val != nil {
					lastKnownValues[key]["in-errors"] = val
				}
				if val := baselines.InFCSErrors[key]; val != nil {
					lastKnownValues[key]["in-fcs-errors"] = val
				}
				if val := baselines.OutDiscards[key]; val != nil {
					lastKnownValues[key]["out-discards"] = val
				}
				if val := baselines.OutErrors[key]; val != nil {
					lastKnownValues[key]["out-errors"] = val
				}
			}
		}

		isFirstRow := key != "" && !firstRowSeen[key]

		// For all counter fields: use value if present, otherwise forward-fill with last known
		// Sparse counters (errors/discards) have baselines from 10-year query
		// Non-sparse counters: first row is used as baseline, not stored
		allCounterFields := []struct {
			field     string
			dest      **int64
			deltaDest **int64
			baseline  map[string]*int64
			isSparse  bool
		}{
			{"carrier-transitions", &u.CarrierTransitions, &u.CarrierTransitionsDelta, nil, false},
			{"in-broadcast-pkts", &u.InBroadcastPkts, &u.InBroadcastPktsDelta, nil, false},
			{"in-discards", &u.InDiscards, &u.InDiscardsDelta, baselines.InDiscards, true},
			{"in-errors", &u.InErrors, &u.InErrorsDelta, baselines.InErrors, true},
			{"in-fcs-errors", &u.InFCSErrors, &u.InFCSErrorsDelta, baselines.InFCSErrors, true},
			{"in-multicast-pkts", &u.InMulticastPkts, &u.InMulticastPktsDelta, nil, false},
			{"in-octets", &u.InOctets, &u.InOctetsDelta, nil, false},
			{"in-pkts", &u.InPkts, &u.InPktsDelta, nil, false},
			{"in-unicast-pkts", &u.InUnicastPkts, &u.InUnicastPktsDelta, nil, false},
			{"out-broadcast-pkts", &u.OutBroadcastPkts, &u.OutBroadcastPktsDelta, nil, false},
			{"out-discards", &u.OutDiscards, &u.OutDiscardsDelta, baselines.OutDiscards, true},
			{"out-errors", &u.OutErrors, &u.OutErrorsDelta, baselines.OutErrors, true},
			{"out-multicast-pkts", &u.OutMulticastPkts, &u.OutMulticastPktsDelta, nil, false},
			{"out-octets", &u.OutOctets, &u.OutOctetsDelta, nil, false},
			{"out-pkts", &u.OutPkts, &u.OutPktsDelta, nil, false},
			{"out-unicast-pkts", &u.OutUnicastPkts, &u.OutUnicastPktsDelta, nil, false},
		}

		// For non-sparse counters on first row: extract values and use as baseline, skip storing the row
		// For sparse counters, we still process and store the first row (they have baselines from 10-year query)
		if isFirstRow {
			// Check if we have any non-sparse counter values
			hasNonSparseValues := false
			for _, cf := range allCounterFields {
				if !cf.isSparse {
					value := extractInt64FromRow(row, cf.field)
					if value != nil {
						hasNonSparseValues = true
						break
					}
				}
			}

			if hasNonSparseValues {
				// Extract all non-sparse counter values and store as baselines
				for _, cf := range allCounterFields {
					if !cf.isSparse {
						value := extractInt64FromRow(row, cf.field)
						if value != nil && key != "" {
							lastKnownValues[key][cf.field] = value
						}
					}
				}
				lastTime[key] = t
				firstRowSeen[key] = true
				continue
			}
			// If no non-sparse values, continue processing normally (sparse counters will be stored)
			firstRowSeen[key] = true
		}

		// Process all counters
		for _, cf := range allCounterFields {
			var currentValue *int64
			value := extractInt64FromRow(row, cf.field)
			if value != nil {
				currentValue = value
			} else if key != "" {
				// Forward-fill with last known value (includes pre-populated baselines)
				if lastKnown, ok := lastKnownValues[key][cf.field]; ok && lastKnown != nil {
					currentValue = lastKnown
				}
			}

			*cf.dest = currentValue

			// Compute delta against last-known value
			if currentValue != nil && key != "" {
				var previousValue *int64
				if lastKnown, ok := lastKnownValues[key][cf.field]; ok && lastKnown != nil {
					previousValue = lastKnown
				}

				if previousValue != nil {
					delta := *currentValue - *previousValue
					*cf.deltaDest = &delta
				}

				lastKnownValues[key][cf.field] = currentValue
			}
		}

		// Compute delta_duration: time difference from previous measurement
		if key != "" {
			if lastT, ok := lastTime[key]; ok {
				duration := t.Sub(lastT).Seconds()
				u.DeltaDuration = &duration
			}
			// Update last time for next iteration
			lastTime[key] = t
		}

		usage = append(usage, *u)
	}

	return usage, nil
}

// queryBaselineCountersFromClickHouse queries ClickHouse for the last non-null counter values before the window start
// for each device/interface combination. Returns error if ClickHouse doesn't have data or query fails.
func (v *View) queryBaselineCountersFromClickHouse(ctx context.Context, windowStart time.Time) (*CounterBaselines, error) {
	// Query recent data before the window start to find the last non-null values
	// Limit to 90 days lookback to enable partition pruning - baselines don't need to go back years
	lookbackStart := windowStart.Add(-90 * 24 * time.Hour)

	baselines := &CounterBaselines{
		InDiscards:  make(map[string]*int64),
		InErrors:    make(map[string]*int64),
		InFCSErrors: make(map[string]*int64),
		OutDiscards: make(map[string]*int64),
		OutErrors:   make(map[string]*int64),
	}

	// Only query baselines for sparse counters (errors/discards)
	// For non-sparse counters, we use the first row as baseline and don't store it
	counterFields := []struct {
		field    string
		baseline map[string]*int64
	}{
		{"in_discards", baselines.InDiscards},
		{"in_errors", baselines.InErrors},
		{"in_fcs_errors", baselines.InFCSErrors},
		{"out_discards", baselines.OutDiscards},
		{"out_errors", baselines.OutErrors},
	}

	conn, err := v.cfg.ClickHouse.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get ClickHouse connection: %w", err)
	}

	for _, cf := range counterFields {
		// Use argMax to get the latest row per device/interface
		sqlQuery := fmt.Sprintf(`
			SELECT
				device_pk,
				intf,
				argMax(%s, (event_ts)) as value
			FROM fact_dz_device_interface_counters
			WHERE event_ts >= ? AND event_ts < ? AND %s IS NOT NULL
			GROUP BY device_pk, intf
		`, cf.field, cf.field)

		ctx := context.Background()
		rows, err := conn.Query(ctx, sqlQuery, lookbackStart, windowStart)
		if err != nil {
			v.log.Warn("telemetry/usage: failed to query baseline for counter from clickhouse", "counter", cf.field, "error", err)
			continue
		}
		defer rows.Close()

		for rows.Next() {
			var devicePK, intf *string
			var val *int64
			if err := rows.Scan(&devicePK, &intf, &val); err != nil {
				v.log.Warn("telemetry/usage: failed to scan baseline row", "counter", cf.field, "error", err)
				continue
			}

			if devicePK == nil || intf == nil {
				continue
			}

			key := fmt.Sprintf("%s:%s", *devicePK, *intf)
			if val != nil {
				cf.baseline[key] = val
			}
		}
	}

	return baselines, nil
}

// queryBaselineCounters queries InfluxDB for the last non-null counter values before the window start
// for sparse counters (errors/discards) using a 10-year lookback window.
func (v *View) queryBaselineCounters(ctx context.Context, windowStart time.Time) (*CounterBaselines, error) {
	baselines := &CounterBaselines{
		InDiscards:  make(map[string]*int64),
		InErrors:    make(map[string]*int64),
		InFCSErrors: make(map[string]*int64),
		OutDiscards: make(map[string]*int64),
		OutErrors:   make(map[string]*int64),
	}

	// Only query baselines for sparse counters (errors/discards)
	// For non-sparse counters, we use the first row as baseline and don't store it
	counterFields := []struct {
		field    string
		baseline map[string]*int64
	}{
		{"in-discards", baselines.InDiscards},
		{"in-errors", baselines.InErrors},
		{"in-fcs-errors", baselines.InFCSErrors},
		{"out-discards", baselines.OutDiscards},
		{"out-errors", baselines.OutErrors},
	}

	v.log.Debug("telemetry/usage: querying baseline counters from influxdb", "counters", len(counterFields))
	var wg sync.WaitGroup
	errCh := make(chan error, len(counterFields))

	for _, cf := range counterFields {
		wg.Add(1)
		go func(cf struct {
			field    string
			baseline map[string]*int64
		}) {
			defer wg.Done()
			counterStart := time.Now()

			// For sparse counters, just use 10-year window directly (they're sparse, so it's fast)
			lookbackStart := windowStart.Add(-10 * 365 * 24 * time.Hour)
			sqlQuery := fmt.Sprintf(`
				SELECT
					dzd_pubkey,
					intf,
					"%s" as value
				FROM (
					SELECT
						dzd_pubkey,
						intf,
						"%s",
						ROW_NUMBER() OVER (PARTITION BY dzd_pubkey, intf ORDER BY time DESC) as rn
					FROM "intfCounters"
					WHERE time >= '%s' AND time < '%s' AND "%s" IS NOT NULL
				) ranked
				WHERE rn = 1
			`, cf.field, cf.field, lookbackStart.Format(time.RFC3339Nano), windowStart.Format(time.RFC3339Nano), cf.field)

			rows, err := v.cfg.InfluxDB.QuerySQL(ctx, sqlQuery)
			counterDuration := time.Since(counterStart)
			if err != nil {
				v.log.Warn("telemetry/usage: failed to query baseline counter", "counter", cf.field, "error", err, "duration", counterDuration.String())
				errCh <- fmt.Errorf("failed to query baseline for %s: %w", cf.field, err)
				return
			}

			baselineCount := 0
			for _, row := range rows {
				devicePK := extractStringFromRow(row, "dzd_pubkey")
				intf := extractStringFromRow(row, "intf")
				if devicePK == nil || intf == nil {
					continue
				}
				key := fmt.Sprintf("%s:%s", *devicePK, *intf)
				value := extractInt64FromRow(row, "value")
				if value != nil {
					cf.baseline[key] = value
					baselineCount++
				}
			}
			v.log.Debug("telemetry/usage: completed baseline counter query", "counter", cf.field, "baselines", baselineCount, "duration", counterDuration.String())
		}(cf)
	}

	wg.Wait()
	close(errCh)

	// Check for errors
	hasErrors := false
	for err := range errCh {
		if err != nil {
			hasErrors = true
			v.log.Warn("telemetry/usage: baseline counter query error", "error", err)
		}
	}

	if hasErrors {
		// Return partial baselines even if some queries failed
		totalKeys := v.countUniqueBaselineKeys(baselines)
		v.log.Warn("telemetry/usage: some baseline counter queries failed, returning partial baselines", "unique_keys", totalKeys)
	} else {
		totalKeys := v.countUniqueBaselineKeys(baselines)
		v.log.Debug("telemetry/usage: completed all baseline counter queries", "unique_keys", totalKeys)
	}

	return baselines, nil
}

// countUniqueBaselineKeys counts the number of unique device/interface keys across all baseline maps
func (v *View) countUniqueBaselineKeys(baselines *CounterBaselines) int {
	keys := make(map[string]bool)
	for k := range baselines.InDiscards {
		keys[k] = true
	}
	for k := range baselines.InErrors {
		keys[k] = true
	}
	for k := range baselines.InFCSErrors {
		keys[k] = true
	}
	for k := range baselines.OutDiscards {
		keys[k] = true
	}
	for k := range baselines.OutErrors {
		keys[k] = true
	}
	return len(keys)
}

func extractStringFromRow(row map[string]any, key string) *string {
	val, ok := row[key]
	if !ok || val == nil {
		return nil
	}
	switch v := val.(type) {
	case string:
		return &v
	default:
		s := fmt.Sprintf("%v", v)
		return &s
	}
}

func extractInt64FromRow(row map[string]any, key string) *int64 {
	val, ok := row[key]
	if !ok || val == nil {
		return nil
	}
	switch v := val.(type) {
	case string:
		if i, err := strconv.ParseInt(v, 10, 64); err == nil {
			return &i
		}
		return nil
	case int64:
		return &v
	case int:
		i := int64(v)
		return &i
	case float64:
		i := int64(v)
		return &i
	default:
		return nil
	}
}

// extractTunnelIDFromInterface extracts the tunnel ID from an interface name.
// For interfaces with "Tunnel" prefix (e.g., "Tunnel501"), it returns the numeric part (501).
// Returns nil if the interface name doesn't match the pattern.
func extractTunnelIDFromInterface(intfName string) *int64 {
	if !strings.HasPrefix(intfName, "Tunnel") {
		return nil
	}
	// Extract the numeric part after "Tunnel"
	suffix := intfName[len("Tunnel"):]
	if suffix == "" {
		return nil
	}
	// Parse as int64
	if id, err := strconv.ParseInt(suffix, 10, 64); err == nil {
		return &id
	}
	return nil
}

// Ready returns true if the view has completed at least one successful refresh
func (v *View) Ready() bool {
	select {
	case <-v.readyCh:
		return true
	default:
		return false
	}
}

// WaitReady waits for the view to be ready (has completed at least one successful refresh)
// It returns immediately if already ready, or blocks until ready or context is cancelled.
func (v *View) WaitReady(ctx context.Context) error {
	select {
	case <-v.readyCh:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("context cancelled while waiting for telemetry-usage view: %w", ctx.Err())
	}
}

// Store returns the underlying store
func (v *View) Store() *Store {
	return v.store
}
