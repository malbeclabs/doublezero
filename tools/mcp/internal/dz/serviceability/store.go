package dzsvc

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/malbeclabs/doublezero/tools/mcp/internal/duck"
)

type StoreConfig struct {
	Logger *slog.Logger
	DB     duck.DB
}

func (cfg *StoreConfig) Validate() error {
	if cfg.Logger == nil {
		return errors.New("logger is required")
	}
	if cfg.DB == nil {
		return errors.New("db is required")
	}
	return nil
}

type Store struct {
	log *slog.Logger
	cfg StoreConfig
	db  duck.DB
}

func NewStore(cfg StoreConfig) (*Store, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Store{
		log: cfg.Logger,
		cfg: cfg,
		db:  cfg.DB,
	}, nil
}

func (s *Store) CreateTablesIfNotExists() error {
	sqls := []string{
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
	for _, sql := range sqls {
		if _, err := s.db.Exec(sql); err != nil {
			return fmt.Errorf("failed to create table: %w", err)
		}
	}
	return nil
}

func (s *Store) ReplaceContributors(contributors []Contributor) error {
	s.log.Debug("serviceability/store: replacing contributors", "count", len(contributors))
	return s.replaceTable("dz_contributors", "DELETE FROM dz_contributors", "INSERT INTO dz_contributors (pk, code, name) VALUES (?, ?, ?)", len(contributors), func(stmt *sql.Stmt, i int) error {
		c := contributors[i]
		_, err := stmt.Exec(c.PK, c.Code, c.Name)
		return err
	})
}

func (s *Store) ReplaceDevices(devices []Device) error {
	s.log.Debug("serviceability/store: replacing devices", "count", len(devices))
	return s.replaceTable("dz_devices", "DELETE FROM dz_devices", "INSERT INTO dz_devices (pk, status, device_type, code, public_ip, contributor_pk, metro_pk) VALUES (?, ?, ?, ?, ?, ?, ?)", len(devices), func(stmt *sql.Stmt, i int) error {
		d := devices[i]
		_, err := stmt.Exec(d.PK, d.Status, d.DeviceType, d.Code, d.PublicIP, d.ContributorPK, d.MetroPK)
		return err
	})
}

func (s *Store) ReplaceUsers(users []User) error {
	s.log.Debug("serviceability/store: replacing users", "count", len(users))
	return s.replaceTable("dz_users", "DELETE FROM dz_users", "INSERT INTO dz_users (pk, owner_pk, status, kind, client_ip, dz_ip, device_pk) VALUES (?, ?, ?, ?, ?, ?, ?)", len(users), func(stmt *sql.Stmt, i int) error {
		u := users[i]
		_, err := stmt.Exec(u.PK, u.OwnerPK, u.Status, u.Kind, u.ClientIP.String(), u.DZIP.String(), u.DevicePK)
		return err
	})
}

func (s *Store) ReplaceMetros(metros []Metro) error {
	s.log.Debug("serviceability/store: replacing metros", "count", len(metros))
	return s.replaceTable("dz_metros", "DELETE FROM dz_metros", "INSERT INTO dz_metros (pk, code, name, longitude, latitude) VALUES (?, ?, ?, ?, ?)", len(metros), func(stmt *sql.Stmt, i int) error {
		m := metros[i]
		_, err := stmt.Exec(m.PK, m.Code, m.Name, m.Longitude, m.Latitude)
		return err
	})
}

func (s *Store) ReplaceLinks(links []Link) error {
	s.log.Debug("serviceability/store: replacing links", "count", len(links))
	return s.replaceTable("dz_links", "DELETE FROM dz_links", "INSERT INTO dz_links (pk, status, code, tunnel_net, contributor_pk, side_a_pk, side_z_pk, side_a_iface_name, side_z_iface_name, link_type, delay_ns, jitter_ns, bandwidth_bps) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)", len(links), func(stmt *sql.Stmt, i int) error {
		l := links[i]
		_, err := stmt.Exec(l.PK, l.Status, l.Code, l.TunnelNet, l.ContributorPK, l.SideAPK, l.SideZPK, l.SideAIfaceName, l.SideZIfaceName, l.LinkType, l.DelayNs, l.JitterNs, l.Bandwidth)
		return err
	})
}

func (s *Store) replaceTable(tableName, deleteSQL, insertSQL string, count int, insertFn func(*sql.Stmt, int) error) error {
	tableRefreshStart := time.Now()
	s.log.Info("serviceability: refreshing table started", "table", tableName, "rows", count, "start_time", tableRefreshStart)
	defer func() {
		duration := time.Since(tableRefreshStart)
		s.log.Info("serviceability: refreshing table completed", "table", tableName, "duration", duration.String())
	}()

	s.log.Debug("serviceability: refreshing table", "table", tableName, "rows", count)

	txStart := time.Now()
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction for %s: %w", tableName, err)
	}
	s.log.Debug("serviceability: transaction begun", "table", tableName, "tx_start_time", txStart)
	defer tx.Rollback()

	if _, err := tx.Exec(deleteSQL); err != nil {
		return fmt.Errorf("failed to clear %s: %w", tableName, err)
	}

	if count == 0 {
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("failed to commit transaction for %s: %w", tableName, err)
		}
		s.log.Debug("serviceability: table refreshed (empty)", "table", tableName)
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
			s.log.Error("failed to insert row", "table", tableName, "row", i, "total", count, "error", err)
			return fmt.Errorf("failed to insert into %s: %w", tableName, err)
		}
		if (i+1)%logInterval == 0 || i == count-1 {
			s.log.Debug("insert progress", "table", tableName, "inserted", i+1, "total", count, "percent", float64(i+1)*100.0/float64(count))
		}
	}

	commitStart := time.Now()
	s.log.Info("serviceability: committing transaction", "table", tableName, "rows", count, "tx_duration", time.Since(txStart).String(), "commit_start_time", commitStart)
	if err := tx.Commit(); err != nil {
		txDuration := time.Since(txStart)
		s.log.Error("serviceability: transaction commit failed", "table", tableName, "error", err, "tx_duration", txDuration.String())
		return fmt.Errorf("failed to commit transaction for %s: %w", tableName, err)
	}
	commitDuration := time.Since(commitStart)
	s.log.Info("serviceability: transaction committed", "table", tableName, "commit_duration", commitDuration.String(), "total_tx_duration", time.Since(txStart).String())

	s.log.Debug("serviceability: table refreshed", "table", tableName, "rows", count)
	return nil
}

func (s *Store) GetDevices() ([]Device, error) {
	query := `SELECT pk, status, device_type, code, public_ip, contributor_pk, metro_pk FROM dz_devices`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query devices: %w", err)
	}
	defer rows.Close()

	var devices []Device
	for rows.Next() {
		var d Device
		if err := rows.Scan(&d.PK, &d.Status, &d.DeviceType, &d.Code, &d.PublicIP, &d.ContributorPK, &d.MetroPK); err != nil {
			return nil, fmt.Errorf("failed to scan device: %w", err)
		}
		devices = append(devices, d)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating devices: %w", err)
	}

	return devices, nil
}

func (s *Store) GetLinks() ([]Link, error) {
	query := `SELECT pk, status, code, tunnel_net, contributor_pk, side_a_pk, side_z_pk, side_a_iface_name, side_z_iface_name, link_type, delay_ns, jitter_ns, bandwidth_bps FROM dz_links`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query links: %w", err)
	}
	defer rows.Close()

	var links []Link
	for rows.Next() {
		var l Link
		if err := rows.Scan(&l.PK, &l.Status, &l.Code, &l.TunnelNet, &l.ContributorPK, &l.SideAPK, &l.SideZPK, &l.SideAIfaceName, &l.SideZIfaceName, &l.LinkType, &l.DelayNs, &l.JitterNs, &l.Bandwidth); err != nil {
			return nil, fmt.Errorf("failed to scan link: %w", err)
		}
		links = append(links, l)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating links: %w", err)
	}

	return links, nil
}

func (s *Store) GetContributors() ([]Contributor, error) {
	query := `SELECT pk, code, name FROM dz_contributors`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query contributors: %w", err)
	}
	defer rows.Close()

	var contributors []Contributor
	for rows.Next() {
		var c Contributor
		if err := rows.Scan(&c.PK, &c.Code, &c.Name); err != nil {
			return nil, fmt.Errorf("failed to scan contributor: %w", err)
		}
		contributors = append(contributors, c)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating contributors: %w", err)
	}

	return contributors, nil
}

func (s *Store) GetMetros() ([]Metro, error) {
	query := `SELECT pk, code, name, longitude, latitude FROM dz_metros`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query metros: %w", err)
	}
	defer rows.Close()

	var metros []Metro
	for rows.Next() {
		var m Metro
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
