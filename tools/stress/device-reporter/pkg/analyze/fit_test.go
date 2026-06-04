package analyze

import (
	"math"
	"testing"
)

func TestLinearLeastSquares_PerfectLine(t *testing.T) {
	// y = 2x + 1
	x := []float64{1, 2, 3, 4, 5}
	y := []float64{3, 5, 7, 9, 11}
	f := LinearLeastSquares(x, y)
	if math.Abs(f.Slope-2) > 1e-9 || math.Abs(f.Intercept-1) > 1e-9 {
		t.Fatalf("expected slope=2 intercept=1, got slope=%g intercept=%g", f.Slope, f.Intercept)
	}
	if math.Abs(f.R2-1) > 1e-9 {
		t.Fatalf("expected R²=1 for perfect line, got %g", f.R2)
	}
	if f.N != 5 {
		t.Fatalf("expected N=5, got %d", f.N)
	}
}

func TestLinearLeastSquares_RejectsTooFewPoints(t *testing.T) {
	f := LinearLeastSquares([]float64{1}, []float64{2})
	if f.N != 0 || f.Slope != 0 {
		t.Fatalf("expected zero-value fit, got %+v", f)
	}
}

func TestLinearLeastSquares_RejectsZeroVariance(t *testing.T) {
	// All x's equal — vertical line, no slope.
	f := LinearLeastSquares([]float64{2, 2, 2}, []float64{1, 2, 3})
	if f.Slope != 0 || f.Intercept != 0 {
		t.Fatalf("expected zero slope for zero-variance x, got %+v", f)
	}
	if f.N != 3 {
		t.Fatalf("N still reflects input size; expected 3, got %d", f.N)
	}
}

func TestLinearLeastSquares_NoisyR2(t *testing.T) {
	// y = x + noise; R² should be < 1 but > 0.5.
	x := []float64{1, 2, 3, 4, 5}
	y := []float64{1.1, 2.2, 2.9, 4.1, 4.9}
	f := LinearLeastSquares(x, y)
	if f.R2 < 0.95 {
		t.Fatalf("expected high R² for near-linear data, got %g", f.R2)
	}
}

func TestPercentile_BasicCases(t *testing.T) {
	v := []float64{1, 2, 3, 4, 5}
	if got := Percentile(v, 0.5); got != 3 {
		t.Errorf("p50 of [1..5] should be 3, got %g", got)
	}
	// p95 of 5 elements at indices [0..4] is 0.95*4 = 3.8 → 4 + 0.8*(5-4) = 4.8
	if got := Percentile(v, 0.95); math.Abs(got-4.8) > 1e-9 {
		t.Errorf("p95 should be 4.8, got %g", got)
	}
	if got := Percentile(v, 0); got != 1 {
		t.Errorf("p0 should be 1, got %g", got)
	}
	if got := Percentile(v, 1); got != 5 {
		t.Errorf("p100 should be 5, got %g", got)
	}
}

func TestPercentile_EmptyAndSingle(t *testing.T) {
	if Percentile(nil, 0.5) != 0 {
		t.Error("empty percentile should be 0")
	}
	if Percentile([]float64{42}, 0.5) != 42 {
		t.Error("single-element percentile should be that element")
	}
}
