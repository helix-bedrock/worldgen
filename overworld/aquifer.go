package overworld

import (
	"github.com/helix-bedrock/worldgen/internal/mathx"
	"github.com/helix-bedrock/worldgen/noise"
	"github.com/helix-bedrock/worldgen/rand"
)

// Aquifers decide what fills the empty space of the terrain: water or lava at
// a per-cell fluid level, or air. Copied from BDS (sub_5158C60 for the block,
// sub_5159950 for a cell's fluid).

const (
	// noFluidLevel is the fluid level of a cell with no fluid.
	noFluidLevel = -4096
	// noSurface is BDS's preliminary surface level when no corner qualifies.
	noSurface = 0x7FFF
)

type aquiferNoises struct {
	barrier, flood, lava, spread *noise.Normal
	pos                          rand.Positional
}

func newAquiferNoises(world rand.Positional) aquiferNoises {
	r := world.FromHash("minecraft:aquifer")
	return aquiferNoises{
		barrier: namedNoise(world, "aquifer_barrier", -3, 1),
		flood:   namedNoise(world, "aquifer_fluid_level_floodedness", -7, 1),
		spread:  namedNoise(world, "aquifer_fluid_level_spread", -5, 1),
		lava:    namedNoise(world, "aquifer_lava", -1, 1),
		pos:     r.Fork(),
	}
}

// aquiferNeighbours are the chunk offsets whose preliminary surface a cell
// checks, in BDS's order.
func aquiferNeighbours() [13][2]int32 {
	return [13][2]int32{
		{-2, -1}, {-1, -1}, {0, -1}, {1, -1},
		{-3, 0}, {-2, 0}, {-1, 0}, {0, 0}, {1, 0},
		{-2, 1}, {-1, 1}, {0, 1}, {1, 1},
	}
}

type aquiferCenter struct {
	x, y, z int32
	made    bool // the centre is computed
	set     bool
	// Fluid status, filled on first use.
	level int32
	lava  bool
}

// aquifer is the per-chunk aquifer state: cached cell centres, cell fluids
// and preliminary surface levels. It is not safe for concurrent use.
type aquifer struct {
	o *Overworld
	// grid holds the cells a chunk's own blocks sample, x and z from
	// gridX/gridZ on, y from aquiferMinCellY; cells outside it go in cells.
	grid         [3 * aquiferCellsY * 3]aquiferCenter
	gridX, gridZ int32
	cells        map[[3]int32]*aquiferCenter
	// groups caches, per 2x3x2 block of cells a sample searches, the cells
	// and whether they all hold the same fluid.
	groups [2 * aquiferCellsY * 2]aquiferGroup
	prelim map[[2]int32]int32
	// Results of the last sample.
	level   int32
	isLava  bool
	density float32
}

// aquiferGroup is the 12 cells a block samples, in search order. When they
// all hold the same fluid the sample does not depend on which is nearest:
// every pressure term is 0, so the extra density is 0 and the level is
// theirs. That holds for all of the sky and most open water.
type aquiferGroup struct {
	made    bool
	uniform bool
	level   int32
	lava    bool
	cells   [12]*aquiferCenter
}

// group returns the cell group whose search starts at (cx, cy, cz), or nil
// outside the chunk's own range.
func (a *aquifer) group(cx, cy, cz int32) *aquiferGroup {
	gx, gy, gz := cx-a.gridX, cy-1-aquiferMinCellY, cz-a.gridZ
	if uint32(gx) >= 2 || uint32(gy) >= aquiferCellsY || uint32(gz) >= 2 {
		return nil
	}
	g := &a.groups[(gx*aquiferCellsY+gy)*2+gz]
	if g.made {
		return g
	}
	n := 0
	for dx := int32(0); dx <= 1; dx++ {
		for dy := int32(-1); dy <= 1; dy++ {
			for dz := int32(0); dz <= 1; dz++ {
				g.cells[n] = a.center(cx+dx, cy+dy, cz+dz)
				n++
			}
		}
	}
	g.level, g.lava = a.fluid(g.cells[0])
	g.uniform = true
	for _, c := range g.cells[1:] {
		if l, lava := a.fluid(c); l != g.level || lava != g.lava {
			g.uniform = false
			break
		}
	}
	g.made = true
	return g
}

// The cell y range blocks above y -55 sample: mathx.FloorDiv(y+1, 12) - 1 to
// mathx.FloorDiv(y+1, 12) + 1.
const (
	aquiferMinCellY = -6
	aquiferCellsY   = (MaxY+12)/12 + 1 - aquiferMinCellY
)

// newAquifer returns the aquifer state for chunk (chunkX, chunkZ).
func newAquifer(o *Overworld, chunkX, chunkZ int32) *aquifer {
	return &aquifer{o: o, gridX: chunkX - 1, gridZ: chunkZ - 1,
		cells: map[[3]int32]*aquiferCenter{}, prelim: map[[2]int32]int32{}}
}

// prelimAt is the preliminary surface level of quarter column (qx, qz).
func (a *aquifer) prelimAt(qx, qz int32) int32 {
	k := [2]int32{qx, qz}
	if v, ok := a.prelim[k]; ok {
		return v
	}
	v := prelimSurface(a.o.column(qx, qz))
	a.prelim[k] = v
	return v
}

func (a *aquifer) center(cx, cy, cz int32) *aquiferCenter {
	var c *aquiferCenter
	gx, gy, gz := cx-a.gridX, cy-aquiferMinCellY, cz-a.gridZ
	if uint32(gx) < 3 && uint32(gy) < aquiferCellsY && uint32(gz) < 3 {
		c = &a.grid[(gx*aquiferCellsY+gy)*3+gz]
		if c.made {
			return c
		}
	} else {
		k := [3]int32{cx, cy, cz}
		if c, ok := a.cells[k]; ok {
			return c
		}
		c = new(aquiferCenter)
		a.cells[k] = c
	}
	r := a.o.aquiferNoise.pos.At(cx, cy, cz)
	dx := r.IntN(10)
	dy := r.IntN(9)
	dz := r.IntN(10)
	*c = aquiferCenter{x: cx*16 + dx, y: cy*12 + dy, z: cz*16 + dz, made: true}
	return c
}

// fluid returns the fluid status of a cell centre, computing it once.
func (a *aquifer) fluid(c *aquiferCenter) (int32, bool) {
	if c.set {
		return c.level, c.lava
	}
	c.level, c.lava = a.computeFluid(c.x, c.y, c.z)
	c.set = true
	return c.level, c.lava
}

func (a *aquifer) computeFluid(x, y, z int32) (int32, bool) {
	o := a.o
	minSurface := int32(noSurface)
	near := false
	for _, off := range aquiferNeighbours() {
		h := a.prelimAt((x+16*off[0])>>2, (z+16*off[1])>>2)
		centre := off[0] == 0 && off[1] == 0
		if centre && y-12 > h+8 {
			return SeaLevel, false
		}
		if !(y+4 <= h && !centre) && h+8 < SeaLevel {
			if y+4 > h {
				return SeaLevel, false
			}
			near = near || centre
		}
		minSurface = min(minSurface, h)
	}
	// Deep dark: no fluid where the erosion is low under high terrain.
	col := o.column(x>>2, z>>2)
	if col.erosion < -0.225 && (float32(y)*-0.0078125+1)+col.offset > 0.9 {
		return noFluidLevel, false
	}
	partial := float32(1)
	if near {
		v := float32(minSurface+8-y) * 0.015625
		switch {
		case v < 0:
			partial = 0
		case v <= 1:
			partial = -((1 - v) + -1)
		}
	}
	flood := o.aquiferNoise.flood.Sample(float32(x), float32(y)*0.67, float32(z))
	if 1 < flood {
		flood = 1
	} else {
		flood = max(flood, -1)
	}
	if flood > 1.1*partial+-0.3 {
		return SeaLevel, false
	}
	if partial*1.2+-0.8 > flood {
		return noFluidLevel, false
	}
	gy := mathx.FloorDiv(y, 40)
	s := (o.aquiferNoise.spread.Sample(float32(mathx.FloorDiv(x, 16)), float32(gy)/1.4, float32(mathx.FloorDiv(z, 16))) * 10) / 3
	fl := int32(s)
	if float32(fl) > s {
		fl--
	}
	raw := 3*fl + 40*gy + 20
	level := min(raw, minSurface)
	lava := raw <= -10 && mathx.AbsFloat(o.aquiferNoise.lava.Sample(float32(mathx.FloorDiv(x, 64)), float32(gy), float32(mathx.FloorDiv(z, 64)))) > 0.3
	return level, lava
}

func pressureTerm(y, levelA, levelB int32, lavaA, lavaB bool) (float32, bool) {
	if levelA > y && levelB > y && lavaA != lavaB {
		return 1, false
	}
	if levelA == levelB {
		return 0, false
	}
	diff := levelB - levelA
	if diff < 0 {
		diff = -diff
	}
	mid := (float32(y) + 0.5) - float32(levelA+levelB)*0.5
	q := float32(diff)*0.5 - mathx.AbsFloat(mid)
	var r float32
	if mid <= 0 {
		div := float32(10)
		if q+3 > 0 {
			div = 3
		}
		r = (q + 3) / div
	} else {
		div := float32(2.5)
		if q > 0 {
			div = 1.5
		}
		r = (q + 0) / div
	}
	return r, mathx.AbsFloat(r) <= 2
}

// sample computes the aquifer at block (x, y, z): fluid level, lava flag and
// the extra density that keeps fluids of different levels apart.
func (a *aquifer) sample(x, y, z int32) {
	if y <= -55 {
		a.level, a.isLava, a.density = -54, true, 0
		return
	}
	cx, cy, cz := mathx.FloorDiv(x-5, 16), mathx.FloorDiv(y+1, 12), mathx.FloorDiv(z-5, 16)
	g := a.group(cx, cy, cz)
	if g != nil && g.uniform {
		a.level, a.isLava, a.density = g.level, g.lava, 0
		return
	}
	const maxDist = ^uint64(0)
	d1, d2, d3 := maxDist, maxDist, maxDist
	var c1, c2, c3 *aquiferCenter
	n := 0
	for dx := int32(0); dx <= 1; dx++ {
		for dy := int32(-1); dy <= 1; dy++ {
			for dz := int32(0); dz <= 1; dz++ {
				var c *aquiferCenter
				if g != nil {
					c = g.cells[n]
				} else {
					c = a.center(cx+dx, cy+dy, cz+dz)
				}
				n++
				ex, ey, ez := int64(c.x-x), int64(c.y-y), int64(c.z-z)
				d := uint64(ex*ex + ey*ey + ez*ez)
				switch {
				case d1 >= d:
					c3, d3 = c2, d2
					c2, d2 = c1, d1
					c1, d1 = c, d
				case d2 >= d:
					c3, d3 = c2, d2
					c2, d2 = c, d
				case d3 >= d:
					c3, d3 = c, d
				}
			}
		}
	}
	l1, lava1 := a.fluid(c1)
	f12 := float32(absDiff(d2, d1))*-0.04 + 1
	a.level, a.isLava = l1, lava1
	if !(f12 > -1) {
		a.density = 0
		return
	}
	l2, lava2 := a.fluid(c2)
	l3, lava3 := a.fluid(c3)
	f13 := 1 - float32(absDiff(d3, d1))*0.04
	f23 := 1 - float32(absDiff(d3, d2))*0.04

	barrier := barrierSample{n: a.o.aquiferNoise.barrier, x: x, y: y, z: z}
	p12 := barrier.add(pressureTerm(y, l1, l2, lava1, lava2))
	p13 := barrier.add(pressureTerm(y, l1, l3, lava1, lava3))
	p23 := barrier.add(pressureTerm(y, l2, l3, lava2, lava3))
	f := max(0, f12)
	a.density = max(0, (f+f)*max(p12, max(max(0, f13)*p13, max(0, f23)*p23)))
}

// barrierSample is the aquifer barrier noise at one block, sampled only
// once a pressure term first needs it.
type barrierSample struct {
	n       *noise.Normal
	x, y, z int32
	v       float32
	have    bool
}

// add adds the barrier to pressure v when the two fluids are near.
func (b *barrierSample) add(v float32, near bool) float32 {
	if !near {
		return v
	}
	if !b.have {
		b.v = b.n.Sample(float32(b.x), float32(b.y)*0.5, float32(b.z))
		b.have = true
	}
	return v + b.v
}

func absDiff(a, b uint64) uint64 {
	if a > b {
		return a - b
	}
	return b - a
}
