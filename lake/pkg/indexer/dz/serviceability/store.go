package dzsvc

import (
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"

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

// SCD2ConfigContributors returns the base SCD2 config for contributors table
func SCD2ConfigContributors() duck.SCDTableConfig {
	return duck.SCDTableConfig{
		TableBaseName:       "dz_contributors",
		PrimaryKeyColumns:   []string{"pk:VARCHAR"},
		PayloadColumns:      []string{"code:VARCHAR", "name:VARCHAR"},
		MissingMeansDeleted: true,
		TrackIngestRuns:     false,
	}
}

func (s *Store) ReplaceContributors(ctx context.Context, contributors []Contributor) error {
	s.log.Debug("serviceability/store: replacing contributors", "count", len(contributors))
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()

	fetchedAt := time.Now().UTC()
	cfg := SCD2ConfigContributors()
	cfg.SnapshotTS = fetchedAt
	cfg.RunID = fmt.Sprintf("contributors_%d", fetchedAt.Unix())

	return duck.SCDTableViaCSV(ctx, s.log, conn, cfg, len(contributors), func(w *csv.Writer, i int) error {
		c := contributors[i]
		return w.Write([]string{c.PK, c.Code, c.Name})
	})
}

// SCD2ConfigDevices returns the base SCD2 config for devices table
func SCD2ConfigDevices() duck.SCDTableConfig {
	return duck.SCDTableConfig{
		TableBaseName:       "dz_devices",
		PrimaryKeyColumns:   []string{"pk:VARCHAR"},
		PayloadColumns:      []string{"status:VARCHAR", "device_type:VARCHAR", "code:VARCHAR", "public_ip:VARCHAR", "contributor_pk:VARCHAR", "metro_pk:VARCHAR", "max_users:INTEGER"},
		MissingMeansDeleted: true,
		TrackIngestRuns:     false,
	}
}

func (s *Store) ReplaceDevices(ctx context.Context, devices []Device) error {
	s.log.Debug("serviceability/store: replacing devices", "count", len(devices))
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()

	fetchedAt := time.Now().UTC()
	cfg := SCD2ConfigDevices()
	cfg.SnapshotTS = fetchedAt
	cfg.RunID = fmt.Sprintf("devices_%d", fetchedAt.Unix())

	return duck.SCDTableViaCSV(ctx, s.log, conn, cfg, len(devices), func(w *csv.Writer, i int) error {
		d := devices[i]
		return w.Write([]string{d.PK, d.Status, d.DeviceType, d.Code, d.PublicIP, d.ContributorPK, d.MetroPK, fmt.Sprintf("%d", d.MaxUsers)})
	})
}

// SCD2ConfigUsers returns the base SCD2 config for users table
func SCD2ConfigUsers() duck.SCDTableConfig {
	return duck.SCDTableConfig{
		TableBaseName:       "dz_users",
		PrimaryKeyColumns:   []string{"pk:VARCHAR"},
		PayloadColumns:      []string{"owner_pk:VARCHAR", "status:VARCHAR", "kind:VARCHAR", "client_ip:VARCHAR", "dz_ip:VARCHAR", "device_pk:VARCHAR", "tunnel_id:INTEGER"},
		MissingMeansDeleted: true,
		TrackIngestRuns:     false,
	}
}

func (s *Store) ReplaceUsers(ctx context.Context, users []User) error {
	s.log.Debug("serviceability/store: replacing users", "count", len(users))
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()

	fetchedAt := time.Now().UTC()
	cfg := SCD2ConfigUsers()
	cfg.SnapshotTS = fetchedAt
	cfg.RunID = fmt.Sprintf("users_%d", fetchedAt.Unix())

	return duck.SCDTableViaCSV(ctx, s.log, conn, cfg, len(users), func(w *csv.Writer, i int) error {
		u := users[i]
		return w.Write([]string{u.PK, u.OwnerPK, u.Status, u.Kind, u.ClientIP.String(), u.DZIP.String(), u.DevicePK, fmt.Sprintf("%d", u.TunnelID)})
	})
}

// SCD2ConfigMetros returns the base SCD2 config for metros table
func SCD2ConfigMetros() duck.SCDTableConfig {
	return duck.SCDTableConfig{
		TableBaseName:       "dz_metros",
		PrimaryKeyColumns:   []string{"pk:VARCHAR"},
		PayloadColumns:      []string{"code:VARCHAR", "name:VARCHAR", "longitude:DOUBLE", "latitude:DOUBLE"},
		MissingMeansDeleted: true,
		TrackIngestRuns:     false,
	}
}

func (s *Store) ReplaceMetros(ctx context.Context, metros []Metro) error {
	s.log.Debug("serviceability/store: replacing metros", "count", len(metros))
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()

	fetchedAt := time.Now().UTC()
	cfg := SCD2ConfigMetros()
	cfg.SnapshotTS = fetchedAt
	cfg.RunID = fmt.Sprintf("metros_%d", fetchedAt.Unix())

	return duck.SCDTableViaCSV(ctx, s.log, conn, cfg, len(metros), func(w *csv.Writer, i int) error {
		m := metros[i]
		return w.Write([]string{m.PK, m.Code, m.Name, fmt.Sprintf("%.6f", m.Longitude), fmt.Sprintf("%.6f", m.Latitude)})
	})
}

// SCD2ConfigLinks returns the base SCD2 config for links table
func SCD2ConfigLinks() duck.SCDTableConfig {
	return duck.SCDTableConfig{
		TableBaseName:       "dz_links",
		PrimaryKeyColumns:   []string{"pk:VARCHAR"},
		PayloadColumns:      []string{"status:VARCHAR", "code:VARCHAR", "tunnel_net:VARCHAR", "contributor_pk:VARCHAR", "side_a_pk:VARCHAR", "side_z_pk:VARCHAR", "side_a_iface_name:VARCHAR", "side_z_iface_name:VARCHAR", "link_type:VARCHAR", "committed_rtt_ns:BIGINT", "committed_jitter_ns:BIGINT", "bandwidth_bps:BIGINT", "isis_delay_override_ns:BIGINT"},
		MissingMeansDeleted: true,
		TrackIngestRuns:     false,
	}
}

func (s *Store) ReplaceLinks(ctx context.Context, links []Link) error {
	s.log.Debug("serviceability/store: replacing links", "count", len(links))
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()

	fetchedAt := time.Now().UTC()
	cfg := SCD2ConfigLinks()
	cfg.SnapshotTS = fetchedAt
	cfg.RunID = fmt.Sprintf("links_%d", fetchedAt.Unix())

	return duck.SCDTableViaCSV(ctx, s.log, conn, cfg, len(links), func(w *csv.Writer, i int) error {
		l := links[i]
		return w.Write([]string{
			l.PK, l.Status, l.Code, l.TunnelNet, l.ContributorPK, l.SideAPK, l.SideZPK,
			l.SideAIfaceName, l.SideZIfaceName, l.LinkType,
			fmt.Sprintf("%d", l.CommittedRTTNs), fmt.Sprintf("%d", l.CommittedJitterNs), fmt.Sprintf("%d", l.Bandwidth),
			fmt.Sprintf("%d", l.ISISDelayOverrideNs),
		})
	})
}

func (s *Store) GetDevices() ([]Device, error) {
	ctx := context.Background()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()
	query := `SELECT pk, status, device_type, code, public_ip, contributor_pk, metro_pk, max_users FROM dz_devices_current ORDER BY code`
	rows, err := conn.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query devices: %w", err)
	}
	defer rows.Close()

	var devices []Device
	for rows.Next() {
		var d Device
		if err := rows.Scan(&d.PK, &d.Status, &d.DeviceType, &d.Code, &d.PublicIP, &d.ContributorPK, &d.MetroPK, &d.MaxUsers); err != nil {
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
	query := `SELECT pk, status, code, tunnel_net, contributor_pk, side_a_pk, side_z_pk, side_a_iface_name, side_z_iface_name, link_type, committed_rtt_ns, committed_jitter_ns, bandwidth_bps, isis_delay_override_ns FROM dz_links_current ORDER BY code`
	rows, err := conn.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query links: %w", err)
	}
	defer rows.Close()

	var links []Link
	for rows.Next() {
		var l Link
		if err := rows.Scan(&l.PK, &l.Status, &l.Code, &l.TunnelNet, &l.ContributorPK, &l.SideAPK, &l.SideZPK, &l.SideAIfaceName, &l.SideZIfaceName, &l.LinkType, &l.CommittedRTTNs, &l.CommittedJitterNs, &l.Bandwidth, &l.ISISDelayOverrideNs); err != nil {
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
	query := `SELECT pk, code, name FROM dz_contributors_current ORDER BY code`
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
	query := `SELECT pk, code, name, longitude, latitude FROM dz_metros_current ORDER BY code`
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

func (s *Store) GetUsers(ctx context.Context) ([]User, error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()
	query := `SELECT pk, owner_pk, status, kind, client_ip, dz_ip, device_pk, tunnel_id FROM dz_users_current`
	rows, err := conn.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query users: %w", err)
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		var clientIPStr, dzIPStr string
		if err := rows.Scan(&u.PK, &u.OwnerPK, &u.Status, &u.Kind, &clientIPStr, &dzIPStr, &u.DevicePK, &u.TunnelID); err != nil {
			return nil, fmt.Errorf("failed to scan user: %w", err)
		}
		if clientIPStr != "" {
			u.ClientIP = net.ParseIP(clientIPStr)
		}
		if dzIPStr != "" {
			u.DZIP = net.ParseIP(dzIPStr)
		}
		users = append(users, u)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating users: %w", err)
	}

	return users, nil
}
