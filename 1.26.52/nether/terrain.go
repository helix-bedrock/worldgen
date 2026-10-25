package nether

import (
	"math"

	"github.com/helix-bedrock/worldgen/1.26.52/noise"
)

// The terrain is the old 3D-noise generator: a selector noise blends two
// limit noises on a grid of 5 x 17 x 5 corners, 4 blocks apart across and 8
// tall, minus a height falloff that closes the floor and the roof (BDS's
// sub_5161D20).
const (
	gridY = 17
	// Input scales of the selector and the limits, per octave amplitude 1.
	selectorXZ = 8.55515
	selectorY  = 34.220596
	limitXZ    = 684.412
	limitY     = 2053.2358
	// limitScale brings a limit noise to density units: 1/512.
	limitScale = 0.001953125
)

// falloff is subtracted from every corner at grid height j: a cosine wave
// over the height, pulled down hard within four corners of the floor and
// the roof.
func falloff() (f [gridY]float32) {
	for j := range gridY {
		d := float32(j)
		if j >= 9 {
			d = 16 - d
		}
		// BDS's 65536-entry sine table, read a quarter turn on: a cosine.
		a := float32(float32(float32(float32(float32(j)*3.1415927)*6)/17) * sinTurn)
		cos := float32(math.Sin(float64(uint16(int32(a+16384))) * (2 * math.Pi / 65536)))
		v := cos + cos
		if d < 4 {
			v += (4 - d) * (4 - d) * (4 - d) * -10
		}
		f[j] = v
	}
	return f
}

// terrain fills c with netherrack where the density is positive, lava
// below the sea level and air above.
func (n *Nether) terrain(chunkX, chunkZ int32, c *Chunk) {
	var sel, lo, hi, d [densitySize]float32
	fall := falloff()
	x0, z0 := float32(chunkX*4), float32(chunkZ*4)
	n.selector.AddGrid(sel[:], gridY, x0, 0, z0, selectorXZ, selectorY)
	n.minLimit.AddGrid(lo[:], gridY, x0, 0, z0, limitXZ, limitY)
	n.maxLimit.AddGrid(hi[:], gridY, x0, 0, z0, limitXZ, limitY)
	for i := range d {
		j := i % gridY
		if j == gridY-1 {
			// The top corner is always -10 (its value times 0, minus 10).
			d[i] = -10
			continue
		}
		v := lo[i] * limitScale
		t := (sel[i]/10 + 1) * 0.5
		if t >= 0 {
			if t <= 1 {
				v += (hi[i]*0.001953125 - v) * t
			} else {
				v = hi[i] * limitScale
			}
		}
		v -= fall[j]
		// The two corners below the top slide toward -10.
		switch j {
		case gridY - 3:
			v = v*0.6666667 + -3.3333335
		case gridY - 2:
			v = v*0.33333334 + -6.666667
		}
		d[i] = v
	}
	for i := range noise.GridXZ - 1 {
		for k := range noise.GridXZ - 1 {
			for j := range gridY - 1 {
				for dx := range 4 {
					tx := 0.25 * float32(dx)
					for dz := range 4 {
						tz := 0.25 * float32(dz)
						z00 := lerp(tz, densityAt(&d, i, j, k), densityAt(&d, i, j, k+1))
						z10 := lerp(tz, densityAt(&d, i+1, j, k), densityAt(&d, i+1, j, k+1))
						z01 := lerp(tz, densityAt(&d, i, j+1, k), densityAt(&d, i, j+1, k+1))
						z11 := lerp(tz, densityAt(&d, i+1, j+1, k), densityAt(&d, i+1, j+1, k+1))
						lo, hi := lerp(tx, z00, z10), lerp(tx, z01, z11)
						for dy := range 8 {
							y := j*8 + dy
							b := n.ids.lava
							if y >= SeaLevel {
								b = n.ids.air
							}
							if lerp(0.125*float32(dy), lo, hi) > 0 {
								b = n.ids.netherrack
							}
							c.set(i*4+dx, y, k*4+dz, b)
						}
					}
				}
			}
		}
	}
}

// densitySize is the number of density corners in a chunk.
const densitySize = noise.GridXZ * gridY * noise.GridXZ

// densityAt returns the density corner (i, j, k) of d.
func densityAt(d *[densitySize]float32, i, j, k int) float32 {
	return d[(i*noise.GridXZ+k)*gridY+j]
}

// lerp blends a toward b by t.
func lerp(t, a, b float32) float32 { return a + t*(b-a) }
