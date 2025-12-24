package dzsvc

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/gagliardetto/solana-go"
	"github.com/jonboulle/clockwork"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/malbeclabs/doublezero/tools/mcp/internal/duck"
)

type Contributor struct {
	PK   string
	Code string
	Name string
}

var (
	contributorNamesByCode = map[string]string{
		"jump_":    "Jump Crypto",
		"dgt":      "Distributed Global",
		"cherry":   "Cherry Servers",
		"cdrw":     "CDRW",
		"glxy":     "Galaxy",
		"latitude": "Latitude",
		"rox":      "RockawayX",
		"s3v":      "S3V",
		"stakefac": "Staking Facilities",
	}
)

type Device struct {
	PK            string
	Status        string
	DeviceType    string
	Code          string
	PublicIP      string
	ContributorPK string
	MetroPK       string
}

type Metro struct {
	PK        string
	Code      string
	Name      string
	Longitude float64
	Latitude  float64
}

type Link struct {
	PK             string
	Status         string
	Code           string
	TunnelNet      string
	ContributorPK  string
	SideAPK        string
	SideZPK        string
	SideAIfaceName string
	SideZIfaceName string
	LinkType       string
	DelayNs        uint64
	JitterNs       uint64
	Bandwidth      uint64
}

type User struct {
	PK       string
	OwnerPK  string
	Status   string
	Kind     string
	ClientIP net.IP
	DZIP     net.IP
	DevicePK string
}

type ServiceabilityRPC interface {
	GetProgramData(ctx context.Context) (*serviceability.ProgramData, error)
}

type ViewConfig struct {
	Logger            *slog.Logger
	Clock             clockwork.Clock
	ServiceabilityRPC ServiceabilityRPC
	RefreshInterval   time.Duration
	DB                duck.DB
}

func (cfg *ViewConfig) Validate() error {
	if cfg.Logger == nil {
		return errors.New("logger is required")
	}
	if cfg.ServiceabilityRPC == nil {
		return errors.New("serviceability rpc is required")
	}
	if cfg.DB == nil {
		return errors.New("database is required")
	}
	if cfg.RefreshInterval <= 0 {
		return errors.New("refresh interval must be greater than 0")
	}

	if cfg.Clock == nil {
		cfg.Clock = clockwork.NewRealClock()
	}
	return nil
}

type View struct {
	log       *slog.Logger
	cfg       ViewConfig
	db        duck.DB
	refreshMu sync.Mutex // prevents concurrent refreshes

	fetchedAt time.Time
	readyOnce sync.Once
	readyCh   chan struct{}
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

func (v *View) Close() error {
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
		return fmt.Errorf("context cancelled while waiting for serviceability view: %w", ctx.Err())
	}
}

func (v *View) Start(ctx context.Context) {
	go func() {
		v.log.Info("serviceability: starting refresh loop", "interval", v.cfg.RefreshInterval)

		if err := v.Refresh(ctx); err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			v.log.Error("serviceability: initial refresh failed", "error", err)
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
					v.log.Error("serviceability: refresh failed", "error", err)
				}
			}
		}
	}()
}

func (v *View) Refresh(ctx context.Context) error {
	v.refreshMu.Lock()
	defer v.refreshMu.Unlock()

	refreshStart := time.Now()
	v.log.Info("serviceability: refresh started", "start_time", refreshStart)
	defer func() {
		duration := time.Since(refreshStart)
		v.log.Info("serviceability: refresh completed", "duration", duration.String())
	}()

	v.log.Debug("serviceability: starting refresh")

	pd, err := v.cfg.ServiceabilityRPC.GetProgramData(ctx)
	if err != nil {
		return err
	}

	v.log.Debug("serviceability: fetched program data",
		"contributors", len(pd.Contributors),
		"devices", len(pd.Devices),
		"users", len(pd.Users),
		"links", len(pd.Links),
		"metros", len(pd.Exchanges))

	contributors := convertContributors(pd.Contributors)
	devices := convertDevices(pd.Devices)
	users := convertUsers(pd.Users)
	links := convertLinks(pd.Links)
	metros := convertMetros(pd.Exchanges)

	fetchedAt := time.Now().UTC()

	v.log.Debug("serviceability: refreshing contributors", "count", len(contributors))
	if err := v.refreshTable("dz_contributors", "DELETE FROM dz_contributors", "INSERT INTO dz_contributors (pk, code, name) VALUES (?, ?, ?)", len(contributors), func(stmt *sql.Stmt, i int) error {
		c := contributors[i]
		_, err := stmt.Exec(c.PK, c.Code, c.Name)
		return err
	}); err != nil {
		return fmt.Errorf("failed to refresh contributors: %w", err)
	}

	v.log.Debug("serviceability: refreshing devices", "count", len(devices))
	if err := v.refreshTable("dz_devices", "DELETE FROM dz_devices", "INSERT INTO dz_devices (pk, status, device_type, code, public_ip, contributor_pk, metro_pk) VALUES (?, ?, ?, ?, ?, ?, ?)", len(devices), func(stmt *sql.Stmt, i int) error {
		d := devices[i]
		_, err := stmt.Exec(d.PK, d.Status, d.DeviceType, d.Code, d.PublicIP, d.ContributorPK, d.MetroPK)
		return err
	}); err != nil {
		return fmt.Errorf("failed to refresh devices: %w", err)
	}

	v.log.Debug("serviceability: refreshing users", "count", len(users))
	if err := v.refreshTable("dz_users", "DELETE FROM dz_users", "INSERT INTO dz_users (pk, owner_pk, status, kind, client_ip, dz_ip, device_pk) VALUES (?, ?, ?, ?, ?, ?, ?)", len(users), func(stmt *sql.Stmt, i int) error {
		u := users[i]
		_, err := stmt.Exec(u.PK, u.OwnerPK, u.Status, u.Kind, u.ClientIP.String(), u.DZIP.String(), u.DevicePK)
		return err
	}); err != nil {
		return fmt.Errorf("failed to refresh users: %w", err)
	}

	v.log.Debug("serviceability: refreshing metros", "count", len(metros))
	if err := v.refreshTable("dz_metros", "DELETE FROM dz_metros", "INSERT INTO dz_metros (pk, code, name, longitude, latitude) VALUES (?, ?, ?, ?, ?)", len(metros), func(stmt *sql.Stmt, i int) error {
		m := metros[i]
		_, err := stmt.Exec(m.PK, m.Code, m.Name, m.Longitude, m.Latitude)
		return err
	}); err != nil {
		return fmt.Errorf("failed to refresh metros: %w", err)
	}

	v.log.Debug("serviceability: refreshing links", "count", len(links))
	if err := v.refreshTable("dz_links", "DELETE FROM dz_links", "INSERT INTO dz_links (pk, status, code, tunnel_net, contributor_pk, side_a_pk, side_z_pk, side_a_iface_name, side_z_iface_name, link_type, delay_ns, jitter_ns, bandwidth_bps) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)", len(links), func(stmt *sql.Stmt, i int) error {
		l := links[i]
		_, err := stmt.Exec(l.PK, l.Status, l.Code, l.TunnelNet, l.ContributorPK, l.SideAPK, l.SideZPK, l.SideAIfaceName, l.SideZIfaceName, l.LinkType, l.DelayNs, l.JitterNs, l.Bandwidth)
		return err
	}); err != nil {
		return fmt.Errorf("failed to refresh links: %w", err)
	}

	v.fetchedAt = fetchedAt
	v.readyOnce.Do(func() {
		close(v.readyCh)
		v.log.Info("serviceability: view is now ready")
	})

	v.log.Debug("serviceability: refresh completed", "fetched_at", fetchedAt)
	return nil
}

func (v *View) refreshTable(tableName, deleteSQL, insertSQL string, count int, insertFn func(*sql.Stmt, int) error) error {
	tableRefreshStart := time.Now()
	v.log.Info("serviceability: refreshing table started", "table", tableName, "rows", count, "start_time", tableRefreshStart)
	defer func() {
		duration := time.Since(tableRefreshStart)
		v.log.Info("serviceability: refreshing table completed", "table", tableName, "duration", duration.String())
	}()

	v.log.Debug("serviceability: refreshing table", "table", tableName, "rows", count)

	txStart := time.Now()
	tx, err := v.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction for %s: %w", tableName, err)
	}
	v.log.Debug("serviceability: transaction begun", "table", tableName, "tx_start_time", txStart)
	defer tx.Rollback()

	if _, err := tx.Exec(deleteSQL); err != nil {
		return fmt.Errorf("failed to clear %s: %w", tableName, err)
	}

	if count == 0 {
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("failed to commit transaction for %s: %w", tableName, err)
		}
		v.log.Debug("serviceability: table refreshed (empty)", "table", tableName)
		return nil
	}

	stmt, err := tx.Prepare(insertSQL)
	if err != nil {
		return fmt.Errorf("failed to prepare statement for %s: %w", tableName, err)
	}
	defer stmt.Close()

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
	v.log.Info("serviceability: committing transaction", "table", tableName, "rows", count, "tx_duration", time.Since(txStart).String(), "commit_start_time", commitStart)
	if err := tx.Commit(); err != nil {
		txDuration := time.Since(txStart)
		v.log.Error("serviceability: transaction commit failed", "table", tableName, "error", err, "tx_duration", txDuration.String())
		return fmt.Errorf("failed to commit transaction for %s: %w", tableName, err)
	}
	commitDuration := time.Since(commitStart)
	v.log.Info("serviceability: transaction committed", "table", tableName, "commit_duration", commitDuration.String(), "total_tx_duration", time.Since(txStart).String())

	v.log.Debug("serviceability: table refreshed", "table", tableName, "rows", count)
	return nil
}

func (v *View) initDB() error {
	schemas := []string{
		`CREATE TABLE IF NOT EXISTS dz_contributors (
			pk VARCHAR PRIMARY KEY,
			code VARCHAR,
			name VARCHAR
		)`,
		`CREATE TABLE IF NOT EXISTS dz_devices (
			pk VARCHAR PRIMARY KEY,
			status VARCHAR,
			device_type VARCHAR,
			code VARCHAR,
			public_ip VARCHAR,
			contributor_pk VARCHAR,
			metro_pk VARCHAR
		)`,
		`CREATE TABLE IF NOT EXISTS dz_metros (
			pk VARCHAR PRIMARY KEY,
			code VARCHAR,
			name VARCHAR,
			longitude DOUBLE,
			latitude DOUBLE
		)`,
		`CREATE TABLE IF NOT EXISTS dz_links (
			pk VARCHAR PRIMARY KEY,
			status VARCHAR,
			code VARCHAR,
			tunnel_net VARCHAR,
			contributor_pk VARCHAR,
			side_a_pk VARCHAR,
			side_z_pk VARCHAR,
			side_a_iface_name VARCHAR,
			side_z_iface_name VARCHAR,
			link_type VARCHAR,
			delay_ns BIGINT,
			jitter_ns BIGINT,
			bandwidth_bps BIGINT
		)`,
		`CREATE TABLE IF NOT EXISTS dz_users (
			pk VARCHAR PRIMARY KEY,
			owner_pk VARCHAR,
			status VARCHAR,
			kind VARCHAR,
			client_ip VARCHAR,
			dz_ip VARCHAR,
			device_pk VARCHAR
		)`,
	}

	for _, schema := range schemas {
		if _, err := v.db.Exec(schema); err != nil {
			return fmt.Errorf("failed to create table: %w", err)
		}
	}

	return nil
}

func convertContributors(onchain []serviceability.Contributor) []Contributor {
	result := make([]Contributor, len(onchain))
	for i, contributor := range onchain {
		name := contributorNamesByCode[contributor.Code]
		result[i] = Contributor{
			PK:   solana.PublicKeyFromBytes(contributor.PubKey[:]).String(),
			Code: contributor.Code,
			Name: name, // Empty string if not in mapping
		}
	}
	return result
}

func convertDevices(onchain []serviceability.Device) []Device {
	result := make([]Device, len(onchain))
	for i, device := range onchain {
		result[i] = Device{
			PK:            solana.PublicKeyFromBytes(device.PubKey[:]).String(),
			Status:        device.Status.String(),
			DeviceType:    device.DeviceType.String(),
			Code:          device.Code,
			PublicIP:      net.IP(device.PublicIp[:]).String(),
			ContributorPK: solana.PublicKeyFromBytes(device.ContributorPubKey[:]).String(),
			MetroPK:       solana.PublicKeyFromBytes(device.ExchangePubKey[:]).String(),
		}
	}
	return result
}

func convertUsers(onchain []serviceability.User) []User {
	result := make([]User, len(onchain))
	for i, user := range onchain {
		result[i] = User{
			PK:       solana.PublicKeyFromBytes(user.PubKey[:]).String(),
			OwnerPK:  solana.PublicKeyFromBytes(user.Owner[:]).String(),
			Status:   user.Status.String(),
			Kind:     user.UserType.String(),
			ClientIP: net.IP(user.ClientIp[:]),
			DZIP:     net.IP(user.DzIp[:]),
			DevicePK: solana.PublicKeyFromBytes(user.DevicePubKey[:]).String(),
		}
	}
	return result
}

func convertLinks(onchain []serviceability.Link) []Link {
	result := make([]Link, len(onchain))
	for i, link := range onchain {
		tunnelNet := net.IPNet{
			IP:   net.IP(link.TunnelNet[:4]),
			Mask: net.CIDRMask(int(link.TunnelNet[4]), 32),
		}
		result[i] = Link{
			PK:             solana.PublicKeyFromBytes(link.PubKey[:]).String(),
			Status:         link.Status.String(),
			Code:           link.Code,
			SideAPK:        solana.PublicKeyFromBytes(link.SideAPubKey[:]).String(),
			SideZPK:        solana.PublicKeyFromBytes(link.SideZPubKey[:]).String(),
			ContributorPK:  solana.PublicKeyFromBytes(link.ContributorPubKey[:]).String(),
			SideAIfaceName: link.SideAIfaceName,
			SideZIfaceName: link.SideZIfaceName,
			TunnelNet:      tunnelNet.String(),
			LinkType:       link.LinkType.String(),
			DelayNs:        link.DelayNs,
			JitterNs:       link.JitterNs,
			Bandwidth:      link.Bandwidth,
		}
	}
	return result
}

func convertMetros(onchain []serviceability.Exchange) []Metro {
	result := make([]Metro, len(onchain))
	for i, exchange := range onchain {
		result[i] = Metro{
			PK:        solana.PublicKeyFromBytes(exchange.PubKey[:]).String(),
			Code:      exchange.Code,
			Name:      exchange.Name,
			Longitude: float64(exchange.Lng),
			Latitude:  float64(exchange.Lat),
		}
	}
	return result
}
