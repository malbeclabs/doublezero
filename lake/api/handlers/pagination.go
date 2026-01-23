package handlers

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

const (
	DefaultLimit = 100
	MaxLimit     = 1000
)

type PaginationParams struct {
	Limit  int
	Offset int
}

type PaginatedResponse[T any] struct {
	Items  []T `json:"items"`
	Total  int `json:"total"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

func ParsePagination(r *http.Request, defaultLimit int) PaginationParams {
	if defaultLimit <= 0 {
		defaultLimit = DefaultLimit
	}

	limit := defaultLimit
	offset := 0

	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
			if limit > MaxLimit {
				limit = MaxLimit
			}
		}
	}

	if o := r.URL.Query().Get("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	return PaginationParams{Limit: limit, Offset: offset}
}

type SortParams struct {
	Field     string
	Direction string
}

func ParseSort(r *http.Request, defaultField string, allowedFields map[string]string) SortParams {
	field := r.URL.Query().Get("sort_by")
	if field == "" {
		field = defaultField
	}

	// Validate field against allowlist
	if _, ok := allowedFields[field]; !ok {
		field = defaultField
	}

	direction := r.URL.Query().Get("sort_dir")
	if direction != "asc" && direction != "desc" {
		direction = "desc"
	}

	return SortParams{Field: field, Direction: direction}
}

func (s SortParams) OrderByClause(fieldMapping map[string]string) string {
	column := fieldMapping[s.Field]
	if column == "" {
		return ""
	}
	dir := "DESC"
	if s.Direction == "asc" {
		dir = "ASC"
	}
	return "ORDER BY " + column + " " + dir
}

// FilterParams holds parsed filter parameters
type FilterParams struct {
	Field string
	Value string
}

// ParseFilter extracts filter parameters from the request
func ParseFilter(r *http.Request) FilterParams {
	return FilterParams{
		Field: r.URL.Query().Get("filter_field"),
		Value: r.URL.Query().Get("filter_value"),
	}
}

// IsEmpty returns true if no filter is set
func (f FilterParams) IsEmpty() bool {
	return f.Value == ""
}

// FieldType indicates how to filter the field
type FieldType int

const (
	FieldTypeText FieldType = iota
	FieldTypeNumeric
	FieldTypeBoolean
	FieldTypeBandwidth // numeric with gbps/mbps units
	FieldTypeStake     // numeric with k/m units
)

// FilterFieldConfig describes how to filter a field
type FilterFieldConfig struct {
	Column    string
	Type      FieldType
}

// NumericOp represents a numeric comparison operator
type NumericOp struct {
	Op    string
	Value float64
}

var numericOpRegex = regexp.MustCompile(`^(>=|<=|>|<|==|=)\s*(-?\d+(?:\.\d+)?)([a-zA-Z]*)$`)

// ParseNumericFilter parses a string like ">100" or ">=10k" into operator and value
func ParseNumericFilter(input string, fieldType FieldType) *NumericOp {
	input = strings.TrimSpace(input)
	matches := numericOpRegex.FindStringSubmatch(input)
	if matches == nil {
		return nil
	}

	op := matches[1]
	if op == "==" {
		op = "="
	}

	value, err := strconv.ParseFloat(matches[2], 64)
	if err != nil {
		return nil
	}

	unit := strings.ToLower(matches[3])

	// Apply unit multiplier based on field type
	switch fieldType {
	case FieldTypeStake:
		switch unit {
		case "k":
			value *= 1e3
		case "m":
			value *= 1e6
		case "":
			// no unit, use raw value
		default:
			return nil // unknown unit
		}
	case FieldTypeBandwidth:
		switch unit {
		case "gbps":
			value *= 1e9
		case "mbps":
			value *= 1e6
		case "kbps":
			value *= 1e3
		case "bps", "":
			// default to gbps if no unit for bandwidth
			if unit == "" {
				value *= 1e9
			}
		default:
			return nil
		}
	case FieldTypeNumeric:
		// For plain numeric fields, k and m suffixes work
		switch unit {
		case "k":
			value *= 1e3
		case "m":
			value *= 1e6
		case "":
			// no unit
		default:
			return nil
		}
	}

	return &NumericOp{Op: op, Value: value}
}

// BuildFilterClause builds a WHERE clause fragment for the given filter
// Returns the clause (without WHERE keyword) and any query parameters
func (f FilterParams) BuildFilterClause(fields map[string]FilterFieldConfig) (string, []interface{}) {
	if f.IsEmpty() {
		return "", nil
	}

	// Handle "all" field - search across all text fields
	if f.Field == "all" {
		var textClauses []string
		var args []interface{}
		for _, config := range fields {
			if config.Type == FieldTypeText {
				textClauses = append(textClauses, "positionCaseInsensitive("+config.Column+", ?) > 0")
				args = append(args, f.Value)
			}
		}
		if len(textClauses) == 0 {
			return "", nil
		}
		return "(" + strings.Join(textClauses, " OR ") + ")", args
	}

	config, ok := fields[f.Field]
	if !ok {
		return "", nil
	}

	switch config.Type {
	case FieldTypeText:
		// Case-insensitive substring match using ClickHouse's positionCaseInsensitive
		return "positionCaseInsensitive(" + config.Column + ", ?) > 0", []interface{}{f.Value}

	case FieldTypeBoolean:
		val := strings.ToLower(strings.TrimSpace(f.Value))
		if val == "yes" || val == "true" || val == "1" {
			return config.Column + " = true", nil
		} else if val == "no" || val == "false" || val == "0" {
			return config.Column + " = false", nil
		}
		return "", nil

	case FieldTypeNumeric, FieldTypeStake, FieldTypeBandwidth:
		numOp := ParseNumericFilter(f.Value, config.Type)
		if numOp == nil {
			return "", nil
		}
		return config.Column + " " + numOp.Op + " ?", []interface{}{numOp.Value}
	}

	return "", nil
}
