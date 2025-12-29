package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/malbeclabs/doublezero/telemetry/flow-analytics/internal/analytics"
)

func main() {
	// Setup logging
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	// Load configuration
	cfg := analytics.ConfigFromEnv()
	if err := cfg.Validate(); err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	// Create ClickHouse client
	chClient, err := analytics.NewClickHouseClient(
		analytics.WithAddr(cfg.ClickHouseAddr),
		analytics.WithDatabase(cfg.ClickHouseDatabase),
		analytics.WithUser(cfg.ClickHouseUser),
		analytics.WithPassword(cfg.ClickHousePassword),
		analytics.WithSecure(cfg.ClickHouseSecure),
		analytics.WithLogger(logger),
	)
	if err != nil {
		logger.Error("failed to create ClickHouse client", "error", err)
		os.Exit(1)
	}

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := chClient.Ping(ctx); err != nil {
		logger.Warn("ClickHouse ping failed (continuing anyway)", "error", err)
	}
	cancel()

	// Create app
	app, err := analytics.NewApp(
		analytics.WithQuerier(chClient),
		analytics.WithTableName(cfg.TableName),
		analytics.WithAppLogger(logger),
	)
	if err != nil {
		logger.Error("failed to create app", "error", err)
		os.Exit(1)
	}

	// Setup routes
	mux := http.NewServeMux()

	// Static files
	mux.Handle("/static/", http.FileServer(http.FS(analytics.StaticFS())))

	// API routes
	mux.HandleFunc("/", app.HandleIndex)
	mux.HandleFunc("/api/typeahead", app.HandleTypeahead)
	mux.HandleFunc("/api/query", app.HandleQuery)
	mux.HandleFunc("/api/columns", app.HandleColumns)
	mux.HandleFunc("/api/filter/add", app.HandleAddFilter)
	mux.HandleFunc("/api/groupby/add", app.HandleAddGroupBy)

	// Health check
	mux.HandleFunc("/healthz", app.HandleHealthz)

	// Create server
	server := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: mux,
	}

	// Handle graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		<-sigCh

		logger.Info("shutting down server")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			logger.Error("shutdown error", "error", err)
		}
	}()

	// Start server
	logger.Info("starting Flow Analytics server",
		"port", cfg.Port,
		"clickhouse", cfg.ClickHouseAddr,
		"table", cfg.TableName,
	)

	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		logger.Error("server failed", "error", err)
		os.Exit(1)
	}
}
