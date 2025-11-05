package routing

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
)

type RouteConfig struct {
	Exclude []string `json:"exclude"`
}

type ConfiguredRouteReaderWriter struct {
	log     *slog.Logger
	nlr     Netlinker
	exclude map[string]struct{}
}

func NewConfiguredRouteReaderWriter(log *slog.Logger, nlr Netlinker, path string) (*ConfiguredRouteReaderWriter, error) {
	cfg, err := loadConfig(path)
	if err != nil {
		return nil, fmt.Errorf("error loading route config: %v", err)
	}
	log.Info("routes: loaded routes", "routes", len(cfg.Exclude))
	return &ConfiguredRouteReaderWriter{
		log:     log,
		nlr:     nlr,
		exclude: makeExcludeMap(cfg.Exclude),
	}, nil
}

func (c *ConfiguredRouteReaderWriter) RouteAdd(r *Route) error {
	_, ok := c.exclude[r.Dst.IP.String()]
	if ok {
		c.log.Info("routes: excluding configured route", "route", r.String())
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
