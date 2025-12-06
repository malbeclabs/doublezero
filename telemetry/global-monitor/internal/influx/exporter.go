package influx

import (
	"context"
	"errors"
	"log/slog"
	"time"

	influxdb2api "github.com/influxdata/influxdb-client-go/v2/api"

	write "github.com/influxdata/influxdb-client-go/v2/api/write"
)

type Sample interface {
	Points() []*write.Point
}

type ExporterConfig struct {
	Logger     *slog.Logger
	WriteAPI   influxdb2api.WriteAPIBlocking
	BatchSize  int
	FlushEvery time.Duration
}

type Exporter struct {
	log        *slog.Logger
	writeAPI   influxdb2api.WriteAPIBlocking
	batchSize  int
	flushEvery time.Duration
}

func NewExporter(cfg ExporterConfig) *Exporter {
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 500
	}
	if cfg.FlushEvery <= 0 {
		cfg.FlushEvery = 3 * time.Second
	}
	return &Exporter{
		log:        cfg.Logger,
		writeAPI:   cfg.WriteAPI,
		batchSize:  cfg.BatchSize,
		flushEvery: cfg.FlushEvery,
	}
}

func (e *Exporter) Start(ctx context.Context, in <-chan Sample, cancel context.CancelFunc) {
	go e.run(ctx, in, cancel)
}

func (e *Exporter) run(ctx context.Context, in <-chan Sample, cancel context.CancelFunc) {
	buf := make([]*write.Point, 0, e.batchSize)
	ticker := time.NewTicker(e.flushEvery)
	defer ticker.Stop()

	flush := func() {
		if len(buf) == 0 {
			return
		}
		if err := e.writeAPI.WritePoint(ctx, buf...); err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			e.log.Error("failed to write metrics batch", "error", err, "points", len(buf))
		}
		e.log.Info("influx: wrote metrics batch", "points", len(buf))
		buf = buf[:0]
	}

	for {
		select {
		case <-ctx.Done():
			flush()
			cancel()
			return
		case s, ok := <-in:
			if !ok {
				flush()
				cancel()
				return
			}
			buf = append(buf, s.Points()...)
			if len(buf) >= e.batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}
