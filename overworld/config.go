package overworld

import (
	"io/fs"
	"log/slog"
	"strings"
)

// Config changes what an Overworld generates. The zero Config, and the one
// Vanilla returns, generate exactly what BDS does; every change starts from
// that and departs from it only where asked. Changes are checked when the
// generator is built: a name that matches nothing is an error, with the
// closest known names in it.
//
// A Config is used once, by NewOverworld (through WithConfig), and is read
// there: changing it later has no effect on a generator already built.
// It is not safe for concurrent use while it is being filled in.
//
// What a change leaves alone stays vanilla. A disabled feature rule still
// makes the random draws it makes in BDS, and a disabled structure still
// counts for the structures that keep their distance from it, so the
// features and structures around it land where they land in vanilla. Two
// things do move: features placed after a removed one see the blocks it no
// longer placed, and the few features that draw from the chunk's shared
// population random (Molang math.random in a scatter feature, sculk) draw
// differently after a feature that used it is gone.
type Config struct {
	// Features changes feature rules (where trees, flowers, ores... are
	// placed) and features (what is placed).
	Features FeatureConfig
	// Biomes changes which biome generates where.
	Biomes BiomeConfig
	// Structures changes where structures generate.
	Structures StructureConfig
	// Skip turns whole generation steps off.
	Skip Steps
	// Terrain changes the terrain's settings.
	Terrain Terrain
	// Logger receives the panics of user code (Feature, StructureFilter),
	// which are recovered so one bad plugin cannot stop the server. Nil
	// drops them.
	Logger *slog.Logger

	packs []fs.FS
	// packPaths are the packs' folder paths when read by ParseConfig.
	packPaths []string
}

// Vanilla returns a Config that changes nothing: the world BDS generates.
func Vanilla() *Config { return &Config{} }

// WithConfig builds the generator with cfg's changes.
func WithConfig(cfg *Config) Option {
	return func(o *options) { o.cfg = cfg }
}

// AddPack adds a behaviour pack's world generation files, in Mojang's
// add-on format: features/*.json and feature_rules/*.json at the root of
// fsys. A pack feature or rule with the identifier of a vanilla one
// replaces it (a rule keeps the vanilla rule's place in the order); new
// ones are added after the vanilla ones, in file name order. Packs added
// later win over earlier ones.
func (c *Config) AddPack(fsys fs.FS) { c.packs = append(c.packs, fsys) }

// Steps are generation steps that can be turned off. The zero value runs
// every step, as BDS does.
type Steps struct {
	// Caves skips the cave carvers (the tunnels BDS carves after the
	// surface, not the noise caves of the terrain).
	Caves bool
	// Canyons skips the canyon (ravine) carver.
	Canyons bool
	// BedrockFloor skips the bedrock at the bottom of the world.
	BedrockFloor bool
	// Surface skips the biome surfaces: the ground stays stone, with no
	// grass, sand, dirt or terracotta on top.
	Surface bool
	// Structures skips every structure.
	Structures bool
	// Decoration skips every feature rule, lakes and dungeons: no trees,
	// plants, ores or rocks. Snow and ice still form.
	Decoration bool
	// Freeze skips the snow and ice the climate puts on top.
	Freeze bool
}

// Terrain are terrain settings. The zero value is vanilla.
type Terrain struct {
	// SeaLevel is the height the sea and the surface aquifers fill up to
	// (exclusive); 0 keeps vanilla's 63. The land's shape does not change,
	// and surfaces, structures and features keep vanilla's sea level for
	// their own checks.
	SeaLevel int32
	// SeaBlock replaces the water of the sea and the aquifers
	// ("minecraft:lava", "minecraft:air"); empty keeps water.
	SeaBlock string
	// NoDeepslate makes the deep ground stone, so ores are their stone
	// kinds there too.
	NoDeepslate bool
	// NoOreVeins leaves out the big copper and iron ore veins.
	NoOreVeins bool
}

// Pass is a feature placement pass. Passes run in this order; rules of one
// pass run in rule order, biome by biome.
type Pass int

// The placement passes, in BDS's order.
const (
	FirstPass Pass = iota
	UndergroundPass
	AfterUndergroundPass
	BeforeSurfacePass
	SurfacePass
	AfterSurfacePass
	BeforeSkyPass
	SkyPass
	AfterSkyPass
	FinalPass
)

// FeatureConfig holds the feature and feature rule changes. Rule and
// feature IDs are Mojang identifiers; "minecraft:" may be left out. Where
// a method takes patterns, "*" matches any run of characters, as in
// "*birch*".
type FeatureConfig struct {
	ops []featureOp
	// registered are the Go features by ID.
	registered map[string]Feature
	rules      []Rule
}

type featureOpKind uint8

const (
	opDisableRule featureOpKind = iota
	opDisableRuleIn
	opDisableFeature
	opSetChance
	opSetIterations
	opReplace
	opSetWeight
)

// featureOp is one recorded feature change, applied in order.
type featureOp struct {
	kind    featureOpKind
	pattern string
	biomes  []string
	feature string
	value   float32
}

// Disable turns off the rules that match the patterns, in every biome.
func (f *FeatureConfig) Disable(patterns ...string) {
	for _, p := range patterns {
		f.ops = append(f.ops, featureOp{kind: opDisableRule, pattern: p})
	}
}

// DisableIn turns off the rules that match the patterns in the given
// biomes only (biome names as "plains").
func (f *FeatureConfig) DisableIn(biomes []string, patterns ...string) {
	for _, p := range patterns {
		f.ops = append(f.ops, featureOp{kind: opDisableRuleIn, pattern: p, biomes: biomes})
	}
}

// DisableFeature turns off the features that match the patterns wherever
// they are placed, also as part of other features: disabling
// "minecraft:birch_tree_feature" removes birch trees from every forest
// mix. A mix that picks a disabled feature places nothing that time; to
// pick another feature instead, set its weight to 0 with SetWeight.
func (f *FeatureConfig) DisableFeature(patterns ...string) {
	for _, p := range patterns {
		f.ops = append(f.ops, featureOp{kind: opDisableFeature, pattern: p})
	}
}

// SetChance sets the chance, in percent (0 to 100), that the matching rules
// run in a chunk. It replaces the rule's scatter_chance.
func (f *FeatureConfig) SetChance(pattern string, percent float32) {
	f.ops = append(f.ops, featureOp{kind: opSetChance, pattern: pattern, value: percent})
}

// SetIterations sets how many times the matching rules place their feature
// in a chunk. A rule whose vanilla count is Molang still evaluates it, for
// its random draws, before the count is replaced.
func (f *FeatureConfig) SetIterations(pattern string, n int32) {
	f.ops = append(f.ops, featureOp{kind: opSetIterations, pattern: pattern, value: float32(n)})
}

// Replace makes the matching rules place feature instead of their own, at
// the same positions and chances.
func (f *FeatureConfig) Replace(pattern, feature string) {
	f.ops = append(f.ops, featureOp{kind: opReplace, pattern: pattern, feature: feature})
}

// SetWeight sets the weight of feature in the weighted random feature mix
// (a minecraft:weighted_random_feature, such as a forest's tree mix). A
// feature not in the mix is added to it. Weight 0 takes it out of the
// pick.
func (f *FeatureConfig) SetWeight(mix, feature string, weight float32) {
	f.ops = append(f.ops, featureOp{kind: opSetWeight, pattern: mix, feature: feature, value: weight})
}

// Register adds a feature written in Go under id ("myserver:crystal").
// Rules, packs and other features place it by that ID. Registering a
// vanilla ID replaces the vanilla feature everywhere it is placed.
func (f *FeatureConfig) Register(id string, feat Feature) {
	if f.registered == nil {
		f.registered = map[string]Feature{}
	}
	f.registered[fullID(id)] = feat
}

// AddRule adds a feature rule. It runs after the vanilla rules of its pass.
func (f *FeatureConfig) AddRule(r Rule) { f.rules = append(f.rules, r) }

// Rule is a feature rule: where, how often and in which biomes a feature is
// placed, as a feature_rules file says it.
type Rule struct {
	// ID names the rule ("myserver:crystal_spikes"). Its hash seeds the
	// rule's random, so two rules never share draws.
	ID string
	// Places is the feature placed.
	Places string
	// Pass is the placement pass.
	Pass Pass
	// Biomes are the biome names the rule runs in; Tags adds the biomes
	// that have any of these tags. With neither, the rule runs everywhere.
	Biomes, Tags []string
	// Iterations is the number of placements per chunk; 0 means 1.
	Iterations int32
	// Percent is the chance in percent that the rule runs in a chunk; 0
	// means 100 (always). To turn a rule off, use Disable.
	Percent float32
	// X, Y and Z give each placement's position. The zero X and Z spread
	// over the chunk (Uniform(0, 16)); the zero Y is the heightmap.
	X, Y, Z Axis
}

// Axis is how one coordinate of a placement is chosen.
type Axis struct {
	set bool
	a   axis
}

// Uniform picks a value in [min, max): for X and Z an offset from the
// chunk's corner, for Y a height.
func Uniform(minimum, maximum int32) Axis {
	return Axis{set: true, a: axis{kind: distUniform, min: minimum, max: maximum}}
}

// Fixed always gives v: for X and Z an offset from the chunk's corner, for
// Y a height.
func Fixed(v int32) Axis { return Axis{set: true, a: axis{kind: distConstant, min: v, max: v}} }

// Heightmap is the height of the column's top block, for Y.
func Heightmap() Axis { return Axis{set: true, a: axis{kind: distHeightmap}} }

// AboveTopSolid is the height above the column's top solid block, for Y.
func AboveTopSolid() Axis { return Axis{set: true, a: axis{kind: distAboveTopSolid}} }

// BiomeConfig holds the biome changes. Biome names are as in the biome list
// ("plains"); "minecraft:" may be left out.
type BiomeConfig struct {
	ops   []biomeOp
	added []Biome
}

type biomeOpKind uint8

const (
	opRemoveBiome biomeOpKind = iota
	opReplaceBiome
	opScaleBiome
	opSetTags
)

type biomeOp struct {
	kind     biomeOpKind
	name, to string
	factor   float32
	tags     []string
}

// Remove takes the biomes out of the climate table: the neighbouring
// biomes in climate space fill their area.
func (b *BiomeConfig) Remove(names ...string) {
	for _, n := range names {
		b.ops = append(b.ops, biomeOp{kind: opRemoveBiome, name: n})
	}
}

// Replace generates biome to wherever from would generate. The terrain
// shape stays (it does not depend on the biome); the surface, features
// and structures become to's.
func (b *BiomeConfig) Replace(from, to string) {
	b.ops = append(b.ops, biomeOp{kind: opReplaceBiome, name: from, to: to})
}

// Only generates one biome everywhere.
func (b *BiomeConfig) Only(name string) {
	b.ops = append(b.ops, biomeOp{kind: opReplaceBiome, name: "*", to: name})
}

// Scale grows (factor > 1) or shrinks (factor < 1) a biome's climate boxes
// around their centres, so it takes more or less of the world. Biomes are
// picked by the nearest climate box, not by a weight, so this is not an
// exact percentage.
func (b *BiomeConfig) Scale(name string, factor float32) {
	b.ops = append(b.ops, biomeOp{kind: opScaleBiome, name: name, factor: factor})
}

// SetTags replaces a biome's tags. Feature rules pick biomes by tag, so
// this changes which vanilla features grow in it.
func (b *BiomeConfig) SetTags(name string, tags ...string) {
	b.ops = append(b.ops, biomeOp{kind: opSetTags, name: name, tags: tags})
}

// Add adds a biome of your own. Its name must be in the biome ID map given
// to NewOverworld, with the ID the client knows it by.
func (b *BiomeConfig) Add(biome Biome) { b.added = append(b.added, biome) }

// Biome is a biome of your own.
type Biome struct {
	// Name is the biome's name ("myserver:crystal_plains").
	Name string
	// Like is the vanilla biome it copies its surface, temperature, tags
	// and village type from.
	Like string
	// Tags, when not nil, replaces the copied tags.
	Tags []string
	// Climate is where the biome generates. Each range is in the climate
	// noise's [-1, 1]; Depth is 0 at the surface and 1 deep below. The
	// zero Range is the whole axis.
	Climate Climate
}

// Climate is a box in climate space.
type Climate struct {
	Temperature, Humidity, Continentalness, Erosion, Depth, Weirdness Range
	// Offset makes the biome lose against overlapping boxes; 0 for most.
	Offset float32
}

// Range is a closed interval; the zero Range means [-1, 1].
type Range struct{ Min, Max float32 }

// StructureKind names a structure, as the /locate command does.
type StructureKind string

// The structure kinds.
const (
	StructureVillage         StructureKind = "minecraft:village"
	StructureMineshaft       StructureKind = "minecraft:mineshaft"
	StructureStronghold      StructureKind = "minecraft:stronghold"
	StructureAncientCity     StructureKind = "minecraft:ancient_city"
	StructureDesertPyramid   StructureKind = "minecraft:desert_pyramid"
	StructureJungleTemple    StructureKind = "minecraft:jungle_temple"
	StructureSwampHut        StructureKind = "minecraft:swamp_hut"
	StructureIgloo           StructureKind = "minecraft:igloo"
	StructurePillagerOutpost StructureKind = "minecraft:pillager_outpost"
	StructureMansion         StructureKind = "minecraft:mansion"
	StructureMonument        StructureKind = "minecraft:monument"
	StructureOceanRuin       StructureKind = "minecraft:ocean_ruin"
	StructureShipwreck       StructureKind = "minecraft:shipwreck"
	StructureBuriedTreasure  StructureKind = "minecraft:buried_treasure"
	StructureRuinedPortal    StructureKind = "minecraft:ruined_portal"
	StructureTrailRuins      StructureKind = "minecraft:trail_ruins"
	StructureTrialChambers   StructureKind = "minecraft:trial_chambers"
	StructureAbandonedCamp   StructureKind = "minecraft:abandoned_camp"
)

// StructureKinds lists every structure kind the generator builds.
func StructureKinds() []StructureKind {
	return []StructureKind{StructureVillage, StructureMineshaft, StructureStronghold, StructureAncientCity, StructureDesertPyramid, StructureJungleTemple, StructureSwampHut,
		StructureIgloo, StructurePillagerOutpost, StructureMansion, StructureMonument, StructureOceanRuin, StructureShipwreck, StructureBuriedTreasure, StructureRuinedPortal,
		StructureTrailRuins, StructureTrialChambers, StructureAbandonedCamp}
}

// StructureConfig holds the structure changes.
type StructureConfig struct {
	disabled   []StructureKind
	excludes   []structExclude
	filters    []StructureFilter
	placements []kindPlacement
	forced     []forcedStart
	biomes     []kindBiomes
	templates  map[string]string
	pools      []poolEdit
	loot       map[string]string
	swaps      []blockSwap
}

type poolEdit struct {
	pool, element string
	weight        int32
	add           bool
}

type blockSwap struct {
	kind     StructureKind
	from, to string
}

// ReplaceTemplate builds template with instead wherever a structure uses
// it (paths as "village/plains/houses/plains_small_house_1"). instead may
// be a vanilla template or one from a pack's structures folder: AddPack
// also replaces a vanilla template outright when the pack has a file of
// the same path (structures/<path>.mcstructure or .nbt).
func (s *StructureConfig) ReplaceTemplate(template, instead string) {
	if s.templates == nil {
		s.templates = map[string]string{}
	}
	s.templates[template] = instead
}

// SetPoolWeight sets the weight of an element in a jigsaw pool (village,
// ancient city, trail ruins, trial chambers and camp pools, as
// "minecraft:village/plains/houses"). The element is named by its ID
// ("minecraft:plains_small_house_1") or template path. Weight 0 takes it
// out: a village without blacksmiths, say.
func (s *StructureConfig) SetPoolWeight(pool, element string, weight int32) {
	s.pools = append(s.pools, poolEdit{pool: pool, element: element, weight: weight})
}

// AddToPool adds a template (vanilla, or from a pack's structures folder)
// to a jigsaw pool with a weight, so it can be picked like the pool's own
// pieces. It is placed like the pool's first element (rigid or terrain
// matching), with no block rules. Its jigsaw blocks connect it like any
// other piece.
func (s *StructureConfig) AddToPool(pool, template string, weight int32) {
	s.pools = append(s.pools, poolEdit{pool: pool, element: template, weight: weight, add: true})
}

// ReplaceLoot gives the containers that would get loot table from the
// loot table to instead (Bedrock paths, as
// "loot_tables/chests/village/village_plains_house.json").
func (s *StructureConfig) ReplaceLoot(from, to string) {
	if s.loot == nil {
		s.loot = map[string]string{}
	}
	s.loot[from] = to
}

// ReplaceBlock swaps the block from for to in the blocks kind places
// ("minecraft:cobblestone" for "minecraft:mossy_cobblestone"); states
// keep the properties both blocks share (stairs keep their facing). It
// covers what the structure itself writes, not the features it grows
// (village trees), and a block the structure places over the same block
// is left as it is.
func (s *StructureConfig) ReplaceBlock(kind StructureKind, from, to string) {
	s.swaps = append(s.swaps, blockSwap{kind, from, to})
}

type kindPlacement struct {
	kind StructureKind
	p    Placement
}

type forcedStart struct {
	kind   StructureKind
	cx, cz int32
}

// Placement is a structure kind's placement grid. The world is cut into
// regions of Spacing x Spacing chunks; each region has one start, drawn
// from a random salted with Salt, at least Separation chunks from the
// region's far edges. Larger spacing makes the kind rarer, larger
// separation keeps starts further apart. Mineshafts have no grid: Chance
// is their chance in each chunk. A zero field keeps the vanilla value.
//
// The four scattered kinds (desert pyramids, jungle temples, swamp huts,
// igloos) share one grid: setting any of them sets it for all four.
// Strongholds are placed in rings and have no Placement.
type Placement struct {
	Spacing    int32   `json:"spacing,omitempty"`
	Separation int32   `json:"separation,omitempty"`
	Salt       int32   `json:"salt,omitempty"`
	Chance     float32 `json:"chance,omitempty"`
}

// SetBiomes replaces the biomes a kind may start in (biome names as
// "plains"). Villages in a biome without a village type are plains
// villages; ocean ruins in a biome without ruins are a plain ocean's.
// Ruined portals, mineshafts and strongholds have no biome list.
func (s *StructureConfig) SetBiomes(kind StructureKind, biomes ...string) {
	s.biomes = append(s.biomes, kindBiomes{kind, biomes})
}

type kindBiomes struct {
	kind   StructureKind
	biomes []string
}

// SetPlacement changes a kind's placement grid.
func (s *StructureConfig) SetPlacement(kind StructureKind, p Placement) {
	s.placements = append(s.placements, kindPlacement{kind, p})
}

// PlaceAt starts a structure of kind in chunk (chunkX, chunkZ). It takes
// the place of the vanilla start of the chunk's placement region (so it
// keeps the kind's spacing), and it skips the biome and terrain checks:
// a village placed in a desert is a desert village, one placed in an
// ocean is a plains village built on the sea floor. Exclusions and filters
// do not apply to it; a disabled kind is an error. Mineshafts may be
// placed anywhere; a placed stronghold is one more stronghold.
func (s *StructureConfig) PlaceAt(kind StructureKind, chunkX, chunkZ int32) {
	s.forced = append(s.forced, forcedStart{kind, chunkX, chunkZ})
}

type structExclude struct {
	area  Area
	kinds []StructureKind
}

// Area is a box of block columns, corners included.
type Area struct{ MinX, MinZ, MaxX, MaxZ int32 }

// Box returns the area between two corners, in any order.
func Box(x0, z0, x1, z1 int32) Area {
	return Area{min(x0, x1), min(z0, z1), max(x0, x1), max(z0, z1)}
}

func (a Area) holdsChunk(cx, cz int32) bool {
	x, z := cx*16+8, cz*16+8
	return x >= a.MinX && x <= a.MaxX && z >= a.MinZ && z <= a.MaxZ
}

// StructureFilter decides whether a structure may start in chunk
// (chunkX, chunkZ). It must give the same answer every time for the same
// arguments, and it may be called from many goroutines at once: chunks
// are generated in any order and each asks about the starts near it.
type StructureFilter func(kind StructureKind, chunkX, chunkZ int32) bool

// Disable turns the kinds off. Structures that keep their distance from a
// kind still do, so they stay where they are in vanilla.
func (s *StructureConfig) Disable(kinds ...StructureKind) {
	s.disabled = append(s.disabled, kinds...)
}

// Exclude stops structures of the kinds starting in the area (a start is
// in it when its chunk's centre is); no kinds means every kind. A
// structure that starts outside can still reach in.
func (s *StructureConfig) Exclude(area Area, kinds ...StructureKind) {
	s.excludes = append(s.excludes, structExclude{area, kinds})
}

// Filter adds a filter every structure start must pass.
func (s *StructureConfig) Filter(f StructureFilter) { s.filters = append(s.filters, f) }

// fullID adds "minecraft:" to an identifier without a namespace.
func fullID(id string) string {
	if strings.Contains(id, ":") {
		return id
	}
	return "minecraft:" + id
}

// biomeName strips "minecraft:" from a biome name, as the biome list keys
// have none.
func biomeName(n string) string { return strings.TrimPrefix(n, "minecraft:") }
