package routing

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
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
	watcher *fsnotify.Watcher
	done    chan struct{}
}

func NewConfiguredRouteReaderWriter(log *slog.Logger, nlr Netlinker, path string) *ConfiguredRouteReaderWriter {
	cfg, err := loadConfig(path)
	if err != nil {
		log.Error("error loading route config", "error", err)
		return nil
	}
	c := &ConfiguredRouteReaderWriter{
		log:     log,
		nlr:     nlr,
		path:    path,
		exclude: makeExcludeMap(cfg.Exclude),
		done:    make(chan struct{}),
	}
	c.watchConfig()
	return c
}

func (c *ConfiguredRouteReaderWriter) RouteAdd(r *Route) error {
	c.mu.RLock()
	_, excluded := c.exclude[r.Dst.String()]
	c.mu.RUnlock()
	if excluded {
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

func (c *ConfiguredRouteReaderWriter) Close() error {
	close(c.done)
	if c.watcher != nil {
		return c.watcher.Close()
	}
	return nil
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

func (c *ConfiguredRouteReaderWriter) watchConfig() {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		c.log.Error("failed to create watcher", "error", err)
		return
	}
	c.watcher = w

	parent := filepath.Dir(c.path)
	if err := w.Add(parent); err != nil {
		c.log.Error("failed to add watch", "path", parent, "error", err)
		return
	}

	// tiny debounce to coalesce bursts
	type tick struct{}
	debounce := make(chan tick, 1)
	trigger := func() {
		select {
		case debounce <- tick{}:
		default:
		}
	}

	go func() {
		defer c.log.Debug("config watcher stopped")
		for {
			select {
			case <-c.done:
				return

			case evt, ok := <-w.Events:
				if !ok {
					return
				}
				// We watch the parent dir; only care about our file.
				if evt.Name != c.path {
					break
				}

				// Any content-affecting op should prompt a reload.
				if evt.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename|fsnotify.Remove|fsnotify.Chmod) != 0 {
					trigger()
				}

			case err, ok := <-w.Errors:
				if !ok {
					return
				}
				c.log.Error("watcher error", "error", err)

			case <-debounce:
				// Debounce window
				time.Sleep(50 * time.Millisecond)

				cfg, err := loadConfig(c.path)
				if err != nil {
					// Could be a partial write or transient absence during rename.
					c.log.Warn("reload skipped; config not readable yet", "error", err)
					// Try once more shortly after; if still bad, the next FS event will retrigger anyway.
					time.Sleep(100 * time.Millisecond)
					if cfg, err = loadConfig(c.path); err != nil {
						c.log.Error("failed to reload config", "error", err)
						break
					}
				}

				c.mu.Lock()
				c.exclude = makeExcludeMap(cfg.Exclude)
				c.mu.Unlock()
				c.log.Info("reloaded route config", "exclude", cfg.Exclude)
			}
		}
	}()
}
