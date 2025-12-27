package querier

import (
	"fmt"
	"log/slog"

	"github.com/malbeclabs/doublezero/tools/dz-ai/internal/data/duck"
	"github.com/malbeclabs/doublezero/tools/dz-ai/internal/data/indexer/schema"
)

type Config struct {
	Logger  *slog.Logger
	DB      duck.DB
	Schemas []*schema.Schema
}

func (cfg *Config) Validate() error {
	if cfg.Logger == nil {
		return fmt.Errorf("logger is required")
	}
	if cfg.DB == nil {
		return fmt.Errorf("database is required")
	}
	if len(cfg.Schemas) == 0 {
		return fmt.Errorf("schemas are required")
	}
	return nil
}
