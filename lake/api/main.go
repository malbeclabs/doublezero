package main

import (
	"context"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
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

// spaHandler serves static files and falls back to index.html for SPA routing
func spaHandler(staticDir string) http.HandlerFunc {
	fileServer := http.FileServer(http.Dir(staticDir))
	return func(w http.ResponseWriter, r *http.Request) {
		path := filepath.Join(staticDir, strings.TrimPrefix(r.URL.Path, "/"))

		// Check if file exists
		_, err := os.Stat(path)
		if os.IsNotExist(err) || err != nil {
			// Check if it's a directory (and serve index.html from it or fallback)
			if fi, statErr := os.Stat(path); statErr == nil && fi.IsDir() {
				indexPath := filepath.Join(path, "index.html")
				if _, indexErr := os.Stat(indexPath); indexErr == nil {
					http.ServeFile(w, r, indexPath)
					return
				}
			}
			// Fallback to root index.html for SPA routing
			http.ServeFile(w, r, filepath.Join(staticDir, "index.html"))
			return
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
		AllowedHeaders:   []string{"Content-Type"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

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
	r.Get("/api/timeline", handlers.GetTimeline)
	r.Get("/api/timeline/bounds", handlers.GetTimelineBounds)

	// Search routes
	r.Get("/api/search", handlers.Search)
	r.Get("/api/search/autocomplete", handlers.SearchAutocomplete)

	// DZ entity routes
	r.Get("/api/dz/devices", handlers.GetDevices)
	r.Get("/api/dz/devices/{pk}", handlers.GetDevice)
	r.Get("/api/dz/links", handlers.GetLinks)
	r.Get("/api/dz/links/{pk}", handlers.GetLink)
	r.Get("/api/dz/metros", handlers.GetMetros)
	r.Get("/api/dz/metros/{pk}", handlers.GetMetro)
	r.Get("/api/dz/contributors", handlers.GetContributors)
	r.Get("/api/dz/contributors/{pk}", handlers.GetContributor)
	r.Get("/api/dz/users", handlers.GetUsers)
	r.Get("/api/dz/users/{pk}", handlers.GetUser)

	// Solana entity routes
	r.Get("/api/solana/validators", handlers.GetValidators)
	r.Get("/api/solana/validators/{vote_pubkey}", handlers.GetValidator)
	r.Get("/api/solana/gossip-nodes", handlers.GetGossipNodes)
	r.Get("/api/solana/gossip-nodes/{pubkey}", handlers.GetGossipNode)

	r.Get("/api/topology", handlers.GetTopology)
	r.Get("/api/topology/traffic", handlers.GetTopologyTraffic)
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
	r.Get("/api/sessions/{id}", handlers.GetSession)
	r.Put("/api/sessions/{id}", handlers.UpdateSession)
	r.Delete("/api/sessions/{id}", handlers.DeleteSession)

	// Session lock routes (for cross-browser coordination)
	r.Get("/api/sessions/{id}/lock", handlers.GetSessionLock)
	r.Post("/api/sessions/{id}/lock", handlers.AcquireSessionLock)
	r.Delete("/api/sessions/{id}/lock", handlers.ReleaseSessionLock)
	r.Get("/api/sessions/{id}/lock/watch", handlers.WatchSessionLock)

	// Serve static files from the web dist directory
	webDir := os.Getenv("WEB_DIST_DIR")
	if webDir == "" {
		webDir = "/doublezero/web/dist"
	}
	if _, err := os.Stat(webDir); err == nil {
		log.Printf("Serving static files from %s", webDir)
		r.Get("/*", spaHandler(webDir))
	}

	server := &http.Server{
		Addr:         ":8080",
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second, // Longer for streaming endpoints
		IdleTimeout:  60 * time.Second,
	}

	// Channel to listen for shutdown signals
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)

	// Start server in a goroutine
	go func() {
		log.Println("API server starting on :8080")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// Wait for shutdown signal
	sig := <-shutdown
	log.Printf("Received signal %v, shutting down gracefully...", sig)

	// Immediately mark as shutting down so readiness probe returns 503
	shuttingDown.Store(true)

	// Give existing connections 30 seconds to complete
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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
