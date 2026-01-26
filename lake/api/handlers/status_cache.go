package handlers

import (
	"context"
	"log"
	"strconv"
	"sync"
	"time"
)

const cacheStopTimeout = 5 * time.Second

// StatusCache provides periodic background caching for status endpoints.
// This ensures fast initial page loads by pre-computing expensive queries.
type StatusCache struct {
	mu sync.RWMutex

	// Cached responses
	status            *StatusResponse
	linkHistory       map[string]*LinkHistoryResponse   // keyed by "range:buckets" e.g. "24h:72"
	deviceHistory     map[string]*DeviceHistoryResponse // keyed by "range:buckets" e.g. "24h:72"
	timeline          *TimelineResponse                 // default 24h timeline
	outages           *LinkOutagesResponse              // default 24h outages
	latencyComparison *LatencyComparisonResponse        // DZ vs Internet latency comparison
	metroPathLatency  map[string]*MetroPathLatencyResponse // keyed by optimize strategy (hops, latency, bandwidth)

	// Refresh intervals
	statusInterval      time.Duration
	linkHistoryInterval time.Duration
	timelineInterval    time.Duration
	outagesInterval     time.Duration
	performanceInterval time.Duration // for latency comparison and metro path latency

	// Last refresh times (for observability)
	statusLastRefresh            time.Time
	linkHistoryLastRefresh       time.Time
	deviceHistoryLastRefresh     time.Time
	timelineLastRefresh          time.Time
	outagesLastRefresh           time.Time
	latencyComparisonLastRefresh time.Time
	metroPathLatencyLastRefresh  time.Time

	// Context for cancellation
	ctx    context.Context
	cancel context.CancelFunc

	// WaitGroup to track running goroutines
	wg sync.WaitGroup
}

// Common link history configurations to pre-cache
var linkHistoryConfigs = []struct {
	timeRange string
	buckets   int
}{
	{"3h", 36},   // 3-hour view
	{"6h", 36},   // 6-hour view
	{"12h", 48},  // 12-hour view (default filter)
	{"24h", 72},  // 24-hour view
	{"24h", 48},  // 24-hour responsive (smaller screens)
	{"3d", 72},   // 3-day view
	{"7d", 84},   // 7-day view
}

// Common device history configurations to pre-cache (same as link history)
var deviceHistoryConfigs = []struct {
	timeRange string
	buckets   int
}{
	{"3h", 36},   // 3-hour view
	{"6h", 36},   // 6-hour view
	{"12h", 48},  // 12-hour view (default filter)
	{"24h", 72},  // 24-hour view
	{"24h", 48},  // 24-hour responsive (smaller screens)
	{"3d", 72},   // 3-day view
	{"7d", 84},   // 7-day view
}

// NewStatusCache creates a new cache with the specified refresh intervals.
func NewStatusCache(statusInterval, linkHistoryInterval, timelineInterval, outagesInterval, performanceInterval time.Duration) *StatusCache {
	ctx, cancel := context.WithCancel(context.Background())
	return &StatusCache{
		linkHistory:         make(map[string]*LinkHistoryResponse),
		deviceHistory:       make(map[string]*DeviceHistoryResponse),
		metroPathLatency:    make(map[string]*MetroPathLatencyResponse),
		statusInterval:      statusInterval,
		linkHistoryInterval: linkHistoryInterval,
		timelineInterval:    timelineInterval,
		outagesInterval:     outagesInterval,
		performanceInterval: performanceInterval,
		ctx:                 ctx,
		cancel:              cancel,
	}
}

// Start begins background refresh goroutines.
// It performs an initial refresh synchronously to ensure cache is warm before returning.
func (c *StatusCache) Start() {
	log.Printf("Starting status cache with intervals: status=%v, linkHistory=%v, timeline=%v, outages=%v, performance=%v",
		c.statusInterval, c.linkHistoryInterval, c.timelineInterval, c.outagesInterval, c.performanceInterval)

	// Initial refresh (synchronous to ensure cache is warm)
	c.refreshStatus()
	c.refreshLinkHistory()
	c.refreshDeviceHistory()
	c.refreshTimeline()
	c.refreshOutages()
	c.refreshLatencyComparison()
	c.refreshMetroPathLatency()

	// Start background refresh goroutines
	c.wg.Add(7)
	go c.statusRefreshLoop()
	go c.linkHistoryRefreshLoop()
	go c.deviceHistoryRefreshLoop()
	go c.timelineRefreshLoop()
	go c.outagesRefreshLoop()
	go c.latencyComparisonRefreshLoop()
	go c.metroPathLatencyRefreshLoop()
}

// Stop cancels the background refresh goroutines and waits for them to exit.
func (c *StatusCache) Stop() {
	log.Println("Stopping status cache...")
	c.cancel()

	// Wait for goroutines to exit with a timeout
	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Println("Status cache stopped")
	case <-time.After(cacheStopTimeout):
		log.Println("Status cache stop timed out, continuing shutdown")
	}
}

// IsReady returns true if the cache has been populated with initial data.
func (c *StatusCache) IsReady() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.status != nil && c.timeline != nil && c.outages != nil
}

// GetStatus returns the cached status response.
// Returns nil if cache is empty (should not happen after Start() completes).
func (c *StatusCache) GetStatus() *StatusResponse {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.status
}

// GetLinkHistory returns the cached link history response for the given parameters.
// Returns nil if the specific configuration is not cached.
func (c *StatusCache) GetLinkHistory(timeRange string, buckets int) *LinkHistoryResponse {
	c.mu.RLock()
	defer c.mu.RUnlock()
	key := linkHistoryCacheKey(timeRange, buckets)
	return c.linkHistory[key]
}

// GetDeviceHistory returns the cached device history response for the given parameters.
// Returns nil if the specific configuration is not cached.
func (c *StatusCache) GetDeviceHistory(timeRange string, buckets int) *DeviceHistoryResponse {
	c.mu.RLock()
	defer c.mu.RUnlock()
	key := deviceHistoryCacheKey(timeRange, buckets)
	return c.deviceHistory[key]
}

// GetTimeline returns the cached default timeline response.
// Returns nil if cache is empty (should not happen after Start() completes).
func (c *StatusCache) GetTimeline() *TimelineResponse {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.timeline
}

// GetOutages returns the cached default outages response.
// Returns nil if cache is empty (should not happen after Start() completes).
func (c *StatusCache) GetOutages() *LinkOutagesResponse {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.outages
}

// GetLatencyComparison returns the cached DZ vs Internet latency comparison.
// Returns nil if cache is empty (should not happen after Start() completes).
func (c *StatusCache) GetLatencyComparison() *LatencyComparisonResponse {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.latencyComparison
}

// GetMetroPathLatency returns the cached metro path latency for the given optimize strategy.
// Returns nil if the specific strategy is not cached.
func (c *StatusCache) GetMetroPathLatency(optimize string) *MetroPathLatencyResponse {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.metroPathLatency[optimize]
}

// statusRefreshLoop runs the status refresh on a ticker.
func (c *StatusCache) statusRefreshLoop() {
	defer c.wg.Done()
	ticker := time.NewTicker(c.statusInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.refreshStatus()
		case <-c.ctx.Done():
			return
		}
	}
}

// linkHistoryRefreshLoop runs the link history refresh on a ticker.
func (c *StatusCache) linkHistoryRefreshLoop() {
	defer c.wg.Done()
	ticker := time.NewTicker(c.linkHistoryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.refreshLinkHistory()
		case <-c.ctx.Done():
			return
		}
	}
}

// deviceHistoryRefreshLoop runs the device history refresh on a ticker.
func (c *StatusCache) deviceHistoryRefreshLoop() {
	defer c.wg.Done()
	ticker := time.NewTicker(c.linkHistoryInterval) // Use same interval as link history
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.refreshDeviceHistory()
		case <-c.ctx.Done():
			return
		}
	}
}

// refreshStatus fetches fresh status data and updates the cache.
func (c *StatusCache) refreshStatus() {
	start := time.Now()
	ctx, cancel := context.WithTimeout(c.ctx, 15*time.Second)
	defer cancel()

	resp := fetchStatusData(ctx)

	c.mu.Lock()
	c.status = resp
	c.statusLastRefresh = time.Now()
	c.mu.Unlock()

	log.Printf("Status cache refreshed in %v", time.Since(start))
}

// refreshLinkHistory fetches fresh link history data for all configured ranges.
func (c *StatusCache) refreshLinkHistory() {
	start := time.Now()

	// Refresh all common configurations
	for _, cfg := range linkHistoryConfigs {
		ctx, cancel := context.WithTimeout(c.ctx, 20*time.Second)
		resp, err := fetchLinkHistoryData(ctx, cfg.timeRange, cfg.buckets)
		cancel()

		if err != nil {
			log.Printf("Link history cache refresh error (range=%s, buckets=%d): %v", cfg.timeRange, cfg.buckets, err)
			continue
		}
		key := linkHistoryCacheKey(cfg.timeRange, cfg.buckets)
		c.mu.Lock()
		c.linkHistory[key] = resp
		c.mu.Unlock()
	}

	c.mu.Lock()
	c.linkHistoryLastRefresh = time.Now()
	c.mu.Unlock()

	log.Printf("Link history cache refreshed in %v (%d configs)",
		time.Since(start), len(linkHistoryConfigs))
}

// refreshDeviceHistory fetches fresh device history data for all configured ranges.
func (c *StatusCache) refreshDeviceHistory() {
	start := time.Now()

	// Refresh all common configurations
	for _, cfg := range deviceHistoryConfigs {
		ctx, cancel := context.WithTimeout(c.ctx, 20*time.Second)
		resp, err := fetchDeviceHistoryData(ctx, cfg.timeRange, cfg.buckets)
		cancel()

		if err != nil {
			log.Printf("Device history cache refresh error (range=%s, buckets=%d): %v", cfg.timeRange, cfg.buckets, err)
			continue
		}
		key := deviceHistoryCacheKey(cfg.timeRange, cfg.buckets)
		c.mu.Lock()
		c.deviceHistory[key] = resp
		c.mu.Unlock()
	}

	c.mu.Lock()
	c.deviceHistoryLastRefresh = time.Now()
	c.mu.Unlock()

	log.Printf("Device history cache refreshed in %v (%d configs)",
		time.Since(start), len(deviceHistoryConfigs))
}

// timelineRefreshLoop runs the timeline refresh on a ticker.
func (c *StatusCache) timelineRefreshLoop() {
	defer c.wg.Done()
	ticker := time.NewTicker(c.timelineInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.refreshTimeline()
		case <-c.ctx.Done():
			return
		}
	}
}

// outagesRefreshLoop runs the outages refresh on a ticker.
func (c *StatusCache) outagesRefreshLoop() {
	defer c.wg.Done()
	ticker := time.NewTicker(c.outagesInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.refreshOutages()
		case <-c.ctx.Done():
			return
		}
	}
}

// refreshTimeline fetches fresh timeline data for the default 24h view.
func (c *StatusCache) refreshTimeline() {
	start := time.Now()
	ctx, cancel := context.WithTimeout(c.ctx, 30*time.Second)
	defer cancel()

	resp := fetchDefaultTimelineData(ctx)

	c.mu.Lock()
	c.timeline = resp
	c.timelineLastRefresh = time.Now()
	c.mu.Unlock()

	log.Printf("Timeline cache refreshed in %v (%d events)", time.Since(start), len(resp.Events))
}

// refreshOutages fetches fresh outages data for the default 24h view.
func (c *StatusCache) refreshOutages() {
	start := time.Now()
	ctx, cancel := context.WithTimeout(c.ctx, 30*time.Second)
	defer cancel()

	resp := fetchDefaultOutagesData(ctx)

	c.mu.Lock()
	c.outages = resp
	c.outagesLastRefresh = time.Now()
	c.mu.Unlock()

	log.Printf("Outages cache refreshed in %v (%d outages)", time.Since(start), len(resp.Outages))
}

// latencyComparisonRefreshLoop runs the latency comparison refresh on a ticker.
func (c *StatusCache) latencyComparisonRefreshLoop() {
	defer c.wg.Done()
	ticker := time.NewTicker(c.performanceInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.refreshLatencyComparison()
		case <-c.ctx.Done():
			return
		}
	}
}

// metroPathLatencyRefreshLoop runs the metro path latency refresh on a ticker.
func (c *StatusCache) metroPathLatencyRefreshLoop() {
	defer c.wg.Done()
	ticker := time.NewTicker(c.performanceInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.refreshMetroPathLatency()
		case <-c.ctx.Done():
			return
		}
	}
}

// refreshLatencyComparison fetches fresh DZ vs Internet latency comparison data.
func (c *StatusCache) refreshLatencyComparison() {
	start := time.Now()
	ctx, cancel := context.WithTimeout(c.ctx, 30*time.Second)
	defer cancel()

	resp, err := fetchLatencyComparisonData(ctx)
	if err != nil {
		log.Printf("Latency comparison cache refresh error: %v", err)
		return
	}

	c.mu.Lock()
	c.latencyComparison = resp
	c.latencyComparisonLastRefresh = time.Now()
	c.mu.Unlock()

	log.Printf("Latency comparison cache refreshed in %v (%d comparisons)", time.Since(start), len(resp.Comparisons))
}

// refreshMetroPathLatency fetches fresh metro path latency data for all optimization strategies.
func (c *StatusCache) refreshMetroPathLatency() {
	start := time.Now()

	// Cache all three optimization strategies
	strategies := []string{"latency", "hops", "bandwidth"}
	for _, strategy := range strategies {
		ctx, cancel := context.WithTimeout(c.ctx, 45*time.Second)
		resp, err := fetchMetroPathLatencyData(ctx, strategy)
		cancel()

		if err != nil {
			log.Printf("Metro path latency cache refresh error (optimize=%s): %v", strategy, err)
			continue
		}

		c.mu.Lock()
		c.metroPathLatency[strategy] = resp
		c.mu.Unlock()
	}

	c.mu.Lock()
	c.metroPathLatencyLastRefresh = time.Now()
	c.mu.Unlock()

	log.Printf("Metro path latency cache refreshed in %v (%d strategies)", time.Since(start), len(strategies))
}

func linkHistoryCacheKey(timeRange string, buckets int) string {
	return timeRange + ":" + strconv.Itoa(buckets)
}

func deviceHistoryCacheKey(timeRange string, buckets int) string {
	return timeRange + ":" + strconv.Itoa(buckets)
}

// Global cache instance
var statusCache *StatusCache

// InitStatusCache initializes the global status cache.
// Should be called once during server startup.
func InitStatusCache() {
	statusCache = NewStatusCache(
		30*time.Second,  // Status refresh every 30s
		60*time.Second,  // Link history refresh every 60s
		30*time.Second,  // Timeline refresh every 30s
		60*time.Second,  // Outages refresh every 60s
		120*time.Second, // Performance (latency comparison, metro path latency) refresh every 120s
	)
	statusCache.Start()
}

// StopStatusCache stops the global status cache.
// Should be called during server shutdown.
func StopStatusCache() {
	if statusCache != nil {
		statusCache.Stop()
	}
}

// IsStatusCacheReady returns true if the status cache is initialized and populated.
func IsStatusCacheReady() bool {
	return statusCache != nil && statusCache.IsReady()
}
