package routing

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"sync"
)

type RouteConfig struct {
	Exclude []string `json:"exclude"`
}

type ConfiguredRoutes struct {
	cfg     *RouteConfig
	exclude map[string]struct{}
	mu      sync.Mutex
}

func NewConfiguredRoutes(path string) (*ConfiguredRoutes, error) {
	cfg, err := loadConfig(path)
	if err != nil {
		return nil, fmt.Errorf("error loading route config: %v", err)
	}
	return &ConfiguredRoutes{
		cfg:     cfg,
		exclude: makeExcludeMap(cfg.Exclude),
	}, nil
}

func (c *ConfiguredRoutes) GetExcluded() map[string]struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.exclude
}

func (c *ConfiguredRoutes) IsExcluded(ip string) bool {
	_, ok := c.GetExcluded()[ip]
	return ok
}

type ConfiguredRouteReaderWriter struct {
	log *slog.Logger
	nlr Netlinker
	cfg *ConfiguredRoutes
}

func NewConfiguredRouteReaderWriter(log *slog.Logger, nlr Netlinker, cfg *ConfiguredRoutes) (*ConfiguredRouteReaderWriter, error) {
	log.Info("routes: loaded routes", "excluded", len(cfg.GetExcluded()))
	return &ConfiguredRouteReaderWriter{
		log: log,
		nlr: nlr,
		cfg: cfg,
	}, nil
}

func (c *ConfiguredRouteReaderWriter) RouteAdd(r *Route) error {
	if c.cfg.IsExcluded(r.Dst.IP.String()) {
		c.log.Info("routes: excluding configured route from route add", "route", r.String())
		return nil
	}
	return c.nlr.RouteAdd(r)
}

func (c *ConfiguredRouteReaderWriter) RouteDelete(r *Route) error {
	if c.cfg.IsExcluded(r.Dst.IP.String()) {
		c.log.Info("routes: excluding configured route from route delete", "route", r.String())
		return nil
	}
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

	for _, ip := range cfg.Exclude {
		if net.ParseIP(ip) == nil {
			return nil, fmt.Errorf("invalid ip: %s", ip)
		}
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
