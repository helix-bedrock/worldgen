package noise

import "github.com/helix-bedrock/worldgen/1.26.60/rand"

// legacyOctave is one octave of the old-style Perlin noise the terrain's base
// 3D noise is built from. Unlike Perlin, its origin is drawn with nextFloat,
// and it is only sampled on a whole density grid at once, the way BDS does
// (see AddGrid).
type legacyOctave struct {
	ox, oy, oz float32
	p          [512]int32
}

func newLegacyOctave(r Random) legacyOctave {
	var n legacyOctave
	n.ox = r.Float() * 256
	n.oy = r.Float() * 256
	n.oz = r.Float() * 256
	for i := range 256 {
		n.p[i] = int32(i)
	}
	for i := range int32(256) {
		j := i + r.IntN(256-i)
		n.p[i], n.p[j] = n.p[j], n.p[i]
		n.p[i+256] = n.p[i]
	}
	return n
}

// Grid is the density grid's layout: 5 x 41 x 5 corners, index
// (x*5+z)*41 + y.
const (
	GridXZ = 5
	GridY  = 41
)

// GridValues holds one value per density grid corner.
type GridValues [GridXZ * GridY * GridXZ]float32

// addGrid adds this octave, times invAmp, to every corner of the grid whose
// first corner is at (x0, y0, z0) in cell units, with the per-axis input
// scale s. It copies BDS's sampler exactly:
//
//   - corners are visited x, then z, then y;
//   - the y offset fed to the gradients is frac - floor(frac/sy)*sy (the
//     "y fudge"), while smoothing uses the plain fraction;
//   - when a corner's y lattice cell equals the previous corner's, the eight
//     gradient products are not recomputed: the previous corner's are reused,
//     even across columns. BDS never resets this between columns, so the
//     lowest octaves carry values over from one column to the next.
func (n *legacyOctave) addGrid(out []float32, nxz, ny int, x0, y0, z0 float32, sx, sy, sz, invAmp float32) {
	start := 0*sy + n.oy
	startCell := int32(start)
	startFrac := start - float32(int32(start))
	lastCell := int32(-1)
	var cache [4]float32
	idx := 0
	for i := range nxz {
		fx := (float32(i)+x0)*sx + n.ox
		flx := floor32(fx)
		xi := int32(uint8(int32(flx)))
		xf := fx - flx
		xs := smooth(xf)
		for k := range nxz {
			fz := (float32(k)+z0)*sz + n.oz
			flz := floor32(fz)
			zi := int32(uint8(int32(flz)))
			zf := fz - flz
			zs := smooth(zf)
			for j := range ny {
				fy := (float32(j)+y0)*sy + n.oy
				fly := floor32(fy)
				yi := int32(uint8(int32(fly)))
				yf := fy - fly
				compute := true
				var yUse float32
				if startCell != int32(fy) {
					if yi == lastCell {
						compute = false
					} else {
						yUse = yf - floor32(yf/sy)*sy
					}
				} else {
					yUse = startFrac
				}
				if compute {
					a := n.p[xi] + yi
					aa, ab := n.p[a], n.p[a+1]
					b := n.p[xi+1] + yi
					ba, bb := n.p[b], n.p[b+1]
					d0 := grad(int(n.p[aa+zi]), xf, yUse, zf)
					d1 := grad(int(n.p[ba+zi]), xf-1, yUse, zf)
					d2 := grad(int(n.p[ab+zi]), xf, yUse-1, zf)
					d3 := grad(int(n.p[bb+zi]), xf-1, yUse-1, zf)
					d4 := grad(int(n.p[aa+zi+1]), xf, yUse, zf-1)
					d5 := grad(int(n.p[ba+zi+1]), xf-1, yUse, zf-1)
					d6 := grad(int(n.p[ab+zi+1]), xf, yUse-1, zf-1)
					d7 := grad(int(n.p[bb+zi+1]), xf-1, yUse-1, zf-1)
					cache = [4]float32{lerp(xs, d0, d1), lerp(xs, d2, d3), lerp(xs, d4, d5), lerp(xs, d6, d7)}
					lastCell = yi
				}
				ys := smooth(yf)
				l0 := lerp(ys, cache[0], cache[1])
				l1 := lerp(ys, cache[2], cache[3])
				out[idx] += lerp(zs, l0, l1) * invAmp
				idx++
			}
		}
	}
}

// Blended is the terrain's base 3D noise: a selector noise blends two limit
// noises. It is sampled on a whole density grid per chunk. It is immutable
// after creation and safe for concurrent use.
type Blended struct {
	limitA, limitB, selector []legacyOctave
}

// NewBlended draws 16 octaves of the first limit noise, 16 of the second,
// then 8 of the selector, in that order, from r ("minecraft:terrain").
func NewBlended(r *rand.Xoroshiro) *Blended {
	b := &Blended{}
	for _, set := range []*[]legacyOctave{&b.limitA, &b.limitB, &b.selector} {
		n := 16
		if set == &b.selector {
			n = 8
		}
		*set = make([]legacyOctave, n)
		for i := range *set {
			(*set)[i] = newLegacyOctave(r)
		}
	}
	return b
}

// Selector scales, per octave amplitude 1.
const (
	selectorScaleXZ = 8.55515
	selectorScaleY  = 4.277575
	limitScale      = 684.412
)

func addOctaves(octs []legacyOctave, out *GridValues, x0, y0, z0, sxz, sy float32) {
	a := float32(1)
	for i := range octs {
		octs[i].addGrid(out[:], GridXZ, GridY, x0, y0, z0, sxz*a, sy*a, sxz*a, 1/a)
		a *= 0.5
	}
}

// Grid fills out with the blended value at every corner of the grid whose
// first corner is (x0, y0, z0) in cell units (4 blocks across, 8 tall).
func (b *Blended) Grid(out *GridValues, x0, y0, z0 float32) {
	var sel, lo, hi GridValues
	addOctaves(b.selector, &sel, x0, y0, z0, selectorScaleXZ, selectorScaleY)
	addOctaves(b.limitA, &lo, x0, y0, z0, limitScale, limitScale)
	addOctaves(b.limitB, &hi, x0, y0, z0, limitScale, limitScale)
	for i := range out {
		v := lo[i] * 0.001953125
		t := (sel[i]/10 + 1) * 0.5
		if t >= 0 {
			if t <= 1 {
				v += (hi[i]*0.001953125 - v) * t
			} else {
				v = hi[i] * 0.001953125
			}
		}
		out[i] = v
	}
}

// LegacyOctaves is a sum of old-style Perlin octaves, sampled on a whole
// grid of GridXZ x ny x GridXZ corners at once, as the Nether's and the
// End's terrain noises are. It is immutable after creation and safe for
// concurrent use.
type LegacyOctaves struct {
	octs  []legacyOctave
	scale float32
}

// NewLegacyOctaves draws n octaves from r, in order (BDS's sub_D241B00).
func NewLegacyOctaves(r Random, n int) *LegacyOctaves {
	o := &LegacyOctaves{octs: make([]legacyOctave, n)}
	var sum float32
	a := float32(1)
	for i := range o.octs {
		o.octs[i] = newLegacyOctave(r)
		sum += 1 / a
		a *= 0.5
	}
	if sum != 0 {
		o.scale = 1 / sum
	}
	return o
}

// Scale is 1 over the octaves' summed weights, which callers multiply a
// sample by to bring it to about [-1, 1].
func (o *LegacyOctaves) Scale() float32 { return o.scale }

// AddGrid adds the octave sum to out, a grid of GridXZ x ny x GridXZ
// corners, index (x*GridXZ+z)*ny + y, whose first corner is (x0, y0, z0)
// in cell units. Octave i is sampled at input scale s*2^-i and weighted
// 2^i (BDS's sub_D23EF10 per octave).
func (o *LegacyOctaves) AddGrid(out []float32, ny int, x0, y0, z0, sxz, sy float32) {
	o.AddGridN(out, GridXZ, ny, x0, y0, z0, sxz, sy)
}

// AddGridN is AddGrid on a grid nxz corners wide: the End's is 3.
func (o *LegacyOctaves) AddGridN(out []float32, nxz, ny int, x0, y0, z0, sxz, sy float32) {
	a := float32(1)
	for i := range o.octs {
		o.octs[i].addGrid(out, nxz, ny, x0, y0, z0, sxz*a, sy*a, sxz*a, 1/a)
		a *= 0.5
	}
}

// Sample returns the octave sum at one point: octave i at input scale 2^-i,
// weighted 2^i (BDS's sub_D242930). The caller applies the set's scale.
func (o *LegacyOctaves) Sample(x, y, z float32) float32 {
	var v float32
	a := float32(1)
	for i := range o.octs {
		v += o.octs[i].sample(x*a, y*a, z*a) / a
		a *= 0.5
	}
	return v
}

// sample is one octave at one point, lerped along x, then y, then z.
func (n *legacyOctave) sample(x, y, z float32) float32 {
	x, y, z = x+n.ox, y+n.oy, z+n.oz
	flx, fly, flz := floor32(x), floor32(y), floor32(z)
	xi, yi, zi := int32(uint8(int32(flx))), int32(uint8(int32(fly))), int32(uint8(int32(flz)))
	xf, yf, zf := x-flx, y-fly, z-flz
	xs, ys, zs := smooth(xf), smooth(yf), smooth(zf)
	a := n.p[xi] + yi
	aa, ab := n.p[a], n.p[a+1]
	b := n.p[xi+1] + yi
	ba, bb := n.p[b], n.p[b+1]
	l0 := lerp(ys, lerp(xs, grad(int(n.p[aa+zi]), xf, yf, zf), grad(int(n.p[ba+zi]), xf-1, yf, zf)),
		lerp(xs, grad(int(n.p[ab+zi]), xf, yf-1, zf), grad(int(n.p[bb+zi]), xf-1, yf-1, zf)))
	l1 := lerp(ys, lerp(xs, grad(int(n.p[aa+zi+1]), xf, yf, zf-1), grad(int(n.p[ba+zi+1]), xf-1, yf, zf-1)),
		lerp(xs, grad(int(n.p[ab+zi+1]), xf, yf-1, zf-1), grad(int(n.p[bb+zi+1]), xf-1, yf-1, zf-1)))
	return lerp(zs, l0, l1)
}
