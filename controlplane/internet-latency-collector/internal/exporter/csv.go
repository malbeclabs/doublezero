package exporter

import (
	"context"
	"encoding/csv"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var (
	csvRecordHeader = []string{"source_exchange_code", "target_exchange_code", "timestamp", "latency"}
)

type CSVExporter struct {
	log         *slog.Logger
	file        *os.File
	writer      *csv.Writer
	filename    string
	isAppending bool
}

func NewCSVExporter(log *slog.Logger, prefix, outputDir string) (*CSVExporter, error) {
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}

	now := time.Now().UTC()
	hourlyTimestamp := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 0, 0, now.Location())
	filename := fmt.Sprintf("%s_%s.csv", prefix, hourlyTimestamp.Format("2006-01-02T15:04:05"))
	fullPath := filepath.Join(outputDir, filename)

	fileExists := false
	if _, err := os.Stat(fullPath); err == nil {
		fileExists = true
	}

	var file *os.File
	var err error
	if fileExists {
		file, err = os.OpenFile(fullPath, os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return nil, fmt.Errorf("failed to open CSV file for appending: %w", err)
		}
		log.Debug("Opened existing CSV file for appending",
			slog.String("file_path", fullPath),
			slog.String("prefix", prefix))
	} else {
		file, err = os.Create(fullPath)
		if err != nil {
			return nil, fmt.Errorf("failed to create CSV file: %w", err)
		}
		log.Info("Created new CSV file",
			slog.String("file_path", fullPath),
			slog.String("prefix", prefix))
	}

	writer := csv.NewWriter(file)

	e := &CSVExporter{
		log:         log,
		file:        file,
		writer:      writer,
		filename:    fullPath,
		isAppending: fileExists,
	}

	if err := e.WriteHeader(csvRecordHeader); err != nil {
		return nil, fmt.Errorf("failed to write CSV header: %w", err)
	}

	return e, nil
}

func (e *CSVExporter) WriteHeader(header []string) error {
	if e.isAppending {
		e.log.Debug("Skipping CSV header (appending to existing file)",
			slog.String("file", e.filename))
		return nil
	}

	if err := e.writer.Write(header); err != nil {
		return fmt.Errorf("failed to write CSV header: %w", err)
	}
	e.writer.Flush()
	e.log.Debug("Wrote CSV header", slog.Any("header", header), slog.String("file", e.filename))
	return nil
}

func (e *CSVExporter) WriteRecords(ctx context.Context, records []Record) error {
	for _, record := range records {
		row := []string{
			record.SourceExchangeCode,
			record.TargetExchangeCode,
			record.Timestamp.Format(time.RFC3339),
			record.RTT.String(),
		}

		if err := e.writer.Write(row); err != nil {
			return fmt.Errorf("failed to write CSV record: %w", err)
		}

		e.log.Debug("Wrote CSV record", "record", record)
	}

	e.writer.Flush()

	return nil
}

func (e *CSVExporter) Close() error {
	e.writer.Flush()
	return e.file.Close()
}

func (e *CSVExporter) GetFilename() string {
	return e.filename
}

func EscapeCSVField(field string) string {
	if strings.Contains(field, ",") {
		return fmt.Sprintf("\"%s\"", field)
	}
	return field
}
