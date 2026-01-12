package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/malbeclabs/doublezero/lake/api/config"
	"github.com/malbeclabs/doublezero/lake/api/metrics"
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

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	rows, err := config.DB.Query(ctx, query)
	duration := time.Since(start)
	if err != nil {
		metrics.RecordClickHouseQuery(duration, err)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(QueryResponse{
			Error:     err.Error(),
			ElapsedMs: duration.Milliseconds(),
		})
		return
	}
	defer rows.Close()

	// Get column info
	columnTypes := rows.ColumnTypes()
	columns := make([]string, len(columnTypes))
	for i, ct := range columnTypes {
		columns[i] = ct.Name()
	}

	// Collect all rows
	var resultRows [][]any
	for rows.Next() {
		// Create properly typed values based on column types
		values := make([]any, len(columnTypes))
		for i, ct := range columnTypes {
			values[i] = reflect.New(ct.ScanType()).Interface()
		}

		if err := rows.Scan(values...); err != nil {
			metrics.RecordClickHouseQuery(duration, err)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(QueryResponse{
				Error:     err.Error(),
				ElapsedMs: duration.Milliseconds(),
			})
			return
		}

		// Dereference pointers
		row := make([]any, len(values))
		for i, v := range values {
			row[i] = reflect.ValueOf(v).Elem().Interface()
		}
		resultRows = append(resultRows, row)
	}

	if err := rows.Err(); err != nil {
		metrics.RecordClickHouseQuery(duration, err)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(QueryResponse{
			Error:     err.Error(),
			ElapsedMs: duration.Milliseconds(),
		})
		return
	}

	metrics.RecordClickHouseQuery(duration, nil)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(QueryResponse{
		Columns:   columns,
		Rows:      resultRows,
		RowCount:  len(resultRows),
		ElapsedMs: duration.Milliseconds(),
	})
}
