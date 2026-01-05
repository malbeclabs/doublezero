package querier

import (
	"context"
	"fmt"
	"log/slog"
)

type Querier struct {
	log *slog.Logger
	cfg Config
}

func New(cfg Config) (*Querier, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("failed to validate querier config: %w", err)
	}
	return &Querier{
		log: cfg.Logger,
		cfg: cfg,
	}, nil
}

type QueryResponse struct {
	Columns     []string     `json:"columns"`
	ColumnTypes []ColumnType `json:"column_types"`
	Rows        []QueryRow   `json:"rows"`
	Count       int          `json:"count"`
}

type ColumnType struct {
	Name             string
	DatabaseTypeName string
	ScanType         string
}

type QueryRow map[string]any

func (q *Querier) Ready() bool {
	// Querier is ready if the database is available
	return q.cfg.DB != nil
}

func (q *Querier) Query(ctx context.Context, sql string) (QueryResponse, error) {
	conn, err := q.cfg.DB.Conn(ctx)
	if err != nil {
		return QueryResponse{}, fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()

	rows, err := conn.QueryContext(ctx, sql)
	if err != nil {
		return QueryResponse{}, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return QueryResponse{}, fmt.Errorf("failed to get columns: %w", err)
	}

	// Get column types
	columnTypes, err := rows.ColumnTypes()
	if err != nil {
		return QueryResponse{}, fmt.Errorf("failed to get column types: %w", err)
	}

	// Build column type information
	colTypeInfo := make([]ColumnType, len(columns))
	for i, colType := range columnTypes {
		colTypeInfo[i] = ColumnType{
			Name:             colType.Name(),
			DatabaseTypeName: colType.DatabaseTypeName(),
			ScanType:         "",
		}
		if colType.ScanType() != nil {
			colTypeInfo[i].ScanType = colType.ScanType().String()
		}
	}

	var resultRows []QueryRow
	for rows.Next() {
		values := make([]any, len(columns))
		valuePtrs := make([]any, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return QueryResponse{}, fmt.Errorf("failed to scan row: %w", err)
		}

		row := make(QueryRow)
		for i, col := range columns {
			val := values[i]
			if val == nil {
				row[col] = nil
			} else {
				switch v := val.(type) {
				case []byte:
					row[col] = string(v)
				default:
					row[col] = val
				}
			}
		}
		resultRows = append(resultRows, row)
	}

	if err := rows.Err(); err != nil {
		return QueryResponse{}, fmt.Errorf("error iterating rows: %w", err)
	}

	return QueryResponse{
		Columns:     columns,
		ColumnTypes: colTypeInfo,
		Rows:        resultRows,
		Count:       len(resultRows),
	}, nil
}
