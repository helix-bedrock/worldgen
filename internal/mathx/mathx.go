// Package mathx holds the small integer and float32 helpers the generators
// share.
package mathx

import "math"

// Abs returns the absolute value of v.
func Abs(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

// AbsFloat returns the absolute value of v.
func AbsFloat(v float32) float32 { return float32(math.Abs(float64(v))) }

// FloorDiv returns a / b rounded toward negative infinity.
func FloorDiv(a, b int32) int32 {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}
	return q
}

// Sqrt32 returns the square root of v, rounded to float32.
func Sqrt32(v float32) float32 { return float32(math.Sqrt(float64(v))) }
