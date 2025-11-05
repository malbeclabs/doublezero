package routing

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
)

type RouteConfig struct {
	Exclude []string `json:"exclude"`
}

type ConfiguredRouteReaderWriter struct {
	log     *slog.Logger
	nlr     Netlinker
	path    string
	mu      sync.RWMutex
	exclude map[string]struct{}
}

func NewConfiguredRouteReaderWriter(log *slog.Logger, nlr Netlinker, path string) *ConfiguredRouteReaderWriter {
	cfg, err := loadConfig(path)
	if err != nil {
		log.Error("error loading route config", "error", err)
		return nil
	}

	slog.Info("routes: loaded routes", "routes", len(cfg.Exclude))
	c := &ConfiguredRouteReaderWriter{
		log:     log,
		nlr:     nlr,
		path:    path,
		exclude: makeExcludeMap(cfg.Exclude),
	}
	return c
}

func (c *ConfiguredRouteReaderWriter) RouteAdd(r *Route) error {
	c.mu.RLock()
	_, excluded := c.exclude[r.Dst.IP.String()]
	c.mu.RUnlock()
	if excluded {
		slog.Info("routes: excluding configured route", "route", r.String())
		return nil
	}
	return c.nlr.RouteAdd(r)
}

func (c *ConfiguredRouteReaderWriter) RouteDelete(r *Route) error {
	return c.nlr.RouteDelete(r)
}

func (c *ConfiguredRouteReaderWriter) RouteByProtocol(protocol int) ([]*Route, error) {
	return c.nlr.RouteByProtocol(protocol)
}

func loadConfig(path string) (*RouteConfig, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("error opening route config file: %v", err)
	}
	defer f.Close()
	var cfg RouteConfig
	if err := json.NewDecoder(f).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("error decoding route config file: %v", err)
	}
	return &cfg, nil
}

func makeExcludeMap(exclude []string) map[string]struct{} {
	m := make(map[string]struct{}, len(exclude))
	for _, e := range exclude {
		m[e] = struct{}{}
	}
	return m
}
