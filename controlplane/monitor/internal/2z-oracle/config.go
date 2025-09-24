package twozoracle

import (
	"errors"
	"log/slog"
	"time"
)

type Config struct {
	Logger   *slog.Logger
	Interval time.Duration
	Client   TwoZOracleClient
}

func (c *Config) Validate() error {
	if c.Logger == nil {
		return errors.New("logger is required")
	}
	if c.Interval <= 0 {
		return errors.New("interval must be greater than 0")
	}
	if c.Client == nil {
		return errors.New("client is required")
	}
	return nil
}
