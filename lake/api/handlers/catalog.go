package handlers

import (
	"encoding/json"
	"fmt"
	"io"
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
	query := `
		SELECT
			name,
			database,
			engine,
			CASE
				WHEN engine LIKE '%View%' THEN 'view'
				ELSE 'table'
			END as type
		FROM system.tables
		WHERE database = 'default'
		  AND name NOT LIKE 'stg_%'
		ORDER BY type, name
		FORMAT JSON
	`

	start := time.Now()
	resp, err := http.Get(config.ClickHouseQueryURL(query))
	duration := time.Since(start)
	if err != nil {
		metrics.RecordClickHouseQuery(duration, err)
		http.Error(w, internalError("Failed to connect to database", err), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		metrics.RecordClickHouseQuery(duration, err)
		http.Error(w, internalError("Failed to read database response", err), http.StatusInternalServerError)
		return
	}

	if resp.StatusCode != http.StatusOK {
		metrics.RecordClickHouseQuery(duration, fmt.Errorf("status %d: %s", resp.StatusCode, string(body)))
		http.Error(w, "Database query failed", resp.StatusCode)
		return
	}
	metrics.RecordClickHouseQuery(duration, nil)

	var chResp struct {
		Data []struct {
			Name     string `json:"name"`
			Database string `json:"database"`
			Engine   string `json:"engine"`
			Type     string `json:"type"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &chResp); err != nil {
		http.Error(w, internalError("Failed to parse database response", err), http.StatusInternalServerError)
		return
	}

	tables := make([]TableInfo, 0, len(chResp.Data))
	for _, t := range chResp.Data {
		tables = append(tables, TableInfo{
			Name:     t.Name,
			Database: t.Database,
			Engine:   t.Engine,
			Type:     t.Type,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(CatalogResponse{Tables: tables})
}
