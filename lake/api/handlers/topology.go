package handlers

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/malbeclabs/doublezero/lake/api/config"
	"github.com/malbeclabs/doublezero/lake/api/metrics"
	"golang.org/x/sync/errgroup"
)

type Metro struct {
	PK        string  `json:"pk"`
	Code      string  `json:"code"`
	Name      string  `json:"name"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type Device struct {
	PK             string  `json:"pk"`
	Code           string  `json:"code"`
	Status         string  `json:"status"`
	DeviceType     string  `json:"device_type"`
	MetroPK        string  `json:"metro_pk"`
	UserCount      uint64  `json:"user_count"`
	ValidatorCount uint64  `json:"validator_count"`
	StakeSol       float64 `json:"stake_sol"`
	StakeShare     float64 `json:"stake_share"`
}

type Link struct {
	PK           string  `json:"pk"`
	Code         string  `json:"code"`
	Status       string  `json:"status"`
	LinkType     string  `json:"link_type"`
	BandwidthBps int64   `json:"bandwidth_bps"`
	SideAPK      string  `json:"side_a_pk"`
	SideZPK      string  `json:"side_z_pk"`
	LatencyUs    float64 `json:"latency_us"`
	JitterUs     float64 `json:"jitter_us"`
	LossPercent  float64 `json:"loss_percent"`
	SampleCount  uint64  `json:"sample_count"`
	InBps        float64 `json:"in_bps"`
	OutBps       float64 `json:"out_bps"`
}

type TopologyResponse struct {
	Metros  []Metro  `json:"metros"`
	Devices []Device `json:"devices"`
	Links   []Link   `json:"links"`
	Error   string   `json:"error,omitempty"`
}

func GetTopology(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	start := time.Now()

	var metros []Metro
	var devices []Device
	var links []Link

	g, ctx := errgroup.WithContext(ctx)

	// Fetch metros with coordinates
	g.Go(func() error {
		query := `
			SELECT pk, code, name, latitude, longitude
			FROM dz_metros_current
			WHERE latitude IS NOT NULL AND longitude IS NOT NULL
		`
		rows, err := config.DB.Query(ctx, query)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var m Metro
			if err := rows.Scan(&m.PK, &m.Code, &m.Name, &m.Latitude, &m.Longitude); err != nil {
				return err
			}
			metros = append(metros, m)
		}
		return rows.Err()
	})

	// Fetch activated devices with user/validator/stake stats
	g.Go(func() error {
		query := `
			WITH total_stake AS (
				SELECT COALESCE(SUM(activated_stake_lamports), 0) as total_lamports
				FROM solana_vote_accounts_current
				WHERE epoch_vote_account = 'true' AND activated_stake_lamports > 0
			),
			device_stats AS (
				SELECT
					u.device_pk,
					COUNT(DISTINCT u.pk) as user_count,
					COUNT(DISTINCT va.vote_pubkey) as validator_count,
					COALESCE(SUM(va.activated_stake_lamports), 0) / 1e9 as stake_sol
				FROM dz_users_current u
				LEFT JOIN solana_gossip_nodes_current gn ON u.dz_ip = gn.gossip_ip
				LEFT JOIN solana_vote_accounts_current va ON gn.pubkey = va.node_pubkey
					AND va.epoch_vote_account = 'true'
					AND va.activated_stake_lamports > 0
				WHERE u.status = 'activated'
				GROUP BY u.device_pk
			)
			SELECT
				d.pk, d.code, d.status, d.device_type, d.metro_pk,
				COALESCE(ds.user_count, 0) as user_count,
				COALESCE(ds.validator_count, 0) as validator_count,
				COALESCE(ds.stake_sol, 0) as stake_sol,
				CASE
					WHEN ts.total_lamports > 0 THEN COALESCE(ds.stake_sol, 0) * 1e9 / ts.total_lamports * 100
					ELSE 0
				END as stake_share
			FROM dz_devices_current d
			CROSS JOIN total_stake ts
			LEFT JOIN device_stats ds ON d.pk = ds.device_pk
			WHERE d.status = 'activated'
		`
		rows, err := config.DB.Query(ctx, query)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var d Device
			if err := rows.Scan(&d.PK, &d.Code, &d.Status, &d.DeviceType, &d.MetroPK, &d.UserCount, &d.ValidatorCount, &d.StakeSol, &d.StakeShare); err != nil {
				return err
			}
			devices = append(devices, d)
		}
		return rows.Err()
	})

	// Fetch activated links with measured latency, jitter, loss, and traffic rates
	g.Go(func() error {
		query := `
			SELECT
				l.pk, l.code, l.status, l.link_type, l.bandwidth_bps, l.side_a_pk, l.side_z_pk,
				COALESCE(lat.avg_rtt_us, 0) as latency_us,
				COALESCE(lat.avg_ipdv_us, 0) as jitter_us,
				COALESCE(lat.loss_percent, 0) as loss_percent,
				COALESCE(lat.sample_count, 0) as sample_count,
				COALESCE(traffic.in_bps, 0) as in_bps,
				COALESCE(traffic.out_bps, 0) as out_bps
			FROM dz_links_current l
			LEFT JOIN (
				SELECT link_pk,
					avg(rtt_us) as avg_rtt_us,
					avg(abs(ipdv_us)) as avg_ipdv_us,
					countIf(loss) * 100.0 / count(*) as loss_percent,
					count(*) as sample_count
				FROM fact_dz_device_link_latency
				WHERE event_ts > now() - INTERVAL 3 HOUR
				GROUP BY link_pk
			) lat ON l.pk = lat.link_pk
			LEFT JOIN (
				SELECT link_pk,
					CASE WHEN SUM(delta_duration) > 0 THEN SUM(in_octets_delta) * 8 / SUM(delta_duration) ELSE 0 END as in_bps,
					CASE WHEN SUM(delta_duration) > 0 THEN SUM(out_octets_delta) * 8 / SUM(delta_duration) ELSE 0 END as out_bps
				FROM fact_dz_device_interface_counters
				WHERE event_ts > now() - INTERVAL 5 MINUTE
					AND link_pk != ''
				GROUP BY link_pk
			) traffic ON l.pk = traffic.link_pk
			WHERE l.status = 'activated'
		`
		rows, err := config.DB.Query(ctx, query)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var l Link
			if err := rows.Scan(&l.PK, &l.Code, &l.Status, &l.LinkType, &l.BandwidthBps, &l.SideAPK, &l.SideZPK, &l.LatencyUs, &l.JitterUs, &l.LossPercent, &l.SampleCount, &l.InBps, &l.OutBps); err != nil {
				return err
			}
			links = append(links, l)
		}
		return rows.Err()
	})

	err := g.Wait()
	duration := time.Since(start)
	metrics.RecordClickHouseQuery(duration, err)

	response := TopologyResponse{
		Metros:  metros,
		Devices: devices,
		Links:   links,
	}

	if err != nil {
		log.Printf("Topology query error: %v", err)
		response.Error = err.Error()
	}

	// Ensure non-nil slices for JSON serialization
	if response.Metros == nil {
		response.Metros = []Metro{}
	}
	if response.Devices == nil {
		response.Devices = []Device{}
	}
	if response.Links == nil {
		response.Links = []Link{}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("JSON encoding error: %v", err)
	}
}
