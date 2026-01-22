package devnet

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	dockercontainer "github.com/docker/docker/api/types/container"
	dockerfilters "github.com/docker/docker/api/types/filters"
	"github.com/malbeclabs/doublezero/e2e/internal/logging"
	"github.com/testcontainers/testcontainers-go"
)

type DeviceHealthOracleSpec struct {
	ContainerImage string
	Interval       time.Duration
}

func (s *DeviceHealthOracleSpec) Validate() error {
	if s.ContainerImage == "" {
		s.ContainerImage = os.Getenv("DZ_DEVICE_HEALTH_ORACLE_IMAGE")
	}

	if s.Interval == 0 {
		s.Interval = 10 * time.Second
	}

	return nil
}

type DeviceHealthOracle struct {
	dn  *Devnet
	log *slog.Logger

	ContainerID string
}

func (d *DeviceHealthOracle) dockerContainerName() string {
	return d.dn.Spec.DeployID + "-" + d.dockerContainerHostname()
}

func (d *DeviceHealthOracle) dockerContainerHostname() string {
	return "device-health-oracle"
}

func (d *DeviceHealthOracle) Exists(ctx context.Context) (bool, error) {
	containers, err := d.dn.dockerClient.ContainerList(ctx, dockercontainer.ListOptions{
		All:     true,
		Filters: dockerfilters.NewArgs(dockerfilters.Arg("name", d.dockerContainerName())),
	})
	if err != nil {
		return false, fmt.Errorf("failed to list containers: %w", err)
	}
	for _, container := range containers {
		if container.Names[0] == "/"+d.dockerContainerName() {
			return true, nil
		}
	}
	return false, nil
}

func (d *DeviceHealthOracle) StartIfNotRunning(ctx context.Context) (bool, error) {
	exists, err := d.Exists(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to check if device-health-oracle exists: %w", err)
	}
	if exists {
		container, err := d.dn.dockerClient.ContainerInspect(ctx, d.dockerContainerName())
		if err != nil {
			return false, fmt.Errorf("failed to inspect container: %w", err)
		}

		if container.State.Running {
			d.log.Info("--> DeviceHealthOracle already running", "container", shortContainerID(container.ID))

			err = d.setState(container.ID)
			if err != nil {
				return false, fmt.Errorf("failed to set device-health-oracle state: %w", err)
			}

			return false, nil
		}

		err = d.dn.dockerClient.ContainerStart(ctx, container.ID, dockercontainer.StartOptions{})
		if err != nil {
			return false, fmt.Errorf("failed to start device-health-oracle: %w", err)
		}

		err = d.setState(container.ID)
		if err != nil {
			return false, fmt.Errorf("failed to set device-health-oracle state: %w", err)
		}

		return true, nil
	}

	return false, d.Start(ctx)
}

func (d *DeviceHealthOracle) Start(ctx context.Context) error {
	d.log.Info("==> Starting device-health-oracle", "image", d.dn.Spec.DeviceHealthOracle.ContainerImage)

	env := map[string]string{
		"DZ_LEDGER_URL":                d.dn.Ledger.InternalRPCURL,
		"DZ_SERVICEABILITY_PROGRAM_ID": d.dn.Manager.ServiceabilityProgramID,
		"DZ_TELEMETRY_PROGRAM_ID":      d.dn.Manager.TelemetryProgramID,
		"DZ_INTERVAL":                  d.dn.Spec.DeviceHealthOracle.Interval.String(),
	}
	if d.dn.Prometheus != nil && d.dn.Prometheus.InternalURL != "" {
		env["ALLOY_PROMETHEUS_URL"] = d.dn.Prometheus.RemoteWriteURL()
	}

	req := testcontainers.ContainerRequest{
		Image: d.dn.Spec.DeviceHealthOracle.ContainerImage,
		Name:  d.dockerContainerName(),
		ConfigModifier: func(cfg *dockercontainer.Config) {
			cfg.Hostname = d.dockerContainerHostname()
		},
		Env:      env,
		Networks: []string{d.dn.DefaultNetwork.Name},
		NetworkAliases: map[string][]string{
			d.dn.DefaultNetwork.Name: {"device-health-oracle"},
		},
		Resources: dockercontainer.Resources{
			NanoCPUs: defaultContainerNanoCPUs,
			Memory:   defaultContainerMemory,
		},
		Labels: d.dn.labels,
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
		Logger:           logging.NewTestcontainersAdapter(d.log),
	})
	if err != nil {
		return fmt.Errorf("failed to start device-health-oracle: %w", err)
	}

	err = d.setState(container.GetContainerID())
	if err != nil {
		return fmt.Errorf("failed to set device-health-oracle state: %w", err)
	}

	d.log.Info("--> DeviceHealthOracle started", "container", d.ContainerID)
	return nil
}

func (d *DeviceHealthOracle) setState(containerID string) error {
	d.ContainerID = shortContainerID(containerID)
	return nil
}
