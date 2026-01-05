package tools

import (
	"context"
	"encoding/json"

	"github.com/malbeclabs/doublezero/lake/pkg/querier"
)

// Querier is an interface for executing SQL queries.
type Querier interface {
	Query(ctx context.Context, sql string) (querier.QueryResponse, error)
}

// QueryResponse extends querier.QueryResponse with a ToJSON method.
type QueryResponse struct {
	querier.QueryResponse
}

// ToJSON converts the QueryResponse to a JSON string.
func (r *QueryResponse) ToJSON() (string, error) {
	bytes, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

