package collector

import (
	"encoding/csv"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type CSVExporter struct {
	file        *os.File
	writer      *csv.Writer
	filename    string
	isAppending bool
}

func NewCSVExporter(prefix, outputDir string) (*CSVExporter, error) {
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, NewFileIOError("create_output_directory", "failed to create output directory", err).
			WithContext("output_dir", outputDir)
	}

	now := time.Now()
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
			return nil, NewFileIOError("open_csv_file", "failed to open CSV file for appending", err).
				WithContext("file_path", fullPath)
		}
		LogDebug("Opened existing CSV file for appending",
			slog.String("file_path", fullPath),
			slog.String("prefix", prefix))
	} else {
		file, err = os.Create(fullPath)
		if err != nil {
			return nil, NewFileIOError("create_csv_file", "failed to create CSV file", err).
				WithContext("file_path", fullPath)
		}
		LogInfo("Created new CSV file",
			slog.String("file_path", fullPath),
			slog.String("prefix", prefix))
	}

	writer := csv.NewWriter(file)

	return &CSVExporter{
		file:        file,
		writer:      writer,
		filename:    fullPath,
		isAppending: fileExists,
	}, nil
}

func (e *CSVExporter) WriteHeader(header []string) error {
	if e.isAppending {
		LogDebug("Skipping CSV header (appending to existing file)",
			slog.String("file", e.filename))
		return nil
	}

	if err := e.writer.Write(header); err != nil {
		return NewFileIOError("write_csv_header", "failed to write CSV header", err).
			WithContext("file", e.filename).WithContext("header", header)
	}
	LogDebug("Wrote CSV header", slog.Any("header", header), slog.String("file", e.filename))
	return nil
}

func (e *CSVExporter) WriteRecordWithWarning(record []string) {
	if err := e.writer.Write(record); err != nil {
		LogWarning("Failed to write CSV record",
			slog.String("file", e.filename),
			slog.Any("record", record),
			slog.String("error", err.Error()))
	}
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
