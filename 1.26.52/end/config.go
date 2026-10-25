package end

import (
	"fmt"
	"io/fs"
	"log/slog"
	"maps"

	"github.com/helix-bedrock/worldgen/1.26.52/structure"
)

// Option changes how the End generates. Without options it matches BDS.
type Option func(*options)

type options struct{ cfg *Config }

// WithConfig builds the generator with cfg's changes.
func WithConfig(cfg *Config) Option { return func(o *options) { o.cfg = cfg } }

// Config changes what the End generates; the zero Config and Vanilla's
// change nothing. What a change leaves alone stays vanilla: a skipped
// feature still makes its random draws, and the features after it see its
// blocks, so they land where they do in vanilla.
type Config struct {
	// Structures changes where End cities generate.
	Structures StructureConfig
	// Skip turns generation steps off.
	Skip Steps
	// Logger receives the panics of structure filters. Nil drops them.
	Logger *slog.Logger

	packs     []fs.FS
	packPaths []string
}

// Vanilla returns a Config that changes nothing.
func Vanilla() *Config { return &Config{} }

// AddPack adds a behaviour pack whose structures folder replaces vanilla
// End city templates of the same path (structures/endcity/<piece>.nbt or
// .mcstructure) and holds templates for ReplaceTemplate. Later packs win.
func (c *Config) AddPack(fsys fs.FS) { c.packs = append(c.packs, fsys) }

// Steps are End generation steps that can be turned off. The zero value
// runs every step.
type Steps struct {
	// Pillars skips the obsidian pillars and their End crystals.
	Pillars bool
	// SmallIslands skips the small floating islands of the outer End.
	SmallIslands bool
	// Chorus skips the chorus plants.
	Chorus bool
	// Gateways skips the end gateways of the outer islands.
	Gateways bool
	// Cities skips the End cities.
	Cities bool
}

// StructureCity names End cities, as the /locate command does.
const StructureCity = "minecraft:end_city"

// StructureFilter decides whether an End city may start in a chunk; see
// the overworld's StructureFilter.
type StructureFilter func(chunkX, chunkZ int32) bool

func (f StructureFilter) forGate(_ string, cx, cz int32) bool { return f(cx, cz) }

// StructureConfig holds the End city changes.
type StructureConfig struct {
	excludes  []structure.Area
	filters   []StructureFilter
	forced    [][2]int32
	loot      map[string]string
	templates map[string]string
	swaps     [][2]string
}

// ReplaceTemplate builds instead wherever a city uses template (as
// "endcity/fat_tower_top").
func (s *StructureConfig) ReplaceTemplate(template, instead string) {
	if s.templates == nil {
		s.templates = map[string]string{}
	}
	s.templates[template] = instead
}

// ReplaceBlock swaps the block from for to in the blocks cities place,
// keeping the properties both share.
func (s *StructureConfig) ReplaceBlock(from, to string) {
	s.swaps = append(s.swaps, [2]string{from, to})
}

// Exclude stops End cities starting in the area.
func (s *StructureConfig) Exclude(area structure.Area) { s.excludes = append(s.excludes, area) }

// Filter adds a filter every city start must pass.
func (s *StructureConfig) Filter(f StructureFilter) { s.filters = append(s.filters, f) }

// PlaceAt starts an End city in chunk (chunkX, chunkZ), in place of the
// vanilla start of its placement region. It skips the distance and height
// checks: over the void the city floats at the lowest vanilla city height.
func (s *StructureConfig) PlaceAt(chunkX, chunkZ int32) {
	s.forced = append(s.forced, [2]int32{chunkX, chunkZ})
}

// ReplaceLoot gives containers with loot table from the table to instead.
func (s *StructureConfig) ReplaceLoot(from, to string) {
	if s.loot == nil {
		s.loot = map[string]string{}
	}
	s.loot[from] = to
}

// applyConfig resolves cfg into e.
func (e *End) applyConfig(cfg *Config) error {
	e.skip = cfg.Skip
	e.gate = structure.NewGate([]string{StructureCity}, cfg.Logger)
	if cfg.Skip.Cities {
		e.gate.Disable(0)
	}
	for _, a := range cfg.Structures.excludes {
		e.gate.Exclude(a)
	}
	for _, f := range cfg.Structures.filters {
		if f == nil {
			return fmt.Errorf("worldgen: nil structure filter")
		}
		if err := e.gate.AddFilter(f.forGate); err != nil {
			return err
		}
	}
	for _, c := range cfg.Structures.forced {
		if cfg.Skip.Cities {
			return fmt.Errorf("worldgen: an End city is placed at chunk %d %d but cities are skipped", c[0], c[1])
		}
		reg := [2]int32{structure.Region(c[0], citySpacing), structure.Region(c[1], citySpacing)}
		if at, ok := e.forced[reg]; ok && at != c {
			return fmt.Errorf("worldgen: End cities at chunks %d %d and %d %d share a placement region", c[0], c[1], at[0], at[1])
		}
		if e.forced == nil {
			e.forced = map[[2]int32][2]int32{}
		}
		e.forced[reg] = c
	}
	e.loot = maps.Clone(cfg.Structures.loot)
	if err := structure.CheckTemplates(cfg.Structures.templates, e.st); err != nil {
		return err
	}
	for _, w := range cfg.Structures.swaps {
		if e.swaps == nil {
			e.swaps = map[uint32]uint32{}
		}
		if err := structure.SwapTable(e.reg, w[0], w[1], e.swaps); err != nil {
			return err
		}
	}
	return nil
}

// isForced reports whether the Config placed a city in chunk (cx, cz).
func (e *End) isForced(cx, cz int32) bool {
	at, ok := e.forced[[2]int32{structure.Region(cx, citySpacing), structure.Region(cz, citySpacing)}]
	return ok && at == [2]int32{cx, cz}
}

// finishChunk applies the loot table swaps to a finished chunk.
func (e *End) finishChunk(c *Chunk) {
	if len(e.loot) == 0 {
		return
	}
	for i := range c.BlockEntities {
		be := &c.BlockEntities[i]
		to, ok := e.loot[be.LootTable]
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
