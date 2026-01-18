package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/malbeclabs/doublezero/lake/api/config"
	"github.com/malbeclabs/doublezero/lake/api/metrics"
)

type TableInfo struct {
	Name     string   `json:"name"`
	Database string   `json:"database"`
	Engine   string   `json:"engine"`
	Type     string   `json:"type"`
	Columns  []string `json:"columns,omitempty"`
}

type CatalogResponse struct {
	Tables []TableInfo `json:"tables"`
}

func GetCatalog(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	start := time.Now()

	rows, err := config.DB.Query(ctx, `
		SELECT
			name,
			database,
			engine,
			CASE
				WHEN engine LIKE '%View%' THEN 'view'
				ELSE 'table'
			END as type
		FROM system.tables
		WHERE database = $1
		  AND name NOT LIKE 'stg_%'
		ORDER BY type, name
	`, config.Database())

	duration := time.Since(start)
	if err != nil {
		metrics.RecordClickHouseQuery(duration, err)
		http.Error(w, internalError("Failed to query database", err), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var tables []TableInfo
	for rows.Next() {
		var t TableInfo
		if err := rows.Scan(&t.Name, &t.Database, &t.Engine, &t.Type); err != nil {
			metrics.RecordClickHouseQuery(duration, err)
			http.Error(w, internalError("Failed to scan row", err), http.StatusInternalServerError)
			return
		}
		tables = append(tables, t)
	}

	if err := rows.Err(); err != nil {
		metrics.RecordClickHouseQuery(duration, err)
		http.Error(w, internalError("Failed to read rows", err), http.StatusInternalServerError)
		return
	}

	metrics.RecordClickHouseQuery(duration, nil)

	// Fetch columns for each table
	colStart := time.Now()
	colRows, err := config.DB.Query(ctx, `
		SELECT table, name
		FROM system.columns
		WHERE database = $1
		ORDER BY table, position
	`, config.Database())

	colDuration := time.Since(colStart)
	if err != nil {
		metrics.RecordClickHouseQuery(colDuration, err)
		// Non-fatal: return tables without columns
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(CatalogResponse{Tables: tables})
		return
	}
	defer colRows.Close()

	// Build table -> columns map
	tableColumns := make(map[string][]string)
	for colRows.Next() {
		var tableName, colName string
		if err := colRows.Scan(&tableName, &colName); err != nil {
			metrics.RecordClickHouseQuery(colDuration, err)
			http.Error(w, internalError("Failed to scan column row", err), http.StatusInternalServerError)
			return
		}
		tableColumns[tableName] = append(tableColumns[tableName], colName)
	}
	if err := colRows.Err(); err != nil {
		metrics.RecordClickHouseQuery(colDuration, err)
		http.Error(w, internalError("Failed to iterate column rows", err), http.StatusInternalServerError)
		return
	}
	metrics.RecordClickHouseQuery(colDuration, nil)

	// Attach columns to tables
	for i := range tables {
		if cols, ok := tableColumns[tables[i].Name]; ok {
			tables[i].Columns = cols
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(CatalogResponse{Tables: tables})
}
