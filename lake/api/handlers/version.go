package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
)

// VersionResponse contains the frontend build version info.
type VersionResponse struct {
	BuildTimestamp string `json:"buildTimestamp"`
}

// GetVersion returns the current deployed frontend version.
func GetVersion(w http.ResponseWriter, r *http.Request) {
	webDir := os.Getenv("WEB_DIST_DIR")
	if webDir == "" {
		webDir = "/doublezero/web/dist"
	}

	versionFile := filepath.Join(webDir, "version.json")
	data, err := os.ReadFile(versionFile)
	if err != nil {
		// In development, version.json might not exist
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(VersionResponse{BuildTimestamp: "dev"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}
