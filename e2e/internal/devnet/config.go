package devnet

import (
	"fmt"
	"log/slog"

	"github.com/docker/docker/client"
	"github.com/malbeclabs/doublezero/e2e/internal/docker"
)

type DevnetConfig struct {
	Logger          *slog.Logger
	DeployID        string
	WorkDir         string
	SubnetAllocator *docker.SubnetAllocator
	DockerClient    *client.Client

	ProgramKeypairPath string
	ManagerKeypairPath string
	AgentPubkey        string

	LedgerImage     string
	ControllerImage string
	ActivatorImage  string
	ManagerImage    string
	DeviceImage     string
	ClientImage     string
}

func (c *DevnetConfig) Validate() error {
	if c.Logger == nil {
		return fmt.Errorf("logger is required")
	}
	if c.DeployID == "" {
		return fmt.Errorf("deployID is required")
	}
	if c.WorkDir == "" {
		return fmt.Errorf("workDir is required")
	}
	if c.SubnetAllocator == nil {
		return fmt.Errorf("subnetAllocator is required")
	}
	if c.ProgramKeypairPath == "" {
		return fmt.Errorf("programKeypairPath is required")
	}
	if c.ManagerKeypairPath == "" {
		return fmt.Errorf("managerKeypairPath is required")
	}
	if c.DockerClient == nil {
		return fmt.Errorf("dockerClient is required")
	}

	if c.ProgramKeypairPath == "" {
		return fmt.Errorf("programKeypairPath is required")
	}
	if c.ManagerKeypairPath == "" {
		return fmt.Errorf("managerKeypairPath is required")
	}
	if c.AgentPubkey == "" {
		return fmt.Errorf("agentPubkey is required")
	}

	// All the docker images should be set.
	if c.LedgerImage == "" {
		return fmt.Errorf("ledgerImage is required")
	}
	if c.ControllerImage == "" {
		return fmt.Errorf("controllerImage is required")
	}
	if c.ActivatorImage == "" {
		return fmt.Errorf("activatorImage is required")
	}
	if c.ManagerImage == "" {
		return fmt.Errorf("managerImage is required")
	}
	if c.DeviceImage == "" {
		return fmt.Errorf("deviceImage is required")
	}
	if c.ClientImage == "" {
		return fmt.Errorf("clientImage is required")
	}

	return nil
}
