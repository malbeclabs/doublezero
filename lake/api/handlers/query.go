package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/malbeclabs/doublezero/lake/api/config"
)

type QueryRequest struct {
	Query string `json:"query"`
}

type QueryResponse struct {
	Columns   []string `json:"columns"`
	Rows      [][]any  `json:"rows"`
	RowCount  int      `json:"row_count"`
	ElapsedMs int64    `json:"elapsed_ms"`
	Error     string   `json:"error,omitempty"`
}

func ExecuteQuery(w http.ResponseWriter, r *http.Request) {
	var req QueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(req.Query) == "" {
		http.Error(w, "Query is required", http.StatusBadRequest)
		return
	}

	start := time.Now()

	query := strings.TrimSpace(req.Query)
	query = strings.TrimSuffix(query, ";")
	query = strings.TrimSpace(query)
	if !strings.HasSuffix(strings.ToUpper(query), "FORMAT JSON") {
		query += " FORMAT JSON"
	}

	resp, err := http.Post(config.ClickHouseBaseURL(), "text/plain", strings.NewReader(query))
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(QueryResponse{
			Error: "Failed to connect to ClickHouse: " + err.Error(),
		})
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(QueryResponse{
			Error: "Failed to read response: " + err.Error(),
		})
		return
	}

	elapsed := time.Since(start).Milliseconds()

	if resp.StatusCode != http.StatusOK {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(QueryResponse{
			Error:     string(body),
			ElapsedMs: elapsed,
		})
		return
	}

	var chResp struct {
		Meta []struct {
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"meta"`
		Data []map[string]any `json:"data"`
		Rows int              `json:"rows"`
	}

	if err := json.Unmarshal(body, &chResp); err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(QueryResponse{
			Error: "Failed to parse response: " + err.Error(),
		})
		return
	}

	columns := make([]string, 0, len(chResp.Meta))
	for _, m := range chResp.Meta {
		columns = append(columns, m.Name)
	}

	rows := make([][]any, 0, len(chResp.Data))
	for _, row := range chResp.Data {
		rowData := make([]any, 0, len(columns))
		for _, col := range columns {
			rowData = append(rowData, row[col])
		}
		rows = append(rows, rowData)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(QueryResponse{
		Columns:   columns,
		Rows:      rows,
		RowCount:  len(rows),
		ElapsedMs: elapsed,
	})
}
