// Package overworld generates the vanilla Bedrock Overworld. It depends on
// nothing but the standard library and a block list exported from BDS, so
// any server can use it. See STATUS.md for what is generated.
package overworld

import (
	"fmt"
	"math"
	"strings"
	"sync"

	"github.com/helix-bedrock/worldgen"
	"github.com/helix-bedrock/worldgen/biome"
	"github.com/helix-bedrock/worldgen/blocks"
	"github.com/helix-bedrock/worldgen/internal/lru"
	"github.com/helix-bedrock/worldgen/noise"
	"github.com/helix-bedrock/worldgen/overworld/terrain"
	"github.com/helix-bedrock/worldgen/rand"
	"github.com/helix-bedrock/worldgen/structure"
)

// Overworld dimensions.
const (
	MinY     = -64
	MaxY     = 320
	Height   = MaxY - MinY
	SeaLevel = 63
)

// BlockEntity is the Bedrock block entity data of one block a structure
// placed.
type BlockEntity = structure.BlockEntity

// Chunk is the generator output for one 16x16 column. Blocks holds registry
// indices (16 bits, which halves the memory of cached chunks; NewOverworld
// refuses larger registries), index (y-MinY)<<8 | z<<4 | x. Biomes holds one biome ID per
// block, same index: BDS stores biomes per block (see fillBiomes).
type Chunk struct {
	Blocks [16 * 16 * Height]uint16
	Biomes [16 * 16 * Height]uint16
	// Water has one bit per block, same index as Blocks: set where the
	// block also holds source water (BDS's second block layer), as kelp,
	// seagrass and coral fans placed in the sea do.
	Water [16 * 16 * Height / 64]uint64
	// BlockEntities are the block entities of blocks structures placed
	// (beds, bells, chests...): the client draws these blocks from them.
	BlockEntities []structure.BlockEntity
}

// Waterlogged reports whether the block at local (x, z) and world y also
// holds source water.
func (c *Chunk) Waterlogged(x, y, z int) bool {
	i := (y-MinY)<<8 | z<<4 | x
	return c.Water[i>>6]&(1<<(i&63)) != 0
}

// Biome returns the biome ID at local (x, z) and world y.
func (c *Chunk) Biome(x, y, z int) uint32 {
	return uint32(c.Biomes[(y-MinY)<<8|z<<4|x])
}

// Block returns the state index at local (x, z) and world y.
func (c *Chunk) Block(x, y, z int) uint32 {
	return uint32(c.Blocks[(y-MinY)<<8|z<<4|x])
}

func (c *Chunk) set(x, y, z int, s uint32) {
	c.Blocks[(y-MinY)<<8|z<<4|x] = uint16(s)
}

// Option changes how an Overworld generates. Without options it matches BDS.
type Option func(*options)

type options struct {
	houseChests     bool
	houseChestsPick func(house string) bool
	cfg             *Config
}

// WithHouseChests gives village houses that have no chest in vanilla a
// chest, with its village's house loot table ("loot_tables/chests/village/
// village_<type>_house.json"). pick chooses the houses: it gets the house's
// template name ("village/plains/houses/plains_small_house_1") and returns
// true for those that get a chest; nil picks every house. Houses that have
// a chest in vanilla keep only that one, and farms, pens and other pieces
// with no inside get none. This departs from BDS, which leaves most houses
// without a chest. pick may be called from many goroutines at once.
func WithHouseChests(pick func(house string) bool) Option {
	return func(o *options) { o.houseChests, o.houseChestsPick = true, pick }
}

// Overworld generates overworld chunks for one world seed. GenerateChunk may
// be called from many goroutines at once.
type Overworld struct {
	opts options
	// skip and gates are the Config's step and structure changes.
	skip  Steps
	gates structGates
	// place is each structure kind's placement grid (see placementSlot).
	place [structKindCount]startGrid
	// forced are the PlaceAt starts by kind and placement region.
	forced    [structKindCount]map[[2]int32][2]int32
	anyForced bool
	// forcedStrongholds are the placed strongholds, sorted, so they are
	// visited in the same order on every run.
	forcedStrongholds [][2]int32
	// loot maps loot tables to the Config's replacements; swaps are each
	// kind's block swaps, by state; snapshots pools blockSnapshots.
	loot      map[string]string
	swaps     [structKindCount]map[uint32]uint32
	snapshots sync.Pool

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
	// joinKind sorts each block state for connectBlocks (joinFence,
	// joinPane, or 0 for blocks that take no connections).
	joinKind []uint8
	// villageBiome marks the biomes with a minecraft:village_type;
	// villageType holds its type.
	villageBiome []bool
	// vanillaVillageBiome is villageBiome before the Config's SetBiomes.
	vanillaVillageBiome []bool
	villageType         []string
	// scatterKind is each biome's scattered feature kind (scattered.go).
	scatterKind []uint8
	// ruinBiome is each biome's ocean ruin settings; monumentDeep and
	// monumentWater the monument biome sets; holdsLiquid the blocks that
	// can be waterlogged (oceanruin.go).
	ruinBiome []ruinBiome
	// ruinOcean is a plain ocean's ruin settings, for ruins the Config
	// places outside the ruin biomes.
	ruinOcean                   ruinBiome
	monumentDeep, monumentWater []bool
	holdsLiquid                 map[string]bool
	// topSupport marks the states whose top face holds plants.
	topSupport []bool
	// ruins holds each ruin start's pieces by start chunk.
	ruins sync.Map
	// shipBiome and shipBeach are the shipwreck biome sets; ships holds
	// each shipwreck start's layout by start chunk (shipwreck.go).
	shipBiome, shipBeach []bool
	ships                sync.Map
	// treasureBiome is the buried treasure biome set.
	treasureBiome []bool
	// monuments holds each ocean monument start's layout by start chunk.
	monuments sync.Map
	// outpostBiome is the pillager outpost biome set; outposts holds each
	// outpost start's pieces by start chunk.
	outpostBiome []bool
	outposts     sync.Map
	// portals holds each built ruined portal by start chunk.
	portals sync.Map
	// mansionBiome is the woodland mansion biome set; mansions holds each
	// mansion start's layout by start chunk.
	mansionBiome []bool
	mansions     sync.Map
	// shFirst holds the first three strongholds' chunks, found once;
	// strongholds holds each assembled stronghold by its start chunk.
	// dataJigsaws are the data-driven jigsaw structures (trail ruins,
	// trial chambers).
	dataJigsaws []*djDef
	shFirstOnce sync.Once
	shFirst     [][2]int32
	strongholds sync.Map
	// jigsaw is the village pools and elements; templates caches prepared
	// templates by path and villages assembled villages by start chunk.
	jigsaw *structure.JigsawData
	// st prepares the structure templates.
	st       *structure.Lib
	villages sync.Map
	// city is the ancient city pools and elements; cities holds assembled
	// cities by start chunk.
	city   *structure.JigsawData
	cities sync.Map
	// plainHeights caches surfaceNoStructures height maps by chunk.
	plainHeights lru.LRU[*[256]int16]
	// kernel is the beard kernel of terrain adjustments.
	kernel []float32
	// mineshafts holds assembled mineshafts by start chunk.
	mineshafts sync.Map
	// deepDark is the deep dark's biome ID; no mineshaft piece is built
	// there.
	deepDark uint32
	// cityBiome, when set by the Config, replaces the deep dark as the
	// ancient cities' biomes.
	cityBiome []bool
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
	// magma and bubbleColumn (drag_down) are 0 when the block list lacks them.
	magma, bubbleColumn uint32
	// carvable is the set of blocks cave carvers may cut.
	carvable map[uint32]struct{}
	// canyonCarvable is the set canyons may cut (a different list).
	canyonCarvable map[uint32]struct{}
	// veins are the copper (0) and iron (1) ore vein rows.
	veins [2]oreVein
}

func terracottaColours() [16]string {
	return [16]string{
		"white", "orange", "magenta", "light_blue", "yellow", "lime", "pink", "gray",
		"light_gray", "cyan", "purple", "blue", "brown", "green", "red", "black",
	}
}

// blockSet is a set of block states.
type blockSet = map[uint32]setMember

// setMember is the value of a set entry.
type setMember = struct{}

// caveCarverParams are the settings that differ between the overworld's
// two cave carvers: the feature's skip chance and its height limit.
type caveCarverParams struct {
	skip   uint32
	height int32
}

// overworldCaveCarvers are the underground_cave_carver_feature and
// underground_extra_cave_carver_feature settings, in order.
func overworldCaveCarvers() [2]caveCarverParams {
	return [2]caveCarverParams{{33, 236}, {66, 103}}
}

// resolveVein looks up the blocks of an ore vein row.
func resolveVein(reg *blocks.Registry, ore, raw, filler string, minY, maxY int32) (oreVein, error) {
	v := oreVein{minY: minY, maxY: maxY}
	err := reg.Resolve(
		blocks.Want{Dst: &v.ore, Name: "minecraft:" + ore},
		blocks.Want{Dst: &v.raw, Name: "minecraft:" + raw},
		blocks.Want{Dst: &v.filler, Name: "minecraft:" + filler},
	)
	return v, err
}

// NewOverworld builds the noise for a seed. reg must contain the vanilla
// blocks the generator places; biomeIDs maps biome names (no "minecraft:"
// prefix) to numeric IDs, as from biome.LoadIDs.
func NewOverworld(seed int64, reg *blocks.Registry, biomeIDs map[string]uint32, opts ...Option) (*Overworld, error) {
	if reg.Len() > 1<<16 {
		return nil, fmt.Errorf("worldgen: %d block states do not fit the 16-bit chunk storage", reg.Len())
	}
	o := &Overworld{seed: seed, reg: reg, shape: terrain.NewShape(), plainHeights: lru.New[*[256]int16](1024)}
	for _, opt := range opts {
		opt(&o.opts)
	}
	if o.opts.cfg == nil {
		o.opts.cfg = Vanilla()
	}
	cfg := o.opts.cfg
	o.skip = cfg.Skip
	var err error
	if o.gates, err = newStructGates(&cfg.Structures, cfg.Skip.Structures, cfg.Logger); err != nil {
		return nil, err
	}
	o.place = vanillaPlacements()
	entries, err := cfg.biomeEntries(biome.Entries(), biomeIDs)
	if err != nil {
		return nil, err
	}
	for name, id := range biomeIDs {
		if id >= 1<<16 {
			return nil, fmt.Errorf("worldgen: biome %s has ID %d, over the 16 bits PackedChunk stores", name, id)
		}
	}
	if miss := biome.Missing(biome.Entries(), biomeIDs); len(miss) > 0 {
		return nil, fmt.Errorf("worldgen: biomes missing from the biome list: %v", miss)
	}
	o.biomes = biome.NewTree(entries, biomeIDs)
	o.deepDark = math.MaxUint32
	if id, ok := biomeIDs["deep_dark"]; ok {
		o.deepDark = id
	}
	names := []blocks.Want{
		{Dst: &o.ids.air, Name: "minecraft:air"},
		{Dst: &o.ids.stone, Name: "minecraft:stone"},
		{Dst: &o.ids.deepslate, Name: "minecraft:deepslate"},
		{Dst: &o.ids.water, Name: blockWater},
		{Dst: &o.ids.bedrock, Name: "minecraft:bedrock"},
		{Dst: &o.ids.grass, Name: "minecraft:grass_block"},
		{Dst: &o.ids.dirt, Name: "minecraft:dirt"},
		{Dst: &o.ids.sand, Name: "minecraft:sand"},
		{Dst: &o.ids.gravel, Name: "minecraft:gravel"},
		{Dst: &o.ids.redSand, Name: "minecraft:red_sand"},
		{Dst: &o.ids.suspiciousSand, Name: "minecraft:suspicious_sand"},
		{Dst: &o.ids.sandstone, Name: "minecraft:sandstone"},
		{Dst: &o.ids.redSandstone, Name: "minecraft:red_sandstone"},
		{Dst: &o.ids.flowingWater, Name: "minecraft:flowing_water"},
		{Dst: &o.ids.lava, Name: "minecraft:lava"},
		{Dst: &o.ids.flowingLava, Name: "minecraft:flowing_lava"},
		{Dst: &o.ids.ice, Name: "minecraft:ice"},
		{Dst: &o.ids.packedIce, Name: "minecraft:packed_ice"},
		{Dst: &o.ids.blueIce, Name: "minecraft:blue_ice"},
		{Dst: &o.ids.snow, Name: "minecraft:snow"},
		{Dst: &o.ids.netherrack, Name: "minecraft:netherrack"},
		{Dst: &o.ids.coarseDirt, Name: "minecraft:coarse_dirt"},
		{Dst: &o.ids.mycelium, Name: "minecraft:mycelium"},
	}
	for i, c := range terracottaColours() {
		names = append(names, blocks.Want{Dst: &o.ids.terracotta[i], Name: "minecraft:" + c + "_terracotta"})
	}
	if err := reg.Resolve(names...); err != nil {
		return nil, err
	}
	o.ids.magma, _ = reg.Default("minecraft:magma")
	o.ids.bubbleColumn, _ = reg.Lookup("minecraft:bubble_column", map[string]any{"drag_down": true})
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
	// BDS sub_498AB80: the blocks cave carvers may cut.
	o.ids.carvable = blockSet{}
	for _, n := range carvableBlocks() {
		id, ok := reg.Default("minecraft:" + n)
		if !ok {
			return nil, fmt.Errorf("worldgen: block minecraft:%s missing from the block list", n)
		}
		o.ids.carvable[id] = setMember{}
	}
	o.ids.canyonCarvable = blockSet{}
	for _, n := range canyonCarvableBlocks() {
		id, ok := reg.Default("minecraft:" + n)
		if !ok {
			return nil, fmt.Errorf("worldgen: block minecraft:%s missing from the block list", n)
		}
		o.ids.canyonCarvable[id] = setMember{}
	}
	if o.ids.veins[0], err = resolveVein(reg, "copper_ore", "raw_copper_block", "granite", 0, 50); err != nil {
		return nil, err
	}
	if o.ids.veins[1], err = resolveVein(reg, "deepslate_iron_ore", "raw_iron_block", "tuff", -60, -8); err != nil {
		return nil, err
	}
	terrainRand := world.FromHash("minecraft:terrain")
	o.blended = noise.NewBlended(&terrainRand)
	deep := world.FromHash("minecraft:depth_based_layer")
	o.deepRand = deep.Fork()

	defs, err := biome.LoadDefs(worldgen.Data, "data/vanilla/biomes")
	if err != nil {
		return nil, err
	}
	if err := cfg.biomeDefs(defs); err != nil {
		return nil, err
	}
	o.surfNoise = newSurfaceNoises(seed, world)
	// The overworld's cave carver features (vanilla features
	// underground_cave_carver_feature and underground_extra_cave_carver_feature).
	for _, c := range overworldCaveCarvers() {
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
	o.joinKind = make([]uint8, reg.Len())
	for i := range o.names {
		st, _ := reg.State(uint32(i))
		o.names[i] = st.Name
		o.soilPlant[i] = soilPlants[st.Name]
		o.joinKind[i] = joinKindOf(st.Name)
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
	o.scatterKind = make([]uint8, maxBiome+1)
	for name, kind := range scatterBiomes {
		if id, ok := biomeIDs[name]; ok {
			o.scatterKind[id] = kind
		}
	}
	if err := o.initOceanStructures(biomeIDs, maxBiome+1); err != nil {
		return nil, err
	}
	for name, id := range biomeIDs {
		if d, ok := defs[name]; ok {
			o.biomeTags[id] = d.Tags
			o.villageBiome[id] = d.Village != ""
			o.villageType[id] = d.Village
		}
	}
	if o.st, err = structure.NewLib(seed, reg); err != nil {
		return nil, err
	}
	o.st.SetPacks(cfg.packs, cfg.Structures.templates)
	if o.jigsaw, err = structure.LoadJigsawData("villages.json", reg); err != nil {
		return nil, err
	}
	if o.city, err = structure.LoadJigsawData("ancient_city.json", reg); err != nil {
		return nil, err
	}
	o.kernel = beardKernel()
	for _, load := range []func() (*djDef, error){o.loadTrailRuins, o.loadTrialChambers, o.loadAbandonedCamps} {
		d, err := load()
		if err != nil {
			return nil, err
		}
		d.sk = djStructKind(d.kind)
		o.dataJigsaws = append(o.dataJigsaws, d)
	}
	if err := o.applyPlacements(&cfg.Structures); err != nil {
		return nil, err
	}
	features, err := o.loadFeatures(worldgen.Data, "data/vanilla/features", cfg.packs)
	if err != nil {
		return nil, err
	}
	if err := o.applyFeatures(cfg, features); err != nil {
		return nil, err
	}
	o.features = features
	if err := o.loadFeatureRules(worldgen.Data, "data/vanilla/feature_rules", cfg.packs, defs, biomeIDs, features); err != nil {
		return nil, err
	}
	if err := o.applyRules(cfg, defs, biomeIDs); err != nil {
		return nil, err
	}
	if err := o.applyStructureContents(&cfg.Structures); err != nil {
		return nil, err
	}
	o.vanillaVillageBiome = o.villageBiome
	if err := o.applyStructureBiomes(&cfg.Structures, biomeIDs); err != nil {
		return nil, err
	}
	return o, nil
}

// Seed returns the world seed.
func (o *Overworld) Seed() int64 { return o.seed }

// Registry returns the block list the generator places from.
func (o *Overworld) Registry() *blocks.Registry { return o.reg }

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
	if !o.skip.BedrockFloor {
		o.bedrockFloor(mt, c)
	} else {
		// The floor's draws come before the surface's from the same random.
		for range 16 * 16 {
			mt.IntN(bedrockFloorRange)
		}
	}
	o.fillBiomes(chunkX, chunkZ, c)
	if !o.skip.Surface {
		o.surface(chunkX, chunkZ, c, mt)
	}
	if !o.skip.Caves {
		for _, cv := range o.carvers {
			cv.carve(o, c, aq, chunkX, chunkZ)
		}
	}
	if !o.skip.Canyons {
		o.canyons(c, aq, chunkX, chunkZ)
	}
}

// bedrockFloorRange is the number of bedrock floor heights (2 to 5).
const bedrockFloorRange = 4

// floorBedrock reports whether b at height y is part of the bedrock floor.
// Hard rule: once the floor is drawn, no later step (structures, features,
// carvers) replaces it, so the border to the void is always whole.
func (o *Overworld) floorBedrock(b uint32, y int32) bool {
	return y < MinY+5 && b == o.ids.bedrock
}

// bedrockFloor draws 2-5 bedrock blocks per column from the chunk's
// Mersenne Twister, x outer and z inner.
func (o *Overworld) bedrockFloor(mt *rand.MT, c *Chunk) {
	for x := range 16 {
		for z := range 16 {
			n := int(mt.IntN(bedrockFloorRange)) + 2
			for y := MinY; y < MinY+n; y++ {
				c.set(x, y, z, o.ids.bedrock)
			}
		}
	}
}

// carvableBlocks are the blocks BDS's cave carvers may cut (sub_498AB80).
func carvableBlocks() []string {
	return []string{
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
}

// canyonCarvableBlocks are the blocks BDS's canyons may cut (list at
// 0xF60D710).
func canyonCarvableBlocks() []string {
	return []string{
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
	case blockSnowLayer, blockVine, blockSeagrass, "minecraft:kelp",
		"minecraft:waterlily", blockDeadBush, "minecraft:reeds", "minecraft:web",
		"minecraft:fire", blockTorch, "minecraft:brown_mushroom", "minecraft:red_mushroom":
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
