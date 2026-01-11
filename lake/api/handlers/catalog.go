package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
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

const clickhouseURL = "http://localhost:8123"

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

	resp, err := http.Get(clickhouseURL + "/?query=" + url.QueryEscape(query))
	if err != nil {
		http.Error(w, "Failed to connect to ClickHouse: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, "Failed to read response: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if resp.StatusCode != http.StatusOK {
		http.Error(w, "ClickHouse error: "+string(body), resp.StatusCode)
		return
	}

	var chResp struct {
		Data []struct {
			Name     string `json:"name"`
			Database string `json:"database"`
			Engine   string `json:"engine"`
			Type     string `json:"type"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &chResp); err != nil {
		http.Error(w, "Failed to parse response: "+err.Error(), http.StatusInternalServerError)
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
