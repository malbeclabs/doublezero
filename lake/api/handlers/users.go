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

type UserListItem struct {
	PK          string  `json:"pk"`
	OwnerPubkey string  `json:"owner_pubkey"`
	Status      string  `json:"status"`
	Kind        string  `json:"kind"`
	DzIP        string  `json:"dz_ip"`
	DevicePK    string  `json:"device_pk"`
	DeviceCode  string  `json:"device_code"`
	MetroCode   string  `json:"metro_code"`
	MetroName   string  `json:"metro_name"`
	InBps       float64 `json:"in_bps"`
	OutBps      float64 `json:"out_bps"`
}

func GetUsers(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	pagination := ParsePagination(r, 100)
	start := time.Now()

	// Get total count
	countQuery := `SELECT count(*) FROM dz_users_current`
	var total uint64
	if err := config.DB.QueryRow(ctx, countQuery).Scan(&total); err != nil {
		log.Printf("Users count error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	query := `
		WITH traffic_rates AS (
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
			u.pk,
			COALESCE(u.owner_pubkey, '') as owner_pubkey,
			u.status,
			COALESCE(u.kind, '') as kind,
			COALESCE(u.dz_ip, '') as dz_ip,
			COALESCE(u.device_pk, '') as device_pk,
			COALESCE(d.code, '') as device_code,
			COALESCE(m.code, '') as metro_code,
			COALESCE(m.name, '') as metro_name,
			COALESCE(tr.in_bps, 0) as in_bps,
			COALESCE(tr.out_bps, 0) as out_bps
		FROM dz_users_current u
		LEFT JOIN dz_devices_current d ON u.device_pk = d.pk
		LEFT JOIN dz_metros_current m ON d.metro_pk = m.pk
		LEFT JOIN traffic_rates tr ON u.tunnel_id = tr.user_tunnel_id
		ORDER BY u.owner_pubkey
		LIMIT ? OFFSET ?
	`

	rows, err := config.DB.Query(ctx, query, pagination.Limit, pagination.Offset)
	duration := time.Since(start)
	metrics.RecordClickHouseQuery(duration, err)

	if err != nil {
		log.Printf("Users query error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var users []UserListItem
	for rows.Next() {
		var u UserListItem
		if err := rows.Scan(
			&u.PK,
			&u.OwnerPubkey,
			&u.Status,
			&u.Kind,
			&u.DzIP,
			&u.DevicePK,
			&u.DeviceCode,
			&u.MetroCode,
			&u.MetroName,
			&u.InBps,
			&u.OutBps,
		); err != nil {
			log.Printf("Users scan error: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		users = append(users, u)
	}

	if err := rows.Err(); err != nil {
		log.Printf("Users rows error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Return empty array instead of null
	if users == nil {
		users = []UserListItem{}
	}

	response := PaginatedResponse[UserListItem]{
		Items:  users,
		Total:  int(total),
		Limit:  pagination.Limit,
		Offset: pagination.Offset,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("JSON encoding error: %v", err)
	}
}

type UserDetail struct {
	PK             string  `json:"pk"`
	OwnerPubkey    string  `json:"owner_pubkey"`
	Status         string  `json:"status"`
	Kind           string  `json:"kind"`
	DzIP           string  `json:"dz_ip"`
	DevicePK       string  `json:"device_pk"`
	DeviceCode     string  `json:"device_code"`
	MetroPK        string  `json:"metro_pk"`
	MetroCode      string  `json:"metro_code"`
	MetroName      string  `json:"metro_name"`
	ContributorPK  string  `json:"contributor_pk"`
	ContributorCode string `json:"contributor_code"`
	InBps          float64 `json:"in_bps"`
	OutBps         float64 `json:"out_bps"`
	IsValidator    bool    `json:"is_validator"`
	VotePubkey     string  `json:"vote_pubkey"`
	StakeSol       float64 `json:"stake_sol"`
}

func GetUser(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	pk := chi.URLParam(r, "pk")
	if pk == "" {
		http.Error(w, "missing user pk", http.StatusBadRequest)
		return
	}

	start := time.Now()
	query := `
		WITH traffic_rates AS (
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
		validator_info AS (
			SELECT
				g.gossip_ip,
				v.vote_pubkey,
				v.activated_stake_lamports / 1e9 as stake_sol
			FROM solana_gossip_nodes_current g
			JOIN solana_vote_accounts_current v ON g.pubkey = v.node_pubkey
			WHERE v.epoch_vote_account = 'true'
		)
		SELECT
			u.pk,
			COALESCE(u.owner_pubkey, '') as owner_pubkey,
			u.status,
			COALESCE(u.kind, '') as kind,
			COALESCE(u.dz_ip, '') as dz_ip,
			COALESCE(u.device_pk, '') as device_pk,
			COALESCE(d.code, '') as device_code,
			COALESCE(d.metro_pk, '') as metro_pk,
			COALESCE(m.code, '') as metro_code,
			COALESCE(m.name, '') as metro_name,
			COALESCE(d.contributor_pk, '') as contributor_pk,
			COALESCE(c.code, '') as contributor_code,
			COALESCE(tr.in_bps, 0) as in_bps,
			COALESCE(tr.out_bps, 0) as out_bps,
			vi.vote_pubkey IS NOT NULL as is_validator,
			COALESCE(vi.vote_pubkey, '') as vote_pubkey,
			COALESCE(vi.stake_sol, 0) as stake_sol
		FROM dz_users_current u
		LEFT JOIN dz_devices_current d ON u.device_pk = d.pk
		LEFT JOIN dz_metros_current m ON d.metro_pk = m.pk
		LEFT JOIN dz_contributors_current c ON d.contributor_pk = c.pk
		LEFT JOIN traffic_rates tr ON u.tunnel_id = tr.user_tunnel_id
		LEFT JOIN validator_info vi ON u.dz_ip = vi.gossip_ip
		WHERE u.pk = ?
	`

	var user UserDetail
	err := config.DB.QueryRow(ctx, query, pk).Scan(
		&user.PK,
		&user.OwnerPubkey,
		&user.Status,
		&user.Kind,
		&user.DzIP,
		&user.DevicePK,
		&user.DeviceCode,
		&user.MetroPK,
		&user.MetroCode,
		&user.MetroName,
		&user.ContributorPK,
		&user.ContributorCode,
		&user.InBps,
		&user.OutBps,
		&user.IsValidator,
		&user.VotePubkey,
		&user.StakeSol,
	)
	duration := time.Since(start)
	metrics.RecordClickHouseQuery(duration, err)

	if err != nil {
		log.Printf("User query error: %v", err)
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(user); err != nil {
		log.Printf("JSON encoding error: %v", err)
	}
}
