package sqltools

import (
	"context"

	"github.com/malbeclabs/doublezero/tools/dz-ai/internal/mcp/duck"
)

type DB interface {
	Catalog() string
	Schema() string
	Conn(ctx context.Context) (duck.Connection, error)
}
