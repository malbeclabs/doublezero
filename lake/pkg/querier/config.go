package querier

import (
	"fmt"
	"log/slog"

	"github.com/malbeclabs/doublezero/lake/pkg/duck"
)

type Config struct {
	Logger *slog.Logger
	DB     duck.DB
}

func (cfg *Config) Validate() error {
	if cfg.Logger == nil {
		return fmt.Errorf("logger is required")
	}
	if cfg.DB == nil {
		return fmt.Errorf("database is required")
	}
	return nil
}
