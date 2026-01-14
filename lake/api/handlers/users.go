package handlers

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

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
