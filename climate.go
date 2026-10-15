package worldgen

import (
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

// fillBiomes picks a biome for every 4x4x4 cell of the chunk. Depth is
// 1 - y/128 + offset at the cell's quarter column.
// The density grid's columns are reused: its corners are the same quarters.
func (o *Overworld) fillBiomes(cols *[gridXZ * gridXZ]column, c *Chunk) {
	for qx := range 4 {
		for qz := range 4 {
			col := cols[qx*gridXZ+qz]
			var target [7]int64
			target[0], target[1], target[2], target[3] = col.climate[0], col.climate[1], col.climate[2], col.climate[3]
			target[5] = col.climate[4]
			for qy := range Height / 4 {
				y := float32(MinY + qy*4)
				target[4] = quantize(1 - y/128 + col.offset)
				id, _ := o.biomes.Search(target)
				c.Biomes[qy<<4|qz<<2|qx] = id
			}
		}
	}
}
