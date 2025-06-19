package logging

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	tclog "github.com/testcontainers/testcontainers-go/log"
)

type testcontainersLogger struct {
	logger *slog.Logger
}

func NewTestcontainersAdapter(logger *slog.Logger) *testcontainersLogger {
	return &testcontainersLogger{logger: logger}
}

func (s *testcontainersLogger) Printf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if strings.Contains(msg, "Connected to docker:") {
		return
	}
	switch {
	case strings.HasPrefix(format, "❌"):
		s.logger.ErrorContext(context.Background(), msg)
	case strings.HasPrefix(format, "✅"), strings.HasPrefix(format, "🐳"), strings.HasPrefix(format, "🔔"):
		s.logger.DebugContext(context.Background(), msg)
	default:
		s.logger.InfoContext(context.Background(), msg)
	}
}

func SetTestcontainersLogger(logger *slog.Logger) {
	tclog.SetDefault(NewTestcontainersAdapter(logger))
}
