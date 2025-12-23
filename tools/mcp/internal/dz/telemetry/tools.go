package dztelem

import (
	"fmt"
	"log/slog"

	"github.com/malbeclabs/doublezero/tools/mcp/internal/duck"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Tools struct {
	log *slog.Logger
	db  duck.DB
}

func NewTools(log *slog.Logger, db duck.DB) *Tools {
	return &Tools{
		log: log,
		db:  db,
	}
}

func (t *Tools) Register(server *mcp.Server) error {
	if err := t.validateSchema(); err != nil {
		return fmt.Errorf("schema validation failed: %w", err)
	}
	if err := t.registerSchema(server); err != nil {
		return fmt.Errorf("failed to register schema tool: %w", err)
	}
	return nil
}
