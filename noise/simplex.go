package noise

// simplexGrad are the 12 edge directions simplex noise picks from.
var simplexGrad = [12][3]float32{
	{1, 1, 0}, {-1, 1, 0}, {1, -1, 0}, {-1, -1, 0},
	{1, 0, 1}, {-1, 0, 1}, {1, 0, -1}, {-1, 0, -1},
	{0, 1, 1}, {0, -1, 1}, {0, 1, -1}, {0, -1, -1},
}

const (
	f2 = 0.36602542 // (sqrt(3) - 1) / 2
	g2 = 0.21132487 // (3 - sqrt(3)) / 6
	f3 = 0.33333334 // 1 / 3
	g3 = 0.16666667 // 1 / 6
)

// Random is what a simplex octave draws its origin and permutation from. Both
// the Mersenne Twister and Xoroshiro are used for this, depending on the
// caller.
type Random interface {
	Float() float32
	IntN(n int32) int32
}

// Simplex is one octave of simplex noise. It is immutable after creation and
// safe for concurrent use.
type Simplex struct {
	ox, oy, oz float32
	p          [512]int32
}

// newSimplexOrder draws three origin floats, then shuffles the
// permutation. swap stores the first draw as the x origin instead of the
// third; callers differ and both orders are used.
func newSimplexOrder(r Random, swap bool) Simplex {
	var s Simplex
	a, b, c := r.Float(), r.Float(), r.Float()
	if swap {
		s.ox, s.oy, s.oz = a*256, b*256, c*256
	} else {
		s.ox, s.oy, s.oz = c*256, b*256, a*256
	}
	for i := range 256 {
		s.p[i] = int32(i)
	}
	for i := range int32(256) {
		j := i + r.IntN(256-i)
		s.p[i], s.p[j] = s.p[j], s.p[i]
		s.p[i+256] = s.p[i]
	}
	return s
}

// fastFloor rounds down, except that values <= 0 that are already whole go
// one lower. Vanilla does it this way and the noise depends on it.
func fastFloor(v float32) int32 {
	if v > 0 {
		return int32(v)
	}
	return int32(v) - 1
}

func (s *Simplex) corner2(gi int32, x, y float32) float32 {
	t := 0.5 - x*x - y*y
	if t < 0 {
		return 0
	}
	g := &simplexGrad[gi%12]
	t *= t
	return t * t * (g[0]*x + g[1]*y)
}

// Sample2 returns 2D simplex noise at (x, y), without the octave origin.
func (s *Simplex) Sample2(x, y float32) float32 {
	sk := (x + y) * f2
	i, j := fastFloor(x+sk), fastFloor(y+sk)
	t := float32(i+j) * g2
	x0, y0 := x-(float32(i)-t), y-(float32(j)-t)
	var i1, j1 int32
	if x0 > y0 {
		i1 = 1
	} else {
		j1 = 1
	}
	x1, y1 := x0-float32(i1)+g2, y0-float32(j1)+g2
	x2, y2 := x0-1+2*g2, y0-1+2*g2
	ii, jj := i&255, j&255
	n0 := s.corner2(s.p[ii+s.p[jj]], x0, y0)
	n1 := s.corner2(s.p[ii+i1+s.p[jj+j1]], x1, y1)
	n2 := s.corner2(s.p[ii+1+s.p[jj+1]], x2, y2)
	return (n0 + n1 + n2) * 70
}

func (s *Simplex) corner3(gi int32, x, y, z float32) float32 {
	t := 0.6 - x*x - y*y - z*z
	if t < 0 {
		return 0
	}
	g := &simplexGrad[gi%12]
	t *= t
	return t * t * (g[0]*x + g[1]*y + g[2]*z)
}

// Sample3 returns 3D simplex noise at (x, y, z), without the octave origin.
func (s *Simplex) Sample3(x, y, z float32) float32 {
	sk := (x + y + z) * f3
	i, j, k := fastFloor(x+sk), fastFloor(y+sk), fastFloor(z+sk)
	t := float32(i+j+k) * g3
	x0, y0, z0 := x-(float32(i)-t), y-(float32(j)-t), z-(float32(k)-t)
	var i1, j1, k1, i2, j2, k2 int32
	if x0 >= y0 {
		switch {
		case y0 >= z0:
			i1, i2, j2 = 1, 1, 1
		case x0 >= z0:
			i1, i2, k2 = 1, 1, 1
		default:
			k1, i2, k2 = 1, 1, 1
		}
	} else {
		switch {
		case y0 < z0:
			k1, j2, k2 = 1, 1, 1
		case x0 < z0:
			j1, j2, k2 = 1, 1, 1
		default:
			j1, i2, j2 = 1, 1, 1
		}
	}
	x1, y1, z1 := x0-float32(i1)+g3, y0-float32(j1)+g3, z0-float32(k1)+g3
	x2, y2, z2 := x0-float32(i2)+2*g3, y0-float32(j2)+2*g3, z0-float32(k2)+2*g3
	x3, y3, z3 := x0-1+0.5, y0-1+0.5, z0-1+0.5
	ii, jj, kk := i&255, j&255, k&255
	n0 := s.corner3(s.p[ii+s.p[jj+s.p[kk]]], x0, y0, z0)
	n1 := s.corner3(s.p[ii+i1+s.p[jj+j1+s.p[kk+k1]]], x1, y1, z1)
	n2 := s.corner3(s.p[ii+i2+s.p[jj+j2+s.p[kk+k2]]], x2, y2, z2)
	n3 := s.corner3(s.p[ii+1+s.p[jj+1+s.p[kk+1]]], x3, y3, z3)
	return (n0 + n1 + n2 + n3) * 32
}

// SimplexOctaves is a stack of simplex octaves. Octave i is sampled at half
// the input scale of octave i-1 and counts twice as much. Scale is
// 1 / sum(1/a) over the octave amplitudes a; callers apply it where vanilla
// does.
type SimplexOctaves struct {
	octs  []Simplex
	Scale float32
}

// NewSimplexOctaves draws n octaves from r. swap selects the origin order
// (see newSimplexOrder).
func NewSimplexOctaves(r Random, n int, swap bool) *SimplexOctaves {
	o := &SimplexOctaves{octs: make([]Simplex, n)}
	var sum float32
	a := float32(1)
	for i := range o.octs {
		o.octs[i] = newSimplexOrder(r, swap)
		sum += 1 / a
		a *= 0.5
	}
	if sum != 0 {
		o.Scale = 1 / sum
	}
	return o
}

// Sum2 adds the octaves of 2D noise at (x, y): octave i at (x, y) * a_i,
// divided by a_i, with a_0 = 1 halving per octave. Not scaled.
func (o *SimplexOctaves) Sum2(x, y float32) float32 {
	var v float32
	a := float32(1)
	for i := range o.octs {
		v += o.octs[i].Sample2(x*a, y*a) / a
		a *= 0.5
	}
	return v
}

// Sum3 is Sum2 in three dimensions.
func (o *SimplexOctaves) Sum3(x, y, z float32) float32 {
	var v float32
	a := float32(1)
	for i := range o.octs {
		v += o.octs[i].Sample3(x*a, y*a, z*a) / a
		a *= 0.5
	}
	return v
}

// SurfaceDepth fills buf (index x + 16*z) with the surface-depth noise of the
// chunk whose first block is (x0, z0): octave i with a_i = 2^-i adds
// simplex((x0+x)*0.125*a + ox, (z0+z)*0.125*a + oy) * 0.55 / a.
func (o *SimplexOctaves) SurfaceDepth(buf *[256]float32, x0, z0 int32) {
	o.SurfaceNoise(buf, x0, z0, 0.125)
}

// SurfaceNoise is SurfaceDepth with base frequency freq in place of 0.125:
// the Nether samples its surface noise at 0.0625.
func (o *SimplexOctaves) SurfaceNoise(buf *[256]float32, x0, z0 int32, freq float32) {
	*buf = [256]float32{}
	a := float32(1)
	for i := range o.octs {
		s := &o.octs[i]
		f, amp := freq*a, 0.55/a
		for z := range int32(16) {
			zz := (float32(z)+float32(z0))*f + s.oy
			for x := range int32(16) {
				xx := (float32(x)+float32(x0))*f + s.ox
				buf[x+16*z] += s.Sample2(xx, zz) * amp
			}
		}
		a *= 0.5
	}
}
