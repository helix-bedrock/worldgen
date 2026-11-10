package end

import (
	"math"

	"github.com/helix-bedrock/worldgen/internal/mathx"
)

// The terrain is the old 3D-noise generator: a selector noise blends two
// limit noises on a grid of 3 x 33 x 3 corners, 8 blocks apart across and
// 4 tall, plus the island height of the column, slid down to nothing at
// the bottom and the top (BDS's sub_5195000, filled in by sub_5195960).
const (
	gridXZ = 3
	gridY  = 33
	// Input scales of the selector and the limits, per octave amplitude 1.
	selectorXZ = 17.1103
	selectorY  = 4.277575
	limitXZ    = 1368.824
	limitY     = 684.412
	// limitScale brings the limit noises down to density units (1/512).
	limitScale = 0.001953125
	// densityOffset is added to every corner before the island height.
	densityOffset = -8

	// cellXZ and cellY are the blocks between grid corners.
	cellXZ = 8
	cellY  = 4

	// slideBottom and slideTop bound the grid heights left unslid.
	slideBottom = 8
	slideTop    = 15
)

// The islands (sub_5196720): the main island is a cone around the origin;
// outer islands stand on chunks more than 64 chunks out where the island
// noise is below outerIslandNoise.
const (
	islandMax        = 80
	islandMin        = -100
	islandPeak       = 100
	mainIslandSlope  = -8
	outerIslandReach = 12
	// outerIslandChunks2 is 64 chunks, squared.
	outerIslandChunks2 = 64 * 64
	outerIslandNoise   = -0.9
	// An outer island's slope is (sizeMulX*|x| + sizeMulZ*|z|) % sizeRange
	// + sizeMin, with x and z its chunk.
	sizeMulX  = 3439
	sizeMulZ  = 147
	sizeRange = 13
	sizeMin   = 9
)

// slide is the factor and offset BDS applies to a corner at each grid
// height, as stored in the binary: the bottom eight corners slide toward
// -30, the top eighteen toward -3000.
var slide = slideTable()

func slideTable() (s [gridY][2]float32) {
	f := math.Float32frombits
	for j := range s {
		s[j] = [2]float32{1, 0}
	}
	bits := map[int][2]uint32{
		0: {0xbe124928, 0xc2092493}, 1: {0, 0xc1f00000}, 2: {0x3e124924, 0xc1cdb6dc}, 3: {0x3e924924, 0xc1ab6db7},
		4: {0x3edb6db6, 0xc1892493}, 5: {0x3f124924, 0xc14db6dc}, 6: {0x3f36db6e, 0xc1092493}, 7: {0x3f5b6db7, 0xc0892493},
		15: {0x3f7c0000, 0xc23b8000},
		16: {0x3f780000, 0xc2bb8000}, 17: {0x3f740000, 0xc30ca000}, 18: {0x3f700000, 0xc33b8000}, 19: {0x3f6c0000, 0xc36a6000},
		20: {0x3f680000, 0xc38ca000}, 21: {0x3f640000, 0xc3a41000}, 22: {0x3f600000, 0xc3bb8000}, 23: {0x3f5c0000, 0xc3d2f000},
		24: {0x3f580000, 0xc3ea6000}, 25: {0x3f540000, 0xc400e800}, 26: {0x3f500000, 0xc40ca000}, 27: {0x3f4c0000, 0xc4185800},
		28: {0x3f480000, 0xc4241000}, 29: {0x3f440000, 0xc42fc800}, 30: {0x3f400000, 0xc43b8000}, 31: {0x3f3c0000, 0xc4473800},
		32: {0x3f380000, 0xc452f000},
	}
	for j, b := range bits {
		s[j] = [2]float32{f(b[0]), f(b[1])}
	}
	return s
}

// sliding reports whether grid height j is slid.
func sliding(j int) bool { return j < slideBottom || j >= slideTop }

// islandHeight is the height the islands add at grid column (gx, gz) of
// chunk (cx, cz), in grid cells of 8 blocks: the main island, a cone around
// the origin, or the highest of the outer islands within 12 chunks. An
// outer island stands on every chunk more than 64 chunks out where the
// island noise is below -0.9.
func (e *End) islandHeight(cx, cz, gx, gz int32) float32 {
	x, z := float32(gx+2*cx), float32(gz+2*cz)
	h := clampIsland(float32(mathx.Sqrt32(float32(x*x)+float32(z*z))*mainIslandSlope) + islandPeak)
	for dx := int32(-outerIslandReach); dx <= outerIslandReach; dx++ {
		ix := cx + dx
		ddx := float32(gx - 2*dx)
		for dz := int32(-outerIslandReach); dz <= outerIslandReach; dz++ {
			iz := cz + dz
			if int64(ix)*int64(ix)+int64(iz)*int64(iz) <= outerIslandChunks2 {
				continue
			}
			if e.island.Sample2(float32(ix), float32(iz)) >= outerIslandNoise {
				continue
			}
			size := float32(int32((uint32(sizeMulX*mathx.Abs(ix))+uint32(sizeMulZ*mathx.Abs(iz)))%sizeRange + sizeMin))
			ddz := float32(gz - 2*dz)
			h = max(clampIsland(islandPeak-float32(mathx.Sqrt32(float32(ddz*ddz)+float32(ddx*ddx))*size)), h)
		}
	}
	return h
}

// ChunkIslandHeight is the island height at the middle of chunk (cx, cz),
// as the End's decoration reads it (sub_5196720).
func (e *End) ChunkIslandHeight(cx, cz int32) float32 { return e.islandHeight(cx, cz, 1, 1) }

func clampIsland(v float32) float32 {
	if v > islandMax {
		return islandMax
	}
	return max(v, islandMin)
}

// terrain fills c with end stone where the density is positive. Every
// product is converted to float32 on its own so that no compiler fuses it
// into the next addition: BDS rounds each step.
func (e *End) terrain(cx, cz int32, c *Chunk) {
	const size = gridXZ * gridY * gridXZ
	var sel, lo, hi, d [size]float32
	x0, z0 := float32(2*cx), float32(2*cz)
	e.selector.AddGridN(sel[:], gridXZ, gridY, x0, 0, z0, selectorXZ, selectorY)
	e.minLimit.AddGridN(lo[:], gridXZ, gridY, x0, 0, z0, limitXZ, limitY)
	e.maxLimit.AddGridN(hi[:], gridXZ, gridY, x0, 0, z0, limitXZ, limitY)
	for gx := range int32(gridXZ) {
		for gz := range int32(gridXZ) {
			h := e.islandHeight(cx, cz, gx, gz)
			base := int(gx*gridXZ+gz) * gridY
			for j := range gridY {
				i := base + j
				l, u := float32(lo[i]*limitScale), float32(hi[i]*limitScale)
				t := (sel[i]/10 + 1) * 0.5
				v := l
				if t >= 0 {
					if t <= 1 {
						v = float32((u-l)*t) + l
					} else {
						v = u
					}
				}
				v = float32(v+densityOffset) + h
				if sliding(j) {
					v = float32(v*slide[j][0]) + slide[j][1]
				}
				d[i] = v
			}
		}
	}
	e.fill(&d, c)
}

// corner is the density at grid corner (gx, j, gz).
func corner(d *[gridXZ * gridY * gridXZ]float32, gx, gz, j int) float32 {
	return d[(gx*gridXZ+gz)*gridY+j]
}

// fill lerps the grid densities d across each cell and sets end stone
// where the result is positive.
func (e *End) fill(d *[gridXZ * gridY * gridXZ]float32, c *Chunk) {
	for gx := range gridXZ - 1 {
		for gz := range gridXZ - 1 {
			for j := range gridY - 1 {
				c00, c01 := corner(d, gx, gz, j), corner(d, gx, gz+1, j)
				c10, c11 := corner(d, gx+1, gz, j), corner(d, gx+1, gz+1, j)
				u00, u01 := corner(d, gx, gz, j+1), corner(d, gx, gz+1, j+1)
				u10, u11 := corner(d, gx+1, gz, j+1), corner(d, gx+1, gz+1, j+1)
				dz0, dz1 := c01-c00, c11-c10
				uz0, uz1 := u01-u00, u11-u10
				for zi := range cellXZ {
					tz := float32(zi) / cellXZ
					a := float32(dz0*tz) + c00
					b := float32(float32(dz1*tz)+c10) - a
					au := float32(uz0*tz) + u00
					bu := float32(float32(tz*uz1)+u10) - au
					for xi := range cellXZ {
						tx := float32(xi) / cellXZ
						lo := float32(b*tx) + a
						dy := float32(float32(tx*bu)+au) - lo
						for yi := range cellY {
							if float32(dy*(float32(yi)/cellY))+lo > 0 {
								c.set(gx*cellXZ+xi, j*cellY+yi, gz*cellXZ+zi, e.ids.endStone)
							}
						}
					}
				}
			}
		}
	}
}
