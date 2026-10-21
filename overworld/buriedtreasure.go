package overworld

import (
	"cmp"
	"slices"

	"github.com/helix-bedrock/worldgen/rand"
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

// treasureBiomes are the biomes all around a buried treasure (radius 3).
var treasureBiomes = []string{"beach", "cold_beach", "stone_beach", "mushroom_island_shore"}

// isTreasureChunk reports whether a buried treasure starts in chunk
// (cx, cz).
func (o *Overworld) isTreasureChunk(cx, cz int32) bool {
	x, z := o.treasurePick(floorDivRegion(cx, treasureSpacing), floorDivRegion(cz, treasureSpacing))
	if x != cx || z != cz {
		return false
	}
	return o.biomesOnly(cx*16+8, o.structureY(cx, cz, 1), cz*16+8, 3, o.treasureBiome)
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
	solid := func(x, y, z int32) bool {
		if y < MinY || y >= MaxY {
			return false
		}
		b := c.Block(int(x&15), int(y), int(z&15))
		return o.solidMaterial(b) && !o.liquidMaterial(b)
	}
	y := o.topSolid(raw, x, z, false) - r.IntN(2) - 4
	for !(solid(x, y+1, z) && solid(x, y, z-1) && solid(x+1, y, z) && solid(x, y, z+1) && solid(x-1, y, z)) && y >= 6 {
		y--
	}
	for !solid(x, y-1, z) && y >= 6 {
		y--
	}
	return [3]int32{x, y, z}
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
	b := scBuild{o: o, c: c, cbox: box{cx * 16, MinY, cz * 16, cx*16 + 15, MaxY - 1, cz*16 + 15}}
	b.ids()
	chest, _ := o.reg.Default("minecraft:chest")
	if !b.setWorld(p[0], p[1], p[2], chest) {
		return
	}
	b.dropEntity(p)
	if d := o.blockEntityFor(chest, "", p[0], p[1], p[2]); d != nil {
		table := "loot_tables/chests/buriedtreasure.json"
		d["LootTable"] = table
		d["LootTableSeed"] = int32(r.Uint32() >> 1)
		c.BlockEntities = append(c.BlockEntities, BlockEntity{X: p[0], Y: p[1], Z: p[2], Data: d, LootTable: table})
	}
}

// NearestBuriedTreasure finds the buried treasure nearest to block (x, z),
// searching maxRegions placement regions (4 chunks each) around it, and
// returns its chest. It is safe for concurrent use.
func (o *Overworld) NearestBuriedTreasure(x, z int32, maxRegions int32) (Scattered, bool) {
	type cand struct {
		cx, cz int32
		d      int64
	}
	var cands []cand
	rx0, rz0 := floorDivRegion(x>>4, treasureSpacing), floorDivRegion(z>>4, treasureSpacing)
	for rx := rx0 - maxRegions; rx <= rx0+maxRegions; rx++ {
		for rz := rz0 - maxRegions; rz <= rz0+maxRegions; rz++ {
			cx, cz := o.treasurePick(rx, rz)
			dx, dz := int64(cx*16+8-x), int64(cz*16+8-z)
			cands = append(cands, cand{cx, cz, dx*dx + dz*dz})
		}
	}
	slices.SortFunc(cands, func(a, b cand) int { return cmp.Compare(a.d, b.d) })
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
