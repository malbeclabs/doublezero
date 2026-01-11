package main

import (
	"log"
	"net/http"

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
	r.Post("/api/visualize/recommend", handlers.RecommendVisualization)

	log.Println("API server starting on :8080")
	if err := http.ListenAndServe(":8080", r); err != nil {
		log.Fatal(err)
	}
}
