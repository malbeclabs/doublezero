package iterutil

import "iter"

// Map returns a new iter.Seq whose elements are produced by applying f to
// each element of the input sequence in. The returned sequence is lazy:
// values are transformed only as they are requested by the downstream
// iterator. Early termination is respected; if the downstream yield
// function returns false, iteration stops immediately.
func Map[A, B any](in iter.Seq[A], f func(A) B) iter.Seq[B] {
	return func(yield func(B) bool) {
		in(func(a A) bool {
			return yield(f(a))
		})
	}
}

// MapFilter returns a new iter.Seq that applies f to each element of the
// input sequence in. For each input value, f returns a transformed value
// and an ok flag. Only values with ok == true are yielded. The sequence is
// lazy, performs no intermediate allocations, and respects early
// termination by propagating the yield function's return value.
func MapFilter[A, B any](in iter.Seq[A], f func(A) (B, bool)) iter.Seq[B] {
	return func(yield func(B) bool) {
		in(func(a A) bool {
			if b, ok := f(a); ok {
				return yield(b)
			}
			return true
		})
	}
}

// CollectSet consumes the input sequence in and returns a set represented as
// map[A]struct{}. The sequence is fully consumed. Duplicate elements naturally
// collapse since map keys are unique.
func CollectSet[A comparable](in iter.Seq[A]) map[A]struct{} {
	m := make(map[A]struct{})
	in(func(a A) bool {
		m[a] = struct{}{}
		return true
	})
	return m
}
