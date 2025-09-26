package twozoracle

import (
	"context"
	"log/slog"
	"strconv"
	"time"
)

const (
	watcherName = "twozoracle"
)

type TwoZOracleWatcher struct {
	log *slog.Logger
	cfg *Config
}

func NewTwoZOracleWatcher(cfg *Config) (*TwoZOracleWatcher, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &TwoZOracleWatcher{
		log: cfg.Logger.With("watcher", watcherName),
		cfg: cfg,
	}, nil
}

func (w *TwoZOracleWatcher) Name() string {
	return watcherName
}

func (w *TwoZOracleWatcher) Run(ctx context.Context) error {
	ticker := time.NewTicker(w.cfg.Interval)
	defer ticker.Stop()

	err := w.Tick(ctx)
	if err != nil {
		w.log.Error("failed to tick", "error", err)
	}

	for {
		select {
		case <-ctx.Done():
			w.log.Debug("context done, stopping")
			return nil
		case <-ticker.C:
			err := w.Tick(ctx)
			if err != nil {
				w.log.Error("failed to tick", "error", err)
			}
		}
	}
}

func (w *TwoZOracleWatcher) Tick(ctx context.Context) error {
	w.log.Debug("ticking twozoracle")

	health, statusCode, err := w.cfg.Client.Health(ctx)
	if err != nil {
		if statusCode != 0 {
			MetricHealthResponse.WithLabelValues(strconv.Itoa(statusCode)).Inc()
		}
		MetricErrors.WithLabelValues(MetricErrorTypeGetHealth, strconv.Itoa(statusCode)).Inc()
		w.log.Info("failed to get health", "error", err)
		return nil
	}
	MetricHealthResponse.WithLabelValues(strconv.Itoa(statusCode)).Inc()
	w.log.Debug("health", "health", health)
	if !health.Healthy {
		w.log.Warn("health is not healthy", "health", health)
		MetricHealthNotHealthy.Inc()
	}

	swapRate, statusCode, err := w.cfg.Client.SwapRate(ctx)
	if err != nil {
		if statusCode != 0 {
			MetricSwapRateResponse.WithLabelValues(strconv.Itoa(statusCode)).Inc()
		}
		MetricErrors.WithLabelValues(MetricErrorTypeGetSwapRate, strconv.Itoa(statusCode)).Inc()
		w.log.Info("failed to get swap rate", "error", err)
		return nil
	}
	MetricSwapRateResponse.WithLabelValues(strconv.Itoa(statusCode)).Inc()
	w.log.Debug("swap rate", "swapRate", swapRate)
	MetricSwapRate.Set(float64(swapRate.SwapRate))
	solPriceUSD, err := strconv.ParseFloat(swapRate.SOLPriceUSD, 64)
	if err != nil {
		MetricErrors.WithLabelValues(MetricErrorTypeParseSOLPriceUSD, strconv.Itoa(0)).Inc()
		w.log.Info("failed to parse sol price usd", "error", err)
		return nil
	}
	MetricSOLPriceUSD.Set(solPriceUSD)
	twoZPriceUSD, err := strconv.ParseFloat(swapRate.TwoZPriceUSD, 64)
	if err != nil {
		MetricErrors.WithLabelValues(MetricErrorTypeParseTwoZPriceUSD, strconv.Itoa(0)).Inc()
		w.log.Info("failed to parse twoz price usd", "error", err)
		return nil
	}
	MetricTwoZPriceUSD.Set(twoZPriceUSD)

	return nil
}
