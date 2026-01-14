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

type Validator struct {
	VotePubkey string  `json:"vote_pubkey"`
	NodePubkey string  `json:"node_pubkey"`
	DevicePK   string  `json:"device_pk"`
	TunnelID   int32   `json:"tunnel_id"`
	Latitude   float64 `json:"latitude"`
	Longitude  float64 `json:"longitude"`
	City       string  `json:"city"`
	Country    string  `json:"country"`
	StakeSol   float64 `json:"stake_sol"`
	StakeShare float64 `json:"stake_share"`
	InBps      float64 `json:"in_bps"`
	OutBps     float64 `json:"out_bps"`
}

type TopologyResponse struct {
	Metros     []Metro     `json:"metros"`
	Devices    []Device    `json:"devices"`
	Links      []Link      `json:"links"`
	Validators []Validator `json:"validators"`
	Error      string      `json:"error,omitempty"`
}

func GetTopology(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	start := time.Now()

	var metros []Metro
	var devices []Device
	var links []Link
	var validators []Validator

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

	// Fetch validators on DZ with their GeoIP locations and traffic rates
	g.Go(func() error {
		query := `
			WITH total_dz_stake AS (
				SELECT COALESCE(SUM(va.activated_stake_lamports), 0) as total_lamports
				FROM dz_users_current u
				JOIN solana_gossip_nodes_current gn ON u.dz_ip = gn.gossip_ip
				JOIN solana_vote_accounts_current va ON gn.pubkey = va.node_pubkey
					AND va.epoch_vote_account = 'true'
					AND va.activated_stake_lamports > 0
				WHERE u.status = 'activated'
			),
			user_traffic AS (
				SELECT
					user_tunnel_id,
					CASE WHEN SUM(delta_duration) > 0 THEN SUM(in_octets_delta) * 8 / SUM(delta_duration) ELSE 0 END as in_bps,
					CASE WHEN SUM(delta_duration) > 0 THEN SUM(out_octets_delta) * 8 / SUM(delta_duration) ELSE 0 END as out_bps
				FROM fact_dz_device_interface_counters
				WHERE event_ts > now() - INTERVAL 5 MINUTE
					AND user_tunnel_id IS NOT NULL
				GROUP BY user_tunnel_id
			)
			SELECT
				va.vote_pubkey,
				gn.pubkey as node_pubkey,
				u.device_pk,
				u.tunnel_id,
				geo.latitude,
				geo.longitude,
				COALESCE(geo.city, '') as city,
				COALESCE(geo.country, '') as country,
				va.activated_stake_lamports / 1e9 as stake_sol,
				CASE
					WHEN ts.total_lamports > 0 THEN va.activated_stake_lamports / ts.total_lamports * 100
					ELSE 0
				END as stake_share,
				COALESCE(traffic.in_bps, 0) as in_bps,
				COALESCE(traffic.out_bps, 0) as out_bps
			FROM dz_users_current u
			JOIN solana_gossip_nodes_current gn ON u.dz_ip = gn.gossip_ip
			JOIN solana_vote_accounts_current va ON gn.pubkey = va.node_pubkey
				AND va.epoch_vote_account = 'true'
				AND va.activated_stake_lamports > 0
			LEFT JOIN geoip_records_current geo ON gn.gossip_ip = geo.ip
			LEFT JOIN user_traffic traffic ON u.tunnel_id = traffic.user_tunnel_id
			CROSS JOIN total_dz_stake ts
			WHERE u.status = 'activated'
				AND geo.latitude IS NOT NULL
				AND geo.longitude IS NOT NULL
		`
		rows, err := config.DB.Query(ctx, query)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var v Validator
			if err := rows.Scan(&v.VotePubkey, &v.NodePubkey, &v.DevicePK, &v.TunnelID, &v.Latitude, &v.Longitude, &v.City, &v.Country, &v.StakeSol, &v.StakeShare, &v.InBps, &v.OutBps); err != nil {
				return err
			}
			validators = append(validators, v)
		}
		return rows.Err()
	})

	err := g.Wait()
	duration := time.Since(start)
	metrics.RecordClickHouseQuery(duration, err)

	response := TopologyResponse{
		Metros:     metros,
		Devices:    devices,
		Links:      links,
		Validators: validators,
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
	if response.Validators == nil {
		response.Validators = []Validator{}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("JSON encoding error: %v", err)
	}
}

// Traffic data point for charts
type TrafficDataPoint struct {
	Time    string  `json:"time"`
	AvgIn   float64 `json:"avgIn"`
	AvgOut  float64 `json:"avgOut"`
	PeakIn  float64 `json:"peakIn"`
	PeakOut float64 `json:"peakOut"`
}

type TrafficResponse struct {
	Points []TrafficDataPoint `json:"points"`
	Error  string             `json:"error,omitempty"`
}

func GetTopologyTraffic(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	itemType := r.URL.Query().Get("type")
	pk := r.URL.Query().Get("pk")

	if pk == "" || (itemType != "link" && itemType != "device" && itemType != "validator") {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(TrafficResponse{Points: []TrafficDataPoint{}})
		return
	}

	start := time.Now()

	var points []TrafficDataPoint
	var query string

	if itemType == "link" {
		// Get hourly traffic for a link over the last 24 hours
		query = `
			SELECT
				formatDateTime(toStartOfHour(event_ts), '%H:%M') as time_bucket,
				avg(in_octets_delta * 8 / nullIf(delta_duration, 0)) as avg_in_bps,
				avg(out_octets_delta * 8 / nullIf(delta_duration, 0)) as avg_out_bps,
				max(in_octets_delta * 8 / nullIf(delta_duration, 0)) as peak_in_bps,
				max(out_octets_delta * 8 / nullIf(delta_duration, 0)) as peak_out_bps
			FROM fact_dz_device_interface_counters
			WHERE event_ts > now() - INTERVAL 24 HOUR
				AND link_pk = $1
				AND delta_duration > 0
			GROUP BY time_bucket
			ORDER BY min(event_ts)
		`
	} else if itemType == "validator" {
		// Get hourly traffic for a validator (user tunnel) over the last 24 hours
		query = `
			SELECT
				formatDateTime(toStartOfHour(event_ts), '%H:%M') as time_bucket,
				avg(in_octets_delta * 8 / nullIf(delta_duration, 0)) as avg_in_bps,
				avg(out_octets_delta * 8 / nullIf(delta_duration, 0)) as avg_out_bps,
				max(in_octets_delta * 8 / nullIf(delta_duration, 0)) as peak_in_bps,
				max(out_octets_delta * 8 / nullIf(delta_duration, 0)) as peak_out_bps
			FROM fact_dz_device_interface_counters
			WHERE event_ts > now() - INTERVAL 24 HOUR
				AND user_tunnel_id = $1
				AND delta_duration > 0
			GROUP BY time_bucket
			ORDER BY min(event_ts)
		`
	} else {
		// Get hourly traffic for a device over the last 24 hours
		query = `
			SELECT
				formatDateTime(toStartOfHour(event_ts), '%H:%M') as time_bucket,
				avg(in_octets_delta * 8 / nullIf(delta_duration, 0)) as avg_in_bps,
				avg(out_octets_delta * 8 / nullIf(delta_duration, 0)) as avg_out_bps,
				max(in_octets_delta * 8 / nullIf(delta_duration, 0)) as peak_in_bps,
				max(out_octets_delta * 8 / nullIf(delta_duration, 0)) as peak_out_bps
			FROM fact_dz_device_interface_counters
			WHERE event_ts > now() - INTERVAL 24 HOUR
				AND device_pk = $1
				AND delta_duration > 0
			GROUP BY time_bucket
			ORDER BY min(event_ts)
		`
	}

	rows, err := config.DB.Query(ctx, query, pk)
	if err != nil {
		log.Printf("Traffic query error: %v", err)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(TrafficResponse{Error: err.Error()})
		return
	}
	defer rows.Close()

	for rows.Next() {
		var p TrafficDataPoint
		var avgIn, avgOut, peakIn, peakOut *float64
		if err := rows.Scan(&p.Time, &avgIn, &avgOut, &peakIn, &peakOut); err != nil {
			log.Printf("Traffic scan error: %v", err)
			continue
		}
		if avgIn != nil {
			p.AvgIn = *avgIn
		}
		if avgOut != nil {
			p.AvgOut = *avgOut
		}
		if peakIn != nil {
			p.PeakIn = *peakIn
		}
		if peakOut != nil {
			p.PeakOut = *peakOut
		}
		points = append(points, p)
	}

	duration := time.Since(start)
	metrics.RecordClickHouseQuery(duration, rows.Err())

	if points == nil {
		points = []TrafficDataPoint{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(TrafficResponse{Points: points})
}
