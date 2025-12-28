package dzsvc

import (
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"log/slog"

	"github.com/malbeclabs/doublezero/lake/pkg/duck"
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
	tablePrefix := s.db.Catalog() + "." + s.db.Schema() + "."
	sqls := []string{
		`CREATE TABLE IF NOT EXISTS ` + tablePrefix + `dz_contributors (
			pk VARCHAR,
			code VARCHAR,
			name VARCHAR
		)`,
		`CREATE TABLE IF NOT EXISTS ` + tablePrefix + `dz_devices (
			pk VARCHAR,
			status VARCHAR,
			device_type VARCHAR,
			code VARCHAR,
			public_ip VARCHAR,
			contributor_pk VARCHAR,
			metro_pk VARCHAR
		)`,
		`CREATE TABLE IF NOT EXISTS ` + tablePrefix + `dz_metros (
			pk VARCHAR,
			code VARCHAR,
			name VARCHAR,
			longitude DOUBLE,
			latitude DOUBLE
		)`,
		`CREATE TABLE IF NOT EXISTS ` + tablePrefix + `dz_links (
			pk VARCHAR,
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
			bandwidth_bps BIGINT,
			delay_override_ns BIGINT
		)`,
		`CREATE TABLE IF NOT EXISTS ` + tablePrefix + `dz_users (
			pk VARCHAR,
			owner_pk VARCHAR,
			status VARCHAR,
			kind VARCHAR,
			client_ip VARCHAR,
			dz_ip VARCHAR,
			device_pk VARCHAR,
			tunnel_id INTEGER
		)`,
	}
	ctx := context.Background()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()
	s.log.Debug("serviceability/store: creating tables", "count", len(sqls))
	for _, sql := range sqls {
		if _, err := conn.ExecContext(ctx, sql); err != nil {
			s.log.Error("serviceability/store: failed to create table", "error", err)
			return fmt.Errorf("failed to create table: %w", err)
		}
	}
	return nil
}

func (s *Store) ReplaceContributors(ctx context.Context, contributors []Contributor) error {
	s.log.Debug("serviceability/store: replacing contributors", "count", len(contributors))
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()
	return duck.ReplaceTableViaCSV(ctx, s.log, conn, "dz_contributors", len(contributors), func(w *csv.Writer, i int) error {
		c := contributors[i]
		return w.Write([]string{c.PK, c.Code, c.Name})
	}, []string{"pk"})
}

func (s *Store) ReplaceDevices(ctx context.Context, devices []Device) error {
	s.log.Debug("serviceability/store: replacing devices", "count", len(devices))
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()
	return duck.ReplaceTableViaCSV(ctx, s.log, conn, "dz_devices", len(devices), func(w *csv.Writer, i int) error {
		d := devices[i]
		return w.Write([]string{d.PK, d.Status, d.DeviceType, d.Code, d.PublicIP, d.ContributorPK, d.MetroPK})
	}, []string{"pk"})
}

func (s *Store) ReplaceUsers(ctx context.Context, users []User) error {
	s.log.Debug("serviceability/store: replacing users", "count", len(users))
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()
	return duck.ReplaceTableViaCSV(ctx, s.log, conn, "dz_users", len(users), func(w *csv.Writer, i int) error {
		u := users[i]
		return w.Write([]string{u.PK, u.OwnerPK, u.Status, u.Kind, u.ClientIP.String(), u.DZIP.String(), u.DevicePK, fmt.Sprintf("%d", u.TunnelID)})
	}, []string{"pk"})
}

func (s *Store) ReplaceMetros(ctx context.Context, metros []Metro) error {
	s.log.Debug("serviceability/store: replacing metros", "count", len(metros))
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()
	return duck.ReplaceTableViaCSV(ctx, s.log, conn, "dz_metros", len(metros), func(w *csv.Writer, i int) error {
		m := metros[i]
		return w.Write([]string{m.PK, m.Code, m.Name, fmt.Sprintf("%.6f", m.Longitude), fmt.Sprintf("%.6f", m.Latitude)})
	}, []string{"pk"})
}

func (s *Store) ReplaceLinks(ctx context.Context, links []Link) error {
	s.log.Debug("serviceability/store: replacing links", "count", len(links))
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()
	return duck.ReplaceTableViaCSV(ctx, s.log, conn, "dz_links", len(links), func(w *csv.Writer, i int) error {
		l := links[i]
		return w.Write([]string{
			l.PK, l.Status, l.Code, l.TunnelNet, l.ContributorPK, l.SideAPK, l.SideZPK,
			l.SideAIfaceName, l.SideZIfaceName, l.LinkType,
			fmt.Sprintf("%d", l.DelayNs), fmt.Sprintf("%d", l.JitterNs), fmt.Sprintf("%d", l.Bandwidth),
			fmt.Sprintf("%d", l.DelayOverrideNs),
		})
	}, []string{"pk"})
}

func (s *Store) GetDevices() ([]Device, error) {
	ctx := context.Background()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()
	query := `SELECT pk, status, device_type, code, public_ip, contributor_pk, metro_pk FROM dz_devices ORDER BY code`
	rows, err := conn.QueryContext(ctx, query)
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
	ctx := context.Background()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()
	query := `SELECT pk, status, code, tunnel_net, contributor_pk, side_a_pk, side_z_pk, side_a_iface_name, side_z_iface_name, link_type, delay_ns, jitter_ns, bandwidth_bps, delay_override_ns FROM dz_links ORDER BY code`
	rows, err := conn.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query links: %w", err)
	}
	defer rows.Close()

	var links []Link
	for rows.Next() {
		var l Link
		if err := rows.Scan(&l.PK, &l.Status, &l.Code, &l.TunnelNet, &l.ContributorPK, &l.SideAPK, &l.SideZPK, &l.SideAIfaceName, &l.SideZIfaceName, &l.LinkType, &l.DelayNs, &l.JitterNs, &l.Bandwidth, &l.DelayOverrideNs); err != nil {
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
	ctx := context.Background()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()
	query := `SELECT pk, code, name FROM dz_contributors ORDER BY code`
	rows, err := conn.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query contributors: %w", err)
	}
	defer rows.Close()

	var contributors []Contributor
	for rows.Next() {
		var c Contributor
		var name sql.NullString
		if err := rows.Scan(&c.PK, &c.Code, &name); err != nil {
			return nil, fmt.Errorf("failed to scan contributor: %w", err)
		}
		if name.Valid {
			c.Name = name.String
		}
		contributors = append(contributors, c)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating contributors: %w", err)
	}

	return contributors, nil
}

func (s *Store) GetMetros() ([]Metro, error) {
	ctx := context.Background()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()
	query := `SELECT pk, code, name, longitude, latitude FROM dz_metros ORDER BY code`
	rows, err := conn.QueryContext(ctx, query)
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
