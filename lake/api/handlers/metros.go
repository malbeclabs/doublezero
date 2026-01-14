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
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		metros = append(metros, m)
	}

	if err := rows.Err(); err != nil {
		log.Printf("Metros rows error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
