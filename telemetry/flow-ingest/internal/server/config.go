package server

import (
	"errors"
	"log/slog"
	"net"
	"runtime"
	"time"

	"github.com/jonboulle/clockwork"
)

type Config struct {
	Logger         *slog.Logger
	Clock          clockwork.Clock
	FlowListener   *net.UDPConn
	HealthListener net.Listener
	KafkaClient    KafkaClient
	KafkaTopic     string

	// Optional with defaults.
	ReadTimeout       time.Duration
	WorkerCount       int
	BufferSizePackets int
	BufferSizeBytes   int
}

func (c *Config) Validate() error {
	if c.Logger == nil {
		return errors.New("logger is required")
	}
	if c.Clock == nil {
		c.Clock = clockwork.NewRealClock()
	}
	if c.FlowListener == nil {
		return errors.New("flow listener is required")
	}
	if c.HealthListener == nil {
		return errors.New("health listener is required")
	}
	if c.KafkaClient == nil {
		return errors.New("kafka client is required")
	}
	if c.KafkaTopic == "" {
		return errors.New("kafka topic is required")
	}

	if c.ReadTimeout == 0 {
		c.ReadTimeout = defaultReadTimeout
	}
	if c.ReadTimeout <= 0 {
		return errors.New("read timeout must be > 0")
	}

	if c.WorkerCount == defaultWorkerCount {
		c.WorkerCount = runtime.NumCPU()
	}
	if c.WorkerCount <= 0 {
		return errors.New("worker count must be > 0")
	}

	if c.BufferSizePackets == 0 {
		c.BufferSizePackets = defaultBufferSizePackets
	}
	if c.BufferSizePackets <= 0 {
		return errors.New("buffer size packets must be > 0")
	}

	if c.BufferSizeBytes == 0 {
		c.BufferSizeBytes = defaultBufferSizeBytes
	}
	if c.BufferSizeBytes <= 0 {
		return errors.New("buffer size bytes must be > 0")
	}

	return nil
}
