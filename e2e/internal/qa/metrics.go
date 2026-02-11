package qa

import (
	"context"
	"fmt"
	"log/slog"
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

func PublishMetrics(ctx context.Context, log *slog.Logger, cfg *MetricsConfig, env string, results []DeviceTestResult, duration time.Duration) error {
	if cfg == nil {
		log.Debug("Metrics publishing skipped: no InfluxDB configuration")
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

	log.Debug("Published metrics to InfluxDB",
		"devices", len(results),
		"success", successCount,
		"failed", failureCount,
	)

	return nil
}
