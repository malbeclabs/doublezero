package handlers

import (
	"context"
	"encoding/json"
	"log"
	"math"
	"net"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/malbeclabs/doublezero/lake/api/config"
	"github.com/malbeclabs/doublezero/lake/api/metrics"
)

// toJSONSafe converts ClickHouse values to JSON-serializable types.
// Handles special cases like net.IP, NaN, Inf, and other non-JSON-safe values.
func toJSONSafe(v any) any {
	if v == nil {
		return nil
	}

	switch val := v.(type) {
	case net.IP:
		return val.String()
	case *net.IP:
		if val == nil {
			return nil
		}
		return val.String()
	case float32:
		if math.IsNaN(float64(val)) || math.IsInf(float64(val), 0) {
			return nil
		}
		return val
	case float64:
		if math.IsNaN(val) || math.IsInf(val, 0) {
			return nil
		}
		return val
	case *float32:
		if val == nil {
			return nil
		}
		if math.IsNaN(float64(*val)) || math.IsInf(float64(*val), 0) {
			return nil
		}
		return *val
	case *float64:
		if val == nil {
			return nil
		}
		if math.IsNaN(*val) || math.IsInf(*val, 0) {
			return nil
		}
		return *val
	case time.Time:
		return val.Format(time.RFC3339)
	case *time.Time:
		if val == nil {
			return nil
		}
		return val.Format(time.RFC3339)
	default:
		// For pointer types, dereference to get actual value
		rv := reflect.ValueOf(v)
		if rv.Kind() == reflect.Ptr {
			if rv.IsNil() {
				return nil
			}
			return toJSONSafe(rv.Elem().Interface())
		}
		return v
	}
}

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

	// Convert rows to JSON-safe values
	safeRows := make([][]any, len(resultRows))
	for i, row := range resultRows {
		safeRow := make([]any, len(row))
		for j, v := range row {
			safeRow[j] = toJSONSafe(v)
		}
		safeRows[i] = safeRow
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(QueryResponse{
		Columns:   columns,
		Rows:      safeRows,
		RowCount:  len(safeRows),
		ElapsedMs: duration.Milliseconds(),
	}); err != nil {
		// Log encoding error - response is already partially written
		log.Printf("JSON encoding error: %v", err)
	}
}
