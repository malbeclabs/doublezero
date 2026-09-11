package collector

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
)

type JSONNode struct {
	Code          string  `json:"code"`
	Cloud         string  `json:"cloud"`
	Latitude      float64 `json:"lat"`
	Longitude     float64 `json:"lng"`
	AtlasProbeIDs []int   `json:"atlas_probe_ids"`
	PingTarget    string  `json:"ping_target"` // Address other nodes ping to reach this node
}

func LoadNodesFromJSON(logger *slog.Logger, filename string) ([]JSONNode, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open JSON file: %w", err)
	}
	defer file.Close()

	var nodes []JSONNode
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&nodes); err != nil {
		return nil, fmt.Errorf("invalid JSON format: %w", err)
	}

	if err := validateNodes(nodes); err != nil {
		return nil, err
	}

	logger.Info("Loaded nodes from JSON file",
		slog.Int("node_count", len(nodes)),
		slog.String("filename", filename))

	return nodes, nil
}

func validateNodes(nodes []JSONNode) error {
	if len(nodes) == 0 {
		return ErrInvalidNodeFile.WithContext("reason", "node file contains empty array")
	}

	seen := make(map[string]bool, len(nodes))
	for i, node := range nodes {
		if node.Code == "" {
			return ErrInvalidNodeFile.WithContext("index", i).
				WithContext("reason", "missing code")
		}
		if node.Cloud == "" {
			return ErrInvalidNodeFile.WithContext("index", i).
				WithContext("code", node.Code).
				WithContext("reason", "missing cloud")
		}
		if node.Latitude == 0 && node.Longitude == 0 {
			return ErrInvalidNodeFile.WithContext("index", i).
				WithContext("code", node.Code).
				WithContext("reason", "invalid coordinates")
		}
		if len(node.AtlasProbeIDs) == 0 {
			return ErrInvalidNodeFile.WithContext("index", i).
				WithContext("code", node.Code).
				WithContext("reason", "no atlas probe ids")
		}
		ip := net.ParseIP(node.PingTarget)
		if ip == nil || ip.To4() == nil {
			return ErrInvalidNodeFile.WithContext("index", i).
				WithContext("code", node.Code).
				WithContext("ping_target", node.PingTarget).
				WithContext("reason", "ping target is not an IPv4 address")
		}
		if !IsInternetRoutable(node.PingTarget) {
			return ErrInvalidNodeFile.WithContext("index", i).
				WithContext("code", node.Code).
				WithContext("ping_target", node.PingTarget).
				WithContext("reason", "ping target is not a routable address")
		}
		if seen[node.Code] {
			return ErrInvalidNodeFile.WithContext("index", i).
				WithContext("code", node.Code).
				WithContext("reason", "duplicate code")
		}
		seen[node.Code] = true
	}

	return nil
}
