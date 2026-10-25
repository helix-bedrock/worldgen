// Package end generates the vanilla Bedrock End. Like the overworld and
// nether packages it depends on nothing but the standard library and a
// block list exported from BDS. See STATUS.md for what is generated.
package end

import (
	"fmt"
	"sync"

	"github.com/helix-bedrock/worldgen/1.26.52/blocks"
	"github.com/helix-bedrock/worldgen/1.26.52/noise"
	"github.com/helix-bedrock/worldgen/1.26.52/rand"
	"github.com/helix-bedrock/worldgen/1.26.52/structure"
)

// Height is the End's height. The terrain fills only the lower 128 blocks;
// structures reach higher.
const Height = 256

// biomeName is the End's one biome.
const biomeName = "the_end"

// Chunk is the generator output for one 16x16 column. Blocks holds
// registry indices, index y<<8 | z<<4 | x.
type Chunk struct {
	Blocks [16 * 16 * Height]uint16
	// Biomes holds one biome ID per column, index z<<4 | x. The End has a
	// single biome.
	Biomes [16 * 16]uint16
	// BlockEntities are the block entities of blocks structures placed.
	BlockEntities []structure.BlockEntity
	// Entities are the entities to spawn with the chunk: the End crystals
	// on the pillars.
	Entities []structure.EntitySpawn
}

// Biome returns the biome ID at local (x, z).
func (c *Chunk) Biome(x, z int) uint32 {
	return uint32(c.Biomes[z<<4|x])
}

// Block returns the state index at local (x, z) and y.
func (c *Chunk) Block(x, y, z int) uint32 {
	return uint32(c.Blocks[y<<8|z<<4|x])
}

func (c *Chunk) set(x, y, z int, s uint32) {
	c.Blocks[y<<8|z<<4|x] = uint16(s)
}

// End generates End chunks for one seed. It is immutable after creation and
// safe for concurrent use.
type End struct {
	seed  int64
	reg   *blocks.Registry
	biome uint16
	ids   struct {
		air, endStone uint32
	}
	// The terrain noises (BDS's TheEndGenerator, constructor sub_51938A0).
	minLimit, maxLimit, selector *noise.LegacyOctaves
	island                       *noise.Simplex

	deco    decoIDs
	spikes  [10]spike
	regions sync.Pool

	// st prepares the city templates; cities holds each laid-out city by
	// start chunk (nil where none starts).
	st     *structure.Lib
	cities sync.Map

	// The Config's changes: steps skipped, the city gate, the placed city
	// starts by placement region, and the loot swaps.
	skip   Steps
	gate   *structure.Gate
	forced map[[2]int32][2]int32
	loot   map[string]string
	swaps  map[uint32]uint32
}

// New builds the noises for a seed. reg must contain the vanilla blocks the
// generator places; biomeIDs maps biome names (no "minecraft:" prefix) to
// numeric IDs, as from biome.LoadIDs.
func New(seed int64, reg *blocks.Registry, biomeIDs map[string]uint32, opts ...Option) (*End, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	if o.cfg == nil {
		o.cfg = Vanilla()
	}
	if reg.Len() > 1<<16 {
		return nil, fmt.Errorf("worldgen: %d block states do not fit the 16-bit chunk storage", reg.Len())
	}
	e := &End{seed: seed, reg: reg}
	b, ok := biomeIDs[biomeName]
	if !ok {
		return nil, fmt.Errorf("worldgen: biome %s missing from the biome list", biomeName)
	}
	e.biome = uint16(b)
	if err := reg.Resolve(
		blocks.Want{Dst: &e.ids.air, Name: "minecraft:air"},
		blocks.Want{Dst: &e.ids.endStone, Name: "minecraft:end_stone"},
	); err != nil {
		return nil, err
	}
	// Every noise is drawn in turn from one Mersenne Twister on the low 32
	// bits of the seed. A last 3-octave noise follows that the terrain
	// never samples; nothing is drawn after it.
	mt := rand.NewMT(uint32(seed))
	e.minLimit = noise.NewLegacyOctaves(mt, 16)
	e.maxLimit = noise.NewLegacyOctaves(mt, 16)
	e.selector = noise.NewLegacyOctaves(mt, 8)
	e.island = noise.NewSimplex(mt)
	if err := e.loadDecoIDs(); err != nil {
		return nil, err
	}
	e.spikes = spikes(seed)
	var err error
	if e.st, err = structure.NewLib(seed, reg); err != nil {
		return nil, err
	}
	e.st.SetPacks(o.cfg.packs, o.cfg.Structures.templates)
	if err := e.applyConfig(o.cfg); err != nil {
		return nil, err
	}
	return e, nil
}

// Seed returns the world seed.
func (e *End) Seed() int64 { return e.seed }

// GenerateChunk fills c with the terrain of the chunk at (chunkX, chunkZ),
// in BDS's order (TheEndGenerator loadChunk, sub_5194820). The End's
// surface builder only turns stone into end stone, and the terrain has no
// stone, so it is left out. Decorate adds the rest, End cities included.
func (e *End) GenerateChunk(chunkX, chunkZ int32, c *Chunk) {
	e.baseChunk(chunkX, chunkZ, c)
}

// baseChunk is the chunk's terrain.
func (e *End) baseChunk(chunkX, chunkZ int32, c *Chunk) {
	c.BlockEntities = c.BlockEntities[:0]
	c.Entities = c.Entities[:0]
	for i := range c.Blocks {
		c.Blocks[i] = uint16(e.ids.air)
	}
	for i := range c.Biomes {
		c.Biomes[i] = e.biome
	}
	e.terrain(chunkX, chunkZ, c)
}
