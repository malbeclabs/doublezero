package geoip

import schematypes "github.com/malbeclabs/doublezero/lake/pkg/indexer/schema"

var Datasets = []schematypes.Dataset{
	{
		Name:        "geoip_records",
		DatasetType: schematypes.DatasetTypeSCD2,
		Purpose: `
			IP geolocation and ASN records from MaxMind GeoIP2 databases.
			Contains country, region, city, coordinates, timezone, ASN, and network characteristics (anycast, proxy, satellite).
			Use for IP geolocation and ASN analysis of DZ users and Solana nodes.
		`,
		Tables: []string{"geoip_records_current", "geoip_records_history"},
		Description: `
		USAGE:
		- This dataset contains geoip-derived data from MaxMind GeoIP2 databases. Users should be informed that this is geoip-derived data and may have accuracy limitations.
		- Joins:
			- geoip_records_current.ip = solana_gossip_nodes_current.gossip_ip
			- geoip_records_current.ip = dz_users_current.client_ip

		COLUMNS:
		- ip (VARCHAR): IP address (primary key)
		- country_code (VARCHAR): ISO 3166-1 alpha-2 country code (e.g. US, GB, DE)
		- country (VARCHAR): Country name
		- region (VARCHAR): State/province/region name
		- city (VARCHAR): City name
		- city_id (INTEGER): MaxMind GeoName ID for the city
		- metro_name (VARCHAR): Metro name (e.g. New York, London, Frankfurt)
		- latitude (DOUBLE): Latitude
		- longitude (DOUBLE): Longitude
		- postal_code (VARCHAR): Postal/ZIP code
		- time_zone (VARCHAR): IANA time zone (e.g. America/New_York)
		- accuracy_radius (INTEGER): Accuracy radius in kilometers
		- asn (BIGINT): Autonomous System Number
		- asn_org (VARCHAR): ASN organization name
		- is_anycast (BOOLEAN): True if IP is anycast
		- is_anonymous_proxy (BOOLEAN): True if IP is an anonymous proxy
		- is_satellite_provider (BOOLEAN): True if IP is from a satellite provider

		SCD2 DATA STRUCTURE:
		- {table}_current: Current state of the dataset. One row per dataset entity.
		- {table}_history: Append-only historical versions with validity windows.
		- Always query {table}_current for current state. Query {table}_history only for historical analysis or point-in-time queries.
		- All joins should use {table}_current tables unless explicitly doing historical analysis.
		- {table}_current.as_of_ts is the timestamp of the snapshot that produced this row.
		- {table}_current.row_hash is the hash of the payload columns for change detection.
		- {table}_history.valid_from is the timestamp of the start of the validity window.
		- {table}_history.valid_to is the timestamp of the end of the validity window.
		- {table}_history.op is the operation that produced this row (I|U|D).
		- {table}_history.run_id is the identifier of the ingestion run that produced this row.
		`,
	},
}
