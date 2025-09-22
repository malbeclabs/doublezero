package main

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	api "github.com/malbeclabs/doublezero/api/internal"

	"github.com/minio/minio-go"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	listenAddr := flag.String("listen-addr", ":8080", "Address to listen on for HTTP requests")
	csvPath := flag.String("supply-csv", "estimated_supply.csv", "Path to the CSV file with date,estimated_supply")
	useS3 := flag.Bool("use-s3", false, "Whether to download the CSV from S3")
	flag.Parse()

	if *useS3 {
		s3Endpoint := os.Getenv("S3_ENDPOINT")
		if s3Endpoint == "" {
			logger.Error("S3_ENDPOINT environment variable not set")
			os.Exit(1)
		}
		s3AccessKey := os.Getenv("S3_ACCESS_KEY")
		if s3AccessKey == "" {
			logger.Error("S3_ACCESS_KEY environment variable not set")
			os.Exit(1)
		}
		s3SecretKey := os.Getenv("S3_ACCESS_SECRET")
		if s3SecretKey == "" {
			logger.Error("S3_ACCESS_SECRET environment variable not set")
			os.Exit(1)
		}

		s3, err := minio.New(s3Endpoint, s3AccessKey, s3SecretKey, true)
		if err != nil {
			log.Fatal(err)
		}
		if err != nil {
			logger.Error("Failed to create S3 client", "error", err)
			os.Exit(1)
		}
		if err := s3.FGetObjectWithContext(context.Background(), "doublezero", "estimated_supply.csv", *csvPath, minio.GetObjectOptions{}); err != nil {
			logger.Error("Failed to download estimated supply CSV from S3", "error", err)
			os.Exit(1)
		}
	}
	file, err := os.Open(*csvPath)
	if err != nil {
		logger.Error("Failed to open estimated supply CSV", "path", *csvPath, "error", err)
		os.Exit(1)
	}
	defer file.Close()
	supplyMap, err := readSupplyCSV(file)
	if err != nil {
		logger.Error("Failed to read estimated supply CSV", "error", err)
		os.Exit(1)
	}

	rpcClient := api.NewSolanaClient()
	apiServer, err := api.NewApiServer(
		api.WithRpcClient(rpcClient),
		api.WithEstimatedSupply(supplyMap),
		api.WithLogger(logger),
		api.WithListenAddr(*listenAddr),
	)
	if err != nil {
		logger.Error("Failed to create API server", "error", err)
		os.Exit(1)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- apiServer.Run()
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		if err != nil {
			logger.Error("API server error", "error", err)
		}
	case <-quit:
		logger.Info("Shutdown signal received, initiating graceful shutdown...")
	}

	if err := apiServer.Shutdown(); err != nil {
		logger.Error("Server shutdown failed", "error", err)
		os.Exit(1)
	}
	logger.Info("Server shut down gracefully")
}

// readSupplyCSV reads the estimated supply CSV file and returns a map of date strings to estimated supply values.
// The CSV is expected to have the following format:
//
// Date,Almost Circulating Supply
// 19-Sep-2025,"20,471,417,500"
// 20-Sep-2025,"20,471,417,500"
func readSupplyCSV(r io.Reader) (map[string]float64, error) {
	reader := csv.NewReader(r)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("could not read csv records: %w", err)
	}

	supplyMap := make(map[string]float64)
	for i, record := range records {
		if i == 0 {
			continue // Skip header row
		}
		if len(record) < 2 {
			return nil, fmt.Errorf("invalid record on line %d: expected at least 2 columns, got %d", i+1, len(record))
		}
		dateStr := record[0]
		parsedDate, err := time.Parse("2-Jan-2006", dateStr)
		if err != nil {
			return nil, fmt.Errorf("could not parse date on line %d: %w", i+1, err)
		}
		formattedDate := parsedDate.Format("2006-01-02")
		supplyStr := strings.ReplaceAll(record[1], ",", "")
		supply, err := strconv.ParseFloat(supplyStr, 64)
		if err != nil {
			return nil, fmt.Errorf("could not parse supply on line %d: %w", i+1, err)
		}
		supplyMap[formattedDate] = supply
	}
	return supplyMap, nil
}
