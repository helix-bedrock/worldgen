package worldgen

import (
	"github.com/helix-bedrock/worldgen/noise"
	"github.com/helix-bedrock/worldgen/rand"
)

// The terrain fill, copied from BDS (OverworldGeneratorMultinoise slot 64 and
// its density grid): 5 x 41 x 5 corner samples per chunk, cells 4 blocks wide
// and 8 tall, interpolated per block. The grid covers the bottom 320 blocks
// of the overworld (y -64 to 255); everything above is air.
const (
	gridXZ     = 5
	fillHeight = 320
	gridY      = fillHeight/8 + 1
	cellXZ     = 4
	cellY      = 8
	minCellY   = MinY / cellY
	gridLen    = gridXZ * gridY * gridXZ
)

type densityGrid [gridLen]float32

func (g *densityGrid) at(i, j, k int) float32 {
	return g[(i*gridXZ+k)*gridY+j]
}

// Auxiliary grids sampled with the density: noodle caves and ore veins.
const (
	auxNoodle = iota
	auxNoodleThickness
	auxRidgeA
	auxRidgeB
	auxVeininess
	auxVeinA
	auxVeinB
	auxCount
)

// grids is everything the fill interpolates.
type grids struct {
	density densityGrid
	aux     [auxCount]densityGrid
}

// oreNoises are the noodle cave and ore vein noises.
type oreNoises struct {
	noodle, noodleThickness, ridgeA, ridgeB *noise.Normal
	veininess, veinA, veinB, gap            *noise.Normal
	pos                                     rand.Positional
}

func newOreNoises(world rand.Positional) oreNoises {
	n := func(name string, first int) *noise.Normal {
		r := world.FromHash("minecraft:" + name)
		return noise.NewNormal(&r, first, []float32{1})
	}
	r := world.FromHash("minecraft:ore")
	return oreNoises{
		noodle:          n("noodle", -8),
		noodleThickness: n("noodle_thickness", -8),
		ridgeA:          n("noodle_ridge_a", -7),
		ridgeB:          n("noodle_ridge_b", -7),
		veininess:       n("ore_veininess", -8),
		veinA:           n("ore_vein_a", -7),
		veinB:           n("ore_vein_b", -7),
		gap:             n("ore_gap", -5),
		pos:             r.Fork(),
	}
}

// fillDensity computes the density and the auxiliary values at each corner.
// Auxiliary values stay 1 where the density is below -150, as in BDS.
func (o *Overworld) fillDensity(chunkX, chunkZ int32, cols *[gridXZ * gridXZ]column, g *grids) {
	var base noise.GridValues
	o.blended.Grid(&base, float32(chunkX*4), minCellY, float32(chunkZ*4))
	for a := range g.aux {
		for i := range g.aux[a] {
			g.aux[a][i] = 1
		}
	}
	n := &o.ores
	for i := range gridXZ {
		cx := float32(chunkX*4 + int32(i))
		x := cx * cellXZ
		for k := range gridXZ {
			cz := float32(chunkZ*4 + int32(k))
			z := cz * cellXZ
			col := cols[i*gridXZ+k]
			for j := range gridY {
				cy := float32(j + minCellY)
				y := cy * cellY
				idx := (i*gridXZ+k)*gridY + j
				d := o.cornerDensity(cx, cy, cz, j, col, base[idx])
				g.density[idx] = d
				if d < -150 {
					continue
				}
				g.aux[auxVeininess][idx] = clamp1(n.veininess.Sample(x*1.5, y*1.5, z*1.5))
				g.aux[auxVeinA][idx] = clamp1(n.veinA.Sample(x*4, y*4, z*4))
				g.aux[auxVeinB][idx] = clamp1(n.veinB.Sample(x*4, y*4, z*4))
				g.aux[auxNoodle][idx] = clamp1(n.noodle.Sample(x, y, z))
				g.aux[auxNoodleThickness][idx] = clamp1(n.noodleThickness.Sample(x, y, z))
				rx, ry, rz := x*2.6666667, y*2.6666667, z*2.6666667
				g.aux[auxRidgeA][idx] = clamp1(n.ridgeA.Sample(rx, ry, rz))
				g.aux[auxRidgeB][idx] = clamp1(n.ridgeB.Sample(rx, ry, rz))
			}
		}
	}
}

func (o *Overworld) cornerDensity(cx, cy, cz float32, j int, col column, noise float32) float32 {
	term := ((cy*cellY)*-0.0078125 + 1 + col.offset + col.jag) * col.factor
	if term > 0 {
		term *= 4
	}
	d := term + noise
	d = o.caves.route(cx*cellXZ, cy*cellY, cz*cellXZ, d)

	// Top slide: the top two cells fade to -10.
	if s := float32(gridY-1-j) * 0.5; s < 0 {
		d = -10
	} else if s <= 1 {
		d = (d+10)*s - 10
	}
	// Bottom slide: the bottom three cells fade to +15.
	if t := float32(j) / 3; t <= 1 {
		d = (d-15)*t + 15
	}
	return d
}

func lerp(t, a, b float32) float32 { return a + t*(b-a) }

// interp interpolates grid g inside cell (i, j, k) at block offsets
// (dx, dy, dz), in BDS's order: along z, then x, then y.
func interp(g *densityGrid, i, j, k, dx, dy, dz int) float32 {
	tz := 0.25 * float32(dz)
	tx := 0.25 * float32(dx)
	ty := 0.125 * float32(dy)
	z00 := lerp(tz, g.at(i, j, k), g.at(i, j, k+1))
	z10 := lerp(tz, g.at(i+1, j, k), g.at(i+1, j, k+1))
	z01 := lerp(tz, g.at(i, j+1, k), g.at(i, j+1, k+1))
	z11 := lerp(tz, g.at(i+1, j+1, k), g.at(i+1, j+1, k+1))
	return lerp(ty, lerp(tx, z00, z10), lerp(tx, z01, z11))
}

// squeeze is the final density shaping; only its sign decides solid or not.
func squeeze(d float32) float32 {
	x := d * 0.005
	if 1 < x {
		x = 1
	} else {
		x = max(x, -1)
	}
	return x*0.5 + ((x*x)*x)/-24
}

// oreVein is one row of BDS's ore vein table.
type oreVein struct {
	ore, raw, filler uint32
	minY, maxY       int32
}

// fillBlocks interpolates the grids and places stone, deepslate, ore veins,
// aquifer fluids and air.
func (o *Overworld) fillBlocks(chunkX, chunkZ int32, g *grids, aq *aquifer, c *Chunk) {
	for i := fillHeight << 8; i < len(c.Blocks); i++ {
		c.Blocks[i] = uint16(o.ids.air)
	}
	for i := range gridXZ - 1 {
		for k := range gridXZ - 1 {
			for j := range gridY - 1 {
				for dz := range cellXZ {
					for dx := range cellXZ {
						for dy := range cellY {
							x, z := i*cellXZ+dx, k*cellXZ+dz
							y := MinY + j*cellY + dy
							c.set(x, y, z, o.fillBlock(g, aq, i, j, k, dx, dy, dz,
								chunkX*16+int32(x), int32(y), chunkZ*16+int32(z)))
						}
					}
				}
			}
		}
	}
}

func (o *Overworld) fillBlock(g *grids, aq *aquifer, i, j, k, dx, dy, dz int, x, y, z int32) uint32 {
	d := squeeze(interp(&g.density, i, j, k, dx, dy, dz))
	if y >= MinY+4 && d > 0 && interp(&g.aux[auxNoodle], i, j, k, dx, dy, dz) >= 0 {
		t := (interp(&g.aux[auxNoodleThickness], i, j, k, dx, dy, dz) + 1) * 0.5
		th := float32(0.05)
		if t >= 0 {
			th = 0.1
			if t <= 1 {
				th = t*0.05 + 0.05
			}
		}
		a := abs32(interp(&g.aux[auxRidgeA], i, j, k, dx, dy, dz)*1.5) - th
		b := abs32(interp(&g.aux[auxRidgeB], i, j, k, dx, dy, dz)*1.5) - th
		d = min(max(b, a), d)
	}
	if !(d > 0) {
		aq.sample(x, y, z)
		if !(d+aq.density > 0) {
			if y < aq.level {
				if aq.isLava {
					return o.ids.lava
				}
				return o.ids.water
			}
			return o.ids.air
		}
	}
	block := o.stoneAt(x, y, z)
	return o.oreVein(g, i, j, k, dx, dy, dz, x, y, z, block)
}

// oreVein places copper or iron veins in solid ground.
func (o *Overworld) oreVein(g *grids, i, j, k, dx, dy, dz int, x, y, z int32, block uint32) uint32 {
	v := interp(&g.aux[auxVeininess], i, j, k, dx, dy, dz)
	vein := &o.ids.veins[0]
	if v <= 0 {
		vein = &o.ids.veins[1]
	}
	fy := float32(y)
	lo := fy - float32(vein.minY)
	if lo < 0 {
		return block
	}
	hi := float32(vein.maxY) - fy
	if hi < 0 {
		return block
	}
	e := min(hi, lo) / 20
	var edge float32
	if !(1 < e) {
		edge = e*0.2 + -0.2
	}
	av := abs32(v)
	if !(edge+av >= 0.4) {
		return block
	}
	r := o.ores.pos.At(x, y, z)
	if !(r.Float() <= 0.7) {
		return block
	}
	a := abs32(interp(&g.aux[auxVeinA], i, j, k, dx, dy, dz)) + -0.08
	b := abs32(interp(&g.aux[auxVeinB], i, j, k, dx, dy, dz)) + -0.08
	if !(max(a, b) < 0) {
		return block
	}
	r2 := r.Float()
	rich := (av + -0.4) / 0.20000002
	richness := float32(0.1)
	if rich >= 0 {
		richness = 0.3
		if rich <= 1 {
			richness = rich*0.20000002 + 0.1
		}
	}
	gap := o.ores.gap.Sample(float32(x), fy, float32(z))
	if richness > r2 && gap >= -1 && min(1, gap) > -0.3 {
		if r.Float() < 0.02 {
			return vein.raw
		}
		return vein.ore
	}
	return vein.filler
}

// stoneAt is deepslate below y 0, stone from y 8, and a random mix between,
// deepslate with chance 1 - y/8.
func (o *Overworld) stoneAt(x, y, z int32) uint32 {
	if y < 0 {
		return o.ids.deepslate
	}
	if y > 8 {
		return o.ids.stone
	}
	r := o.deepRand.At(x, y, z)
	if 1-float32(y)*0.125 <= r.Float() {
		return o.ids.stone
	}
	return o.ids.deepslate
}

// DensityColumn returns the interpolated density of the block column at
// world (x, z), index y - MinY, before the squeeze and caves. It is a
// debugging aid for comparing terrain with BDS.
func (o *Overworld) DensityColumn(x, z int32) []float32 {
	chunkX, chunkZ := x>>4, z>>4
	var cols [gridXZ * gridXZ]column
	for i := range gridXZ {
		for k := range gridXZ {
			cols[i*gridXZ+k] = o.column(chunkX*4+int32(i), chunkZ*4+int32(k))
		}
	}
	g := new(grids)
	o.fillDensity(chunkX, chunkZ, &cols, g)
	lx, lz := int(x&15), int(z&15)
	out := make([]float32, Height)
	for j := range gridY - 1 {
		for dy := range cellY {
			out[j*cellY+dy] = interp(&g.density, lx/cellXZ, j, lz/cellXZ, lx%cellXZ, dy, lz%cellXZ)
		}
	}
	return out
}
