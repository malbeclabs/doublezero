package handlers

import (
	"net/http"
	"strconv"
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
