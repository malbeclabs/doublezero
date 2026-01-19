package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/malbeclabs/doublezero/lake/api/config"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/neo4j"
)

type CypherQueryRequest struct {
	Query string `json:"query"`
}

type CypherQueryResponse struct {
	Columns   []string         `json:"columns"`
	Rows      []map[string]any `json:"rows"`
	RowCount  int              `json:"row_count"`
	ElapsedMs int64            `json:"elapsed_ms"`
	Error     string           `json:"error,omitempty"`
}

// ExecuteCypher executes a Cypher query against Neo4j and returns formatted results.
func ExecuteCypher(w http.ResponseWriter, r *http.Request) {
	var req CypherQueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(req.Query) == "" {
		http.Error(w, "Query is required", http.StatusBadRequest)
		return
	}

	// Check if Neo4j is available
	if config.Neo4jClient == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(CypherQueryResponse{
			Error: "Neo4j is not available",
		})
		return
	}

	start := time.Now()

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	session := config.Neo4jSession(ctx)
	defer session.Close(ctx)

	result, err := session.ExecuteRead(ctx, func(tx neo4j.Transaction) (any, error) {
		res, err := tx.Run(ctx, req.Query, nil)
		if err != nil {
			return nil, err
		}

		records, err := res.Collect(ctx)
		if err != nil {
			return nil, err
		}

		// Get column names from keys
		var columns []string
		if len(records) > 0 {
			columns = records[0].Keys
		}

		// Convert records to row maps
		rows := make([]map[string]any, 0, len(records))
		for _, record := range records {
			row := make(map[string]any)
			for _, key := range record.Keys {
				val, _ := record.Get(key)
				row[key] = convertNeo4jValue(val)
			}
			rows = append(rows, row)
		}

		return CypherQueryResponse{
			Columns:  columns,
			Rows:     rows,
			RowCount: len(rows),
		}, nil
	})

	duration := time.Since(start)

	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(CypherQueryResponse{
			Error:     err.Error(),
			ElapsedMs: duration.Milliseconds(),
		})
		return
	}

	response := result.(CypherQueryResponse)
	response.ElapsedMs = duration.Milliseconds()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
