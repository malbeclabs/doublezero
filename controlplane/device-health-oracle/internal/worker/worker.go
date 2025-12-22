package worker

import (
	"context"
	"log/slog"
	"time"
)

type Worker struct {
	log *slog.Logger
	cfg *Config
}

func New(cfg *Config) (*Worker, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &Worker{
		log: cfg.Logger,
		cfg: cfg,
	}, nil
}

func (w *Worker) Run(ctx context.Context) error {
	w.log.Info("Starting worker", "env", w.cfg.Env)

	ticker := time.NewTicker(w.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.log.Info("Shutting down worker")
			return nil
		case <-ticker.C:
			w.log.Info("Device health oracle tick")
		}
	}
}
