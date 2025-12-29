package analytics

import "time"

// ColumnInfo represents metadata for a flows table column.
type ColumnInfo struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Category    string `json:"category"`    // "dimension", "metric", "time"
	UICategory  string `json:"ui_category"` // For grouping in UI: "network", "location", "as", etc.
	Description string `json:"description"`
}

// ColumnGroup represents a category of columns for UI display.
type ColumnGroup struct {
	Name        string       `json:"name"`
	DisplayName string       `json:"display_name"`
	Columns     []ColumnInfo `json:"columns"`
}

// Filter represents a user-defined filter.
type Filter struct {
	Column   string   `json:"column"`
	Operator string   `json:"operator"`
	Values   []string `json:"values"`
}

// QueryParams represents the full query configuration.
type QueryParams struct {
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
	Filters   []Filter  `json:"filters"`
	GroupBy   []string  `json:"group_by"`
	Interval  string    `json:"interval"`  // e.g., "1 minute", "5 minute", "1 hour"
	TopN      int       `json:"top_n"`     // Limit to top N groups by traffic (0 = no limit)
	TableName string    `json:"table_name"`
}

// TimeSeriesPoint represents a single data point.
type TimeSeriesPoint struct {
	Timestamp int64   `json:"timestamp"` // Unix milliseconds
	Value     float64 `json:"value"`
}

// TimeSeries represents a single series with its label.
type TimeSeries struct {
	Label string            `json:"label"`
	Data  []TimeSeriesPoint `json:"data"`
}

// QueryResult contains the query results and metadata.
type QueryResult struct {
	Series       []TimeSeries `json:"series"`
	GeneratedSQL string       `json:"generated_sql"`
	ExecutionMs  int64        `json:"execution_ms"`
	Error        string       `json:"error,omitempty"`
}
