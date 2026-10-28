// Package noise implements the noise functions of vanilla Bedrock world
// generation. All maths is float32, as in vanilla: doing it in float64 drifts
// away from vanilla terrain.
package noise

import (
	"math"

	"github.com/helix-bedrock/worldgen/1.26.60/rand"
)

// gradients are the 16 gradient directions (x, y, z). The last four repeat
// earlier ones so that a 4-bit index can be used.
var gradients = [16][3]float32{
	{1, 1, 0}, {-1, 1, 0}, {1, -1, 0}, {-1, -1, 0},
	{1, 0, 1}, {-1, 0, 1}, {1, 0, -1}, {-1, 0, -1},
	{0, 1, 1}, {0, -1, 1}, {0, 1, -1}, {0, -1, -1},
	{1, 1, 0}, {0, -1, 1}, {-1, 1, 0}, {0, -1, -1},
}

// Perlin is one octave of improved Perlin noise with a random origin and
// permutation. It is immutable after creation and safe for concurrent use.
type Perlin struct {
	ox, oy, oz float32
	p          [256]uint8
}

// NewPerlin draws the origin and permutation from r.
func NewPerlin(r *rand.Xoroshiro) *Perlin {
	n := &Perlin{}
	n.ox = float32(r.Double() * 256)
	n.oy = float32(r.Double() * 256)
	n.oz = float32(r.Double() * 256)
	for i := range 256 {
		n.p[i] = uint8(i)
	}
	for i := range 256 {
		j := i + int(r.IntN(int32(256-i)))
		if j > 255 {
			j = 255
		}
		n.p[i], n.p[j] = n.p[j], n.p[i]
	}
	return n
}

func (n *Perlin) perm(i int) int {
	return int(n.p[i&255])
}

func floor32(v float32) float32 {
	return float32(math.Floor(float64(v)))
}

func smooth(t float32) float32 {
	return t * t * t * (t*(t*6-15) + 10)
}

func lerp(t, a, b float32) float32 {
	return a + t*(b-a)
}

func grad(h int, x, y, z float32) float32 {
	g := gradients[h&15]
	return g[0]*x + g[1]*y + g[2]*z
}

// Sample returns the noise at (x, y, z), roughly in [-1, 1].
func (n *Perlin) Sample(x, y, z float32) float32 {
	x += n.ox
	y += n.oy
	z += n.oz
	fx, fy, fz := floor32(x), floor32(y), floor32(z)
	ix, iy, iz := int(fx), int(fy), int(fz)
	dx, dy, dz := x-fx, y-fy, z-fz

	a := n.perm(ix)
	b := n.perm(ix + 1)
	aa := n.perm(a + iy)
	ab := n.perm(a + iy + 1)
	ba := n.perm(b + iy)
	bb := n.perm(b + iy + 1)

	d0 := grad(n.perm(aa+iz), dx, dy, dz)
	d1 := grad(n.perm(ba+iz), dx-1, dy, dz)
	d2 := grad(n.perm(ab+iz), dx, dy-1, dz)
	d3 := grad(n.perm(bb+iz), dx-1, dy-1, dz)
	d4 := grad(n.perm(aa+iz+1), dx, dy, dz-1)
	d5 := grad(n.perm(ba+iz+1), dx-1, dy, dz-1)
	d6 := grad(n.perm(ab+iz+1), dx, dy-1, dz-1)
	d7 := grad(n.perm(bb+iz+1), dx-1, dy-1, dz-1)

	sx, sy, sz := smooth(dx), smooth(dy), smooth(dz)
	l0 := lerp(sy, lerp(sx, d0, d1), lerp(sx, d2, d3))
	l1 := lerp(sy, lerp(sx, d4, d5), lerp(sx, d6, d7))
	return lerp(sz, l0, l1)
}
