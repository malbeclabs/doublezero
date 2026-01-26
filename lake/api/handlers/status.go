package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/malbeclabs/doublezero/lake/api/config"
	"github.com/malbeclabs/doublezero/lake/api/metrics"
	"golang.org/x/sync/errgroup"
)

// StatusResponse contains comprehensive health/status information
type StatusResponse struct {
	// Overall status
	Status    string `json:"status"` // "healthy", "degraded", "unhealthy"
	Timestamp string `json:"timestamp"`

	// System health
	System SystemHealth `json:"system"`

	// Network summary
	Network NetworkSummary `json:"network"`

	// Link health
	Links LinkHealth `json:"links"`

	// Interface issues
	Interfaces InterfaceHealth `json:"interfaces"`

	// Infrastructure alerts (non-activated devices/links)
	Alerts InfrastructureAlerts `json:"alerts"`

	// Performance metrics
	Performance PerformanceMetrics `json:"performance"`

	// Device utilization (top by tunnel usage)
	TopDeviceUtil []DeviceUtilization `json:"top_device_util"`

	Error string `json:"error,omitempty"`
}

type SystemHealth struct {
	Database     bool   `json:"database"`
	DatabaseMsg  string `json:"database_msg,omitempty"`
	LastIngested string `json:"last_ingested,omitempty"` // Most recent data timestamp
}

type NetworkSummary struct {
	// Counts
	ValidatorsOnDZ   uint64  `json:"validators_on_dz"`
	TotalStakeSol    float64 `json:"total_stake_sol"`
	StakeSharePct    float64 `json:"stake_share_pct"`
	StakeShareDelta  float64 `json:"stake_share_delta"` // Change from 24h ago (percentage points)
	Users            uint64  `json:"users"`
	Devices          uint64  `json:"devices"`
	Links            uint64  `json:"links"`
	Contributors     uint64  `json:"contributors"`
	Metros           uint64  `json:"metros"`
	BandwidthBps     int64   `json:"bandwidth_bps"`
	UserInboundBps   float64 `json:"user_inbound_bps"`

	// Status breakdown
	DevicesByStatus map[string]uint64 `json:"devices_by_status"`
	LinksByStatus   map[string]uint64 `json:"links_by_status"`
}

type LinkHealth struct {
	Total          uint64       `json:"total"`
	Healthy        uint64       `json:"healthy"`
	Degraded       uint64       `json:"degraded"`  // High latency or some loss
	Unhealthy      uint64       `json:"unhealthy"` // Significant loss or down
	Disabled       uint64       `json:"disabled"`  // Extended packet loss (100% for 2+ hours)
	Issues         []LinkIssue  `json:"issues"`    // Top issues
	HighUtilLinks  []LinkMetric `json:"high_util_links"` // Links with high utilization
	TopUtilLinks   []LinkMetric `json:"top_util_links"`  // Top 10 links by max utilization
}

type LinkIssue struct {
	Code        string  `json:"code"`
	LinkType    string  `json:"link_type"`
	Contributor string  `json:"contributor"`
	Issue       string  `json:"issue"`       // "packet_loss", "high_latency", "down"
	Value       float64 `json:"value"`       // The problematic value
	Threshold   float64 `json:"threshold"`   // The threshold exceeded
	SideAMetro  string  `json:"side_a_metro"`
	SideZMetro  string  `json:"side_z_metro"`
	Since       string  `json:"since"`       // ISO timestamp when issue started
}

type LinkMetric struct {
	PK             string  `json:"pk"`
	Code           string  `json:"code"`
	LinkType       string  `json:"link_type"`
	Contributor    string  `json:"contributor"`
	BandwidthBps   int64   `json:"bandwidth_bps"`
	InBps          float64 `json:"in_bps"`
	OutBps         float64 `json:"out_bps"`
	UtilizationIn  float64 `json:"utilization_in"`
	UtilizationOut float64 `json:"utilization_out"`
	SideAMetro     string  `json:"side_a_metro"`
	SideZMetro     string  `json:"side_z_metro"`
}

type DeviceUtilization struct {
	PK           string  `json:"pk"`
	Code         string  `json:"code"`
	DeviceType   string  `json:"device_type"`
	Contributor  string  `json:"contributor"`
	Metro        string  `json:"metro"`
	CurrentUsers int32   `json:"current_users"`
	MaxUsers     int32   `json:"max_users"`
	Utilization  float64 `json:"utilization"` // percentage
}

type PerformanceMetrics struct {
	// Latency stats (WAN links, last 3 hours)
	AvgLatencyUs float64 `json:"avg_latency_us"`
	P95LatencyUs float64 `json:"p95_latency_us"`
	MinLatencyUs float64 `json:"min_latency_us"`
	MaxLatencyUs float64 `json:"max_latency_us"`

	// Packet loss (WAN links, last 3 hours)
	AvgLossPercent float64 `json:"avg_loss_percent"`

	// Jitter (WAN links, last 3 hours)
	AvgJitterUs float64 `json:"avg_jitter_us"`

	// Total throughput
	TotalInBps  float64 `json:"total_in_bps"`
	TotalOutBps float64 `json:"total_out_bps"`
}

type InterfaceHealth struct {
	Issues []InterfaceIssue `json:"issues"` // Interfaces with errors/discards/carrier transitions
}

type InterfaceIssue struct {
	DevicePK           string `json:"device_pk"`
	DeviceCode         string `json:"device_code"`
	DeviceType         string `json:"device_type"`
	Contributor        string `json:"contributor"`
	Metro              string `json:"metro"`
	InterfaceName      string `json:"interface_name"`
	LinkPK             string `json:"link_pk,omitempty"`      // Empty if not a link interface
	LinkCode           string `json:"link_code,omitempty"`    // Empty if not a link interface
	LinkType           string `json:"link_type,omitempty"`    // WAN, DZX, etc.
	LinkSide           string `json:"link_side,omitempty"`    // A or Z
	InErrors           uint64 `json:"in_errors"`
	OutErrors          uint64 `json:"out_errors"`
	InDiscards         uint64 `json:"in_discards"`
	OutDiscards        uint64 `json:"out_discards"`
	CarrierTransitions uint64 `json:"carrier_transitions"`
	FirstSeen          string `json:"first_seen"` // When issues first appeared in window
	LastSeen           string `json:"last_seen"`  // Most recent occurrence in window
}

type NonActivatedDevice struct {
	PK         string `json:"pk"`
	Code       string `json:"code"`
	DeviceType string `json:"device_type"`
	Metro      string `json:"metro"`
	Status     string `json:"status"`
	Since      string `json:"since"` // ISO timestamp when entered this status
}

type NonActivatedLink struct {
	PK         string `json:"pk"`
	Code       string `json:"code"`
	LinkType   string `json:"link_type"`
	SideAMetro string `json:"side_a_metro"`
	SideZMetro string `json:"side_z_metro"`
	Status     string `json:"status"`
	Since      string `json:"since"` // ISO timestamp when entered this status
}

type InfrastructureAlerts struct {
	Devices []NonActivatedDevice `json:"devices"`
	Links   []NonActivatedLink   `json:"links"`
}

// Thresholds for health classification (matching methodology)
// Packet loss severity: Minor (<1%), Moderate (1-10%), Severe (≥10%)
const (
	LatencyWarningPct  = 20.0  // 20% over committed RTT
	LatencyCriticalPct = 50.0  // 50% over committed RTT
	LossWarningPct     = 1.0   // 1% - Moderate (degraded)
	LossCriticalPct    = 10.0  // 10% - Severe (unhealthy)
	UtilWarningPct     = 70.0  // 70%
	UtilCriticalPct    = 90.0  // 90%
)

func GetStatus(w http.ResponseWriter, r *http.Request) {
	// Try to serve from cache first
	if statusCache != nil {
		if cached := statusCache.GetStatus(); cached != nil {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Cache", "HIT")
			if err := json.NewEncoder(w).Encode(cached); err != nil {
				log.Printf("JSON encoding error: %v", err)
			}
			return
		}
	}

	// Cache miss - fetch fresh data (should only happen during startup)
	w.Header().Set("X-Cache", "MISS")
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	resp := fetchStatusData(ctx)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("JSON encoding error: %v", err)
	}
}

// fetchStatusData performs the actual status data fetch from the database.
// This is called by both the cache refresh and direct requests.
func fetchStatusData(ctx context.Context) *StatusResponse {
	start := time.Now()

	resp := &StatusResponse{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Network: NetworkSummary{
			DevicesByStatus: make(map[string]uint64),
			LinksByStatus:   make(map[string]uint64),
		},
		Links: LinkHealth{
			Issues:        []LinkIssue{},
			HighUtilLinks: []LinkMetric{},
		},
		Interfaces: InterfaceHealth{
			Issues: []InterfaceIssue{},
		},
		Alerts: InfrastructureAlerts{
			Devices: []NonActivatedDevice{},
			Links:   []NonActivatedLink{},
		},
	}

	g, ctx := errgroup.WithContext(ctx)

	// Check database connectivity
	g.Go(func() error {
		pingCtx, pingCancel := context.WithTimeout(ctx, 2*time.Second)
		defer pingCancel()
		if err := config.DB.Ping(pingCtx); err != nil {
			resp.System.Database = false
			resp.System.DatabaseMsg = err.Error()
		} else {
			resp.System.Database = true
		}
		return nil
	})

	// Get last ingested timestamp
	g.Go(func() error {
		query := `
			SELECT formatDateTime(max(event_ts), '%Y-%m-%dT%H:%i:%sZ', 'UTC')
			FROM fact_dz_device_link_latency
			WHERE event_ts > now() - INTERVAL 1 HOUR
		`
		row := config.DB.QueryRow(ctx, query)
		var ts string
		if err := row.Scan(&ts); err == nil && ts != "" {
			resp.System.LastIngested = ts
		}
		return nil
	})

	// Network summary stats (same as /api/stats)
	g.Go(func() error {
		query := `
			SELECT COUNT(DISTINCT va.vote_pubkey) AS validators_on_dz
			FROM dz_users_current u
			JOIN solana_gossip_nodes_current gn ON u.dz_ip = gn.gossip_ip
			JOIN solana_vote_accounts_current va ON gn.pubkey = va.node_pubkey
			WHERE u.status = 'activated'
			  AND va.activated_stake_lamports > 0
		`
		row := config.DB.QueryRow(ctx, query)
		return row.Scan(&resp.Network.ValidatorsOnDZ)
	})

	g.Go(func() error {
		query := `
			SELECT COALESCE(SUM(va.activated_stake_lamports), 0) / 1000000000.0 AS total_stake_sol
			FROM dz_users_current u
			JOIN solana_gossip_nodes_current gn ON u.dz_ip = gn.gossip_ip
			JOIN solana_vote_accounts_current va ON gn.pubkey = va.node_pubkey
			WHERE u.status = 'activated'
			  AND va.activated_stake_lamports > 0
		`
		row := config.DB.QueryRow(ctx, query)
		return row.Scan(&resp.Network.TotalStakeSol)
	})

	g.Go(func() error {
		query := `
			SELECT
				COALESCE(
					(SELECT SUM(va.activated_stake_lamports)
					 FROM dz_users_current u
					 JOIN solana_gossip_nodes_current gn ON u.dz_ip = gn.gossip_ip
					 JOIN solana_vote_accounts_current va ON gn.pubkey = va.node_pubkey
					 WHERE u.status = 'activated' AND va.activated_stake_lamports > 0)
					* 100.0 / NULLIF((SELECT SUM(activated_stake_lamports) FROM solana_vote_accounts_current WHERE activated_stake_lamports > 0), 0),
					0
				) AS stake_share_pct
		`
		row := config.DB.QueryRow(ctx, query)
		return row.Scan(&resp.Network.StakeSharePct)
	})

	// Calculate stake share delta from 24 hours ago (or oldest available if less than 24h of data)
	g.Go(func() error {
		query := `
			WITH historical_ts AS (
				-- Get the oldest snapshot that's at least 1 hour old
				SELECT max(snapshot_ts) as ts
				FROM dim_solana_vote_accounts_history
				WHERE snapshot_ts <= now() - INTERVAL 1 HOUR
			),
			current_share AS (
				SELECT COALESCE(
					(SELECT SUM(va.activated_stake_lamports)
					 FROM dz_users_current u
					 JOIN solana_gossip_nodes_current gn ON u.dz_ip = gn.gossip_ip
					 JOIN solana_vote_accounts_current va ON gn.pubkey = va.node_pubkey
					 WHERE u.status = 'activated' AND va.activated_stake_lamports > 0)
					* 100.0 / NULLIF((SELECT SUM(activated_stake_lamports) FROM solana_vote_accounts_current WHERE activated_stake_lamports > 0), 0),
					0
				) AS pct
			),
			historical_share AS (
				SELECT COALESCE(
					(SELECT SUM(va.activated_stake_lamports)
					 FROM dim_dz_users_history u
					 JOIN solana_gossip_nodes_current gn ON u.dz_ip = gn.gossip_ip
					 JOIN dim_solana_vote_accounts_history va ON gn.pubkey = va.node_pubkey
					 WHERE u.status = 'activated'
					   AND va.activated_stake_lamports > 0
					   AND u.snapshot_ts = (SELECT max(snapshot_ts) FROM dim_dz_users_history WHERE snapshot_ts <= (SELECT ts FROM historical_ts))
					   AND va.snapshot_ts = (SELECT ts FROM historical_ts))
					* 100.0 / NULLIF((SELECT SUM(activated_stake_lamports) FROM dim_solana_vote_accounts_history
					  WHERE activated_stake_lamports > 0
					    AND snapshot_ts = (SELECT ts FROM historical_ts)), 0),
					0
				) AS pct
			)
			SELECT
				-- Only show delta if we have valid historical data (non-zero historical share)
				CASE WHEN historical_share.pct > 0
				     THEN current_share.pct - historical_share.pct
				     ELSE 0
				END AS delta
			FROM current_share, historical_share
		`
		row := config.DB.QueryRow(ctx, query)
		var delta float64
		if err := row.Scan(&delta); err != nil {
			// If historical data unavailable, delta is 0
			resp.Network.StakeShareDelta = 0
			return nil
		}
		resp.Network.StakeShareDelta = delta
		return nil
	})

	g.Go(func() error {
		query := `SELECT COUNT(*) FROM dz_users_current`
		row := config.DB.QueryRow(ctx, query)
		return row.Scan(&resp.Network.Users)
	})

	g.Go(func() error {
		query := `SELECT COUNT(*) FROM dz_devices_current`
		row := config.DB.QueryRow(ctx, query)
		return row.Scan(&resp.Network.Devices)
	})

	g.Go(func() error {
		query := `SELECT COUNT(*) FROM dz_links_current`
		row := config.DB.QueryRow(ctx, query)
		return row.Scan(&resp.Network.Links)
	})

	g.Go(func() error {
		query := `SELECT COUNT(DISTINCT pk) FROM dz_contributors_current`
		row := config.DB.QueryRow(ctx, query)
		return row.Scan(&resp.Network.Contributors)
	})

	g.Go(func() error {
		query := `SELECT COUNT(DISTINCT pk) FROM dz_metros_current`
		row := config.DB.QueryRow(ctx, query)
		return row.Scan(&resp.Network.Metros)
	})

	// Sum total bandwidth for activated links
	g.Go(func() error {
		query := `
			SELECT COALESCE(SUM(bandwidth_bps), 0)
			FROM dz_links_current
			WHERE status = 'activated'
		`
		row := config.DB.QueryRow(ctx, query)
		return row.Scan(&resp.Network.BandwidthBps)
	})

	g.Go(func() error {
		query := `
			SELECT COALESCE(SUM(interface_rate), 0) FROM (
				SELECT SUM(in_octets_delta) * 8.0 / NULLIF(SUM(delta_duration), 0) AS interface_rate
				FROM fact_dz_device_interface_counters
				WHERE event_ts > now() - INTERVAL 1 HOUR
				  AND user_tunnel_id IS NOT NULL
				GROUP BY device_pk, intf
			)
		`
		row := config.DB.QueryRow(ctx, query)
		return row.Scan(&resp.Network.UserInboundBps)
	})

	// Device status breakdown
	g.Go(func() error {
		query := `SELECT status, COUNT(*) as cnt FROM dz_devices_current GROUP BY status`
		rows, err := config.DB.Query(ctx, query)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var status string
			var cnt uint64
			if err := rows.Scan(&status, &cnt); err != nil {
				return err
			}
			resp.Network.DevicesByStatus[status] = cnt
		}
		return rows.Err()
	})

	// Link status breakdown
	g.Go(func() error {
		query := `SELECT status, COUNT(*) as cnt FROM dz_links_current GROUP BY status`
		rows, err := config.DB.Query(ctx, query)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var status string
			var cnt uint64
			if err := rows.Scan(&status, &cnt); err != nil {
				return err
			}
			resp.Network.LinksByStatus[status] = cnt
		}
		return rows.Err()
	})

	// Link health analysis
	g.Go(func() error {
		query := `
			SELECT
				l.pk,
				l.code,
				l.link_type,
				COALESCE(c.code, '') as contributor,
				l.bandwidth_bps,
				l.committed_rtt_ns / 1000.0 as committed_rtt_us,
				ma.code as side_a_metro,
				mz.code as side_z_metro,
				COALESCE(lat.avg_rtt_us, 0) as latency_us,
				COALESCE(lat.loss_percent, 0) as loss_percent,
				-- Use direct link traffic if available, otherwise use parent interface traffic
				COALESCE(traffic_direct.in_bps, traffic_parent.in_bps, 0) as in_bps,
				COALESCE(traffic_direct.out_bps, traffic_parent.out_bps, 0) as out_bps
			FROM dz_links_current l
			JOIN dz_devices_current da ON l.side_a_pk = da.pk
			JOIN dz_devices_current dz ON l.side_z_pk = dz.pk
			JOIN dz_metros_current ma ON da.metro_pk = ma.pk
			JOIN dz_metros_current mz ON dz.metro_pk = mz.pk
			LEFT JOIN dz_contributors_current c ON l.contributor_pk = c.pk
			LEFT JOIN (
				SELECT link_pk,
					avg(rtt_us) as avg_rtt_us,
					countIf(loss OR rtt_us = 0) * 100.0 / count(*) as loss_percent
				FROM fact_dz_device_link_latency
				WHERE event_ts > now() - INTERVAL 1 HOUR
				GROUP BY link_pk
			) lat ON l.pk = lat.link_pk
			-- Direct link traffic (where link_pk is populated)
			LEFT JOIN (
				SELECT link_pk,
					quantile(0.95)(CASE WHEN delta_duration > 0 THEN in_octets_delta * 8 / delta_duration ELSE 0 END) as in_bps,
					quantile(0.95)(CASE WHEN delta_duration > 0 THEN out_octets_delta * 8 / delta_duration ELSE 0 END) as out_bps
				FROM fact_dz_device_interface_counters
				WHERE event_ts > now() - INTERVAL 24 HOUR
					AND link_pk != ''
				GROUP BY link_pk
			) traffic_direct ON l.pk = traffic_direct.link_pk
			-- Parent interface traffic (for sub-interfaces like PortChannel2000.10023)
			LEFT JOIN (
				SELECT device_pk, intf,
					quantile(0.95)(CASE WHEN delta_duration > 0 THEN in_octets_delta * 8 / delta_duration ELSE 0 END) as in_bps,
					quantile(0.95)(CASE WHEN delta_duration > 0 THEN out_octets_delta * 8 / delta_duration ELSE 0 END) as out_bps
				FROM fact_dz_device_interface_counters
				WHERE event_ts > now() - INTERVAL 24 HOUR
				GROUP BY device_pk, intf
			) traffic_parent ON traffic_parent.device_pk = l.side_a_pk
				AND traffic_parent.intf = splitByChar('.', l.side_a_iface_name)[1]
				AND traffic_direct.link_pk IS NULL
			WHERE l.status = 'activated'
		`
		rows, err := config.DB.Query(ctx, query)
		if err != nil {
			return err
		}
		defer rows.Close()

		var healthy, degraded, unhealthy, disabled uint64
		var issues []LinkIssue
		var highUtil []LinkMetric
		var allUtilLinks []LinkMetric

		// Threshold for "disabled" classification - near 100% loss over 3h window
		// indicates the link has been down for an extended period
		const DisabledLossPct = 95.0

		for rows.Next() {
			var pk, code, linkType, contributor, sideAMetro, sideZMetro string
			var bandwidthBps int64
			var committedRttUs, latencyUs, lossPct, inBps, outBps float64

			if err := rows.Scan(&pk, &code, &linkType, &contributor, &bandwidthBps, &committedRttUs, &sideAMetro, &sideZMetro, &latencyUs, &lossPct, &inBps, &outBps); err != nil {
				return err
			}

			// Calculate latency overage percentage vs committed RTT
			// Only consider latency for inter-metro WAN links
			var latencyOveragePct float64
			isInterMetroWAN := linkType == "WAN" && sideAMetro != sideZMetro
			if isInterMetroWAN && committedRttUs > 0 && latencyUs > 0 {
				latencyOveragePct = ((latencyUs - committedRttUs) / committedRttUs) * 100
			}

			// Classify link health
			// First check for "disabled" - extended packet loss (near 100% over 3h)
			isDisabled := lossPct >= DisabledLossPct
			isUnhealthy := !isDisabled && (lossPct >= LossCriticalPct || latencyOveragePct >= LatencyCriticalPct)
			isDegraded := !isDisabled && !isUnhealthy && (lossPct >= LossWarningPct || latencyOveragePct >= LatencyWarningPct)

			if isDisabled {
				disabled++
			} else if isUnhealthy {
				unhealthy++
			} else if isDegraded {
				degraded++
			} else {
				healthy++
			}

			// Track issues (top 10)
			if lossPct >= LossWarningPct && len(issues) < 10 {
				issues = append(issues, LinkIssue{
					Code:        code,
					LinkType:    linkType,
					Contributor: contributor,
					Issue:       "packet_loss",
					Value:       lossPct,
					Threshold:   LossWarningPct,
					SideAMetro:  sideAMetro,
					SideZMetro:  sideZMetro,
				})
			}
			if isInterMetroWAN && latencyOveragePct >= LatencyWarningPct && len(issues) < 10 {
				issues = append(issues, LinkIssue{
					Code:        code,
					LinkType:    linkType,
					Contributor: contributor,
					Issue:       "high_latency",
					Value:       latencyOveragePct, // Now shows % over committed
					Threshold:   LatencyWarningPct,
					SideAMetro:  sideAMetro,
					SideZMetro:  sideZMetro,
				})
			}

			// Track utilization links
			if bandwidthBps > 0 {
				utilIn := (inBps / float64(bandwidthBps)) * 100
				utilOut := (outBps / float64(bandwidthBps)) * 100
				metric := LinkMetric{
					PK:             pk,
					Code:           code,
					LinkType:       linkType,
					Contributor:    contributor,
					BandwidthBps:   bandwidthBps,
					InBps:          inBps,
					OutBps:         outBps,
					UtilizationIn:  utilIn,
					UtilizationOut: utilOut,
					SideAMetro:     sideAMetro,
					SideZMetro:     sideZMetro,
				}
				// Track all for top utilization list
				allUtilLinks = append(allUtilLinks, metric)
				// Track high utilization (>70%) separately
				if (utilIn >= UtilWarningPct || utilOut >= UtilWarningPct) && len(highUtil) < 10 {
					highUtil = append(highUtil, metric)
				}
			}
		}

		// Sort all links by max utilization (descending) and take top 10
		sort.Slice(allUtilLinks, func(i, j int) bool {
			maxI := allUtilLinks[i].UtilizationIn
			if allUtilLinks[i].UtilizationOut > maxI {
				maxI = allUtilLinks[i].UtilizationOut
			}
			maxJ := allUtilLinks[j].UtilizationIn
			if allUtilLinks[j].UtilizationOut > maxJ {
				maxJ = allUtilLinks[j].UtilizationOut
			}
			return maxI > maxJ
		})
		if len(allUtilLinks) > 100 {
			allUtilLinks = allUtilLinks[:100]
		}

		resp.Links.Total = healthy + degraded + unhealthy + disabled
		resp.Links.Healthy = healthy
		resp.Links.Degraded = degraded
		resp.Links.Unhealthy = unhealthy
		resp.Links.Disabled = disabled
		resp.Links.HighUtilLinks = highUtil
		resp.Links.TopUtilLinks = allUtilLinks

		// Populate issue start times - find when the CURRENT continuous issue started
		if len(issues) > 0 {
			// Build list of link codes to query
			linkCodes := make([]string, len(issues))
			for i, issue := range issues {
				linkCodes[i] = issue.Code
			}

			// Query to find when the current continuous issue started:
			// Find the most recent healthy hour, then the issue started the hour after.
			// If no healthy hour exists in the last 7 days, use the earliest data we have.
			issueStartQuery := `
				WITH hourly AS (
					SELECT
						l.code,
						toStartOfHour(event_ts) as hour,
						countIf(loss OR rtt_us = 0) * 100.0 / count(*) as loss_pct
					FROM fact_dz_device_link_latency lat
					JOIN dz_links_current l ON lat.link_pk = l.pk
					WHERE lat.event_ts > now() - INTERVAL 7 DAY
					  AND l.code IN (?)
					GROUP BY l.code, hour
				),
				last_healthy AS (
					SELECT code, max(hour) as last_good_hour
					FROM hourly
					WHERE loss_pct < ?
					GROUP BY code
				),
				earliest_issue AS (
					SELECT code, min(hour) as first_issue_hour
					FROM hourly
					WHERE loss_pct >= ?
					GROUP BY code
				)
				SELECT
					ei.code,
					if(lh.code != '',
					   lh.last_good_hour + INTERVAL 1 HOUR,
					   ei.first_issue_hour) as issue_start
				FROM earliest_issue ei
				LEFT JOIN last_healthy lh ON ei.code = lh.code
			`
			issueRows, err := config.DB.Query(ctx, issueStartQuery, linkCodes, LossWarningPct, LossWarningPct)
			if err == nil {
				defer issueRows.Close()
				issueSince := make(map[string]time.Time)
				for issueRows.Next() {
					var code string
					var issueStart time.Time
					if err := issueRows.Scan(&code, &issueStart); err == nil {
						issueSince[code] = issueStart
					}
				}
				// Populate Since field and filter out resolved issues
				// An issue is considered resolved if its calculated start time is in the future,
				// which happens when the current hour is healthy (last_good_hour + 1h > now)
				now := time.Now()
				filtered := issues[:0]
				for i := range issues {
					if since, ok := issueSince[issues[i].Code]; ok {
						if since.After(now) {
							// Issue has ended - the current hour is healthy
							continue
						}
						issues[i].Since = since.UTC().Format(time.RFC3339)
					}
					filtered = append(filtered, issues[i])
				}
				issues = filtered
			}
		}

		// Add "no_data" issues for links that stopped reporting latency data
		// These are links with historical data (30 days) but no recent data (15 minutes)
		noDataQuery := `
			WITH link_last_seen AS (
				SELECT
					link_pk,
					max(event_ts) as last_seen
				FROM fact_dz_device_link_latency
				WHERE event_ts >= now() - INTERVAL 30 DAY
				  AND link_pk != ''
				GROUP BY link_pk
			)
			SELECT
				l.code,
				l.link_type,
				COALESCE(c.code, '') as contributor,
				COALESCE(ma.code, '') as side_a_metro,
				COALESCE(mz.code, '') as side_z_metro,
				lls.last_seen
			FROM link_last_seen lls
			JOIN dz_links_current l ON lls.link_pk = l.pk
			LEFT JOIN dz_contributors_current c ON l.contributor_pk = c.pk
			LEFT JOIN dz_devices_current da ON l.side_a_pk = da.pk
			LEFT JOIN dz_metros_current ma ON da.metro_pk = ma.pk
			LEFT JOIN dz_devices_current dz ON l.side_z_pk = dz.pk
			LEFT JOIN dz_metros_current mz ON dz.metro_pk = mz.pk
			WHERE lls.last_seen < now() - INTERVAL 15 MINUTE
			  AND lls.last_seen >= now() - INTERVAL 30 DAY
			  AND l.status NOT IN ('soft-drained', 'hard-drained')
			ORDER BY lls.last_seen DESC
			LIMIT 10
		`
		noDataRows, err := config.DB.Query(ctx, noDataQuery)
		if err == nil {
			defer noDataRows.Close()
			for noDataRows.Next() {
				var code, linkType, contributor, sideAMetro, sideZMetro string
				var lastSeen time.Time
				if err := noDataRows.Scan(&code, &linkType, &contributor, &sideAMetro, &sideZMetro, &lastSeen); err == nil {
					// The outage started when we last saw data (plus 5 min buffer for expected interval)
					since := lastSeen.Add(5 * time.Minute)
					issues = append(issues, LinkIssue{
						Code:        code,
						LinkType:    linkType,
						Contributor: contributor,
						Issue:       "no_data",
						Value:       0,
						Threshold:   0,
						SideAMetro:  sideAMetro,
						SideZMetro:  sideZMetro,
						Since:       since.UTC().Format(time.RFC3339),
					})
				}
			}
		}

		resp.Links.Issues = issues

		return rows.Err()
	})

	// Performance metrics (WAN links, last 3 hours)
	g.Go(func() error {
		query := `
			SELECT
				ifNotFinite(avg(rtt_us), 0) as avg_latency,
				ifNotFinite(quantile(0.95)(rtt_us), 0) as p95_latency,
				ifNotFinite(toFloat64(min(rtt_us)), 0) as min_latency,
				ifNotFinite(toFloat64(max(rtt_us)), 0) as max_latency,
				ifNotFinite(countIf(loss OR rtt_us = 0) * 100.0 / count(*), 0) as avg_loss,
				ifNotFinite(avg(abs(ipdv_us)), 0) as avg_jitter
			FROM fact_dz_device_link_latency lat
			JOIN dz_links_current l ON lat.link_pk = l.pk
			WHERE lat.event_ts > now() - INTERVAL 3 HOUR
			  AND l.link_type = 'WAN'
			  AND lat.loss = false
			  AND lat.rtt_us > 0
		`
		row := config.DB.QueryRow(ctx, query)
		return row.Scan(
			&resp.Performance.AvgLatencyUs,
			&resp.Performance.P95LatencyUs,
			&resp.Performance.MinLatencyUs,
			&resp.Performance.MaxLatencyUs,
			&resp.Performance.AvgLossPercent,
			&resp.Performance.AvgJitterUs,
		)
	})

	// Total throughput (sum of per-interface rates)
	g.Go(func() error {
		query := `
			SELECT
				COALESCE(SUM(in_rate), 0) as total_in_bps,
				COALESCE(SUM(out_rate), 0) as total_out_bps
			FROM (
				SELECT
					SUM(in_octets_delta) * 8.0 / NULLIF(SUM(delta_duration), 0) AS in_rate,
					SUM(out_octets_delta) * 8.0 / NULLIF(SUM(delta_duration), 0) AS out_rate
				FROM fact_dz_device_interface_counters
				WHERE event_ts > now() - INTERVAL 5 MINUTE
				  AND link_pk != ''
				GROUP BY device_pk, intf
			)
		`
		row := config.DB.QueryRow(ctx, query)
		return row.Scan(&resp.Performance.TotalInBps, &resp.Performance.TotalOutBps)
	})

	// Top device utilization by tunnel usage
	g.Go(func() error {
		query := `
			SELECT
				d.pk,
				d.code,
				d.device_type,
				COALESCE(c.code, '') as contributor,
				m.code as metro,
				toInt32(count(u.pk)) as current_users,
				d.max_users,
				CASE WHEN d.max_users > 0 THEN count(u.pk) * 100.0 / d.max_users ELSE 0 END as utilization
			FROM dz_devices_current d
			LEFT JOIN dz_users_current u ON u.device_pk = d.pk
			LEFT JOIN dz_contributors_current c ON d.contributor_pk = c.pk
			LEFT JOIN dz_metros_current m ON d.metro_pk = m.pk
			WHERE d.status = 'activated'
			  AND d.max_users > 0
			GROUP BY d.pk, d.code, d.device_type, c.code, m.code, d.max_users
			ORDER BY utilization DESC
			LIMIT 100
		`
		rows, err := config.DB.Query(ctx, query)
		if err != nil {
			return err
		}
		defer rows.Close()

		var devices []DeviceUtilization
		for rows.Next() {
			var d DeviceUtilization
			if err := rows.Scan(&d.PK, &d.Code, &d.DeviceType, &d.Contributor, &d.Metro, &d.CurrentUsers, &d.MaxUsers, &d.Utilization); err != nil {
				return err
			}
			devices = append(devices, d)
		}
		resp.TopDeviceUtil = devices
		return rows.Err()
	})

	// Interface issues (errors, discards, carrier transitions in last 1 hour)
	g.Go(func() error {
		query := `
			SELECT
				d.pk as device_pk,
				d.code as device_code,
				d.device_type,
				COALESCE(contrib.code, '') as contributor,
				m.code as metro,
				c.intf as interface_name,
				COALESCE(l.pk, '') as link_pk,
				COALESCE(l.code, '') as link_code,
				COALESCE(l.link_type, '') as link_type,
				COALESCE(c.link_side, '') as link_side,
				toUInt64(SUM(c.in_errors_delta)) as in_errors,
				toUInt64(SUM(c.out_errors_delta)) as out_errors,
				toUInt64(SUM(c.in_discards_delta)) as in_discards,
				toUInt64(SUM(c.out_discards_delta)) as out_discards,
				toUInt64(SUM(c.carrier_transitions_delta)) as carrier_transitions,
				formatDateTime(min(c.event_ts), '%Y-%m-%dT%H:%i:%sZ', 'UTC') as first_seen,
				formatDateTime(max(c.event_ts), '%Y-%m-%dT%H:%i:%sZ', 'UTC') as last_seen
			FROM fact_dz_device_interface_counters c
			JOIN dz_devices_current d ON c.device_pk = d.pk
			JOIN dz_metros_current m ON d.metro_pk = m.pk
			LEFT JOIN dz_contributors_current contrib ON d.contributor_pk = contrib.pk
			LEFT JOIN dz_links_current l ON c.link_pk = l.pk
			WHERE c.event_ts > now() - INTERVAL 1 HOUR
			  AND d.status = 'activated'
			  AND (c.in_errors_delta > 0 OR c.out_errors_delta > 0 OR c.in_discards_delta > 0 OR c.out_discards_delta > 0 OR c.carrier_transitions_delta > 0)
			GROUP BY d.pk, d.code, d.device_type, contrib.code, m.code, c.intf, l.pk, l.code, l.link_type, c.link_side
			ORDER BY (in_errors + out_errors + in_discards + out_discards + carrier_transitions) DESC
			LIMIT 20
		`
		rows, err := config.DB.Query(ctx, query)
		if err != nil {
			return err
		}
		defer rows.Close()

		var issues []InterfaceIssue
		for rows.Next() {
			var issue InterfaceIssue
			if err := rows.Scan(
				&issue.DevicePK,
				&issue.DeviceCode,
				&issue.DeviceType,
				&issue.Contributor,
				&issue.Metro,
				&issue.InterfaceName,
				&issue.LinkPK,
				&issue.LinkCode,
				&issue.LinkType,
				&issue.LinkSide,
				&issue.InErrors,
				&issue.OutErrors,
				&issue.InDiscards,
				&issue.OutDiscards,
				&issue.CarrierTransitions,
				&issue.FirstSeen,
				&issue.LastSeen,
			); err != nil {
				return err
			}
			issues = append(issues, issue)
		}
		resp.Interfaces.Issues = issues
		return rows.Err()
	})

	// Non-activated devices
	g.Go(func() error {
		query := `
			SELECT
				d.pk,
				d.code,
				d.device_type,
				m.code as metro,
				d.status,
				formatDateTime(d.snapshot_ts, '%Y-%m-%dT%H:%i:%sZ', 'UTC') as since
			FROM dz_devices_current d
			JOIN dz_metros_current m ON d.metro_pk = m.pk
			WHERE d.status != 'activated'
			ORDER BY d.snapshot_ts DESC
			LIMIT 50
		`
		rows, err := config.DB.Query(ctx, query)
		if err != nil {
			return err
		}
		defer rows.Close()

		var devices []NonActivatedDevice
		for rows.Next() {
			var dev NonActivatedDevice
			if err := rows.Scan(&dev.PK, &dev.Code, &dev.DeviceType, &dev.Metro, &dev.Status, &dev.Since); err != nil {
				return err
			}
			devices = append(devices, dev)
		}
		resp.Alerts.Devices = devices
		return rows.Err()
	})

	// Non-activated links (including delay-override drained)
	g.Go(func() error {
		// 1000ms delay override in nanoseconds indicates soft-drained
		const delayOverrideSoftDrainedNs = 1_000_000_000
		query := `
			SELECT
				l.pk,
				l.code,
				l.link_type,
				ma.code as side_a_metro,
				mz.code as side_z_metro,
				CASE
					WHEN l.status = 'activated' AND l.isis_delay_override_ns = ? THEN 'soft-drained'
					ELSE l.status
				END as status,
				formatDateTime(l.snapshot_ts, '%Y-%m-%dT%H:%i:%sZ', 'UTC') as since
			FROM dz_links_current l
			JOIN dz_devices_current da ON l.side_a_pk = da.pk
			JOIN dz_devices_current dz ON l.side_z_pk = dz.pk
			JOIN dz_metros_current ma ON da.metro_pk = ma.pk
			JOIN dz_metros_current mz ON dz.metro_pk = mz.pk
			WHERE l.status != 'activated' OR l.isis_delay_override_ns = ?
			ORDER BY l.snapshot_ts DESC
			LIMIT 50
		`
		rows, err := config.DB.Query(ctx, query, delayOverrideSoftDrainedNs, delayOverrideSoftDrainedNs)
		if err != nil {
			return err
		}
		defer rows.Close()

		var links []NonActivatedLink
		for rows.Next() {
			var link NonActivatedLink
			if err := rows.Scan(&link.PK, &link.Code, &link.LinkType, &link.SideAMetro, &link.SideZMetro, &link.Status, &link.Since); err != nil {
				return err
			}
			links = append(links, link)
		}
		resp.Alerts.Links = links
		return rows.Err()
	})

	err := g.Wait()
	duration := time.Since(start)
	metrics.RecordClickHouseQuery(duration, err)

	if err != nil {
		log.Printf("Status query error: %v", err)
		resp.Error = err.Error()
	}

	// Determine overall status
	resp.Status = determineOverallStatus(resp)

	return resp
}

// Link history types for status timeline
type LinkHourStatus struct {
	Hour         string  `json:"hour"`
	Status       string  `json:"status"` // "healthy", "degraded", "unhealthy", "no_data"
	AvgLatencyUs float64 `json:"avg_latency_us"`
	AvgLossPct   float64 `json:"avg_loss_pct"`
	Samples      uint64  `json:"samples"`
	// Per-side latency/loss metrics (direction: A→Z vs Z→A)
	SideALatencyUs float64 `json:"side_a_latency_us,omitempty"`
	SideALossPct   float64 `json:"side_a_loss_pct,omitempty"`
	SideASamples   uint64  `json:"side_a_samples,omitempty"`
	SideZLatencyUs float64 `json:"side_z_latency_us,omitempty"`
	SideZLossPct   float64 `json:"side_z_loss_pct,omitempty"`
	SideZSamples   uint64  `json:"side_z_samples,omitempty"`
	// Per-side interface issues (errors, discards, carrier transitions)
	SideAInErrors           uint64 `json:"side_a_in_errors,omitempty"`
	SideAOutErrors          uint64 `json:"side_a_out_errors,omitempty"`
	SideAInDiscards         uint64 `json:"side_a_in_discards,omitempty"`
	SideAOutDiscards        uint64 `json:"side_a_out_discards,omitempty"`
	SideACarrierTransitions uint64 `json:"side_a_carrier_transitions,omitempty"`
	SideZInErrors           uint64 `json:"side_z_in_errors,omitempty"`
	SideZOutErrors          uint64 `json:"side_z_out_errors,omitempty"`
	SideZInDiscards         uint64 `json:"side_z_in_discards,omitempty"`
	SideZOutDiscards        uint64 `json:"side_z_out_discards,omitempty"`
	SideZCarrierTransitions uint64 `json:"side_z_carrier_transitions,omitempty"`
}

type LinkHistory struct {
	PK             string           `json:"pk"`
	Code           string           `json:"code"`
	LinkType       string           `json:"link_type"`
	Contributor    string           `json:"contributor"`
	SideAMetro     string           `json:"side_a_metro"`
	SideZMetro     string           `json:"side_z_metro"`
	SideADevice    string           `json:"side_a_device"`
	SideZDevice    string           `json:"side_z_device"`
	BandwidthBps   int64            `json:"bandwidth_bps"`
	CommittedRttUs float64          `json:"committed_rtt_us"`
	Hours          []LinkHourStatus `json:"hours"`
	IssueReasons   []string         `json:"issue_reasons"` // "packet_loss", "high_latency", "drained", "no_data", "interface_errors", "discards", "carrier_transitions"
}

type LinkHistoryResponse struct {
	Links          []LinkHistory `json:"links"`
	TimeRange      string        `json:"time_range"`       // "24h", "3d", "7d"
	BucketMinutes  int           `json:"bucket_minutes"`   // Size of each bucket in minutes
	BucketCount    int           `json:"bucket_count"`     // Number of buckets
	Error          string        `json:"error,omitempty"`
}

func GetLinkHistory(w http.ResponseWriter, r *http.Request) {
	// Parse time range parameter
	timeRange := r.URL.Query().Get("range")
	if timeRange == "" {
		timeRange = "24h"
	}

	// Parse optional bucket count (for responsive display)
	requestedBuckets := 72 // default
	if b := r.URL.Query().Get("buckets"); b != "" {
		if n, err := strconv.Atoi(b); err == nil && n >= 12 && n <= 168 {
			requestedBuckets = n
		}
	}

	// Try to serve from cache first
	if statusCache != nil {
		if cached := statusCache.GetLinkHistory(timeRange, requestedBuckets); cached != nil {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Cache", "HIT")
			if err := json.NewEncoder(w).Encode(cached); err != nil {
				log.Printf("JSON encoding error: %v", err)
			}
			return
		}
	}

	// Cache miss - fetch fresh data
	w.Header().Set("X-Cache", "MISS")
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	resp, err := fetchLinkHistoryData(ctx, timeRange, requestedBuckets)
	if err != nil {
		log.Printf("fetchLinkHistoryData error: %v", err)
		http.Error(w, "Failed to fetch link history", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("JSON encoding error: %v", err)
	}
}

// fetchLinkHistoryData performs the actual link history data fetch from the database.
// This is called by both the cache refresh and direct requests.
func fetchLinkHistoryData(ctx context.Context, timeRange string, requestedBuckets int) (*LinkHistoryResponse, error) {
	start := time.Now()

	// Configure bucket size based on time range and requested bucket count
	var totalMinutes int
	switch timeRange {
	case "1h":
		totalMinutes = 60
	case "3h":
		totalMinutes = 3 * 60
	case "6h":
		totalMinutes = 6 * 60
	case "12h":
		totalMinutes = 12 * 60
	case "3d":
		totalMinutes = 3 * 24 * 60
	case "7d":
		totalMinutes = 7 * 24 * 60
	default: // "24h"
		timeRange = "24h"
		totalMinutes = 24 * 60
	}

	// Calculate bucket size to fit requested number of buckets
	bucketMinutes := totalMinutes / requestedBuckets
	if bucketMinutes < 5 {
		bucketMinutes = 5 // minimum 5 minutes
	}
	bucketCount := totalMinutes / bucketMinutes
	totalHours := totalMinutes / 60

	// Build the bucket interval expression
	var bucketInterval string
	if bucketMinutes >= 60 && bucketMinutes%60 == 0 {
		bucketInterval = fmt.Sprintf("toStartOfInterval(event_ts, INTERVAL %d HOUR)", bucketMinutes/60)
	} else {
		bucketInterval = fmt.Sprintf("toStartOfInterval(event_ts, INTERVAL %d MINUTE)", bucketMinutes)
	}

	// Get all WAN links with their metadata
	linkQuery := `
		SELECT
			l.pk,
			l.code,
			l.link_type,
			COALESCE(c.code, '') as contributor,
			ma.code as side_a_metro,
			mz.code as side_z_metro,
			da.code as side_a_device,
			dz.code as side_z_device,
			l.bandwidth_bps,
			l.committed_rtt_ns / 1000.0 as committed_rtt_us,
			l.isis_delay_override_ns,
			l.status
		FROM dz_links_current l
		JOIN dz_devices_current da ON l.side_a_pk = da.pk
		JOIN dz_devices_current dz ON l.side_z_pk = dz.pk
		JOIN dz_metros_current ma ON da.metro_pk = ma.pk
		JOIN dz_metros_current mz ON dz.metro_pk = mz.pk
		LEFT JOIN dz_contributors_current c ON l.contributor_pk = c.pk
		WHERE l.status IN ('activated', 'soft-drained', 'hard-drained')
	`

	linkRows, err := config.DB.Query(ctx, linkQuery)
	if err != nil {
		return nil, fmt.Errorf("link history query error: %w", err)
	}
	defer linkRows.Close()

	// Build map of link metadata
	type linkMeta struct {
		code              string
		linkType          string
		contributor       string
		sideAMetro        string
		sideZMetro        string
		sideADevice       string
		sideZDevice       string
		bandwidthBps      int64
		committedRttUs    float64
		delayOverrideNs   int64
		status            string
	}
	linkMap := make(map[string]linkMeta)

	for linkRows.Next() {
		var pk string
		var meta linkMeta
		if err := linkRows.Scan(&pk, &meta.code, &meta.linkType, &meta.contributor, &meta.sideAMetro, &meta.sideZMetro, &meta.sideADevice, &meta.sideZDevice, &meta.bandwidthBps, &meta.committedRttUs, &meta.delayOverrideNs, &meta.status); err != nil {
			return nil, fmt.Errorf("link scan error: %w", err)
		}
		linkMap[pk] = meta
	}
	if err := linkRows.Err(); err != nil {
		return nil, fmt.Errorf("link rows iteration error: %w", err)
	}

	// Get stats for the configured time range, grouped by direction (A→Z vs Z→A)
	// direction = 'A' means origin is side_a (A→Z), direction = 'Z' means origin is side_z (Z→A)
	historyQuery := `
		SELECT
			f.link_pk,
			` + bucketInterval + ` as bucket,
			if(f.origin_device_pk = l.side_a_pk, 'A', 'Z') as direction,
			avg(f.rtt_us) as avg_latency,
			countIf(f.loss OR f.rtt_us = 0) * 100.0 / count(*) as loss_pct,
			count(*) as samples
		FROM fact_dz_device_link_latency f
		JOIN dz_links_current l ON f.link_pk = l.pk
		WHERE f.event_ts > now() - INTERVAL ? HOUR
		GROUP BY f.link_pk, bucket, direction
		ORDER BY f.link_pk, bucket, direction
	`

	historyRows, err := config.DB.Query(ctx, historyQuery, totalHours)
	if err != nil {
		return nil, fmt.Errorf("link history stats query error: %w", err)
	}
	defer historyRows.Close()

	// Build bucket stats per link with per-side breakdown
	type sideStats struct {
		avgLatency float64
		lossPct    float64
		samples    uint64
	}
	type bucketStats struct {
		bucket     time.Time
		avgLatency float64
		lossPct    float64
		samples    uint64
		sideA      *sideStats
		sideZ      *sideStats
	}
	// Use a nested map: linkPK -> bucket time string -> bucketStats
	linkBucketMap := make(map[string]map[string]*bucketStats)

	for historyRows.Next() {
		var linkPK, direction string
		var bucket time.Time
		var avgLatency, lossPct float64
		var samples uint64
		if err := historyRows.Scan(&linkPK, &bucket, &direction, &avgLatency, &lossPct, &samples); err != nil {
			return nil, fmt.Errorf("history scan error: %w", err)
		}

		bucketKey := bucket.UTC().Format(time.RFC3339)
		if linkBucketMap[linkPK] == nil {
			linkBucketMap[linkPK] = make(map[string]*bucketStats)
		}
		if linkBucketMap[linkPK][bucketKey] == nil {
			linkBucketMap[linkPK][bucketKey] = &bucketStats{bucket: bucket}
		}
		stats := linkBucketMap[linkPK][bucketKey]

		// Store per-side stats
		if direction == "A" {
			stats.sideA = &sideStats{avgLatency: avgLatency, lossPct: lossPct, samples: samples}
		} else {
			stats.sideZ = &sideStats{avgLatency: avgLatency, lossPct: lossPct, samples: samples}
		}
	}
	if err := historyRows.Err(); err != nil {
		return nil, fmt.Errorf("history rows iteration error: %w", err)
	}

	// Compute aggregate stats for each bucket (combining both directions)
	linkBuckets := make(map[string][]bucketStats)
	for linkPK, bucketMap := range linkBucketMap {
		var buckets []bucketStats
		for _, stats := range bucketMap {
			// Combine stats from both directions
			var totalLatency, totalLoss float64
			var totalSamples uint64
			var count int

			if stats.sideA != nil {
				totalLatency += stats.sideA.avgLatency * float64(stats.sideA.samples)
				totalLoss += stats.sideA.lossPct * float64(stats.sideA.samples)
				totalSamples += stats.sideA.samples
				count++
			}
			if stats.sideZ != nil {
				totalLatency += stats.sideZ.avgLatency * float64(stats.sideZ.samples)
				totalLoss += stats.sideZ.lossPct * float64(stats.sideZ.samples)
				totalSamples += stats.sideZ.samples
				count++
			}

			if totalSamples > 0 {
				stats.avgLatency = totalLatency / float64(totalSamples)
				stats.lossPct = totalLoss / float64(totalSamples)
				stats.samples = totalSamples
			}
			buckets = append(buckets, *stats)
		}
		linkBuckets[linkPK] = buckets
	}

	// Get interface issues per link per bucket (grouped by side)
	// Use greatest(0, delta) to ignore negative deltas (counter resets from device restarts)
	interfaceQuery := `
		SELECT
			link_pk,
			link_side,
			` + bucketInterval + ` as bucket,
			toUInt64(SUM(greatest(0, in_errors_delta))) as in_errors,
			toUInt64(SUM(greatest(0, out_errors_delta))) as out_errors,
			toUInt64(SUM(greatest(0, in_discards_delta))) as in_discards,
			toUInt64(SUM(greatest(0, out_discards_delta))) as out_discards,
			toUInt64(SUM(greatest(0, carrier_transitions_delta))) as carrier_transitions
		FROM fact_dz_device_interface_counters
		WHERE event_ts > now() - INTERVAL ? HOUR
		  AND link_pk != ''
		GROUP BY link_pk, link_side, bucket
		ORDER BY link_pk, link_side, bucket
	`

	interfaceRows, err := config.DB.Query(ctx, interfaceQuery, totalHours)
	if err != nil {
		return nil, fmt.Errorf("link interface query error: %w", err)
	}
	defer interfaceRows.Close()

	// Build interface stats per link per bucket per side
	type interfaceStats struct {
		inErrors           uint64
		outErrors          uint64
		inDiscards         uint64
		outDiscards        uint64
		carrierTransitions uint64
	}
	type linkInterfaceBucketKey struct {
		linkPK string
		bucket string
	}
	linkInterfaceBuckets := make(map[linkInterfaceBucketKey]map[string]*interfaceStats) // key -> side -> stats

	for interfaceRows.Next() {
		var linkPK, linkSide string
		var bucket time.Time
		var inErrors, outErrors, inDiscards, outDiscards, carrierTransitions uint64
		if err := interfaceRows.Scan(&linkPK, &linkSide, &bucket, &inErrors, &outErrors, &inDiscards, &outDiscards, &carrierTransitions); err != nil {
			return nil, fmt.Errorf("interface scan error: %w", err)
		}
		bucketKey := bucket.UTC().Format(time.RFC3339)
		key := linkInterfaceBucketKey{linkPK: linkPK, bucket: bucketKey}
		if linkInterfaceBuckets[key] == nil {
			linkInterfaceBuckets[key] = make(map[string]*interfaceStats)
		}
		linkInterfaceBuckets[key][linkSide] = &interfaceStats{
			inErrors:           inErrors,
			outErrors:          outErrors,
			inDiscards:         inDiscards,
			outDiscards:        outDiscards,
			carrierTransitions: carrierTransitions,
		}
	}
	if err := interfaceRows.Err(); err != nil {
		return nil, fmt.Errorf("interface rows iteration error: %w", err)
	}

	// Get historical link status per bucket from dim_dz_links_history
	// This tells us if a link was drained at each point in time
	// Build bucket interval for snapshot_ts (history table uses snapshot_ts, not event_ts)
	var historyBucketInterval string
	if bucketMinutes >= 60 && bucketMinutes%60 == 0 {
		historyBucketInterval = fmt.Sprintf("toStartOfInterval(snapshot_ts, INTERVAL %d HOUR)", bucketMinutes/60)
	} else {
		historyBucketInterval = fmt.Sprintf("toStartOfInterval(snapshot_ts, INTERVAL %d MINUTE)", bucketMinutes)
	}

	statusHistoryQuery := `
		SELECT
			pk as link_pk,
			` + historyBucketInterval + ` as bucket,
			argMax(status, snapshot_ts) as status
		FROM dim_dz_links_history
		WHERE snapshot_ts > now() - INTERVAL ? HOUR
		GROUP BY link_pk, bucket
		ORDER BY link_pk, bucket
	`

	statusRows, err := config.DB.Query(ctx, statusHistoryQuery, totalHours)
	if err != nil {
		log.Printf("Link status history query error: %v", err)
		// Non-fatal - continue without historical status
	}

	// Build map of link status per bucket
	type linkBucketKey struct {
		linkPK string
		bucket string
	}
	linkStatusHistory := make(map[linkBucketKey]string)

	if statusRows != nil {
		defer statusRows.Close()
		for statusRows.Next() {
			var linkPK, status string
			var bucket time.Time
			if err := statusRows.Scan(&linkPK, &bucket, &status); err != nil {
				return nil, fmt.Errorf("status history scan error: %w", err)
			}
			key := linkBucketKey{linkPK: linkPK, bucket: bucket.UTC().Format(time.RFC3339)}
			linkStatusHistory[key] = status
		}
		if err := statusRows.Err(); err != nil {
			return nil, fmt.Errorf("status rows iteration error: %w", err)
		}
	}

	duration := time.Since(start)
	metrics.RecordClickHouseQuery(duration, err)

	// Build response with all buckets for each link
	now := time.Now().UTC()
	bucketDuration := time.Duration(bucketMinutes) * time.Minute
	var links []LinkHistory

	// 1000ms delay override in nanoseconds indicates soft-drained
	const delayOverrideSoftDrainedNs = 1_000_000_000

	for pk, meta := range linkMap {
		// Check if link is currently drained (by status or delay override)
		isCurrentlyDrained := meta.status == "soft-drained" || meta.status == "hard-drained" || meta.delayOverrideNs == delayOverrideSoftDrainedNs

		// Track issue reasons for this link
		issueReasons := make(map[string]bool)

		// Check if this link has any issues in the time range
		buckets := linkBuckets[pk]

		if isCurrentlyDrained {
			issueReasons["drained"] = true
		}

		// Check latency/loss issues
		for _, b := range buckets {
			// Check for packet loss issues
			if b.lossPct >= LossWarningPct {
				issueReasons["packet_loss"] = true
			}
			// Check for high latency issues (WAN links only, excluding intra-metro)
			isInterMetro := meta.sideAMetro != meta.sideZMetro
			if meta.linkType == "WAN" && isInterMetro && meta.committedRttUs > 0 && b.avgLatency > 0 {
				latencyOveragePct := ((b.avgLatency - meta.committedRttUs) / meta.committedRttUs) * 100
				if latencyOveragePct >= LatencyWarningPct {
					issueReasons["high_latency"] = true
				}
			}
		}

		// Also check if link was drained at any point in the history
		for key := range linkStatusHistory {
			if key.linkPK == pk {
				if linkStatusHistory[key] == "soft-drained" || linkStatusHistory[key] == "hard-drained" {
					issueReasons["drained"] = true
					break
				}
			}
		}

		// Include all links (both healthy and those with issues)

		// Convert issue reasons to slice
		var issueReasonsList []string
		for reason := range issueReasons {
			issueReasonsList = append(issueReasonsList, reason)
		}
		sort.Strings(issueReasonsList)

		// Build bucket status array
		bucketMap := make(map[string]bucketStats)
		for _, b := range buckets {
			key := b.bucket.UTC().Format(time.RFC3339)
			bucketMap[key] = b
		}

		var hourStatuses []LinkHourStatus
		for i := bucketCount - 1; i >= 0; i-- {
			bucketStart := now.Truncate(bucketDuration).Add(-time.Duration(i) * bucketDuration)
			key := bucketStart.UTC().Format(time.RFC3339)

			// Check historical status for this bucket
			histKey := linkBucketKey{linkPK: pk, bucket: key}
			historicalStatus, hasHistory := linkStatusHistory[histKey]
			wasDrained := hasHistory && (historicalStatus == "soft-drained" || historicalStatus == "hard-drained")

			// If link was drained at this time (confirmed by history), show as disabled
			if wasDrained {
				hourStatuses = append(hourStatuses, LinkHourStatus{
					Hour:   key,
					Status: "disabled",
				})
				continue
			}

			// Check if we have latency/traffic data for this bucket
			if stats, ok := bucketMap[key]; ok {
				// Only consider latency for inter-metro WAN links
				committedRtt := meta.committedRttUs
				if meta.linkType != "WAN" || meta.sideAMetro == meta.sideZMetro {
					committedRtt = 0
				}
				status := classifyLinkStatus(stats.avgLatency, stats.lossPct, committedRtt)
				hourStatus := LinkHourStatus{
					Hour:         key,
					Status:       status,
					AvgLatencyUs: stats.avgLatency,
					AvgLossPct:   stats.lossPct,
					Samples:      stats.samples,
				}
				// Add per-side latency/loss metrics if available
				if stats.sideA != nil {
					hourStatus.SideALatencyUs = stats.sideA.avgLatency
					hourStatus.SideALossPct = stats.sideA.lossPct
					hourStatus.SideASamples = stats.sideA.samples
				}
				if stats.sideZ != nil {
					hourStatus.SideZLatencyUs = stats.sideZ.avgLatency
					hourStatus.SideZLossPct = stats.sideZ.lossPct
					hourStatus.SideZSamples = stats.sideZ.samples
				}
				// Add per-side interface issues if available
				intfKey := linkInterfaceBucketKey{linkPK: pk, bucket: key}
				hasErrors := false
				hasDiscards := false
				hasCarrier := false
				if intfBucket, ok := linkInterfaceBuckets[intfKey]; ok {
					if sideA, ok := intfBucket["A"]; ok {
						hourStatus.SideAInErrors = sideA.inErrors
						hourStatus.SideAOutErrors = sideA.outErrors
						hourStatus.SideAInDiscards = sideA.inDiscards
						hourStatus.SideAOutDiscards = sideA.outDiscards
						hourStatus.SideACarrierTransitions = sideA.carrierTransitions
						// Track issue reasons
						if sideA.inErrors > 0 || sideA.outErrors > 0 {
							issueReasons["interface_errors"] = true
							hasErrors = true
						}
						if sideA.inDiscards > 0 || sideA.outDiscards > 0 {
							issueReasons["discards"] = true
							hasDiscards = true
						}
						if sideA.carrierTransitions > 0 {
							issueReasons["carrier_transitions"] = true
							hasCarrier = true
						}
					}
					if sideZ, ok := intfBucket["Z"]; ok {
						hourStatus.SideZInErrors = sideZ.inErrors
						hourStatus.SideZOutErrors = sideZ.outErrors
						hourStatus.SideZInDiscards = sideZ.inDiscards
						hourStatus.SideZOutDiscards = sideZ.outDiscards
						hourStatus.SideZCarrierTransitions = sideZ.carrierTransitions
						// Track issue reasons
						if sideZ.inErrors > 0 || sideZ.outErrors > 0 {
							issueReasons["interface_errors"] = true
							hasErrors = true
						}
						if sideZ.inDiscards > 0 || sideZ.outDiscards > 0 {
							issueReasons["discards"] = true
							hasDiscards = true
						}
						if sideZ.carrierTransitions > 0 {
							issueReasons["carrier_transitions"] = true
							hasCarrier = true
						}
					}
				}
				// Upgrade status based on interface issues
				// Carrier transitions (interface up/down) -> unhealthy
				// Errors or discards -> degraded (if currently healthy)
				if hasCarrier && hourStatus.Status == "healthy" {
					hourStatus.Status = "unhealthy"
				} else if (hasErrors || hasDiscards) && hourStatus.Status == "healthy" {
					hourStatus.Status = "degraded"
				}
				hourStatuses = append(hourStatuses, hourStatus)
			} else {
				hourStatuses = append(hourStatuses, LinkHourStatus{
					Hour:   key,
					Status: "no_data",
				})
			}
		}

		// Check if there are any no_data buckets (missing telemetry)
		// Skip the most recent bucket (last in array) since it may still be collecting data
		for idx, h := range hourStatuses {
			if idx == len(hourStatuses)-1 {
				continue // skip most recent bucket
			}
			if h.Status == "no_data" {
				issueReasons["no_data"] = true
				break
			}
		}

		// Post-process: treat consecutive 100% loss for 2+ hours as disabled
		bucketsFor2Hours := 120 / bucketMinutes
		if bucketsFor2Hours < 1 {
			bucketsFor2Hours = 1
		}

		// Find runs of 100% loss and mark as disabled if >= 2 hours
		i := 0
		for i < len(hourStatuses) {
			// Find start of a 100% loss run
			if hourStatuses[i].AvgLossPct >= 99.9 && hourStatuses[i].Status != "disabled" {
				runStart := i
				// Find end of the run
				for i < len(hourStatuses) && hourStatuses[i].AvgLossPct >= 99.9 && hourStatuses[i].Status != "disabled" {
					i++
				}
				runLength := i - runStart
				// If run is >= 2 hours, mark all as disabled
				if runLength >= bucketsFor2Hours {
					for j := runStart; j < i; j++ {
						hourStatuses[j].Status = "disabled"
					}
					issueReasons["extended_loss"] = true
				}
			} else {
				i++
			}
		}

		// If all packet loss buckets are now disabled, remove packet_loss from reasons
		// (extended_loss is a more accurate classification for extended outages)
		if issueReasons["extended_loss"] && issueReasons["packet_loss"] {
			hasNonDisabledLoss := false
			for _, h := range hourStatuses {
				if h.AvgLossPct >= LossWarningPct && h.Status != "disabled" {
					hasNonDisabledLoss = true
					break
				}
			}
			if !hasNonDisabledLoss {
				delete(issueReasons, "packet_loss")
			}
		}

		// Update issue reasons list after potential disabled additions
		issueReasonsList = nil
		for reason := range issueReasons {
			issueReasonsList = append(issueReasonsList, reason)
		}
		sort.Strings(issueReasonsList)

		links = append(links, LinkHistory{
			PK:             pk,
			Code:           meta.code,
			LinkType:       meta.linkType,
			Contributor:    meta.contributor,
			SideAMetro:     meta.sideAMetro,
			SideZMetro:     meta.sideZMetro,
			SideADevice:    meta.sideADevice,
			SideZDevice:    meta.sideZDevice,
			BandwidthBps:   meta.bandwidthBps,
			CommittedRttUs: meta.committedRttUs,
			Hours:          hourStatuses,
			IssueReasons:   issueReasonsList,
		})
	}

	// Sort links by code for consistent ordering
	sort.Slice(links, func(i, j int) bool {
		return links[i].Code < links[j].Code
	})

	resp := &LinkHistoryResponse{
		Links:         links,
		TimeRange:     timeRange,
		BucketMinutes: bucketMinutes,
		BucketCount:   bucketCount,
	}

	log.Printf("fetchLinkHistoryData completed in %v (range=%s, buckets=%d, links=%d)",
		time.Since(start), timeRange, bucketCount, len(links))

	return resp, nil
}

func classifyLinkStatus(avgLatency, lossPct, committedRttUs float64) string {
	// Calculate latency overage percentage vs committed RTT
	var latencyOveragePct float64
	if committedRttUs > 0 && avgLatency > 0 {
		latencyOveragePct = ((avgLatency - committedRttUs) / committedRttUs) * 100
	}

	// Classify based on thresholds
	if lossPct >= LossCriticalPct || latencyOveragePct >= LatencyCriticalPct {
		return "unhealthy"
	}
	if lossPct >= LossWarningPct || latencyOveragePct >= LatencyWarningPct {
		return "degraded"
	}
	return "healthy"
}

func determineOverallStatus(resp *StatusResponse) string {
	// Check critical issues
	if !resp.System.Database {
		return "unhealthy"
	}

	// Check link health
	if resp.Links.Total > 0 {
		unhealthyPct := float64(resp.Links.Unhealthy) / float64(resp.Links.Total) * 100
		degradedPct := float64(resp.Links.Degraded) / float64(resp.Links.Total) * 100

		if unhealthyPct > 10 {
			return "unhealthy"
		}
		if degradedPct > 20 || unhealthyPct > 0 {
			return "degraded"
		}
	}

	// Check performance
	if resp.Performance.AvgLossPercent >= LossCriticalPct {
		return "unhealthy"
	}
	if resp.Performance.AvgLossPercent >= LossWarningPct {
		return "degraded"
	}

	return "healthy"
}

// Device history types for status timeline
type DeviceHourStatus struct {
	Hour               string  `json:"hour"`
	Status             string  `json:"status"` // "healthy", "degraded", "unhealthy", "no_data", "disabled"
	CurrentUsers       int32   `json:"current_users"`
	MaxUsers           int32   `json:"max_users"`
	UtilizationPct     float64 `json:"utilization_pct"`
	InErrors           uint64  `json:"in_errors"`
	OutErrors          uint64  `json:"out_errors"`
	InDiscards         uint64  `json:"in_discards"`
	OutDiscards        uint64  `json:"out_discards"`
	CarrierTransitions uint64  `json:"carrier_transitions"`
}

type DeviceHistory struct {
	PK           string             `json:"pk"`
	Code         string             `json:"code"`
	DeviceType   string             `json:"device_type"`
	Contributor  string             `json:"contributor"`
	Metro        string             `json:"metro"`
	MaxUsers     int32              `json:"max_users"`
	Hours        []DeviceHourStatus `json:"hours"`
	IssueReasons []string           `json:"issue_reasons"` // "interface_errors", "discards", "carrier_transitions", "drained"
}

type DeviceHistoryResponse struct {
	Devices       []DeviceHistory `json:"devices"`
	TimeRange     string          `json:"time_range"`
	BucketMinutes int             `json:"bucket_minutes"`
	BucketCount   int             `json:"bucket_count"`
}

func GetDeviceHistory(w http.ResponseWriter, r *http.Request) {
	// Parse time range parameter
	timeRange := r.URL.Query().Get("range")
	if timeRange == "" {
		timeRange = "24h"
	}

	// Parse optional bucket count (for responsive display)
	requestedBuckets := 72 // default
	if b := r.URL.Query().Get("buckets"); b != "" {
		if n, err := strconv.Atoi(b); err == nil && n >= 12 && n <= 168 {
			requestedBuckets = n
		}
	}

	// Try to serve from cache first
	if statusCache != nil {
		if cached := statusCache.GetDeviceHistory(timeRange, requestedBuckets); cached != nil {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Cache", "HIT")
			if err := json.NewEncoder(w).Encode(cached); err != nil {
				log.Printf("JSON encoding error: %v", err)
			}
			return
		}
	}

	// Cache miss - fetch fresh data
	w.Header().Set("X-Cache", "MISS")
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	resp, err := fetchDeviceHistoryData(ctx, timeRange, requestedBuckets)
	if err != nil {
		log.Printf("fetchDeviceHistoryData error: %v", err)
		http.Error(w, "Failed to fetch device history", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("JSON encoding error: %v", err)
	}
}

// fetchDeviceHistoryData performs the actual device history data fetch from the database.
func fetchDeviceHistoryData(ctx context.Context, timeRange string, requestedBuckets int) (*DeviceHistoryResponse, error) {
	start := time.Now()

	// Configure bucket size based on time range and requested bucket count
	var totalMinutes int
	switch timeRange {
	case "1h":
		totalMinutes = 60
	case "3h":
		totalMinutes = 3 * 60
	case "6h":
		totalMinutes = 6 * 60
	case "12h":
		totalMinutes = 12 * 60
	case "3d":
		totalMinutes = 3 * 24 * 60
	case "7d":
		totalMinutes = 7 * 24 * 60
	default: // "24h"
		timeRange = "24h"
		totalMinutes = 24 * 60
	}

	// Calculate bucket size to fit requested number of buckets
	bucketMinutes := totalMinutes / requestedBuckets
	if bucketMinutes < 5 {
		bucketMinutes = 5 // minimum 5 minutes
	}
	bucketCount := totalMinutes / bucketMinutes
	totalHours := totalMinutes / 60

	// Build the bucket interval expression
	var bucketInterval string
	if bucketMinutes >= 60 && bucketMinutes%60 == 0 {
		bucketInterval = fmt.Sprintf("toStartOfInterval(event_ts, INTERVAL %d HOUR)", bucketMinutes/60)
	} else {
		bucketInterval = fmt.Sprintf("toStartOfInterval(event_ts, INTERVAL %d MINUTE)", bucketMinutes)
	}

	// Get all devices with their metadata
	deviceQuery := `
		SELECT
			d.pk,
			d.code,
			d.device_type,
			COALESCE(c.code, '') as contributor,
			m.code as metro,
			d.max_users,
			d.status
		FROM dz_devices_current d
		LEFT JOIN dz_contributors_current c ON d.contributor_pk = c.pk
		LEFT JOIN dz_metros_current m ON d.metro_pk = m.pk
		WHERE d.status IN ('activated', 'soft-drained', 'hard-drained', 'suspended')
	`

	deviceRows, err := config.DB.Query(ctx, deviceQuery)
	if err != nil {
		return nil, fmt.Errorf("device history query error: %w", err)
	}
	defer deviceRows.Close()

	// Build map of device metadata
	type deviceMeta struct {
		code        string
		deviceType  string
		contributor string
		metro       string
		maxUsers    int32
		status      string
	}
	deviceMap := make(map[string]deviceMeta)

	for deviceRows.Next() {
		var pk string
		var meta deviceMeta
		if err := deviceRows.Scan(&pk, &meta.code, &meta.deviceType, &meta.contributor, &meta.metro, &meta.maxUsers, &meta.status); err != nil {
			return nil, fmt.Errorf("device scan error: %w", err)
		}
		deviceMap[pk] = meta
	}
	if err := deviceRows.Err(); err != nil {
		return nil, fmt.Errorf("device rows iteration error: %w", err)
	}

	// Get interface issues per bucket
	// Use greatest(0, delta) to ignore negative deltas (counter resets from device restarts)
	interfaceQuery := `
		SELECT
			device_pk,
			` + bucketInterval + ` as bucket,
			toUInt64(SUM(greatest(0, in_errors_delta))) as in_errors,
			toUInt64(SUM(greatest(0, out_errors_delta))) as out_errors,
			toUInt64(SUM(greatest(0, in_discards_delta))) as in_discards,
			toUInt64(SUM(greatest(0, out_discards_delta))) as out_discards,
			toUInt64(SUM(greatest(0, carrier_transitions_delta))) as carrier_transitions
		FROM fact_dz_device_interface_counters
		WHERE event_ts > now() - INTERVAL ? HOUR
		GROUP BY device_pk, bucket
		ORDER BY device_pk, bucket
	`

	interfaceRows, err := config.DB.Query(ctx, interfaceQuery, totalHours)
	if err != nil {
		return nil, fmt.Errorf("device interface query error: %w", err)
	}
	defer interfaceRows.Close()

	// Build bucket stats per device
	type bucketStats struct {
		bucket             time.Time
		inErrors           uint64
		outErrors          uint64
		inDiscards         uint64
		outDiscards        uint64
		carrierTransitions uint64
	}
	deviceBuckets := make(map[string][]bucketStats)

	for interfaceRows.Next() {
		var devicePK string
		var stats bucketStats
		if err := interfaceRows.Scan(&devicePK, &stats.bucket, &stats.inErrors, &stats.outErrors, &stats.inDiscards, &stats.outDiscards, &stats.carrierTransitions); err != nil {
			return nil, fmt.Errorf("interface scan error: %w", err)
		}
		deviceBuckets[devicePK] = append(deviceBuckets[devicePK], stats)
	}
	if err := interfaceRows.Err(); err != nil {
		return nil, fmt.Errorf("interface rows iteration error: %w", err)
	}

	// Get historical device status per bucket
	var historyBucketInterval string
	if bucketMinutes >= 60 && bucketMinutes%60 == 0 {
		historyBucketInterval = fmt.Sprintf("toStartOfInterval(snapshot_ts, INTERVAL %d HOUR)", bucketMinutes/60)
	} else {
		historyBucketInterval = fmt.Sprintf("toStartOfInterval(snapshot_ts, INTERVAL %d MINUTE)", bucketMinutes)
	}

	statusHistoryQuery := `
		SELECT
			pk as device_pk,
			` + historyBucketInterval + ` as bucket,
			argMax(status, snapshot_ts) as status
		FROM dim_dz_devices_history
		WHERE snapshot_ts > now() - INTERVAL ? HOUR
		GROUP BY device_pk, bucket
		ORDER BY device_pk, bucket
	`

	statusRows, err := config.DB.Query(ctx, statusHistoryQuery, totalHours)
	if err != nil {
		log.Printf("Device status history query error: %v", err)
		// Non-fatal - continue without historical status
	}

	// Build map of device status per bucket
	type deviceBucketKey struct {
		devicePK string
		bucket   string
	}
	deviceStatusHistory := make(map[deviceBucketKey]string)

	if statusRows != nil {
		defer statusRows.Close()
		for statusRows.Next() {
			var devicePK, status string
			var bucket time.Time
			if err := statusRows.Scan(&devicePK, &bucket, &status); err != nil {
				return nil, fmt.Errorf("device status history scan error: %w", err)
			}
			key := deviceBucketKey{devicePK: devicePK, bucket: bucket.UTC().Format(time.RFC3339)}
			deviceStatusHistory[key] = status
		}
		if err := statusRows.Err(); err != nil {
			return nil, fmt.Errorf("device status rows iteration error: %w", err)
		}
	}

	duration := time.Since(start)
	metrics.RecordClickHouseQuery(duration, err)

	// Build response with all buckets for each device
	now := time.Now().UTC()
	bucketDuration := time.Duration(bucketMinutes) * time.Minute
	var devices []DeviceHistory

	for pk, meta := range deviceMap {
		// Check if device is currently drained
		isCurrentlyDrained := meta.status == "soft-drained" || meta.status == "hard-drained" || meta.status == "suspended"

		// Track issue reasons for this device
		issueReasons := make(map[string]bool)

		if isCurrentlyDrained {
			issueReasons["drained"] = true
		}

		// Get interface stats for this device
		buckets := deviceBuckets[pk]

		// Check for interface issues
		for _, b := range buckets {
			totalErrors := b.inErrors + b.outErrors
			totalDiscards := b.inDiscards + b.outDiscards
			if totalErrors > 0 {
				issueReasons["interface_errors"] = true
			}
			if totalDiscards > 0 {
				issueReasons["discards"] = true
			}
			if b.carrierTransitions > 0 {
				issueReasons["carrier_transitions"] = true
			}
		}

		// Also check if device was drained at any point in the history
		for key := range deviceStatusHistory {
			if key.devicePK == pk {
				status := deviceStatusHistory[key]
				if status == "soft-drained" || status == "hard-drained" || status == "suspended" {
					issueReasons["drained"] = true
					break
				}
			}
		}

		// Convert issue reasons to slice
		var issueReasonsList []string
		for reason := range issueReasons {
			issueReasonsList = append(issueReasonsList, reason)
		}
		sort.Strings(issueReasonsList)

		// Build bucket status array
		bucketMap := make(map[string]bucketStats)
		for _, b := range buckets {
			key := b.bucket.UTC().Format(time.RFC3339)
			bucketMap[key] = b
		}

		var hourStatuses []DeviceHourStatus
		for i := bucketCount - 1; i >= 0; i-- {
			bucketStart := now.Truncate(bucketDuration).Add(-time.Duration(i) * bucketDuration)
			key := bucketStart.UTC().Format(time.RFC3339)

			// Check historical status for this bucket
			histKey := deviceBucketKey{devicePK: pk, bucket: key}
			historicalStatus, hasHistory := deviceStatusHistory[histKey]
			wasDrained := hasHistory && (historicalStatus == "soft-drained" || historicalStatus == "hard-drained" || historicalStatus == "suspended")

			// If device was drained at this time, show as disabled
			if wasDrained {
				hourStatuses = append(hourStatuses, DeviceHourStatus{
					Hour:   key,
					Status: "disabled",
				})
				continue
			}

			// Check if we have interface data for this bucket
			if stats, ok := bucketMap[key]; ok {
				status := classifyDeviceStatus(stats.inErrors+stats.outErrors, stats.inDiscards+stats.outDiscards, stats.carrierTransitions)
				hourStatuses = append(hourStatuses, DeviceHourStatus{
					Hour:               key,
					Status:             status,
					MaxUsers:           meta.maxUsers,
					InErrors:           stats.inErrors,
					OutErrors:          stats.outErrors,
					InDiscards:         stats.inDiscards,
					OutDiscards:        stats.outDiscards,
					CarrierTransitions: stats.carrierTransitions,
				})
			} else {
				// No interface data - show as healthy (no errors)
				hourStatuses = append(hourStatuses, DeviceHourStatus{
					Hour:     key,
					Status:   "healthy",
					MaxUsers: meta.maxUsers,
				})
			}
		}

		devices = append(devices, DeviceHistory{
			PK:           pk,
			Code:         meta.code,
			DeviceType:   meta.deviceType,
			Contributor:  meta.contributor,
			Metro:        meta.metro,
			MaxUsers:     meta.maxUsers,
			Hours:        hourStatuses,
			IssueReasons: issueReasonsList,
		})
	}

	// Sort devices by code for consistent ordering
	sort.Slice(devices, func(i, j int) bool {
		return devices[i].Code < devices[j].Code
	})

	resp := &DeviceHistoryResponse{
		Devices:       devices,
		TimeRange:     timeRange,
		BucketMinutes: bucketMinutes,
		BucketCount:   bucketCount,
	}

	log.Printf("fetchDeviceHistoryData completed in %v (range=%s, buckets=%d, devices=%d)",
		time.Since(start), timeRange, bucketCount, len(devices))

	return resp, nil
}

func classifyDeviceStatus(totalErrors, totalDiscards uint64, carrierTransitions uint64) string {
	// Thresholds for device health (per bucket)
	// Unhealthy: >= 100 of any metric
	// Degraded: > 0 and < 100 of any metric
	const UnhealthyThreshold = 100

	if totalErrors >= UnhealthyThreshold || totalDiscards >= UnhealthyThreshold || carrierTransitions >= UnhealthyThreshold {
		return "unhealthy"
	}
	if totalErrors > 0 || totalDiscards > 0 || carrierTransitions > 0 {
		return "degraded"
	}
	return "healthy"
}

// InterfaceIssuesResponse is the response for interface issues endpoint
type InterfaceIssuesResponse struct {
	Issues    []InterfaceIssue `json:"issues"`
	TimeRange string           `json:"time_range"`
}

// GetInterfaceIssues returns interface issues for a given time range
func GetInterfaceIssues(w http.ResponseWriter, r *http.Request) {
	timeRange := r.URL.Query().Get("range")
	if timeRange == "" {
		timeRange = "24h"
	}

	// Convert time range to duration
	var duration time.Duration
	switch timeRange {
	case "3h":
		duration = 3 * time.Hour
	case "6h":
		duration = 6 * time.Hour
	case "12h":
		duration = 12 * time.Hour
	case "24h":
		duration = 24 * time.Hour
	case "3d":
		duration = 3 * 24 * time.Hour
	case "7d":
		duration = 7 * 24 * time.Hour
	default:
		duration = 24 * time.Hour
		timeRange = "24h"
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	issues, err := fetchInterfaceIssuesData(ctx, duration)
	if err != nil {
		log.Printf("Error fetching interface issues: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	resp := &InterfaceIssuesResponse{
		Issues:    issues,
		TimeRange: timeRange,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func fetchInterfaceIssuesData(ctx context.Context, duration time.Duration) ([]InterfaceIssue, error) {
	// Convert duration to hours for the SQL interval
	hours := int(duration.Hours())

	query := fmt.Sprintf(`
		SELECT
			d.pk as device_pk,
			d.code as device_code,
			d.device_type,
			COALESCE(contrib.code, '') as contributor,
			m.code as metro,
			c.intf as interface_name,
			COALESCE(l.pk, '') as link_pk,
			COALESCE(l.code, '') as link_code,
			COALESCE(l.link_type, '') as link_type,
			COALESCE(c.link_side, '') as link_side,
			toUInt64(SUM(c.in_errors_delta)) as in_errors,
			toUInt64(SUM(c.out_errors_delta)) as out_errors,
			toUInt64(SUM(c.in_discards_delta)) as in_discards,
			toUInt64(SUM(c.out_discards_delta)) as out_discards,
			toUInt64(SUM(c.carrier_transitions_delta)) as carrier_transitions,
			formatDateTime(min(c.event_ts), '%%Y-%%m-%%dT%%H:%%i:%%sZ', 'UTC') as first_seen,
			formatDateTime(max(c.event_ts), '%%Y-%%m-%%dT%%H:%%i:%%sZ', 'UTC') as last_seen
		FROM fact_dz_device_interface_counters c
		JOIN dz_devices_current d ON c.device_pk = d.pk
		JOIN dz_metros_current m ON d.metro_pk = m.pk
		LEFT JOIN dz_contributors_current contrib ON d.contributor_pk = contrib.pk
		LEFT JOIN dz_links_current l ON c.link_pk = l.pk
		WHERE c.event_ts > now() - INTERVAL %d HOUR
		  AND d.status = 'activated'
		  AND (c.in_errors_delta > 0 OR c.out_errors_delta > 0 OR c.in_discards_delta > 0 OR c.out_discards_delta > 0 OR c.carrier_transitions_delta > 0)
		GROUP BY d.pk, d.code, d.device_type, contrib.code, m.code, c.intf, l.pk, l.code, l.link_type, c.link_side
		ORDER BY (in_errors + out_errors + in_discards + out_discards + carrier_transitions) DESC
		LIMIT 50
	`, hours)

	rows, err := config.DB.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var issues []InterfaceIssue
	for rows.Next() {
		var issue InterfaceIssue
		if err := rows.Scan(
			&issue.DevicePK,
			&issue.DeviceCode,
			&issue.DeviceType,
			&issue.Contributor,
			&issue.Metro,
			&issue.InterfaceName,
			&issue.LinkPK,
			&issue.LinkCode,
			&issue.LinkType,
			&issue.LinkSide,
			&issue.InErrors,
			&issue.OutErrors,
			&issue.InDiscards,
			&issue.OutDiscards,
			&issue.CarrierTransitions,
			&issue.FirstSeen,
			&issue.LastSeen,
		); err != nil {
			return nil, err
		}
		issues = append(issues, issue)
	}

	return issues, rows.Err()
}

// DeviceInterfaceHistoryResponse is the response for device interface history endpoint
type DeviceInterfaceHistoryResponse struct {
	Interfaces    []InterfaceHistory `json:"interfaces"`
	TimeRange     string             `json:"time_range"`
	BucketMinutes int                `json:"bucket_minutes"`
	BucketCount   int                `json:"bucket_count"`
}

// InterfaceHistory is the history of a single interface
type InterfaceHistory struct {
	InterfaceName string                   `json:"interface_name"`
	LinkPK        string                   `json:"link_pk,omitempty"`
	LinkCode      string                   `json:"link_code,omitempty"`
	LinkType      string                   `json:"link_type,omitempty"`
	LinkSide      string                   `json:"link_side,omitempty"`
	Hours         []InterfaceHourStatus    `json:"hours"`
}

// InterfaceHourStatus is the status of an interface for a single time bucket
type InterfaceHourStatus struct {
	Hour               string `json:"hour"`
	InErrors           uint64 `json:"in_errors"`
	OutErrors          uint64 `json:"out_errors"`
	InDiscards         uint64 `json:"in_discards"`
	OutDiscards        uint64 `json:"out_discards"`
	CarrierTransitions uint64 `json:"carrier_transitions"`
}

// GetDeviceInterfaceHistory returns interface-level history for a specific device
func GetDeviceInterfaceHistory(w http.ResponseWriter, r *http.Request) {
	devicePK := chi.URLParam(r, "pk")
	if devicePK == "" {
		http.Error(w, "Device PK is required", http.StatusBadRequest)
		return
	}

	timeRange := r.URL.Query().Get("range")
	if timeRange == "" {
		timeRange = "24h"
	}

	bucketsStr := r.URL.Query().Get("buckets")
	requestedBuckets := 72 // default
	if bucketsStr != "" {
		if b, err := strconv.Atoi(bucketsStr); err == nil && b > 0 && b <= 168 {
			requestedBuckets = b
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	resp, err := fetchDeviceInterfaceHistoryData(ctx, devicePK, timeRange, requestedBuckets)
	if err != nil {
		log.Printf("Error fetching device interface history: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func fetchDeviceInterfaceHistoryData(ctx context.Context, devicePK string, timeRange string, requestedBuckets int) (*DeviceInterfaceHistoryResponse, error) {
	// Calculate bucket size based on time range and requested buckets
	var totalHours int
	switch timeRange {
	case "3h":
		totalHours = 3
	case "6h":
		totalHours = 6
	case "12h":
		totalHours = 12
	case "24h":
		totalHours = 24
	case "3d":
		totalHours = 72
	case "7d":
		totalHours = 168
	default:
		totalHours = 24
		timeRange = "24h"
	}

	// Calculate bucket size in minutes
	totalMinutes := totalHours * 60
	bucketMinutes := totalMinutes / requestedBuckets
	if bucketMinutes < 1 {
		bucketMinutes = 1
	}
	bucketCount := totalMinutes / bucketMinutes

	// Build interval expression for ClickHouse
	bucketInterval := fmt.Sprintf("toStartOfInterval(event_ts, INTERVAL %d MINUTE, 'UTC')", bucketMinutes)

	// Query interface stats per bucket for this device
	query := `
		SELECT
			c.intf as interface_name,
			COALESCE(l.pk, '') as link_pk,
			COALESCE(l.code, '') as link_code,
			COALESCE(l.link_type, '') as link_type,
			COALESCE(c.link_side, '') as link_side,
			` + bucketInterval + ` as bucket,
			toUInt64(SUM(greatest(0, c.in_errors_delta))) as in_errors,
			toUInt64(SUM(greatest(0, c.out_errors_delta))) as out_errors,
			toUInt64(SUM(greatest(0, c.in_discards_delta))) as in_discards,
			toUInt64(SUM(greatest(0, c.out_discards_delta))) as out_discards,
			toUInt64(SUM(greatest(0, c.carrier_transitions_delta))) as carrier_transitions
		FROM fact_dz_device_interface_counters c
		LEFT JOIN dz_links_current l ON c.link_pk = l.pk
		WHERE c.device_pk = ?
		  AND c.event_ts > now() - INTERVAL ? HOUR
		GROUP BY c.intf, l.pk, l.code, l.link_type, c.link_side, bucket
		ORDER BY c.intf, bucket
	`

	rows, err := config.DB.Query(ctx, query, devicePK, totalHours)
	if err != nil {
		return nil, fmt.Errorf("interface history query error: %w", err)
	}
	defer rows.Close()

	// Build interface history map
	type interfaceMeta struct {
		linkPK   string
		linkCode string
		linkType string
		linkSide string
	}
	type bucketStats struct {
		bucket             time.Time
		inErrors           uint64
		outErrors          uint64
		inDiscards         uint64
		outDiscards        uint64
		carrierTransitions uint64
	}

	interfaceMetaMap := make(map[string]interfaceMeta)
	interfaceBuckets := make(map[string][]bucketStats)

	for rows.Next() {
		var intfName string
		var meta interfaceMeta
		var stats bucketStats
		if err := rows.Scan(
			&intfName,
			&meta.linkPK,
			&meta.linkCode,
			&meta.linkType,
			&meta.linkSide,
			&stats.bucket,
			&stats.inErrors,
			&stats.outErrors,
			&stats.inDiscards,
			&stats.outDiscards,
			&stats.carrierTransitions,
		); err != nil {
			return nil, fmt.Errorf("interface history scan error: %w", err)
		}
		interfaceMetaMap[intfName] = meta
		interfaceBuckets[intfName] = append(interfaceBuckets[intfName], stats)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("interface history rows iteration error: %w", err)
	}

	// Build response with all buckets for each interface
	now := time.Now().UTC()
	bucketDuration := time.Duration(bucketMinutes) * time.Minute
	var interfaces []InterfaceHistory

	for intfName, meta := range interfaceMetaMap {
		buckets := interfaceBuckets[intfName]
		bucketMap := make(map[string]bucketStats)
		for _, b := range buckets {
			key := b.bucket.UTC().Format(time.RFC3339)
			bucketMap[key] = b
		}

		var hourStatuses []InterfaceHourStatus
		for i := bucketCount - 1; i >= 0; i-- {
			bucketStart := now.Truncate(bucketDuration).Add(-time.Duration(i) * bucketDuration)
			key := bucketStart.UTC().Format(time.RFC3339)

			if stats, ok := bucketMap[key]; ok {
				hourStatuses = append(hourStatuses, InterfaceHourStatus{
					Hour:               key,
					InErrors:           stats.inErrors,
					OutErrors:          stats.outErrors,
					InDiscards:         stats.inDiscards,
					OutDiscards:        stats.outDiscards,
					CarrierTransitions: stats.carrierTransitions,
				})
			} else {
				hourStatuses = append(hourStatuses, InterfaceHourStatus{
					Hour: key,
				})
			}
		}

		interfaces = append(interfaces, InterfaceHistory{
			InterfaceName: intfName,
			LinkPK:        meta.linkPK,
			LinkCode:      meta.linkCode,
			LinkType:      meta.linkType,
			LinkSide:      meta.linkSide,
			Hours:         hourStatuses,
		})
	}

	// Sort interfaces by name
	sort.Slice(interfaces, func(i, j int) bool {
		return interfaces[i].InterfaceName < interfaces[j].InterfaceName
	})

	return &DeviceInterfaceHistoryResponse{
		Interfaces:    interfaces,
		TimeRange:     timeRange,
		BucketMinutes: bucketMinutes,
		BucketCount:   bucketCount,
	}, nil
}
