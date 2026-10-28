// Package feature holds feature shapes shared by the dimensions' generators.
// Each shape draws from the caller's random exactly as BDS does and reports
// the blocks it covers; the caller decides what to place there.
package feature

import (
	"math"

	"github.com/helix-bedrock/worldgen/1.26.60/rand"
)

// sinTable is the 65536-entry sine table BDS uses for angles. It is filled
// once and only read.
var sinTable = buildSinTable()

// sinTableSize is the number of entries in BDS's sine table.
const sinTableSize = 65536

// buildSinTable fills the sine table, one entry per 1/65536 of a turn.
func buildSinTable() (t [sinTableSize]float32) {
	for i := range t {
		t[i] = float32(math.Sin(float64(i) * math.Pi * 2 / sinTableSize))
	}
	return
}

// SinTable returns BDS's sine table, indexed by angle*65536/(2*pi) wrapped
// to 16 bits. Callers must not modify it.
func SinTable() *[65536]float32 { return &sinTable }

func sinIndex(v float32) float32 { return sinTable[uint16(int32(v))] }

type oreSphere struct{ x, y, z, r2, r float32 }

// OreVein draws the vein of minecraft:ore_feature (BDS OreFeature::place,
// sub_D2F38D0): count spheres along a short line through (x+8, y, z+8). It
// calls visit for every block inside a sphere, in BDS's order, x outer, then
// z, then y. Draw order: angle, start y, end y, then one radius per sphere;
// visit may draw too (air-exposure rolls), as BDS does while placing. It
// returns false when every sphere was dropped.
func OreVein(count int32, x, y, z int32, r *rand.MT, visit func(bx, by, bz int32)) bool {
	countF := float32(count)
	a := r.Float() * 3.1415927 * 10430.378
	sinA := sinTable[uint16(int32(a))]
	cosA := sinTable[uint16(int32(a+16384))]
	s := countF * 0.125
	sx := sinA * s
	cz := s * cosA
	startZ := (cz + 8) + float32(z)
	startX := (sx + 8) + float32(x)
	startY := (float32(r.IntN(3)) + -2) + float32(y)
	endZ := float32(z) + (8 - cz)
	endX := (8 - sx) + float32(x)
	endY := (float32(r.IntN(3)) + -2) + float32(y)
	dx, dy, dz := endX-startX, endY-startY, endZ-startZ

	inv := 1 / countF
	invPi := inv * 3.1415927
	var sphereBuf [64]oreSphere
	spheres := sphereBuf[:0]
	if int(count) > len(sphereBuf) {
		spheres = make([]oreSphere, 0, count)
	}
	spheres = spheres[:count]
	for i := range spheres {
		fi := float32(i)
		sp := &spheres[i]
		sp.x = inv*(fi*dx) + startX
		sp.y = inv*(fi*dy) + startY
		sp.z = (dz*fi)*inv + startZ
		nf := r.Float()
		sv := sinIndex((fi * invPi) * 10430.378)
		rr := ((sv+1)*((nf*countF)*0.0625) + 1) * 0.5
		sp.r2 = rr * rr
		sp.r = float32(math.Sqrt(float64(sp.r2)))
	}
	// Drop spheres that a later, larger sphere contains. Filtering in place
	// is safe: only spheres after i are read once i is written.
	kept := spheres[:0]
	for i := range spheres {
		si := spheres[i]
		inside := false
		for j := i + 1; j < len(spheres); j++ {
			sj := spheres[j]
			ddz, ddx, ddy := sj.z-si.z, sj.x-si.x, sj.y-si.y
			d := float32(math.Sqrt(float64(ddz*ddz + (ddx*ddx + ddy*ddy))))
			if !(si.r > sj.r) && sj.r > d+si.r {
				inside = true
				break
			}
		}
		if !inside {
			kept = append(kept, si)
		}
	}
	if len(kept) == 0 {
		return false
	}
	minX, minY, minZ := int32(math.MaxInt32), int32(math.MaxInt32), int32(math.MaxInt32)
	maxX, maxY, maxZ := int32(math.MinInt32), int32(math.MinInt32), int32(math.MinInt32)
	for _, sp := range kept {
		minX = min(minX, int32(math.Floor(float64(sp.x-sp.r))))
		minY = min(minY, int32(math.Floor(float64(sp.y-sp.r))))
		minZ = min(minZ, int32(math.Floor(float64(sp.z-sp.r))))
		maxX = max(maxX, int32(math.Floor(float64(sp.x+sp.r))))
		maxY = max(maxY, int32(math.Floor(float64(sp.y+sp.r))))
		maxZ = max(maxZ, int32(math.Floor(float64(sp.z+sp.r))))
	}
	// A sphere can hold a block only if the block's x (then z) distance
	// alone is inside it: float sums of squares never shrink, so this
	// prefilter is exact and the blocks are visited in the same order.
	var slabBuf, colBuf [64]oreSphere
	fx := float32(minX) + 0.5
	for bx := minX; bx <= maxX; bx, fx = bx+1, fx+1 {
		slab := slabBuf[:0]
		for _, sp := range kept {
			if ddx := fx - sp.x; sp.r2 > ddx*ddx {
				slab = append(slab, sp)
			}
		}
		if len(slab) == 0 {
			continue
		}
		fz := float32(minZ) + 0.5
		for bz := minZ; bz <= maxZ; bz, fz = bz+1, fz+1 {
			col := colBuf[:0]
			for _, sp := range slab {
				if ddz := fz - sp.z; sp.r2 > ddz*ddz {
					col = append(col, sp)
				}
			}
			if len(col) == 0 {
				continue
			}
			fy := float32(minY) + 0.5
			for by := minY; by <= maxY; by, fy = by+1, fy+1 {
				for _, sp := range col {
					ddz, ddx, ddy := fz-sp.z, fx-sp.x, fy-sp.y
					if sp.r2 > ddz*ddz+(ddx*ddx+ddy*ddy) {
						visit(bx, by, bz)
						break
					}
				}
			}
		}
	}
	return true
}
