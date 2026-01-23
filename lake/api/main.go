package main

import (
	"context"
	"flag"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/joho/godotenv"
	"github.com/malbeclabs/doublezero/lake/api/config"
	"github.com/malbeclabs/doublezero/lake/api/handlers"
	"github.com/malbeclabs/doublezero/lake/api/metrics"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// Set by LDFLAGS
	version = "dev"
	commit  = "none"
	date    = "unknown"

	// shuttingDown is set to true when shutdown signal is received.
	// Readiness probe checks this to immediately return 503.
	shuttingDown atomic.Bool
)

const (
	defaultMetricsAddr = "0.0.0.0:0"
)

// spaHandler serves static files and falls back to index.html for SPA routing.
// If assetBucketURL is set, missing assets are fetched from the bucket and cached locally.
func spaHandler(staticDir, assetBucketURL string) http.HandlerFunc {
	fileServer := http.FileServer(http.Dir(staticDir))

	// Static asset extensions that should 404 if missing (not fallback to index.html)
	staticExtensions := map[string]bool{
		".js": true, ".mjs": true, ".css": true, ".map": true,
		".woff": true, ".woff2": true, ".ttf": true, ".eot": true,
		".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".svg": true, ".ico": true, ".webp": true,
		".json": true, ".wasm": true,
	}

	// setNoCacheHeaders prevents browsers from caching the response
	setNoCacheHeaders := func(w http.ResponseWriter) {
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, proxy-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
	}

	// setLongCacheHeaders allows browsers to cache content-hashed assets indefinitely
	setLongCacheHeaders := func(w http.ResponseWriter) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}

	// Asset cache directory for assets fetched from S3
	cacheDir := filepath.Join(os.TempDir(), "lake-asset-cache")
	if assetBucketURL != "" {
		if err := os.MkdirAll(cacheDir, 0755); err != nil {
			log.Printf("Warning: failed to create asset cache dir: %v", err)
		}
	}

	// Track in-flight fetches to avoid duplicate requests for the same asset
	var fetchMu sync.Mutex
	fetching := make(map[string]chan struct{})

	// fetchFromBucket fetches an asset from S3 and caches it locally.
	// Returns the local cache path on success, empty string on failure.
	fetchFromBucket := func(assetName string) string {
		if assetBucketURL == "" {
			return ""
		}

		cachePath := filepath.Join(cacheDir, assetName)

		// Check if already cached
		if _, err := os.Stat(cachePath); err == nil {
			return cachePath
		}

		// Coordinate concurrent fetches for the same asset
		fetchMu.Lock()
		if ch, ok := fetching[assetName]; ok {
			fetchMu.Unlock()
			<-ch // Wait for in-flight fetch
			if _, err := os.Stat(cachePath); err == nil {
				return cachePath
			}
			return ""
		}
		ch := make(chan struct{})
		fetching[assetName] = ch
		fetchMu.Unlock()

		defer func() {
			fetchMu.Lock()
			delete(fetching, assetName)
			close(ch)
			fetchMu.Unlock()
		}()

		// Fetch from S3
		url := strings.TrimSuffix(assetBucketURL, "/") + "/" + assetName
		resp, err := http.Get(url)
		if err != nil {
			log.Printf("Failed to fetch asset from bucket: %v", err)
			return ""
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return ""
		}

		// Write to cache
		if err := os.MkdirAll(filepath.Dir(cachePath), 0755); err != nil {
			log.Printf("Failed to create cache subdir: %v", err)
			return ""
		}

		f, err := os.Create(cachePath)
		if err != nil {
			log.Printf("Failed to create cache file: %v", err)
			return ""
		}
		defer f.Close()

		if _, err := io.Copy(f, resp.Body); err != nil {
			log.Printf("Failed to write cache file: %v", err)
			os.Remove(cachePath)
			return ""
		}

		log.Printf("Cached asset from bucket: %s", assetName)
		return cachePath
	}

	return func(w http.ResponseWriter, r *http.Request) {
		path := filepath.Join(staticDir, strings.TrimPrefix(r.URL.Path, "/"))

		// Check if file exists locally
		_, err := os.Stat(path)
		if os.IsNotExist(err) || err != nil {
			// Check if it's a directory (and serve index.html from it or fallback)
			if fi, statErr := os.Stat(path); statErr == nil && fi.IsDir() {
				indexPath := filepath.Join(path, "index.html")
				if _, indexErr := os.Stat(indexPath); indexErr == nil {
					setNoCacheHeaders(w)
					http.ServeFile(w, r, indexPath)
					return
				}
			}

			// For static assets, try fetching from S3 bucket if configured
			ext := strings.ToLower(filepath.Ext(r.URL.Path))
			if staticExtensions[ext] {
				// Extract asset name (e.g., "assets/index-abc123.js" from "/assets/index-abc123.js")
				assetName := strings.TrimPrefix(r.URL.Path, "/assets/")
				if cachePath := fetchFromBucket(assetName); cachePath != "" {
					// Serve from cache with appropriate content type and long cache headers
					setLongCacheHeaders(w)
					contentType := mime.TypeByExtension(ext)
					if contentType != "" {
						w.Header().Set("Content-Type", contentType)
					}
					http.ServeFile(w, r, cachePath)
					return
				}

				// Not in bucket either, return 404
				setNoCacheHeaders(w)
				http.NotFound(w, r)
				return
			}

			// Fallback to root index.html for SPA routing
			setNoCacheHeaders(w)
			http.ServeFile(w, r, filepath.Join(staticDir, "index.html"))
			return
		}

		// Direct request to index.html - never cache
		if strings.HasSuffix(r.URL.Path, "index.html") {
			setNoCacheHeaders(w)
		}

		fileServer.ServeHTTP(w, r)
	}
}

func main() {
	metricsAddrFlag := flag.String("metrics-addr", defaultMetricsAddr, "Address to listen on for prometheus metrics")
	flag.Parse()

	log.Printf("Starting lake-api version=%s commit=%s date=%s", version, commit, date)

	// Load .env file if it exists
	_ = godotenv.Load()

	// Load configuration
	if err := config.Load(); err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Load PostgreSQL
	if err := config.LoadPostgres(); err != nil {
		log.Fatalf("Failed to load PostgreSQL: %v", err)
	}
	defer config.ClosePostgres()
	defer config.Close() // Close ClickHouse connection

	// Load Neo4j (optional - log warning if unavailable)
	if err := config.LoadNeo4j(); err != nil {
		log.Printf("Warning: Neo4j not available: %v", err)
	} else {
		defer config.CloseNeo4j()
	}

	// Initialize status cache for fast page loads
	handlers.InitStatusCache()
	// Note: StopStatusCache() is called explicitly before server shutdown, not deferred

	// Start metrics server
	var metricsServer *http.Server
	if *metricsAddrFlag != "" {
		metrics.BuildInfo.WithLabelValues(version, commit, date).Set(1)
		listener, err := net.Listen("tcp", *metricsAddrFlag)
		if err != nil {
			log.Printf("Failed to start prometheus metrics server listener: %v", err)
		} else {
			log.Printf("Prometheus metrics server listening on %s", listener.Addr().String())
			mux := http.NewServeMux()
			mux.Handle("/metrics", promhttp.Handler())
			metricsServer = &http.Server{Handler: mux}
			go func() {
				if err := metricsServer.Serve(listener); err != nil && err != http.ErrServerClosed {
					log.Printf("Metrics server error: %v", err)
				}
			}()
		}
	}

	r := chi.NewRouter()

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(metrics.Middleware)

	// CORS configuration - origins from env or allow all
	corsOrigins := []string{"*"}
	if origins := os.Getenv("CORS_ORIGINS"); origins != "" {
		corsOrigins = strings.Split(origins, ",")
	}
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   corsOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type", "Authorization"},
		ExposedHeaders:   []string{"X-RateLimit-Limit", "X-RateLimit-Remaining", "X-RateLimit-Reset"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	// Security headers middleware
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Content Security Policy for Google Sign-In and app resources
			csp := strings.Join([]string{
				"default-src 'self'",
				"script-src 'self' 'unsafe-inline' https://accounts.google.com https://static.cloudflareinsights.com",
				"worker-src 'self' blob:",
				"frame-src https://accounts.google.com https://accounts.googleusercontent.com",
				"connect-src 'self' https://accounts.google.com https://cloudflareinsights.com https://*.basemaps.cartocdn.com",
				"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com https://accounts.google.com",
				"font-src 'self' https://fonts.gstatic.com",
				"img-src 'self' data: blob: https://lh3.googleusercontent.com https://*.basemaps.cartocdn.com",
			}, "; ")
			w.Header().Set("Content-Security-Policy", csp)

			// Additional security headers
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

			next.ServeHTTP(w, r)
		})
	})

	// Apply optional auth middleware globally to attach user context
	r.Use(handlers.OptionalAuth)

	// Health check endpoints
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	r.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		// Immediately fail if shutting down
		if shuttingDown.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("shutting down"))
			return
		}

		// Check database connectivity
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		if err := config.DB.Ping(ctx); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("database connection failed: " + err.Error()))
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	r.Get("/api/catalog", handlers.GetCatalog)
	r.Get("/api/stats", handlers.GetStats)
	r.Get("/api/status", handlers.GetStatus)
	r.Get("/api/status/link-history", handlers.GetLinkHistory)
	r.Get("/api/status/device-history", handlers.GetDeviceHistory)
	r.Get("/api/status/interface-issues", handlers.GetInterfaceIssues)
	r.Get("/api/timeline", handlers.GetTimeline)
	r.Get("/api/timeline/bounds", handlers.GetTimelineBounds)

	// Outage routes
	r.Get("/api/outages/links", handlers.GetLinkOutages)
	r.Get("/api/outages/links/csv", handlers.GetLinkOutagesCSV)

	// Search routes
	r.Get("/api/search", handlers.Search)
	r.Get("/api/search/autocomplete", handlers.SearchAutocomplete)

	// DZ entity routes
	r.Get("/api/dz/devices", handlers.GetDevices)
	r.Get("/api/dz/devices/{pk}", handlers.GetDevice)
	r.Get("/api/dz/links", handlers.GetLinks)
	r.Get("/api/dz/links/{pk}", handlers.GetLink)
	r.Get("/api/dz/links-health", handlers.GetLinkHealth)
	r.Get("/api/dz/metros", handlers.GetMetros)
	r.Get("/api/dz/metros/{pk}", handlers.GetMetro)
	r.Get("/api/dz/contributors", handlers.GetContributors)
	r.Get("/api/dz/contributors/{pk}", handlers.GetContributor)
	r.Get("/api/dz/users", handlers.GetUsers)
	r.Get("/api/dz/users/{pk}", handlers.GetUser)
	r.Get("/api/dz/field-values", handlers.GetFieldValues)

	// Solana entity routes
	r.Get("/api/solana/validators", handlers.GetValidators)
	r.Get("/api/solana/validators/{vote_pubkey}", handlers.GetValidator)
	r.Get("/api/solana/gossip-nodes", handlers.GetGossipNodes)
	r.Get("/api/solana/gossip-nodes/{pubkey}", handlers.GetGossipNode)

	// Stake analytics routes
	r.Get("/api/stake/overview", handlers.GetStakeOverview)
	r.Get("/api/stake/history", handlers.GetStakeHistory)
	r.Get("/api/stake/changes", handlers.GetStakeChanges)
	r.Get("/api/stake/validators", handlers.GetStakeValidators)

	r.Get("/api/topology", handlers.GetTopology)
	r.Get("/api/topology/traffic", handlers.GetTopologyTraffic)
	r.Get("/api/topology/link-latency", handlers.GetLinkLatencyHistory)
	r.Get("/api/topology/isis", handlers.GetISISTopology)
	r.Get("/api/topology/path", handlers.GetISISPath)
	r.Get("/api/topology/paths", handlers.GetISISPaths)
	r.Get("/api/topology/compare", handlers.GetTopologyCompare)
	r.Get("/api/topology/impact/{pk}", handlers.GetFailureImpact)
	r.Get("/api/topology/critical-links", handlers.GetCriticalLinks)
	r.Get("/api/topology/redundancy-report", handlers.GetRedundancyReport)
	r.Get("/api/topology/simulate-link-removal", handlers.GetSimulateLinkRemoval)
	r.Get("/api/topology/simulate-link-addition", handlers.GetSimulateLinkAddition)
	r.Get("/api/topology/metro-connectivity", handlers.GetMetroConnectivity)
	r.Get("/api/topology/latency-comparison", handlers.GetLatencyComparison)
	r.Get("/api/topology/latency-history/{origin}/{target}", handlers.GetLatencyHistory)
	r.Get("/api/topology/metro-path-latency", handlers.GetMetroPathLatency)
	r.Get("/api/topology/metro-path-detail", handlers.GetMetroPathDetail)
	r.Get("/api/topology/metro-paths", handlers.GetMetroPaths)
	r.Post("/api/topology/maintenance-impact", handlers.PostMaintenanceImpact)
	r.Post("/api/topology/whatif-removal", handlers.PostWhatIfRemoval)

	// SQL endpoints
	r.Post("/api/sql/query", handlers.ExecuteQuery)
	r.Post("/api/sql/generate", handlers.GenerateSQL)
	r.Post("/api/sql/generate/stream", handlers.GenerateSQLStream)

	// Cypher endpoints
	r.Post("/api/cypher/query", handlers.ExecuteCypher)
	r.Post("/api/cypher/generate", handlers.GenerateCypher)
	r.Post("/api/cypher/generate/stream", handlers.GenerateCypherStream)

	// Auto-detection endpoint
	r.Post("/api/auto/generate/stream", handlers.AutoGenerateStream)

	// Legacy SQL endpoints (backward compatibility)
	r.Post("/api/query", handlers.ExecuteQuery)
	r.Post("/api/generate", handlers.GenerateSQL)
	r.Post("/api/generate/stream", handlers.GenerateSQLStream)
	r.Post("/api/chat", handlers.Chat)
	r.Post("/api/chat/stream", handlers.ChatStream)
	r.Post("/api/complete", handlers.Complete)
	r.Post("/api/visualize/recommend", handlers.RecommendVisualization)
	r.Get("/api/version", handlers.GetVersion)

	// Session persistence routes
	r.Get("/api/sessions", handlers.ListSessions)
	r.Post("/api/sessions", handlers.CreateSession)
	r.Post("/api/sessions/batch", handlers.BatchGetSessions)
	r.Get("/api/sessions/{id}", handlers.GetSession)
	r.Put("/api/sessions/{id}", handlers.UpdateSession)
	r.Delete("/api/sessions/{id}", handlers.DeleteSession)

	// Session workflow route (get running workflow for a session)
	r.Get("/api/sessions/{id}/workflow", handlers.GetWorkflowForSession)

	// Workflow routes (for durable workflow persistence)
	r.Get("/api/workflows/{id}", handlers.GetWorkflow)
	r.Get("/api/workflows/{id}/stream", handlers.StreamWorkflow)

	// Auth routes
	r.Get("/api/auth/me", handlers.GetAuthMe)
	r.Post("/api/auth/logout", handlers.PostAuthLogout)
	r.Get("/api/auth/nonce", handlers.GetAuthNonce)
	r.Post("/api/auth/wallet", handlers.PostAuthWallet)
	r.Post("/api/auth/google", handlers.PostAuthGoogle)
	r.Get("/api/usage/quota", handlers.GetUsageQuota)

	// Serve static files from the web dist directory
	webDir := os.Getenv("WEB_DIST_DIR")
	if webDir == "" {
		webDir = "/doublezero/web/dist"
	}
	// Optional S3 bucket URL for fetching assets not in the local dist
	// (allows serving old assets after deploys while users still have old index.html cached)
	assetBucketURL := os.Getenv("ASSET_BUCKET_URL")
	if _, err := os.Stat(webDir); err == nil {
		log.Printf("Serving static files from %s", webDir)
		if assetBucketURL != "" {
			log.Printf("Asset bucket fallback enabled: %s", assetBucketURL)
		}
		r.Get("/*", spaHandler(webDir, assetBucketURL))
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 0, // Disabled for SSE streaming endpoints
		IdleTimeout:  60 * time.Second,
	}

	// Channel to listen for shutdown signals
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)

	// Create a cancellable context for all requests - this allows us to signal
	// SSE connections to close during shutdown (http.Server.Shutdown does NOT
	// cancel request contexts by default)
	serverCtx, serverCancel := context.WithCancel(context.Background())
	server.BaseContext = func(_ net.Listener) context.Context {
		return serverCtx
	}

	// Start server in a goroutine
	go func() {
		log.Printf("API server starting on :%s", port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// Start auto-resume of incomplete workflows in background
	go handlers.Manager.ResumeIncompleteWorkflows()

	// Start cleanup worker for expired sessions/nonces
	handlers.StartCleanupWorker(serverCtx)

	// Initialize usage metrics and start daily reset worker
	handlers.InitUsageMetrics(serverCtx)
	handlers.StartDailyResetWorker(serverCtx)

	// Wait for shutdown signal
	sig := <-shutdown
	log.Printf("Received signal %v, shutting down gracefully...", sig)

	// Immediately mark as shutting down so readiness probe returns 503
	shuttingDown.Store(true)

	// Cancel the server context to signal SSE connections to close
	// This triggers ctx.Done() in all active request handlers
	serverCancel()

	// Stop background cache goroutines (they may be blocking on DB queries)
	handlers.StopStatusCache()

	// Give existing connections a short time to complete after context cancellation
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Printf("Graceful shutdown error: %v", err)
	} else {
		log.Println("Server stopped gracefully")
	}

	// Shutdown metrics server
	if metricsServer != nil {
		if err := metricsServer.Shutdown(ctx); err != nil {
			log.Printf("Metrics server shutdown error: %v", err)
		} else {
			log.Println("Metrics server stopped gracefully")
		}
	}
}

