package structure

import (
	"sync"

	"github.com/helix-bedrock/worldgen/blocks"
	"github.com/helix-bedrock/worldgen/rand"
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
	reg                      *blocks.Registry
	seed                     int64
	names                    []string
	air, water, flowingWater uint32
	holdsLiquid              map[string]bool
	templates                sync.Map
	// popA and popB are the population seed's chunk multipliers.
	popA, popB uint32
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

func (l *Lib) BlockName(id uint32) string {
	if int(id) < len(l.names) {
		return l.names[id]
	}
	return ""
}

func (l *Lib) IsWater(b uint32) bool { return b == l.water || b == l.flowingWater }

// holdsWater reports whether a block keeps the water it is placed in: BDS's
// can-contain-liquid flag (Block+184 bit 0), from data/bds/liquid_holders.json.
func (l *Lib) HoldsWater(b uint32) bool {
	return b != l.air && l.holdsLiquid[l.BlockName(b)]
}
