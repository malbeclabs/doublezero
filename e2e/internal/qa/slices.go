package qa

// Map applies f to each element of in and returns a new slice containing
// the transformed values.
func Map[A any, B any](in []A, f func(A) B) []B {
	out := make([]B, len(in))
	for i, v := range in {
		out[i] = f(v)
	}
	return out
}

// MapFilter applies f to each element of in and includes the result only
// when f returns ok == true, producing a filtered, transformed slice.
func MapFilter[A any, B any](in []A, f func(A) (B, bool)) []B {
	out := make([]B, 0, len(in))
	for _, v := range in {
		if b, ok := f(v); ok {
			out = append(out, b)
		}
	}
	return out
}
