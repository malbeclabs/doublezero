package handlers

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/malbeclabs/doublezero/lake/api/config"
	"github.com/malbeclabs/doublezero/lake/api/handlers/dberror"
	"github.com/malbeclabs/doublezero/lake/api/metrics"
)

type MetroListItem struct {
	PK          string  `json:"pk"`
	Code        string  `json:"code"`
	Name        string  `json:"name"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
	DeviceCount uint64  `json:"device_count"`
	UserCount   uint64  `json:"user_count"`
}

func GetMetros(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	pagination := ParsePagination(r, 100)
	start := time.Now()

	// Get total count
	countQuery := `SELECT count(*) FROM dz_metros_current`
	var total uint64
	if err := config.DB.QueryRow(ctx, countQuery).Scan(&total); err != nil {
		log.Printf("Metros count error: %v", err)
		http.Error(w, dberror.UserMessage(err), http.StatusInternalServerError)
		return
	}

	query := `
		WITH device_counts AS (
			SELECT metro_pk, count(*) as device_count
			FROM dz_devices_current
			WHERE metro_pk IS NOT NULL
			GROUP BY metro_pk
		),
		user_counts AS (
			SELECT d.metro_pk, count(*) as user_count
			FROM dz_users_current u
			JOIN dz_devices_current d ON u.device_pk = d.pk
			WHERE u.status = 'activated' AND d.metro_pk IS NOT NULL
			GROUP BY d.metro_pk
		)
		SELECT
			m.pk,
			m.code,
			COALESCE(m.name, '') as name,
			COALESCE(m.latitude, 0) as latitude,
			COALESCE(m.longitude, 0) as longitude,
			COALESCE(dc.device_count, 0) as device_count,
			COALESCE(uc.user_count, 0) as user_count
		FROM dz_metros_current m
		LEFT JOIN device_counts dc ON m.pk = dc.metro_pk
		LEFT JOIN user_counts uc ON m.pk = uc.metro_pk
		ORDER BY m.code
		LIMIT ? OFFSET ?
	`

	rows, err := config.DB.Query(ctx, query, pagination.Limit, pagination.Offset)
	duration := time.Since(start)
	metrics.RecordClickHouseQuery(duration, err)

	if err != nil {
		log.Printf("Metros query error: %v", err)
		http.Error(w, dberror.UserMessage(err), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var metros []MetroListItem
	for rows.Next() {
		var m MetroListItem
		if err := rows.Scan(
			&m.PK,
			&m.Code,
			&m.Name,
			&m.Latitude,
			&m.Longitude,
			&m.DeviceCount,
			&m.UserCount,
		); err != nil {
			log.Printf("Metros scan error: %v", err)
			http.Error(w, dberror.UserMessage(err), http.StatusInternalServerError)
			return
		}
		metros = append(metros, m)
	}

	if err := rows.Err(); err != nil {
		log.Printf("Metros rows error: %v", err)
		http.Error(w, dberror.UserMessage(err), http.StatusInternalServerError)
		return
	}

	// Return empty array instead of null
	if metros == nil {
		metros = []MetroListItem{}
	}

	response := PaginatedResponse[MetroListItem]{
		Items:  metros,
		Total:  int(total),
		Limit:  pagination.Limit,
		Offset: pagination.Offset,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("JSON encoding error: %v", err)
	}
}

type MetroDetail struct {
	PK             string  `json:"pk"`
	Code           string  `json:"code"`
	Name           string  `json:"name"`
	Latitude       float64 `json:"latitude"`
	Longitude      float64 `json:"longitude"`
	DeviceCount    uint64  `json:"device_count"`
	UserCount      uint64  `json:"user_count"`
	ValidatorCount uint64  `json:"validator_count"`
	StakeSol       float64 `json:"stake_sol"`
	InBps          float64 `json:"in_bps"`
	OutBps         float64 `json:"out_bps"`
}

func GetMetro(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	pk := chi.URLParam(r, "pk")
	if pk == "" {
		http.Error(w, "missing metro pk", http.StatusBadRequest)
		return
	}

	start := time.Now()
	query := `
		WITH device_counts AS (
			SELECT metro_pk, count(*) as device_count
			FROM dz_devices_current
			WHERE metro_pk IS NOT NULL
			GROUP BY metro_pk
		),
		user_counts AS (
			SELECT d.metro_pk, count(*) as user_count
			FROM dz_users_current u
			JOIN dz_devices_current d ON u.device_pk = d.pk
			WHERE u.status = 'activated' AND d.metro_pk IS NOT NULL
			GROUP BY d.metro_pk
		),
		validator_stats AS (
			SELECT
				d.metro_pk,
				count(DISTINCT v.vote_pubkey) as validator_count,
				sum(v.activated_stake_lamports) / 1e9 as stake_sol
			FROM dz_users_current u
			JOIN dz_devices_current d ON u.device_pk = d.pk
			JOIN solana_gossip_nodes_current g ON u.dz_ip = g.gossip_ip
			JOIN solana_vote_accounts_current v ON g.pubkey = v.node_pubkey
			WHERE u.status = 'activated' AND v.epoch_vote_account = 'true' AND d.metro_pk IS NOT NULL
			GROUP BY d.metro_pk
		),
		traffic_rates AS (
			SELECT
				d.metro_pk,
				SUM(CASE WHEN f.delta_duration > 0 THEN f.in_octets_delta * 8 / f.delta_duration ELSE 0 END) as in_bps,
				SUM(CASE WHEN f.delta_duration > 0 THEN f.out_octets_delta * 8 / f.delta_duration ELSE 0 END) as out_bps
			FROM fact_dz_device_interface_counters f
			JOIN dz_devices_current d ON f.device_pk = d.pk
			WHERE f.event_ts > now() - INTERVAL 5 MINUTE
				AND f.user_tunnel_id IS NULL
				AND f.link_pk = ''
				AND d.metro_pk IS NOT NULL
			GROUP BY d.metro_pk
		)
		SELECT
			m.pk,
			m.code,
			COALESCE(m.name, '') as name,
			COALESCE(m.latitude, 0) as latitude,
			COALESCE(m.longitude, 0) as longitude,
			COALESCE(dc.device_count, 0) as device_count,
			COALESCE(uc.user_count, 0) as user_count,
			COALESCE(vs.validator_count, 0) as validator_count,
			COALESCE(vs.stake_sol, 0) as stake_sol,
			COALESCE(tr.in_bps, 0) as in_bps,
			COALESCE(tr.out_bps, 0) as out_bps
		FROM dz_metros_current m
		LEFT JOIN device_counts dc ON m.pk = dc.metro_pk
		LEFT JOIN user_counts uc ON m.pk = uc.metro_pk
		LEFT JOIN validator_stats vs ON m.pk = vs.metro_pk
		LEFT JOIN traffic_rates tr ON m.pk = tr.metro_pk
		WHERE m.pk = ?
	`

	var metro MetroDetail
	err := config.DB.QueryRow(ctx, query, pk).Scan(
		&metro.PK,
		&metro.Code,
		&metro.Name,
		&metro.Latitude,
		&metro.Longitude,
		&metro.DeviceCount,
		&metro.UserCount,
		&metro.ValidatorCount,
		&metro.StakeSol,
		&metro.InBps,
		&metro.OutBps,
	)
	duration := time.Since(start)
	metrics.RecordClickHouseQuery(duration, err)

	if err != nil {
		log.Printf("Metro query error: %v", err)
		http.Error(w, "metro not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(metro); err != nil {
		log.Printf("JSON encoding error: %v", err)
	}
}
