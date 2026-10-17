package worldgen

import (
	"math"

	"github.com/helix-bedrock/worldgen/noise"
	"github.com/helix-bedrock/worldgen/rand"
	"github.com/helix-bedrock/worldgen/terrain"
)

// climate holds the named climate and terrain noises.
type climate struct {
	shift, continents, erosion, ridge, jagged *noise.Normal
	temperature, vegetation                   *noise.Normal
}

func normal(world rand.Positional, name string, first int, amps ...float32) *noise.Normal {
	r := world.FromHash(name)
	return noise.NewNormal(&r, first, amps)
}

func newClimate(world rand.Positional) climate {
	jag := make([]float32, 16)
	for i := range jag {
		jag[i] = 1
	}
	return climate{
		shift:       normal(world, "minecraft:offset", -3, 1, 1, 1, 0),
		continents:  normal(world, "minecraft:continentalness", -9, 1, 1, 2, 2, 2, 1, 1, 1, 1),
		erosion:     normal(world, "minecraft:erosion", -9, 1, 1, 0, 1, 1),
		ridge:       normal(world, "minecraft:ridge", -7, 1, 2, 1, 0, 0, 0),
		jagged:      normal(world, "minecraft:jagged", -16, jag...),
		temperature: normal(world, "minecraft:temperature", -10, 1.5, 0, 1, 0, 0, 0),
		vegetation:  normal(world, "minecraft:vegetation", -8, 1, 1, 0, 0, 0, 0),
	}
}

// column is the climate and terrain shape at one quarter-resolution column.
type column struct {
	offset, factor, jag float32
	erosion             float32
	// Quantized temperature, humidity, continentalness, erosion, weirdness.
	climate [5]int64
}

// factorScale: BDS keeps density at 128x the vanilla Java scale (its final
// squeeze multiplies by 0.005 = 0.64/128, its slides go to -10 and +15), so
// the factor carries the 128.
const factorScale = 128

// column samples the climate at quarter coordinates (qx, qz), one per 4
// blocks.
func (o *Overworld) column(qx, qz int32) column {
	c := &o.climate
	x, z := float32(qx), float32(qz)
	dx := c.shift.Sample(x, 0, z)
	dz := c.shift.Sample(z, x, 0)
	sx, sz := x+4*dx, z+4*dz
	p := terrain.Point{
		Continents: c.continents.Sample(sx, 0, sz),
		Erosion:    c.erosion.Sample(sx, 0, sz),
		Weirdness:  c.ridge.Sample(sx, 0, sz),
	}
	p.PeaksValleys = terrain.PeaksValleys(p.Weirdness)

	col := column{
		offset:  o.shape.Offset(&p),
		erosion: p.Erosion,
		factor:  o.shape.Factor(&p) * factorScale,
		climate: [5]int64{
			quantize(c.temperature.Sample(sx, 0, sz)),
			quantize(c.vegetation.Sample(sx, 0, sz)),
			quantize(p.Continents),
			quantize(p.Erosion),
			quantize(p.Weirdness),
		},
	}
	if j := o.shape.Jaggedness(&p); j > 0 {
		bx, bz := float32(qx*4), float32(qz*4)
		n := c.jagged.Sample(bx*1500, 0, bz*1500)
		if n <= 0 {
			col.jag = n * j * 0.5
		} else {
			col.jag = n * j
		}
	}
	return col
}

// quantize turns a climate value into the x10000 integer the biome search
// uses, truncated toward zero like BDS.
func quantize(v float32) int64 { return int64(float64(v) * 10000) }

// Biome zoom, after BDS 1.26.52 (BiomeSource3d::fillBiomes, slot 4,
// sub_493AAF0): biomes are searched on a grid every 8 blocks starting 8
// blocks before the chunk (sub_493A2F0), each grid point is moved by a
// random offset, and every block takes the biome of the nearest moved point
// among the 8 around it (BiomeChunkVolume, sub_C74F340, with the offsets of
// sub_C74E6D0 and the search sub_D1C42B0).
const (
	zoomXZ = 4
	zoomY  = Height/8 + 2
)

// fillBiomes stores a biome for every block of chunk (chunkX, chunkZ).
func (o *Overworld) fillBiomes(chunkX, chunkZ int32, c *Chunk) {
	var grid [zoomXZ * zoomXZ * zoomY]uint16
	var offs [zoomXZ * zoomXZ * zoomY][3]float32
	x0, z0, y0 := chunkX*16-8, chunkZ*16-8, int32(MinY-8)
	var jr rand.Java
	seed := int64(uint32(o.seed))
	for i := range int32(zoomXZ) {
		for j := range int32(zoomXZ) {
			x, z := x0+8*i, z0+8*j
			col := o.column(x>>2, z>>2)
			for k := range int32(zoomY) {
				y := y0 + 8*k
				n := (i*zoomXZ+j)*zoomY + k
				grid[n] = uint16(o.biomeIn(col, float32(y)))
				jr.Seed(rand.BlockSeed(x>>3, y>>3, z>>3) ^ seed)
				for a := range 3 {
					offs[n][a] = (jr.Float() - 0.5) * 0.9
				}
			}
		}
	}
	for y := range int32(Height) {
		wy := MinY + y
		ky := (wy >> 3) - (y0 >> 3)
		fy := float32(wy&7) * 0.125
		for z := range int32(16) {
			wz := chunkZ*16 + z
			kz := (wz >> 3) - (z0 >> 3)
			fz := float32(wz&7) * 0.125
			for x := range int32(16) {
				wx := chunkX*16 + x
				kx := (wx >> 3) - (x0 >> 3)
				fx := float32(wx&7) * 0.125
				base := (kx*zoomXZ+kz)*zoomY + ky
				if uniform(&grid, base) {
					// All 8 corners agree, so the nearest one does too (BDS
					// skips these cells the same way).
					c.Biomes[y<<8|z<<4|x] = grid[base]
					continue
				}
				c.Biomes[y<<8|z<<4|x] = grid[zoomNearest(&offs, base, fx, fy, fz)]
			}
		}
	}
}

// zoomCorners are the grid steps to the 8 corners of a cell in the order
// BDS tries them (z, y, then x outermost), with the block's offset from
// each corner.
var zoomCorners = [8]struct {
	step       int32
	dx, dy, dz float32
}{
	{0, 0, 0, 0}, {zoomY, 0, 0, -1}, {1, 0, -1, 0}, {zoomY + 1, 0, -1, -1},
	{zoomXZ * zoomY, -1, 0, 0}, {zoomXZ*zoomY + zoomY, -1, 0, -1},
	{zoomXZ*zoomY + 1, -1, -1, 0}, {zoomXZ*zoomY + zoomY + 1, -1, -1, -1},
}

// uniform reports whether the 8 corners of the cell at base hold one biome.
func uniform(grid *[zoomXZ * zoomXZ * zoomY]uint16, base int32) bool {
	b := grid[base]
	for _, cr := range zoomCorners[1:] {
		if grid[base+cr.step] != b {
			return false
		}
	}
	return true
}

// zoomNearest returns the grid index of the moved corner nearest to the
// block; a tie keeps the corner tried first.
func zoomNearest(offs *[zoomXZ * zoomXZ * zoomY][3]float32, base int32, fx, fy, fz float32) int32 {
	best, bestD := base, float32(math.MaxFloat32)
	for i, cr := range zoomCorners {
		n := base + cr.step
		o := &offs[n]
		x := float32(o[0] + float32(fx+cr.dx))
		y := float32(o[1] + float32(fy+cr.dy))
		z := float32(o[2] + float32(fz+cr.dz))
		d := float32(float32(z*z) + float32(float32(x*x)+float32(y*y)))
		if i == 0 {
			if d < bestD {
				bestD = d
			}
			continue
		}
		if bestD > d {
			best, bestD = n, d
		}
	}
	return best
}
