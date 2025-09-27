package serviceability

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
)

type ServiceabilityClient interface {
	GetProgramData(context.Context) (*serviceability.ProgramData, error)
}

type InfluxWriter interface {
	Errors() <-chan error
	WriteRecord(string)
	Flush()
}

type Config struct {
	Logger          *slog.Logger
	Serviceability  ServiceabilityClient
	Interval        time.Duration
	SlackWebhookURL string
	InfluxWriter    InfluxWriter
	Env             string
}

func (c *Config) Validate() error {
	if c.Logger == nil {
		return errors.New("logger is required")
	}
	if c.Serviceability == nil {
		return errors.New("serviceability client is required")
	}
	if c.Interval <= 0 {
		return errors.New("interval must be greater than 0")
	}
	return nil
}
