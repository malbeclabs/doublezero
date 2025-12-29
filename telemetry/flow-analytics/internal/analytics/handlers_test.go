package analytics

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

// mockQuerier implements Querier for testing.
type mockQuerier struct {
	pingErr  error
	queryErr error
	rows     *mockRows
}

func (m *mockQuerier) Query(_ context.Context, _ string, _ ...any) (driver.Rows, error) {
	if m.queryErr != nil {
		return nil, m.queryErr
	}
	return m.rows, nil
}

func (m *mockQuerier) Ping(_ context.Context) error {
	return m.pingErr
}

// mockRows implements driver.Rows for testing.
type mockRows struct {
	data    [][]any
	index   int
	columns []string
	scanErr error
}

func (m *mockRows) Next() bool {
	if m.index >= len(m.data) {
		return false
	}
	m.index++
	return true
}

func (m *mockRows) Scan(dest ...any) error {
	if m.scanErr != nil {
		return m.scanErr
	}
	if m.index == 0 || m.index > len(m.data) {
		return errors.New("no current row")
	}
	row := m.data[m.index-1]
	for i, d := range dest {
		if i < len(row) {
			switch v := d.(type) {
			case *string:
				if s, ok := row[i].(string); ok {
					*v = s
				}
			}
		}
	}
	return nil
}

func (m *mockRows) Close() error                     { return nil }
func (m *mockRows) Columns() []string                { return m.columns }
func (m *mockRows) ColumnTypes() []driver.ColumnType { return nil }
func (m *mockRows) Err() error                       { return nil }
func (m *mockRows) Totals(_ ...any) error            { return nil }
func (m *mockRows) ScanStruct(_ any) error           { return nil }

func newTestApp(t *testing.T, querier Querier) *App {
	t.Helper()
	app, err := NewApp(
		WithQuerier(querier),
		WithTableName("test_flows"),
	)
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	return app
}

func TestHandleHealthz(t *testing.T) {
	tests := []struct {
		name           string
		pingErr        error
		expectedStatus int
		expectedBody   string
	}{
		{
			name:           "healthy",
			pingErr:        nil,
			expectedStatus: http.StatusOK,
			expectedBody:   "ok",
		},
		{
			name:           "unhealthy - ping fails",
			pingErr:        errors.New("connection refused"),
			expectedStatus: http.StatusServiceUnavailable,
			expectedBody:   "unhealthy: database connection failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := newTestApp(t, &mockQuerier{pingErr: tt.pingErr})

			req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
			rec := httptest.NewRecorder()

			app.HandleHealthz(rec, req)

			if rec.Code != tt.expectedStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.expectedStatus)
			}
			if body := rec.Body.String(); body != tt.expectedBody {
				t.Errorf("body = %q, want %q", body, tt.expectedBody)
			}
		})
	}
}

func TestHandleColumns(t *testing.T) {
	app := newTestApp(t, &mockQuerier{})

	tests := []struct {
		name          string
		category      string
		checkResponse func(t *testing.T, cols []ColumnInfo)
	}{
		{
			name:     "all columns",
			category: "",
			checkResponse: func(t *testing.T, cols []ColumnInfo) {
				if len(cols) == 0 {
					t.Error("expected columns, got none")
				}
			},
		},
		{
			name:     "dimension columns only",
			category: "dimension",
			checkResponse: func(t *testing.T, cols []ColumnInfo) {
				for _, col := range cols {
					if col.Category != "dimension" {
						t.Errorf("expected dimension, got %q", col.Category)
					}
				}
			},
		},
		{
			name:     "metric columns only",
			category: "metric",
			checkResponse: func(t *testing.T, cols []ColumnInfo) {
				if len(cols) == 0 {
					t.Error("expected metric columns")
				}
				for _, col := range cols {
					if col.Category != "metric" {
						t.Errorf("expected metric, got %q", col.Category)
					}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := "/api/columns"
			if tt.category != "" {
				url += "?category=" + tt.category
			}
			req := httptest.NewRequest(http.MethodGet, url, nil)
			rec := httptest.NewRecorder()

			app.HandleColumns(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
			}

			var cols []ColumnInfo
			if err := json.Unmarshal(rec.Body.Bytes(), &cols); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}

			tt.checkResponse(t, cols)
		})
	}
}

func TestHandleTypeahead_InvalidColumn(t *testing.T) {
	app := newTestApp(t, &mockQuerier{})

	req := httptest.NewRequest(http.MethodGet, "/api/typeahead?column=invalid_column", nil)
	rec := httptest.NewRecorder()

	app.HandleTypeahead(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandleTypeahead_DatabaseError(t *testing.T) {
	app := newTestApp(t, &mockQuerier{
		queryErr: errors.New("database unavailable"),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/typeahead?column=src_addr", nil)
	rec := httptest.NewRecorder()

	app.HandleTypeahead(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

func TestHandleTypeahead_EmptyResults(t *testing.T) {
	app := newTestApp(t, &mockQuerier{
		rows: &mockRows{data: [][]any{}},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/typeahead?column=src_addr", nil)
	rec := httptest.NewRecorder()

	app.HandleTypeahead(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), "typeahead-empty") {
		t.Error("expected empty typeahead message")
	}
}

func TestHandleTypeahead_WithResults(t *testing.T) {
	app := newTestApp(t, &mockQuerier{
		rows: &mockRows{
			data: [][]any{
				{"192.168.1.1"},
				{"192.168.1.2"},
			},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/typeahead?column=src_addr", nil)
	rec := httptest.NewRecorder()

	app.HandleTypeahead(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "192.168.1.1") {
		t.Error("expected first value in response")
	}
	if !strings.Contains(body, "192.168.1.2") {
		t.Error("expected second value in response")
	}
	if !strings.Contains(body, "typeahead-item") {
		t.Error("expected typeahead-item class")
	}
}

func TestHandleQuery_MethodNotAllowed(t *testing.T) {
	app := newTestApp(t, &mockQuerier{})

	req := httptest.NewRequest(http.MethodGet, "/api/query", nil)
	rec := httptest.NewRecorder()

	app.HandleQuery(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandleQuery_InvalidJSON(t *testing.T) {
	app := newTestApp(t, &mockQuerier{})

	req := httptest.NewRequest(http.MethodPost, "/api/query", strings.NewReader("not json"))
	rec := httptest.NewRecorder()

	app.HandleQuery(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestNewApp_RequiresQuerier(t *testing.T) {
	_, err := NewApp(WithTableName("test"))
	if err == nil {
		t.Error("expected error when querier not provided")
	}
	if !strings.Contains(err.Error(), "querier is required") {
		t.Errorf("unexpected error: %v", err)
	}
}
