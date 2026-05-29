package exec

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIPForIndex(t *testing.T) {
	t.Parallel()

	base := [4]byte{100, 64, 0, 0}
	tests := []struct {
		idx  int
		want [4]byte
	}{
		{0, [4]byte{100, 64, 0, 0}},
		{1, [4]byte{100, 64, 0, 1}},
		{255, [4]byte{100, 64, 0, 255}},
		{256, [4]byte{100, 64, 1, 0}},
		{1000, [4]byte{100, 64, 3, 232}},
	}
	for _, tc := range tests {
		got := ipForIndex(base, tc.idx)
		assert.Equal(t, tc.want, got, "idx=%d", tc.idx)
	}
}
