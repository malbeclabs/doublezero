package geoip

import schematypes "github.com/malbeclabs/doublezero/lake/pkg/indexer/schema"

var Schema = &schematypes.Schema{
	Name: "geoip",
	Description: `
Use for IP geolocation and ASN information:
- IP address geolocation (country, region, city, coordinates)
- ASN (Autonomous System Number) and organization
- Network characteristics (anycast, proxy, satellite)

SCD2 TABLE STRUCTURE (CRITICAL):
The geoip_records table uses SCD2 (Slowly Changing Dimension Type 2) pattern:
- geoip_records_current: One row per IP address with the most recent version. Contains: ip (primary key), payload columns, as_of_ts (timestamp of snapshot), row_hash (hash for change detection).
- geoip_records_history: Append-only historical versions with validity windows. Contains: ip (primary key), payload columns, valid_from (timestamp), valid_to (timestamp, NULL for current), row_hash, op (I|U|D), run_id (optional).
- Always query geoip_records_current for current state. Query geoip_records_history only for historical analysis or point-in-time queries.
- All joins should use geoip_records_current unless explicitly doing historical analysis.

Join to other tables:
- geoip_records_current.ip = solana_gossip_nodes_current.gossip_ip
- geoip_records_current.ip = dz_users_current.client_ip
`,
	Tables: []schematypes.TableInfo{
		{
			Name:        "geoip_records",
			Description: "IP geolocation and ASN records from MaxMind GeoIP2 databases (SCD2). STRUCTURE: geoip_records_current (one row per ip with latest version + as_of_ts, row_hash) and geoip_records_history (historical versions with valid_from, valid_to, op, run_id). USAGE: Always query geoip_records_current for current state. Primary key: ip. Join to Solana nodes via geoip_records_current.ip = solana_gossip_nodes_current.gossip_ip. Join to DZ users via geoip_records_current.ip = dz_users_current.client_ip.",
			Columns: []schematypes.ColumnInfo{
				{Name: "ip", Type: "VARCHAR", Description: "IP address (primary key)"},
				{Name: "country_code", Type: "VARCHAR", Description: "ISO 3166-1 alpha-2 country code (e.g. US, GB, DE)"},
				{Name: "country", Type: "VARCHAR", Description: "Country name"},
				{Name: "region", Type: "VARCHAR", Description: "State/province/region name"},
				{Name: "city", Type: "VARCHAR", Description: "City name"},
				{Name: "city_id", Type: "INTEGER", Description: "MaxMind GeoName ID for the city"},
				{Name: "metro_name", Type: "VARCHAR", Description: "Metro name (e.g. New York, London, Frankfurt)"},
				{Name: "latitude", Type: "DOUBLE", Description: "Latitude"},
				{Name: "longitude", Type: "DOUBLE", Description: "Longitude"},
				{Name: "postal_code", Type: "VARCHAR", Description: "Postal/ZIP code"},
				{Name: "time_zone", Type: "VARCHAR", Description: "IANA time zone (e.g. America/New_York)"},
				{Name: "accuracy_radius", Type: "INTEGER", Description: "Accuracy radius in kilometers"},
				{Name: "asn", Type: "BIGINT", Description: "Autonomous System Number"},
				{Name: "asn_org", Type: "VARCHAR", Description: "ASN organization name"},
				{Name: "is_anycast", Type: "BOOLEAN", Description: "True if IP is anycast"},
				{Name: "is_anonymous_proxy", Type: "BOOLEAN", Description: "True if IP is an anonymous proxy"},
				{Name: "is_satellite_provider", Type: "BOOLEAN", Description: "True if IP is from a satellite provider"},
			},
		},
	},
}
