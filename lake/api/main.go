package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/joho/godotenv"
	"github.com/malbeclabs/doublezero/lake/api/handlers"
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
	// Load .env file if it exists
	_ = godotenv.Load()
	r := chi.NewRouter()

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"http://localhost:5173"},
		AllowedMethods:   []string{"GET", "POST", "OPTIONS"},
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
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	r.Get("/api/catalog", handlers.GetCatalog)
	r.Post("/api/query", handlers.ExecuteQuery)
	r.Post("/api/generate", handlers.GenerateSQL)
	r.Post("/api/generate/stream", handlers.GenerateSQLStream)
	r.Post("/api/chat", handlers.Chat)
	r.Post("/api/chat/stream", handlers.ChatStream)
	r.Post("/api/complete", handlers.Complete)
	r.Post("/api/visualize/recommend", handlers.RecommendVisualization)

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
		Addr:    ":8080",
		Handler: r,
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

	// Give existing connections 30 seconds to complete
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Printf("Graceful shutdown error: %v", err)
	} else {
		log.Println("Server stopped gracefully")
	}
}
