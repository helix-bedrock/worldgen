package overworld

import (
	"slices"
	"strings"

	"github.com/helix-bedrock/worldgen/rand"
	"github.com/helix-bedrock/worldgen/structure"
)

// Scattered features: desert pyramids, jungle temples, swamp huts and
// igloos, written from BDS 1.26.52. They share one placement
// (RandomScatteredLargeFeature: constructor sub_4E86130, chunk test
// sub_4E86B40, start sub_4E86DE0) and pick the building by the biome at the
// start chunk's middle. The pieces are code-built (desert pyramid
// sub_4E8B6F0, jungle temple sub_4E905B0, swamp hut sub_4E98530) except the
// igloo, which places templates (sub_4E9A2C0).
//
// Bedrock differs from Java here, and the differences are kept:
//   - every piece has orientation 0, so no building is ever rotated (the
//     igloo rotates its templates instead);
//   - a piece's height comes from the top solid block (looking through
//     leaves, stopping at water), not a Java height map;
//   - the swamp hut stands one block higher than Java's, and clears
//     non-air blocks in its upper box first;
//   - the desert pyramid's west upper wall runs to z 18, and the first
//     two "sandstone" blocks of its entrance are default sandstone stairs;
//   - jungle temple vines face all four sides;
//   - an igloo with a basement has nextInt(8)+4 ladder sections, not one
//     fewer, and the witch hut's cauldron holds a random potion.
//
// BDS builds a piece in every chunk it reaches, each chunk with its own
// population-seeded random (reseeded per structure type, sub_C970F80). The
// piece's height is fixed by the first chunk that builds it; here that is
// the start chunk.
// VERIFY: the start chunk is the first to be built in BDS worlds too.

// Scattered feature placement: linear spread (sub_4E86B40).
const (
	scatterSalt       = 14357617
	scatterSpacing    = 32
	scatterSeparation = 8
)

// Scattered feature kinds.
const (
	scatterNone uint8 = iota
	scatterDesert
	scatterJungle
	scatterHut
	scatterIgloo
)

// scatterBiomes are the biomes each kind starts in (sub_4E86130 lists them
// all; sub_4E86DE0 maps them to kinds).
var scatterBiomes = map[string]uint8{
	"desert": scatterDesert, "desert_hills": scatterDesert,
	"jungle": scatterJungle, "jungle_hills": scatterJungle,
	"swampland": scatterHut, "swampland_mutated": scatterHut,
	"ice_plains": scatterIgloo, "cold_taiga": scatterIgloo, "snowy_slopes": scatterIgloo,
}

// scatterSize is each kind's piece size (x, y, z).
var scatterSize = [...][3]int32{
	scatterDesert: {21, 15, 21},
	scatterJungle: {12, 10, 15},
	scatterHut:    {7, 7, 9},
	scatterIgloo:  {7, 5, 8},
}

// scatterKindAt is the kind of the biome at a position.
func (o *Overworld) scatterKindAt(x, y, z int32) uint8 {
	if b := o.BiomeAt(x, y, z); int(b) < len(o.scatterKind) {
		return o.scatterKind[b]
	}
	return scatterNone
}

// scatteredAt returns the kind of the scattered feature that starts in
// chunk (cx, cz), scatterNone if none does.
func (o *Overworld) scatteredAt(cx, cz int32) uint8 {
	var r rand.MT
	rx, rz := structure.Region(cx, scatterSpacing), structure.Region(cz, scatterSpacing)
	structureSeed(&r, o.seed, rx, rz, scatterSalt)
	n := int32(scatterSpacing - scatterSeparation)
	if rx*scatterSpacing+r.IntN(n) != cx || rz*scatterSpacing+r.IntN(n) != cz {
		return scatterNone
	}
	// The test reads the biome at the preliminary surface (0 if there is
	// none), the start at the same level (1 if there is none).
	y := prelimSurface(o.column(cx*4, cz*4))
	test, start := y, y
	if y == noSurface {
		test, start = 0, 1
	}
	if o.scatterKindAt(cx*16+8, test, cz*16+8) == scatterNone {
		return scatterNone
	}
	// No column of the chunk may be below sea level.
	if o.filledMinHeight(cx, cz) < SeaLevel {
		return scatterNone
	}
	return o.scatterKindAt(cx*16+8, start, cz*16+8)
}

// filledMinHeight is the lowest height-map value of a chunk's terrain fill,
// before surfaces and carvers: sub_C972C70 fills the chunk again for the
// height map the fill records (the y above each column's top block, water
// included).
func (o *Overworld) filledMinHeight(cx, cz int32) int32 {
	var cols [gridXZ * gridXZ]column
	for i := range gridXZ {
		for k := range gridXZ {
			cols[i*gridXZ+k] = o.column(cx*4+int32(i), cz*4+int32(k))
		}
	}
	c := new(Chunk)
	g := new(grids)
	o.fillDensity(cx, cz, &cols, g)
	o.fillBlocks(cx, cz, g, newAquifer(o, cx, cz), c)
	lowest := int32(MaxY)
	for x := range 16 {
		for z := range 16 {
			h := int32(MinY)
			for y := MaxY - 1; y >= MinY; y-- {
				if c.Block(x, y, z) != o.ids.air {
					h = int32(y + 1)
					break
				}
			}
			lowest = min(lowest, h)
		}
	}
	return lowest
}

// scatteredReach lists the starts whose pieces can reach chunk (cx, cz):
// a desert pyramid reaches one chunk east and south, an igloo's rotated
// templates one chunk around.
func (o *Overworld) scatteredReach(cx, cz int32, f func(sx, sz int32, kind uint8)) {
	for sx := cx - 1; sx <= cx+1; sx++ {
		for sz := cz - 1; sz <= cz+1; sz++ {
			kind := o.scatteredAt(sx, sz)
			switch kind {
			case scatterNone:
				continue
			case scatterJungle, scatterHut:
				if sx != cx || sz != cz {
					continue
				}
			case scatterDesert:
				if sx > cx || sz > cz {
					continue
				}
			}
			f(sx, sz, kind)
		}
	}
}

// scatteredReaches reports whether a scattered feature has a piece in chunk
// (cx, cz).
func (o *Overworld) scatteredReaches(cx, cz int32) bool {
	found := false
	o.scatteredReach(cx, cz, func(int32, int32, uint8) { found = true })
	return found
}

// topSolid is the y above a column's top solid block, looking through
// leaves and plants; with water, water counts as the top (sub_C9F7970 with
// its stop-at-water flag, as the scattered pieces call it).
func (o *Overworld) topSolid(p *PackedChunk, x, z int32, water bool) int32 {
	if p == nil {
		return MinY
	}
	lx, lz := int(x&15), int(z&15)
	for y := MaxY - 1; y >= MinY; y-- {
		b := p.at(int(y-MinY)<<8 | lz<<4 | lx)
		if water && (b == o.ids.water || b == o.ids.flowingWater) || o.solidMaterial(b) {
			return int32(y + 1)
		}
	}
	return MinY
}

// scatteredY is the bottom of a piece started in chunk (sx, sz), from the
// start chunk's terrain: the lowest top in the chunk minus nextInt(3) for a
// desert pyramid (sub_4E8B6F0), the average top for the others
// (sub_4E8B570) plus 1 (hut), 0 (jungle temple) or -1 (igloo). The average
// counts tops under 0 as 0.
func (o *Overworld) scatteredY(kind uint8, sx, sz int32, start *PackedChunk) int32 {
	x0, z0 := sx*16, sz*16
	size := scatterSize[kind]
	x1, z1 := min(x0+size[0]-1, x0+15), min(z0+size[2]-1, z0+15)
	if kind == scatterDesert {
		var r rand.MT
		r.Seed(populationSeed(o.seed, sx, sz))
		lowest := int32(MaxY)
		for z := z0; z <= z1; z++ {
			for x := x0; x <= x1; x++ {
				lowest = min(lowest, o.topSolid(start, x, z, true))
			}
		}
		return lowest - r.IntN(3)
	}
	var sum, n int32
	for z := z0; z <= z1; z++ {
		for x := x0; x <= x1; x++ {
			sum += max(o.topSolid(start, x, z, true), 0)
			n++
		}
	}
	switch kind {
	case scatterHut:
		return sum/n + 1
	case scatterIgloo:
		return sum/n - 1
	}
	return sum / n
}

// placeScattered writes the scattered features that reach chunk (cx, cz)
// into c. raw gives undecorated terrain.
func (o *Overworld) placeScattered(cx, cz int32, c *Chunk, raw func(cx, cz int32) *PackedChunk) {
	o.scatteredReach(cx, cz, func(sx, sz int32, kind uint8) {
		start := raw(sx, sz)
		if start == nil {
			return
		}
		b := scBuild{o: o, c: c, cbox: structure.NewBox(cx*16, MinY, cz*16, cx*16+15, MaxY-1, cz*16+15),
			x0: sx * 16, y0: o.scatteredY(kind, sx, sz, start), z0: sz * 16}
		b.ids()
		if kind == scatterIgloo {
			// Only the start chunk builds the igloo (its piece fits in
			// it), writing its templates whole.
			b.r.Seed(populationSeed(o.seed, sx, sz))
			b.igloo(o.topSolid(start, sx*16, sz*16, false) - 1)
			return
		}
		b.r.Seed(populationSeed(o.seed, cx, cz))
		switch kind {
		case scatterDesert:
			b.desertPyramid()
		case scatterJungle:
			b.jungleTemple()
		case scatterHut:
			b.swampHut()
		}
	})
}

// scBuild builds one scattered piece into one chunk. Coordinates are
// relative to the piece's corner; orientation 0 leaves them unturned.
type scBuild struct {
	o          *Overworld
	c          *Chunk
	cbox       structure.Box
	r          rand.MT
	x0, y0, z0 int32

	air, sandstone, cut, chiseled, orange, blue, sand uint32
	cobble, mossy, redstone                           uint32
}

func (b *scBuild) ids() {
	b.air = b.o.ids.air
	b.sandstone, b.cut, b.chiseled = b.o.blockID("minecraft:sandstone"), b.o.blockID("minecraft:cut_sandstone"), b.o.blockID("minecraft:chiseled_sandstone")
	b.orange, b.blue, b.sand = b.o.blockID("minecraft:orange_terracotta"), b.o.blockID("minecraft:blue_terracotta"), b.o.blockID("minecraft:sand")
	b.cobble, b.mossy, b.redstone = b.o.blockID("minecraft:cobblestone"), b.o.blockID("minecraft:mossy_cobblestone"), b.o.blockID("minecraft:redstone_wire")
}

// state is a block with properties. A property the block does not have is
// left at its default, as BDS's setState does.
func (o *Overworld) state(name string, props map[string]any) uint32 {
	for k := range props {
		if !o.hasState(name, k) {
			delete(props, k)
		}
	}
	if id, ok := o.reg.Lookup(name, props); ok {
		return id
	}
	id, _ := o.reg.Default(name)
	return id
}

// get is a piece's getBlock: air outside the chunk.
func (b *scBuild) get(x, y, z int32) uint32 {
	wx, wy, wz := b.x0+x, b.y0+y, b.z0+z
	if !b.cbox.Contains([3]int32{wx, wy, wz}) {
		return b.air
	}
	return b.c.Block(int(wx&15), int(wy), int(wz&15))
}

// setWorld writes a block at a world position inside the chunk.
func (b *scBuild) setWorld(wx, wy, wz int32, s uint32) bool {
	if !b.cbox.Contains([3]int32{wx, wy, wz}) {
		return false
	}
	i := (wy-MinY)<<8 | (wz&15)<<4 | wx&15
	if b.o.floorBedrock(uint32(b.c.Blocks[i]), wy) {
		return false
	}
	// A block that can hold water keeps the water it replaces, as in
	// BDS's template placement (sub_D2B7A10).
	wet := b.o.isWater(uint32(b.c.Blocks[i])) && b.o.holdsWater(s)
	b.c.set(int(wx&15), int(wy), int(wz&15), s)
	if wet {
		b.c.Water[i>>waterWordShift] |= 1 << (i & waterBitMask)
	} else {
		b.c.Water[i>>waterWordShift] &^= 1 << (i & waterBitMask)
	}
	return true
}

// put is placeBlock.
func (b *scBuild) put(s uint32, x, y, z int32) bool {
	return b.setWorld(b.x0+x, b.y0+y, b.z0+z, s)
}

// entity adds a block entity for the block just placed at a piece
// position, and returns its data (nil if the block has none).
func (b *scBuild) entity(x, y, z int32, table string) map[string]any {
	wx, wy, wz := b.x0+x, b.y0+y, b.z0+z
	d := b.o.st.BlockEntityFor(b.c.Block(int(wx&15), int(wy), int(wz&15)), "", wx, wy, wz)
	if d == nil {
		return nil
	}
	be := structure.BlockEntity{X: wx, Y: wy, Z: wz, Data: d, LootTable: table}
	if table != "" {
		d["LootTable"] = table
	}
	b.c.BlockEntities = append(b.c.BlockEntities, be)
	return d
}

// fill is generateBox (sub_D260890): edge on the box's faces, inner inside,
// in y, x, z order; with skipAir only over blocks that are not air.
func (b *scBuild) fill(x0, y0, z0, x1, y1, z1 int32, edge, inner uint32, skipAir bool) {
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			for z := z0; z <= z1; z++ {
				if skipAir && b.get(x, y, z) == b.air {
					continue
				}
				s := inner
				if y == y0 || y == y1 || x == x0 || x == x1 || z == z0 || z == z1 {
					s = edge
				}
				b.put(s, x, y, z)
			}
		}
	}
}

// fillMoss is generateBox with the moss stone selector (sub_D260DB0 with
// off_EE8D0E8): a float for every block of the box, inside the chunk or
// not; under 0.4 cobblestone, otherwise mossy cobblestone.
func (b *scBuild) fillMoss(x0, y0, z0, x1, y1, z1 int32) {
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			for z := z0; z <= z1; z++ {
				s := b.mossy
				if b.r.Float() < 0.4 {
					s = b.cobble
				}
				b.put(s, x, y, z)
			}
		}
	}
}

// fillDown is fillColumnDown (sub_D255920): s down through air and liquid
// to the first other block (no lower than the second block above the
// world's bottom), which turns from grass to dirt.
func (b *scBuild) fillDown(s uint32, x, y, z int32) {
	b.fillDownWorld(s, b.x0+x, b.y0+y, b.z0+z)
}

// fillDownWorld is fillDown at a world position.
func (b *scBuild) fillDownWorld(s uint32, wx, wy, wz int32) {
	if !b.cbox.Contains([3]int32{wx, wy, wz}) {
		return
	}
	for {
		cur := b.c.Block(int(wx&15), int(wy), int(wz&15))
		if cur != b.air && !b.o.liquidMaterial(cur) && !soilPlants[b.o.blockName(cur)] {
			break
		}
		if wy <= MinY+1 {
			break
		}
		b.setWorld(wx, wy, wz, s)
		wy--
	}
	if b.o.blockName(b.c.Block(int(wx&15), int(wy), int(wz&15))) == "minecraft:grass_block" {
		dirt, _ := b.o.reg.Default("minecraft:dirt")
		b.setWorld(wx, wy, wz, dirt)
	}
}

// cardinalOf maps a legacy chest facing (2 north, 3 south, 4 west, 5 east)
// to minecraft:cardinal_direction.
var cardinalOf = map[int32]string{2: "north", 3: "south", 4: "west", 5: "east"}

// chest is createChest (sub_D254CA0): a chest facing dir with a loot table
// and a seed drawn from the random, unless a chest is already there.
func (b *scBuild) chest(x, y, z, dir int32, table string) {
	wx, wy, wz := b.x0+x, b.y0+y, b.z0+z
	if !b.cbox.Contains([3]int32{wx, wy, wz}) || b.o.blockName(b.get(x, y, z)) == blockChest {
		return
	}
	b.put(b.o.state(blockChest, map[string]any{stateCardinalDirection: cardinalOf[dir]}), x, y, z)
	if d := b.entity(x, y, z, "loot_tables/chests/"+table+".json"); d != nil {
		d["LootTableSeed"] = int32(b.r.Uint32() >> 1)
	}
}

// dispenser is createDispenser (sub_D255620), the chest's counterpart.
func (b *scBuild) dispenser(x, y, z, facing int32, table string) {
	wx, wy, wz := b.x0+x, b.y0+y, b.z0+z
	if !b.cbox.Contains([3]int32{wx, wy, wz}) || b.o.blockName(b.get(x, y, z)) == "minecraft:dispenser" {
		return
	}
	b.put(b.o.state("minecraft:dispenser", map[string]any{"facing_direction": facing}), x, y, z)
	if d := b.entity(x, y, z, "loot_tables/chests/"+table+".json"); d != nil {
		d["LootTableSeed"] = int32(b.r.Uint32() >> 1)
	}
}

// stairs is a stair block facing a weirdo_direction. BDS turns the piece's
// direction d through sub_D254F10, which for orientation 0 swaps 2 and 3.
func (b *scBuild) stairs(name string, d int32) uint32 {
	switch d {
	case 2:
		d = 3
	case 3:
		d = 2
	}
	return b.o.state("minecraft:"+name, map[string]any{"weirdo_direction": d})
}

// desertPyramid builds a desert pyramid (sub_4E8B6F0), Java 1.20's layout
// with its archaeology cellar.
func (b *scBuild) desertPyramid() {
	o := b.o
	b.r.IntN(3) // the height offset, already taken from the start chunk
	const w, d = 21, 21
	sandstone, cut, chis, orange, blue, air := b.sandstone, b.cut, b.chiseled, b.orange, b.blue, b.air
	b.fill(0, -4, 0, w-1, 0, d-1, sandstone, sandstone, false)
	for i := int32(1); i <= 9; i++ {
		b.fill(i, i, i, w-1-i, i, d-1-i, sandstone, sandstone, false)
		b.fill(i+1, i, i+1, w-2-i, i, d-2-i, air, air, false)
	}
	for x := int32(0); x < w; x++ {
		for z := int32(0); z < d; z++ {
			b.fillDown(sandstone, x, -5, z)
		}
	}
	s3, s2, s0, s1 := b.stairs("sandstone_stairs", 3), b.stairs("sandstone_stairs", 2),
		b.stairs("sandstone_stairs", 0), b.stairs("sandstone_stairs", 1)
	b.fill(0, 0, 0, 4, 9, 4, sandstone, air, false)
	b.fill(1, 10, 1, 3, 10, 3, sandstone, sandstone, false)
	b.put(s3, 2, 10, 0)
	b.put(s2, 2, 10, 4)
	b.put(s0, 0, 10, 2)
	b.put(s1, 4, 10, 2)
	b.fill(w-5, 0, 0, w-1, 9, 4, sandstone, air, false)
	b.fill(w-4, 10, 1, w-2, 10, 3, sandstone, sandstone, false)
	b.put(s3, w-3, 10, 0)
	b.put(s2, w-3, 10, 4)
	b.put(s0, w-5, 10, 2)
	b.put(s1, w-1, 10, 2)
	b.fill(8, 0, 0, 12, 4, 4, sandstone, air, false)
	b.fill(9, 1, 0, 11, 3, 4, air, air, false)
	for _, p := range [][3]int32{{9, 1, 1}, {9, 2, 1}, {9, 3, 1}, {10, 3, 1}, {11, 3, 1}, {11, 2, 1}, {11, 1, 1}} {
		b.put(cut, p[0], p[1], p[2])
	}
	b.fill(4, 1, 1, 8, 3, 3, sandstone, air, false)
	b.fill(4, 1, 2, 8, 2, 2, air, air, false)
	b.fill(12, 1, 1, 16, 3, 3, sandstone, air, false)
	b.fill(12, 1, 2, 16, 2, 2, air, air, false)
	b.fill(5, 4, 5, w-6, 4, d-6, sandstone, sandstone, false)
	b.fill(9, 4, 9, 11, 4, 11, air, air, false)
	b.fill(8, 1, 8, 8, 3, 8, cut, sandstone, false)
	b.fill(12, 1, 8, 12, 3, 8, cut, sandstone, false)
	b.fill(8, 1, 12, 8, 3, 12, cut, sandstone, false)
	b.fill(12, 1, 12, 12, 3, 12, cut, sandstone, false)
	b.fill(1, 1, 5, 4, 4, 11, sandstone, sandstone, false)
	b.fill(w-5, 1, 5, w-2, 4, 11, sandstone, sandstone, false)
	b.fill(6, 7, 9, 6, 7, 11, sandstone, sandstone, false)
	b.fill(w-7, 7, 9, w-7, 7, 11, sandstone, sandstone, false)
	b.fill(5, 5, 9, 5, 7, 11, cut, cut, false)
	b.fill(w-6, 5, 9, w-6, 7, 11, cut, cut, false)
	b.put(air, 5, 5, 10)
	b.put(air, 5, 6, 10)
	b.put(air, 6, 6, 10)
	b.put(air, w-6, 5, 10)
	b.put(air, w-6, 6, 10)
	b.put(air, w-7, 6, 10)
	b.fill(2, 4, 4, 2, 6, 4, air, air, false)
	b.fill(w-3, 4, 4, w-3, 6, 4, air, air, false)
	b.put(s3, 2, 4, 5)
	b.put(s3, 2, 3, 4)
	b.put(s3, w-3, 4, 5)
	b.put(s3, w-3, 3, 4)
	b.fill(1, 1, 3, 2, 2, 3, sandstone, sandstone, false)
	b.fill(w-3, 1, 3, w-2, 2, 3, sandstone, sandstone, false)
	plain, _ := o.reg.Default("minecraft:sandstone_stairs")
	slab, _ := o.reg.Default("minecraft:sandstone_slab")
	b.put(plain, 1, 1, 2)
	b.put(plain, w-2, 1, 2)
	b.put(slab, 1, 2, 2)
	b.put(slab, w-2, 2, 2)
	b.put(s1, 2, 1, 2)
	b.put(s0, w-3, 1, 2)
	b.fill(4, 3, 5, 4, 3, 18, sandstone, sandstone, false)
	b.fill(w-5, 3, 5, w-5, 3, 17, sandstone, sandstone, false)
	b.fill(3, 1, 5, 4, 2, 16, air, air, false)
	b.fill(w-6, 1, 5, w-5, 2, 16, air, air, false)
	for z := int32(5); z <= 17; z += 2 {
		b.put(cut, 4, 1, z)
		b.put(chis, 4, 2, z)
		b.put(cut, w-5, 1, z)
		b.put(chis, w-5, 2, z)
	}
	for _, p := range [][2]int32{{10, 7}, {10, 8}, {9, 9}, {11, 9}, {8, 10}, {12, 10}, {7, 10}, {13, 10}, {9, 11}, {11, 11}, {10, 12}, {10, 13}} {
		b.put(orange, p[0], 0, p[1])
	}
	b.put(blue, 10, 0, 10)
	// The towers' faces: rows y 2..8 of three blocks.
	rows := [7][3]uint32{
		{cut, orange, cut}, {cut, orange, cut}, {orange, chis, orange}, {cut, orange, cut},
		{orange, chis, orange}, {orange, orange, orange}, {cut, cut, cut},
	}
	for _, x := range []int32{0, w - 1} {
		for i, row := range rows {
			for k, s := range row {
				b.put(s, x, int32(i)+2, int32(k)+1)
			}
		}
	}
	for _, x := range []int32{2, w - 3} {
		for i, row := range rows {
			for k, s := range row {
				b.put(s, x+int32(k)-1, int32(i)+2, 0)
			}
		}
	}
	b.fill(8, 4, 0, 12, 6, 0, cut, cut, false)
	b.put(air, 8, 6, 0)
	b.put(air, 12, 6, 0)
	b.put(orange, 9, 5, 0)
	b.put(chis, 10, 5, 0)
	b.put(orange, 11, 5, 0)
	// The treasure room and its trap.
	b.fill(8, -14, 8, 12, -11, 12, cut, cut, false)
	b.fill(8, -10, 8, 12, -10, 12, chis, chis, false)
	b.fill(8, -9, 8, 12, -9, 12, cut, cut, false)
	b.fill(8, -8, 8, 12, -1, 12, sandstone, sandstone, false)
	b.fill(9, -11, 9, 11, -1, 11, air, air, false)
	plate, _ := o.reg.Default("minecraft:stone_pressure_plate")
	tnt, _ := o.reg.Default("minecraft:tnt")
	b.put(plate, 10, -11, 10)
	b.fill(9, -13, 9, 11, -13, 11, tnt, air, false)
	for _, p := range [][2]int32{{8, 10}, {12, 10}, {10, 8}, {10, 12}} {
		ox, oz := p[0]+(p[0]-10)/2, p[1]+(p[1]-10)/2
		b.put(air, p[0], -11, p[1])
		b.put(air, p[0], -10, p[1])
		b.put(chis, ox, -10, oz)
		b.put(cut, ox, -11, oz)
	}
	b.chest(10, -11, 12, 2, "desert_pyramid")
	b.chest(8, -11, 10, 5, "desert_pyramid")
	b.chest(10, -11, 8, 3, "desert_pyramid")
	b.chest(12, -11, 10, 4, "desert_pyramid")
	b.desertCellar()
}

// layerBlock is the block of one horizontal layer at local height y.
type layerBlock struct {
	y int32
	s uint32
}

// desertCellar is the archaeology cellar (the second half of sub_4E8B6F0).
// VERIFY: BDS skips it for worlds of older base game versions (byte_F4A2978
// and the version test); new worlds get it.
func (b *scBuild) desertCellar() {
	o := b.o
	sand, sandstone, cut, chis, orange, blue := b.sand, b.sandstone, b.cut, b.chiseled, b.orange, b.blue
	// nextBoolean is bit 27 of the output (Random vfunc +40, sub_8A5AA20).
	flip := b.r.Uint32()&0x8000000 != 0
	s1 := b.stairs("sandstone_stairs", 1)
	b.put(s1, 13, -1, 17)
	b.put(s1, 14, -2, 17)
	b.put(s1, 15, -3, 17)
	for x := int32(12); x <= 16; x++ {
		b.put(sand, x, 0, 17)
	}
	b.put(sand, 14, -1, 17)
	a, c := sandstone, sand
	if flip {
		a, c = sand, sandstone
	}
	b.put(a, 15, -1, 17)
	b.put(c, 16, -1, 17)
	b.put(sand, 15, -2, 17)
	b.put(sandstone, 16, -2, 17)
	b.put(sand, 16, -3, 17)
	for _, layer := range [3]layerBlock{{-3, cut}, {-2, chis}, {-1, cut}} {
		b.fill(13, layer.y, 10, 13, layer.y, 15, layer.s, layer.s, true)
		b.fill(19, layer.y, 10, 19, layer.y, 15, layer.s, layer.s, true)
		b.fill(13, layer.y, 10, 19, layer.y, 11, layer.s, layer.s, true)
		b.fill(13, layer.y, 16, 19, layer.y, 16, layer.s, layer.s, true)
	}
	// The cellar's own random, seeded from its corner (Mth.getSeed of
	// (16, -4, 13)) into a Xoroshiro128++.
	xr := rand.NewXoroshiro(rand.Seed128(rand.PositionSeed(b.x0+16, b.y0-4, b.z0+13)))
	var spots [][3]int32
	for y := int32(-3); y <= -1; y++ {
		for x := int32(14); x <= 18; x++ {
			for z := int32(11); z <= 15; z++ {
				b.put(sand, x, y, z)
				spots = append(spots, [3]int32{x, y, z})
			}
		}
	}
	for x := int32(14); x <= 18; x++ {
		for z := int32(11); z <= 15; z++ {
			s := sand
			if xr.IntN(3) == 0 {
				s = sandstone
			}
			b.put(s, x, 0, z)
		}
	}
	b.put(blue, 16, -4, 13)
	for _, p := range [][2]int32{{17, 14}, {17, 12}, {15, 14}, {15, 12}, {18, 13}, {14, 13}, {16, 15}, {16, 11}, {19, 13}} {
		b.put(orange, p[0], -4, p[1])
	}
	spots = b.cellarPillar(spots, sand, 19, 13)
	b.put(cut, 20, -3, 13)
	b.put(chis, 20, -2, 13)
	b.put(orange, 13, -4, 13)
	spots = b.cellarPillar(spots, sand, 13, 13)
	b.put(cut, 12, -3, 13)
	b.put(chis, 12, -2, 13)
	b.put(orange, 16, -4, 16)
	spots = b.cellarPillar(spots, sand, 16, 16)
	b.put(orange, 16, -4, 10)
	spots = b.cellarPillar(spots, sand, 16, 10)
	b.put(cut, 16, -3, 9)
	b.put(chis, 16, -2, 9)
	count := min(int(xr.IntN(3))+5, len(spots))
	for i := 1; i < len(spots); i++ {
		if j := int(xr.IntN(int32(i + 1))); j != i {
			spots[i], spots[j] = spots[j], spots[i]
		}
	}
	sus, _ := o.reg.Default("minecraft:suspicious_sand")
	for i := 0; i < max(count, 1) && i < len(spots); i++ {
		b.susSand(&xr, sus, spots[i])
	}
	x := xr.IntN(5) + 14
	z := xr.IntN(5) + 11
	b.susSand(&xr, sus, [3]int32{x, 0, z})
}

// jungleTemple builds a jungle temple (sub_4E905B0), Java's layout.
func (b *scBuild) jungleTemple() {
	o := b.o
	const w, d = 12, 15
	air, mossy, red := b.air, b.mossy, b.redstone
	b.fillMoss(0, -4, 0, w-1, 0, d-1)
	for _, p := range [][6]int32{
		{2, 1, 2, 9, 2, 2}, {2, 1, 12, 9, 2, 12}, {2, 1, 3, 2, 2, 11}, {9, 1, 3, 9, 2, 11},
		{1, 3, 1, 10, 6, 1}, {1, 3, 13, 10, 6, 13}, {1, 3, 2, 1, 6, 12}, {10, 3, 2, 10, 6, 12},
		{2, 3, 2, 9, 3, 12}, {2, 6, 2, 9, 6, 12}, {3, 7, 3, 8, 7, 11}, {4, 8, 4, 7, 8, 10},
	} {
		b.fillMoss(p[0], p[1], p[2], p[3], p[4], p[5])
	}
	b.fill(3, 1, 3, 8, 2, 11, air, air, false)
	b.fill(4, 3, 6, 7, 3, 9, air, air, false)
	b.fill(2, 4, 2, 9, 5, 12, air, air, false)
	b.fill(4, 6, 5, 7, 6, 9, air, air, false)
	b.fill(5, 7, 6, 6, 7, 8, air, air, false)
	b.fill(5, 1, 2, 6, 2, 2, air, air, false)
	b.fill(5, 2, 12, 6, 2, 12, air, air, false)
	b.fill(5, 5, 1, 6, 5, 1, air, air, false)
	b.fill(5, 5, 13, 6, 5, 13, air, air, false)
	b.put(air, 1, 5, 5)
	b.put(air, 10, 5, 5)
	b.put(air, 1, 5, 9)
	b.put(air, 10, 5, 9)
	for _, z := range []int32{0, 14} {
		for _, x := range []int32{2, 4, 7, 9} {
			b.fillMoss(x, 4, z, x, 5, z)
		}
	}
	b.fillMoss(5, 6, 0, 6, 6, 0)
	for _, x := range []int32{0, 11} {
		for z := int32(2); z <= 12; z += 2 {
			b.fillMoss(x, 4, z, x, 5, z)
		}
		b.fillMoss(x, 6, 5, x, 6, 5)
		b.fillMoss(x, 6, 9, x, 6, 9)
	}
	for _, p := range [][6]int32{
		{2, 7, 2, 2, 9, 2}, {9, 7, 2, 9, 9, 2}, {2, 7, 12, 2, 9, 12}, {9, 7, 12, 9, 9, 12},
		{4, 9, 4, 4, 9, 4}, {7, 9, 4, 7, 9, 4}, {4, 9, 10, 4, 9, 10}, {7, 9, 10, 7, 9, 10}, {5, 9, 7, 6, 9, 7},
	} {
		b.fillMoss(p[0], p[1], p[2], p[3], p[4], p[5])
	}
	s3, s2, s0, s1 := b.stairs("stone_stairs", 3), b.stairs("stone_stairs", 2),
		b.stairs("stone_stairs", 0), b.stairs("stone_stairs", 1)
	b.put(s3, 5, 9, 6)
	b.put(s3, 6, 9, 6)
	b.put(s2, 5, 9, 8)
	b.put(s2, 6, 9, 8)
	for _, p := range [][3]int32{{4, 0, 0}, {5, 0, 0}, {6, 0, 0}, {7, 0, 0}, {4, 1, 8}, {4, 2, 9}, {4, 3, 10}, {7, 1, 8}, {7, 2, 9}, {7, 3, 10}} {
		b.put(s3, p[0], p[1], p[2])
	}
	b.fillMoss(4, 1, 9, 4, 1, 9)
	b.fillMoss(7, 1, 9, 7, 1, 9)
	b.fillMoss(4, 1, 10, 7, 2, 10)
	b.fillMoss(5, 4, 5, 6, 4, 5)
	b.put(s0, 4, 4, 5)
	b.put(s1, 7, 4, 5)
	for i := int32(0); i < 4; i++ {
		b.put(s2, 5, -i, 6+i)
		b.put(s2, 6, -i, 6+i)
		b.fill(5, -i, 7+i, 6, -i, 9+i, air, air, false)
	}
	b.fill(1, -3, 12, 10, -1, 13, air, air, false)
	b.fill(1, -3, 1, 3, -1, 13, air, air, false)
	b.fill(1, -3, 1, 9, -1, 5, air, air, false)
	for z := int32(1); z <= 13; z += 2 {
		b.fillMoss(1, -3, z, 1, -2, z)
	}
	for z := int32(2); z <= 12; z += 2 {
		b.fillMoss(1, -1, z, 3, -1, z)
	}
	b.fillMoss(2, -2, 1, 5, -2, 1)
	b.fillMoss(7, -2, 1, 9, -2, 1)
	b.fillMoss(6, -3, 1, 6, -3, 1)
	b.fillMoss(6, -1, 1, 6, -1, 1)
	// The tripwire traps. Hooks, levers, pistons and repeaters keep the
	// direction given (sub_D254F10 leaves them alone).
	// Placed wire joins its neighbours (BDS updates it after placing).
	wireX := o.state("minecraft:trip_wire", map[string]any{"attached_bit": true,
		"minecraft:connection_east": true, "minecraft:connection_west": true})
	wireZ := o.state("minecraft:trip_wire", map[string]any{"attached_bit": true,
		"minecraft:connection_north": true, "minecraft:connection_south": true})
	// VineBlock's all-sides constant (dword_F60D6C0).
	vine := o.state(blockVine, map[string]any{"vine_direction_bits": int32(15)})
	b.put(o.tripwireHook(3, true), 1, -3, 8)
	b.put(o.tripwireHook(1, true), 4, -3, 8)
	b.put(wireX, 2, -3, 8)
	b.put(wireX, 3, -3, 8)
	for z := int32(7); z >= 1; z-- {
		b.put(red, 5, -3, z)
	}
	b.put(red, 4, -3, 1)
	b.put(mossy, 3, -3, 1)
	b.dispenser(3, -2, 1, 3, "dispenser_trap")
	b.put(vine, 3, -2, 2)
	// This pair is built from the plain hook, not the attached one.
	b.put(o.tripwireHook(0, false), 7, -3, 1)
	b.put(o.tripwireHook(2, false), 7, -3, 5)
	b.put(wireZ, 7, -3, 2)
	b.put(wireZ, 7, -3, 3)
	b.put(wireZ, 7, -3, 4)
	b.put(red, 8, -3, 6)
	b.put(red, 9, -3, 6)
	b.put(red, 9, -3, 5)
	b.put(mossy, 9, -3, 4)
	b.put(red, 9, -2, 4)
	b.dispenser(9, -2, 3, 4, "dispenser_trap")
	b.put(vine, 8, -1, 3)
	b.put(vine, 8, -2, 3)
	b.chest(8, -3, 3, 4, "jungle_temple")
	for _, p := range [][3]int32{{9, -3, 2}, {8, -3, 1}, {4, -3, 5}, {5, -2, 5}, {5, -1, 5}, {6, -3, 5}, {7, -2, 5}, {7, -1, 5}, {8, -3, 5}} {
		b.put(mossy, p[0], p[1], p[2])
	}
	b.fillMoss(9, -1, 1, 9, -1, 5)
	b.fill(8, -3, 8, 10, -1, 10, air, air, false)
	bricks, _ := o.reg.Default("minecraft:chiseled_stone_bricks")
	lever := o.state("minecraft:lever", map[string]any{"lever_direction": "south"})
	for x := int32(8); x <= 10; x++ {
		b.put(bricks, x, -2, 11)
	}
	for x := int32(8); x <= 10; x++ {
		b.put(lever, x, -2, 12)
	}
	b.fillMoss(8, -3, 8, 8, -3, 10)
	b.fillMoss(10, -3, 8, 10, -3, 10)
	b.put(mossy, 10, -2, 9)
	b.put(red, 8, -2, 9)
	b.put(red, 8, -2, 10)
	b.put(red, 10, -1, 9)
	b.put(o.state("minecraft:sticky_piston", map[string]any{"facing_direction": int32(1)}), 9, -2, 8)
	piston := o.state("minecraft:sticky_piston", map[string]any{"facing_direction": int32(5)})
	b.put(piston, 10, -2, 8)
	b.put(piston, 10, -1, 8)
	b.put(o.state("minecraft:unpowered_repeater", map[string]any{"direction": int32(0)}), 10, -2, 10)
	b.chest(9, -3, 10, 3, "jungle_temple")
}

// hutPotions are the witch hut cauldron's potions by nextInt(100): water
// breathing under 10, fire resistance under 15, swiftness under 30,
// slowness under 40, poison under 65, weakness under 75, else healing
// (sub_4E98530). The values are Bedrock potion IDs.
var hutPotions = [...]struct {
	below int32
	id    int16
}{{10, 19}, {15, 12}, {30, 14}, {40, 17}, {65, 25}, {75, 34}, {100, 21}}

// swampHut builds a witch hut (sub_4E98530).
func (b *scBuild) swampHut() {
	o := b.o
	air := b.air
	planks, _ := o.reg.Default("minecraft:spruce_planks")
	log, _ := o.reg.Default("minecraft:oak_log")
	fence, _ := o.reg.Default("minecraft:oak_fence")
	b.fill(0, 2, 0, 6, 3, 8, air, air, true)
	for _, p := range [][6]int32{
		{1, 1, 1, 5, 1, 7}, {1, 5, 2, 5, 5, 7}, {2, 1, 0, 4, 1, 0}, {2, 2, 2, 3, 4, 2},
		{1, 2, 3, 1, 4, 6}, {5, 2, 3, 5, 4, 6}, {2, 2, 7, 4, 4, 7},
	} {
		b.fill(p[0], p[1], p[2], p[3], p[4], p[5], planks, planks, false)
	}
	for _, p := range [][2]int32{{1, 2}, {5, 2}, {1, 7}, {5, 7}} {
		b.fill(p[0], 0, p[1], p[0], 4, p[1], log, log, false)
	}
	b.put(fence, 2, 3, 2)
	b.put(fence, 3, 3, 7)
	b.put(air, 1, 3, 4)
	b.put(air, 5, 3, 4)
	b.put(air, 5, 3, 5)
	pot, _ := o.reg.Default("minecraft:flower_pot")
	if b.put(pot, 1, 3, 5) {
		if d := b.entity(1, 3, 5, ""); d != nil {
			d["PlantBlock"] = map[string]any{"name": "minecraft:red_mushroom", "states": map[string]any{}}
		}
	}
	b.put(planks, 4, 4, 2)
	table, _ := o.reg.Default("minecraft:crafting_table")
	b.put(table, 3, 2, 6)
	// The cauldron: 2, 4 or 6 levels of water holding a random potion.
	if wx, wy, wz := b.x0+4, b.y0+2, b.z0+6; b.cbox.Contains([3]int32{wx, wy, wz}) && o.blockName(b.get(4, 2, 6)) != "minecraft:cauldron" {
		level := 2*b.r.IntN(3) + 2
		b.put(o.state("minecraft:cauldron", map[string]any{"fill_level": level}), 4, 2, 6)
		if d := b.entity(4, 2, 6, ""); d != nil {
			n := b.r.IntN(100)
			for _, p := range hutPotions {
				if n < p.below {
					// VERIFY: the PotionType BDS saves with it.
					d["PotionId"] = p.id
					d["PotionType"] = int16(0)
					break
				}
			}
		}
	}
	b.put(fence, 1, 2, 1)
	b.put(fence, 5, 2, 1)
	b.fill(0, 5, 1, 6, 5, 1, b.stairs("spruce_stairs", 3), b.stairs("spruce_stairs", 3), false)
	b.fill(0, 5, 2, 0, 5, 7, b.stairs("spruce_stairs", 0), b.stairs("spruce_stairs", 0), false)
	b.fill(6, 5, 2, 6, 5, 7, b.stairs("spruce_stairs", 1), b.stairs("spruce_stairs", 1), false)
	b.fill(0, 5, 8, 6, 5, 8, b.stairs("spruce_stairs", 2), b.stairs("spruce_stairs", 2), false)
	for _, p := range [][2]int32{{1, 2}, {5, 2}, {1, 7}, {5, 7}} {
		b.fillDown(log, p[0], -1, p[1])
	}
}

// igloo builds an igloo (sub_4E9A2C0) at height y: its top, and half the
// time a basement under a trapdoor, nextInt(8)+4 ladder sections down. The
// templates turn about their corner by a random quarter turn.
func (b *scBuild) igloo(y int32) {
	rot := int(b.r.IntN(4))
	origin := [3]int32{b.x0, y, b.z0}
	if b.r.Float64() < 0.5 {
		b.template("igloo/igloo_top_no_trapdoor", origin, rot)
		return
	}
	b.template("igloo/igloo_top_trapdoor", origin, rot)
	n := b.r.IntN(8) + 4
	// Each section sits where its connection point meets the top's
	// (sub_D2B50C0: both points turned, then subtracted).
	for k := range max(n, 1) {
		b.template("igloo/igloo_middle", iglooSection(origin, rot, [3]int32{3, -1 - 3*k, 5}, [3]int32{1, 2, 1}), rot)
	}
	bottom := iglooSection(origin, rot, [3]int32{3, -3*n - 1, 5}, [3]int32{3, 5, 7})
	markers := b.template("igloo/igloo_bottom", bottom, rot)
	for _, m := range markers {
		if m.name != "chest" {
			continue
		}
		// The chest under the marker gets its loot.
		seed := int32(b.r.Uint32() >> 1)
		p := [3]int32{m.pos[0], m.pos[1] - 1, m.pos[2]}
		for i := range b.c.BlockEntities {
			if be := &b.c.BlockEntities[i]; be.X == p[0] && be.Y == p[1] && be.Z == p[2] {
				be.LootTable = "loot_tables/chests/igloo_chest.json"
				be.Data["LootTable"] = be.LootTable
				be.Data["LootTableSeed"] = seed
			}
		}
	}
}

// templateMarker is a data structure block of a placed template.
type templateMarker struct {
	pos  [3]int32
	name string
}

// template places a template with its corner at origin, turned rot
// quarter turns about it, writing the blocks inside the chunk. Structure
// voids and anvils (the settings' ignored block) are skipped; data markers
// become air and are returned.
func (b *scBuild) template(path string, origin [3]int32, rot int) []templateMarker {
	o := b.o
	jt := o.st.Template(path)
	if jt == nil {
		return nil
	}
	conv := o.st.TemplateStates(jt, rot)
	var markers []templateMarker
	for _, tb := range jt.Template.Blocks {
		rp := structure.RotatePos(tb.Pos, rot)
		p := [3]int32{origin[0] + rp[0], origin[1] + rp[1], origin[2] + rp[2]}
		st := jt.Template.Palette[tb.State]
		cs := conv[tb.State]
		switch st.Name {
		case blockStructureVoid, "minecraft:anvil":
			continue
		case blockStructureBlock:
			if m, _ := tb.NBT["metadata"].(string); m != "" {
				markers = append(markers, templateMarker{p, m})
			}
			cs = structure.ConvertedState{ID: o.ids.air, Water: false, OK: true}
		}
		if !cs.OK || !b.setWorld(p[0], p[1], p[2], cs.ID) {
			continue
		}
		if cs.Water {
			i := (p[1]-MinY)<<8 | (p[2]&15)<<4 | p[0]&15
			b.c.Water[i>>waterWordShift] |= 1 << (i & waterBitMask)
		}
		if d := o.st.BlockEntityFor(cs.ID, st.Name, p[0], p[1], p[2]); d != nil {
			b.c.BlockEntities = append(b.c.BlockEntities, structure.BlockEntity{X: p[0], Y: p[1], Z: p[2], Data: d})
		}
	}
	return markers
}

// Scattered is a scattered feature found by NearestScattered: its kind
// ("desert_pyramid", "jungle_temple", "swamp_hut" or "igloo"), the middle
// of its piece and the piece's bottom y.
type Scattered struct {
	Kind    string
	X, Y, Z int32
}

// ScatteredKinds are the kind names NearestScattered takes.
var ScatteredKinds = []string{"desert_pyramid", "jungle_temple", "swamp_hut", "igloo"}

// NearestScattered finds the scattered feature of a kind ("" for any)
// nearest to block (x, z), searching maxRegions placement regions around
// it. It generates the start chunk's terrain for the height. It is safe for
// concurrent use.
func (o *Overworld) NearestScattered(x, z int32, kind string, maxRegions int32) (Scattered, bool) {
	for _, c := range o.scatterCandidates(x, z, maxRegions) {
		k := o.scatteredAt(c[0], c[1])
		if k == scatterNone || kind != "" && ScatteredKinds[k-1] != kind {
			continue
		}
		var ch Chunk
		o.GenerateChunk(c[0], c[1], &ch)
		size := scatterSize[k]
		return Scattered{Kind: ScatteredKinds[k-1], X: c[0]*16 + size[0]/2, Y: o.scatteredY(k, c[0], c[1], Pack(&ch)),
			Z: c[1]*16 + size[2]/2}, true
	}
	return Scattered{}, false
}

// NearestIglooBasement finds the igloo with a basement nearest to block
// (x, z), searching maxRegions placement regions around it. The position
// returned is the trapdoor over the ladder down. It is safe for concurrent
// use.
func (o *Overworld) NearestIglooBasement(x, z int32, maxRegions int32) (Scattered, bool) {
	jt := o.st.Template("igloo/igloo_top_trapdoor")
	if jt == nil {
		return Scattered{}, false
	}
	var door [3]int32
	for _, b := range jt.Template.Blocks {
		if strings.HasSuffix(jt.Template.Palette[b.State].Name, "trapdoor") {
			door = b.Pos
		}
	}
	for _, c := range o.scatterCandidates(x, z, maxRegions) {
		if o.scatteredAt(c[0], c[1]) != scatterIgloo {
			continue
		}
		// The draws of igloo: the turn, then the basement roll.
		var r rand.MT
		r.Seed(populationSeed(o.seed, c[0], c[1]))
		rot := int(r.IntN(4))
		if r.Float64() < 0.5 {
			continue
		}
		var ch Chunk
		o.GenerateChunk(c[0], c[1], &ch)
		y := o.topSolid(Pack(&ch), c[0]*16, c[1]*16, false) - 1
		d := structure.RotatePos(door, rot)
		return Scattered{Kind: "igloo_basement", X: c[0]*16 + d[0], Y: y + d[1], Z: c[1]*16 + d[2]}, true
	}
	return Scattered{}, false
}

// scatterCandidates lists the start chunks of the scattered feature
// regions within maxRegions of block (x, z), nearest first.
func (o *Overworld) scatterCandidates(x, z int32, maxRegions int32) [][2]int32 {
	var cands []locateCand
	rx0, rz0 := structure.Region(x>>4, scatterSpacing), structure.Region(z>>4, scatterSpacing)
	for rx := rx0 - maxRegions; rx <= rx0+maxRegions; rx++ {
		for rz := rz0 - maxRegions; rz <= rz0+maxRegions; rz++ {
			var r rand.MT
			structureSeed(&r, o.seed, rx, rz, scatterSalt)
			n := int32(scatterSpacing - scatterSeparation)
			cx := rx*scatterSpacing + r.IntN(n)
			cz := rz*scatterSpacing + r.IntN(n)
			dx, dz := int64(cx*16+8-x), int64(cz*16+8-z)
			cands = append(cands, locateCand{cx, cz, dx*dx + dz*dz})
		}
	}
	slices.SortFunc(cands, compareLocateCand)
	out := make([][2]int32, len(cands))
	for i, c := range cands {
		out[i] = [2]int32{c.cx, c.cz}
	}
	return out
}

// iglooSection is where an igloo section goes so that its connection point
// c meets point a of the section above, both turned by rot.
func iglooSection(origin [3]int32, rot int, a, c [3]int32) [3]int32 {
	ra, rc := structure.RotatePos(a, rot), structure.RotatePos(c, rot)
	return [3]int32{origin[0] + ra[0] - rc[0], origin[1] + ra[1] - rc[1], origin[2] + ra[2] - rc[2]}
}

// tripwireHook is a tripwire hook facing dir.
func (o *Overworld) tripwireHook(dir int32, attached bool) uint32 {
	return o.state("minecraft:tripwire_hook", map[string]any{"attached_bit": attached, "direction": dir})
}

// cellarPillar puts the two sand blocks of a cellar pillar at (x, z) and
// adds them to the suspicious sand spots.
func (b *scBuild) cellarPillar(spots [][3]int32, sand uint32, x, z int32) [][3]int32 {
	b.put(sand, x, -3, z)
	spots = append(spots, [3]int32{x, -3, z})
	b.put(sand, x, -2, z)
	return append(spots, [3]int32{x, -2, z})
}

// susSand places suspicious sand at p with its loot seed drawn from xr.
func (b *scBuild) susSand(xr *rand.Xoroshiro, sus uint32, p [3]int32) {
	if !b.put(sus, p[0], p[1], p[2]) {
		return
	}
	// The loot seed is drawn only when the block entity is there.
	if d := b.entity(p[0], p[1], p[2], "loot_tables/entities/desert_pyramid_brushable_block.json"); d != nil {
		d["LootTableSeed"] = int32(uint32(xr.Uint64()))
	}
}
