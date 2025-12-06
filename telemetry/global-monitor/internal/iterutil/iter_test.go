package iterutil

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMap(t *testing.T) {
	seq := func(yield func(int) bool) {
		for _, v := range []int{1, 2, 3, 4} {
			if !yield(v) {
				return
			}
		}
	}

	double := func(x int) int { return x * 2 }

	out := slices.Collect(Map(seq, double))
	require.Equal(t, []int{2, 4, 6, 8}, out)
}

func TestMap_EarlyTermination(t *testing.T) {
	count := 0

	seq := func(yield func(int) bool) {
		for _, v := range []int{1, 2, 3, 4, 5} {
			count++
			if !yield(v) {
				return
			}
		}
	}

	mapped := Map(seq, func(x int) int { return x })

	out := slices.Collect(func(yield func(int) bool) {
		i := 0
		mapped(func(v int) bool {
			if i == 2 {
				return false
			}
			i++
			return yield(v)
		})
	})

	require.Equal(t, []int{1, 2}, out)
	require.Equal(t, 3, count, "upstream should stop after 3 pulls")
}

func TestMapFilter(t *testing.T) {
	seq := func(yield func(int) bool) {
		for _, v := range []int{1, 2, 3, 4, 5, 6} {
			if !yield(v) {
				return
			}
		}
	}

	onlyEvens := func(x int) (int, bool) {
		if x%2 == 0 {
			return x * 10, true
		}
		return 0, false
	}

	out := slices.Collect(MapFilter(seq, onlyEvens))
	require.Equal(t, []int{20, 40, 60}, out)
}

func TestMapFilter_EarlyTermination(t *testing.T) {
	count := 0

	seq := func(yield func(int) bool) {
		for _, v := range []int{1, 2, 3, 4, 5, 6, 7} {
			count++
			if !yield(v) {
				return
			}
		}
	}

	filter := func(x int) (int, bool) {
		if x%2 == 1 {
			return x, true
		}
		return 0, false
	}

	filtered := MapFilter(seq, filter)

	out := slices.Collect(func(yield func(int) bool) {
		i := 0
		filtered(func(v int) bool {
			if i == 3 {
				return false
			}
			i++
			return yield(v)
		})
	})

	require.Equal(t, []int{1, 3, 5}, out)
	require.Greater(t, count, 3, "upstream iteration should exceed collected values due to skipped evens")
}

func TestCollectSet(t *testing.T) {
	seq := func(yield func(int) bool) {
		for _, v := range []int{1, 2, 2, 3, 4, 3} {
			if !yield(v) {
				return
			}
		}
	}

	set := CollectSet(seq)

	require.Len(t, set, 4)
	require.Contains(t, set, 1)
	require.Contains(t, set, 2)
	require.Contains(t, set, 3)
	require.Contains(t, set, 4)
}
