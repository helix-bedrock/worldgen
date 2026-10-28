package overworld

// The freeze pass BDS runs at the end of decoration (postProcess,
// sub_517CD10): per column, water at the top freezes to ice and a snow layer
// falls on the top block where the biome is cold enough.

// Constants of the biome temperature (sub_C7B8560, sub_C7B91C0).
const (
	// coolingStart is how far above sea level the air starts to cool.
	coolingStart = 17
	// columnNoiseScale samples the column noise at 1/8 of block scale and
	// reads the stored byte back at 1/8.
	columnNoiseScale = 0.125
	// columnNoiseByte scales the noise up twice before it is stored in a
	// byte.
	columnNoiseByte = 8
	// coolingRate and coolingDivisor give the cooling per block of height.
	coolingRate    = -0.05
	coolingDivisor = 40
	// freezeTemperature is the temperature at or below which water freezes
	// and snow falls.
	freezeTemperature = 0.15
)

// temperature is the biome temperature at a block (sub_C7B8560): the
// biome's own, frozen ocean patches, and cooling above sea level + 17 that
// varies per column.
func (o *Overworld) temperature(b uint32, x, y, z int32) float32 {
	if int(b) >= len(o.surfaces) || o.surfaces[b] == nil {
		return 1
	}
	t := o.temperatureAt(o.surfaces[b], x, z)
	const limit = SeaLevel + coolingStart
	if y <= limit {
		return t
	}
	// The column noise is stored in a byte at 1/8 resolution
	// (sub_C7B91C0): noise * 8 * 8, read back * 0.125.
	n := o.surfNoise.heightTemp.Sum2(float32(x)*columnNoiseScale, float32(z)*columnNoiseScale)
	col := float32(int8(int32(n * columnNoiseByte * columnNoiseByte)))
	return t + float32(float64(((float32(y)+col*columnNoiseScale)-limit)*coolingRate)/coolingDivisor)
}

// rainTop is the y above the highest block that stops rain in a column of
// the chunk (LevelChunk::getTopRainBlockPos, sub_C9FBAC0): solid blocks,
// liquids and leaves; plants and snow layers let it through.
func (o *Overworld) rainTop(c *Chunk, x, z int) int32 {
	for y := MaxY - 1; y >= MinY; y-- {
		b := c.Block(x, y, z)
		if b == o.ids.air {
			continue
		}
		if o.isWater(b) || b == o.ids.lava || b == o.ids.flowingLava || o.solidMaterial(b) || o.hasBlockTag(b, "leaves") {
			return int32(y + 1)
		}
	}
	return MinY
}

func (o *Overworld) freeze(chunkX, chunkZ int32, c *Chunk) {
	snow, ok := o.reg.Default(blockSnowLayer)
	if !ok {
		return
	}
	for x := range 16 {
		for z := range 16 {
			wx, wz := chunkX*16+int32(x), chunkZ*16+int32(z)
			top := o.rainTop(c, x, z)
			below := top - 1
			if below >= MinY && below < MaxY {
				b := c.Block(x, int(below), z)
				// Only source water freezes; generated water is all source.
				if b == o.ids.water && o.temperature(c.Biome(x, int(below), z), wx, below, wz) <= freezeTemperature {
					c.set(x, int(below), z, o.ids.ice)
				}
			}
			if top >= MinY && top < MaxY && o.snowReplaces(c.Block(x, int(top), z)) &&
				o.temperature(c.Biome(x, int(top), z), wx, top, wz) <= freezeTemperature && o.snowCanStand(c, x, int(top), z) {
				c.set(x, int(top), z, snow)
			}
		}
	}
}

// snowCanStand is the snow layer's placement rule for the block below
// (TopSnowBlock::mayPlace, sub_CE980E0): leaves, or a full solid block that
// is not ice or packed ice.
// VERIFY: "full top face" is approximated by the solid-material list.
func (o *Overworld) snowCanStand(c *Chunk, x, y, z int) bool {
	if y-1 < MinY {
		return false
	}
	b := c.Block(x, y-1, z)
	if o.hasBlockTag(b, "leaves") {
		return true
	}
	switch b {
	case o.ids.ice, o.ids.packedIce, o.ids.blueIce:
		return false
	}
	// A campfire's top is not full: BDS leaves the abandoned camp's
	// campfire at -846 70 2226 bare on the comparison seed.
	switch o.blockName(b) {
	case "minecraft:campfire", "minecraft:soul_campfire":
		return false
	}
	return o.solidMaterial(b)
}

// snowReplaces reports whether a snow layer may take a block's place: air,
// or a block BDS marks as replaceable (component type 3 in
// sub_C8028A0).
// VERIFY: the replaceable set is taken to be the small single plants.
func (o *Overworld) snowReplaces(b uint32) bool {
	if b == o.ids.air {
		return true
	}
	switch o.blockName(b) {
	case blockShortGrass, blockFern, blockDeadBush, "minecraft:short_dry_grass":
		return true
	}
	return false
}
