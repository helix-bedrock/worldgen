// Package terrain turns climate values into terrain shape: the offset,
// factor and jaggedness splines, and the density grid.
package terrain

import "sort"

// Point is the input a spline reads its coordinate from.
type Point struct {
	Continents, Erosion, Weirdness, PeaksValleys float32
}

// coord picks one input of a Point.
type coord uint8

const (
	coordContinents coord = iota
	coordErosion
	coordWeirdness
	coordPeaksValleys
)

func (c coord) of(p *Point) float32 {
	switch c {
	case coordContinents:
		return p.Continents
	case coordErosion:
		return p.Erosion
	case coordWeirdness:
		return p.Weirdness
	default:
		return p.PeaksValleys
	}
}

// spline is a cubic spline over one coordinate. Each point's value is either a
// constant or another spline. Outside the first and last location it extends
// linearly with that end's derivative.
type spline struct {
	coord     coord
	locations []float32
	values    []*spline // nil means constant
	constants []float32
	derivs    []float32
}

func newSpline(c coord) *spline { return &spline{coord: c} }

// add adds a point with a constant value and derivative 0.
func (s *spline) add(loc, v float32) *spline { return s.addD(loc, v, 0) }

// addD adds a point with a constant value and a derivative.
func (s *spline) addD(loc, v, d float32) *spline {
	s.locations = append(s.locations, loc)
	s.values = append(s.values, nil)
	s.constants = append(s.constants, v)
	s.derivs = append(s.derivs, d)
	return s
}

// addS adds a point whose value is another spline, with derivative 0.
func (s *spline) addS(loc float32, v *spline) *spline {
	s.locations = append(s.locations, loc)
	s.values = append(s.values, v)
	s.constants = append(s.constants, 0)
	s.derivs = append(s.derivs, 0)
	return s
}

func (s *spline) valueAt(i int, p *Point) float32 {
	if s.values[i] != nil {
		return s.values[i].eval(p)
	}
	return s.constants[i]
}

func (s *spline) eval(p *Point) float32 {
	n := len(s.locations)
	if n == 0 {
		return 0
	}
	x := s.coord.of(p)
	// i = last index with locations[i] <= x, or -1.
	i := sort.Search(n, func(k int) bool { return s.locations[k] > x }) - 1
	if i < 0 {
		return s.extend(x, 0, p)
	}
	if i == n-1 {
		return s.extend(x, n-1, p)
	}
	x0, x1 := s.locations[i], s.locations[i+1]
	t := (x - x0) / (x1 - x0)
	v0, v1 := s.valueAt(i, p), s.valueAt(i+1, p)
	d0, d1 := s.derivs[i], s.derivs[i+1]
	a := d0*(x1-x0) - (v1 - v0)
	b := -d1*(x1-x0) + (v1 - v0)
	return v0 + t*(v1-v0) + t*(1-t)*(a+t*(b-a))
}

func (s *spline) extend(x float32, i int, p *Point) float32 {
	v := s.valueAt(i, p)
	if d := s.derivs[i]; d != 0 {
		return v + d*(x-s.locations[i])
	}
	return v
}
