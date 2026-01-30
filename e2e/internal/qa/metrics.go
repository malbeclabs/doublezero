package qa

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/InfluxCommunity/influxdb3-go/v2/influxdb3"
)

type MetricsConfig struct {
	Host     string
	Token    string
	Database string
}

type GrafanaConfig struct {
	PrometheusURL string
	Username      string
	APIKey        string
}

type DeviceTestResult struct {
	DeviceCode   string
	DevicePubkey string
	Success      bool
}

func MetricsConfigFromEnv() *MetricsConfig {
	host := os.Getenv("INFLUXDB_HOST")
	token := os.Getenv("INFLUXDB_TOKEN")
	database := os.Getenv("INFLUXDB_DATABASE")

	if host == "" || token == "" {
		return nil
	}

	if !strings.HasPrefix(host, "https://") && !strings.HasPrefix(host, "http://") {
		host = "https://" + host
	}

	if database == "" {
		database = "qa-metrics"
	}

	return &MetricsConfig{
		Host:     host,
		Token:    token,
		Database: database,
	}
}

func GrafanaConfigFromEnv() *GrafanaConfig {
	prometheusURL := os.Getenv("GRAFANA_PROMETHEUS_URL")
	user := os.Getenv("GRAFANA_PROMETHEUS_USER")
	apiKey := os.Getenv("GRAFANA_API_KEY")

	if prometheusURL == "" || apiKey == "" {
		return nil
	}

	return &GrafanaConfig{
		PrometheusURL: strings.TrimSuffix(prometheusURL, "/"),
		Username:      user,
		APIKey:        apiKey,
	}
}

func PublishMetrics(ctx context.Context, log *slog.Logger, cfg *MetricsConfig, env string, results []DeviceTestResult, duration time.Duration) error {
	if cfg == nil {
		log.Info("Metrics publishing skipped: no InfluxDB configuration")
		return nil
	}

	client, err := influxdb3.New(influxdb3.ClientConfig{
		Host:     cfg.Host,
		Token:    cfg.Token,
		Database: cfg.Database,
	})
	if err != nil {
		return fmt.Errorf("failed to create InfluxDB client: %w", err)
	}
	defer client.Close()

	now := time.Now()
	var successCount, failureCount int
	var points []*influxdb3.Point

	for _, result := range results {
		p := influxdb3.NewPoint(
			"device_qa_test_results",
			map[string]string{
				"device_code":   result.DeviceCode,
				"device_pubkey": result.DevicePubkey,
				"env":           env,
			},
			map[string]any{
				"success": result.Success,
			},
			now,
		)
		points = append(points, p)

		if result.Success {
			successCount++
		} else {
			failureCount++
		}
	}

	summary := influxdb3.NewPoint(
		"device_qa_test_metadata",
		map[string]string{
			"env": env,
		},
		map[string]any{
			"devices_tested":  len(results),
			"devices_success": successCount,
			"devices_failed":  failureCount,
			"duration_s":      duration.Seconds(),
		},
		now,
	)
	points = append(points, summary)

	if err := client.WritePoints(ctx, points); err != nil {
		return fmt.Errorf("failed to write points: %w", err)
	}

	log.Info("Published metrics to InfluxDB",
		"devices", len(results),
		"success", successCount,
		"failed", failureCount,
	)

	return nil
}

func GetActiveDeviceCodes(ctx context.Context, cfg *GrafanaConfig) (map[string]bool, error) {
	if cfg == nil {
		return nil, fmt.Errorf("grafana config is nil")
	}

	// Query for all devices with GetConfig activity in the last 5m
	query := `sum by (device_code) (increase(controller_grpc_getconfig_requests_total[5m])) > 0`

	// The PrometheusURL already includes /api/prom, so we just append /api/v1/query
	queryURL := fmt.Sprintf("%s/api/v1/query?query=%s", cfg.PrometheusURL, url.QueryEscape(query))

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, queryURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	// Grafana Cloud Prometheus uses Basic Auth with instance ID and API key
	req.SetBasicAuth(cfg.Username, cfg.APIKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to query grafana: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("grafana query failed with status: %d", resp.StatusCode)
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
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if result.Status != "success" {
		return nil, fmt.Errorf("query returned non-success status: %s", result.Status)
	}

	active := make(map[string]bool)
	for _, r := range result.Data.Result {
		if deviceCode, ok := r.Metric["device_code"]; ok && deviceCode != "" {
			active[deviceCode] = true
		}
	}

	return active, nil
}
