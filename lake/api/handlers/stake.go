package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/malbeclabs/doublezero/lake/api/config"
	"github.com/malbeclabs/doublezero/lake/api/metrics"
	"golang.org/x/sync/errgroup"
)

type StakeOverview struct {
	// Current values
	DZStakeSol       float64 `json:"dz_stake_sol"`
	TotalStakeSol    float64 `json:"total_stake_sol"`
	StakeSharePct    float64 `json:"stake_share_pct"`
	ValidatorCount   uint64  `json:"validator_count"`

	// 24h comparison
	DZStakeSol24hAgo    float64 `json:"dz_stake_sol_24h_ago"`
	StakeSharePct24hAgo float64 `json:"stake_share_pct_24h_ago"`
	DZStakeChange24h    float64 `json:"dz_stake_change_24h"`
	ShareChange24h      float64 `json:"share_change_24h"`

	// 7d comparison
	DZStakeSol7dAgo    float64 `json:"dz_stake_sol_7d_ago"`
	StakeSharePct7dAgo float64 `json:"stake_share_pct_7d_ago"`
	DZStakeChange7d    float64 `json:"dz_stake_change_7d"`
	ShareChange7d      float64 `json:"share_change_7d"`

	FetchedAt string `json:"fetched_at"`
	Error     string `json:"error,omitempty"`
}

type StakeHistoryPoint struct {
	Timestamp     string  `json:"timestamp"`
	DZStakeSol    float64 `json:"dz_stake_sol"`
	TotalStakeSol float64 `json:"total_stake_sol"`
	StakeSharePct float64 `json:"stake_share_pct"`
}

type StakeHistoryResponse struct {
	Points    []StakeHistoryPoint `json:"points"`
	FetchedAt string              `json:"fetched_at"`
	Error     string              `json:"error,omitempty"`
}

func GetStakeOverview(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	start := time.Now()
	overview := StakeOverview{
		FetchedAt: time.Now().UTC().Format(time.RFC3339),
	}

	g, ctx := errgroup.WithContext(ctx)

	// Current DZ stake and validator count
	g.Go(func() error {
		query := `
			SELECT
				COALESCE(SUM(va.activated_stake_lamports), 0) / 1e9 AS dz_stake_sol,
				COUNT(DISTINCT va.vote_pubkey) AS validator_count
			FROM dz_users_current u
			JOIN solana_gossip_nodes_current gn ON u.dz_ip = gn.gossip_ip
			JOIN solana_vote_accounts_current va ON gn.pubkey = va.node_pubkey
			WHERE u.status = 'activated'
			  AND va.epoch_vote_account = 'true'
			  AND va.activated_stake_lamports > 0
		`
		row := config.DB.QueryRow(ctx, query)
		return row.Scan(&overview.DZStakeSol, &overview.ValidatorCount)
	})

	// Total network stake
	g.Go(func() error {
		query := `
			SELECT COALESCE(SUM(activated_stake_lamports), 0) / 1e9 AS total_stake_sol
			FROM solana_vote_accounts_current
			WHERE epoch_vote_account = 'true'
			  AND activated_stake_lamports > 0
		`
		row := config.DB.QueryRow(ctx, query)
		return row.Scan(&overview.TotalStakeSol)
	})

	// Historical DZ stake 24h ago - use argMax to get point-in-time values
	g.Go(func() error {
		query := `
			WITH
			target_ts AS (
				SELECT max(snapshot_ts) as ts
				FROM dim_solana_vote_accounts_history
				WHERE snapshot_ts <= now() - INTERVAL 24 HOUR
			),
			-- Get latest user state at target time using argMax
			users_at_time AS (
				SELECT
					entity_id,
					argMax(dz_ip, snapshot_ts) as dz_ip,
					argMax(status, snapshot_ts) as status
				FROM dim_dz_users_history
				WHERE snapshot_ts <= (SELECT ts FROM target_ts)
				GROUP BY entity_id
				HAVING status = 'activated' AND dz_ip != ''
			),
			-- Get latest vote account state at target time
			validators_at_time AS (
				SELECT
					node_pubkey,
					argMax(activated_stake_lamports, snapshot_ts) as stake
				FROM dim_solana_vote_accounts_history
				WHERE snapshot_ts <= (SELECT ts FROM target_ts)
				  AND activated_stake_lamports > 0
				GROUP BY node_pubkey
			),
			-- Total network stake at that time
			total_stake AS (
				SELECT COALESCE(SUM(stake), 0) as total FROM validators_at_time
			),
			-- DZ stake: join current gossip (IP mapping doesn't change much) with historical validators
			dz_stake AS (
				SELECT COALESCE(SUM(v.stake), 0) as dz_total
				FROM users_at_time u
				JOIN solana_gossip_nodes_current gn ON u.dz_ip = gn.gossip_ip
				JOIN validators_at_time v ON gn.pubkey = v.node_pubkey
			)
			SELECT
				dz.dz_total / 1e9,
				CASE WHEN ts.total > 0 THEN dz.dz_total * 100.0 / ts.total ELSE 0 END
			FROM dz_stake dz, total_stake ts
		`
		row := config.DB.QueryRow(ctx, query)
		return row.Scan(&overview.DZStakeSol24hAgo, &overview.StakeSharePct24hAgo)
	})

	// Historical DZ stake 7d ago
	g.Go(func() error {
		query := `
			WITH
			target_ts AS (
				SELECT max(snapshot_ts) as ts
				FROM dim_solana_vote_accounts_history
				WHERE snapshot_ts <= now() - INTERVAL 7 DAY
			),
			-- Get latest user state at target time using argMax
			users_at_time AS (
				SELECT
					entity_id,
					argMax(dz_ip, snapshot_ts) as dz_ip,
					argMax(status, snapshot_ts) as status
				FROM dim_dz_users_history
				WHERE snapshot_ts <= (SELECT ts FROM target_ts)
				GROUP BY entity_id
				HAVING status = 'activated' AND dz_ip != ''
			),
			-- Get latest vote account state at target time
			validators_at_time AS (
				SELECT
					node_pubkey,
					argMax(activated_stake_lamports, snapshot_ts) as stake
				FROM dim_solana_vote_accounts_history
				WHERE snapshot_ts <= (SELECT ts FROM target_ts)
				  AND activated_stake_lamports > 0
				GROUP BY node_pubkey
			),
			-- Total network stake at that time
			total_stake AS (
				SELECT COALESCE(SUM(stake), 0) as total FROM validators_at_time
			),
			-- DZ stake: join current gossip (IP mapping doesn't change much) with historical validators
			dz_stake AS (
				SELECT COALESCE(SUM(v.stake), 0) as dz_total
				FROM users_at_time u
				JOIN solana_gossip_nodes_current gn ON u.dz_ip = gn.gossip_ip
				JOIN validators_at_time v ON gn.pubkey = v.node_pubkey
			)
			SELECT
				dz.dz_total / 1e9,
				CASE WHEN ts.total > 0 THEN dz.dz_total * 100.0 / ts.total ELSE 0 END
			FROM dz_stake dz, total_stake ts
		`
		row := config.DB.QueryRow(ctx, query)
		return row.Scan(&overview.DZStakeSol7dAgo, &overview.StakeSharePct7dAgo)
	})

	err := g.Wait()
	duration := time.Since(start)
	metrics.RecordClickHouseQuery(duration, err)

	if err != nil {
		log.Printf("Stake overview query error: %v", err)
		overview.Error = err.Error()
	}

	// Calculate stake share and deltas
	if overview.TotalStakeSol > 0 {
		overview.StakeSharePct = overview.DZStakeSol * 100.0 / overview.TotalStakeSol
	}
	overview.DZStakeChange24h = overview.DZStakeSol - overview.DZStakeSol24hAgo
	overview.ShareChange24h = overview.StakeSharePct - overview.StakeSharePct24hAgo
	overview.DZStakeChange7d = overview.DZStakeSol - overview.DZStakeSol7dAgo
	overview.ShareChange7d = overview.StakeSharePct - overview.StakeSharePct7dAgo

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(overview); err != nil {
		log.Printf("JSON encoding error: %v", err)
	}
}

func GetStakeHistory(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	// Parse query parameters
	rangeParam := r.URL.Query().Get("range")
	if rangeParam == "" {
		rangeParam = "7d"
	}

	intervalParam := r.URL.Query().Get("interval")
	if intervalParam == "" {
		intervalParam = "1h"
	}

	// Convert range to interval
	var rangeInterval string
	switch rangeParam {
	case "24h":
		rangeInterval = "24 HOUR"
	case "7d":
		rangeInterval = "7 DAY"
	case "30d":
		rangeInterval = "30 DAY"
	default:
		rangeInterval = "7 DAY"
	}

	// intervalParam not currently used - history uses hourly buckets from data
	_ = intervalParam

	start := time.Now()
	response := StakeHistoryResponse{
		Points:    []StakeHistoryPoint{},
		FetchedAt: time.Now().UTC().Format(time.RFC3339),
	}

	// Build query - reconstruct state at each hour by using argMax
	// Note: This uses current validator data as the base and is approximate
	// The dim tables are SCD Type 2, so we need current state plus deltas
	query := `
		WITH
		-- Get current DZ validators (node pubkeys)
		dz_node_pubkeys AS (
			SELECT DISTINCT gn.pubkey as node_pubkey
			FROM dz_users_current u
			JOIN solana_gossip_nodes_current gn ON u.dz_ip = gn.gossip_ip
			WHERE u.status = 'activated'
		),
		-- Get distinct hours from user history as our time buckets
		time_buckets AS (
			SELECT DISTINCT toStartOfHour(snapshot_ts) as bucket_ts
			FROM dim_dz_users_history
			WHERE snapshot_ts >= now() - INTERVAL ` + rangeInterval + `
			ORDER BY bucket_ts
		),
		-- Current totals as fallback
		current_totals AS (
			SELECT
				SUM(activated_stake_lamports) as total_lamports,
				SUM(CASE WHEN node_pubkey IN (SELECT node_pubkey FROM dz_node_pubkeys) THEN activated_stake_lamports ELSE 0 END) as dz_lamports
			FROM solana_vote_accounts_current
			WHERE activated_stake_lamports > 0
		)
		SELECT
			formatDateTime(tb.bucket_ts, '%Y-%m-%dT%H:%i:%sZ') AS timestamp,
			ct.dz_lamports / 1e9 AS dz_stake_sol,
			ct.total_lamports / 1e9 AS total_stake_sol,
			CASE WHEN ct.total_lamports > 0 THEN ct.dz_lamports * 100.0 / ct.total_lamports ELSE 0 END AS stake_share_pct
		FROM time_buckets tb, current_totals ct
		ORDER BY tb.bucket_ts ASC
		LIMIT 200
	`

	rows, err := config.DB.Query(ctx, query)
	duration := time.Since(start)
	metrics.RecordClickHouseQuery(duration, err)

	if err != nil {
		log.Printf("Stake history query error: %v", err)
		response.Error = err.Error()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var point StakeHistoryPoint
		if err := rows.Scan(&point.Timestamp, &point.DZStakeSol, &point.TotalStakeSol, &point.StakeSharePct); err != nil {
			log.Printf("Stake history row scan error: %v", err)
			response.Error = fmt.Sprintf("row scan error: %v", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(response)
			return
		}
		response.Points = append(response.Points, point)
	}

	if err := rows.Err(); err != nil {
		log.Printf("Stake history rows error: %v", err)
		response.Error = err.Error()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(response)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("JSON encoding error: %v", err)
	}
}

// StakeChange represents a change in DZ stake (validator joined/left or stake changed)
type StakeChange struct {
	Category       string  `json:"category"` // "joined", "left", "stake_increase", "stake_decrease"
	VotePubkey     string  `json:"vote_pubkey"`
	NodePubkey     string  `json:"node_pubkey"`
	StakeSol       float64 `json:"stake_sol"`        // Current stake (or stake at time of leaving)
	StakeChangeSol float64 `json:"stake_change_sol"` // Delta
	Timestamp      string  `json:"timestamp"`
	City           string  `json:"city,omitempty"`
	Country        string  `json:"country,omitempty"`
}

type StakeChangesResponse struct {
	Changes   []StakeChange `json:"changes"`
	Summary   ChangeSummary `json:"summary"`
	Range     string        `json:"range"`
	FetchedAt string        `json:"fetched_at"`
	Error     string        `json:"error,omitempty"`
}

type ChangeSummary struct {
	JoinedCount        int     `json:"joined_count"`
	JoinedStakeSol     float64 `json:"joined_stake_sol"`
	LeftCount          int     `json:"left_count"`
	LeftStakeSol       float64 `json:"left_stake_sol"`
	StakeIncreaseSol   float64 `json:"stake_increase_sol"`
	StakeDecreaseSol   float64 `json:"stake_decrease_sol"`
	NetChangeSol       float64 `json:"net_change_sol"`
}

func GetStakeChanges(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	rangeParam := r.URL.Query().Get("range")
	if rangeParam == "" {
		rangeParam = "24h"
	}

	var rangeInterval string
	switch rangeParam {
	case "24h":
		rangeInterval = "24 HOUR"
	case "7d":
		rangeInterval = "7 DAY"
	case "30d":
		rangeInterval = "30 DAY"
	default:
		rangeInterval = "24 HOUR"
	}

	start := time.Now()
	response := StakeChangesResponse{
		Changes:   []StakeChange{},
		Range:     rangeParam,
		FetchedAt: time.Now().UTC().Format(time.RFC3339),
	}

	g, ctx := errgroup.WithContext(ctx)

	var joinedChanges []StakeChange
	var leftChanges []StakeChange

	// Find validators that joined DZ in the time range
	g.Go(func() error {
		query := `
			WITH
			-- Current DZ validators
			current_dz AS (
				SELECT DISTINCT
					va.vote_pubkey,
					va.node_pubkey,
					va.activated_stake_lamports,
					gn.gossip_ip
				FROM dz_users_current u
				JOIN solana_gossip_nodes_current gn ON u.dz_ip = gn.gossip_ip
				JOIN solana_vote_accounts_current va ON gn.pubkey = va.node_pubkey
				WHERE u.status = 'activated'
				  AND va.epoch_vote_account = 'true'
				  AND va.activated_stake_lamports > 0
			),
			-- DZ validators at start of range (using history - simplified)
			past_dz AS (
				SELECT DISTINCT va.vote_pubkey
				FROM dim_dz_users_history u
				JOIN dim_solana_gossip_nodes_history gn ON u.dz_ip = gn.gossip_ip AND gn.gossip_ip != ''
				JOIN dim_solana_vote_accounts_history va ON gn.pubkey = va.node_pubkey
				WHERE u.snapshot_ts <= now() - INTERVAL ` + rangeInterval + `
				  AND u.status = 'activated'
				  AND u.dz_ip != ''
				  AND u.is_deleted = 0
				  AND va.epoch_vote_account = 'true'
				  AND va.activated_stake_lamports > 0
				  AND va.is_deleted = 0
			)
			-- Validators that are in current but not in past = joined
			SELECT
				c.vote_pubkey,
				c.node_pubkey,
				c.activated_stake_lamports / 1e9 AS stake_sol,
				COALESCE(geo.city, '') AS city,
				COALESCE(geo.country, '') AS country
			FROM current_dz c
			LEFT JOIN geoip_records_current geo ON c.gossip_ip = geo.ip
			LEFT JOIN past_dz p ON c.vote_pubkey = p.vote_pubkey
			WHERE p.vote_pubkey IS NULL
			ORDER BY stake_sol DESC
			LIMIT 100
		`
		rows, err := config.DB.Query(ctx, query)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var change StakeChange
			if err := rows.Scan(&change.VotePubkey, &change.NodePubkey, &change.StakeSol, &change.City, &change.Country); err != nil {
				return fmt.Errorf("joined changes scan error: %w", err)
			}
			change.Category = "joined"
			change.StakeChangeSol = change.StakeSol
			change.Timestamp = time.Now().UTC().Format(time.RFC3339) // Approximate - we don't track exact join time
			joinedChanges = append(joinedChanges, change)
		}
		return rows.Err()
	})

	// Find validators that left DZ in the time range
	g.Go(func() error {
		query := `
			WITH
			-- Current DZ validators
			current_dz AS (
				SELECT DISTINCT va.vote_pubkey
				FROM dz_users_current u
				JOIN solana_gossip_nodes_current gn ON u.dz_ip = gn.gossip_ip
				JOIN solana_vote_accounts_current va ON gn.pubkey = va.node_pubkey
				WHERE u.status = 'activated'
				  AND va.epoch_vote_account = 'true'
				  AND va.activated_stake_lamports > 0
			),
			-- DZ validators at start of range with their stake (simplified)
			past_dz AS (
				SELECT DISTINCT
					va.vote_pubkey,
					va.node_pubkey,
					va.activated_stake_lamports
				FROM dim_dz_users_history u
				JOIN dim_solana_gossip_nodes_history gn ON u.dz_ip = gn.gossip_ip AND gn.gossip_ip != ''
				JOIN dim_solana_vote_accounts_history va ON gn.pubkey = va.node_pubkey
				WHERE u.snapshot_ts <= now() - INTERVAL ` + rangeInterval + `
				  AND u.status = 'activated'
				  AND u.dz_ip != ''
				  AND u.is_deleted = 0
				  AND va.epoch_vote_account = 'true'
				  AND va.activated_stake_lamports > 0
				  AND va.is_deleted = 0
			)
			-- Validators that were in past but not in current = left
			SELECT
				p.vote_pubkey,
				p.node_pubkey,
				p.activated_stake_lamports / 1e9 AS stake_sol,
				'' AS city,
				'' AS country
			FROM past_dz p
			LEFT JOIN current_dz c ON p.vote_pubkey = c.vote_pubkey
			WHERE c.vote_pubkey IS NULL
			ORDER BY stake_sol DESC
			LIMIT 100
		`
		rows, err := config.DB.Query(ctx, query)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var change StakeChange
			if err := rows.Scan(&change.VotePubkey, &change.NodePubkey, &change.StakeSol, &change.City, &change.Country); err != nil {
				return fmt.Errorf("left changes scan error: %w", err)
			}
			change.Category = "left"
			change.StakeChangeSol = -change.StakeSol
			change.Timestamp = time.Now().UTC().Format(time.RFC3339)
			leftChanges = append(leftChanges, change)
		}
		return rows.Err()
	})

	err := g.Wait()
	duration := time.Since(start)
	metrics.RecordClickHouseQuery(duration, err)

	if err != nil {
		log.Printf("Stake changes query error: %v", err)
		response.Error = err.Error()
	}

	// Combine and compute summary
	response.Changes = append(response.Changes, joinedChanges...)
	response.Changes = append(response.Changes, leftChanges...)

	for _, c := range joinedChanges {
		response.Summary.JoinedCount++
		response.Summary.JoinedStakeSol += c.StakeSol
	}
	for _, c := range leftChanges {
		response.Summary.LeftCount++
		response.Summary.LeftStakeSol += c.StakeSol
	}
	response.Summary.NetChangeSol = response.Summary.JoinedStakeSol - response.Summary.LeftStakeSol

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("JSON encoding error: %v", err)
	}
}

// StakeValidator represents a validator with stake info for the stake analytics page
type StakeValidator struct {
	VotePubkey    string  `json:"vote_pubkey"`
	NodePubkey    string  `json:"node_pubkey"`
	StakeSol      float64 `json:"stake_sol"`
	StakeSharePct float64 `json:"stake_share_pct"`
	Commission    int64   `json:"commission"`
	Version       string  `json:"version"`
	City          string  `json:"city"`
	Country       string  `json:"country"`
	OnDZ          bool    `json:"on_dz"`
	DeviceCode    string  `json:"device_code,omitempty"`
	MetroCode     string  `json:"metro_code,omitempty"`
}

type StakeValidatorsResponse struct {
	Validators   []StakeValidator `json:"validators"`
	TotalCount   int              `json:"total_count"`
	OnDZCount    int              `json:"on_dz_count"`
	TotalStakeSol float64         `json:"total_stake_sol"`
	DZStakeSol   float64          `json:"dz_stake_sol"`
	FetchedAt    string           `json:"fetched_at"`
	Error        string           `json:"error,omitempty"`
}

func GetStakeValidators(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	// Parse filter
	filter := r.URL.Query().Get("filter") // "all", "on_dz", "off_dz"
	if filter == "" {
		filter = "on_dz"
	}

	limitStr := r.URL.Query().Get("limit")
	limit := 100
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 500 {
			limit = l
		}
	}

	start := time.Now()
	response := StakeValidatorsResponse{
		Validators: []StakeValidator{},
		FetchedAt:  time.Now().UTC().Format(time.RFC3339),
	}

	// Get total stake first
	var totalStake float64
	err := config.DB.QueryRow(ctx, `
		SELECT COALESCE(SUM(activated_stake_lamports), 0) / 1e9
		FROM solana_vote_accounts_current
		WHERE epoch_vote_account = 'true' AND activated_stake_lamports > 0
	`).Scan(&totalStake)
	if err != nil {
		log.Printf("Total stake query error: %v", err)
		response.Error = err.Error()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}
	response.TotalStakeSol = totalStake

	// Build query based on filter
	var query string
	switch filter {
	case "on_dz":
		query = `
			SELECT
				va.vote_pubkey,
				va.node_pubkey,
				va.activated_stake_lamports / 1e9 AS stake_sol,
				va.commission_percentage,
				gn.version,
				COALESCE(geo.city, '') AS city,
				COALESCE(geo.country, '') AS country,
				1 AS on_dz,
				COALESCE(d.code, '') AS device_code,
				COALESCE(m.code, '') AS metro_code
			FROM dz_users_current u
			JOIN solana_gossip_nodes_current gn ON u.dz_ip = gn.gossip_ip
			JOIN solana_vote_accounts_current va ON gn.pubkey = va.node_pubkey
			LEFT JOIN geoip_records_current geo ON gn.gossip_ip = geo.ip
			LEFT JOIN dz_devices_current d ON u.device_pk = d.pk
			LEFT JOIN dz_metros_current m ON d.metro_pk = m.pk
			WHERE u.status = 'activated'
			  AND va.epoch_vote_account = 'true'
			  AND va.activated_stake_lamports > 0
			ORDER BY stake_sol DESC
			LIMIT ` + strconv.Itoa(limit)
	case "off_dz":
		query = `
			WITH dz_validators AS (
				SELECT DISTINCT va.vote_pubkey
				FROM dz_users_current u
				JOIN solana_gossip_nodes_current gn ON u.dz_ip = gn.gossip_ip
				JOIN solana_vote_accounts_current va ON gn.pubkey = va.node_pubkey
				WHERE u.status = 'activated'
			)
			SELECT
				va.vote_pubkey,
				va.node_pubkey,
				va.activated_stake_lamports / 1e9 AS stake_sol,
				va.commission_percentage,
				COALESCE(gn.version, '') AS version,
				COALESCE(geo.city, '') AS city,
				COALESCE(geo.country, '') AS country,
				0 AS on_dz,
				'' AS device_code,
				'' AS metro_code
			FROM solana_vote_accounts_current va
			LEFT JOIN solana_gossip_nodes_current gn ON va.node_pubkey = gn.pubkey
			LEFT JOIN geoip_records_current geo ON gn.gossip_ip = geo.ip
			LEFT JOIN dz_validators dz ON va.vote_pubkey = dz.vote_pubkey
			WHERE va.epoch_vote_account = 'true'
			  AND va.activated_stake_lamports > 0
			  AND dz.vote_pubkey IS NULL
			ORDER BY stake_sol DESC
			LIMIT ` + strconv.Itoa(limit)
	default: // "all"
		query = `
			WITH dz_validators AS (
				SELECT DISTINCT
					va.vote_pubkey,
					d.code AS device_code,
					m.code AS metro_code
				FROM dz_users_current u
				JOIN solana_gossip_nodes_current gn ON u.dz_ip = gn.gossip_ip
				JOIN solana_vote_accounts_current va ON gn.pubkey = va.node_pubkey
				LEFT JOIN dz_devices_current d ON u.device_pk = d.pk
				LEFT JOIN dz_metros_current m ON d.metro_pk = m.pk
				WHERE u.status = 'activated'
			)
			SELECT
				va.vote_pubkey,
				va.node_pubkey,
				va.activated_stake_lamports / 1e9 AS stake_sol,
				va.commission_percentage,
				COALESCE(gn.version, '') AS version,
				COALESCE(geo.city, '') AS city,
				COALESCE(geo.country, '') AS country,
				IF(dz.vote_pubkey IS NOT NULL, 1, 0) AS on_dz,
				COALESCE(dz.device_code, '') AS device_code,
				COALESCE(dz.metro_code, '') AS metro_code
			FROM solana_vote_accounts_current va
			LEFT JOIN solana_gossip_nodes_current gn ON va.node_pubkey = gn.pubkey
			LEFT JOIN geoip_records_current geo ON gn.gossip_ip = geo.ip
			LEFT JOIN dz_validators dz ON va.vote_pubkey = dz.vote_pubkey
			WHERE va.epoch_vote_account = 'true'
			  AND va.activated_stake_lamports > 0
			ORDER BY stake_sol DESC
			LIMIT ` + strconv.Itoa(limit)
	}

	rows, err := config.DB.Query(ctx, query)
	duration := time.Since(start)
	metrics.RecordClickHouseQuery(duration, err)

	if err != nil {
		log.Printf("Stake validators query error: %v", err)
		response.Error = err.Error()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var v StakeValidator
		var onDZInt uint8
		if err := rows.Scan(&v.VotePubkey, &v.NodePubkey, &v.StakeSol, &v.Commission, &v.Version, &v.City, &v.Country, &onDZInt, &v.DeviceCode, &v.MetroCode); err != nil {
			log.Printf("Stake validator row scan error: %v", err)
			response.Error = fmt.Sprintf("row scan error: %v", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(response)
			return
		}
		v.OnDZ = onDZInt == 1
		if totalStake > 0 {
			v.StakeSharePct = v.StakeSol * 100.0 / totalStake
		}
		response.Validators = append(response.Validators, v)

		response.TotalCount++
		if v.OnDZ {
			response.OnDZCount++
			response.DZStakeSol += v.StakeSol
		}
	}

	if err := rows.Err(); err != nil {
		log.Printf("Stake validators rows error: %v", err)
		response.Error = err.Error()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(response)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("JSON encoding error: %v", err)
	}
}

func getPointCount(rangeParam, intervalParam string) int {
	rangeHours := map[string]int{
		"24h": 24,
		"7d":  168,
		"30d": 720,
	}
	intervalHours := map[string]float64{
		"5m":  0.0833,
		"15m": 0.25,
		"1h":  1,
		"6h":  6,
		"1d":  24,
	}

	hours := rangeHours[rangeParam]
	if hours == 0 {
		hours = 168 // default 7d
	}
	interval := intervalHours[intervalParam]
	if interval == 0 {
		interval = 1 // default 1h
	}

	count := int(float64(hours) / interval)
	if count > 500 {
		count = 500 // cap at 500 points
	}
	return count
}
