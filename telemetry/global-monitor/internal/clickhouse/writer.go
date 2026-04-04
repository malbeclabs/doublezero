package clickhouse

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
)

// SolanaValidatorICMPProbeRow represents a row in the solana_validator_icmp_probe table.
type SolanaValidatorICMPProbeRow struct {
	Timestamp time.Time

	// Probe dimensions.
	ProbeType string
	ProbePath string

	// Validator dimensions.
	ValidatorPubkey     string
	ValidatorVotePubkey string

	// Target dimensions.
	TargetIP        string
	TargetIPBlock24 string
	TargetEndpoint  string

	// Source dimensions.
	SourceMetro        string
	SourceMetroName    string
	SourceHost         string
	SourceIface        string
	SourceIP           string
	SourceDZDCode      string
	SourceDZDMetroCode string
	SourceDZDMetroName string

	// Target device dimensions.
	TargetDZDCode      string
	TargetDZDMetroCode string
	TargetDZDMetroName string

	// Target GeoIP dimensions.
	TargetGeoIPCountry     string
	TargetGeoIPCountryCode string
	TargetGeoIPRegion      string
	TargetGeoIPCity        string
	TargetGeoIPCityID      int32
	TargetGeoIPMetro       string
	TargetGeoIPASN         uint32
	TargetGeoIPASNOrg      string
	TargetGeoIPLatitude    float64
	TargetGeoIPLongitude   float64

	// Probe result metrics.
	ProbeOK          bool
	ProbeFailReason  string
	ProbeRTTAvgMs    float64
	ProbeRTTLatestMs float64
	ProbeRTTMinMs    float64
	ProbeRTTDevMs    float64
	ProbePacketsSent int64
	ProbePacketsRecv int64
	ProbePacketsLost int64
	ProbeLossRatio   float64

	// Validator metrics.
	ValidatorLeaderRatio   float64
	ValidatorStakeLamports uint64
}

// SolanaValidatorTPUQUICProbeRow represents a row in the solana_validator_tpuquic_probe table.
type SolanaValidatorTPUQUICProbeRow struct {
	Timestamp time.Time

	// Probe dimensions.
	ProbeType string
	ProbePath string

	// Validator dimensions.
	ValidatorPubkey     string
	ValidatorVotePubkey string

	// Target dimensions.
	TargetIP        string
	TargetIPBlock24 string
	TargetPort      uint16
	TargetEndpoint  string

	// Source dimensions.
	SourceMetro        string
	SourceMetroName    string
	SourceHost         string
	SourceIface        string
	SourceIP           string
	SourceDZDCode      string
	SourceDZDMetroCode string
	SourceDZDMetroName string

	// Target device dimensions.
	TargetDZDCode      string
	TargetDZDMetroCode string
	TargetDZDMetroName string

	// Target GeoIP dimensions.
	TargetGeoIPCountry     string
	TargetGeoIPCountryCode string
	TargetGeoIPRegion      string
	TargetGeoIPCity        string
	TargetGeoIPCityID      int32
	TargetGeoIPMetro       string
	TargetGeoIPASN         uint32
	TargetGeoIPASNOrg      string
	TargetGeoIPLatitude    float64
	TargetGeoIPLongitude   float64

	// Probe result metrics.
	ProbeOK          bool
	ProbeFailReason  string
	ProbeRTTAvgMs    float64
	ProbeRTTLatestMs float64
	ProbeRTTMinMs    float64
	ProbeRTTDevMs    float64
	ProbePacketsSent int64
	ProbePacketsRecv int64
	ProbePacketsLost int64
	ProbeLossRatio   float64

	// Validator metrics.
	ValidatorLeaderRatio   float64
	ValidatorStakeLamports uint64
}

// DoubleZeroUserICMPProbeRow represents a row in the doublezero_user_icmp_probe table.
type DoubleZeroUserICMPProbeRow struct {
	Timestamp time.Time

	// Probe dimensions.
	ProbeType string
	ProbePath string

	// User dimensions.
	UserPubkey          string
	UserValidatorPubkey string
	ValidatorVotePubkey string

	// Target dimensions.
	TargetIP        string
	TargetIPBlock24 string

	// Source dimensions.
	SourceMetro        string
	SourceMetroName    string
	SourceHost         string
	SourceIface        string
	SourceIP           string
	SourceUserPubkey   string
	SourceDZDCode      string
	SourceDZDMetroCode string
	SourceDZDMetroName string

	// Target device dimensions.
	TargetDZDCode      string
	TargetDZDMetroCode string
	TargetDZDMetroName string

	// Target GeoIP dimensions.
	TargetGeoIPCountry     string
	TargetGeoIPCountryCode string
	TargetGeoIPRegion      string
	TargetGeoIPCity        string
	TargetGeoIPCityID      int32
	TargetGeoIPMetro       string
	TargetGeoIPASN         uint32
	TargetGeoIPASNOrg      string
	TargetGeoIPLatitude    float64
	TargetGeoIPLongitude   float64

	// Probe result metrics.
	ProbeOK          bool
	ProbeFailReason  string
	ProbeRTTAvgMs    float64
	ProbeRTTLatestMs float64
	ProbeRTTMinMs    float64
	ProbeRTTDevMs    float64
	ProbePacketsSent int64
	ProbePacketsRecv int64
	ProbePacketsLost int64
	ProbeLossRatio   float64

	// Solana cross-reference metrics.
	UserValidatorPubkeyInSolanaVoteAccounts bool
	UserValidatorPubkeyInSolanaGossip       bool
	TargetIPInSolanaGossip                  bool
	TargetIPInSolanaGossipAsTPUQUIC         bool
}

// ProbeWriter is the interface used by planners to append probe result rows.
type ProbeWriter interface {
	AppendSolanaValidatorICMPProbe(row SolanaValidatorICMPProbeRow)
	AppendSolanaValidatorTPUQUICProbe(row SolanaValidatorTPUQUICProbeRow)
	AppendDoubleZeroUserICMPProbe(row DoubleZeroUserICMPProbeRow)
}

// Writer buffers probe result rows and flushes them to ClickHouse in batches.
type Writer struct {
	conn clickhouse.Conn
	db   string
	log  *slog.Logger

	mu             sync.Mutex
	solICMPRows    []SolanaValidatorICMPProbeRow
	solTPUQUICRows []SolanaValidatorTPUQUICProbeRow
	dzUserICMPRows []DoubleZeroUserICMPProbeRow
}

// NewWriter creates a new ClickHouse writer.
func NewWriter(addr, database, username, password string, secure bool, log *slog.Logger) (*Writer, error) {
	opts := &clickhouse.Options{
		Addr: []string{addr},
		Auth: clickhouse.Auth{
			Database: database,
			Username: username,
			Password: password,
		},
		Settings: clickhouse.Settings{
			"async_insert":          0,
			"wait_for_async_insert": 1,
			"insert_deduplicate":    0,
		},
	}
	if secure {
		opts.TLS = &tls.Config{}
	}

	conn, err := clickhouse.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("clickhouse open: %w", err)
	}

	if err := conn.Ping(context.Background()); err != nil {
		return nil, fmt.Errorf("clickhouse ping: %w", err)
	}

	return &Writer{
		conn: conn,
		db:   database,
		log:  log,
	}, nil
}

// Close closes the ClickHouse connection.
func (w *Writer) Close() error {
	return w.conn.Close()
}

// AppendSolanaValidatorICMPProbe buffers a solana validator ICMP probe row.
func (w *Writer) AppendSolanaValidatorICMPProbe(row SolanaValidatorICMPProbeRow) {
	w.mu.Lock()
	w.solICMPRows = append(w.solICMPRows, row)
	w.mu.Unlock()
}

// AppendSolanaValidatorTPUQUICProbe buffers a solana validator TPUQUIC probe row.
func (w *Writer) AppendSolanaValidatorTPUQUICProbe(row SolanaValidatorTPUQUICProbeRow) {
	w.mu.Lock()
	w.solTPUQUICRows = append(w.solTPUQUICRows, row)
	w.mu.Unlock()
}

// AppendDoubleZeroUserICMPProbe buffers a doublezero user ICMP probe row.
func (w *Writer) AppendDoubleZeroUserICMPProbe(row DoubleZeroUserICMPProbeRow) {
	w.mu.Lock()
	w.dzUserICMPRows = append(w.dzUserICMPRows, row)
	w.mu.Unlock()
}

// Flush writes all buffered rows to ClickHouse and clears the buffers.
func (w *Writer) Flush(ctx context.Context) error {
	w.mu.Lock()
	solICMP := w.solICMPRows
	solTPUQUIC := w.solTPUQUICRows
	dzUserICMP := w.dzUserICMPRows
	w.solICMPRows = nil
	w.solTPUQUICRows = nil
	w.dzUserICMPRows = nil
	w.mu.Unlock()

	var errs []error

	if len(solICMP) > 0 {
		if err := w.flushSolanaValidatorICMPProbe(ctx, solICMP); err != nil {
			w.log.Error("clickhouse: failed to flush solana validator ICMP probe rows", "error", err, "count", len(solICMP))
			errs = append(errs, err)
		} else {
			w.log.Debug("clickhouse: flushed solana validator ICMP probe rows", "count", len(solICMP))
		}
	}

	if len(solTPUQUIC) > 0 {
		if err := w.flushSolanaValidatorTPUQUICProbe(ctx, solTPUQUIC); err != nil {
			w.log.Error("clickhouse: failed to flush solana validator TPUQUIC probe rows", "error", err, "count", len(solTPUQUIC))
			errs = append(errs, err)
		} else {
			w.log.Debug("clickhouse: flushed solana validator TPUQUIC probe rows", "count", len(solTPUQUIC))
		}
	}

	if len(dzUserICMP) > 0 {
		if err := w.flushDoubleZeroUserICMPProbe(ctx, dzUserICMP); err != nil {
			w.log.Error("clickhouse: failed to flush doublezero user ICMP probe rows", "error", err, "count", len(dzUserICMP))
			errs = append(errs, err)
		} else {
			w.log.Debug("clickhouse: flushed doublezero user ICMP probe rows", "count", len(dzUserICMP))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("clickhouse: %d flush errors", len(errs))
	}
	return nil
}

func (w *Writer) flushSolanaValidatorICMPProbe(ctx context.Context, rows []SolanaValidatorICMPProbeRow) error {
	batch, err := w.conn.PrepareBatch(ctx, fmt.Sprintf(`INSERT INTO %s.solana_validator_icmp_probe (
		timestamp,
		probe_type, probe_path,
		validator_pubkey, validator_vote_pubkey,
		target_ip, target_ip_block_24, target_endpoint,
		source_metro, source_metro_name, source_host, source_iface, source_ip,
		source_dzd_code, source_dzd_metro_code, source_dzd_metro_name,
		target_dzd_code, target_dzd_metro_code, target_dzd_metro_name,
		target_geoip_country, target_geoip_country_code, target_geoip_region,
		target_geoip_city, target_geoip_city_id, target_geoip_metro,
		target_geoip_asn, target_geoip_asn_org,
		target_geoip_latitude, target_geoip_longitude,
		probe_ok, probe_fail_reason,
		probe_rtt_avg_ms, probe_rtt_latest_ms, probe_rtt_min_ms, probe_rtt_dev_ms,
		probe_packets_sent, probe_packets_recv, probe_packets_lost, probe_loss_ratio,
		validator_leader_ratio, validator_stake_lamports
	)`, w.db))
	if err != nil {
		return fmt.Errorf("prepare batch: %w", err)
	}

	for _, r := range rows {
		if err := batch.Append(
			r.Timestamp,
			r.ProbeType, r.ProbePath,
			r.ValidatorPubkey, r.ValidatorVotePubkey,
			r.TargetIP, r.TargetIPBlock24, r.TargetEndpoint,
			r.SourceMetro, r.SourceMetroName, r.SourceHost, r.SourceIface, r.SourceIP,
			r.SourceDZDCode, r.SourceDZDMetroCode, r.SourceDZDMetroName,
			r.TargetDZDCode, r.TargetDZDMetroCode, r.TargetDZDMetroName,
			r.TargetGeoIPCountry, r.TargetGeoIPCountryCode, r.TargetGeoIPRegion,
			r.TargetGeoIPCity, r.TargetGeoIPCityID, r.TargetGeoIPMetro,
			r.TargetGeoIPASN, r.TargetGeoIPASNOrg,
			r.TargetGeoIPLatitude, r.TargetGeoIPLongitude,
			r.ProbeOK, r.ProbeFailReason,
			r.ProbeRTTAvgMs, r.ProbeRTTLatestMs, r.ProbeRTTMinMs, r.ProbeRTTDevMs,
			r.ProbePacketsSent, r.ProbePacketsRecv, r.ProbePacketsLost, r.ProbeLossRatio,
			r.ValidatorLeaderRatio, r.ValidatorStakeLamports,
		); err != nil {
			_ = batch.Abort()
			return fmt.Errorf("append row: %w", err)
		}
	}

	return batch.Send()
}

func (w *Writer) flushSolanaValidatorTPUQUICProbe(ctx context.Context, rows []SolanaValidatorTPUQUICProbeRow) error {
	batch, err := w.conn.PrepareBatch(ctx, fmt.Sprintf(`INSERT INTO %s.solana_validator_tpuquic_probe (
		timestamp,
		probe_type, probe_path,
		validator_pubkey, validator_vote_pubkey,
		target_ip, target_ip_block_24, target_port, target_endpoint,
		source_metro, source_metro_name, source_host, source_iface, source_ip,
		source_dzd_code, source_dzd_metro_code, source_dzd_metro_name,
		target_dzd_code, target_dzd_metro_code, target_dzd_metro_name,
		target_geoip_country, target_geoip_country_code, target_geoip_region,
		target_geoip_city, target_geoip_city_id, target_geoip_metro,
		target_geoip_asn, target_geoip_asn_org,
		target_geoip_latitude, target_geoip_longitude,
		probe_ok, probe_fail_reason,
		probe_rtt_avg_ms, probe_rtt_latest_ms, probe_rtt_min_ms, probe_rtt_dev_ms,
		probe_packets_sent, probe_packets_recv, probe_packets_lost, probe_loss_ratio,
		validator_leader_ratio, validator_stake_lamports
	)`, w.db))
	if err != nil {
		return fmt.Errorf("prepare batch: %w", err)
	}

	for _, r := range rows {
		if err := batch.Append(
			r.Timestamp,
			r.ProbeType, r.ProbePath,
			r.ValidatorPubkey, r.ValidatorVotePubkey,
			r.TargetIP, r.TargetIPBlock24, r.TargetPort, r.TargetEndpoint,
			r.SourceMetro, r.SourceMetroName, r.SourceHost, r.SourceIface, r.SourceIP,
			r.SourceDZDCode, r.SourceDZDMetroCode, r.SourceDZDMetroName,
			r.TargetDZDCode, r.TargetDZDMetroCode, r.TargetDZDMetroName,
			r.TargetGeoIPCountry, r.TargetGeoIPCountryCode, r.TargetGeoIPRegion,
			r.TargetGeoIPCity, r.TargetGeoIPCityID, r.TargetGeoIPMetro,
			r.TargetGeoIPASN, r.TargetGeoIPASNOrg,
			r.TargetGeoIPLatitude, r.TargetGeoIPLongitude,
			r.ProbeOK, r.ProbeFailReason,
			r.ProbeRTTAvgMs, r.ProbeRTTLatestMs, r.ProbeRTTMinMs, r.ProbeRTTDevMs,
			r.ProbePacketsSent, r.ProbePacketsRecv, r.ProbePacketsLost, r.ProbeLossRatio,
			r.ValidatorLeaderRatio, r.ValidatorStakeLamports,
		); err != nil {
			_ = batch.Abort()
			return fmt.Errorf("append row: %w", err)
		}
	}

	return batch.Send()
}

func (w *Writer) flushDoubleZeroUserICMPProbe(ctx context.Context, rows []DoubleZeroUserICMPProbeRow) error {
	batch, err := w.conn.PrepareBatch(ctx, fmt.Sprintf(`INSERT INTO %s.doublezero_user_icmp_probe (
		timestamp,
		probe_type, probe_path,
		user_pubkey, user_validator_pubkey, validator_vote_pubkey,
		target_ip, target_ip_block_24,
		source_metro, source_metro_name, source_host, source_iface, source_ip,
		source_user_pubkey,
		source_dzd_code, source_dzd_metro_code, source_dzd_metro_name,
		target_dzd_code, target_dzd_metro_code, target_dzd_metro_name,
		target_geoip_country, target_geoip_country_code, target_geoip_region,
		target_geoip_city, target_geoip_city_id, target_geoip_metro,
		target_geoip_asn, target_geoip_asn_org,
		target_geoip_latitude, target_geoip_longitude,
		probe_ok, probe_fail_reason,
		probe_rtt_avg_ms, probe_rtt_latest_ms, probe_rtt_min_ms, probe_rtt_dev_ms,
		probe_packets_sent, probe_packets_recv, probe_packets_lost, probe_loss_ratio,
		user_validator_pubkey_in_solana_vote_accounts,
		user_validator_pubkey_in_solana_gossip,
		target_ip_in_solana_gossip,
		target_ip_in_solana_gossip_as_tpuquic
	)`, w.db))
	if err != nil {
		return fmt.Errorf("prepare batch: %w", err)
	}

	for _, r := range rows {
		if err := batch.Append(
			r.Timestamp,
			r.ProbeType, r.ProbePath,
			r.UserPubkey, r.UserValidatorPubkey, r.ValidatorVotePubkey,
			r.TargetIP, r.TargetIPBlock24,
			r.SourceMetro, r.SourceMetroName, r.SourceHost, r.SourceIface, r.SourceIP,
			r.SourceUserPubkey,
			r.SourceDZDCode, r.SourceDZDMetroCode, r.SourceDZDMetroName,
			r.TargetDZDCode, r.TargetDZDMetroCode, r.TargetDZDMetroName,
			r.TargetGeoIPCountry, r.TargetGeoIPCountryCode, r.TargetGeoIPRegion,
			r.TargetGeoIPCity, r.TargetGeoIPCityID, r.TargetGeoIPMetro,
			r.TargetGeoIPASN, r.TargetGeoIPASNOrg,
			r.TargetGeoIPLatitude, r.TargetGeoIPLongitude,
			r.ProbeOK, r.ProbeFailReason,
			r.ProbeRTTAvgMs, r.ProbeRTTLatestMs, r.ProbeRTTMinMs, r.ProbeRTTDevMs,
			r.ProbePacketsSent, r.ProbePacketsRecv, r.ProbePacketsLost, r.ProbeLossRatio,
			r.UserValidatorPubkeyInSolanaVoteAccounts,
			r.UserValidatorPubkeyInSolanaGossip,
			r.TargetIPInSolanaGossip,
			r.TargetIPInSolanaGossipAsTPUQUIC,
		); err != nil {
			_ = batch.Abort()
			return fmt.Errorf("append row: %w", err)
		}
	}

	return batch.Send()
}
