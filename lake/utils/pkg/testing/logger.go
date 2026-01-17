package laketesting

import (
	"log/slog"
	"os"
)

func NewLogger() *slog.Logger {
	debugLevel := os.Getenv("DEBUG")
	var level slog.Level
	switch debugLevel {
	case "2":
		level = slog.LevelDebug
	case "1":
		level = slog.LevelInfo
	default:
		// Suppress logs by default (only show errors and above)
		level = slog.LevelError
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}
