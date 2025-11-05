package liveness

import (
	"errors"
	"time"
)

type Config struct {
	Iface string
	Port  int

	TxMin      time.Duration
	RxMin      time.Duration
	DetectMult uint8
}

func (c *Config) Validate() error {
	if c.Iface == "" {
		return errors.New("iface is required")
	}
	if c.Port <= 0 {
		return errors.New("port must be greater than 0")
	}
	if c.TxMin <= 0 {
		return errors.New("txMin must be greater than 0")
	}
	if c.RxMin <= 0 {
		return errors.New("rxMin must be greater than 0")
	}
	if c.DetectMult <= 0 {
		return errors.New("detectMult must be greater than 0")
	}
	return nil
}
