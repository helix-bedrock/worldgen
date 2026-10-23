package overworld

import (
	"slices"

	"github.com/helix-bedrock/worldgen/rand"
	"github.com/helix-bedrock/worldgen/structure"
)

// Buried treasure, written from BDS 1.26.52: BuriedTreasureFeature
// (constructor sub_4E3EFC0, chunk test sub_4E3F4A0, start sub_4E3F700) and
// its piece (postProcess sub_4E3FA50). The piece is one chest at the start
// chunk's local (8, 8) (box corner 3 plus offset 5; Java uses 9, 9).
//
// The chest goes nextInt(2)+4 below the column's top solid block (through
// water), moves down until the block above it and its four neighbours are
// solid, then down again onto the first solid block under it, never below
// y 6. Nothing is built around it.
// VERIFY: "solid" is BDS's material test (not liquid, solid, block flag
// 0x40000); taken here as the solid material.

// Buried treasure placement (sub_4E3F4A0): triangular spread.
const (
	treasureSalt       = 16842397
	treasureSpacing    = 4
	treasureSeparation = 2
)

// Buried treasure chest placement (sub_4E3FA50).
const (
	// treasureBiomeReach is the radius of the start biome test, in blocks.
	treasureBiomeReach = 3
	// treasureMinDepth is the least the chest starts below the top solid
	// block.
	treasureMinDepth = 4
	// treasureExtraDepth bounds the random depth added to treasureMinDepth.
	treasureExtraDepth = 2
	// treasureFloorY is the lowest y the chest moves down to.
	treasureFloorY = 6
)

// treasureBiomes are the biomes all around a buried treasure.
func treasureBiomes() []string {
	return []string{"beach", "cold_beach", "stone_beach", "mushroom_island_shore"}
}

// isTreasureChunk reports whether a buried treasure starts in chunk
// (cx, cz).
func (o *Overworld) isTreasureChunk(cx, cz int32) bool {
	x, z := o.treasurePick(structure.Region(cx, treasureSpacing), structure.Region(cz, treasureSpacing))
	if x != cx || z != cz || !o.structOK(skBuriedTreasure, cx, cz) {
		return false
	}
	return o.biomesOnly(cx*16+8, o.structureY(cx, cz, noSurfaceY), cz*16+8, treasureBiomeReach, o.treasureBiome)
}

// treasurePick is the chunk a buried treasure region picks.
func (o *Overworld) treasurePick(rx, rz int32) (int32, int32) {
	var r rand.MT
	structureSeed(&r, o.seed, rx, rz, treasureSalt)
	n := int32(treasureSpacing - treasureSeparation)
	a := r.IntN(n)
	x := rx*treasureSpacing + (a+r.IntN(n))/2
	b := r.IntN(n)
	return x, rz*treasureSpacing + (b+r.IntN(n))/2
}

// treasureSpot is where the chest of chunk (cx, cz) goes, read from c and
// drawing from r.
func (o *Overworld) treasureSpot(cx, cz int32, c *Chunk, raw *PackedChunk, r *rand.MT) [3]int32 {
	x, z := cx*16+8, cz*16+8
	y := o.topSolid(raw, x, z, false) - r.IntN(treasureExtraDepth) - treasureMinDepth
	for !(o.treasureSolid(c, x, y+1, z) && o.treasureSolid(c, x, y, z-1) && o.treasureSolid(c, x+1, y, z) &&
		o.treasureSolid(c, x, y, z+1) && o.treasureSolid(c, x-1, y, z)) && y >= treasureFloorY {
		y--
	}
	for !o.treasureSolid(c, x, y-1, z) && y >= treasureFloorY {
		y--
	}
	return [3]int32{x, y, z}
}

// treasureSolid reports whether the block of c at (x, y, z) is solid and
// not liquid; blocks outside the world are not.
func (o *Overworld) treasureSolid(c *Chunk, x, y, z int32) bool {
	if y < MinY || y >= MaxY {
		return false
	}
	b := c.Block(int(x&15), int(y), int(z&15))
	return o.solidMaterial(b) && !o.liquidMaterial(b)
}

// placeTreasure writes the buried treasure of chunk (cx, cz), if it has
// one, into c. raw gives the chunk's undecorated terrain.
func (o *Overworld) placeTreasure(cx, cz int32, c *Chunk, raw *PackedChunk) {
	if raw == nil || !o.isTreasureChunk(cx, cz) {
		return
	}
	var r rand.MT
	r.Seed(populationSeed(o.seed, cx, cz))
	p := o.treasureSpot(cx, cz, c, raw, &r)
	b := scBuild{o: o, c: c, cbox: structure.NewBox(cx*16, MinY, cz*16, cx*16+15, MaxY-1, cz*16+15)}
	b.ids()
	chest, _ := o.reg.Default(blockChest)
	if !b.setWorld(p[0], p[1], p[2], chest) {
		return
	}
	b.dropEntity(p)
	if d := o.st.BlockEntityFor(chest, "", p[0], p[1], p[2]); d != nil {
		table := "loot_tables/chests/buriedtreasure.json"
		d["LootTable"] = table
		d["LootTableSeed"] = int32(r.Uint32() >> 1)
		c.BlockEntities = append(c.BlockEntities, structure.BlockEntity{X: p[0], Y: p[1], Z: p[2], Data: d, LootTable: table})
	}
}

// NearestBuriedTreasure finds the buried treasure nearest to block (x, z),
// searching maxRegions placement regions (4 chunks each) around it, and
// returns its chest. It is safe for concurrent use.
func (o *Overworld) NearestBuriedTreasure(x, z int32, maxRegions int32) (Scattered, bool) {
	var cands []locateCand
	rx0, rz0 := structure.Region(x>>4, treasureSpacing), structure.Region(z>>4, treasureSpacing)
	for rx := rx0 - maxRegions; rx <= rx0+maxRegions; rx++ {
		for rz := rz0 - maxRegions; rz <= rz0+maxRegions; rz++ {
			cx, cz := o.treasurePick(rx, rz)
			dx, dz := int64(cx*16+8-x), int64(cz*16+8-z)
			cands = append(cands, locateCand{cx, cz, dx*dx + dz*dz})
		}
	}
	slices.SortFunc(cands, compareLocateCand)
	for _, c := range cands {
		if !o.isTreasureChunk(c.cx, c.cz) {
			continue
		}
		var ch Chunk
		o.GenerateChunk(c.cx, c.cz, &ch)
		raw := Pack(&ch)
		var r rand.MT
		r.Seed(populationSeed(o.seed, c.cx, c.cz))
		p := o.treasureSpot(c.cx, c.cz, &ch, raw, &r)
		return Scattered{Kind: "buried_treasure", X: p[0], Y: p[1], Z: p[2]}, true
	}
	return Scattered{}, false
}
