package collector

import (
	"log/slog"
	"os"
)

var Logger *slog.Logger

type LogLevel string

const (
	LogLevelDebug LogLevel = "debug"
	LogLevelInfo  LogLevel = "info"
	LogLevelWarn  LogLevel = "warn"
	LogLevelError LogLevel = "error"
)

func InitLogger(level LogLevel) {
	var slogLevel slog.Level

	switch level {
	case LogLevelDebug:
		slogLevel = slog.LevelDebug
	case LogLevelInfo:
		slogLevel = slog.LevelInfo
	case LogLevelWarn:
		slogLevel = slog.LevelWarn
	case LogLevelError:
		slogLevel = slog.LevelError
	default:
		slogLevel = slog.LevelInfo
	}

	isDebugMode := (level == LogLevelDebug)

	opts := &slog.HandlerOptions{
		Level:     slogLevel,
		AddSource: isDebugMode, // Add source location when in debug mode
	}

	var handler slog.Handler
	if isDebugMode {
		handler = slog.NewTextHandler(os.Stderr, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	}

	Logger = slog.New(handler)
	slog.SetDefault(Logger)
}

func GetLogger() *slog.Logger {
	if Logger == nil {
		// Initialize with default settings if not already initialized
		InitLogger(LogLevelInfo)
	}
	return Logger
}

func LogError(err *CollectorError, msg string) {
	attrs := []slog.Attr{
		slog.String("error_type", string(err.Type)),
		slog.String("operation", err.Operation),
		slog.String("error_message", err.Message),
	}

	for key, value := range err.Context {
		attrs = append(attrs, slog.Any(key, value))
	}

	if err.Cause != nil {
		attrs = append(attrs, slog.String("cause", err.Cause.Error()))
	}

	Logger.LogAttrs(nil, slog.LevelError, msg, attrs...)
}

func LogWarning(msg string, attrs ...slog.Attr) {
	Logger.LogAttrs(nil, slog.LevelWarn, msg, attrs...)
}

func LogInfo(msg string, attrs ...slog.Attr) {
	Logger.LogAttrs(nil, slog.LevelInfo, msg, attrs...)
}

func LogDebug(msg string, attrs ...slog.Attr) {
	Logger.LogAttrs(nil, slog.LevelDebug, msg, attrs...)
}

func LogOperationStart(operation string, attrs ...slog.Attr) {
	allAttrs := append([]slog.Attr{slog.String("operation", operation)}, attrs...)
	Logger.LogAttrs(nil, slog.LevelInfo, "Operation started", allAttrs...)
}

func LogOperationComplete(operation string, attrs ...slog.Attr) {
	allAttrs := append([]slog.Attr{slog.String("operation", operation)}, attrs...)
	Logger.LogAttrs(nil, slog.LevelInfo, "Operation completed", allAttrs...)
}

func LogOperationFailed(operation string, err error, attrs ...slog.Attr) {
	allAttrs := append([]slog.Attr{
		slog.String("operation", operation),
		slog.String("error", err.Error()),
	}, attrs...)
	Logger.LogAttrs(nil, slog.LevelError, "Operation failed", allAttrs...)
}
