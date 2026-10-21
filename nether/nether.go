// Package nether generates the vanilla Bedrock Nether. Like the overworld
// package it depends on nothing but the standard library and a block list
// exported from BDS. See STATUS.md for what is generated.
package nether

import (
	"fmt"
	"sync"

	"github.com/helix-bedrock/worldgen/blocks"
	"github.com/helix-bedrock/worldgen/noise"
	"github.com/helix-bedrock/worldgen/overworld"
	"github.com/helix-bedrock/worldgen/rand"
)

// Nether dimensions.
const (
	Height   = 128
	SeaLevel = 32
)

// Chunk is the generator output for one 16x16 column. Blocks holds
// registry indices, index y<<8 | z<<4 | x.
type Chunk struct {
	Blocks [16 * 16 * Height]uint16
	// Biomes holds one biome ID per column, index z<<4 | x: the Nether's
	// biomes are the same at every height.
	Biomes [16 * 16]uint16
	// BlockEntities are the block entities of blocks structures placed
	// (chests and spawners).
	BlockEntities []overworld.BlockEntity
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

// Nether generates Nether chunks for one seed. It is immutable after
// creation and safe for concurrent use.
type Nether struct {
	seed int64
	reg  *blocks.Registry
	ids  struct {
		air, netherrack, lava, bedrock   uint32
		water, flowingWater, flowingLava uint32
	}
	// The terrain noises (BDS's NetherGenerator, sub_515FDF0).
	minLimit, maxLimit, selector *noise.LegacyOctaves
	surface4                     *noise.SimplexOctaves
	biomes                       *biomeSource
	surfaces                     []*biomeSurface
	surfNoise                    surfaceNoises

	deco         decoIDs
	basaltStates []bool // by state: any basalt
	topSupport   []bool // by state: a sturdy top face
	regions      sync.Pool

	crimson, warped fungusKind
	fungi           fungusIDs
	// rules[biome][pass] are the biome's feature rules.
	rules  map[uint32][][]*netherRule
	carver netherCarver
	fossil fossilIDs

	nb         fortressIDs
	fenceJoin  []uint8  // by state: what it is to a nether brick fence
	fortresses sync.Map // [2]int32 start chunk -> *fortress
}

// New builds the noises for a seed. reg must contain the vanilla blocks the
// generator places; biomeIDs maps biome names (no "minecraft:" prefix) to
// numeric IDs, as from biome.LoadIDs.
func New(seed int64, reg *blocks.Registry, biomeIDs map[string]uint32) (*Nether, error) {
	if reg.Len() > 1<<16 {
		return nil, fmt.Errorf("worldgen: %d block states do not fit the 16-bit chunk storage", reg.Len())
	}
	n := &Nether{seed: seed, reg: reg}
	for _, b := range []struct {
		dst  *uint32
		name string
	}{
		{&n.ids.air, "minecraft:air"},
		{&n.ids.netherrack, "minecraft:netherrack"},
		{&n.ids.lava, "minecraft:lava"},
		{&n.ids.bedrock, "minecraft:bedrock"},
		{&n.ids.water, "minecraft:water"},
		{&n.ids.flowingWater, "minecraft:flowing_water"},
		{&n.ids.flowingLava, "minecraft:flowing_lava"},
	} {
		id, ok := reg.Default(b.name)
		if !ok {
			return nil, fmt.Errorf("worldgen: block %s missing from the block list", b.name)
		}
		*b.dst = id
	}
	// Every noise is drawn in turn from one Mersenne Twister on the low 32
	// bits of the seed, including three the terrain never samples: their
	// draws still move the ones after them.
	mt := rand.NewMT(uint32(seed))
	n.minLimit = noise.NewLegacyOctaves(mt, 16)
	n.maxLimit = noise.NewLegacyOctaves(mt, 16)
	n.selector = noise.NewLegacyOctaves(mt, 8)
	noise.NewLegacyOctaves(mt, 4)
	n.surface4 = noise.NewSimplexOctaves(mt, 4, false)
	noise.NewLegacyOctaves(mt, 10)
	noise.NewLegacyOctaves(mt, 16)
	// The overworld builder's material adjustment noise comes last.
	n.surfNoise.adjustment = noise.NewSimplexOctaves(mt, 3, false)
	var err error
	if n.biomes, err = newBiomeSource(seed, biomeIDs); err != nil {
		return nil, err
	}
	if err := n.loadSurfaces(biomeIDs); err != nil {
		return nil, err
	}
	if err := n.loadDecoIDs(); err != nil {
		return nil, err
	}
	if err := n.loadFortressIDs(); err != nil {
		return nil, err
	}
	if err := n.loadFungi(); err != nil {
		return nil, err
	}
	if err := n.loadFossils(); err != nil {
		return nil, err
	}
	if err := n.loadRules(biomeIDs); err != nil {
		return nil, err
	}
	if err := n.loadCarver(); err != nil {
		return nil, err
	}
	return n, nil
}

// Seed returns the world seed.
func (n *Nether) Seed() int64 { return n.seed }

// GenerateChunk fills c with the chunk at (chunkX, chunkZ), in BDS's order
// (NetherGenerator loadChunk, sub_5163F50), and the structure pieces in it.
// Fence connections across the chunk's sides need its neighbours: Decorate
// sets them.
func (n *Nether) GenerateChunk(chunkX, chunkZ int32, c *Chunk) {
	c.BlockEntities = c.BlockEntities[:0]
	n.terrain(chunkX, chunkZ, c)
	n.biomes.fill(chunkX, chunkZ, &c.Biomes)
	mt := rand.NewMT(chunkSeed(chunkX, chunkZ))
	n.bedrock(mt, c)
	n.surface(chunkX, chunkZ, c, mt)
	n.carver.carve(n.seed, chunkX, chunkZ, c)
	n.placeFortresses(chunkX, chunkZ, c)
}

// ChunkBiomes writes the biome ID of every column of chunk (chunkX, chunkZ),
// index z<<4 | x, without generating its blocks. It is safe for concurrent
// use.
func (n *Nether) ChunkBiomes(chunkX, chunkZ int32, out *[256]uint16) {
	n.biomes.fill(chunkX, chunkZ, out)
}

// chunkSeed seeds the chunk's Mersenne Twister. It differs from the
// overworld's and, like it, leaves out the world seed.
func chunkSeed(chunkX, chunkZ int32) uint32 {
	return uint32(-1724254968*chunkX - 245998635*chunkZ)
}

// bedrock draws the floor (two to five layers) and the roof (one to five),
// each column x outer, z inner, the floor first.
func (n *Nether) bedrock(mt *rand.MT, c *Chunk) {
	for x := range 16 {
		for z := range 16 {
			for y := range int(mt.IntN(4)) + 2 {
				c.set(x, y, z, n.ids.bedrock)
			}
		}
	}
	for x := range 16 {
		for z := range 16 {
			for y := Height - 1 - int(mt.IntN(5)); y < Height; y++ {
				c.set(x, y, z, n.ids.bedrock)
			}
		}
	}
}
