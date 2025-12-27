package geoip

import schematypes "github.com/malbeclabs/doublezero/tools/dz-ai/internal/data/indexer/schema"

var Schema = &schematypes.Schema{
	Name: "geoip",
	Description: `
Use for IP geolocation and ASN information:
- IP address geolocation (country, region, city, coordinates)
- ASN (Autonomous System Number) and organization
- Network characteristics (anycast, proxy, satellite)

Join to other tables:
- geoip_records.ip = solana_gossip_nodes.gossip_ip
- geoip_records.ip = dz_users.client_ip
`,
	Tables: []schematypes.TableInfo{
		{
			Name:        "geoip_records",
			Description: "IP geolocation and ASN records from MaxMind GeoIP2 databases. Primary key: ip. Join to Solana nodes via geoip_records.ip = solana_gossip_nodes.gossip_ip. Join to DZ users via geoip_records.ip = dz_users.client_ip.",
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
