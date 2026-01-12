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
	Name     string `json:"name"`
	Database string `json:"database"`
	Engine   string `json:"engine"`
	Type     string `json:"type"`
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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(CatalogResponse{Tables: tables})
}
