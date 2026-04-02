package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
)

type ValidatorMetadataItem struct {
	IP              string `json:"ip"`
	ActiveStake     int64  `json:"active_stake"`
	VoteAccount     string `json:"vote_account"`
	SoftwareClient  string `json:"software_client"`
	SoftwareVersion string `json:"software_version"`
}

type server struct {
	mu         sync.RWMutex
	validators []ValidatorMetadataItem
}

func main() {
	s := &server{}

	// Load initial response from config file if provided.
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "/etc/mock/validators.json"
	}
	if data, err := os.ReadFile(configPath); err == nil {
		if err := json.Unmarshal(data, &s.validators); err != nil {
			log.Fatalf("Failed to parse config file %s: %v", configPath, err)
		}
		log.Printf("Loaded %d validators from %s", len(s.validators), configPath)
	} else {
		log.Printf("No config file at %s, starting with empty response", configPath)
	}

	http.HandleFunc("/api/v1/validators-metadata", s.handleValidatorsMetadata)
	http.HandleFunc("/solana-rpc", s.handleSolanaRPC)
	http.HandleFunc("/config", s.handleConfig)
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})

	addr := ":8080"
	log.Printf("Validator metadata service mock listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}

func (s *server) handleValidatorsMetadata(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Log for debugging.
	log.Printf("GET /api/v1/validators-metadata")

	s.mu.RLock()
	defer s.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.validators)
}

// handleSolanaRPC handles Solana JSON-RPC requests (getVoteAccounts, getClusterNodes).
// It synthesizes responses from the configured validator metadata items.
func (s *server) handleSolanaRPC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var req struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id"`
		Method  string `json:"method"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	log.Printf("POST /solana-rpc method=%s", req.Method)

	s.mu.RLock()
	defer s.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")

	switch req.Method {
	case "getVoteAccounts":
		s.handleGetVoteAccounts(w, req.ID)
	case "getClusterNodes":
		s.handleGetClusterNodes(w, req.ID)
	default:
		resp := map[string]any{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"error":   map[string]any{"code": -32601, "message": "Method not found"},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}
}

func (s *server) handleGetVoteAccounts(w http.ResponseWriter, id int) {
	type VoteAccount struct {
		VotePubkey       string      `json:"votePubkey"`
		NodePubkey       string      `json:"nodePubkey"`
		ActivatedStake   int64       `json:"activatedStake"`
		Commission       int         `json:"commission"`
		EpochVoteAccount bool        `json:"epochVoteAccount"`
		LastVote         int         `json:"lastVote"`
		RootSlot         int         `json:"rootSlot"`
		EpochCredits     [][]int     `json:"epochCredits"`
	}

	var current []VoteAccount
	for _, v := range s.validators {
		nodePubkey := fmt.Sprintf("node-%s", v.IP)
		current = append(current, VoteAccount{
			VotePubkey:       v.VoteAccount,
			NodePubkey:       nodePubkey,
			ActivatedStake:   v.ActiveStake,
			Commission:       0,
			EpochVoteAccount: true,
			LastVote:         100,
			RootSlot:         99,
			EpochCredits:     [][]int{{1, 64, 0}},
		})
	}
	if current == nil {
		current = []VoteAccount{}
	}

	resp := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result": map[string]any{
			"current":    current,
			"delinquent": []VoteAccount{},
		},
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *server) handleGetClusterNodes(w http.ResponseWriter, id int) {
	type ClusterNode struct {
		Pubkey       string  `json:"pubkey"`
		Gossip       *string `json:"gossip"`
		TPU          *string `json:"tpu"`
		RPC          *string `json:"rpc"`
		Version      *string `json:"version"`
		FeatureSet   *int    `json:"featureSet"`
		ShredVersion *int    `json:"shredVersion"`
	}

	var nodes []ClusterNode
	for _, v := range s.validators {
		nodePubkey := fmt.Sprintf("node-%s", v.IP)
		gossip := fmt.Sprintf("%s:8001", v.IP)
		nodes = append(nodes, ClusterNode{
			Pubkey: nodePubkey,
			Gossip: &gossip,
		})
	}
	if nodes == nil {
		nodes = []ClusterNode{}
	}

	resp := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  nodes,
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *server) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var validators []ValidatorMetadataItem
	if err := json.Unmarshal(body, &validators); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	s.validators = validators
	s.mu.Unlock()

	log.Printf("Config updated: %d validators", len(validators))
	w.WriteHeader(http.StatusOK)
}
