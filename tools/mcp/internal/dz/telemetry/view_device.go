package dztelem

import (
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
	dzsvc "github.com/malbeclabs/doublezero/tools/mcp/internal/dz/serviceability"
)

type DeviceLinkCircuit struct {
	Code            string
	OriginDevicePK  string
	TargetDevicePK  string
	LinkPK          string
	LinkCode        string
	LinkType        string
	ContributorCode string
	CommittedRTT    float64
	CommittedJitter float64
}

func ComputeDeviceLinkCircuits(devices []dzsvc.Device, links []dzsvc.Link, contributors []dzsvc.Contributor) []DeviceLinkCircuit {
	devicesByPK := make(map[string]dzsvc.Device)
	for _, d := range devices {
		devicesByPK[d.PK] = d
	}

	contributorsByPK := make(map[string]dzsvc.Contributor)
	for _, c := range contributors {
		contributorsByPK[c.PK] = c
	}

	circuits := make([]DeviceLinkCircuit, 0, 2*len(links))
	for _, link := range links {
		deviceA, okA := devicesByPK[link.SideAPK]
		deviceZ, okZ := devicesByPK[link.SideZPK]
		contributor, okC := contributorsByPK[link.ContributorPK]

		if !okA || !okZ || !okC {
			continue
		}

		// Convert delay and jitter from nanoseconds to microseconds
		committedRTT := float64(link.DelayNs) / 1000.0
		committedJitter := float64(link.JitterNs) / 1000.0

		// Forward circuit: A -> Z
		forwardCode := fmt.Sprintf("%s → %s (%s)", deviceA.Code, deviceZ.Code, link.PK[len(link.PK)-7:])
		circuits = append(circuits, DeviceLinkCircuit{
			Code:            forwardCode,
			OriginDevicePK:  deviceA.PK,
			TargetDevicePK:  deviceZ.PK,
			LinkPK:          link.PK,
			LinkCode:        link.Code,
			LinkType:        link.LinkType,
			ContributorCode: contributor.Code,
			CommittedRTT:    committedRTT,
			CommittedJitter: committedJitter,
		})

		// Reverse circuit: Z -> A
		reverseCode := fmt.Sprintf("%s → %s (%s)", deviceZ.Code, deviceA.Code, link.PK[len(link.PK)-7:])
		circuits = append(circuits, DeviceLinkCircuit{
			Code:            reverseCode,
			OriginDevicePK:  deviceZ.PK,
			TargetDevicePK:  deviceA.PK,
			LinkPK:          link.PK,
			LinkCode:        link.Code,
			LinkType:        link.LinkType,
			ContributorCode: contributor.Code,
			CommittedRTT:    committedRTT,
			CommittedJitter: committedJitter,
		})
	}

	return circuits
}

type DeviceLinkLatencySample struct {
	CircuitCode           string
	Epoch                 uint64
	SampleIndex           int
	TimestampMicroseconds uint64
	RTTMicroseconds       uint32
}

func (v *View) refreshDeviceLinkCircuitsTable(circuits []DeviceLinkCircuit) error {
	return v.refreshTable("dz_device_link_circuits", "DELETE FROM dz_device_link_circuits", "INSERT INTO dz_device_link_circuits (code, origin_device_pk, target_device_pk, link_pk, link_code, link_type, contributor_code, committed_rtt, committed_jitter) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)", len(circuits), func(stmt *sql.Stmt, i int) error {
		c := circuits[i]
		_, err := stmt.Exec(c.Code, c.OriginDevicePK, c.TargetDevicePK, c.LinkPK, c.LinkCode, c.LinkType, c.ContributorCode, c.CommittedRTT, c.CommittedJitter)
		return err
	})
}

func (v *View) refreshDeviceLinkTelemetrySamples(ctx context.Context, circuits []DeviceLinkCircuit) error {
	v.log.Debug("telemetry/device-link: starting sample refresh", "circuits", len(circuits))

	// Get current epoch
	epochInfo, err := v.cfg.EpochRPC.GetEpochInfo(ctx, solanarpc.CommitmentFinalized)
	if err != nil {
		return fmt.Errorf("failed to get epoch info: %w", err)
	}
	currentEpoch := epochInfo.Epoch
	v.log.Debug("telemetry/device-link: current epoch", "epoch", currentEpoch)

	// Fetch samples for current epoch and previous epoch
	epochsToFetch := []uint64{currentEpoch}
	if currentEpoch > 0 {
		epochsToFetch = append(epochsToFetch, currentEpoch-1)
	}
	v.log.Debug("telemetry/device-link: fetching epochs", "epochs", epochsToFetch, "max_concurrency", v.cfg.MaxConcurrency)

	var allSamples []DeviceLinkLatencySample
	var samplesMu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, v.cfg.MaxConcurrency)
	circuitsProcessed := 0
	circuitsWithSamples := 0
	var circuitsWithSamplesMu sync.Mutex

	for _, circuit := range circuits {
		circuitsProcessed++
		originPK, err := solana.PublicKeyFromBase58(circuit.OriginDevicePK)
		if err != nil {
			v.log.Debug("telemetry/device-link: invalid origin device PK", "circuit", circuit.Code, "error", err)
			continue
		}
		targetPK, err := solana.PublicKeyFromBase58(circuit.TargetDevicePK)
		if err != nil {
			v.log.Debug("telemetry/device-link: invalid target device PK", "circuit", circuit.Code, "error", err)
			continue
		}
		linkPK, err := solana.PublicKeyFromBase58(circuit.LinkPK)
		if err != nil {
			v.log.Debug("telemetry/device-link: invalid link PK", "circuit", circuit.Code, "error", err)
			continue
		}

		wg.Add(1)
		sem <- struct{}{} // Acquire semaphore
		go func(circuit DeviceLinkCircuit, originPK, targetPK, linkPK solana.PublicKey) {
			defer wg.Done()
			defer func() { <-sem }() // Release semaphore

			circuitHasSamples := false
			var circuitSamples []DeviceLinkLatencySample

			for _, epoch := range epochsToFetch {
				samples, err := v.cfg.TelemetryRPC.GetDeviceLatencySamples(ctx, originPK, targetPK, linkPK, epoch)
				if err != nil {
					if errors.Is(err, telemetry.ErrAccountNotFound) {
						v.log.Debug("telemetry/device-link: no samples found", "circuit", circuit.Code, "epoch", epoch)
						continue
					}
					v.log.Debug("telemetry/device-link: failed to get latency samples", "circuit", circuit.Code, "epoch", epoch, "error", err)
					continue
				}

				circuitHasSamples = true
				sampleCount := len(samples.Samples)
				v.log.Debug("telemetry/device-link: fetched samples", "circuit", circuit.Code, "epoch", epoch, "samples", sampleCount)

				// Convert samples to our format
				converted := convertDeviceLatencySamples(samples, circuit.Code, epoch)
				circuitSamples = append(circuitSamples, converted...)
			}

			if circuitHasSamples {
				circuitsWithSamplesMu.Lock()
				circuitsWithSamples++
				circuitsWithSamplesMu.Unlock()
			}

			// Append samples to shared slice
			if len(circuitSamples) > 0 {
				samplesMu.Lock()
				allSamples = append(allSamples, circuitSamples...)
				samplesMu.Unlock()
			}
		}(circuit, originPK, targetPK, linkPK)
	}

	wg.Wait()

	v.log.Debug("telemetry/device-link: processed circuits", "total", circuitsProcessed, "with_samples", circuitsWithSamples, "total_samples", len(allSamples))

	// Refresh samples table - no lock needed, DuckDB handles concurrency
	// For large datasets, use a more efficient bulk insert approach
	v.log.Debug("telemetry/device-link: refreshing latency samples table", "samples", len(allSamples))
	if err := v.refreshDeviceLinkLatencySamplesTable(allSamples); err != nil {
		v.log.Error("telemetry/device-link: failed to refresh latency samples", "error", err, "total_samples", len(allSamples))
		return fmt.Errorf("failed to refresh latency samples: %w", err)
	}

	v.log.Debug("telemetry/device-link: sample refresh completed", "samples_inserted", len(allSamples))
	return nil
}

func convertDeviceLatencySamples(samples *telemetry.DeviceLatencySamples, circuitCode string, epoch uint64) []DeviceLinkLatencySample {
	result := make([]DeviceLinkLatencySample, len(samples.Samples))
	for i, rtt := range samples.Samples {
		timestamp := samples.StartTimestampMicroseconds + uint64(i)*samples.SamplingIntervalMicroseconds
		result[i] = DeviceLinkLatencySample{
			CircuitCode:           circuitCode,
			Epoch:                 epoch,
			SampleIndex:           i,
			TimestampMicroseconds: timestamp,
			RTTMicroseconds:       rtt,
		}
	}
	return result
}

func (v *View) refreshDeviceLinkLatencySamplesTable(samples []DeviceLinkLatencySample) error {
	tableRefreshStart := time.Now()
	v.log.Info("telemetry: refreshing table started", "table", "dz_device_link_latency_samples", "rows", len(samples), "start_time", tableRefreshStart)
	defer func() {
		duration := time.Since(tableRefreshStart)
		v.log.Info("telemetry: refreshing table completed", "table", "dz_device_link_latency_samples", "duration", duration.String())
	}()

	if len(samples) == 0 {
		// Use regular refreshTable for empty case
		return v.refreshTable("dz_device_link_latency_samples", "DELETE FROM dz_device_link_latency_samples", "INSERT INTO dz_device_link_latency_samples (circuit_code, epoch, sample_index, timestamp_us, rtt_us) VALUES (?, ?, ?, ?, ?)", 0, nil)
	}

	v.log.Debug("telemetry/device-link: starting bulk insert using COPY FROM", "samples", len(samples))
	startTime := time.Now()

	// Create a temporary CSV file for COPY FROM (much faster than INSERT)
	tmpFile, err := os.CreateTemp("", "dz_device_link_latency_samples_*.csv")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	// Write CSV data
	v.log.Debug("telemetry/device-link: writing CSV file", "samples", len(samples))
	csvWriter := csv.NewWriter(tmpFile)
	csvWriter.Comma = ','

	writeStart := time.Now()
	for _, s := range samples {
		record := []string{
			s.CircuitCode,
			fmt.Sprintf("%d", s.Epoch),
			fmt.Sprintf("%d", s.SampleIndex),
			fmt.Sprintf("%d", s.TimestampMicroseconds),
			fmt.Sprintf("%d", s.RTTMicroseconds),
		}
		if err := csvWriter.Write(record); err != nil {
			return fmt.Errorf("failed to write CSV record: %w", err)
		}
	}
	csvWriter.Flush()
	if err := csvWriter.Error(); err != nil {
		return fmt.Errorf("CSV writer error: %w", err)
	}
	if err := tmpFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync temp file: %w", err)
	}
	writeDuration := time.Since(writeStart)
	v.log.Debug("telemetry/device-link: CSV file written", "duration_ms", writeDuration.Milliseconds(), "file_size_mb", float64(getFileSize(tmpFile))/1024/1024)

	// Get file info for COPY
	fileInfo, err := tmpFile.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat temp file: %w", err)
	}
	v.log.Debug("telemetry/device-link: file ready for COPY", "size_bytes", fileInfo.Size())

	// Close file before COPY (DuckDB needs to open it)
	tmpFile.Close()

	// Use COPY FROM for bulk load (much faster than INSERT)
	txStart := time.Now()
	tx, err := v.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	v.log.Debug("telemetry: transaction begun", "table", "dz_device_link_latency_samples", "tx_start_time", txStart)
	defer tx.Rollback()

	// Clear existing data using TRUNCATE (faster and avoids some concurrency issues)
	if _, err := tx.Exec("TRUNCATE dz_device_link_latency_samples"); err != nil {
		return fmt.Errorf("failed to clear table: %w", err)
	}

	// Use COPY FROM CSV - this is the fastest way to load data
	copyStart := time.Now()
	copySQL := fmt.Sprintf("COPY dz_device_link_latency_samples FROM '%s' (FORMAT CSV, HEADER false)", tmpFile.Name())
	if _, err := tx.Exec(copySQL); err != nil {
		return fmt.Errorf("failed to COPY FROM CSV: %w", err)
	}
	copyDuration := time.Since(copyStart)
	v.log.Debug("telemetry/device-link: COPY FROM completed", "duration", copyDuration.String())

	commitStart := time.Now()
	v.log.Info("telemetry: committing transaction", "table", "dz_device_link_latency_samples", "rows", len(samples), "tx_duration", time.Since(txStart).String(), "commit_start_time", commitStart)
	if err := tx.Commit(); err != nil {
		txDuration := time.Since(txStart)
		v.log.Error("telemetry: transaction commit failed", "table", "dz_device_link_latency_samples", "error", err, "tx_duration", txDuration.String())
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	commitDuration := time.Since(commitStart)
	v.log.Info("telemetry: transaction committed", "table", "dz_device_link_latency_samples", "commit_duration", commitDuration.String(), "total_tx_duration", time.Since(txStart).String())

	totalDuration := time.Since(startTime)
	rate := float64(len(samples)) / totalDuration.Seconds()
	v.log.Debug("telemetry/device-link: bulk insert completed", "samples", len(samples), "total_duration_ms", totalDuration.Milliseconds(), "rate_rows_per_sec", int(rate))
	return nil
}
