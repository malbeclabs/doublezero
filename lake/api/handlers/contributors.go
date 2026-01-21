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

type ContributorListItem struct {
	PK           string `json:"pk"`
	Code         string `json:"code"`
	Name         string `json:"name"`
	DeviceCount  uint64 `json:"device_count"`
	SideADevices uint64 `json:"side_a_devices"`
	SideZDevices uint64 `json:"side_z_devices"`
	LinkCount    uint64 `json:"link_count"`
}

func GetContributors(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	pagination := ParsePagination(r, 100)
	start := time.Now()

	// Get total count
	countQuery := `SELECT count(*) FROM dz_contributors_current`
	var total uint64
	if err := config.DB.QueryRow(ctx, countQuery).Scan(&total); err != nil {
		log.Printf("Contributors count error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	query := `
		WITH device_counts AS (
			SELECT contributor_pk, count(*) as cnt
			FROM dz_devices_current
			WHERE contributor_pk IS NOT NULL
			GROUP BY contributor_pk
		),
		side_a_counts AS (
			SELECT d.contributor_pk as cpk, count(DISTINCT l.pk) as cnt
			FROM dz_links_current l
			JOIN dz_devices_current d ON l.side_a_pk = d.pk
			WHERE d.contributor_pk IS NOT NULL
			GROUP BY d.contributor_pk
		),
		side_z_counts AS (
			SELECT d.contributor_pk as cpk, count(DISTINCT l.pk) as cnt
			FROM dz_links_current l
			JOIN dz_devices_current d ON l.side_z_pk = d.pk
			WHERE d.contributor_pk IS NOT NULL
			GROUP BY d.contributor_pk
		),
		link_counts AS (
			SELECT contributor_pk, count(*) as cnt
			FROM dz_links_current
			WHERE contributor_pk IS NOT NULL
			GROUP BY contributor_pk
		)
		SELECT
			c.pk,
			c.code,
			COALESCE(c.name, '') as name,
			COALESCE(dc.cnt, 0) as device_count,
			COALESCE(sa.cnt, 0) as side_a_devices,
			COALESCE(sz.cnt, 0) as side_z_devices,
			COALESCE(lc.cnt, 0) as link_count
		FROM dz_contributors_current c
		LEFT JOIN device_counts dc ON c.pk = dc.contributor_pk
		LEFT JOIN side_a_counts sa ON c.pk = sa.cpk
		LEFT JOIN side_z_counts sz ON c.pk = sz.cpk
		LEFT JOIN link_counts lc ON c.pk = lc.contributor_pk
		ORDER BY c.code
		LIMIT ? OFFSET ?
	`

	rows, err := config.DB.Query(ctx, query, pagination.Limit, pagination.Offset)
	duration := time.Since(start)
	metrics.RecordClickHouseQuery(duration, err)

	if err != nil {
		log.Printf("Contributors query error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var contributors []ContributorListItem
	for rows.Next() {
		var c ContributorListItem
		if err := rows.Scan(
			&c.PK,
			&c.Code,
			&c.Name,
			&c.DeviceCount,
			&c.SideADevices,
			&c.SideZDevices,
			&c.LinkCount,
		); err != nil {
			log.Printf("Contributors scan error: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		contributors = append(contributors, c)
	}

	if err := rows.Err(); err != nil {
		log.Printf("Contributors rows error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Return empty array instead of null
	if contributors == nil {
		contributors = []ContributorListItem{}
	}

	response := PaginatedResponse[ContributorListItem]{
		Items:  contributors,
		Total:  int(total),
		Limit:  pagination.Limit,
		Offset: pagination.Offset,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("JSON encoding error: %v", err)
	}
}

type ContributorDetail struct {
	PK           string  `json:"pk"`
	Code         string  `json:"code"`
	Name         string  `json:"name"`
	DeviceCount  uint64  `json:"device_count"`
	SideADevices uint64  `json:"side_a_devices"`
	SideZDevices uint64  `json:"side_z_devices"`
	LinkCount    uint64  `json:"link_count"`
	UserCount    uint64  `json:"user_count"`
	InBps        float64 `json:"in_bps"`
	OutBps       float64 `json:"out_bps"`
}

func GetContributor(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	pk := chi.URLParam(r, "pk")
	if pk == "" {
		http.Error(w, "missing contributor pk", http.StatusBadRequest)
		return
	}

	start := time.Now()
	query := `
		WITH device_counts AS (
			SELECT contributor_pk, count(*) as cnt
			FROM dz_devices_current
			WHERE contributor_pk IS NOT NULL
			GROUP BY contributor_pk
		),
		side_a_counts AS (
			SELECT d.contributor_pk as cpk, count(DISTINCT l.pk) as cnt
			FROM dz_links_current l
			JOIN dz_devices_current d ON l.side_a_pk = d.pk
			WHERE d.contributor_pk IS NOT NULL
			GROUP BY d.contributor_pk
		),
		side_z_counts AS (
			SELECT d.contributor_pk as cpk, count(DISTINCT l.pk) as cnt
			FROM dz_links_current l
			JOIN dz_devices_current d ON l.side_z_pk = d.pk
			WHERE d.contributor_pk IS NOT NULL
			GROUP BY d.contributor_pk
		),
		link_counts AS (
			SELECT contributor_pk, count(*) as cnt
			FROM dz_links_current
			WHERE contributor_pk IS NOT NULL
			GROUP BY contributor_pk
		),
		user_counts AS (
			SELECT d.contributor_pk, count(*) as cnt
			FROM dz_users_current u
			JOIN dz_devices_current d ON u.device_pk = d.pk
			WHERE u.status = 'activated' AND d.contributor_pk IS NOT NULL
			GROUP BY d.contributor_pk
		),
		traffic_rates AS (
			SELECT
				d.contributor_pk,
				SUM(CASE WHEN f.delta_duration > 0 THEN f.in_octets_delta * 8 / f.delta_duration ELSE 0 END) as in_bps,
				SUM(CASE WHEN f.delta_duration > 0 THEN f.out_octets_delta * 8 / f.delta_duration ELSE 0 END) as out_bps
			FROM fact_dz_device_interface_counters f
			JOIN dz_devices_current d ON f.device_pk = d.pk
			WHERE f.event_ts > now() - INTERVAL 5 MINUTE
				AND f.user_tunnel_id IS NULL
				AND f.link_pk = ''
				AND d.contributor_pk IS NOT NULL
			GROUP BY d.contributor_pk
		)
		SELECT
			c.pk,
			c.code,
			COALESCE(c.name, '') as name,
			COALESCE(dc.cnt, 0) as device_count,
			COALESCE(sa.cnt, 0) as side_a_devices,
			COALESCE(sz.cnt, 0) as side_z_devices,
			COALESCE(lc.cnt, 0) as link_count,
			COALESCE(uc.cnt, 0) as user_count,
			COALESCE(tr.in_bps, 0) as in_bps,
			COALESCE(tr.out_bps, 0) as out_bps
		FROM dz_contributors_current c
		LEFT JOIN device_counts dc ON c.pk = dc.contributor_pk
		LEFT JOIN side_a_counts sa ON c.pk = sa.cpk
		LEFT JOIN side_z_counts sz ON c.pk = sz.cpk
		LEFT JOIN link_counts lc ON c.pk = lc.contributor_pk
		LEFT JOIN user_counts uc ON c.pk = uc.contributor_pk
		LEFT JOIN traffic_rates tr ON c.pk = tr.contributor_pk
		WHERE c.pk = ?
	`

	var contributor ContributorDetail
	err := config.DB.QueryRow(ctx, query, pk).Scan(
		&contributor.PK,
		&contributor.Code,
		&contributor.Name,
		&contributor.DeviceCount,
		&contributor.SideADevices,
		&contributor.SideZDevices,
		&contributor.LinkCount,
		&contributor.UserCount,
		&contributor.InBps,
		&contributor.OutBps,
	)
	duration := time.Since(start)
	metrics.RecordClickHouseQuery(duration, err)

	if err != nil {
		log.Printf("Contributor query error: %v", err)
		http.Error(w, "contributor not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(contributor); err != nil {
		log.Printf("JSON encoding error: %v", err)
	}
}
