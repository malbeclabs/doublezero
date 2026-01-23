package handlers_test

import (
	"net/http/httptest"
	"testing"

	"github.com/malbeclabs/doublezero/lake/api/handlers"
	"github.com/stretchr/testify/assert"
)

func TestParsePagination_Defaults(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/test", nil)

	params := handlers.ParsePagination(req, 0)

	assert.Equal(t, handlers.DefaultLimit, params.Limit)
	assert.Equal(t, 0, params.Offset)
}

func TestParsePagination_Custom(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/test?limit=25&offset=50", nil)

	params := handlers.ParsePagination(req, 0)

	assert.Equal(t, 25, params.Limit)
	assert.Equal(t, 50, params.Offset)
}

func TestParsePagination_CustomDefault(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/test", nil)

	params := handlers.ParsePagination(req, 50)

	assert.Equal(t, 50, params.Limit)
	assert.Equal(t, 0, params.Offset)
}

func TestParsePagination_MaxLimit(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/test?limit=5000", nil)

	params := handlers.ParsePagination(req, 0)

	assert.Equal(t, handlers.MaxLimit, params.Limit)
}

func TestParsePagination_NegativeValues(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/test?limit=-10&offset=-5", nil)

	params := handlers.ParsePagination(req, 100)

	// Negative limit should use default
	assert.Equal(t, 100, params.Limit)
	// Negative offset should stay at 0
	assert.Equal(t, 0, params.Offset)
}

func TestParsePagination_InvalidValues(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/test?limit=abc&offset=xyz", nil)

	params := handlers.ParsePagination(req, 100)

	// Invalid values should use defaults
	assert.Equal(t, 100, params.Limit)
	assert.Equal(t, 0, params.Offset)
}

func TestParsePagination_ZeroLimit(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/test?limit=0", nil)

	params := handlers.ParsePagination(req, 100)

	// Zero limit should use default
	assert.Equal(t, 100, params.Limit)
}

func TestParsePagination_ExactMaxLimit(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/test?limit=1000", nil)

	params := handlers.ParsePagination(req, 0)

	assert.Equal(t, 1000, params.Limit)
}

func TestParsePagination_JustOverMaxLimit(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/test?limit=1001", nil)

	params := handlers.ParsePagination(req, 0)

	assert.Equal(t, handlers.MaxLimit, params.Limit)
}

func TestPaginatedResponse_JSONStructure(t *testing.T) {
	// Test that the generic type works correctly
	type Item struct {
		Name string `json:"name"`
	}

	response := handlers.PaginatedResponse[Item]{
		Items:  []Item{{Name: "test1"}, {Name: "test2"}},
		Total:  100,
		Limit:  10,
		Offset: 0,
	}

	assert.Len(t, response.Items, 2)
	assert.Equal(t, 100, response.Total)
	assert.Equal(t, 10, response.Limit)
	assert.Equal(t, 0, response.Offset)
}
