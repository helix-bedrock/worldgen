package end

import (
	"math"
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
)

// slide is the factor and offset BDS applies to a corner at each grid
// height, as stored in the binary: the bottom eight corners slide toward
// -30, the top eighteen toward -3000.
var slide = func() (s [gridY][2]float32) {
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
}()

// sliding reports whether grid height j is slid.
func sliding(j int) bool { return j < 8 || j >= 15 }

// islandHeight is the height the islands add at grid column (gx, gz) of
// chunk (cx, cz), in grid cells of 8 blocks: the main island, a cone around
// the origin, or the highest of the outer islands within 12 chunks. An
// outer island stands on every chunk more than 64 chunks out where the
// island noise is below -0.9.
func (e *End) islandHeight(cx, cz, gx, gz int32) float32 {
	x, z := float32(gx+2*cx), float32(gz+2*cz)
	h := clampIsland(float32(sqrt32(float32(x*x)+float32(z*z))*-8) + 100)
	for dx := int32(-12); dx <= 12; dx++ {
		ix := cx + dx
		ddx := float32(gx - 2*dx)
		for dz := int32(-12); dz <= 12; dz++ {
			iz := cz + dz
			if int64(ix)*int64(ix)+int64(iz)*int64(iz) <= 4096 {
				continue
			}
			if e.island.Sample2(float32(ix), float32(iz)) >= -0.9 {
				continue
			}
			size := float32(int32((uint32(3439*abs32(ix))+uint32(147*abs32(iz)))%13 + 9))
			ddz := float32(gz - 2*dz)
			h = max(clampIsland(100-float32(sqrt32(float32(ddz*ddz)+float32(ddx*ddx))*size)), h)
		}
	}
	return h
}

// ChunkIslandHeight is the island height at the middle of chunk (cx, cz),
// as the End's decoration reads it (sub_5196720).
func (e *End) ChunkIslandHeight(cx, cz int32) float32 { return e.islandHeight(cx, cz, 1, 1) }

func clampIsland(v float32) float32 {
	if v > 80 {
		return 80
	}
	return max(v, -100)
}

func sqrt32(v float32) float32 { return float32(math.Sqrt(float64(v))) }

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
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
				l, u := float32(lo[i]*0.001953125), float32(hi[i]*0.001953125)
				t := (sel[i]/10 + 1) * 0.5
				v := l
				if t >= 0 {
					if t <= 1 {
						v = float32((u-l)*t) + l
					} else {
						v = u
					}
				}
				v = float32(v+-8) + h
				if sliding(j) {
					v = float32(v*slide[j][0]) + slide[j][1]
				}
				d[i] = v
			}
		}
	}
	at := func(gx, gz, j int) float32 { return d[(gx*gridXZ+gz)*gridY+j] }
	for gx := range gridXZ - 1 {
		for gz := range gridXZ - 1 {
			for j := range gridY - 1 {
				c00, c01 := at(gx, gz, j), at(gx, gz+1, j)
				c10, c11 := at(gx+1, gz, j), at(gx+1, gz+1, j)
				u00, u01 := at(gx, gz, j+1), at(gx, gz+1, j+1)
				u10, u11 := at(gx+1, gz, j+1), at(gx+1, gz+1, j+1)
				dz0, dz1 := c01-c00, c11-c10
				uz0, uz1 := u01-u00, u11-u10
				for zi := range 8 {
					tz := float32(zi) * 0.125
					a := float32(dz0*tz) + c00
					b := float32(float32(dz1*tz)+c10) - a
					au := float32(uz0*tz) + u00
					bu := float32(float32(tz*uz1)+u10) - au
					for xi := range 8 {
						tx := float32(xi) * 0.125
						lo := float32(b*tx) + a
						dy := float32(float32(tx*bu)+au) - lo
						for yi, f := range [4]float32{0, 0.25, 0.5, 0.75} {
							if float32(dy*f)+lo > 0 {
								c.set(gx*8+xi, j*4+yi, gz*8+zi, e.ids.endStone)
							}
						}
					}
				}
			}
		}
	}
}
