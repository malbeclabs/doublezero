package dztelem

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/jonboulle/clockwork"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
	"github.com/malbeclabs/doublezero/tools/mcp/internal/duck"
	dzsvc "github.com/malbeclabs/doublezero/tools/mcp/internal/dz/serviceability"
)

type TelemetryRPC interface {
	GetDeviceLatencySamples(ctx context.Context, originDevicePK, targetDevicePK, linkPK solana.PublicKey, epoch uint64) (*telemetry.DeviceLatencySamples, error)
	GetInternetLatencySamples(ctx context.Context, dataProviderName string, originLocationPK, targetLocationPK, agentPK solana.PublicKey, epoch uint64) (*telemetry.InternetLatencySamples, error)
}

type EpochRPC interface {
	GetEpochInfo(ctx context.Context, commitment solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error)
}

type ViewConfig struct {
	Logger                     *slog.Logger
	Clock                      clockwork.Clock
	TelemetryRPC               TelemetryRPC
	EpochRPC                   EpochRPC
	MaxConcurrency             int
	InternetLatencyAgentPK     solana.PublicKey
	InternetDataProviders      []string
	DB                         duck.DB
	Serviceability             *dzsvc.View
	RefreshInterval            time.Duration
	ServiceabilityReadyTimeout time.Duration
}

func (cfg *ViewConfig) Validate() error {
	if cfg.Logger == nil {
		return errors.New("logger is required")
	}
	if cfg.TelemetryRPC == nil {
		return errors.New("telemetry rpc is required")
	}
	if cfg.EpochRPC == nil {
		return errors.New("epoch rpc is required")
	}
	if cfg.DB == nil {
		return errors.New("database is required")
	}
	if cfg.Serviceability == nil {
		return errors.New("serviceability view is required")
	}
	if cfg.RefreshInterval <= 0 {
		return errors.New("refresh interval must be greater than 0")
	}
	if cfg.InternetLatencyAgentPK.IsZero() {
		return errors.New("internet latency agent pk is required")
	}
	if len(cfg.InternetDataProviders) == 0 {
		return errors.New("internet data providers are required")
	}
	if cfg.MaxConcurrency <= 0 {
		return errors.New("max concurrency must be greater than 0")
	}

	if cfg.Clock == nil {
		cfg.Clock = clockwork.NewRealClock()
	}
	if cfg.ServiceabilityReadyTimeout <= 0 {
		cfg.ServiceabilityReadyTimeout = 2 * cfg.RefreshInterval
	}
	return nil
}

type View struct {
	log       *slog.Logger
	cfg       ViewConfig
	db        duck.DB
	readyOnce sync.Once
	readyCh   chan struct{}
	refreshMu sync.Mutex // prevents concurrent refreshes
}

func NewView(cfg ViewConfig) (*View, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	v := &View{
		log:     cfg.Logger,
		cfg:     cfg,
		db:      cfg.DB,
		readyCh: make(chan struct{}),
	}

	if err := v.initDB(); err != nil {
		return nil, fmt.Errorf("failed to initialize database: %w", err)
	}

	return v, nil
}

func (v *View) Start(ctx context.Context) {
	go func() {
		v.log.Info("telemetry: starting refresh loop", "interval", v.cfg.RefreshInterval)

		if err := v.Refresh(ctx); err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			v.log.Error("telemetry: initial refresh failed", "error", err)
		}
		ticker := v.cfg.Clock.NewTicker(v.cfg.RefreshInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.Chan():
				if err := v.Refresh(ctx); err != nil {
					if errors.Is(err, context.Canceled) {
						return
					}
					v.log.Error("telemetry: refresh failed", "error", err)
				}
			}
		}
	}()
}

func (v *View) Refresh(ctx context.Context) error {
	v.refreshMu.Lock()
	defer v.refreshMu.Unlock()

	refreshStart := time.Now()
	v.log.Info("telemetry: refresh started", "start_time", refreshStart)
	defer func() {
		duration := time.Since(refreshStart)
		v.log.Info("telemetry: refresh completed", "duration", duration.String())
	}()

	// Wait for serviceability view to be ready (has completed at least one refresh)
	if !v.cfg.Serviceability.Ready() {
		v.log.Debug("telemetry: waiting for serviceability view to be ready")
		waitCtx, cancel := context.WithTimeout(ctx, v.cfg.ServiceabilityReadyTimeout)
		defer cancel()

		if err := v.cfg.Serviceability.WaitReady(waitCtx); err != nil {
			return fmt.Errorf("serviceability view not ready: %w", err)
		}
		v.log.Debug("telemetry: serviceability view is now ready")
	}

	// Get devices, links, and contributors from View to compute circuits
	devices, err := v.getDevicesFromDB()
	if err != nil {
		return fmt.Errorf("failed to get devices: %w", err)
	}

	links, err := v.getLinksFromDB()
	if err != nil {
		return fmt.Errorf("failed to get links: %w", err)
	}

	contributors, err := v.getContributorsFromDB()
	if err != nil {
		return fmt.Errorf("failed to get contributors: %w", err)
	}

	// Compute and refresh circuits from devices and links
	v.log.Debug("telemetry: computing device-link circuits", "links", len(links))
	circuits := ComputeDeviceLinkCircuits(devices, links, contributors)
	v.log.Debug("telemetry: computed device-link circuits", "count", len(circuits))
	if err := v.refreshDeviceLinkCircuitsTable(circuits); err != nil {
		return fmt.Errorf("failed to refresh device-link circuits: %w", err)
	}

	// Refresh device-link telemetry samples
	if err := v.refreshDeviceLinkTelemetrySamples(ctx, circuits); err != nil {
		v.log.Warn("failed to refresh device-link telemetry samples", "error", err)
		// Don't fail the entire refresh if telemetry fails
	}

	// Refresh internet-metro latency samples if configured
	if !v.cfg.InternetLatencyAgentPK.IsZero() && len(v.cfg.InternetDataProviders) > 0 {
		metros, err := v.getMetrosFromDB()
		if err != nil {
			v.log.Warn("failed to get metros for internet-metro samples", "error", err)
		} else {
			internetCircuits := ComputeInternetMetroCircuits(metros)
			if err := v.refreshInternetMetroLatencySamples(ctx, internetCircuits); err != nil {
				v.log.Warn("failed to refresh internet-metro telemetry samples", "error", err)
				// Don't fail the entire refresh if telemetry fails
			}
		}
	} else {
		v.log.Debug("telemetry: skipping internet-metro samples refresh", "agent_pk_configured", !v.cfg.InternetLatencyAgentPK.IsZero(), "data_providers", len(v.cfg.InternetDataProviders))
	}

	// Signal readiness once (close channel) - safe to call multiple times
	v.readyOnce.Do(func() {
		close(v.readyCh)
		v.log.Info("telemetry: view is now ready")
	})

	return nil
}

// Ready returns true if the view has completed at least one successful refresh
func (v *View) Ready() bool {
	select {
	case <-v.readyCh:
		return true
	default:
		return false
	}
}

// WaitReady waits for the view to be ready (has completed at least one successful refresh)
// It returns immediately if already ready, or blocks until ready or context is cancelled.
func (v *View) WaitReady(ctx context.Context) error {
	select {
	case <-v.readyCh:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("context cancelled while waiting for telemetry view: %w", ctx.Err())
	}
}

func (v *View) initDB() error {
	schemas := []string{
		`CREATE TABLE IF NOT EXISTS dz_device_link_circuits (
			code VARCHAR PRIMARY KEY,
			origin_device_pk VARCHAR,
			target_device_pk VARCHAR,
			link_pk VARCHAR,
			link_code VARCHAR,
			link_type VARCHAR,
			contributor_code VARCHAR,
			committed_rtt DOUBLE,
			committed_jitter DOUBLE
		)`,
		`CREATE TABLE IF NOT EXISTS dz_device_link_latency_samples (
			circuit_code VARCHAR,
			epoch BIGINT,
			sample_index INTEGER,
			timestamp_us BIGINT,
			rtt_us BIGINT,
			PRIMARY KEY (circuit_code, epoch, sample_index)
		)`,
		`CREATE TABLE IF NOT EXISTS dz_internet_metro_latency_samples (
			circuit_code VARCHAR,
			data_provider VARCHAR,
			epoch BIGINT,
			sample_index INTEGER,
			timestamp_us BIGINT,
			rtt_us BIGINT,
			PRIMARY KEY (circuit_code, data_provider, epoch, sample_index)
		)`,
	}

	for _, schema := range schemas {
		if _, err := v.db.Exec(schema); err != nil {
			return fmt.Errorf("failed to create table: %w", err)
		}
	}

	return nil
}

func (v *View) getDevicesFromDB() ([]dzsvc.Device, error) {
	query := `SELECT pk, status, code, public_ip, contributor_pk, metro_pk FROM dz_devices`
	rows, err := v.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query devices: %w", err)
	}
	defer rows.Close()

	var devices []dzsvc.Device
	for rows.Next() {
		var d dzsvc.Device
		if err := rows.Scan(&d.PK, &d.Status, &d.Code, &d.PublicIP, &d.ContributorPK, &d.MetroPK); err != nil {
			return nil, fmt.Errorf("failed to scan device: %w", err)
		}
		devices = append(devices, d)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating devices: %w", err)
	}

	return devices, nil
}

func (v *View) getLinksFromDB() ([]dzsvc.Link, error) {
	query := `SELECT pk, status, code, tunnel_net, contributor_pk, side_a_pk, side_z_pk, side_a_iface_name, side_z_iface_name, link_type, delay_ns, jitter_ns FROM dz_links`
	rows, err := v.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query links: %w", err)
	}
	defer rows.Close()

	var links []dzsvc.Link
	for rows.Next() {
		var l dzsvc.Link
		if err := rows.Scan(&l.PK, &l.Status, &l.Code, &l.TunnelNet, &l.ContributorPK, &l.SideAPK, &l.SideZPK, &l.SideAIfaceName, &l.SideZIfaceName, &l.LinkType, &l.DelayNs, &l.JitterNs); err != nil {
			return nil, fmt.Errorf("failed to scan link: %w", err)
		}
		links = append(links, l)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating links: %w", err)
	}

	return links, nil
}

func (v *View) getContributorsFromDB() ([]dzsvc.Contributor, error) {
	query := `SELECT pk, code FROM dz_contributors`
	rows, err := v.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query contributors: %w", err)
	}
	defer rows.Close()

	var contributors []dzsvc.Contributor
	for rows.Next() {
		var c dzsvc.Contributor
		if err := rows.Scan(&c.PK, &c.Code); err != nil {
			return nil, fmt.Errorf("failed to scan contributor: %w", err)
		}
		contributors = append(contributors, c)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating contributors: %w", err)
	}

	return contributors, nil
}

func (v *View) getMetrosFromDB() ([]dzsvc.Metro, error) {
	query := `SELECT pk, code, name, longitude, latitude FROM dz_metros`
	rows, err := v.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query metros: %w", err)
	}
	defer rows.Close()

	var metros []dzsvc.Metro
	for rows.Next() {
		var m dzsvc.Metro
		if err := rows.Scan(&m.PK, &m.Code, &m.Name, &m.Longitude, &m.Latitude); err != nil {
			return nil, fmt.Errorf("failed to scan metro: %w", err)
		}
		metros = append(metros, m)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating metros: %w", err)
	}

	return metros, nil
}

func (v *View) refreshTable(tableName, deleteSQL, insertSQL string, count int, insertFn func(*sql.Stmt, int) error) error {
	tableRefreshStart := time.Now()
	v.log.Info("telemetry: refreshing table started", "table", tableName, "rows", count, "start_time", tableRefreshStart)
	defer func() {
		duration := time.Since(tableRefreshStart)
		v.log.Info("telemetry: refreshing table completed", "table", tableName, "duration", duration.String())
	}()

	v.log.Debug("telemetry: refreshing table", "table", tableName, "rows", count)

	txStart := time.Now()
	tx, err := v.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction for %s: %w", tableName, err)
	}
	v.log.Debug("telemetry: transaction begun", "table", tableName, "tx_start_time", txStart)
	defer tx.Rollback()

	if _, err := tx.Exec(deleteSQL); err != nil {
		return fmt.Errorf("failed to clear %s: %w", tableName, err)
	}

	if count == 0 {
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("failed to commit transaction for %s: %w", tableName, err)
		}
		v.log.Debug("telemetry: table refreshed (empty)", "table", tableName)
		return nil
	}

	stmt, err := tx.Prepare(insertSQL)
	if err != nil {
		return fmt.Errorf("failed to prepare statement for %s: %w", tableName, err)
	}
	defer stmt.Close()

	// Log progress for large inserts
	logInterval := min(max(count/10, 1000), 100000)

	for i := range count {
		if err := insertFn(stmt, i); err != nil {
			v.log.Error("failed to insert row", "table", tableName, "row", i, "total", count, "error", err)
			return fmt.Errorf("failed to insert into %s: %w", tableName, err)
		}
		if (i+1)%logInterval == 0 || i == count-1 {
			v.log.Debug("insert progress", "table", tableName, "inserted", i+1, "total", count, "percent", float64(i+1)*100.0/float64(count))
		}
	}

	commitStart := time.Now()
	v.log.Info("telemetry: committing transaction", "table", tableName, "rows", count, "tx_duration", time.Since(txStart).String(), "commit_start_time", commitStart)
	if err := tx.Commit(); err != nil {
		txDuration := time.Since(txStart)
		v.log.Error("telemetry: transaction commit failed", "table", tableName, "error", err, "tx_duration", txDuration.String())
		return fmt.Errorf("failed to commit transaction for %s: %w", tableName, err)
	}
	commitDuration := time.Since(commitStart)
	v.log.Info("telemetry: transaction committed", "table", tableName, "commit_duration", commitDuration.String(), "total_tx_duration", time.Since(txStart).String())

	v.log.Debug("telemetry: table refreshed", "table", tableName, "rows", count)
	return nil
}

func getFileSize(f *os.File) int64 {
	if info, err := f.Stat(); err == nil {
		return info.Size()
	}
	return 0
}
