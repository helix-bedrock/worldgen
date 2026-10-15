package worldgen

// BiomeAt returns the biome ID at a block position, as GenerateChunk would
// place it. It may be called from many goroutines at once.
func (o *Overworld) BiomeAt(x, y, z int32) uint32 {
	col := o.column(x>>2, z>>2)
	return o.biomeIn(col, float32(y&^3))
}

func (o *Overworld) biomeIn(col column, y float32) uint32 {
	target := [7]int64{col.climate[0], col.climate[1], col.climate[2], col.climate[3],
		quantize(1 - y/128 + col.offset), col.climate[4], 0}
	id, _ := o.biomes.Search(target)
	return id
}

// SurfaceBiomeAt returns the biome at the terrain surface of a column: the
// biome picked at depth 0, where the terrain shape puts the ground.
func (o *Overworld) SurfaceBiomeAt(x, z int32) uint32 {
	col := o.column(x>>2, z>>2)
	target := [7]int64{col.climate[0], col.climate[1], col.climate[2], col.climate[3], 0, col.climate[4], 0}
	id, _ := o.biomes.Search(target)
	return id
}

// CaveBiomeAt returns the biome deep underground in a column, at depth 0.5
// (where lush caves, dripstone caves and sulfur caves are picked).
func (o *Overworld) CaveBiomeAt(x, z int32) uint32 {
	col := o.column(x>>2, z>>2)
	target := [7]int64{col.climate[0], col.climate[1], col.climate[2], col.climate[3], 5000, col.climate[4], 0}
	id, _ := o.biomes.Search(target)
	return id
}

// DeepBiomeAt returns the biome near the bottom of the world in a column, at
// depth 1.1 (where the deep dark is picked).
func (o *Overworld) DeepBiomeAt(x, z int32) uint32 {
	col := o.column(x>>2, z>>2)
	target := [7]int64{col.climate[0], col.climate[1], col.climate[2], col.climate[3], 11000, col.climate[4], 0}
	id, _ := o.biomes.Search(target)
	return id
}
