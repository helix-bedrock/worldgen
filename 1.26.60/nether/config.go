package nether

import (
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"path"
	"slices"
	"strings"

	"github.com/helix-bedrock/worldgen/1.26.60/internal/mathx"
	"github.com/helix-bedrock/worldgen/1.26.60/rand"
	"github.com/helix-bedrock/worldgen/1.26.60/structure"
)

// Option changes how the Nether generates. Without options it matches BDS.
type Option func(*options)

type options struct{ cfg *Config }

// WithConfig builds the generator with cfg's changes.
func WithConfig(cfg *Config) Option { return func(o *options) { o.cfg = cfg } }

// Config changes what the Nether generates; the zero Config and Vanilla's
// change nothing. Changes are checked by New: a name that matches nothing
// is an error. As in the overworld's Config, what a change leaves alone
// stays vanilla: a disabled rule still makes its random draws.
type Config struct {
	// Features turns the biomes' feature rules (fungi, vines, basalt,
	// fossils...) off.
	Features FeatureConfig
	// Biomes changes which biome generates where.
	Biomes BiomeConfig
	// Structures changes where structures generate.
	Structures StructureConfig
	// Skip turns whole generation steps off.
	Skip Steps
	// Logger receives the panics of structure filters. Nil drops them.
	Logger *slog.Logger

	packs     []fs.FS
	packPaths []string
}

// Vanilla returns a Config that changes nothing.
func Vanilla() *Config { return &Config{} }

// AddPack adds a behaviour pack whose structures folder replaces vanilla
// templates of the same path (structures/<path>.mcstructure or .nbt) and
// holds templates for ReplaceTemplate and AddToPool. Later packs win.
func (c *Config) AddPack(fsys fs.FS) { c.packs = append(c.packs, fsys) }

// Steps are generation steps that can be turned off. The zero value runs
// every step.
type Steps struct {
	// Surface skips the biome surfaces (nylium, soul sand, basalt...).
	Surface bool
	// Caves skips the cave carver.
	Caves bool
	// Structures skips fortresses, bastions and ruined portals.
	Structures bool
	// Decoration skips every decoration: fire, glowstone, ores, springs,
	// ancient debris and the feature rules.
	Decoration bool
}

// FeatureConfig holds the feature rule changes. Patterns are rule
// identifiers ("minecraft:" may be left out), with "*" for any run of
// characters.
type FeatureConfig struct{ ops []ruleOp }

type ruleOp struct {
	pattern string
	biomes  []string
}

// Disable turns the matching rules off in every biome.
func (f *FeatureConfig) Disable(patterns ...string) {
	for _, p := range patterns {
		f.ops = append(f.ops, ruleOp{pattern: p})
	}
}

// DisableIn turns the matching rules off in the given biomes ("hell",
// "crimson_forest", ...).
func (f *FeatureConfig) DisableIn(biomes []string, patterns ...string) {
	for _, p := range patterns {
		f.ops = append(f.ops, ruleOp{pattern: p, biomes: biomes})
	}
}

// BiomeConfig holds the biome changes. Biome names are as in the biome
// list ("hell", "soulsand_valley", "crimson_forest", "warped_forest",
// "basalt_deltas").
type BiomeConfig struct {
	remove  []string
	replace [][2]string
}

// Remove takes biomes out of the Nether: the nearest others in climate
// space fill their area.
func (b *BiomeConfig) Remove(names ...string) { b.remove = append(b.remove, names...) }

// Replace generates to wherever from would generate.
func (b *BiomeConfig) Replace(from, to string) { b.replace = append(b.replace, [2]string{from, to}) }

// Only generates one biome everywhere.
func (b *BiomeConfig) Only(name string) { b.replace = append(b.replace, [2]string{"*", name}) }

// StructureKind names a Nether structure.
type StructureKind string

// The Nether structure kinds.
const (
	StructureFortress     StructureKind = "minecraft:fortress"
	StructureBastion      StructureKind = "minecraft:bastion_remnant"
	StructureRuinedPortal StructureKind = "minecraft:ruined_portal"
)

// StructureKinds lists the Nether structure kinds.
func StructureKinds() []StructureKind {
	return []StructureKind{StructureFortress, StructureBastion, StructureRuinedPortal}
}

// The kinds' gate indices, in StructureKinds order.
const (
	skFortress = iota
	skBastion
	skPortal
	structKindCount
)

// StructureFilter decides whether a structure may start in a chunk; see the
// overworld's StructureFilter.
type StructureFilter func(kind StructureKind, chunkX, chunkZ int32) bool

func (f StructureFilter) forGate(kind string, cx, cz int32) bool {
	return f(StructureKind(kind), cx, cz)
}

// StructureConfig holds the structure changes.
type StructureConfig struct {
	disabled  []StructureKind
	excludes  []structExclude
	filters   []StructureFilter
	forced    []forcedStart
	loot      map[string]string
	templates map[string]string
	pools     []structure.PoolEdit
	swaps     []blockSwap
}

type blockSwap struct {
	kind     StructureKind
	from, to string
}

// ReplaceTemplate builds instead wherever a structure uses template
// (bastion and ruined portal templates, as "bastion/bridge/starting_pieces/entrance").
func (s *StructureConfig) ReplaceTemplate(template, instead string) {
	if s.templates == nil {
		s.templates = map[string]string{}
	}
	s.templates[template] = instead
}

// SetPoolWeight sets the weight of an element (by ID or template path) in
// a bastion jigsaw pool; 0 takes it out.
func (s *StructureConfig) SetPoolWeight(pool, element string, weight int32) {
	s.pools = append(s.pools, structure.PoolEdit{Pool: pool, Element: element, Weight: weight})
}

// AddToPool adds a template to a bastion jigsaw pool with a weight.
func (s *StructureConfig) AddToPool(pool, template string, weight int32) {
	s.pools = append(s.pools, structure.PoolEdit{Pool: pool, Element: template, Weight: weight, Add: true})
}

// ReplaceBlock swaps the block from for to in the blocks kind places,
// keeping the properties both share.
func (s *StructureConfig) ReplaceBlock(kind StructureKind, from, to string) {
	s.swaps = append(s.swaps, blockSwap{kind, from, to})
}

type structExclude struct {
	area  structure.Area
	kinds []StructureKind
}

type forcedStart struct {
	kind   StructureKind
	cx, cz int32
}

// Disable turns the kinds off. Fortresses and bastions share one grid: a
// disabled fortress does not become a bastion.
func (s *StructureConfig) Disable(kinds ...StructureKind) { s.disabled = append(s.disabled, kinds...) }

// Exclude stops structures of the kinds (every kind when none are given)
// starting in the area.
func (s *StructureConfig) Exclude(area structure.Area, kinds ...StructureKind) {
	s.excludes = append(s.excludes, structExclude{area, kinds})
}

// Filter adds a filter every structure start must pass.
func (s *StructureConfig) Filter(f StructureFilter) { s.filters = append(s.filters, f) }

// PlaceAt starts a structure in chunk (chunkX, chunkZ), in place of the
// vanilla start of its placement region (fortresses and bastions share
// theirs), skipping the biome checks.
func (s *StructureConfig) PlaceAt(kind StructureKind, chunkX, chunkZ int32) {
	s.forced = append(s.forced, forcedStart{kind, chunkX, chunkZ})
}

// ReplaceLoot gives containers with loot table from the table to instead.
func (s *StructureConfig) ReplaceLoot(from, to string) {
	if s.loot == nil {
		s.loot = map[string]string{}
	}
	s.loot[from] = to
}

// structIndex is a kind's gate index.
func structIndex(k StructureKind) (int, error) {
	id := string(k)
	if !strings.Contains(id, ":") {
		id = "minecraft:" + id
	}
	i := slices.Index(StructureKinds(), StructureKind(id))
	if i < 0 {
		return 0, fmt.Errorf("worldgen: unknown Nether structure %q (one of %v)", k, StructureKinds())
	}
	return i, nil
}

// applyConfig resolves cfg into n, after the vanilla parts are loaded.
func (n *Nether) applyConfig(cfg *Config, biomeIDs map[string]uint32) error {
	n.skip = cfg.Skip
	names := make([]string, structKindCount)
	for i, k := range StructureKinds() {
		names[i] = string(k)
	}
	n.gate = structure.NewGate(names, cfg.Logger)
	sc := &cfg.Structures
	for _, k := range sc.disabled {
		i, err := structIndex(k)
		if err != nil {
			return err
		}
		n.gate.Disable(i)
	}
	if cfg.Skip.Structures {
		for i := range structKindCount {
			n.gate.Disable(i)
		}
	}
	for _, e := range sc.excludes {
		var idx []int
		for _, k := range e.kinds {
			i, err := structIndex(k)
			if err != nil {
				return err
			}
			idx = append(idx, i)
		}
		n.gate.Exclude(e.area, idx...)
	}
	for _, f := range sc.filters {
		if f == nil {
			return fmt.Errorf("worldgen: nil structure filter")
		}
		if err := n.gate.AddFilter(f.forGate); err != nil {
			return err
		}
	}
	for _, f := range sc.forced {
		k, err := structIndex(f.kind)
		if err != nil {
			return err
		}
		if n.gate.Disabled(k) {
			return fmt.Errorf("worldgen: %s is placed at chunk %d %d but disabled", f.kind, f.cx, f.cz)
		}
		reg, grid := n.regionOf(k, f.cx, f.cz)
		if at, ok := n.forced[grid][reg]; ok && at.chunk != [2]int32{f.cx, f.cz} {
			return fmt.Errorf("worldgen: %s at chunk %d %d shares its placement region with a start placed at chunk %d %d",
				f.kind, f.cx, f.cz, at.chunk[0], at.chunk[1])
		}
		if n.forced[grid] == nil {
			n.forced[grid] = map[[2]int32]forcedAt{}
		}
		n.forced[grid][reg] = forcedAt{kind: k, chunk: [2]int32{f.cx, f.cz}}
	}
	n.loot = maps.Clone(sc.loot)
	if err := structure.CheckTemplates(sc.templates, n.st); err != nil {
		return err
	}
	if len(sc.pools) > 0 {
		pools := map[string][]*structure.Pool{}
		for name, p := range n.bastionData.Pools {
			pools[name] = []*structure.Pool{p}
		}
		if err := structure.EditPools(pools, sc.pools, n.st); err != nil {
			return err
		}
	}
	for _, w := range sc.swaps {
		k, err := structIndex(w.kind)
		if err != nil {
			return err
		}
		if n.swaps[k] == nil {
			n.swaps[k] = map[uint32]uint32{}
		}
		if err := structure.SwapTable(n.reg, w.from, w.to, n.swaps[k]); err != nil {
			return err
		}
	}
	if err := n.applyBiomes(&cfg.Biomes, biomeIDs); err != nil {
		return err
	}
	return n.applyRules(&cfg.Features, biomeIDs)
}

// forcedAt is a start placed by the Config.
type forcedAt struct {
	kind  int
	chunk [2]int32
}

// The placement grids placed starts are kept by: fortresses and bastions
// share one.
const (
	gridComplex = iota
	gridPortal
	gridCount
)

// regionOf is chunk (cx, cz)'s placement region on kind k's grid.
func (n *Nether) regionOf(k int, cx, cz int32) ([2]int32, int) {
	if k == skPortal {
		sp := int32(structure.NetherPortalSpacing)
		return [2]int32{mathx.FloorDiv(cx, sp), mathx.FloorDiv(cz, sp)}, gridPortal
	}
	return [2]int32{mathx.FloorDiv(cx, complexSpacing), mathx.FloorDiv(cz, complexSpacing)}, gridComplex
}

// forcedIn returns the placed start of a region of a grid.
func (n *Nether) forcedIn(grid int, rx, rz int32) (forcedAt, bool) {
	if n.forced[grid] == nil {
		return forcedAt{}, false
	}
	at, ok := n.forced[grid][[2]int32{rx, rz}]
	return at, ok
}

// complexPick is complexStart with the Config's placed starts: the region's
// start chunk, seeding r as the start goes on.
func (n *Nether) complexPick(rx, rz int32, r *rand.MT) (int32, int32) {
	sx, sz := n.complexStart(rx, rz, r)
	if at, ok := n.forcedIn(gridComplex, rx, rz); ok {
		return at.chunk[0], at.chunk[1]
	}
	return sx, sz
}

// applyBiomes applies the biome changes to the biome source.
func (n *Nether) applyBiomes(b *BiomeConfig, biomeIDs map[string]uint32) error {
	for _, name := range b.remove {
		id, err := n.netherBiomeID(biomeIDs, name)
		if err != nil {
			return err
		}
		n.biomes.biomes = slices.DeleteFunc(n.biomes.biomes, func(nb netherBiome) bool { return nb.id == id })
		if len(n.biomes.biomes) == 0 {
			return fmt.Errorf("worldgen: removing %q leaves no Nether biome", name)
		}
	}
	for _, r := range b.replace {
		to, err := n.netherBiomeID(biomeIDs, r[1])
		if err != nil {
			return err
		}
		if n.biomes.remap == nil {
			n.biomes.remap = map[uint32]uint32{}
		}
		if r[0] == "*" {
			for _, id := range n.netherBiomeIDs {
				n.biomes.remap[id] = to
			}
			continue
		}
		from, err := n.netherBiomeID(biomeIDs, r[0])
		if err != nil {
			return err
		}
		n.biomes.remap[from] = to
	}
	return nil
}

// netherBiomeID is the ID of a Nether biome by name.
func (n *Nether) netherBiomeID(biomeIDs map[string]uint32, name string) (uint32, error) {
	id, ok := biomeIDs[strings.TrimPrefix(name, "minecraft:")]
	if !ok || !slices.Contains(n.netherBiomeIDs, id) {
		return 0, fmt.Errorf("worldgen: unknown Nether biome %q", name)
	}
	return id, nil
}

// applyRules applies the rule changes.
func (n *Nether) applyRules(f *FeatureConfig, biomeIDs map[string]uint32) error {
	var ids []string
	for _, passes := range n.rules {
		for _, rules := range passes {
			for _, r := range rules {
				if !slices.Contains(ids, r.id) {
					ids = append(ids, r.id)
				}
			}
		}
	}
	slices.Sort(ids)
	for _, op := range f.ops {
		pattern := op.pattern
		if !strings.Contains(pattern, ":") {
			pattern = "minecraft:" + pattern
		}
		matched := false
		var only map[uint32]bool
		if op.biomes != nil {
			only = map[uint32]bool{}
			for _, b := range op.biomes {
				id, ok := biomeIDs[strings.TrimPrefix(b, "minecraft:")]
				if !ok {
					return fmt.Errorf("worldgen: unknown biome %q", b)
				}
				only[id] = true
			}
		}
		for b, passes := range n.rules {
			for _, rules := range passes {
				for i, r := range rules {
					ok, err := path.Match(pattern, r.id)
					if err != nil {
						return fmt.Errorf("worldgen: rule pattern %q: %w", pattern, err)
					}
					if !ok {
						continue
					}
					matched = true
					if only != nil && !only[b] {
						continue
					}
					// A copy, so other biomes keep the rule.
					off := *r
					off.off = true
					rules[i] = &off
				}
			}
		}
		if !matched {
			return fmt.Errorf("worldgen: unknown Nether feature rule %q (the rules are %v)", pattern, ids)
		}
	}
	return nil
}

// swapped runs place, which writes structure kind k into c, then swaps
// the blocks it changed by k's block swaps.
func (n *Nether) swapped(k int, c *Chunk, place func()) {
	if n.swaps[k] == nil {
		place()
		return
	}
	before := c.Blocks
	place()
	structure.SwapChanged(c.Blocks[:], before[:], n.swaps[k])
}

// finishChunk applies the loot table swaps to a finished chunk.
func (n *Nether) finishChunk(c *Chunk) {
	if len(n.loot) == 0 {
		return
	}
	for i := range c.BlockEntities {
		be := &c.BlockEntities[i]
		to, ok := n.loot[be.LootTable]
		if !ok {
			continue
		}
		be.Data = maps.Clone(be.Data)
		be.LootTable = to
		if _, ok := be.Data["LootTable"]; ok {
			be.Data["LootTable"] = to
		}
	}
}
