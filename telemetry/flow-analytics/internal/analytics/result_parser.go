package analytics

import (
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

// ResultParser converts ClickHouse query results to TimeSeries data.
type ResultParser struct {
	logger *slog.Logger
}

// NewResultParser creates a new ResultParser.
func NewResultParser(logger *slog.Logger) *ResultParser {
	return &ResultParser{logger: logger}
}

// Parse reads rows from a ClickHouse query result and converts them to a slice of TimeSeries.
// The groupBy parameter specifies which columns were used for grouping.
func (p *ResultParser) Parse(rows driver.Rows, groupBy []string) ([]TimeSeries, error) {
	seriesData := make(map[string][]TimeSeriesPoint)
	var parseErrors int

	for rows.Next() {
		timeBucket, label, bps, err := p.parseRow(rows, groupBy)
		if err != nil {
			parseErrors++
			if p.logger != nil {
				p.logger.Debug("skipping malformed row", "error", err)
			}
			continue
		}

		seriesData[label] = append(seriesData[label], TimeSeriesPoint{
			Timestamp: timeBucket.UnixMilli(),
			Value:     bps,
		})
	}

	if parseErrors > 0 && p.logger != nil {
		p.logger.Warn("some rows could not be parsed", "skipped_count", parseErrors)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return p.convertToSeries(seriesData), nil
}

// parseRow extracts a single row's data.
func (p *ResultParser) parseRow(rows driver.Rows, groupBy []string) (time.Time, string, float64, error) {
	var timeBucket time.Time
	var bps float64
	groupValues := make([]string, len(groupBy))

	// Build scan args: time_bucket, [group columns...], bps
	scanArgs := make([]any, 0, 2+len(groupBy))
	scanArgs = append(scanArgs, &timeBucket)
	for i := range groupValues {
		scanArgs = append(scanArgs, &groupValues[i])
	}
	scanArgs = append(scanArgs, &bps)

	if err := rows.Scan(scanArgs...); err != nil {
		return time.Time{}, "", 0, err
	}

	label := p.formatLabel(groupBy, groupValues)
	return timeBucket, label, bps, nil
}

// formatLabel creates a series label from group by columns and their values.
func (p *ResultParser) formatLabel(groupBy []string, values []string) string {
	if len(values) == 0 {
		return "Total"
	}

	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = fmt.Sprintf("%s=%s", groupBy[i], v)
	}
	return strings.Join(parts, ", ")
}

// convertToSeries converts the map of series data to a sorted slice.
func (p *ResultParser) convertToSeries(seriesData map[string][]TimeSeriesPoint) []TimeSeries {
	series := make([]TimeSeries, 0, len(seriesData))
	for label, data := range seriesData {
		series = append(series, TimeSeries{
			Label: label,
			Data:  data,
		})
	}

	// Sort by label for consistent ordering
	slices.SortFunc(series, func(a, b TimeSeries) int {
		return strings.Compare(a.Label, b.Label)
	})

	return series
}

// ParseFromValues parses time series data from raw values (for testing).
// Each row is: [timeBucket time.Time, groupValues ...string, bps float64]
func (p *ResultParser) ParseFromValues(rows [][]any, groupBy []string) []TimeSeries {
	seriesData := make(map[string][]TimeSeriesPoint)

	for _, row := range rows {
		if len(row) < 2 {
			continue
		}

		timeBucket, ok := row[0].(time.Time)
		if !ok {
			continue
		}

		bps, ok := row[len(row)-1].(float64)
		if !ok {
			continue
		}

		// Extract group values
		groupValues := make([]string, len(groupBy))
		for i := range groupBy {
			if i+1 < len(row)-1 {
				if v, ok := row[i+1].(string); ok {
					groupValues[i] = v
				}
			}
		}

		label := p.formatLabel(groupBy, groupValues)
		seriesData[label] = append(seriesData[label], TimeSeriesPoint{
			Timestamp: timeBucket.UnixMilli(),
			Value:     bps,
		})
	}

	return p.convertToSeries(seriesData)
}
