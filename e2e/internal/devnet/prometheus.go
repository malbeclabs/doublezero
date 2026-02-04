package devnet

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	dockercontainer "github.com/docker/docker/api/types/container"
	dockerfilters "github.com/docker/docker/api/types/filters"
	"github.com/docker/go-connections/nat"
	"github.com/malbeclabs/doublezero/e2e/internal/logging"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	prometheusImage        = "prom/prometheus:v2.54.1"
	prometheusInternalPort = 9090
)

type PrometheusSpec struct {
	Enabled        bool
	ContainerImage string
}

func (s *PrometheusSpec) Validate() error {
	if s.ContainerImage == "" {
		s.ContainerImage = prometheusImage
	}
	return nil
}

type Prometheus struct {
	dn  *Devnet
	log *slog.Logger

	ContainerID      string
	DefaultNetworkIP string
	InternalURL      string
	ExternalPort     int
}

func (p *Prometheus) dockerContainerName() string {
	return p.dn.Spec.DeployID + "-" + p.dockerContainerHostname()
}

func (p *Prometheus) dockerContainerHostname() string {
	return "prometheus"
}

func (p *Prometheus) Exists(ctx context.Context) (bool, error) {
	containers, err := p.dn.dockerClient.ContainerList(ctx, dockercontainer.ListOptions{
		All:     true,
		Filters: dockerfilters.NewArgs(dockerfilters.Arg("name", p.dockerContainerName())),
	})
	if err != nil {
		return false, fmt.Errorf("failed to list containers: %w", err)
	}
	for _, container := range containers {
		if container.Names[0] == "/"+p.dockerContainerName() {
			return true, nil
		}
	}
	return false, nil
}

func (p *Prometheus) StartIfNotRunning(ctx context.Context) (bool, error) {
	exists, err := p.Exists(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to check if prometheus exists: %w", err)
	}
	if exists {
		container, err := p.dn.dockerClient.ContainerInspect(ctx, p.dockerContainerName())
		if err != nil {
			return false, fmt.Errorf("failed to inspect container: %w", err)
		}

		if container.State.Running {
			p.log.Info("--> Prometheus already running", "container", shortContainerID(container.ID))

			err = p.setState(ctx, container.ID)
			if err != nil {
				return false, fmt.Errorf("failed to set prometheus state: %w", err)
			}

			return false, nil
		}

		err = p.dn.dockerClient.ContainerStart(ctx, container.ID, dockercontainer.StartOptions{})
		if err != nil {
			return false, fmt.Errorf("failed to start prometheus: %w", err)
		}

		err = p.setState(ctx, container.ID)
		if err != nil {
			return false, fmt.Errorf("failed to set prometheus state: %w", err)
		}

		return true, nil
	}

	return false, p.Start(ctx)
}

func (p *Prometheus) Start(ctx context.Context) error {
	p.log.Info("==> Starting prometheus", "image", p.dn.Spec.Prometheus.ContainerImage)

	req := testcontainers.ContainerRequest{
		Image: p.dn.Spec.Prometheus.ContainerImage,
		Name:  p.dockerContainerName(),
		ConfigModifier: func(cfg *dockercontainer.Config) {
			cfg.Hostname = p.dockerContainerHostname()
		},
		Cmd:          []string{"--config.file=/etc/prometheus/prometheus.yml", "--web.enable-remote-write-receiver"},
		ExposedPorts: []string{fmt.Sprintf("%d/tcp", prometheusInternalPort)},
		Networks:     []string{p.dn.DefaultNetwork.Name},
		NetworkAliases: map[string][]string{
			p.dn.DefaultNetwork.Name: {"prometheus"},
		},
		Resources: dockercontainer.Resources{
			NanoCPUs: defaultContainerNanoCPUs,
			Memory:   defaultContainerMemory,
		},
		Labels: p.dn.labels,
		WaitingFor: wait.ForHTTP("/-/ready").
			WithPort(nat.Port(fmt.Sprintf("%d/tcp", prometheusInternalPort))).
			WithStatusCodeMatcher(func(status int) bool {
				return status == 200
			}).
			WithStartupTimeout(60 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
		Logger:           logging.NewTestcontainersAdapter(p.log),
	})
	if err != nil {
		return fmt.Errorf("failed to start prometheus: %w", err)
	}

	err = p.setState(ctx, container.GetContainerID())
	if err != nil {
		return fmt.Errorf("failed to set prometheus state: %w", err)
	}

	p.log.Info("--> Prometheus started", "container", p.ContainerID, "url", p.InternalURL)
	return nil
}

func (p *Prometheus) setState(ctx context.Context, containerID string) error {
	container, err := p.dn.dockerClient.ContainerInspect(ctx, containerID)
	if err != nil {
		return fmt.Errorf("failed to inspect container: %w", err)
	}

	networkSettings := container.NetworkSettings.Networks[p.dn.DefaultNetwork.Name]
	if networkSettings == nil {
		return fmt.Errorf("prometheus not connected to default network")
	}

	port, err := p.dn.waitForContainerPortExposed(ctx, containerID, prometheusInternalPort, 10*time.Second)
	if err != nil {
		return fmt.Errorf("failed to wait for prometheus port to be exposed: %w", err)
	}

	p.ContainerID = shortContainerID(containerID)
	p.DefaultNetworkIP = networkSettings.IPAddress
	p.InternalURL = fmt.Sprintf("http://%s:%d", p.DefaultNetworkIP, prometheusInternalPort)
	p.ExternalPort = port

	return nil
}

func (p *Prometheus) InternalRemoteWriteURL() string {
	return fmt.Sprintf("http://%s:%d/api/v1/write", p.DefaultNetworkIP, prometheusInternalPort)
}

// HasDeviceMetrics checks if the device has metrics in Prometheus.
// It queries Prometheus for controller_grpc_getconfig_requests_total{pubkey="<devicePubkey>"}.
// TODO: this can be removed once rfc12 has been fully implemented
func (p *Prometheus) HasDeviceMetrics(ctx context.Context, devicePubkey string) (bool, error) {
	query := fmt.Sprintf(`controller_grpc_getconfig_requests_total{pubkey="%s"}`, devicePubkey)
	queryURL := fmt.Sprintf("http://%s:%d/api/v1/query?query=%s", p.dn.ExternalHost, p.ExternalPort, url.QueryEscape(query))

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, queryURL, nil)
	if err != nil {
		return false, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("failed to query prometheus: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("prometheus query failed with status: %d", resp.StatusCode)
	}

	var result struct {
		Status string `json:"status"`
		Data   struct {
			ResultType string `json:"resultType"`
			Result     []struct {
				Metric map[string]string `json:"metric"`
				Value  []any             `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false, fmt.Errorf("failed to decode prometheus response: %w", err)
	}

	if result.Status != "success" {
		return false, fmt.Errorf("prometheus query returned non-success status: %s", result.Status)
	}

	return len(result.Data.Result) > 0, nil
}
