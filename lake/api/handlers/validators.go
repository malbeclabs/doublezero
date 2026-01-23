package handlers

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/malbeclabs/doublezero/lake/api/config"
	"github.com/malbeclabs/doublezero/lake/api/metrics"
)

type ValidatorListItem struct {
	VotePubkey   string  `json:"vote_pubkey"`
	NodePubkey   string  `json:"node_pubkey"`
	StakeSol     float64 `json:"stake_sol"`
	StakeShare   float64 `json:"stake_share"`
	Commission   int64   `json:"commission"`
	OnDZ         bool    `json:"on_dz"`
	DeviceCode   string  `json:"device_code"`
	MetroCode    string  `json:"metro_code"`
	City         string  `json:"city"`
	Country      string  `json:"country"`
	InBps        float64 `json:"in_bps"`
	OutBps       float64 `json:"out_bps"`
	SkipRate     float64 `json:"skip_rate"`
	Version      string  `json:"version"`
}

type ValidatorListResponse struct {
	Items     []ValidatorListItem `json:"items"`
	Total     int                 `json:"total"`
	OnDZCount int                 `json:"on_dz_count"`
	Limit     int                 `json:"limit"`
	Offset    int                 `json:"offset"`
}

var validatorSortFields = map[string]string{
	"vote":       "v.vote_pubkey",
	"node":       "v.node_pubkey",
	"stake":      "v.activated_stake_lamports",
	"share":      "v.activated_stake_lamports",
	"commission": "COALESCE(v.commission_percentage, 0)",
	"dz":         "on_dz",
	"device":     "device_code",
	"city":       "city",
	"country":    "country",
	"in":         "in_bps",
	"out":        "out_bps",
	"skip":       "skip_rate",
	"version":    "version",
}

var validatorFilterFields = map[string]FilterFieldConfig{
	"vote":       {Column: "vote_pubkey", Type: FieldTypeText},
	"node":       {Column: "node_pubkey", Type: FieldTypeText},
	"stake":      {Column: "stake_sol", Type: FieldTypeStake},
	"share":      {Column: "stake_share", Type: FieldTypeNumeric},
	"commission": {Column: "commission", Type: FieldTypeNumeric},
	"dz":         {Column: "on_dz", Type: FieldTypeBoolean},
	"device":     {Column: "device_code", Type: FieldTypeText},
	"city":       {Column: "city", Type: FieldTypeText},
	"country":    {Column: "country", Type: FieldTypeText},
	"in":         {Column: "in_bps", Type: FieldTypeBandwidth},
	"out":        {Column: "out_bps", Type: FieldTypeBandwidth},
	"skip":       {Column: "skip_rate", Type: FieldTypeNumeric},
	"version":    {Column: "version", Type: FieldTypeText},
}

func GetValidators(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	pagination := ParsePagination(r, 100)
	sort := ParseSort(r, "stake", validatorSortFields)
	filter := ParseFilter(r)
	start := time.Now()

	// Build filter clause
	filterClause, filterArgs := filter.BuildFilterClause(validatorFilterFields)
	whereFilter := ""
	if filterClause != "" {
		whereFilter = " AND " + filterClause
	}

	// Base CTE query for validators data
	baseQuery := `
		WITH total_stake AS (
			SELECT sum(activated_stake_lamports) as total
			FROM solana_vote_accounts_current
			WHERE epoch_vote_account = 'true'
		),
		dz_validators AS (
			SELECT
				g.pubkey as node_pubkey,
				u.tunnel_id,
				u.device_pk,
				d.code as device_code,
				m.code as metro_code
			FROM solana_gossip_nodes_current g
			JOIN dz_users_current u ON g.gossip_ip = u.dz_ip
			JOIN dz_devices_current d ON u.device_pk = d.pk
			LEFT JOIN dz_metros_current m ON d.metro_pk = m.pk
			WHERE u.status = 'activated'
				AND u.dz_ip IS NOT NULL
				AND u.dz_ip != ''
		),
		traffic_rates AS (
			SELECT
				user_tunnel_id,
				CASE WHEN SUM(delta_duration) > 0
					THEN SUM(in_octets_delta) * 8 / SUM(delta_duration)
					ELSE 0
				END as in_bps,
				CASE WHEN SUM(delta_duration) > 0
					THEN SUM(out_octets_delta) * 8 / SUM(delta_duration)
					ELSE 0
				END as out_bps
			FROM fact_dz_device_interface_counters
			WHERE event_ts > now() - INTERVAL 5 MINUTE
				AND user_tunnel_id IS NOT NULL
			GROUP BY user_tunnel_id
		),
		geoip AS (
			SELECT
				g.pubkey,
				geo.city,
				geo.country
			FROM solana_gossip_nodes_current g
			LEFT JOIN geoip_records_current geo ON g.gossip_ip = geo.ip
		),
		skip_rates AS (
			SELECT
				leader_identity_pubkey,
				CASE WHEN leader_slots_assigned_cum > 0
					THEN (leader_slots_assigned_cum - blocks_produced_cum) * 100.0 / leader_slots_assigned_cum
					ELSE 0
				END as skip_rate
			FROM fact_solana_block_production
			WHERE (leader_identity_pubkey, event_ts) IN (
				SELECT leader_identity_pubkey, max(event_ts)
				FROM fact_solana_block_production
				GROUP BY leader_identity_pubkey
			)
		),
		validators_data AS (
			SELECT
				v.vote_pubkey,
				v.node_pubkey,
				v.activated_stake_lamports,
				v.activated_stake_lamports / 1e9 as stake_sol,
				CASE WHEN ts.total > 0
					THEN v.activated_stake_lamports * 100.0 / ts.total
					ELSE 0
				END as stake_share,
				COALESCE(v.commission_percentage, 0) as commission,
				dz.node_pubkey IS NOT NULL as on_dz,
				COALESCE(dz.device_code, '') as device_code,
				COALESCE(dz.metro_code, '') as metro_code,
				COALESCE(geo.city, '') as city,
				COALESCE(geo.country, '') as country,
				COALESCE(tr.in_bps, 0) as in_bps,
				COALESCE(tr.out_bps, 0) as out_bps,
				COALESCE(sr.skip_rate, 0) as skip_rate,
				COALESCE(g.version, '') as version
			FROM solana_vote_accounts_current v
			CROSS JOIN total_stake ts
			LEFT JOIN solana_gossip_nodes_current g ON v.node_pubkey = g.pubkey
			LEFT JOIN dz_validators dz ON v.node_pubkey = dz.node_pubkey
			LEFT JOIN traffic_rates tr ON dz.tunnel_id = tr.user_tunnel_id
			LEFT JOIN geoip geo ON v.node_pubkey = geo.pubkey
			LEFT JOIN skip_rates sr ON v.node_pubkey = sr.leader_identity_pubkey
			WHERE v.epoch_vote_account = 'true'
		)
	`

	// Get total count (with filter)
	countQuery := baseQuery + `SELECT count(*) FROM validators_data WHERE 1=1` + whereFilter
	var total uint64
	if err := config.DB.QueryRow(ctx, countQuery, filterArgs...).Scan(&total); err != nil {
		log.Printf("Validators count error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Get on_dz count (with filter)
	onDZCountQuery := baseQuery + `SELECT count(*) FROM validators_data WHERE on_dz = true` + whereFilter
	var onDZCount uint64
	if err := config.DB.QueryRow(ctx, onDZCountQuery, filterArgs...).Scan(&onDZCount); err != nil {
		log.Printf("Validators on_dz count error: %v", err)
		onDZCount = 0
	}

	// Build sort clause using output column names
	sortFieldsForQuery := map[string]string{
		"vote":       "vote_pubkey",
		"node":       "node_pubkey",
		"stake":      "activated_stake_lamports",
		"share":      "activated_stake_lamports",
		"commission": "commission",
		"dz":         "on_dz",
		"device":     "device_code",
		"city":       "city",
		"country":    "country",
		"in":         "in_bps",
		"out":        "out_bps",
		"skip":       "skip_rate",
		"version":    "version",
	}
	orderBy := sort.OrderByClause(sortFieldsForQuery)

	// Main query
	query := baseQuery + `
		SELECT vote_pubkey, node_pubkey, stake_sol, stake_share, commission,
			on_dz, device_code, metro_code, city, country, in_bps, out_bps, skip_rate, version
		FROM validators_data
		WHERE 1=1` + whereFilter + `
		` + orderBy + `
		LIMIT ? OFFSET ?
	`

	queryArgs := append(filterArgs, pagination.Limit, pagination.Offset)
	rows, err := config.DB.Query(ctx, query, queryArgs...)
	duration := time.Since(start)
	metrics.RecordClickHouseQuery(duration, err)

	if err != nil {
		log.Printf("Validators query error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var validators []ValidatorListItem
	for rows.Next() {
		var v ValidatorListItem
		if err := rows.Scan(
			&v.VotePubkey,
			&v.NodePubkey,
			&v.StakeSol,
			&v.StakeShare,
			&v.Commission,
			&v.OnDZ,
			&v.DeviceCode,
			&v.MetroCode,
			&v.City,
			&v.Country,
			&v.InBps,
			&v.OutBps,
			&v.SkipRate,
			&v.Version,
		); err != nil {
			log.Printf("Validators scan error: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		validators = append(validators, v)
	}

	if err := rows.Err(); err != nil {
		log.Printf("Validators rows error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Return empty array instead of null
	if validators == nil {
		validators = []ValidatorListItem{}
	}

	response := ValidatorListResponse{
		Items:     validators,
		Total:     int(total),
		OnDZCount: int(onDZCount),
		Limit:     pagination.Limit,
		Offset:    pagination.Offset,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("JSON encoding error: %v", err)
	}
}

type ValidatorDetail struct {
	VotePubkey      string  `json:"vote_pubkey"`
	NodePubkey      string  `json:"node_pubkey"`
	StakeSol        float64 `json:"stake_sol"`
	StakeShare      float64 `json:"stake_share"`
	Commission      int64   `json:"commission"`
	OnDZ            bool    `json:"on_dz"`
	DevicePK        string  `json:"device_pk"`
	DeviceCode      string  `json:"device_code"`
	MetroPK         string  `json:"metro_pk"`
	MetroCode       string  `json:"metro_code"`
	City            string  `json:"city"`
	Country         string  `json:"country"`
	GossipIP        string  `json:"gossip_ip"`
	GossipPort      int32   `json:"gossip_port"`
	InBps           float64 `json:"in_bps"`
	OutBps          float64 `json:"out_bps"`
	SkipRate        float64 `json:"skip_rate"`
	Version         string  `json:"version"`
}

func GetValidator(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	votePubkey := chi.URLParam(r, "vote_pubkey")
	if votePubkey == "" {
		http.Error(w, "missing vote_pubkey", http.StatusBadRequest)
		return
	}

	start := time.Now()
	query := `
		WITH total_stake AS (
			SELECT sum(activated_stake_lamports) as total
			FROM solana_vote_accounts_current
			WHERE epoch_vote_account = 'true'
		),
		dz_info AS (
			SELECT
				g.pubkey as node_pubkey,
				u.tunnel_id,
				u.device_pk,
				d.code as device_code,
				d.metro_pk,
				m.code as metro_code
			FROM solana_gossip_nodes_current g
			JOIN dz_users_current u ON g.gossip_ip = u.dz_ip
			JOIN dz_devices_current d ON u.device_pk = d.pk
			LEFT JOIN dz_metros_current m ON d.metro_pk = m.pk
			WHERE u.status = 'activated'
				AND u.dz_ip IS NOT NULL
				AND u.dz_ip != ''
		),
		traffic_rates AS (
			SELECT
				user_tunnel_id,
				CASE WHEN SUM(delta_duration) > 0
					THEN SUM(in_octets_delta) * 8 / SUM(delta_duration)
					ELSE 0
				END as in_bps,
				CASE WHEN SUM(delta_duration) > 0
					THEN SUM(out_octets_delta) * 8 / SUM(delta_duration)
					ELSE 0
				END as out_bps
			FROM fact_dz_device_interface_counters
			WHERE event_ts > now() - INTERVAL 5 MINUTE
				AND user_tunnel_id IS NOT NULL
			GROUP BY user_tunnel_id
		),
		skip_rates AS (
			SELECT
				leader_identity_pubkey,
				CASE WHEN leader_slots_assigned_cum > 0
					THEN (leader_slots_assigned_cum - blocks_produced_cum) * 100.0 / leader_slots_assigned_cum
					ELSE 0
				END as skip_rate
			FROM fact_solana_block_production
			WHERE (leader_identity_pubkey, event_ts) IN (
				SELECT leader_identity_pubkey, max(event_ts)
				FROM fact_solana_block_production
				GROUP BY leader_identity_pubkey
			)
		)
		SELECT
			v.vote_pubkey,
			v.node_pubkey,
			v.activated_stake_lamports / 1e9 as stake_sol,
			CASE WHEN ts.total > 0
				THEN v.activated_stake_lamports * 100.0 / ts.total
				ELSE 0
			END as stake_share,
			COALESCE(v.commission_percentage, 0) as commission,
			dz.node_pubkey IS NOT NULL as on_dz,
			COALESCE(dz.device_pk, '') as device_pk,
			COALESCE(dz.device_code, '') as device_code,
			COALESCE(dz.metro_pk, '') as metro_pk,
			COALESCE(dz.metro_code, '') as metro_code,
			COALESCE(geo.city, '') as city,
			COALESCE(geo.country, '') as country,
			COALESCE(g.gossip_ip, '') as gossip_ip,
			COALESCE(g.gossip_port, 0) as gossip_port,
			COALESCE(tr.in_bps, 0) as in_bps,
			COALESCE(tr.out_bps, 0) as out_bps,
			COALESCE(sr.skip_rate, 0) as skip_rate,
			COALESCE(g.version, '') as version
		FROM solana_vote_accounts_current v
		CROSS JOIN total_stake ts
		LEFT JOIN solana_gossip_nodes_current g ON v.node_pubkey = g.pubkey
		LEFT JOIN geoip_records_current geo ON g.gossip_ip = geo.ip
		LEFT JOIN dz_info dz ON v.node_pubkey = dz.node_pubkey
		LEFT JOIN traffic_rates tr ON dz.tunnel_id = tr.user_tunnel_id
		LEFT JOIN skip_rates sr ON v.node_pubkey = sr.leader_identity_pubkey
		WHERE v.vote_pubkey = ?
	`

	var validator ValidatorDetail
	err := config.DB.QueryRow(ctx, query, votePubkey).Scan(
		&validator.VotePubkey,
		&validator.NodePubkey,
		&validator.StakeSol,
		&validator.StakeShare,
		&validator.Commission,
		&validator.OnDZ,
		&validator.DevicePK,
		&validator.DeviceCode,
		&validator.MetroPK,
		&validator.MetroCode,
		&validator.City,
		&validator.Country,
		&validator.GossipIP,
		&validator.GossipPort,
		&validator.InBps,
		&validator.OutBps,
		&validator.SkipRate,
		&validator.Version,
	)
	duration := time.Since(start)
	metrics.RecordClickHouseQuery(duration, err)

	if err != nil {
		log.Printf("Validator query error: %v", err)
		http.Error(w, "validator not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(validator); err != nil {
		log.Printf("JSON encoding error: %v", err)
	}
}
