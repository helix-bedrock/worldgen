package overworld

import (
	"container/list"
	"sync"
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
	terrain    lru[*PackedChunk]
	structured lru[*PackedChunk]
	features   lru[*featureWrites]
}

// NewTerrainCache returns a cache holding at most size chunks of terrain,
// packed (see PackedChunk), and the placements of at most size origins.
// Decorating a chunk reads the terrain of the chunks up to 3 away and the
// placements of the 16 origins around it, so the cache must hold the ring
// of chunks being loaded around a player, or they are made again and
// again. At a 32-chunk view radius 1024 is enough: 768 ran 15% slower,
// 2048 no faster.
func NewTerrainCache(gen *Overworld, size int) *TerrainCache {
	size = max(size, 64)
	return &TerrainCache{gen: gen, terrain: newLRU[*PackedChunk](size), structured: newLRU[*PackedChunk](size),
		features: newLRU[*featureWrites](size)}
}

// Get returns the undecorated chunk at (chunkX, chunkZ) with the structures
// that reach it (villages) placed: what features see, as in BDS, which
// places structures first. The chunk is shared: callers must not modify it.
// A chunk no structure reaches is the raw terrain itself.
func (t *TerrainCache) Get(chunkX, chunkZ int32) *PackedChunk {
	return t.structured.get([2]int32{chunkX, chunkZ}, func() *PackedChunk {
		raw := t.Raw(chunkX, chunkZ)
		height := func(x, z int32) int32 { return t.gen.surfaceHeight(t.Raw, x, z) }
		if !t.gen.structuresReach(chunkX, chunkZ, height, t.Raw) {
			return raw
		}
		full, _ := t.scratch.Get().(*Chunk)
		if full == nil {
			full = new(Chunk)
		}
		raw.Unpack(full)
		t.gen.placeStructures(chunkX, chunkZ, full, height, t.Raw)
		p := Pack(full)
		t.scratch.Put(full)
		return p
	})
}

// Raw returns the terrain of chunk (chunkX, chunkZ) without structures.
func (t *TerrainCache) Raw(chunkX, chunkZ int32) *PackedChunk {
	return t.terrain.get([2]int32{chunkX, chunkZ}, func() *PackedChunk {
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

// originFeatures returns the cached placements of origin chunk (ox, oz) for
// one chunk being decorated. After all 16 chunks they reach have taken
// them, they are dropped: a chunk made again later recomputes them.
func (t *TerrainCache) originFeatures(ox, oz int32) *featureWrites {
	pos := [2]int32{ox, oz}
	fw := t.features.get(pos, func() *featureWrites {
		return t.gen.originFeatures(ox, oz, t.Get)
	})
	if fw != nil && fw.uses.Add(1) == 16 {
		t.features.remove(pos)
	}
	return fw
}

// Generate writes the finished chunk at (chunkX, chunkZ), terrain and
// features, into c.
func (t *TerrainCache) Generate(chunkX, chunkZ int32, c *Chunk) {
	base := t.Get(chunkX, chunkZ)
	base.Unpack(c)
	t.gen.decorate(chunkX, chunkZ, c, base, t.originFeatures)
}

// lru is a least-recently-used cache of values made on first request.
type lru[V any] struct {
	size    int
	mu      sync.Mutex
	entries map[[2]int32]*list.Element
	order   *list.List // front = most recently used
}

type lruEntry[V any] struct {
	pos   [2]int32
	ready chan struct{}
	v     V
}

func newLRU[V any](size int) lru[V] {
	return lru[V]{size: size, entries: map[[2]int32]*list.Element{}, order: list.New()}
}

// remove drops the value at pos.
func (l *lru[V]) remove(pos [2]int32) {
	l.mu.Lock()
	if el, ok := l.entries[pos]; ok {
		l.order.Remove(el)
		delete(l.entries, pos)
	}
	l.mu.Unlock()
}

// get returns the value at pos, calling build for it if it is not cached.
// Callers asking for a value being made wait for it.
func (l *lru[V]) get(pos [2]int32, build func() V) V {
	l.mu.Lock()
	if el, ok := l.entries[pos]; ok {
		l.order.MoveToFront(el)
		e, _ := el.Value.(*lruEntry[V])
		l.mu.Unlock()
		if e == nil {
			var zero V
			return zero
		}
		<-e.ready
		return e.v
	}
	e := &lruEntry[V]{pos: pos, ready: make(chan struct{})}
	l.entries[pos] = l.order.PushFront(e)
	for l.order.Len() > l.size {
		old := l.order.Back()
		l.order.Remove(old)
		if oe, ok := old.Value.(*lruEntry[V]); ok {
			delete(l.entries, oe.pos)
		}
	}
	l.mu.Unlock()
	e.v = build()
	close(e.ready)
	return e.v
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
