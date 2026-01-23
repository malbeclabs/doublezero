package handlers

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/malbeclabs/doublezero/lake/api/config"
	"github.com/malbeclabs/doublezero/lake/api/metrics"
)

// FieldValuesResponse is the response for the field values endpoint
type FieldValuesResponse struct {
	Values []string `json:"values"`
}

// fieldConfig defines how to query distinct values for a field
type fieldConfig struct {
	table  string
	column string
}

// entityFieldConfigs maps entity+field to the SQL query config
var entityFieldConfigs = map[string]map[string]fieldConfig{
	"devices": {
		"status": {table: "dz_devices_current", column: "status"},
		"type":   {table: "dz_devices_current", column: "device_type"},
		"metro":  {table: "dz_devices_current d JOIN dz_metros_current m ON d.metro_pk = m.pk", column: "m.code"},
		"contributor": {table: "dz_devices_current d JOIN dz_contributors_current c ON d.contributor_pk = c.pk", column: "c.code"},
	},
	"links": {
		"status":      {table: "dz_links_current", column: "status"},
		"type":        {table: "dz_links_current", column: "link_type"},
		"contributor": {table: "dz_links_current l JOIN dz_contributors_current c ON l.contributor_pk = c.pk", column: "c.code"},
		"sidea":       {table: "dz_links_current l JOIN dz_devices_current d ON l.side_a_pk = d.pk", column: "d.code"},
		"sidez":       {table: "dz_links_current l JOIN dz_devices_current d ON l.side_z_pk = d.pk", column: "d.code"},
	},
	"metros": {
		// Metros don't have many filterable fields with limited values
	},
	"contributors": {
		// Contributors don't have many filterable fields with limited values
	},
	"users": {
		"status": {table: "dz_users_current", column: "status"},
		"kind":   {table: "dz_users_current", column: "kind"},
		"metro":  {table: "dz_users_current u JOIN dz_devices_current d ON u.device_pk = d.pk JOIN dz_metros_current m ON d.metro_pk = m.pk", column: "m.code"},
		"device": {table: "dz_users_current u JOIN dz_devices_current d ON u.device_pk = d.pk", column: "d.code"},
	},
	"validators": {
		"dz":      {table: "(SELECT 'yes' AS val UNION ALL SELECT 'no' AS val)", column: "val"},
		"version": {table: "solana_vote_accounts_current v JOIN solana_gossip_nodes_current g ON v.node_pubkey = g.pubkey", column: "g.version"},
		"device":  {table: "solana_vote_accounts_current v JOIN solana_gossip_nodes_current g ON v.node_pubkey = g.pubkey JOIN dz_users_current u ON g.gossip_ip = u.dz_ip JOIN dz_devices_current d ON u.device_pk = d.pk", column: "d.code"},
		"city":    {table: "solana_vote_accounts_current v JOIN solana_gossip_nodes_current g ON v.node_pubkey = g.pubkey JOIN geoip_records_current geo ON g.gossip_ip = geo.ip", column: "geo.city"},
		"country": {table: "solana_vote_accounts_current v JOIN solana_gossip_nodes_current g ON v.node_pubkey = g.pubkey JOIN geoip_records_current geo ON g.gossip_ip = geo.ip", column: "geo.country"},
	},
	"gossip": {
		"dz":        {table: "(SELECT 'yes' AS val UNION ALL SELECT 'no' AS val)", column: "val"},
		"validator": {table: "(SELECT 'yes' AS val UNION ALL SELECT 'no' AS val)", column: "val"},
		"version":   {table: "solana_gossip_nodes_current", column: "version"},
		"city":      {table: "solana_gossip_nodes_current g JOIN geoip_records_current geo ON g.gossip_ip = geo.ip", column: "geo.city"},
		"country":   {table: "solana_gossip_nodes_current g JOIN geoip_records_current geo ON g.gossip_ip = geo.ip", column: "geo.country"},
		"device":    {table: "solana_gossip_nodes_current g JOIN dz_users_current u ON g.gossip_ip = u.dz_ip JOIN dz_devices_current d ON u.device_pk = d.pk", column: "d.code"},
	},
}

// GetFieldValues returns distinct values for a given entity field
func GetFieldValues(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	entity := r.URL.Query().Get("entity")
	field := r.URL.Query().Get("field")

	if entity == "" || field == "" {
		http.Error(w, "entity and field parameters are required", http.StatusBadRequest)
		return
	}

	// Look up the field config
	entityFields, ok := entityFieldConfigs[entity]
	if !ok {
		// Return empty for unknown entities
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(FieldValuesResponse{Values: []string{}})
		return
	}

	fieldCfg, ok := entityFields[field]
	if !ok {
		// Return empty for unknown fields
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(FieldValuesResponse{Values: []string{}})
		return
	}

	// Build and execute the query
	query := "SELECT DISTINCT " + fieldCfg.column + " AS val FROM " + fieldCfg.table + " WHERE " + fieldCfg.column + " IS NOT NULL AND " + fieldCfg.column + " != '' ORDER BY val LIMIT 100"

	start := time.Now()
	rows, err := config.DB.Query(ctx, query)
	duration := time.Since(start)
	metrics.RecordClickHouseQuery(duration, err)

	if err != nil {
		log.Printf("Field values query error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var values []string
	for rows.Next() {
		var val string
		if err := rows.Scan(&val); err != nil {
			log.Printf("Field values scan error: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		values = append(values, val)
	}

	if err := rows.Err(); err != nil {
		log.Printf("Field values rows error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if values == nil {
		values = []string{}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(FieldValuesResponse{Values: values}); err != nil {
		log.Printf("JSON encoding error: %v", err)
	}
}
