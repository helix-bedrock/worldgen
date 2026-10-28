package terrain

import "github.com/helix-bedrock/worldgen/1.26.60/internal/mathx"

// The three terrain splines. Their layout and constants were read from BDS
// 1.26.52; they match the vanilla 1.18 terrain design point for point.

// Shape holds the built splines. It is immutable after creation and safe for
// concurrent use.
type Shape struct {
	offset, factor, jaggedness *spline
}

// NewShape builds the overworld splines.
func NewShape() *Shape {
	return &Shape{offset: offsetSpline(), factor: factorSpline(), jaggedness: jaggednessSpline()}
}

// offsetBias is added to the offset spline value.
const offsetBias = -0.50375

// Offset returns the terrain height offset, including the bias.
func (s *Shape) Offset(p *Point) float32 { return s.offset.eval(p) + offsetBias }

// Factor returns how steeply density falls with height.
func (s *Shape) Factor(p *Point) float32 { return s.factor.eval(p) }

// Jaggedness returns how strongly the jagged noise is applied.
func (s *Shape) Jaggedness(p *Point) float32 { return s.jaggedness.eval(p) }

// PeaksValleys folds weirdness into the peaks-and-valleys value.
func PeaksValleys(w float32) float32 {
	return -(mathx.AbsFloat(mathx.AbsFloat(w)-0.6666667) - 0.33333334) * 3
}

func lerpf(t, a, b float32) float32 { return a + t*(b-a) }

func offsetSpline() *spline {
	beach := erosionOffset(-0.15, 0, 0, 0.1, 0, -0.03, false, false)
	low := erosionOffset(-0.1, 0.03, 0.1, 0.1, 0.01, -0.03, false, false)
	mid := erosionOffset(-0.1, 0.03, 0.1, 0.7, 0.01, -0.03, true, true)
	high := erosionOffset(-0.05, 0.03, 0.1, 1.0, 0.01, 0.01, true, true)
	return newSpline(coordContinents).
		add(-1.1, 0.044).
		add(-1.02, -0.2222).
		add(-0.51, -0.2222).
		add(-0.44, -0.12).
		add(-0.18, -0.12).
		addS(-0.16, beach).
		addS(-0.15, beach).
		addS(-0.1, low).
		addS(0.25, mid).
		addS(1.0, high)
}

// erosionOffset is the offset sub-spline over erosion for one continent band.
func erosionOffset(p1, p2, p3, p4, p5, p6 float32, extended, saddle bool) *spline {
	s1 := mountainRidge(lerpf(p4, 0.6, 1.5), saddle)
	s2 := mountainRidge(lerpf(p4, 0.6, 1.0), saddle)
	s3 := mountainRidge(p4, saddle)
	s4 := ridge(p1-0.15, 0.5*p4, lerpf(0.5, 0.5, 0.5)*p4, 0.5*p4, 0.6*p4, 0.5)
	s5 := ridge(p1, p5*p4, p2*p4, 0.5*p4, 0.6*p4, 0.5)
	s6 := ridge(p1, p5, p5, p2, p3, 0.5)
	s7 := ridge(p1, p5, p5, p2, p3, 0.5)
	s8 := newSpline(coordPeaksValleys).add(-1, p1).addS(-0.4, s6).add(0, p3+0.07)
	s9 := ridge(-0.02, p6, p6, p2, p3, 0)
	s := newSpline(coordErosion).
		addS(-0.85, s1).
		addS(-0.7, s2).
		addS(-0.4, s3).
		addS(-0.35, s4).
		addS(-0.1, s5).
		addS(0.2, s6)
	if extended {
		s.addS(0.4, s7).addS(0.45, s8).addS(0.55, s8).addS(0.58, s7)
	}
	return s.addS(0.7, s9)
}

// ridge is a 5-point spline over peaks-and-valleys.
func ridge(valley, low, mid, high, peak, minSlope float32) *spline {
	d1 := max(0.5*(low-valley), minSlope)
	d2 := 5 * (mid - low)
	return newSpline(coordPeaksValleys).
		addD(-1, valley, d1).
		addD(-0.4, low, min(d1, d2)).
		addD(0, mid, d2).
		addD(0.4, high, 2*(high-mid)).
		addD(1, peak, 0.7*(peak-high))
}

// mountainCutoff is the continentalness below which mountain ridges flatten.
const mountainCutoff = -0.7

func mountainContinentalness(pv, magnitude float32) float32 {
	const cutoff = mountainCutoff
	const k1, k2 = 1.17, 0.46082947
	f5 := 1 - (1-magnitude)*0.5
	f6 := 0.5 * (1 - magnitude)
	v := (pv+k1)*k2*f5 - f6
	if pv < cutoff {
		return max(v, -0.2222)
	}
	return max(v, 0)
}

func mountainZeroPoint(magnitude float32) float32 {
	f3 := 1 - (1-magnitude)*0.5
	f4 := 0.5 * (1 - magnitude)
	return f4/(0.46082947*f3) - 1.17
}

func slope(a, b, x0, x1 float32) float32 { return (b - a) / (x1 - x0) }

func mountainRidge(magnitude float32, saddle bool) *spline {
	s := newSpline(coordPeaksValleys)
	lo := mountainContinentalness(-1, magnitude)
	hi := mountainContinentalness(1, magnitude)
	zero := mountainZeroPoint(magnitude)
	if -0.65 < zero && zero < 1 {
		v65 := mountainContinentalness(-0.65, magnitude)
		v75 := mountainContinentalness(-0.75, magnitude)
		d := slope(lo, v75, -1, -0.75)
		s.addD(-1, lo, d)
		s.add(-0.75, v75)
		s.add(-0.65, v65)
		vz := mountainContinentalness(zero, magnitude)
		dz := slope(vz, hi, zero, 1)
		s.add(zero-0.01, vz)
		s.addD(zero, vz, dz)
		s.addD(1, hi, dz)
		return s
	}
	d := slope(lo, hi, -1, 1)
	if saddle {
		s.add(-1, max(0.2, lo))
		s.addD(0, lerpf(0.5, lo, hi), d)
	} else {
		s.addD(-1, lo, d)
	}
	return s.addD(1, hi, d)
}

func factorSpline() *spline {
	return newSpline(coordContinents).
		add(-0.19, 3.95).
		addS(-0.15, erosionFactor(6.25, true)).
		addS(-0.1, erosionFactor(5.47, true)).
		addS(0.03, erosionFactor(5.08, true)).
		addS(0.06, erosionFactor(4.69, false))
}

func erosionFactor(v float32, flat bool) *spline {
	base := newSpline(coordWeirdness).add(-0.2, 6.3).add(0.2, v)
	s := newSpline(coordErosion).
		addS(-0.6, base).
		addS(-0.5, newSpline(coordWeirdness).add(-0.05, 6.3).add(0.05, 2.67)).
		addS(-0.35, base).
		addS(-0.25, base).
		addS(-0.1, newSpline(coordWeirdness).add(-0.05, 2.67).add(0.05, 6.3)).
		addS(0.03, base)
	if flat {
		a := newSpline(coordWeirdness).add(0, v).add(0.1, 0.625)
		b := newSpline(coordPeaksValleys).add(-0.9, v).addS(-0.69, a)
		return s.add(0.35, v).addS(0.45, b).addS(0.55, b).add(0.62, v)
	}
	c := newSpline(coordPeaksValleys).addS(-0.7, base).add(-0.15, 1.37)
	d := newSpline(coordPeaksValleys).addS(0.45, base).add(0.7, 1.56)
	return s.addS(0.05, d).addS(0.4, d).addS(0.45, c).addS(0.55, c).add(0.58, v)
}

func jaggednessSpline() *spline {
	return newSpline(coordContinents).
		add(-0.11, 0).
		addS(0.03, erosionJaggedness(1, 0.5, 0, 0)).
		addS(0.65, erosionJaggedness(1, 1, 1, 0))
}

func erosionJaggedness(a, b, c, d float32) *spline {
	s1 := ridgeJaggedness(a, c)
	s2 := ridgeJaggedness(b, d)
	return newSpline(coordErosion).
		addS(-1, s1).
		addS(-0.78, s2).
		addS(-0.5775, s2).
		add(-0.375, 0)
}

func ridgeJaggedness(high, mid float32) *spline {
	lo := PeaksValleys(0.4)
	hi := PeaksValleys(0.56666666)
	m := (lo + hi) / 2
	s := newSpline(coordPeaksValleys).add(lo, 0)
	if mid > 0 {
		s.addS(m, weirdnessJaggedness(mid))
	} else {
		s.add(m, 0)
	}
	if high > 0 {
		s.addS(1, weirdnessJaggedness(high))
	} else {
		s.add(1, 0)
	}
	return s
}

func weirdnessJaggedness(m float32) *spline {
	return newSpline(coordWeirdness).add(-0.01, 0.63*m).add(0.01, 0.3*m)
}
