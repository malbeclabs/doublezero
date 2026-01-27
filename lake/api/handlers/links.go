package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
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
				AND delta_duration > 0
				AND in_octets_delta >= 0
				AND out_octets_delta >= 0
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

type LinkDetail struct {
	PK              string  `json:"pk"`
	Code            string  `json:"code"`
	Status          string  `json:"status"`
	LinkType        string  `json:"link_type"`
	BandwidthBps    int64   `json:"bandwidth_bps"`
	SideAPK         string  `json:"side_a_pk"`
	SideACode       string  `json:"side_a_code"`
	SideAMetro      string  `json:"side_a_metro"`
	SideAIfaceName  string  `json:"side_a_iface_name"`
	SideAIP         string  `json:"side_a_ip"`
	SideZPK         string  `json:"side_z_pk"`
	SideZCode       string  `json:"side_z_code"`
	SideZMetro      string  `json:"side_z_metro"`
	SideZIfaceName  string  `json:"side_z_iface_name"`
	SideZIP         string  `json:"side_z_ip"`
	ContributorPK   string  `json:"contributor_pk"`
	ContributorCode string  `json:"contributor_code"`
	InBps           float64 `json:"in_bps"`
	OutBps          float64 `json:"out_bps"`
	UtilizationIn   float64 `json:"utilization_in"`
	UtilizationOut  float64 `json:"utilization_out"`
	LatencyUs       float64 `json:"latency_us"`
	JitterUs        float64 `json:"jitter_us"`
	LatencyAtoZUs   float64 `json:"latency_a_to_z_us"`
	JitterAtoZUs    float64 `json:"jitter_a_to_z_us"`
	LatencyZtoAUs   float64 `json:"latency_z_to_a_us"`
	JitterZtoAUs    float64 `json:"jitter_z_to_a_us"`
	LossPercent     float64 `json:"loss_percent"`
	PeakInBps       float64 `json:"peak_in_bps"`
	PeakOutBps      float64 `json:"peak_out_bps"`
}

// TopologyLinkHealth represents the SLA health status of a link for topology overlay
type TopologyLinkHealth struct {
	LinkPK         string  `json:"link_pk"`
	SideAPK        string  `json:"side_a_pk"`
	SideACode      string  `json:"side_a_code"`
	SideZPK        string  `json:"side_z_pk"`
	SideZCode      string  `json:"side_z_code"`
	AvgRttUs       float64 `json:"avg_rtt_us"`
	P95RttUs       float64 `json:"p95_rtt_us"`
	CommittedRttNs int64   `json:"committed_rtt_ns"`
	LossPct        float64 `json:"loss_pct"`
	ExceedsCommit  bool    `json:"exceeds_commit"`
	HasPacketLoss  bool    `json:"has_packet_loss"`
	IsDark         bool    `json:"is_dark"`
	SlaStatus      string  `json:"sla_status"` // "healthy", "warning", "critical", "unknown"
	SlaRatio       float64 `json:"sla_ratio"`  // measured / committed (0 if no commitment)
}

type TopologyLinkHealthResponse struct {
	Links         []TopologyLinkHealth `json:"links"`
	TotalLinks    int                  `json:"total_links"`
	HealthyCount  int                  `json:"healthy_count"`
	WarningCount  int                  `json:"warning_count"`
	CriticalCount int                  `json:"critical_count"`
	UnknownCount  int                  `json:"unknown_count"`
}

func GetLinkHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	start := time.Now()
	query := `
		SELECT
			h.pk AS link_pk,
			l.side_a_pk,
			COALESCE(da.code, '') AS side_a_code,
			l.side_z_pk,
			COALESCE(dz.code, '') AS side_z_code,
			h.avg_rtt_us,
			h.p95_rtt_us,
			h.committed_rtt_ns,
			h.loss_pct,
			toUInt8(h.exceeds_committed_rtt) AS exceeds_committed_rtt,
			toUInt8(h.has_packet_loss) AS has_packet_loss,
			toUInt8(h.is_dark) AS is_dark
		FROM dz_links_health_current h
		JOIN dz_links_current l ON h.pk = l.pk
		LEFT JOIN dz_devices_current da ON l.side_a_pk = da.pk
		LEFT JOIN dz_devices_current dz ON l.side_z_pk = dz.pk
		WHERE l.side_a_pk != '' AND l.side_z_pk != ''
	`

	rows, err := config.DB.Query(ctx, query)
	duration := time.Since(start)
	metrics.RecordClickHouseQuery(duration, err)

	if err != nil {
		log.Printf("Link health query error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var links []TopologyLinkHealth
	healthyCount, warningCount, criticalCount, unknownCount := 0, 0, 0, 0

	for rows.Next() {
		var lh TopologyLinkHealth
		var exceedsCommit, hasPacketLoss, isDark uint8
		if err := rows.Scan(
			&lh.LinkPK,
			&lh.SideAPK,
			&lh.SideACode,
			&lh.SideZPK,
			&lh.SideZCode,
			&lh.AvgRttUs,
			&lh.P95RttUs,
			&lh.CommittedRttNs,
			&lh.LossPct,
			&exceedsCommit,
			&hasPacketLoss,
			&isDark,
		); err != nil {
			log.Printf("Link health scan error: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		lh.ExceedsCommit = exceedsCommit != 0
		lh.HasPacketLoss = hasPacketLoss != 0
		lh.IsDark = isDark != 0

		// Sanitize NaN/Inf values from ClickHouse
		if math.IsNaN(lh.AvgRttUs) || math.IsInf(lh.AvgRttUs, 0) {
			lh.AvgRttUs = 0
		}
		if math.IsNaN(lh.P95RttUs) || math.IsInf(lh.P95RttUs, 0) {
			lh.P95RttUs = 0
		}
		if math.IsNaN(lh.LossPct) || math.IsInf(lh.LossPct, 0) {
			lh.LossPct = 0
		}

		// Calculate SLA status
		if lh.IsDark || lh.CommittedRttNs == 0 {
			lh.SlaStatus = "unknown"
			lh.SlaRatio = 0
			unknownCount++
		} else {
			committedUs := float64(lh.CommittedRttNs) / 1000.0
			lh.SlaRatio = lh.AvgRttUs / committedUs
			// Sanitize SlaRatio as well
			if math.IsNaN(lh.SlaRatio) || math.IsInf(lh.SlaRatio, 0) {
				lh.SlaRatio = 0
			}

			// Thresholds:
			// - Latency: healthy < 150%, warning 150-200%, critical > 200%
			// - Packet loss: warning > 0.1%, critical > 10%
			if lh.LossPct > 10.0 || lh.SlaRatio >= 2.0 {
				lh.SlaStatus = "critical"
				criticalCount++
			} else if lh.LossPct > 0.1 || lh.SlaRatio >= 1.5 {
				lh.SlaStatus = "warning"
				warningCount++
			} else {
				lh.SlaStatus = "healthy"
				healthyCount++
			}
		}

		links = append(links, lh)
	}

	if err := rows.Err(); err != nil {
		log.Printf("Link health rows error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if links == nil {
		links = []TopologyLinkHealth{}
	}

	response := TopologyLinkHealthResponse{
		Links:         links,
		TotalLinks:    len(links),
		HealthyCount:  healthyCount,
		WarningCount:  warningCount,
		CriticalCount: criticalCount,
		UnknownCount:  unknownCount,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("JSON encoding error: %v", err)
	}
}

func GetLink(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	pk := chi.URLParam(r, "pk")
	if pk == "" {
		http.Error(w, "missing link pk", http.StatusBadRequest)
		return
	}

	start := time.Now()
	query := linkDetailQuery(linkDetailSelectsDefault, true)

	var link LinkDetail
	err := config.DB.QueryRow(ctx, query, pk).Scan(
		&link.PK,
		&link.Code,
		&link.Status,
		&link.LinkType,
		&link.BandwidthBps,
		&link.SideAPK,
		&link.SideACode,
		&link.SideAMetro,
		&link.SideAIfaceName,
		&link.SideAIP,
		&link.SideZPK,
		&link.SideZCode,
		&link.SideZMetro,
		&link.SideZIfaceName,
		&link.SideZIP,
		&link.ContributorPK,
		&link.ContributorCode,
		&link.InBps,
		&link.OutBps,
		&link.UtilizationIn,
		&link.UtilizationOut,
		&link.LatencyUs,
		&link.JitterUs,
		&link.LatencyAtoZUs,
		&link.JitterAtoZUs,
		&link.LatencyZtoAUs,
		&link.JitterZtoAUs,
		&link.LossPercent,
		&link.PeakInBps,
		&link.PeakOutBps,
	)
	duration := time.Since(start)
	metrics.RecordClickHouseQuery(duration, err)

	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		missingIP, missingIface, missingDirection := detectMissingLinkColumns(err)
		if missingIP || missingIface || missingDirection {
			fallbackSelects := linkDetailSelectsNoIP
			if missingIface {
				fallbackSelects = linkDetailSelectsNoIfaceOrIP
			}
			log.Printf("Link query missing columns (pk=%s). Retrying with fallback.", pk)
			start = time.Now()
			fallbackQuery := linkDetailQuery(fallbackSelects, !missingDirection)
			err = config.DB.QueryRow(ctx, fallbackQuery, pk).Scan(
				&link.PK,
				&link.Code,
				&link.Status,
				&link.LinkType,
				&link.BandwidthBps,
				&link.SideAPK,
				&link.SideACode,
				&link.SideAMetro,
				&link.SideAIfaceName,
				&link.SideAIP,
				&link.SideZPK,
				&link.SideZCode,
				&link.SideZMetro,
				&link.SideZIfaceName,
				&link.SideZIP,
				&link.ContributorPK,
				&link.ContributorCode,
				&link.InBps,
				&link.OutBps,
				&link.UtilizationIn,
				&link.UtilizationOut,
				&link.LatencyUs,
				&link.JitterUs,
				&link.LatencyAtoZUs,
				&link.JitterAtoZUs,
				&link.LatencyZtoAUs,
				&link.JitterZtoAUs,
				&link.LossPercent,
				&link.PeakInBps,
				&link.PeakOutBps,
			)
			duration = time.Since(start)
			metrics.RecordClickHouseQuery(duration, err)
		}
	}

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			log.Printf("Link query error: %v", err)
			http.Error(w, "link not found", http.StatusNotFound)
			return
		}
		log.Printf("Link query error: %v", err)
		http.Error(w, "failed to fetch link", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(link); err != nil {
		log.Printf("JSON encoding error: %v", err)
	}
}

type linkDetailSelects struct {
	SideAIfaceName string
	SideAIP        string
	SideZIfaceName string
	SideZIP        string
}

var linkDetailSelectsDefault = linkDetailSelects{
	SideAIfaceName: "COALESCE(l.side_a_iface_name, '')",
	SideAIP:        "COALESCE(l.side_a_ip, '')",
	SideZIfaceName: "COALESCE(l.side_z_iface_name, '')",
	SideZIP:        "COALESCE(l.side_z_ip, '')",
}

var linkDetailSelectsNoIP = linkDetailSelects{
	SideAIfaceName: "COALESCE(l.side_a_iface_name, '')",
	SideAIP:        "''",
	SideZIfaceName: "COALESCE(l.side_z_iface_name, '')",
	SideZIP:        "''",
}

var linkDetailSelectsNoIfaceOrIP = linkDetailSelects{
	SideAIfaceName: "''",
	SideAIP:        "''",
	SideZIfaceName: "''",
	SideZIP:        "''",
}

func linkDetailQuery(selects linkDetailSelects, includeDirectional bool) string {
	latencyPerDirectionSelect := `
		latency_per_direction AS (
			SELECT
				link_pk,
				avgIf(rtt_us, direction = 'a_to_z') as avg_rtt_a_to_z,
				avgIf(abs(ipdv_us), direction = 'a_to_z') as avg_jitter_a_to_z,
				avgIf(rtt_us, direction = 'z_to_a') as avg_rtt_z_to_a,
				avgIf(abs(ipdv_us), direction = 'z_to_a') as avg_jitter_z_to_a
			FROM fact_dz_device_link_latency
			WHERE event_ts > now() - INTERVAL 3 HOUR
			GROUP BY link_pk
		)`
	latencyPerDirectionJoin := `
		LEFT JOIN latency_per_direction lpd ON l.pk = lpd.link_pk`
	latencyPerDirectionFields := `
			COALESCE(lpd.avg_rtt_a_to_z, 0) as latency_a_to_z_us,
			COALESCE(lpd.avg_jitter_a_to_z, 0) as jitter_a_to_z_us,
			COALESCE(lpd.avg_rtt_z_to_a, 0) as latency_z_to_a_us,
			COALESCE(lpd.avg_jitter_z_to_a, 0) as jitter_z_to_a_us,`

	if !includeDirectional {
		latencyPerDirectionJoin = ""
		latencyPerDirectionFields = `
			toFloat64(0) as latency_a_to_z_us,
			toFloat64(0) as jitter_a_to_z_us,
			toFloat64(0) as latency_z_to_a_us,
			toFloat64(0) as jitter_z_to_a_us,`
	}

	ctes := []string{
		`
		traffic_rates AS (
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
				AND delta_duration > 0
				AND in_octets_delta >= 0
				AND out_octets_delta >= 0
			GROUP BY link_pk
		)`,
		`
		peak_rates AS (
			SELECT
				link_pk,
				max(in_octets_delta * 8 / nullIf(delta_duration, 0)) as peak_in_bps,
				max(out_octets_delta * 8 / nullIf(delta_duration, 0)) as peak_out_bps
			FROM fact_dz_device_interface_counters
			WHERE event_ts > now() - INTERVAL 1 HOUR
				AND link_pk != ''
				AND delta_duration > 0
				AND in_octets_delta >= 0
				AND out_octets_delta >= 0
			GROUP BY link_pk
		)`,
		`
		latency_stats AS (
			SELECT
				link_pk,
				avg(rtt_us) as avg_rtt_us,
				avg(abs(ipdv_us)) as avg_jitter_us,
				countIf(loss) * 100.0 / count(*) as loss_percent
			FROM fact_dz_device_link_latency
			WHERE event_ts > now() - INTERVAL 3 HOUR
			GROUP BY link_pk
		)`,
	}

	if includeDirectional {
		ctes = append(ctes, latencyPerDirectionSelect)
	}

	withClause := fmt.Sprintf("WITH %s", strings.Join(ctes, ","))

	return fmt.Sprintf(`
		%s
		SELECT
			l.pk,
			l.code,
			l.status,
			l.link_type,
			COALESCE(l.bandwidth_bps, 0) as bandwidth_bps,
			COALESCE(l.side_a_pk, '') as side_a_pk,
			COALESCE(da.code, '') as side_a_code,
			COALESCE(ma.code, '') as side_a_metro,
			%s as side_a_iface_name,
			%s as side_a_ip,
			COALESCE(l.side_z_pk, '') as side_z_pk,
			COALESCE(dz.code, '') as side_z_code,
			COALESCE(mz.code, '') as side_z_metro,
			%s as side_z_iface_name,
			%s as side_z_ip,
			COALESCE(l.contributor_pk, '') as contributor_pk,
			COALESCE(c.code, '') as contributor_code,
			COALESCE(tr.in_bps, 0) as in_bps,
			COALESCE(tr.out_bps, 0) as out_bps,
			CASE WHEN l.bandwidth_bps > 0 THEN COALESCE(tr.in_bps, 0) * 100.0 / l.bandwidth_bps ELSE 0 END as utilization_in,
			CASE WHEN l.bandwidth_bps > 0 THEN COALESCE(tr.out_bps, 0) * 100.0 / l.bandwidth_bps ELSE 0 END as utilization_out,
			COALESCE(ls.avg_rtt_us, 0) as latency_us,
			COALESCE(ls.avg_jitter_us, 0) as jitter_us,
			%s
			COALESCE(ls.loss_percent, 0) as loss_percent,
			COALESCE(pr.peak_in_bps, 0) as peak_in_bps,
			COALESCE(pr.peak_out_bps, 0) as peak_out_bps
		FROM dz_links_current l
		LEFT JOIN dz_devices_current da ON l.side_a_pk = da.pk
		LEFT JOIN dz_metros_current ma ON da.metro_pk = ma.pk
		LEFT JOIN dz_devices_current dz ON l.side_z_pk = dz.pk
		LEFT JOIN dz_metros_current mz ON dz.metro_pk = mz.pk
		LEFT JOIN dz_contributors_current c ON l.contributor_pk = c.pk
		LEFT JOIN traffic_rates tr ON l.pk = tr.link_pk
		LEFT JOIN peak_rates pr ON l.pk = pr.link_pk
		LEFT JOIN latency_stats ls ON l.pk = ls.link_pk
		%s
		WHERE l.pk = ?
	`,
		withClause,
		selects.SideAIfaceName,
		selects.SideAIP,
		selects.SideZIfaceName,
		selects.SideZIP,
		latencyPerDirectionFields,
		latencyPerDirectionJoin,
	)
}

func detectMissingLinkColumns(err error) (missingIP bool, missingIface bool, missingDirection bool) {
	if err == nil {
		return false, false, false
	}
	msg := err.Error()
	if !strings.Contains(msg, "Unknown") && !strings.Contains(msg, "Missing") && !strings.Contains(msg, "identifier") {
		return false, false, false
	}
	missingIP = strings.Contains(msg, "side_a_ip") || strings.Contains(msg, "side_z_ip")
	missingIface = strings.Contains(msg, "side_a_iface_name") || strings.Contains(msg, "side_z_iface_name")
	missingDirection = strings.Contains(msg, "direction")
	return missingIP, missingIface, missingDirection
}
