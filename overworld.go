// Package worldgen generates vanilla Bedrock worlds. It depends on nothing
// but the standard library and a block list exported from BDS, so any server
// can use it.
//
// Status: terrain shape (climate, splines, density), biomes, the bedrock
// floor and the biome surface builders follow BDS 1.26.52. Aquifers, caves,
// features and structures are not done yet; see README.md.
package worldgen

import (
	"fmt"
	"strings"
	"sync"

	"github.com/helix-bedrock/worldgen/biome"
	"github.com/helix-bedrock/worldgen/blocks"
	"github.com/helix-bedrock/worldgen/noise"
	"github.com/helix-bedrock/worldgen/rand"
	"github.com/helix-bedrock/worldgen/terrain"
)

// Overworld dimensions.
const (
	MinY     = -64
	MaxY     = 320
	Height   = MaxY - MinY
	SeaLevel = 63
)

// Chunk is the generator output for one 16x16 column. Blocks holds registry
// indices (16 bits, which halves the memory of cached chunks; NewOverworld
// refuses larger registries), index (y-MinY)<<8 | z<<4 | x. Biomes holds one biome ID per 4x4x4
// cell, index ((y-MinY)>>2)<<4 | (z>>2)<<2 | x>>2.
type Chunk struct {
	Blocks [16 * 16 * Height]uint16
	Biomes [4 * 4 * Height / 4]uint32
	// Water has one bit per block, same index as Blocks: set where the
	// block also holds source water (BDS's second block layer), as kelp,
	// seagrass and coral fans placed in the sea do.
	Water [16 * 16 * Height / 64]uint64
	// BlockEntities are the block entities of blocks structures placed
	// (beds, bells, chests...): the client draws these blocks from them.
	BlockEntities []BlockEntity
}

// BlockEntity is the Bedrock block entity data of one block, at a world
// position. Data holds "id" and the block's fields, as the game saves them.
type BlockEntity struct {
	X, Y, Z int32
	Data    map[string]any
	// LootTable is the loot table of a container the structure fills, as a
	// Bedrock path ("loot_tables/chests/village/village_plains_house.json"),
	// empty otherwise. It is also in Data, with a LootTableSeed, so the game
	// fills the chest when it is first opened; a server that wants other
	// loot can put its own Items in Data instead.
	LootTable string
}

// Waterlogged reports whether the block at local (x, z) and world y also
// holds source water.
func (c *Chunk) Waterlogged(x, y, z int) bool {
	i := (y-MinY)<<8 | z<<4 | x
	return c.Water[i>>6]&(1<<(i&63)) != 0
}

// Biome returns the biome ID at local (x, z) and world y.
func (c *Chunk) Biome(x, y, z int) uint32 {
	return c.Biomes[((y-MinY)>>2)<<4|(z>>2)<<2|x>>2]
}

// Block returns the state index at local (x, z) and world y.
func (c *Chunk) Block(x, y, z int) uint32 {
	return uint32(c.Blocks[(y-MinY)<<8|z<<4|x])
}

func (c *Chunk) set(x, y, z int, s uint32) {
	c.Blocks[(y-MinY)<<8|z<<4|x] = uint16(s)
}

// Overworld generates overworld chunks for one world seed. GenerateChunk may
// be called from many goroutines at once.
// Option changes how an Overworld generates. Without options it matches BDS.
type Option func(*options)

type options struct {
	houseChests     bool
	houseChestsPick func(house string) bool
}

// WithHouseChests gives village houses that have no chest in vanilla a
// chest, with its village's house loot table ("loot_tables/chests/village/
// village_<type>_house.json"). pick chooses the houses: it gets the house's
// template name ("village/plains/houses/plains_small_house_1") and returns
// true for those that get a chest; nil picks every house. Houses that have
// a chest in vanilla keep only that one, and farms, pens and other pieces
// with no inside get none. This departs from BDS, which leaves most houses
// without a chest. It may be called from many goroutines at once.
func WithHouseChests(pick func(house string) bool) Option {
	return func(o *options) { o.houseChests, o.houseChestsPick = true, pick }
}

type Overworld struct {
	opts options

	// overlays pools featureWorld overlay arrays (about 500 KB each).
	overlays sync.Pool

	seed         int64
	reg          *blocks.Registry
	ids          blockIDs
	shape        *terrain.Shape
	climate      climate
	blended      *noise.Blended
	deepRand     rand.Positional
	biomes       *biome.Tree
	caves        caveNoises
	ores         oreNoises
	aquiferNoise aquiferNoises
	carvers      []*caveCarver
	surfaces     []*biomeSurface
	subsurfaces  []*biomeSurface
	surfNoise    surfaceNoises
	rules        *featureRules
	features     *featureSet
	// names is each block state's block name, for feature replace rules.
	names []string
	// villageBiome marks the biomes with a minecraft:village_type;
	// villageType holds its type.
	villageBiome []bool
	villageType  []string
	// jigsaw is the village pools and elements; templates caches prepared
	// templates by path and villages assembled villages by start chunk.
	jigsaw    *jigsawData
	templates sync.Map
	villages  sync.Map
	// soilPlant is soilPlants by state, for applying placements.
	soilPlant []bool
	// blockTags are block tags by block name, without "minecraft:".
	blockTags map[string][]string
	// biomeTags are biome tags by biome ID.
	biomeTags [][]string
}

type blockIDs struct {
	air, stone, deepslate, water, bedrock, grass, dirt, sand, gravel uint32

	redSand, suspiciousSand, sandstone, redSandstone uint32
	flowingWater, lava, flowingLava                  uint32
	ice, packedIce, blueIce, snow                    uint32
	netherrack, coarseDirt                           uint32
	// replaceable is the stone family surface builders paint over.
	replaceable []uint32
	// terracotta is indexed by dye colour (0 white ... 15 black).
	terracotta [16]uint32
	mycelium   uint32
	// carvable is the set of blocks cave carvers may cut.
	carvable map[uint32]struct{}
	// canyonCarvable is the set canyons may cut (a different list).
	canyonCarvable map[uint32]struct{}
	// veins are the copper (0) and iron (1) ore vein rows.
	veins [2]oreVein
}

var terracottaColours = [16]string{
	"white", "orange", "magenta", "light_blue", "yellow", "lime", "pink", "gray",
	"light_gray", "cyan", "purple", "blue", "brown", "green", "red", "black",
}

// NewOverworld builds the noise for a seed. reg must contain the vanilla
// blocks the generator places; biomeIDs maps biome names (no "minecraft:"
// prefix) to numeric IDs, as from biome.LoadIDs.
func NewOverworld(seed int64, reg *blocks.Registry, biomeIDs map[string]uint32, opts ...Option) (*Overworld, error) {
	if reg.Len() > 1<<16 {
		return nil, fmt.Errorf("worldgen: %d block states do not fit the 16-bit chunk storage", reg.Len())
	}
	o := &Overworld{seed: seed, reg: reg, shape: terrain.NewShape()}
	for _, opt := range opts {
		opt(&o.opts)
	}
	entries := biome.Entries()
	for name, id := range biomeIDs {
		if id >= 1<<16 {
			return nil, fmt.Errorf("worldgen: biome %s has ID %d, over the 16 bits PackedChunk stores", name, id)
		}
	}
	if miss := biome.Missing(entries, biomeIDs); len(miss) > 0 {
		return nil, fmt.Errorf("worldgen: biomes missing from the biome list: %v", miss)
	}
	o.biomes = biome.NewTree(entries, biomeIDs)
	names := []struct {
		dst  *uint32
		name string
	}{
		{&o.ids.air, "minecraft:air"},
		{&o.ids.stone, "minecraft:stone"},
		{&o.ids.deepslate, "minecraft:deepslate"},
		{&o.ids.water, "minecraft:water"},
		{&o.ids.bedrock, "minecraft:bedrock"},
		{&o.ids.grass, "minecraft:grass_block"},
		{&o.ids.dirt, "minecraft:dirt"},
		{&o.ids.sand, "minecraft:sand"},
		{&o.ids.gravel, "minecraft:gravel"},
		{&o.ids.redSand, "minecraft:red_sand"},
		{&o.ids.suspiciousSand, "minecraft:suspicious_sand"},
		{&o.ids.sandstone, "minecraft:sandstone"},
		{&o.ids.redSandstone, "minecraft:red_sandstone"},
		{&o.ids.flowingWater, "minecraft:flowing_water"},
		{&o.ids.lava, "minecraft:lava"},
		{&o.ids.flowingLava, "minecraft:flowing_lava"},
		{&o.ids.ice, "minecraft:ice"},
		{&o.ids.packedIce, "minecraft:packed_ice"},
		{&o.ids.blueIce, "minecraft:blue_ice"},
		{&o.ids.snow, "minecraft:snow"},
		{&o.ids.netherrack, "minecraft:netherrack"},
		{&o.ids.coarseDirt, "minecraft:coarse_dirt"},
		{&o.ids.mycelium, "minecraft:mycelium"},
	}
	for i, c := range terracottaColours {
		names = append(names, struct {
			dst  *uint32
			name string
		}{&o.ids.terracotta[i], "minecraft:" + c + "_terracotta"})
	}
	for _, n := range names {
		id, ok := reg.Default(n.name)
		if !ok {
			return nil, fmt.Errorf("worldgen: block %s missing from the block list", n.name)
		}
		*n.dst = id
	}
	for _, n := range []string{"stone", "granite", "polished_granite", "diorite", "polished_diorite", "andesite", "polished_andesite"} {
		id, ok := reg.Default("minecraft:" + n)
		if !ok {
			return nil, fmt.Errorf("worldgen: block minecraft:%s missing from the block list", n)
		}
		o.ids.replaceable = append(o.ids.replaceable, id)
	}

	// The named noises hang off a fork of the seeded generator (its next two
	// outputs), not off the seed state itself.
	lo, hi := rand.Seed128(seed)
	seeded := rand.NewXoroshiro(lo, hi)
	world := seeded.Fork()
	o.climate = newClimate(world)
	o.caves = newCaveNoises(world)
	o.ores = newOreNoises(world)
	o.aquiferNoise = newAquiferNoises(world)
	vein := func(ore, raw, filler string, minY, maxY int32) (oreVein, error) {
		v := oreVein{minY: minY, maxY: maxY}
		for _, b := range []struct {
			dst  *uint32
			name string
		}{{&v.ore, ore}, {&v.raw, raw}, {&v.filler, filler}} {
			id, ok := reg.Default("minecraft:" + b.name)
			if !ok {
				return v, fmt.Errorf("worldgen: block minecraft:%s missing from the block list", b.name)
			}
			*b.dst = id
		}
		return v, nil
	}
	// BDS sub_498AB80: the blocks cave carvers may cut.
	o.ids.carvable = map[uint32]struct{}{}
	for _, n := range carvableBlocks {
		id, ok := reg.Default("minecraft:" + n)
		if !ok {
			return nil, fmt.Errorf("worldgen: block minecraft:%s missing from the block list", n)
		}
		o.ids.carvable[id] = struct{}{}
	}
	o.ids.canyonCarvable = map[uint32]struct{}{}
	for _, n := range canyonCarvableBlocks {
		id, ok := reg.Default("minecraft:" + n)
		if !ok {
			return nil, fmt.Errorf("worldgen: block minecraft:%s missing from the block list", n)
		}
		o.ids.canyonCarvable[id] = struct{}{}
	}
	var err error
	if o.ids.veins[0], err = vein("copper_ore", "raw_copper_block", "granite", 0, 50); err != nil {
		return nil, err
	}
	if o.ids.veins[1], err = vein("deepslate_iron_ore", "raw_iron_block", "tuff", -60, -8); err != nil {
		return nil, err
	}
	terrainRand := world.FromHash("minecraft:terrain")
	o.blended = noise.NewBlended(&terrainRand)
	deep := world.FromHash("minecraft:depth_based_layer")
	o.deepRand = deep.Fork()

	defs, err := biome.LoadDefs(vanillaData, "data/vanilla/biomes")
	if err != nil {
		return nil, err
	}
	o.surfNoise = newSurfaceNoises(seed, world)
	// The overworld's cave carver features (vanilla features
	// underground_cave_carver_feature and underground_extra_cave_carver_feature).
	for _, c := range []struct {
		skip   uint32
		height int32
	}{{33, 236}, {66, 103}} {
		cv, err := newCaveCarver(o, c.skip, c.height, [2]float32{0.1, 0.9}, [2]float32{0.7, 1.4}, [2]float32{0.8, 1.3}, [2]float32{-1, -0.4}, "minecraft:air")
		if err != nil {
			return nil, err
		}
		o.carvers = append(o.carvers, cv)
	}
	if err := o.loadSurfaces(defs, biomeIDs, world); err != nil {
		return nil, err
	}
	o.soilPlant = make([]bool, reg.Len())
	o.names = make([]string, reg.Len())
	for i := range o.names {
		st, _ := reg.State(uint32(i))
		o.names[i] = st.Name
		o.soilPlant[i] = soilPlants[st.Name]
	}
	o.blockTags = map[string][]string{}
	for _, n := range o.names {
		if _, ok := o.blockTags[n]; ok {
			continue
		}
		var tags []string
		for _, t := range reg.Tags(n) {
			tags = append(tags, strings.TrimPrefix(t, "minecraft:"))
		}
		o.blockTags[n] = tags
	}
	var maxBiome uint32
	for _, id := range biomeIDs {
		maxBiome = max(maxBiome, id)
	}
	o.biomeTags = make([][]string, maxBiome+1)
	o.villageBiome = make([]bool, maxBiome+1)
	o.villageType = make([]string, maxBiome+1)
	for name, id := range biomeIDs {
		if d, ok := defs[name]; ok {
			o.biomeTags[id] = d.Tags
			o.villageBiome[id] = d.Village != ""
			o.villageType[id] = d.Village
		}
	}
	if o.jigsaw, err = loadJigsawData(); err != nil {
		return nil, err
	}
	features, err := o.loadFeatures(vanillaData, "data/vanilla/features")
	if err != nil {
		return nil, err
	}
	o.features = features
	if err := o.loadFeatureRules(vanillaData, "data/vanilla/feature_rules", defs, biomeIDs, features); err != nil {
		return nil, err
	}
	return o, nil
}

// Seed returns the world seed.
func (o *Overworld) Seed() int64 { return o.seed }

// GenerateChunk fills c with the chunk at (chunkX, chunkZ).
func (o *Overworld) GenerateChunk(chunkX, chunkZ int32, c *Chunk) {
	var cols [gridXZ * gridXZ]column
	for i := range gridXZ {
		for k := range gridXZ {
			cols[i*gridXZ+k] = o.column(chunkX*4+int32(i), chunkZ*4+int32(k))
		}
	}
	clear(c.Water[:])
	c.BlockEntities = c.BlockEntities[:0]
	g := new(grids)
	o.fillDensity(chunkX, chunkZ, &cols, g)
	aq := newAquifer(o, chunkX, chunkZ)
	o.fillBlocks(chunkX, chunkZ, g, aq, c)
	mt := rand.NewMT(rand.ChunkSeed(chunkX, chunkZ))
	o.bedrockFloor(mt, c)
	o.fillBiomes(&cols, c)
	o.surface(chunkX, chunkZ, c, mt)
	for _, cv := range o.carvers {
		cv.carve(o, c, aq, chunkX, chunkZ)
	}
	o.canyons(c, aq, chunkX, chunkZ)
}

// bedrockFloor draws 2-5 bedrock blocks per column from the chunk's
// Mersenne Twister, x outer and z inner.
func (o *Overworld) bedrockFloor(mt *rand.MT, c *Chunk) {
	for x := range 16 {
		for z := range 16 {
			n := int(mt.IntN(4)) + 2
			for y := MinY; y < MinY+n; y++ {
				c.set(x, y, z, o.ids.bedrock)
			}
		}
	}
}

// carvableBlocks are the blocks BDS's cave carvers may cut (sub_498AB80).
var carvableBlocks = []string{
	"stone", "dirt", "coarse_dirt", "podzol", "grass_block",
	"white_glazed_terracotta", "orange_glazed_terracotta", "magenta_glazed_terracotta", "light_blue_glazed_terracotta",
	"yellow_glazed_terracotta", "lime_glazed_terracotta", "pink_glazed_terracotta", "gray_glazed_terracotta",
	"silver_glazed_terracotta", "cyan_glazed_terracotta", "purple_glazed_terracotta", "blue_glazed_terracotta",
	"brown_glazed_terracotta", "green_glazed_terracotta", "red_glazed_terracotta", "black_glazed_terracotta",
	"sandstone", "chiseled_sandstone", "cut_sandstone", "smooth_sandstone",
	"red_sandstone", "chiseled_red_sandstone", "cut_red_sandstone", "smooth_red_sandstone",
	"mycelium", "snow_layer", "packed_ice", "deepslate", "calcite",
	"sand", "red_sand", "suspicious_sand",
	"gravel", "dirt_with_roots", "tuff", "iron_ore", "deepslate_iron_ore", "raw_iron_block",
	"copper_ore", "deepslate_copper_ore", "raw_copper_block",
}

// canyonCarvableBlocks are the blocks BDS's canyons may cut (list at
// 0xF60D710).
var canyonCarvableBlocks = []string{
	"stone", "granite", "diorite", "andesite", "tuff", "deepslate", "calcite", "dirt", "coarse_dirt", "mud",
	"podzol", "mycelium", "grass_block", "dirt_with_roots", "hardened_clay",
	"white_terracotta", "orange_terracotta", "magenta_terracotta", "light_blue_terracotta", "yellow_terracotta",
	"lime_terracotta", "pink_terracotta", "gray_terracotta", "light_gray_terracotta", "cyan_terracotta",
	"purple_terracotta", "blue_terracotta", "brown_terracotta", "green_terracotta", "red_terracotta", "black_terracotta",
	"sandstone", "chiseled_sandstone", "cut_sandstone", "smooth_sandstone",
	"red_sandstone", "chiseled_red_sandstone", "cut_red_sandstone", "smooth_red_sandstone",
	"sand", "red_sand", "suspicious_sand", "iron_ore", "deepslate_iron_ore", "raw_iron_block",
	"copper_ore", "deepslate_copper_ore", "raw_copper_block", "snow", "snow_layer", "packed_ice", "gravel",
	"sulfur", "cinnabar",
}

// blockName returns the block name of a state.
func (o *Overworld) blockName(id uint32) string {
	if int(id) < len(o.names) {
		return o.names[id]
	}
	return ""
}

// solidMaterial reports whether a block counts as solid for
// query.above_top_solid. Leaves count as not solid: BDS looks through them.
// VERIFY: BDS reads the solid flag of the block's material (byte 3 of
// Material); the material table was not read, so liquids, plants, snow
// layers and other thin blocks are listed by name here.
func (o *Overworld) solidMaterial(b uint32) bool {
	switch b {
	case o.ids.air, o.ids.water, o.ids.flowingWater, o.ids.lava, o.ids.flowingLava:
		return false
	}
	name := o.blockName(b)
	if soilPlants[name] || o.hasBlockTag(b, "leaves") {
		return false
	}
	switch name {
	case "minecraft:snow_layer", "minecraft:vine", "minecraft:seagrass", "minecraft:kelp",
		"minecraft:waterlily", "minecraft:deadbush", "minecraft:reeds", "minecraft:web",
		"minecraft:fire", "minecraft:torch", "minecraft:brown_mushroom", "minecraft:red_mushroom":
		return false
	}
	return true
}

// hasBlockTag reports whether a state's block has a tag (without the
// "minecraft:" prefix).
func (o *Overworld) hasBlockTag(id uint32, tag string) bool {
	for _, t := range o.blockTags[o.blockName(id)] {
		if t == tag {
			return true
		}
	}
	return false
}

// hasState reports whether a block's default state has a property.
func (o *Overworld) hasState(name, prop string) bool {
	id, ok := o.reg.Default(name)
	if !ok {
		return false
	}
	st, ok := o.reg.State(id)
	if !ok {
		return false
	}
	_, ok = st.Props[prop]
	return ok
}
