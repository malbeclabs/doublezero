package handlers

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

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
	WANBandwidthBps  int64   `json:"wan_bandwidth_bps"`
	UserInboundBps   float64 `json:"user_inbound_bps"`

	// Status breakdown
	DevicesByStatus map[string]uint64 `json:"devices_by_status"`
	LinksByStatus   map[string]uint64 `json:"links_by_status"`
}

type LinkHealth struct {
	Total          uint64       `json:"total"`
	Healthy        uint64       `json:"healthy"`
	Degraded       uint64       `json:"degraded"` // High latency or some loss
	Unhealthy      uint64       `json:"unhealthy"` // Significant loss or down
	Issues         []LinkIssue  `json:"issues"`    // Top issues
	HighUtilLinks  []LinkMetric `json:"high_util_links"` // Links with high utilization
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
}

type LinkMetric struct {
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
	DeviceCode         string `json:"device_code"`
	DeviceType         string `json:"device_type"`
	Contributor        string `json:"contributor"`
	Metro              string `json:"metro"`
	InterfaceName      string `json:"interface_name"`
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
	Code       string `json:"code"`
	DeviceType string `json:"device_type"`
	Metro      string `json:"metro"`
	Status     string `json:"status"`
	Since      string `json:"since"` // ISO timestamp when entered this status
}

type NonActivatedLink struct {
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

// Thresholds for health classification
const (
	LatencyWarningPct  = 20.0  // 20% over committed RTT
	LatencyCriticalPct = 50.0  // 50% over committed RTT
	LossWarningPct     = 0.1   // 0.1%
	LossCriticalPct    = 1.0   // 1%
	UtilWarningPct     = 70.0  // 70%
	UtilCriticalPct    = 90.0  // 90%
)

func GetStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	start := time.Now()

	resp := StatusResponse{
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

	g.Go(func() error {
		query := `
			SELECT COALESCE(SUM(l.bandwidth_bps), 0)
			FROM dz_links_current l
			JOIN dz_devices_current da ON l.side_a_pk = da.pk
			JOIN dz_devices_current dz ON l.side_z_pk = dz.pk
			WHERE l.status = 'activated'
			  AND l.link_type = 'WAN'
			  AND da.metro_pk != dz.metro_pk
		`
		row := config.DB.QueryRow(ctx, query)
		return row.Scan(&resp.Network.WANBandwidthBps)
	})

	g.Go(func() error {
		query := `
			SELECT COALESCE(SUM(in_octets_delta) * 8.0 / NULLIF(SUM(delta_duration), 0), 0)
			FROM fact_dz_device_interface_counters
			WHERE event_ts > now() - INTERVAL 1 HOUR
			  AND user_tunnel_id IS NOT NULL
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
				l.code,
				l.link_type,
				COALESCE(c.code, '') as contributor,
				l.bandwidth_bps,
				l.committed_rtt_ns / 1000.0 as committed_rtt_us,
				ma.code as side_a_metro,
				mz.code as side_z_metro,
				COALESCE(lat.avg_rtt_us, 0) as latency_us,
				COALESCE(lat.loss_percent, 0) as loss_percent,
				COALESCE(traffic.in_bps, 0) as in_bps,
				COALESCE(traffic.out_bps, 0) as out_bps
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
				WHERE event_ts > now() - INTERVAL 3 HOUR
				GROUP BY link_pk
			) lat ON l.pk = lat.link_pk
			LEFT JOIN (
				SELECT link_pk,
					CASE WHEN SUM(delta_duration) > 0 THEN SUM(in_octets_delta) * 8 / SUM(delta_duration) ELSE 0 END as in_bps,
					CASE WHEN SUM(delta_duration) > 0 THEN SUM(out_octets_delta) * 8 / SUM(delta_duration) ELSE 0 END as out_bps
				FROM fact_dz_device_interface_counters
				WHERE event_ts > now() - INTERVAL 5 MINUTE
					AND link_pk != ''
				GROUP BY link_pk
			) traffic ON l.pk = traffic.link_pk
			WHERE l.status = 'activated'
			  AND l.link_type = 'WAN'
		`
		rows, err := config.DB.Query(ctx, query)
		if err != nil {
			return err
		}
		defer rows.Close()

		var healthy, degraded, unhealthy uint64
		var issues []LinkIssue
		var highUtil []LinkMetric

		for rows.Next() {
			var code, linkType, contributor, sideAMetro, sideZMetro string
			var bandwidthBps int64
			var committedRttUs, latencyUs, lossPct, inBps, outBps float64

			if err := rows.Scan(&code, &linkType, &contributor, &bandwidthBps, &committedRttUs, &sideAMetro, &sideZMetro, &latencyUs, &lossPct, &inBps, &outBps); err != nil {
				return err
			}

			// Calculate latency overage percentage vs committed RTT
			var latencyOveragePct float64
			if committedRttUs > 0 && latencyUs > 0 {
				latencyOveragePct = ((latencyUs - committedRttUs) / committedRttUs) * 100
			}

			// Classify link health based on committed RTT comparison
			isUnhealthy := lossPct >= LossCriticalPct || latencyOveragePct >= LatencyCriticalPct
			isDegraded := lossPct >= LossWarningPct || latencyOveragePct >= LatencyWarningPct

			if isUnhealthy {
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
			if latencyOveragePct >= LatencyWarningPct && len(issues) < 10 {
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

			// Track high utilization links
			if bandwidthBps > 0 {
				utilIn := (inBps / float64(bandwidthBps)) * 100
				utilOut := (outBps / float64(bandwidthBps)) * 100
				if (utilIn >= UtilWarningPct || utilOut >= UtilWarningPct) && len(highUtil) < 10 {
					highUtil = append(highUtil, LinkMetric{
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
					})
				}
			}
		}

		resp.Links.Total = healthy + degraded + unhealthy
		resp.Links.Healthy = healthy
		resp.Links.Degraded = degraded
		resp.Links.Unhealthy = unhealthy
		resp.Links.Issues = issues
		resp.Links.HighUtilLinks = highUtil

		return rows.Err()
	})

	// Performance metrics (WAN links, last 3 hours)
	g.Go(func() error {
		query := `
			SELECT
				avg(rtt_us) as avg_latency,
				quantile(0.95)(rtt_us) as p95_latency,
				toFloat64(min(rtt_us)) as min_latency,
				toFloat64(max(rtt_us)) as max_latency,
				countIf(loss OR rtt_us = 0) * 100.0 / count(*) as avg_loss,
				avg(abs(ipdv_us)) as avg_jitter
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

	// Total throughput
	g.Go(func() error {
		query := `
			SELECT
				COALESCE(SUM(in_octets_delta) * 8.0 / NULLIF(SUM(delta_duration), 0), 0) as total_in_bps,
				COALESCE(SUM(out_octets_delta) * 8.0 / NULLIF(SUM(delta_duration), 0), 0) as total_out_bps
			FROM fact_dz_device_interface_counters
			WHERE event_ts > now() - INTERVAL 5 MINUTE
			  AND link_pk != ''
		`
		row := config.DB.QueryRow(ctx, query)
		return row.Scan(&resp.Performance.TotalInBps, &resp.Performance.TotalOutBps)
	})

	// Interface issues (errors, discards, carrier transitions in last 24 hours)
	g.Go(func() error {
		query := `
			SELECT
				d.code as device_code,
				d.device_type,
				COALESCE(contrib.code, '') as contributor,
				m.code as metro,
				c.intf as interface_name,
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
			WHERE c.event_ts > now() - INTERVAL 24 HOUR
			  AND d.status = 'activated'
			  AND (c.in_errors_delta > 0 OR c.out_errors_delta > 0 OR c.in_discards_delta > 0 OR c.out_discards_delta > 0 OR c.carrier_transitions_delta > 0)
			GROUP BY d.code, d.device_type, contrib.code, m.code, c.intf, l.code, l.link_type, c.link_side
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
				&issue.DeviceCode,
				&issue.DeviceType,
				&issue.Contributor,
				&issue.Metro,
				&issue.InterfaceName,
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
			if err := rows.Scan(&dev.Code, &dev.DeviceType, &dev.Metro, &dev.Status, &dev.Since); err != nil {
				return err
			}
			devices = append(devices, dev)
		}
		resp.Alerts.Devices = devices
		return rows.Err()
	})

	// Non-activated links
	g.Go(func() error {
		query := `
			SELECT
				l.code,
				l.link_type,
				ma.code as side_a_metro,
				mz.code as side_z_metro,
				l.status,
				formatDateTime(l.snapshot_ts, '%Y-%m-%dT%H:%i:%sZ', 'UTC') as since
			FROM dz_links_current l
			JOIN dz_devices_current da ON l.side_a_pk = da.pk
			JOIN dz_devices_current dz ON l.side_z_pk = dz.pk
			JOIN dz_metros_current ma ON da.metro_pk = ma.pk
			JOIN dz_metros_current mz ON dz.metro_pk = mz.pk
			WHERE l.status != 'activated'
			ORDER BY l.snapshot_ts DESC
			LIMIT 50
		`
		rows, err := config.DB.Query(ctx, query)
		if err != nil {
			return err
		}
		defer rows.Close()

		var links []NonActivatedLink
		for rows.Next() {
			var link NonActivatedLink
			if err := rows.Scan(&link.Code, &link.LinkType, &link.SideAMetro, &link.SideZMetro, &link.Status, &link.Since); err != nil {
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
	resp.Status = determineOverallStatus(&resp)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("JSON encoding error: %v", err)
	}
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
