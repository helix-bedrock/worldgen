package overworld

import (
	"sync"

	"github.com/helix-bedrock/worldgen/internal/lru"
)

// TerrainCache keeps recently generated undecorated chunks and the feature
// placements of recent origin chunks, so a chunk's neighbours are generated
// and their features run once instead of once per chunk that reads them.
// It is safe for concurrent use; an item asked for by several goroutines at
// once is made once.
type TerrainCache struct {
	gen *Overworld

	// scratch pools full chunks to generate into before packing.
	scratch    sync.Pool
	terrain    lru.LRU[*PackedChunk]
	structured lru.LRU[*PackedChunk]
	features   lru.LRU[*featureWrites]
	// stages holds chunks as the origins of phases 1 and up find them.
	stages [originPhases - 1]lru.LRU[*PackedChunk]
}

// NewTerrainCache returns a cache holding at most size chunks of terrain,
// packed (see PackedChunk), and the placements of at most size origins.
// Decorating a chunk reads the placements of the 16 origins around it, and
// those depend on the origins of earlier phases around them, so the cache must
// hold the ring of chunks being loaded around a player, or they are made again
// and again.
func NewTerrainCache(gen *Overworld, size int) *TerrainCache {
	size = max(size, 64)
	t := &TerrainCache{gen: gen, terrain: lru.New[*PackedChunk](size), structured: lru.New[*PackedChunk](size),
		features: lru.New[*featureWrites](size)}
	for i := range t.stages {
		t.stages[i] = lru.New[*PackedChunk](size)
	}
	return t
}

// Get returns the undecorated chunk at (chunkX, chunkZ) with the structures
// that reach it (villages) placed: what features see, as in BDS, which
// places structures first. The chunk is shared: callers must not modify it.
// A chunk no structure reaches is the raw terrain itself.
func (t *TerrainCache) Get(chunkX, chunkZ int32) *PackedChunk {
	return t.structured.Get([2]int32{chunkX, chunkZ}, func() *PackedChunk {
		raw := t.Raw(chunkX, chunkZ)
		height := surfaceHeights{t.gen, t.Raw}.at
		if !t.gen.structuresReach(chunkX, chunkZ, t.Raw) {
			return raw
		}
		full, _ := t.scratch.Get().(*Chunk)
		if full == nil {
			full = new(Chunk)
		}
		raw.Unpack(full)
		t.gen.placeStructures(chunkX, chunkZ, full, height, t.Raw, nil)
		p := Pack(full)
		t.scratch.Put(full)
		return p
	})
}

// Raw returns the terrain of chunk (chunkX, chunkZ) without structures.
func (t *TerrainCache) Raw(chunkX, chunkZ int32) *PackedChunk {
	return t.terrain.Get([2]int32{chunkX, chunkZ}, func() *PackedChunk {
		full, _ := t.scratch.Get().(*Chunk)
		if full == nil {
			full = new(Chunk)
		}
		t.gen.GenerateChunk(chunkX, chunkZ, full)
		p := Pack(full)
		t.scratch.Put(full)
		return p
	})
}

// originFeatures returns the cached placements of origin chunk (ox, oz).
func (t *TerrainCache) originFeatures(ox, oz int32) *featureWrites {
	return t.features.Get([2]int32{ox, oz}, func() *featureWrites {
		return t.gen.originFeatures(ox, oz, t.phaseView(originPhase(ox, oz)))
	})
}

// phaseView returns the chunks as the origins of a phase find them.
func (t *TerrainCache) phaseView(phase int) func(cx, cz int32) *PackedChunk {
	if phase == 0 {
		return t.Get
	}
	return func(cx, cz int32) *PackedChunk {
		return t.stages[phase-1].Get([2]int32{cx, cz}, func() *PackedChunk {
			scratch, _ := t.scratch.Get().(*Chunk)
			if scratch == nil {
				scratch = new(Chunk)
			}
			defer t.scratch.Put(scratch)
			return t.gen.stageChunk(cx, cz, phase, t.Get(cx, cz), scratch, t.originFeatures)
		})
	}
}

// Generate writes the finished chunk at (chunkX, chunkZ), terrain and
// features, into c.
func (t *TerrainCache) Generate(chunkX, chunkZ int32, c *Chunk) {
	base := t.Get(chunkX, chunkZ)
	base.Unpack(c)
	t.gen.decorate(chunkX, chunkZ, c, base, t.originFeatures)
	t.gen.connectBlocks(c, chunkX, chunkZ, t.Get)
	t.gen.finishChunk(c)
}

// Spawner is a monster spawner block a structure or feature placed.
type Spawner struct {
	X, Y, Z int32
	// Mob is the spawned entity, as "minecraft:zombie".
	Mob string
}

// Spawners generates chunk (chunkX, chunkZ), terrain and features, and
// returns its monster spawners (dungeons, mineshaft cave spiders). It is
// meant for tools: it builds the whole chunk.
func (t *TerrainCache) Spawners(chunkX, chunkZ int32) []Spawner {
	c, _ := t.scratch.Get().(*Chunk)
	if c == nil {
		c = new(Chunk)
	}
	defer t.scratch.Put(c)
	t.Generate(chunkX, chunkZ, c)
	var out []Spawner
	for _, be := range c.BlockEntities {
		if id, _ := be.Data["id"].(string); id != "MobSpawner" {
			continue
		}
		mob, _ := be.Data["EntityIdentifier"].(string)
		out = append(out, Spawner{X: be.X, Y: be.Y, Z: be.Z, Mob: mob})
	}
	return out
}

// surfaceHeights reads surface heights from a raw terrain source.
type surfaceHeights struct {
	o   *Overworld
	raw func(cx, cz int32) *PackedChunk
}

// at is the surface height of column (x, z).
func (s surfaceHeights) at(x, z int32) int32 { return s.o.surfaceHeight(s.raw, x, z) }
