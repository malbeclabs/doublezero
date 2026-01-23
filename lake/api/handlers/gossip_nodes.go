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

type GossipNodeListItem struct {
	Pubkey      string  `json:"pubkey"`
	GossipIP    string  `json:"gossip_ip"`
	GossipPort  int32   `json:"gossip_port"`
	Version     string  `json:"version"`
	City        string  `json:"city"`
	Country     string  `json:"country"`
	OnDZ        bool    `json:"on_dz"`
	DeviceCode  string  `json:"device_code"`
	MetroCode   string  `json:"metro_code"`
	StakeSol    float64 `json:"stake_sol"`
	IsValidator bool    `json:"is_validator"`
}

type GossipNodeListResponse struct {
	Items          []GossipNodeListItem `json:"items"`
	Total          int                  `json:"total"`
	OnDZCount      int                  `json:"on_dz_count"`
	ValidatorCount int                  `json:"validator_count"`
	Limit          int                  `json:"limit"`
	Offset         int                  `json:"offset"`
}

var gossipNodeSortFields = map[string]string{
	"pubkey":    "pubkey",
	"ip":        "gossip_ip",
	"version":   "version",
	"city":      "city",
	"country":   "country",
	"validator": "is_validator",
	"stake":     "stake_sol",
	"dz":        "on_dz",
	"device":    "device_code",
}

var gossipNodeFilterFields = map[string]FilterFieldConfig{
	"pubkey":    {Column: "pubkey", Type: FieldTypeText},
	"ip":        {Column: "gossip_ip", Type: FieldTypeText},
	"version":   {Column: "version", Type: FieldTypeText},
	"city":      {Column: "city", Type: FieldTypeText},
	"country":   {Column: "country", Type: FieldTypeText},
	"validator": {Column: "is_validator", Type: FieldTypeBoolean},
	"stake":     {Column: "stake_sol", Type: FieldTypeStake},
	"dz":        {Column: "on_dz", Type: FieldTypeBoolean},
	"device":    {Column: "device_code", Type: FieldTypeText},
}

func GetGossipNodes(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	pagination := ParsePagination(r, 100)
	sort := ParseSort(r, "stake", gossipNodeSortFields)
	filter := ParseFilter(r)
	start := time.Now()

	// Build filter clause
	filterClause, filterArgs := filter.BuildFilterClause(gossipNodeFilterFields)
	whereFilter := ""
	if filterClause != "" {
		whereFilter = " AND " + filterClause
	}

	// Base CTE query for gossip nodes data
	baseQuery := `
		WITH dz_nodes AS (
			SELECT
				u.dz_ip,
				d.code as device_code,
				m.code as metro_code
			FROM dz_users_current u
			JOIN dz_devices_current d ON u.device_pk = d.pk
			LEFT JOIN dz_metros_current m ON d.metro_pk = m.pk
			WHERE u.status = 'activated'
				AND u.dz_ip IS NOT NULL
				AND u.dz_ip != ''
		),
		validator_stake AS (
			SELECT
				node_pubkey,
				activated_stake_lamports / 1e9 as stake_sol
			FROM solana_vote_accounts_current
			WHERE epoch_vote_account = 'true'
		),
		gossip_data AS (
			SELECT
				g.pubkey,
				COALESCE(g.gossip_ip, '') as gossip_ip,
				COALESCE(g.gossip_port, 0) as gossip_port,
				COALESCE(g.version, '') as version,
				COALESCE(geo.city, '') as city,
				COALESCE(geo.country, '') as country,
				dz.dz_ip != '' as on_dz,
				COALESCE(dz.device_code, '') as device_code,
				COALESCE(dz.metro_code, '') as metro_code,
				COALESCE(vs.stake_sol, 0) as stake_sol,
				vs.node_pubkey IS NOT NULL as is_validator
			FROM solana_gossip_nodes_current g
			LEFT JOIN geoip_records_current geo ON g.gossip_ip = geo.ip
			LEFT JOIN dz_nodes dz ON g.gossip_ip = dz.dz_ip
			LEFT JOIN validator_stake vs ON g.pubkey = vs.node_pubkey
		)
	`

	// Get total count (with filter)
	countQuery := baseQuery + `SELECT count(*) FROM gossip_data WHERE 1=1` + whereFilter
	var total uint64
	if err := config.DB.QueryRow(ctx, countQuery, filterArgs...).Scan(&total); err != nil {
		log.Printf("GossipNodes count error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Get on_dz count (with filter)
	onDZCountQuery := baseQuery + `SELECT count(*) FROM gossip_data WHERE on_dz = true` + whereFilter
	var onDZCount uint64
	if err := config.DB.QueryRow(ctx, onDZCountQuery, filterArgs...).Scan(&onDZCount); err != nil {
		log.Printf("GossipNodes on_dz count error: %v", err)
		onDZCount = 0
	}

	// Get validator count (with filter)
	validatorCountQuery := baseQuery + `SELECT count(*) FROM gossip_data WHERE is_validator = true` + whereFilter
	var validatorCount uint64
	if err := config.DB.QueryRow(ctx, validatorCountQuery, filterArgs...).Scan(&validatorCount); err != nil {
		log.Printf("GossipNodes validator count error: %v", err)
		validatorCount = 0
	}

	orderBy := sort.OrderByClause(gossipNodeSortFields)
	query := baseQuery + `
		SELECT pubkey, gossip_ip, gossip_port, version, city, country,
			on_dz, device_code, metro_code, stake_sol, is_validator
		FROM gossip_data
		WHERE 1=1` + whereFilter + `
		` + orderBy + `
		LIMIT ? OFFSET ?
	`

	queryArgs := append(filterArgs, pagination.Limit, pagination.Offset)
	rows, err := config.DB.Query(ctx, query, queryArgs...)
	duration := time.Since(start)
	metrics.RecordClickHouseQuery(duration, err)

	if err != nil {
		log.Printf("GossipNodes query error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var nodes []GossipNodeListItem
	for rows.Next() {
		var n GossipNodeListItem
		if err := rows.Scan(
			&n.Pubkey,
			&n.GossipIP,
			&n.GossipPort,
			&n.Version,
			&n.City,
			&n.Country,
			&n.OnDZ,
			&n.DeviceCode,
			&n.MetroCode,
			&n.StakeSol,
			&n.IsValidator,
		); err != nil {
			log.Printf("GossipNodes scan error: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		nodes = append(nodes, n)
	}

	if err := rows.Err(); err != nil {
		log.Printf("GossipNodes rows error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Return empty array instead of null
	if nodes == nil {
		nodes = []GossipNodeListItem{}
	}

	response := GossipNodeListResponse{
		Items:          nodes,
		Total:          int(total),
		OnDZCount:      int(onDZCount),
		ValidatorCount: int(validatorCount),
		Limit:          pagination.Limit,
		Offset:         pagination.Offset,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("JSON encoding error: %v", err)
	}
}

type GossipNodeDetail struct {
	Pubkey      string  `json:"pubkey"`
	GossipIP    string  `json:"gossip_ip"`
	GossipPort  int32   `json:"gossip_port"`
	Version     string  `json:"version"`
	City        string  `json:"city"`
	Country     string  `json:"country"`
	OnDZ        bool    `json:"on_dz"`
	DevicePK    string  `json:"device_pk"`
	DeviceCode  string  `json:"device_code"`
	MetroPK     string  `json:"metro_pk"`
	MetroCode   string  `json:"metro_code"`
	StakeSol    float64 `json:"stake_sol"`
	IsValidator bool    `json:"is_validator"`
	VotePubkey  string  `json:"vote_pubkey"`
	InBps       float64 `json:"in_bps"`
	OutBps      float64 `json:"out_bps"`
}

func GetGossipNode(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	pubkey := chi.URLParam(r, "pubkey")
	if pubkey == "" {
		http.Error(w, "missing pubkey", http.StatusBadRequest)
		return
	}

	start := time.Now()
	query := `
		WITH dz_nodes AS (
			SELECT
				u.dz_ip,
				u.tunnel_id,
				u.device_pk,
				d.code as device_code,
				d.metro_pk,
				m.code as metro_code
			FROM dz_users_current u
			JOIN dz_devices_current d ON u.device_pk = d.pk
			LEFT JOIN dz_metros_current m ON d.metro_pk = m.pk
			WHERE u.status = 'activated'
				AND u.dz_ip IS NOT NULL
				AND u.dz_ip != ''
		),
		validator_stake AS (
			SELECT
				node_pubkey,
				vote_pubkey,
				activated_stake_lamports / 1e9 as stake_sol
			FROM solana_vote_accounts_current
			WHERE epoch_vote_account = 'true'
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
		)
		SELECT
			g.pubkey,
			COALESCE(g.gossip_ip, '') as gossip_ip,
			COALESCE(g.gossip_port, 0) as gossip_port,
			COALESCE(g.version, '') as version,
			COALESCE(geo.city, '') as city,
			COALESCE(geo.country, '') as country,
			dz.dz_ip != '' as on_dz,
			COALESCE(dz.device_pk, '') as device_pk,
			COALESCE(dz.device_code, '') as device_code,
			COALESCE(dz.metro_pk, '') as metro_pk,
			COALESCE(dz.metro_code, '') as metro_code,
			COALESCE(vs.stake_sol, 0) as stake_sol,
			vs.node_pubkey IS NOT NULL as is_validator,
			COALESCE(vs.vote_pubkey, '') as vote_pubkey,
			COALESCE(tr.in_bps, 0) as in_bps,
			COALESCE(tr.out_bps, 0) as out_bps
		FROM solana_gossip_nodes_current g
		LEFT JOIN geoip_records_current geo ON g.gossip_ip = geo.ip
		LEFT JOIN dz_nodes dz ON g.gossip_ip = dz.dz_ip
		LEFT JOIN validator_stake vs ON g.pubkey = vs.node_pubkey
		LEFT JOIN traffic_rates tr ON dz.tunnel_id = tr.user_tunnel_id
		WHERE g.pubkey = ?
	`

	var node GossipNodeDetail
	err := config.DB.QueryRow(ctx, query, pubkey).Scan(
		&node.Pubkey,
		&node.GossipIP,
		&node.GossipPort,
		&node.Version,
		&node.City,
		&node.Country,
		&node.OnDZ,
		&node.DevicePK,
		&node.DeviceCode,
		&node.MetroPK,
		&node.MetroCode,
		&node.StakeSol,
		&node.IsValidator,
		&node.VotePubkey,
		&node.InBps,
		&node.OutBps,
	)
	duration := time.Since(start)
	metrics.RecordClickHouseQuery(duration, err)

	if err != nil {
		log.Printf("GossipNode query error: %v", err)
		http.Error(w, "gossip node not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(node); err != nil {
		log.Printf("JSON encoding error: %v", err)
	}
}
