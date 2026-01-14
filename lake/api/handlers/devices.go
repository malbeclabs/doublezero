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

type DeviceListItem struct {
	PK              string  `json:"pk"`
	Code            string  `json:"code"`
	Status          string  `json:"status"`
	DeviceType      string  `json:"device_type"`
	ContributorPK   string  `json:"contributor_pk"`
	ContributorCode string  `json:"contributor_code"`
	MetroPK         string  `json:"metro_pk"`
	MetroCode       string  `json:"metro_code"`
	PublicIP        string  `json:"public_ip"`
	MaxUsers        int32   `json:"max_users"`
	CurrentUsers    uint64  `json:"current_users"`
	InBps           float64 `json:"in_bps"`
	OutBps          float64 `json:"out_bps"`
	PeakInBps       float64 `json:"peak_in_bps"`
	PeakOutBps      float64 `json:"peak_out_bps"`
}

func GetDevices(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	pagination := ParsePagination(r, 100)
	start := time.Now()

	// Get total count
	countQuery := `SELECT count(*) FROM dz_devices_current`
	var total uint64
	if err := config.DB.QueryRow(ctx, countQuery).Scan(&total); err != nil {
		log.Printf("Devices count error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	query := `
		WITH user_counts AS (
			SELECT device_pk, count(*) as user_count
			FROM dz_users_current
			WHERE status = 'activated'
			GROUP BY device_pk
		),
		traffic_rates AS (
			SELECT
				device_pk,
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
				AND user_tunnel_id IS NULL
				AND link_pk = ''
			GROUP BY device_pk
		),
		peak_rates AS (
			SELECT
				device_pk,
				max(in_octets_delta * 8 / nullIf(delta_duration, 0)) as peak_in_bps,
				max(out_octets_delta * 8 / nullIf(delta_duration, 0)) as peak_out_bps
			FROM fact_dz_device_interface_counters
			WHERE event_ts > now() - INTERVAL 1 HOUR
				AND user_tunnel_id IS NULL
				AND link_pk = ''
				AND delta_duration > 0
			GROUP BY device_pk
		)
		SELECT
			d.pk,
			d.code,
			d.status,
			d.device_type,
			COALESCE(d.contributor_pk, '') as contributor_pk,
			COALESCE(c.code, '') as contributor_code,
			COALESCE(d.metro_pk, '') as metro_pk,
			COALESCE(m.code, '') as metro_code,
			COALESCE(d.public_ip, '') as public_ip,
			COALESCE(d.max_users, 0) as max_users,
			COALESCE(uc.user_count, 0) as current_users,
			COALESCE(tr.in_bps, 0) as in_bps,
			COALESCE(tr.out_bps, 0) as out_bps,
			COALESCE(pr.peak_in_bps, 0) as peak_in_bps,
			COALESCE(pr.peak_out_bps, 0) as peak_out_bps
		FROM dz_devices_current d
		LEFT JOIN dz_contributors_current c ON d.contributor_pk = c.pk
		LEFT JOIN dz_metros_current m ON d.metro_pk = m.pk
		LEFT JOIN user_counts uc ON d.pk = uc.device_pk
		LEFT JOIN traffic_rates tr ON d.pk = tr.device_pk
		LEFT JOIN peak_rates pr ON d.pk = pr.device_pk
		ORDER BY d.code
		LIMIT ? OFFSET ?
	`

	rows, err := config.DB.Query(ctx, query, pagination.Limit, pagination.Offset)
	duration := time.Since(start)
	metrics.RecordClickHouseQuery(duration, err)

	if err != nil {
		log.Printf("Devices query error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var devices []DeviceListItem
	for rows.Next() {
		var d DeviceListItem
		if err := rows.Scan(
			&d.PK,
			&d.Code,
			&d.Status,
			&d.DeviceType,
			&d.ContributorPK,
			&d.ContributorCode,
			&d.MetroPK,
			&d.MetroCode,
			&d.PublicIP,
			&d.MaxUsers,
			&d.CurrentUsers,
			&d.InBps,
			&d.OutBps,
			&d.PeakInBps,
			&d.PeakOutBps,
		); err != nil {
			log.Printf("Devices scan error: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		devices = append(devices, d)
	}

	if err := rows.Err(); err != nil {
		log.Printf("Devices rows error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Return empty array instead of null
	if devices == nil {
		devices = []DeviceListItem{}
	}

	response := PaginatedResponse[DeviceListItem]{
		Items:  devices,
		Total:  int(total),
		Limit:  pagination.Limit,
		Offset: pagination.Offset,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("JSON encoding error: %v", err)
	}
}

type DeviceDetail struct {
	PK              string  `json:"pk"`
	Code            string  `json:"code"`
	Status          string  `json:"status"`
	DeviceType      string  `json:"device_type"`
	ContributorPK   string  `json:"contributor_pk"`
	ContributorCode string  `json:"contributor_code"`
	MetroPK         string  `json:"metro_pk"`
	MetroCode       string  `json:"metro_code"`
	MetroName       string  `json:"metro_name"`
	PublicIP        string  `json:"public_ip"`
	MaxUsers        int32   `json:"max_users"`
	CurrentUsers    uint64  `json:"current_users"`
	InBps           float64 `json:"in_bps"`
	OutBps          float64 `json:"out_bps"`
	PeakInBps       float64 `json:"peak_in_bps"`
	PeakOutBps      float64 `json:"peak_out_bps"`
	ValidatorCount  uint64  `json:"validator_count"`
	StakeSol        float64 `json:"stake_sol"`
}

func GetDevice(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	pk := chi.URLParam(r, "pk")
	if pk == "" {
		http.Error(w, "missing device pk", http.StatusBadRequest)
		return
	}

	start := time.Now()
	query := `
		WITH user_counts AS (
			SELECT device_pk, count(*) as user_count
			FROM dz_users_current
			WHERE status = 'activated'
			GROUP BY device_pk
		),
		traffic_rates AS (
			SELECT
				device_pk,
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
				AND user_tunnel_id IS NULL
				AND link_pk = ''
			GROUP BY device_pk
		),
		peak_rates AS (
			SELECT
				device_pk,
				max(in_octets_delta * 8 / nullIf(delta_duration, 0)) as peak_in_bps,
				max(out_octets_delta * 8 / nullIf(delta_duration, 0)) as peak_out_bps
			FROM fact_dz_device_interface_counters
			WHERE event_ts > now() - INTERVAL 1 HOUR
				AND user_tunnel_id IS NULL
				AND link_pk = ''
				AND delta_duration > 0
			GROUP BY device_pk
		),
		validator_stats AS (
			SELECT
				u.device_pk,
				count(DISTINCT v.vote_pubkey) as validator_count,
				sum(v.activated_stake_lamports) / 1e9 as stake_sol
			FROM dz_users_current u
			JOIN solana_gossip_nodes_current g ON u.dz_ip = g.gossip_ip
			JOIN solana_vote_accounts_current v ON g.pubkey = v.node_pubkey
			WHERE u.status = 'activated' AND v.epoch_vote_account = 'true'
			GROUP BY u.device_pk
		)
		SELECT
			d.pk,
			d.code,
			d.status,
			d.device_type,
			COALESCE(d.contributor_pk, '') as contributor_pk,
			COALESCE(c.code, '') as contributor_code,
			COALESCE(d.metro_pk, '') as metro_pk,
			COALESCE(m.code, '') as metro_code,
			COALESCE(m.name, '') as metro_name,
			COALESCE(d.public_ip, '') as public_ip,
			COALESCE(d.max_users, 0) as max_users,
			COALESCE(uc.user_count, 0) as current_users,
			COALESCE(tr.in_bps, 0) as in_bps,
			COALESCE(tr.out_bps, 0) as out_bps,
			COALESCE(pr.peak_in_bps, 0) as peak_in_bps,
			COALESCE(pr.peak_out_bps, 0) as peak_out_bps,
			COALESCE(vs.validator_count, 0) as validator_count,
			COALESCE(vs.stake_sol, 0) as stake_sol
		FROM dz_devices_current d
		LEFT JOIN dz_contributors_current c ON d.contributor_pk = c.pk
		LEFT JOIN dz_metros_current m ON d.metro_pk = m.pk
		LEFT JOIN user_counts uc ON d.pk = uc.device_pk
		LEFT JOIN traffic_rates tr ON d.pk = tr.device_pk
		LEFT JOIN peak_rates pr ON d.pk = pr.device_pk
		LEFT JOIN validator_stats vs ON d.pk = vs.device_pk
		WHERE d.pk = ?
	`

	var device DeviceDetail
	err := config.DB.QueryRow(ctx, query, pk).Scan(
		&device.PK,
		&device.Code,
		&device.Status,
		&device.DeviceType,
		&device.ContributorPK,
		&device.ContributorCode,
		&device.MetroPK,
		&device.MetroCode,
		&device.MetroName,
		&device.PublicIP,
		&device.MaxUsers,
		&device.CurrentUsers,
		&device.InBps,
		&device.OutBps,
		&device.PeakInBps,
		&device.PeakOutBps,
		&device.ValidatorCount,
		&device.StakeSol,
	)
	duration := time.Since(start)
	metrics.RecordClickHouseQuery(duration, err)

	if err != nil {
		log.Printf("Device query error: %v", err)
		http.Error(w, "device not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(device); err != nil {
		log.Printf("JSON encoding error: %v", err)
	}
}
