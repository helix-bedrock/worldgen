package structure

import (
	"fmt"
	"io/fs"
	"sync"

	worldgen "github.com/helix-bedrock/worldgen/1.26.60"
	"github.com/helix-bedrock/worldgen/1.26.60/blocks"
	"github.com/helix-bedrock/worldgen/1.26.60/rand"
)

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

// Lib holds what structure templates need for one world: the block list
// and seed, and the templates prepared so far.
type Lib struct {
	reg                        *blocks.Registry
	seed                       int64
	names                      []string
	air, water, flowingWater   uint32
	bedrock, lava, flowingLava uint32
	holdsLiquid                map[string]bool
	templates                  sync.Map
	// popA and popB are the population seed's chunk multipliers.
	popA, popB uint32
	// packs are behaviour packs whose structures folder replaces vanilla
	// templates of the same path, later packs first; alias maps a template
	// path to the one used instead.
	packs []fs.FS
	alias map[string]string
}

// SetPacks makes templates come from the packs' structures folders first
// (as "structures/village/plains/houses/plains_small_house_1.mcstructure"
// or .nbt), the last pack winning, and alias replace template paths by
// others. It must be called before any template is used.
func (l *Lib) SetPacks(packs []fs.FS, alias map[string]string) {
	l.packs, l.alias = packs, alias
}

// readStructure reads a template file, from the packs or the embedded
// vanilla data. ext is ".nbt" or ".mcstructure".
func (l *Lib) readStructure(path, ext string) ([]byte, error) {
	for i := len(l.packs) - 1; i >= 0; i-- {
		if b, err := fs.ReadFile(l.packs[i], "structures/"+path+ext); err == nil {
			return b, nil
		}
	}
	b, err := worldgen.Data.ReadFile("vanilla/structures/" + path + ext)
	if err != nil {
		return nil, fmt.Errorf("template %s%s: %w", path, ext, err)
	}
	return b, nil
}

// resolve is the template path used for path.
func (l *Lib) resolve(path string) string {
	if to, ok := l.alias[path]; ok {
		return to
	}
	return path
}

// NewLib prepares structure templates for a world seed and block list.
func NewLib(seed int64, reg *blocks.Registry) (*Lib, error) {
	l := &Lib{reg: reg, seed: seed, names: make([]string, reg.Len())}
	for i := range l.names {
		st, _ := reg.State(uint32(i))
		l.names[i] = st.Name
	}
	l.air, _ = reg.Default("minecraft:air")
	l.water, _ = reg.Default("minecraft:water")
	l.flowingWater, _ = reg.Default("minecraft:flowing_water")
	l.bedrock, _ = reg.Default("minecraft:bedrock")
	l.lava, _ = reg.Default("minecraft:lava")
	l.flowingLava, _ = reg.Default("minecraft:flowing_lava")
	m := rand.NewMT(uint32(seed))
	l.popA = m.Uint32()>>1 | 1
	l.popB = m.Uint32()>>1 | 1
	var err error
	if l.holdsLiquid, err = LoadLiquidHolders(); err != nil {
		return nil, err
	}
	return l, nil
}

// populationSeed is BDS's decoration seed of chunk (cx, cz), the seed of
// the random every structure piece is post-processed with.
func (l *Lib) populationSeed(cx, cz int32) uint32 {
	return (l.popB*uint32(cz) + l.popA*uint32(cx)) ^ uint32(l.seed)
}

// BlockName returns the block name of a state, empty for an unknown one.
func (l *Lib) BlockName(id uint32) string {
	if int(id) < len(l.names) {
		return l.names[id]
	}
	return ""
}

// IsWater reports whether a block is water, still or flowing.
func (l *Lib) IsWater(b uint32) bool { return b == l.water || b == l.flowingWater }

// HoldsWater reports whether a block keeps the water it is placed in: BDS's
// can-contain-liquid flag (Block+184 bit 0), from bds/liquid_holders.json.
func (l *Lib) HoldsWater(b uint32) bool {
	return b != l.air && l.holdsLiquid[l.BlockName(b)]
}

// state is a block with properties. A property the block does not have is
// left at its default, as BDS's setState does.
func (l *Lib) state(name string, props map[string]any) uint32 {
	id, ok := l.reg.Default(name)
	if !ok {
		return id
	}
	def, _ := l.reg.State(id)
	for k := range props {
		if _, ok := def.Props[k]; !ok {
			delete(props, k)
		}
	}
	if s, ok := l.reg.Lookup(name, props); ok {
		return s
	}
	return id
}
