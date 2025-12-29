package analytics

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	defaultTypeaheadLimit = 20
	maxTypeaheadLimit     = 100
	typeaheadTimeout      = 10 * time.Second
	queryTimeout          = 60 * time.Second
	pingTimeout           = 5 * time.Second
)

// HandleIndex serves the main page.
func (a *App) HandleIndex(w http.ResponseWriter, r *http.Request) {
	data := struct {
		Columns      []ColumnInfo
		ColumnGroups []ColumnGroup
		TableName    string
	}{
		Columns:      a.columns.GetDimensionColumns(),
		ColumnGroups: a.columns.GetColumnGroups(),
		TableName:    a.tableName,
	}

	if err := a.templates.ExecuteTemplate(w, "index.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// HandleTypeahead returns column values for typeahead/autocomplete.
func (a *App) HandleTypeahead(w http.ResponseWriter, r *http.Request) {
	column := r.URL.Query().Get("column")
	search := r.URL.Query().Get("q")
	limitStr := r.URL.Query().Get("limit")

	limit := defaultTypeaheadLimit
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= maxTypeaheadLimit {
		limit = l
	}

	// Validate column name
	if !a.columns.IsValidColumn(column) {
		http.Error(w, "Invalid column", http.StatusBadRequest)
		return
	}

	// Build query for distinct values with smart sorting
	var query string
	var args []any

	// Use numeric sorting for numeric columns, alphabetic for strings
	orderClause := "ORDER BY value"
	if a.columns.IsNumericColumn(column) {
		orderClause = "ORDER BY toInt64OrNull(value), value"
	}

	if search != "" {
		query = fmt.Sprintf(`
			SELECT DISTINCT toString(%s) as value
			FROM %s
			WHERE toString(%s) ILIKE ?
			%s
			LIMIT ?`,
			column, a.tableName, column, orderClause)
		args = []any{"%" + search + "%", limit}
	} else {
		query = fmt.Sprintf(`
			SELECT DISTINCT toString(%s) as value
			FROM %s
			%s
			LIMIT ?`,
			column, a.tableName, orderClause)
		args = []any{limit}
	}

	ctx, cancel := context.WithTimeout(r.Context(), typeaheadTimeout)
	defer cancel()

	rows, err := a.querier.Query(ctx, query, args...)
	if err != nil {
		a.logger.Error("typeahead query failed", "error", err, "column", column)
		http.Error(w, "Database query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var values []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			a.logger.Debug("typeahead scan failed", "error", err)
			continue
		}
		values = append(values, value)
	}

	if err := rows.Err(); err != nil {
		a.logger.Error("typeahead iteration failed", "error", err)
		http.Error(w, "Database query failed", http.StatusInternalServerError)
		return
	}

	// Return as HTML for htmx
	w.Header().Set("Content-Type", "text/html")
	if len(values) == 0 {
		_, _ = w.Write([]byte(`<div class="typeahead-empty">No matching values</div>`))
		return
	}

	var html strings.Builder
	for _, v := range values {
		html.WriteString(fmt.Sprintf(
			`<div class="typeahead-item" onclick="selectTypeaheadValue(this, '%s')">%s</div>`,
			template.HTMLEscapeString(v),
			template.HTMLEscapeString(v),
		))
	}
	_, _ = w.Write([]byte(html.String()))
}

// HandleQuery executes the flow query and returns JSON results.
func (a *App) HandleQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var params QueryParams
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	params.TableName = a.tableName

	result := a.executeFlowQuery(r.Context(), params)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

// executeFlowQuery builds and executes the ClickHouse query.
func (a *App) executeFlowQuery(ctx context.Context, params QueryParams) QueryResult {
	start := time.Now()

	// Build the query
	buildResult, err := a.queryBuilder.Build(params)
	if err != nil {
		return QueryResult{Error: err.Error()}
	}

	// Execute query
	queryCtx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	rows, err := a.querier.Query(queryCtx, buildResult.Query, buildResult.Args...)
	if err != nil {
		a.logger.Error("ClickHouse query failed", "error", err)
		return QueryResult{
			GeneratedSQL: FormatQueryForDisplay(buildResult.Query, buildResult.Args),
			Error:        "Query execution failed. Please check your parameters and try again.",
			ExecutionMs:  time.Since(start).Milliseconds(),
		}
	}
	defer rows.Close()

	// Parse results
	series, err := a.resultParser.Parse(rows, params.GroupBy)
	if err != nil {
		a.logger.Error("result parsing failed", "error", err)
		return QueryResult{
			GeneratedSQL: FormatQueryForDisplay(buildResult.Query, buildResult.Args),
			Error:        "Failed to parse query results.",
			ExecutionMs:  time.Since(start).Milliseconds(),
		}
	}

	return QueryResult{
		Series:       series,
		GeneratedSQL: FormatQueryForDisplay(buildResult.Query, buildResult.Args),
		ExecutionMs:  time.Since(start).Milliseconds(),
	}
}

// HandleAddFilter returns HTML for a new filter row (htmx partial).
func (a *App) HandleAddFilter(w http.ResponseWriter, r *http.Request) {
	indexStr := r.URL.Query().Get("index")
	index, _ := strconv.Atoi(indexStr)

	data := struct {
		Index        int
		Columns      []ColumnInfo
		ColumnGroups []ColumnGroup
	}{
		Index:        index,
		Columns:      a.columns.GetDimensionColumns(),
		ColumnGroups: a.columns.GetColumnGroups(),
	}

	if err := a.templates.ExecuteTemplate(w, "filter-row.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// HandleAddGroupBy returns HTML for a new group by row (htmx partial).
func (a *App) HandleAddGroupBy(w http.ResponseWriter, r *http.Request) {
	indexStr := r.URL.Query().Get("index")
	index, _ := strconv.Atoi(indexStr)

	data := struct {
		Index        int
		Columns      []ColumnInfo
		ColumnGroups []ColumnGroup
	}{
		Index:        index,
		Columns:      a.columns.GetDimensionColumns(),
		ColumnGroups: a.columns.GetColumnGroups(),
	}

	if err := a.templates.ExecuteTemplate(w, "groupby-row.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// HandleColumns returns column metadata as JSON.
func (a *App) HandleColumns(w http.ResponseWriter, r *http.Request) {
	category := r.URL.Query().Get("category")

	var filtered []ColumnInfo
	for _, col := range a.columns.All() {
		if category == "" || col.Category == category {
			filtered = append(filtered, col)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(filtered)
}

// HandleHealthz returns the health status of the service.
func (a *App) HandleHealthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), pingTimeout)
	defer cancel()

	if err := a.querier.Ping(ctx); err != nil {
		a.logger.Error("health check failed", "error", err)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("unhealthy: database connection failed"))
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}
