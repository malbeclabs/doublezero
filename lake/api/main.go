package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/joho/godotenv"
	"github.com/malbeclabs/doublezero/lake/api/handlers"
)

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

	r.Get("/api/catalog", handlers.GetCatalog)
	r.Post("/api/query", handlers.ExecuteQuery)
	r.Post("/api/generate", handlers.GenerateSQL)
	r.Post("/api/generate/stream", handlers.GenerateSQLStream)
	r.Post("/api/chat", handlers.Chat)
	r.Post("/api/chat/stream", handlers.ChatStream)
	r.Post("/api/complete", handlers.Complete)
	r.Post("/api/visualize/recommend", handlers.RecommendVisualization)

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
