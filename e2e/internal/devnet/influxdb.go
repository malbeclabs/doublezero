package devnet

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	dockercontainer "github.com/docker/docker/api/types/container"
	dockerfilters "github.com/docker/docker/api/types/filters"
	"github.com/docker/go-connections/nat"
	"github.com/malbeclabs/doublezero/e2e/internal/docker"
	"github.com/malbeclabs/doublezero/e2e/internal/logging"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	influxDBImage        = "public.ecr.aws/influxdb/influxdb:1.8"
	influxDBInternalPort = 8086
	influxDBDatabase     = "doublezero_devnet"
)

type InfluxDBSpec struct {
	Enabled        bool
	ContainerImage string
}

func (s *InfluxDBSpec) Validate() error {
	if s.ContainerImage == "" {
		s.ContainerImage = influxDBImage
	}
	return nil
}

type InfluxDB struct {
	dn  *Devnet
	log *slog.Logger

	ContainerID      string
	DefaultNetworkIP string
	InternalURL      string
}

func (i *InfluxDB) dockerContainerName() string {
	return i.dn.Spec.DeployID + "-" + i.dockerContainerHostname()
}

func (i *InfluxDB) dockerContainerHostname() string {
	return "influxdb"
}

func (i *InfluxDB) Exists(ctx context.Context) (bool, error) {
	containers, err := i.dn.dockerClient.ContainerList(ctx, dockercontainer.ListOptions{
		All:     true,
		Filters: dockerfilters.NewArgs(dockerfilters.Arg("name", i.dockerContainerName())),
	})
	if err != nil {
		return false, fmt.Errorf("failed to list containers: %w", err)
	}
	for _, container := range containers {
		if container.Names[0] == "/"+i.dockerContainerName() {
			return true, nil
		}
	}
	return false, nil
}

func (i *InfluxDB) StartIfNotRunning(ctx context.Context) (bool, error) {
	exists, err := i.Exists(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to check if influxdb exists: %w", err)
	}
	if exists {
		container, err := i.dn.dockerClient.ContainerInspect(ctx, i.dockerContainerName())
		if err != nil {
			return false, fmt.Errorf("failed to inspect container: %w", err)
		}

		if container.State.Running {
			i.log.Debug("--> InfluxDB already running", "container", shortContainerID(container.ID))

			err = i.setState(ctx, container.ID)
			if err != nil {
				return false, fmt.Errorf("failed to set influxdb state: %w", err)
			}

			return false, nil
		}

		err = i.dn.dockerClient.ContainerStart(ctx, container.ID, dockercontainer.StartOptions{})
		if err != nil {
			return false, fmt.Errorf("failed to start influxdb: %w", err)
		}

		err = i.setState(ctx, container.ID)
		if err != nil {
			return false, fmt.Errorf("failed to set influxdb state: %w", err)
		}

		return true, nil
	}

	return false, i.Start(ctx)
}

func (i *InfluxDB) Start(ctx context.Context) error {
	i.log.Debug("==> Starting influxdb", "image", i.dn.Spec.InfluxDB.ContainerImage)

	req := testcontainers.ContainerRequest{
		Image: i.dn.Spec.InfluxDB.ContainerImage,
		Name:  i.dockerContainerName(),
		ConfigModifier: func(cfg *dockercontainer.Config) {
			cfg.Hostname = i.dockerContainerHostname()
		},
		Env: map[string]string{
			"INFLUXDB_HTTP_AUTH_ENABLED": "false",
			"INFLUXDB_DB":                influxDBDatabase,
		},
		ExposedPorts: []string{fmt.Sprintf("%d/tcp", influxDBInternalPort)},
		Networks:     []string{i.dn.DefaultNetwork.Name},
		NetworkAliases: map[string][]string{
			i.dn.DefaultNetwork.Name: {"influxdb"},
		},
		Resources: dockercontainer.Resources{
			NanoCPUs: defaultContainerNanoCPUs,
			Memory:   defaultContainerMemory,
		},
		Labels: i.dn.labels,
		WaitingFor: wait.ForHTTP("/ping").
			WithPort(nat.Port(fmt.Sprintf("%d/tcp", influxDBInternalPort))).
			WithStatusCodeMatcher(func(status int) bool {
				return status == 204
			}).
			WithStartupTimeout(60 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
		Logger:           logging.NewTestcontainersAdapter(i.log),
	})
	if err != nil {
		return fmt.Errorf("failed to start influxdb: %w", err)
	}

	err = i.setState(ctx, container.GetContainerID())
	if err != nil {
		return fmt.Errorf("failed to set influxdb state: %w", err)
	}

	i.log.Debug("--> InfluxDB started", "container", i.ContainerID, "url", i.InternalURL)
	return nil
}

func (i *InfluxDB) setState(ctx context.Context, containerID string) error {
	container, err := i.dn.dockerClient.ContainerInspect(ctx, containerID)
	if err != nil {
		return fmt.Errorf("failed to inspect container: %w", err)
	}

	networkSettings := container.NetworkSettings.Networks[i.dn.DefaultNetwork.Name]
	if networkSettings == nil {
		return fmt.Errorf("influxdb not connected to default network")
	}

	i.ContainerID = shortContainerID(containerID)
	i.DefaultNetworkIP = networkSettings.IPAddress
	i.InternalURL = fmt.Sprintf("http://%s:%d", i.DefaultNetworkIP, influxDBInternalPort)

	return nil
}

func (i *InfluxDB) Database() string {
	return influxDBDatabase
}

// HasDeviceData checks if the device has published telemetry data to InfluxDB.
// It queries the intfCounters table for entries with the device's pubkey.
func (i *InfluxDB) HasDeviceData(ctx context.Context, devicePubkey string) (bool, error) {
	query := fmt.Sprintf(`SELECT COUNT("in-pkts") FROM intfCounters WHERE dzd_pubkey = '%s'`, devicePubkey)
	output, err := i.execInfluxQuery(ctx, query)
	if err != nil {
		return false, err
	}
	// InfluxDB returns output only when there's data; no data = empty output
	return strings.TrimSpace(output) != "", nil
}

func (i *InfluxDB) execInfluxQuery(ctx context.Context, query string) (string, error) {
	output, err := docker.Exec(ctx, i.dn.dockerClient, i.dockerContainerName(), []string{
		"influx", "-database", influxDBDatabase, "-execute", query,
	})
	if err != nil {
		return "", fmt.Errorf("failed to execute influx query: %w", err)
	}
	return string(output), nil
}
