package analytics

import (
	"strings"
	"testing"
	"time"
)

func TestQueryBuilder_Build(t *testing.T) {
	reg := NewColumnRegistry(DefaultColumns())
	qb := NewQueryBuilder(reg)

	baseTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name        string
		params      QueryParams
		wantErr     bool
		errContains string
		checkQuery  func(t *testing.T, result *BuildResult)
	}{
		{
			name: "simple query no filters",
			params: QueryParams{
				StartTime: baseTime,
				EndTime:   baseTime.Add(1 * time.Hour),
				TableName: "flows",
				Interval:  "1 minute",
			},
			checkQuery: func(t *testing.T, result *BuildResult) {
				if !strings.Contains(result.Query, "FROM flows") {
					t.Error("expected table name in query")
				}
				if !strings.Contains(result.Query, "INTERVAL 1 minute") {
					t.Error("expected interval in query")
				}
				if result.IntervalSeconds != 60 {
					t.Errorf("expected 60 seconds, got %d", result.IntervalSeconds)
				}
				if len(result.Args) != 2 {
					t.Errorf("expected 2 args (start, end), got %d", len(result.Args))
				}
			},
		},
		{
			name: "query with group by",
			params: QueryParams{
				StartTime: baseTime,
				EndTime:   baseTime.Add(1 * time.Hour),
				TableName: "flows",
				Interval:  "5 minute",
				GroupBy:   []string{"src_addr", "dst_addr"},
			},
			checkQuery: func(t *testing.T, result *BuildResult) {
				if !strings.Contains(result.Query, "toString(src_addr) as src_addr") {
					t.Error("expected src_addr in SELECT")
				}
				if !strings.Contains(result.Query, "toString(dst_addr) as dst_addr") {
					t.Error("expected dst_addr in SELECT")
				}
				if !strings.Contains(result.Query, "GROUP BY time_bucket, src_addr, dst_addr") {
					t.Error("expected GROUP BY with columns")
				}
			},
		},
		{
			name: "query with equals filter",
			params: QueryParams{
				StartTime: baseTime,
				EndTime:   baseTime.Add(1 * time.Hour),
				TableName: "flows",
				Interval:  "1 minute",
				Filters: []Filter{
					{Column: "src_addr", Operator: "=", Values: []string{"192.168.1.1"}},
				},
			},
			checkQuery: func(t *testing.T, result *BuildResult) {
				if !strings.Contains(result.Query, "toString(src_addr) = ?") {
					t.Error("expected equals filter in query")
				}
				if len(result.Args) != 3 {
					t.Errorf("expected 3 args, got %d", len(result.Args))
				}
			},
		},
		{
			name: "query with IN filter",
			params: QueryParams{
				StartTime: baseTime,
				EndTime:   baseTime.Add(1 * time.Hour),
				TableName: "flows",
				Interval:  "1 minute",
				Filters: []Filter{
					{Column: "proto", Operator: "IN", Values: []string{"TCP", "UDP"}},
				},
			},
			checkQuery: func(t *testing.T, result *BuildResult) {
				if !strings.Contains(result.Query, "toString(proto) IN (?,?)") {
					t.Error("expected IN filter in query")
				}
				if len(result.Args) != 4 { // start, end, TCP, UDP
					t.Errorf("expected 4 args, got %d", len(result.Args))
				}
			},
		},
		{
			name: "query with LIKE filter",
			params: QueryParams{
				StartTime: baseTime,
				EndTime:   baseTime.Add(1 * time.Hour),
				TableName: "flows",
				Interval:  "1 minute",
				Filters: []Filter{
					{Column: "src_addr", Operator: "LIKE", Values: []string{"192.168"}},
				},
			},
			checkQuery: func(t *testing.T, result *BuildResult) {
				if !strings.Contains(result.Query, "toString(src_addr) ILIKE ?") {
					t.Error("expected LIKE filter in query")
				}
				// Check that wildcard is added to the value
				found := false
				for _, arg := range result.Args {
					if s, ok := arg.(string); ok && s == "%192.168%" {
						found = true
						break
					}
				}
				if !found {
					t.Error("expected wildcard-wrapped value in args")
				}
			},
		},
		{
			name: "invalid group by column",
			params: QueryParams{
				StartTime: baseTime,
				EndTime:   baseTime.Add(1 * time.Hour),
				TableName: "flows",
				GroupBy:   []string{"invalid_column"},
			},
			wantErr:     true,
			errContains: "invalid group by column",
		},
		{
			name: "invalid filter column",
			params: QueryParams{
				StartTime: baseTime,
				EndTime:   baseTime.Add(1 * time.Hour),
				TableName: "flows",
				Filters: []Filter{
					{Column: "nonexistent", Operator: "=", Values: []string{"value"}},
				},
			},
			wantErr:     true,
			errContains: "invalid filter column",
		},
		{
			name: "invalid interval",
			params: QueryParams{
				StartTime: baseTime,
				EndTime:   baseTime.Add(1 * time.Hour),
				TableName: "flows",
				Interval:  "2 minute", // not in allowlist
			},
			wantErr:     true,
			errContains: "invalid interval",
		},
		{
			name: "sql injection in group by rejected",
			params: QueryParams{
				StartTime: baseTime,
				EndTime:   baseTime.Add(1 * time.Hour),
				TableName: "flows",
				GroupBy:   []string{"src_addr; DROP TABLE flows"},
			},
			wantErr:     true,
			errContains: "invalid group by column",
		},
		{
			name: "unsupported operator",
			params: QueryParams{
				StartTime: baseTime,
				EndTime:   baseTime.Add(1 * time.Hour),
				TableName: "flows",
				Filters: []Filter{
					{Column: "src_addr", Operator: "INVALID", Values: []string{"value"}},
				},
			},
			wantErr:     true,
			errContains: "unsupported operator",
		},
		{
			name: "query with IS EMPTY filter",
			params: QueryParams{
				StartTime: baseTime,
				EndTime:   baseTime.Add(1 * time.Hour),
				TableName: "flows",
				Interval:  "1 minute",
				Filters: []Filter{
					{Column: "dst_location", Operator: "IS EMPTY", Values: []string{}},
				},
			},
			checkQuery: func(t *testing.T, result *BuildResult) {
				if !strings.Contains(result.Query, "toString(dst_location) = ''") {
					t.Error("expected IS EMPTY filter in query")
				}
				// Should only have 2 args (start, end) - no value arg for IS EMPTY
				if len(result.Args) != 2 {
					t.Errorf("expected 2 args, got %d", len(result.Args))
				}
			},
		},
		{
			name: "query with IS NOT EMPTY filter",
			params: QueryParams{
				StartTime: baseTime,
				EndTime:   baseTime.Add(1 * time.Hour),
				TableName: "flows",
				Interval:  "1 minute",
				Filters: []Filter{
					{Column: "src_location", Operator: "IS NOT EMPTY", Values: []string{}},
				},
			},
			checkQuery: func(t *testing.T, result *BuildResult) {
				if !strings.Contains(result.Query, "toString(src_location) != ''") {
					t.Error("expected IS NOT EMPTY filter in query")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := qb.Build(tt.params)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				} else if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if tt.checkQuery != nil {
				tt.checkQuery(t, result)
			}
		})
	}
}

func TestQueryBuilder_AutoSelectInterval(t *testing.T) {
	reg := NewColumnRegistry(DefaultColumns())
	qb := NewQueryBuilder(reg)

	baseTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		duration time.Duration
		expected string
	}{
		{"30 minutes -> 1 minute", 30 * time.Minute, "1 minute"},
		{"1 hour -> 1 minute", 1 * time.Hour, "1 minute"},
		{"3 hours -> 5 minute", 3 * time.Hour, "5 minute"},
		{"6 hours -> 5 minute", 6 * time.Hour, "5 minute"},
		{"12 hours -> 15 minute", 12 * time.Hour, "15 minute"},
		{"24 hours -> 15 minute", 24 * time.Hour, "15 minute"},
		{"3 days -> 1 hour", 3 * 24 * time.Hour, "1 hour"},
		{"7 days -> 1 hour", 7 * 24 * time.Hour, "1 hour"},
		{"14 days -> 6 hour", 14 * 24 * time.Hour, "6 hour"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := qb.Build(QueryParams{
				StartTime: baseTime,
				EndTime:   baseTime.Add(tt.duration),
				TableName: "flows",
				Interval:  "", // auto-select
			})

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !strings.Contains(result.Query, "INTERVAL "+tt.expected) {
				t.Errorf("expected interval %q, query: %s", tt.expected, result.Query)
			}
		})
	}
}

func TestParseIntervalSeconds(t *testing.T) {
	tests := []struct {
		interval string
		expected int
	}{
		{"1 second", 1},
		{"5 second", 5},
		{"30 second", 30},
		{"1 minute", 60},
		{"5 minute", 300},
		{"15 minute", 900},
		{"1 hour", 3600},
		{"6 hour", 21600},
		{"1 day", 86400},
		{"7 day", 604800},
		// Edge cases
		{"", 60},        // default
		{"invalid", 60}, // default
		{"1", 60},       // missing unit
	}

	for _, tt := range tests {
		t.Run(tt.interval, func(t *testing.T) {
			got := ParseIntervalSeconds(tt.interval)
			if got != tt.expected {
				t.Errorf("ParseIntervalSeconds(%q) = %d, want %d", tt.interval, got, tt.expected)
			}
		})
	}
}

func TestFormatQueryForDisplay(t *testing.T) {
	testTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	tests := []struct {
		name     string
		query    string
		args     []any
		expected string
	}{
		{
			name:     "string replacement",
			query:    "SELECT * FROM t WHERE col = ?",
			args:     []any{"value"},
			expected: "SELECT * FROM t WHERE col = 'value'",
		},
		{
			name:     "time replacement",
			query:    "SELECT * FROM t WHERE ts >= ?",
			args:     []any{testTime},
			expected: "SELECT * FROM t WHERE ts >= '2024-01-15 10:30:00'",
		},
		{
			name:     "multiple replacements",
			query:    "SELECT * FROM t WHERE a = ? AND b = ?",
			args:     []any{"foo", "bar"},
			expected: "SELECT * FROM t WHERE a = 'foo' AND b = 'bar'",
		},
		{
			name:     "numeric replacement",
			query:    "SELECT * FROM t WHERE n = ?",
			args:     []any{42},
			expected: "SELECT * FROM t WHERE n = 42",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatQueryForDisplay(tt.query, tt.args)
			if got != tt.expected {
				t.Errorf("got %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestAlignTimeUp(t *testing.T) {
	tests := []struct {
		name     string
		input    time.Time
		interval time.Duration
		expected time.Time
	}{
		{
			name:     "already on boundary",
			input:    time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
			interval: 5 * time.Minute,
			expected: time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
		},
		{
			name:     "3 minutes past - round up to 5",
			input:    time.Date(2024, 1, 1, 10, 3, 0, 0, time.UTC),
			interval: 5 * time.Minute,
			expected: time.Date(2024, 1, 1, 10, 5, 0, 0, time.UTC),
		},
		{
			name:     "1 second past - round up",
			input:    time.Date(2024, 1, 1, 10, 0, 1, 0, time.UTC),
			interval: 1 * time.Minute,
			expected: time.Date(2024, 1, 1, 10, 1, 0, 0, time.UTC),
		},
		{
			name:     "47 minutes past hour - round up to next hour",
			input:    time.Date(2024, 1, 1, 10, 47, 0, 0, time.UTC),
			interval: 1 * time.Hour,
			expected: time.Date(2024, 1, 1, 11, 0, 0, 0, time.UTC),
		},
		{
			name:     "exactly on hour boundary",
			input:    time.Date(2024, 1, 1, 11, 0, 0, 0, time.UTC),
			interval: 1 * time.Hour,
			expected: time.Date(2024, 1, 1, 11, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := alignTimeUp(tt.input, tt.interval)
			if !got.Equal(tt.expected) {
				t.Errorf("alignTimeUp(%v, %v) = %v, want %v",
					tt.input, tt.interval, got, tt.expected)
			}
		})
	}
}

func TestAlignTimeDown(t *testing.T) {
	tests := []struct {
		name     string
		input    time.Time
		interval time.Duration
		expected time.Time
	}{
		{
			name:     "already on boundary",
			input:    time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
			interval: 5 * time.Minute,
			expected: time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
		},
		{
			name:     "3 minutes past - round down to 0",
			input:    time.Date(2024, 1, 1, 10, 3, 0, 0, time.UTC),
			interval: 5 * time.Minute,
			expected: time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
		},
		{
			name:     "47 minutes past - round down to 45",
			input:    time.Date(2024, 1, 1, 10, 47, 0, 0, time.UTC),
			interval: 15 * time.Minute,
			expected: time.Date(2024, 1, 1, 10, 45, 0, 0, time.UTC),
		},
		{
			name:     "59 seconds past minute - round down",
			input:    time.Date(2024, 1, 1, 10, 0, 59, 0, time.UTC),
			interval: 1 * time.Minute,
			expected: time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := alignTimeDown(tt.input, tt.interval)
			if !got.Equal(tt.expected) {
				t.Errorf("alignTimeDown(%v, %v) = %v, want %v",
					tt.input, tt.interval, got, tt.expected)
			}
		})
	}
}

func TestQueryBuilder_TimeRangeAlignment(t *testing.T) {
	reg := NewColumnRegistry(DefaultColumns())
	qb := NewQueryBuilder(reg)

	// Query from 10:03 to 11:47 with 5-minute intervals
	// Should align to 10:05 to 11:45
	start := time.Date(2024, 1, 1, 10, 3, 0, 0, time.UTC)
	end := time.Date(2024, 1, 1, 11, 47, 0, 0, time.UTC)

	result, err := qb.Build(QueryParams{
		StartTime: start,
		EndTime:   end,
		TableName: "flows",
		Interval:  "5 minute",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check that args contain aligned times, not original times
	if len(result.Args) < 2 {
		t.Fatalf("expected at least 2 args, got %d", len(result.Args))
	}

	alignedStart, ok := result.Args[0].(time.Time)
	if !ok {
		t.Fatalf("expected first arg to be time.Time")
	}
	alignedEnd, ok := result.Args[1].(time.Time)
	if !ok {
		t.Fatalf("expected second arg to be time.Time")
	}

	expectedStart := time.Date(2024, 1, 1, 10, 5, 0, 0, time.UTC)
	expectedEnd := time.Date(2024, 1, 1, 11, 45, 0, 0, time.UTC)

	if !alignedStart.Equal(expectedStart) {
		t.Errorf("start time = %v, want %v", alignedStart, expectedStart)
	}
	if !alignedEnd.Equal(expectedEnd) {
		t.Errorf("end time = %v, want %v", alignedEnd, expectedEnd)
	}
}

func TestQueryBuilder_TopN(t *testing.T) {
	reg := NewColumnRegistry(DefaultColumns())
	qb := NewQueryBuilder(reg)

	baseTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		params     QueryParams
		checkQuery func(t *testing.T, result *BuildResult)
	}{
		{
			name: "top N with single group by",
			params: QueryParams{
				StartTime: baseTime,
				EndTime:   baseTime.Add(1 * time.Hour),
				TableName: "flows",
				Interval:  "1 minute",
				GroupBy:   []string{"src_addr"},
				TopN:      10,
			},
			checkQuery: func(t *testing.T, result *BuildResult) {
				if !strings.Contains(result.Query, "WITH top_groups AS") {
					t.Error("expected CTE in query")
				}
				if !strings.Contains(result.Query, "LIMIT 10") {
					t.Error("expected LIMIT 10 in CTE")
				}
				if !strings.Contains(result.Query, "toString(src_addr) IN (SELECT grp FROM top_groups)") {
					t.Error("expected top N filter in WHERE clause")
				}
				if !strings.Contains(result.Query, "ORDER BY sum(bytes * sampling_rate) DESC") {
					t.Error("expected ORDER BY traffic in CTE")
				}
			},
		},
		{
			name: "top N with multiple group by columns",
			params: QueryParams{
				StartTime: baseTime,
				EndTime:   baseTime.Add(1 * time.Hour),
				TableName: "flows",
				Interval:  "1 minute",
				GroupBy:   []string{"src_addr", "dst_addr"},
				TopN:      5,
			},
			checkQuery: func(t *testing.T, result *BuildResult) {
				if !strings.Contains(result.Query, "WITH top_groups AS") {
					t.Error("expected CTE in query")
				}
				if !strings.Contains(result.Query, "LIMIT 5") {
					t.Error("expected LIMIT 5 in CTE")
				}
				// Check for concatenated group key
				if !strings.Contains(result.Query, "concat(toString(src_addr), '||', toString(dst_addr))") {
					t.Error("expected concatenated group key for multiple columns")
				}
			},
		},
		{
			name: "top N zero means no limit",
			params: QueryParams{
				StartTime: baseTime,
				EndTime:   baseTime.Add(1 * time.Hour),
				TableName: "flows",
				Interval:  "1 minute",
				GroupBy:   []string{"src_addr"},
				TopN:      0,
			},
			checkQuery: func(t *testing.T, result *BuildResult) {
				if strings.Contains(result.Query, "WITH top_groups AS") {
					t.Error("should not have CTE when TopN is 0")
				}
			},
		},
		{
			name: "top N without group by has no effect",
			params: QueryParams{
				StartTime: baseTime,
				EndTime:   baseTime.Add(1 * time.Hour),
				TableName: "flows",
				Interval:  "1 minute",
				TopN:      10,
			},
			checkQuery: func(t *testing.T, result *BuildResult) {
				if strings.Contains(result.Query, "WITH top_groups AS") {
					t.Error("should not have CTE when no GroupBy")
				}
			},
		},
		{
			name: "top N with filters",
			params: QueryParams{
				StartTime: baseTime,
				EndTime:   baseTime.Add(1 * time.Hour),
				TableName: "flows",
				Interval:  "1 minute",
				GroupBy:   []string{"src_addr"},
				TopN:      10,
				Filters: []Filter{
					{Column: "proto", Operator: "=", Values: []string{"TCP"}},
				},
			},
			checkQuery: func(t *testing.T, result *BuildResult) {
				if !strings.Contains(result.Query, "WITH top_groups AS") {
					t.Error("expected CTE in query")
				}
				// CTE should also have the filter
				// Count occurrences of the filter - should appear twice (CTE and main query)
				count := strings.Count(result.Query, "toString(proto) = ?")
				if count != 2 {
					t.Errorf("expected filter to appear twice (CTE and main), got %d", count)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := qb.Build(tt.params)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			tt.checkQuery(t, result)
		})
	}
}

func TestBuildGroupKey(t *testing.T) {
	reg := NewColumnRegistry(DefaultColumns())
	qb := NewQueryBuilder(reg)

	tests := []struct {
		name     string
		groupBy  []string
		expected string
	}{
		{
			name:     "single column",
			groupBy:  []string{"src_addr"},
			expected: "toString(src_addr)",
		},
		{
			name:     "two columns",
			groupBy:  []string{"src_addr", "dst_addr"},
			expected: "concat(toString(src_addr), '||', toString(dst_addr))",
		},
		{
			name:     "three columns",
			groupBy:  []string{"src_addr", "dst_addr", "proto"},
			expected: "concat(toString(src_addr), '||', toString(dst_addr), '||', toString(proto))",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := qb.buildGroupKey(tt.groupBy)
			if got != tt.expected {
				t.Errorf("buildGroupKey(%v) = %q, want %q", tt.groupBy, got, tt.expected)
			}
		})
	}
}
