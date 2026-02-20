package dz

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
)

type GetStatusFunc func(ctx context.Context) (*Status, error)

type Status struct {
	CurrentDeviceCode string
	MetroName         string
	NetworkSlug       string
}

func GetStatus(ctx context.Context) (*Status, error) {
	cmd := exec.CommandContext(ctx, "doublezero", "status", "--json")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to execute doublezero status command: %w", err)
	}
	return parseStatus(output)
}

func parseStatus(data []byte) (*Status, error) {
	var res []statusResponse
	if err := json.Unmarshal(data, &res); err != nil {
		return nil, fmt.Errorf("failed to unmarshal status response: %w", err)
	}
	if len(res) == 0 {
		return nil, fmt.Errorf("no status response")
	}
	// Use the first non-multicast entry as the primary status, falling back
	// to the first entry if all entries are multicast.
	primary := res[0]
	for _, r := range res {
		if r.Response.UserType != "Multicast" {
			primary = r
			break
		}
	}
	return &Status{
		CurrentDeviceCode: primary.CurrentDevice,
		MetroName:         primary.Metro,
		NetworkSlug:       primary.Network,
	}, nil
}

type statusResponse struct {
	Response struct {
		TunnelName       string `json:"tunnel_name"`
		TunnelSrc        string `json:"tunnel_src"`
		TunnelDst        string `json:"tunnel_dst"`
		DoubleZeroIP     string `json:"doublezero_ip"`
		UserType         string `json:"user_type"`
		DoubleZeroStatus struct {
			SessionStatus     string `json:"session_status"`
			LastSessionUpdate int64  `json:"last_session_update"`
		} `json:"doublezero_status"`
	} `json:"response"`
	CurrentDevice       string `json:"current_device"`
	LowestLatencyDevice string `json:"lowest_latency_device"`
	Metro               string `json:"metro"`
	Network             string `json:"network"`
}
