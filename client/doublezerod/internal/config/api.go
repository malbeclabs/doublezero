package config

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
)

type ConfigResponse struct {
	Status string `json:"status"`
}

func NewUpdateHandler(log *slog.Logger, cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		err = cfg.UpdateFromJSON(body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		log.Info("configuration updated", "ledger_rpc_url", cfg.LedgerRPCURL, "serviceability_program_id", cfg.ServiceabilityProgramID)

		res := ConfigResponse{
			Status: "ok",
		}

		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(res); err != nil {
			http.Error(w, fmt.Sprintf("error generating response: %v", err), http.StatusInternalServerError)
		}
	}
}
