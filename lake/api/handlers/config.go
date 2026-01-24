package handlers

import (
	"encoding/json"
	"net/http"
	"os"
)

// PublicConfig holds configuration that is safe to expose to the frontend
type PublicConfig struct {
	GoogleClientID string `json:"googleClientId,omitempty"`
}

// GetConfig returns public configuration for the frontend
func GetConfig(w http.ResponseWriter, r *http.Request) {
	config := PublicConfig{
		GoogleClientID: os.Getenv("GOOGLE_CLIENT_ID"),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(config)
}
