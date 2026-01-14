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

type LinkListItem struct {
	PK              string  `json:"pk"`
	Code            string  `json:"code"`
	Status          string  `json:"status"`
	LinkType        string  `json:"link_type"`
	BandwidthBps    int64   `json:"bandwidth_bps"`
	SideAPK         string  `json:"side_a_pk"`
	SideACode       string  `json:"side_a_code"`
	SideAMetro      string  `json:"side_a_metro"`
	SideZPK         string  `json:"side_z_pk"`
	SideZCode       string  `json:"side_z_code"`
	SideZMetro      string  `json:"side_z_metro"`
	ContributorPK   string  `json:"contributor_pk"`
	ContributorCode string  `json:"contributor_code"`
	InBps           float64 `json:"in_bps"`
	OutBps          float64 `json:"out_bps"`
	UtilizationIn   float64 `json:"utilization_in"`
	UtilizationOut  float64 `json:"utilization_out"`
	LatencyUs       float64 `json:"latency_us"`
	JitterUs        float64 `json:"jitter_us"`
	LossPercent     float64 `json:"loss_percent"`
}

func GetLinks(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	pagination := ParsePagination(r, 100)
	start := time.Now()

	// Get total count
	countQuery := `SELECT count(*) FROM dz_links_current`
	var total uint64
	if err := config.DB.QueryRow(ctx, countQuery).Scan(&total); err != nil {
		log.Printf("Links count error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	query := `
		WITH traffic_rates AS (
			SELECT
				link_pk,
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
				AND link_pk != ''
			GROUP BY link_pk
		),
		latency_stats AS (
			SELECT
				link_pk,
				avg(rtt_us) as avg_rtt_us,
				avg(abs(ipdv_us)) as avg_jitter_us,
				countIf(loss) * 100.0 / count(*) as loss_percent
			FROM fact_dz_device_link_latency
			WHERE event_ts > now() - INTERVAL 3 HOUR
			GROUP BY link_pk
		)
		SELECT
			l.pk,
			l.code,
			l.status,
			l.link_type,
			COALESCE(l.bandwidth_bps, 0) as bandwidth_bps,
			COALESCE(l.side_a_pk, '') as side_a_pk,
			COALESCE(da.code, '') as side_a_code,
			COALESCE(ma.code, '') as side_a_metro,
			COALESCE(l.side_z_pk, '') as side_z_pk,
			COALESCE(dz.code, '') as side_z_code,
			COALESCE(mz.code, '') as side_z_metro,
			COALESCE(l.contributor_pk, '') as contributor_pk,
			COALESCE(c.code, '') as contributor_code,
			COALESCE(tr.in_bps, 0) as in_bps,
			COALESCE(tr.out_bps, 0) as out_bps,
			CASE WHEN l.bandwidth_bps > 0 THEN COALESCE(tr.in_bps, 0) * 100.0 / l.bandwidth_bps ELSE 0 END as utilization_in,
			CASE WHEN l.bandwidth_bps > 0 THEN COALESCE(tr.out_bps, 0) * 100.0 / l.bandwidth_bps ELSE 0 END as utilization_out,
			COALESCE(ls.avg_rtt_us, 0) as latency_us,
			COALESCE(ls.avg_jitter_us, 0) as jitter_us,
			COALESCE(ls.loss_percent, 0) as loss_percent
		FROM dz_links_current l
		LEFT JOIN dz_devices_current da ON l.side_a_pk = da.pk
		LEFT JOIN dz_metros_current ma ON da.metro_pk = ma.pk
		LEFT JOIN dz_devices_current dz ON l.side_z_pk = dz.pk
		LEFT JOIN dz_metros_current mz ON dz.metro_pk = mz.pk
		LEFT JOIN dz_contributors_current c ON l.contributor_pk = c.pk
		LEFT JOIN traffic_rates tr ON l.pk = tr.link_pk
		LEFT JOIN latency_stats ls ON l.pk = ls.link_pk
		ORDER BY l.code
		LIMIT ? OFFSET ?
	`

	rows, err := config.DB.Query(ctx, query, pagination.Limit, pagination.Offset)
	duration := time.Since(start)
	metrics.RecordClickHouseQuery(duration, err)

	if err != nil {
		log.Printf("Links query error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var links []LinkListItem
	for rows.Next() {
		var l LinkListItem
		if err := rows.Scan(
			&l.PK,
			&l.Code,
			&l.Status,
			&l.LinkType,
			&l.BandwidthBps,
			&l.SideAPK,
			&l.SideACode,
			&l.SideAMetro,
			&l.SideZPK,
			&l.SideZCode,
			&l.SideZMetro,
			&l.ContributorPK,
			&l.ContributorCode,
			&l.InBps,
			&l.OutBps,
			&l.UtilizationIn,
			&l.UtilizationOut,
			&l.LatencyUs,
			&l.JitterUs,
			&l.LossPercent,
		); err != nil {
			log.Printf("Links scan error: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		links = append(links, l)
	}

	if err := rows.Err(); err != nil {
		log.Printf("Links rows error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Return empty array instead of null
	if links == nil {
		links = []LinkListItem{}
	}

	response := PaginatedResponse[LinkListItem]{
		Items:  links,
		Total:  int(total),
		Limit:  pagination.Limit,
		Offset: pagination.Offset,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("JSON encoding error: %v", err)
	}
}
