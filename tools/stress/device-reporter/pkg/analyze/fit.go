// Package analyze contains pure functions over parsed run data. It has no
// I/O dependencies beyond the parser package so the same functions are
// reusable from the CLI markdown writer, the CSV writer, and tests.
package analyze

import "math"

// LinearFit is a simple ordinary-least-squares fit of y = slope*x + intercept.
// R2 is the coefficient of determination (1.0 = perfect linear fit, 0.0 =
// the mean predicts as well as the slope). N is the number of input points.
type LinearFit struct {
	Slope     float64
	Intercept float64
	R2        float64
	N         int
}

// LinearLeastSquares returns the OLS fit of y on x. Returns a zero-value
// LinearFit when len(x) != len(y), len(x) < 2, or x has zero variance
// (vertical line — no meaningful slope).
func LinearLeastSquares(x, y []float64) LinearFit {
	if len(x) != len(y) || len(x) < 2 {
		return LinearFit{}
	}
	n := float64(len(x))
	var sumX, sumY float64
	for i := range x {
		sumX += x[i]
		sumY += y[i]
	}
	meanX := sumX / n
	meanY := sumY / n

	var ssXY, ssXX float64
	for i := range x {
		dx := x[i] - meanX
		dy := y[i] - meanY
		ssXY += dx * dy
		ssXX += dx * dx
	}
	if ssXX == 0 {
		return LinearFit{N: len(x)}
	}

	slope := ssXY / ssXX
	intercept := meanY - slope*meanX

	// R² = 1 - SSres/SStot where SStot is the variance of y around its mean.
	var ssRes, ssTot float64
	for i := range x {
		pred := slope*x[i] + intercept
		ssRes += (y[i] - pred) * (y[i] - pred)
		dy := y[i] - meanY
		ssTot += dy * dy
	}
	r2 := 0.0
	if ssTot > 0 {
		r2 = 1 - ssRes/ssTot
	}

	return LinearFit{
		Slope:     slope,
		Intercept: intercept,
		R2:        r2,
		N:         len(x),
	}
}

// Percentile returns the p-th percentile (0..1) of `values` using linear
// interpolation between adjacent ranks (R-7 / numpy default). The input
// must be sorted ascending; the caller is expected to copy + sort first.
func Percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 1 {
		return sorted[len(sorted)-1]
	}
	idx := p * float64(len(sorted)-1)
	lo := int(math.Floor(idx))
	hi := int(math.Ceil(idx))
	if lo == hi {
		return sorted[lo]
	}
	frac := idx - float64(lo)
	return sorted[lo]*(1-frac) + sorted[hi]*frac
}
