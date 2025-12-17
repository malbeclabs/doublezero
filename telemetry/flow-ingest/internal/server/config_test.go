package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
)

func TestTelemetry_FlowIngest_Server_ConfigValidate_DefaultsAndErrors(t *testing.T) {
	t.Parallel()

	t.Run("missing required fields", func(t *testing.T) {
		t.Parallel()
		cfg := &Config{}
		err := cfg.Validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "logger is required")
	})

	t.Run("defaults applied", func(t *testing.T) {
		t.Parallel()
		cfg := &Config{
			Logger:         newLogger(),
			FlowListener:   newUDPConn(t),
			HealthListener: newTCPListener(t),
			KafkaClient: &mockKafkaClient{
				ProduceFunc: func(context.Context, *kgo.Record, func(*kgo.Record, error)) {},
			},
			KafkaTopic: "topic",
		}
		require.NoError(t, cfg.Validate())
		require.NotZero(t, cfg.ReadTimeout)
		require.NotZero(t, cfg.WorkerCount)
		require.NotZero(t, cfg.BufferSizePackets)
		require.NotZero(t, cfg.BufferSizeBytes)
	})
}
