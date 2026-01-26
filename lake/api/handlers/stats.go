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

type StatsResponse struct {
	ValidatorsOnDZ  uint64  `json:"validators_on_dz"`
	TotalStakeSol   float64 `json:"total_stake_sol"`
	StakeSharePct   float64 `json:"stake_share_pct"`
	Users           uint64  `json:"users"`
	Devices         uint64  `json:"devices"`
	Links           uint64  `json:"links"`
	Contributors    uint64  `json:"contributors"`
	Metros          uint64  `json:"metros"`
	BandwidthBps    int64   `json:"bandwidth_bps"`
	UserInboundBps  float64 `json:"user_inbound_bps"`
	FetchedAt       string  `json:"fetched_at"`
	Error           string  `json:"error,omitempty"`
}

func GetStats(w http.ResponseWriter, r *http.Request) {
	// Try to derive stats from the status cache
	if statusCache != nil {
		if cached := statusCache.GetStatus(); cached != nil {
			stats := StatsResponse{
				ValidatorsOnDZ:  cached.Network.ValidatorsOnDZ,
				TotalStakeSol:   cached.Network.TotalStakeSol,
				StakeSharePct:   cached.Network.StakeSharePct,
				Users:           cached.Network.Users,
				Devices:         cached.Network.Devices,
				Links:           cached.Network.Links,
				Contributors:    cached.Network.Contributors,
				Metros:          cached.Network.Metros,
				BandwidthBps:    cached.Network.BandwidthBps,
				UserInboundBps:  cached.Network.UserInboundBps,
				FetchedAt:       cached.Timestamp,
			}
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Cache", "HIT")
			if err := json.NewEncoder(w).Encode(stats); err != nil {
				log.Printf("JSON encoding error: %v", err)
			}
			return
		}
	}

	// Cache miss - fetch fresh data
	w.Header().Set("X-Cache", "MISS")
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	start := time.Now()

	stats := StatsResponse{
		FetchedAt: time.Now().UTC().Format(time.RFC3339),
	}

	g, ctx := errgroup.WithContext(ctx)

	// Count validators on DZ (Solana validators connected via dz_users)
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
		return row.Scan(&stats.ValidatorsOnDZ)
	})

	// Sum total stake for validators on DZ (in lamports, convert to SOL)
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
		return row.Scan(&stats.TotalStakeSol)
	})

	// Calculate stake share percentage (connected stake / total network stake * 100)
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
		return row.Scan(&stats.StakeSharePct)
	})

	// Count users
	g.Go(func() error {
		query := `SELECT COUNT(*) FROM dz_users_current`
		row := config.DB.QueryRow(ctx, query)
		return row.Scan(&stats.Users)
	})

	// Count devices
	g.Go(func() error {
		query := `SELECT COUNT(*) FROM dz_devices_current`
		row := config.DB.QueryRow(ctx, query)
		return row.Scan(&stats.Devices)
	})

	// Count links
	g.Go(func() error {
		query := `SELECT COUNT(*) FROM dz_links_current`
		row := config.DB.QueryRow(ctx, query)
		return row.Scan(&stats.Links)
	})

	// Count contributors
	g.Go(func() error {
		query := `SELECT COUNT(DISTINCT pk) FROM dz_contributors_current`
		row := config.DB.QueryRow(ctx, query)
		return row.Scan(&stats.Contributors)
	})

	// Count metros
	g.Go(func() error {
		query := `SELECT COUNT(DISTINCT pk) FROM dz_metros_current`
		row := config.DB.QueryRow(ctx, query)
		return row.Scan(&stats.Metros)
	})

	// Sum total bandwidth for activated links
	g.Go(func() error {
		query := `
			SELECT COALESCE(SUM(bandwidth_bps), 0)
			FROM dz_links_current
			WHERE status = 'activated'
		`
		row := config.DB.QueryRow(ctx, query)
		return row.Scan(&stats.BandwidthBps)
	})

	// Calculate total user inbound traffic rate (bps) over last hour
	g.Go(func() error {
		query := `
			SELECT COALESCE(SUM(interface_rate), 0) FROM (
				SELECT SUM(in_octets_delta) * 8.0 / NULLIF(SUM(delta_duration), 0) AS interface_rate
				FROM fact_dz_device_interface_counters
				WHERE event_ts > now() - INTERVAL 1 HOUR
				  AND user_tunnel_id IS NOT NULL
				  AND delta_duration > 0
				  AND in_octets_delta >= 0
				GROUP BY device_pk, intf
			)
		`
		row := config.DB.QueryRow(ctx, query)
		return row.Scan(&stats.UserInboundBps)
	})

	err := g.Wait()
	duration := time.Since(start)
	metrics.RecordClickHouseQuery(duration, err)

	if err != nil {
		log.Printf("Stats query error: %v", err)
		stats.Error = err.Error()
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(stats); err != nil {
		log.Printf("JSON encoding error: %v", err)
	}
}
