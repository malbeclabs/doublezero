package enricher

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestClickhouseWriterOptionsConnMaxLifetime(t *testing.T) {
	cw := &ClickhouseWriter{addr: "localhost:9440"}
	require.Equal(t, 3*time.Minute, cw.options().ConnMaxLifetime)
}
