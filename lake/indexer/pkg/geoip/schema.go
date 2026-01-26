package geoip

import (
	"log/slog"

	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse/dataset"
	"github.com/malbeclabs/doublezero/tools/maxmind/pkg/geoip"
)

// GeoIPRecordSchema defines the schema for GeoIP records
type GeoIPRecordSchema struct{}

func (s *GeoIPRecordSchema) Name() string {
	return "geoip_records"
}

func (s *GeoIPRecordSchema) PrimaryKeyColumns() []string {
	return []string{"ip:VARCHAR"}
}

func (s *GeoIPRecordSchema) PayloadColumns() []string {
	return []string{
		"country_code:VARCHAR",
		"country:VARCHAR",
		"region:VARCHAR",
		"city:VARCHAR",
		"city_id:INTEGER",
		"metro_name:VARCHAR",
		"latitude:DOUBLE",
		"longitude:DOUBLE",
		"postal_code:VARCHAR",
		"time_zone:VARCHAR",
		"accuracy_radius:INTEGER",
		"asn:BIGINT",
		"asn_org:VARCHAR",
		"is_anycast:BOOLEAN",
		"is_anonymous_proxy:BOOLEAN",
		"is_satellite_provider:BOOLEAN",
	}
}

func (s *GeoIPRecordSchema) ToRow(r *geoip.Record) []any {
	if r == nil {
		return nil
	}
	ipStr := ""
	if r.IP != nil {
		ipStr = r.IP.String()
	}
	return []any{
		ipStr,
		r.CountryCode,
		r.Country,
		r.Region,
		r.City,
		r.CityID,
		r.MetroName,
		r.Latitude,
		r.Longitude,
		r.PostalCode,
		r.TimeZone,
		r.AccuracyRadius,
		r.ASN,
		r.ASNOrg,
		r.IsAnycast,
		r.IsAnonymousProxy,
		r.IsSatelliteProvider,
	}
}

func (s *GeoIPRecordSchema) GetPrimaryKey(r *geoip.Record) string {
	if r == nil || r.IP == nil {
		return ""
	}
	return r.IP.String()
}

var geoIPRecordSchema = &GeoIPRecordSchema{}

func NewGeoIPRecordDataset(log *slog.Logger) (*dataset.DimensionType2Dataset, error) {
	return dataset.NewDimensionType2Dataset(log, geoIPRecordSchema)
}
