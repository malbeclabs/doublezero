package geoip

import (
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"log/slog"
	"net"

	"github.com/malbeclabs/doublezero/tools/dz-ai/internal/data/duck"
	"github.com/malbeclabs/doublezero/tools/maxmind/pkg/geoip"
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
		`CREATE TABLE IF NOT EXISTS ` + tablePrefix + `geoip_records (
			ip VARCHAR,
			country_code VARCHAR,
			country VARCHAR,
			region VARCHAR,
			city VARCHAR,
			city_id INTEGER,
			metro_name VARCHAR,
			latitude DOUBLE,
			longitude DOUBLE,
			postal_code VARCHAR,
			time_zone VARCHAR,
			accuracy_radius INTEGER,
			asn BIGINT,
			asn_org VARCHAR,
			is_anycast BOOLEAN,
			is_anonymous_proxy BOOLEAN,
			is_satellite_provider BOOLEAN
		)`,
	}
	ctx := context.Background()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()
	for _, sql := range sqls {
		if _, err := conn.ExecContext(ctx, sql); err != nil {
			return fmt.Errorf("failed to create table: %w", err)
		}
	}
	return nil
}

func (s *Store) UpsertRecords(ctx context.Context, records []*geoip.Record) error {
	s.log.Debug("geoip/store: upserting records", "count", len(records))
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()
	return duck.UpsertTableViaCSV(ctx, s.log, conn, "geoip_records", len(records), func(w *csv.Writer, i int) error {
		r := records[i]
		if r == nil {
			return fmt.Errorf("record at index %d is nil", i)
		}
		ipStr := ""
		if r.IP != nil {
			ipStr = r.IP.String()
		}
		return w.Write([]string{
			ipStr,
			r.CountryCode,
			r.Country,
			r.Region,
			r.City,
			fmt.Sprintf("%d", r.CityID),
			r.MetroName,
			fmt.Sprintf("%.6f", r.Latitude),
			fmt.Sprintf("%.6f", r.Longitude),
			r.PostalCode,
			r.TimeZone,
			fmt.Sprintf("%d", r.AccuracyRadius),
			fmt.Sprintf("%d", r.ASN),
			r.ASNOrg,
			fmt.Sprintf("%t", r.IsAnycast),
			fmt.Sprintf("%t", r.IsAnonymousProxy),
			fmt.Sprintf("%t", r.IsSatelliteProvider),
		})
	}, []string{"ip"})
}

func (s *Store) GetRecord(ip net.IP) (*geoip.Record, error) {
	if ip == nil {
		return nil, fmt.Errorf("ip is nil")
	}
	ipStr := ip.String()

	ctx := context.Background()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()
	query := `SELECT ip, country_code, country, region, city, city_id, metro_name, latitude, longitude,
	          postal_code, time_zone, accuracy_radius, asn, asn_org,
	          is_anycast, is_anonymous_proxy, is_satellite_provider
	          FROM geoip_records WHERE ip = ?`
	row := conn.QueryRowContext(ctx, query, ipStr)

	var record geoip.Record
	var ipStrFromDB string
	var countryCode sql.NullString
	var country sql.NullString
	var region sql.NullString
	var city sql.NullString
	var cityID sql.NullInt64
	var metroName sql.NullString
	var postalCode sql.NullString
	var timeZone sql.NullString
	var accuracyRadius sql.NullInt64
	var asn sql.NullInt64
	var asnOrg sql.NullString
	var isAnycast sql.NullBool
	var isAnonymousProxy sql.NullBool
	var isSatelliteProvider sql.NullBool

	err = row.Scan(
		&ipStrFromDB,
		&countryCode,
		&country,
		&region,
		&city,
		&cityID,
		&metroName,
		&record.Latitude,
		&record.Longitude,
		&postalCode,
		&timeZone,
		&accuracyRadius,
		&asn,
		&asnOrg,
		&isAnycast,
		&isAnonymousProxy,
		&isSatelliteProvider,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to scan record: %w", err)
	}

	record.IP = net.ParseIP(ipStrFromDB)
	if countryCode.Valid {
		record.CountryCode = countryCode.String
	}
	if country.Valid {
		record.Country = country.String
	}
	if region.Valid {
		record.Region = region.String
	}
	if city.Valid {
		record.City = city.String
	}
	if cityID.Valid {
		record.CityID = int(cityID.Int64)
	}
	if metroName.Valid {
		record.MetroName = metroName.String
	}
	if postalCode.Valid {
		record.PostalCode = postalCode.String
	}
	if timeZone.Valid {
		record.TimeZone = timeZone.String
	}
	if accuracyRadius.Valid {
		record.AccuracyRadius = int(accuracyRadius.Int64)
	}
	if asn.Valid {
		record.ASN = uint(asn.Int64)
	}
	if asnOrg.Valid {
		record.ASNOrg = asnOrg.String
	}
	if isAnycast.Valid {
		record.IsAnycast = isAnycast.Bool
	}
	if isAnonymousProxy.Valid {
		record.IsAnonymousProxy = isAnonymousProxy.Bool
	}
	if isSatelliteProvider.Valid {
		record.IsSatelliteProvider = isSatelliteProvider.Bool
	}

	return &record, nil
}

func (s *Store) GetRecords() ([]*geoip.Record, error) {
	ctx := context.Background()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()
	query := `SELECT ip, country_code, country, region, city, city_id, metro_name, latitude, longitude,
	          postal_code, time_zone, accuracy_radius, asn, asn_org,
	          is_anycast, is_anonymous_proxy, is_satellite_provider
	          FROM geoip_records`
	rows, err := conn.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query records: %w", err)
	}
	defer rows.Close()

	records := make([]*geoip.Record, 0)
	for rows.Next() {
		var record geoip.Record
		var ipStr string
		var countryCode sql.NullString
		var country sql.NullString
		var region sql.NullString
		var city sql.NullString
		var cityID sql.NullInt64
		var metroName sql.NullString
		var postalCode sql.NullString
		var timeZone sql.NullString
		var accuracyRadius sql.NullInt64
		var asn sql.NullInt64
		var asnOrg sql.NullString
		var isAnycast sql.NullBool
		var isAnonymousProxy sql.NullBool
		var isSatelliteProvider sql.NullBool

		if err := rows.Scan(
			&ipStr,
			&countryCode,
			&country,
			&region,
			&city,
			&cityID,
			&metroName,
			&record.Latitude,
			&record.Longitude,
			&postalCode,
			&timeZone,
			&accuracyRadius,
			&asn,
			&asnOrg,
			&isAnycast,
			&isAnonymousProxy,
			&isSatelliteProvider,
		); err != nil {
			return nil, fmt.Errorf("failed to scan record: %w", err)
		}

		record.IP = net.ParseIP(ipStr)
		if countryCode.Valid {
			record.CountryCode = countryCode.String
		}
		if country.Valid {
			record.Country = country.String
		}
		if region.Valid {
			record.Region = region.String
		}
		if city.Valid {
			record.City = city.String
		}
		if cityID.Valid {
			record.CityID = int(cityID.Int64)
		}
		if metroName.Valid {
			record.MetroName = metroName.String
		}
		if postalCode.Valid {
			record.PostalCode = postalCode.String
		}
		if timeZone.Valid {
			record.TimeZone = timeZone.String
		}
		if accuracyRadius.Valid {
			record.AccuracyRadius = int(accuracyRadius.Int64)
		}
		if asn.Valid {
			record.ASN = uint(asn.Int64)
		}
		if asnOrg.Valid {
			record.ASNOrg = asnOrg.String
		}
		if isAnycast.Valid {
			record.IsAnycast = isAnycast.Bool
		}
		if isAnonymousProxy.Valid {
			record.IsAnonymousProxy = isAnonymousProxy.Bool
		}
		if isSatelliteProvider.Valid {
			record.IsSatelliteProvider = isSatelliteProvider.Bool
		}

		records = append(records, &record)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating records: %w", err)
	}

	return records, nil
}
