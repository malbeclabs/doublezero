package analytics

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// QueryBuilder constructs ClickHouse queries for flow analytics.
type QueryBuilder struct {
	columns *ColumnRegistry
}

// NewQueryBuilder creates a new QueryBuilder with the given column registry.
func NewQueryBuilder(columns *ColumnRegistry) *QueryBuilder {
	return &QueryBuilder{columns: columns}
}

// BuildResult contains the generated query and its arguments.
type BuildResult struct {
	Query           string
	Args            []any
	IntervalSeconds int
}

// Build constructs a ClickHouse query from the given parameters.
func (qb *QueryBuilder) Build(params QueryParams) (*BuildResult, error) {
	// Validate group by columns
	if err := qb.validateGroupByColumns(params.GroupBy); err != nil {
		return nil, err
	}

	// Validate interval against allowlist
	if !ValidIntervals[params.Interval] {
		return nil, fmt.Errorf("invalid interval value: %q", params.Interval)
	}

	// Determine appropriate interval based on time range
	interval := qb.autoSelectInterval(params.Interval, params.StartTime, params.EndTime)

	// Parse interval for bits/second calculation
	intervalSeconds := ParseIntervalSeconds(interval)
	intervalDuration := time.Duration(intervalSeconds) * time.Second

	// Align time range to interval boundaries to avoid partial bucket dips
	// Start: round UP to next boundary (exclude partial first bucket)
	// End: round DOWN to boundary (exclude partial last bucket)
	alignedStart := alignTimeUp(params.StartTime, intervalDuration)
	alignedEnd := alignTimeDown(params.EndTime, intervalDuration)

	// Build query parts
	selectClause := qb.buildSelectClause(params.GroupBy, interval, intervalSeconds)

	whereClause, args, err := qb.buildWhereClause(params.Filters, alignedStart, alignedEnd)
	if err != nil {
		return nil, err
	}

	groupByClause := qb.buildGroupByClause(params.GroupBy)
	orderByClause := "time_bucket ASC"

	// Check if we need top N filtering
	useTopN := params.TopN > 0 && len(params.GroupBy) > 0

	var query string
	if useTopN {
		// Build CTE for top N groups by total traffic
		cte, cteArgs := qb.buildTopNCTE(params.GroupBy, params.TableName, alignedStart, alignedEnd, params.Filters, params.TopN)

		// Add CTE args before the main query args
		// CTE needs: start, end, filters (same as main query)
		allArgs := append(cteArgs, args...)

		// Add top N filter to WHERE clause
		topNFilter := qb.buildTopNFilter(params.GroupBy)
		whereClauseWithTopN := whereClause + " AND " + topNFilter

		query = fmt.Sprintf(`%s
		SELECT %s
		FROM %s
		WHERE %s
		GROUP BY %s
		ORDER BY %s`,
			cte,
			selectClause,
			params.TableName,
			whereClauseWithTopN,
			groupByClause,
			orderByClause,
		)
		args = allArgs
	} else {
		// Standard query without top N
		query = fmt.Sprintf(`
		SELECT %s
		FROM %s
		WHERE %s
		GROUP BY %s
		ORDER BY %s`,
			selectClause,
			params.TableName,
			whereClause,
			groupByClause,
			orderByClause,
		)
	}

	return &BuildResult{
		Query:           query,
		Args:            args,
		IntervalSeconds: intervalSeconds,
	}, nil
}

// validateGroupByColumns checks that all group by columns are valid.
func (qb *QueryBuilder) validateGroupByColumns(columns []string) error {
	for _, col := range columns {
		if !qb.columns.IsValidColumn(col) {
			return fmt.Errorf("invalid group by column: %s", col)
		}
	}
	return nil
}

// autoSelectInterval returns an appropriate interval based on the time range.
func (qb *QueryBuilder) autoSelectInterval(requestedInterval string, start, end time.Time) string {
	if requestedInterval != "" {
		return requestedInterval
	}

	duration := end.Sub(start)
	switch {
	case duration <= 1*time.Hour:
		return "1 minute"
	case duration <= 6*time.Hour:
		return "5 minute"
	case duration <= 24*time.Hour:
		return "15 minute"
	case duration <= 7*24*time.Hour:
		return "1 hour"
	default:
		return "6 hour"
	}
}

// buildSelectClause constructs the SELECT portion of the query.
func (qb *QueryBuilder) buildSelectClause(groupBy []string, interval string, intervalSeconds int) string {
	var parts []string

	// Time bucket
	parts = append(parts, fmt.Sprintf(
		"toStartOfInterval(time_received_ns, INTERVAL %s) as time_bucket", interval))

	// Group by columns
	for _, col := range groupBy {
		parts = append(parts, fmt.Sprintf("toString(%s) as %s", col, col))
	}

	// Calculate bits per second: (sum(bytes) * 8 * sampling_rate) / interval_seconds
	parts = append(parts,
		fmt.Sprintf("sum(bytes * sampling_rate) * 8 / %d as bps", intervalSeconds))

	return strings.Join(parts, ", ")
}

// buildWhereClause constructs the WHERE portion of the query with parameter placeholders.
func (qb *QueryBuilder) buildWhereClause(filters []Filter, start, end time.Time) (string, []any, error) {
	var parts []string
	var args []any

	// Time range constraints
	parts = append(parts, "time_received_ns >= ?")
	args = append(args, start)

	parts = append(parts, "time_received_ns < ?")
	args = append(args, end)

	// User filters
	for _, filter := range filters {
		if !qb.columns.IsValidColumn(filter.Column) {
			return "", nil, fmt.Errorf("invalid filter column: %s", filter.Column)
		}

		clause, filterArgs, err := qb.buildFilterClause(filter)
		if err != nil {
			return "", nil, err
		}
		if clause != "" {
			parts = append(parts, clause)
			args = append(args, filterArgs...)
		}
	}

	return strings.Join(parts, " AND "), args, nil
}

// buildFilterClause constructs a single filter clause.
func (qb *QueryBuilder) buildFilterClause(filter Filter) (string, []any, error) {
	// IS EMPTY and IS NOT EMPTY don't require values
	if filter.Operator != "IS EMPTY" && filter.Operator != "IS NOT EMPTY" {
		if len(filter.Values) == 0 {
			return "", nil, nil
		}
	}

	var clause string
	var args []any

	switch filter.Operator {
	case "=":
		clause = fmt.Sprintf("toString(%s) = ?", filter.Column)
		args = append(args, filter.Values[0])

	case "!=":
		clause = fmt.Sprintf("toString(%s) != ?", filter.Column)
		args = append(args, filter.Values[0])

	case "IN":
		placeholders := make([]string, len(filter.Values))
		for i, v := range filter.Values {
			placeholders[i] = "?"
			args = append(args, v)
		}
		clause = fmt.Sprintf("toString(%s) IN (%s)", filter.Column, strings.Join(placeholders, ","))

	case "NOT IN":
		placeholders := make([]string, len(filter.Values))
		for i, v := range filter.Values {
			placeholders[i] = "?"
			args = append(args, v)
		}
		clause = fmt.Sprintf("toString(%s) NOT IN (%s)", filter.Column, strings.Join(placeholders, ","))

	case "LIKE":
		clause = fmt.Sprintf("toString(%s) ILIKE ?", filter.Column)
		args = append(args, "%"+filter.Values[0]+"%")

	case "IS EMPTY":
		clause = fmt.Sprintf("toString(%s) = ''", filter.Column)

	case "IS NOT EMPTY":
		clause = fmt.Sprintf("toString(%s) != ''", filter.Column)

	default:
		return "", nil, fmt.Errorf("unsupported operator: %s", filter.Operator)
	}

	return clause, args, nil
}

// buildGroupByClause constructs the GROUP BY portion of the query.
func (qb *QueryBuilder) buildGroupByClause(groupBy []string) string {
	parts := []string{"time_bucket"}
	parts = append(parts, groupBy...)
	return strings.Join(parts, ", ")
}

// buildTopNCTE constructs a CTE to find the top N groups by total traffic.
func (qb *QueryBuilder) buildTopNCTE(groupBy []string, tableName string, start, end time.Time, filters []Filter, topN int) (string, []any) {
	var args []any

	// Build the grouping key - concatenate multiple columns if needed
	groupKey := qb.buildGroupKey(groupBy)

	// Build WHERE clause for CTE (same time range and filters)
	var whereParts []string
	whereParts = append(whereParts, "time_received_ns >= ?")
	args = append(args, start)
	whereParts = append(whereParts, "time_received_ns < ?")
	args = append(args, end)

	// Add user filters to CTE
	for _, filter := range filters {
		clause, filterArgs, err := qb.buildFilterClause(filter)
		if err == nil && clause != "" {
			whereParts = append(whereParts, clause)
			args = append(args, filterArgs...)
		}
	}

	whereClause := strings.Join(whereParts, " AND ")

	// Build column list for GROUP BY in CTE
	groupByList := strings.Join(groupBy, ", ")

	cte := fmt.Sprintf(`WITH top_groups AS (
		SELECT %s as grp
		FROM %s
		WHERE %s
		GROUP BY %s
		ORDER BY sum(bytes * sampling_rate) DESC
		LIMIT %d
	)`,
		groupKey,
		tableName,
		whereClause,
		groupByList,
		topN,
	)

	return cte, args
}

// buildGroupKey builds an expression that creates a unique key for a group.
// For single column: toString(col)
// For multiple columns: concat(toString(col1), '||', toString(col2), ...)
func (qb *QueryBuilder) buildGroupKey(groupBy []string) string {
	if len(groupBy) == 1 {
		return fmt.Sprintf("toString(%s)", groupBy[0])
	}

	// Multiple columns - concatenate with separator
	var parts []string
	for i, col := range groupBy {
		parts = append(parts, fmt.Sprintf("toString(%s)", col))
		if i < len(groupBy)-1 {
			parts = append(parts, "'||'")
		}
	}
	return fmt.Sprintf("concat(%s)", strings.Join(parts, ", "))
}

// buildTopNFilter builds a WHERE clause filter to include only top N groups.
func (qb *QueryBuilder) buildTopNFilter(groupBy []string) string {
	groupKey := qb.buildGroupKey(groupBy)
	return fmt.Sprintf("%s IN (SELECT grp FROM top_groups)", groupKey)
}

// ParseIntervalSeconds converts an interval string to seconds.
func ParseIntervalSeconds(interval string) int {
	parts := strings.Fields(interval)
	if len(parts) != 2 {
		return 60 // default 1 minute
	}

	num, err := strconv.Atoi(parts[0])
	if err != nil {
		return 60
	}

	switch strings.ToLower(parts[1]) {
	case "second", "seconds":
		return num
	case "minute", "minutes":
		return num * 60
	case "hour", "hours":
		return num * 3600
	case "day", "days":
		return num * 86400
	default:
		return 60
	}
}

// FormatQueryForDisplay replaces placeholders with actual values for display.
func FormatQueryForDisplay(query string, args []any) string {
	result := query
	for _, arg := range args {
		var replacement string
		switch v := arg.(type) {
		case string:
			replacement = fmt.Sprintf("'%s'", v)
		case time.Time:
			replacement = fmt.Sprintf("'%s'", v.Format("2006-01-02 15:04:05"))
		default:
			replacement = fmt.Sprintf("%v", v)
		}
		result = strings.Replace(result, "?", replacement, 1)
	}
	return strings.TrimSpace(result)
}

// alignTimeUp rounds a time UP to the next interval boundary.
// If the time is already on a boundary, it returns the time unchanged.
func alignTimeUp(t time.Time, interval time.Duration) time.Time {
	truncated := t.Truncate(interval)
	if truncated.Equal(t) {
		return t
	}
	return truncated.Add(interval)
}

// alignTimeDown rounds a time DOWN to the interval boundary.
func alignTimeDown(t time.Time, interval time.Duration) time.Time {
	return t.Truncate(interval)
}
