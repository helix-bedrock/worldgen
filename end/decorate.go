package end

import (
	"maps"
	"math"
	"sync/atomic"

	"github.com/helix-bedrock/worldgen/internal/lru"
	"github.com/helix-bedrock/worldgen/rand"
	"github.com/helix-bedrock/worldgen/structure"
)

// Decoration is BDS's TheEndGenerator postProcess (sub_5196E80), run from
// each chunk, the origin, on the 3x3 chunks around it, with the origin's
// population random:
//   - the obsidian spikes whose square starts in the origin;
//   - more than 64 chunks out: small floating islands over the void, then,
//     on high ground, chorus plants and, one origin in 700, an end gateway.
//
// The End has no biome feature rules. As in the Nether, an origin's
// features see the undecorated terrain plus their own placements only,
// never another origin's, so a chunk's decoration does not depend on the
// order chunks are made in. BDS instead sees whatever its neighbours placed
// first.
// VERIFY: BDS places a gateway only if the four chunks around it are not
// yet past an internal state (LevelChunk+184 <= 10); here it always does.

// regionSize is the width of the 3x3 chunks an origin decorates.
const regionSize = 48

// region is the block view one origin's features read and write: the
// undecorated terrain of the 3x3 chunks around it, plus its placements.
// It loads the terrain the first time a feature reads or writes it.
type region struct {
	e         *End
	ox, oz    int32
	x0, z0    int32 // world coordinates of the north-west corner
	loaded    bool
	neighbour func(cx, cz int32) *Chunk
	blocks    [regionSize * regionSize * Height]uint16
	// slots holds the placements in each of the 3x3 chunks, in order.
	slots [9]writeList
	be    []structure.BlockEntity
}

// writeList is an origin's placements in one chunk: the Chunk index and
// the state of each.
type writeList struct {
	pos []uint32
	blk []uint16
}

func (r *region) index(x, y, z int32) (int, bool) {
	lx, lz := x-r.x0, z-r.z0
	if uint32(lx) >= regionSize || uint32(lz) >= regionSize || uint32(y) >= Height {
		return 0, false
	}
	return (int(y)*regionSize+int(lz))*regionSize + int(lx), true
}

// at returns the block at a world position. Outside the region and the
// world height it is air, as BDS's generation block source reads it.
func (r *region) at(x, y, z int32) uint32 {
	i, ok := r.index(x, y, z)
	if !ok {
		return r.e.ids.air
	}
	r.load()
	return uint32(r.blocks[i])
}

// set places a block; it is dropped outside the region and the world
// height.
func (r *region) set(x, y, z int32, b uint32) {
	i, ok := r.index(x, y, z)
	if !ok {
		return
	}
	r.load()
	r.blocks[i] = uint16(b)
	lx, lz := x-r.x0, z-r.z0
	l := &r.slots[(lx>>4)*3+(lz>>4)]
	l.pos = append(l.pos, uint32(y)<<8|uint32(lz&15)<<4|uint32(lx&15))
	l.blk = append(l.blk, uint16(b))
}

// height is the column's height map: one above its highest block that
// stops light, 0 for none. Chorus lets light through.
// VERIFY: BDS keeps the height map per chunk as blocks are set; only the
// blocks the End places are sorted here.
func (r *region) height(x, z int32) int32 {
	for y := int32(Height - 1); y >= 0; y-- {
		switch r.at(x, y, z) {
		case r.e.ids.air, r.e.deco.chorusPlant, r.e.deco.chorusFlower:
		default:
			return y + 1
		}
	}
	return 0
}

// reset prepares the region for origin (ox, oz) without loading it.
func (r *region) reset(ox, oz int32, neighbour func(cx, cz int32) *Chunk) {
	r.ox, r.oz = ox, oz
	r.x0, r.z0 = (ox-1)*16, (oz-1)*16
	r.loaded = false
	r.neighbour = neighbour
	r.be = nil
	for i := range r.slots {
		r.slots[i].pos = r.slots[i].pos[:0]
		r.slots[i].blk = r.slots[i].blk[:0]
	}
}

// load fills the region from the undecorated terrain once. A chunk
// neighbour cannot provide reads as air.
func (r *region) load() {
	if r.loaded {
		return
	}
	r.loaded = true
	air := uint16(r.e.ids.air)
	for cx := range int32(3) {
		for cz := range int32(3) {
			c := r.neighbour(r.ox-1+cx, r.oz-1+cz)
			for y := range Height {
				for z := range 16 {
					row := (y*regionSize+int(cz)*16+z)*regionSize + int(cx)*16
					if c == nil {
						for x := range 16 {
							r.blocks[row+x] = air
						}
						continue
					}
					copy(r.blocks[row:row+16], c.Blocks[y<<8|z<<4:y<<8|z<<4+16])
				}
			}
		}
	}
}

// originWrites is everything one origin places, for the 3x3 chunks around
// it. It is immutable once made.
type originWrites struct {
	off  [10]int32 // slot i holds pos/blk[off[i]:off[i+1]]
	pos  []uint32
	blk  []uint16
	be   []structure.BlockEntity
	uses atomic.Int32
}

func (r *region) result() *originWrites {
	n := 0
	for i := range r.slots {
		n += len(r.slots[i].pos)
	}
	ow := &originWrites{pos: make([]uint32, 0, n), blk: make([]uint16, 0, n), be: r.be}
	for i := range r.slots {
		ow.off[i] = int32(len(ow.pos))
		ow.pos = append(ow.pos, r.slots[i].pos...)
		ow.blk = append(ow.blk, r.slots[i].blk...)
	}
	ow.off[9] = int32(n)
	return ow
}

// apply writes the placements in chunk slot to c, which is chunk (cx, cz).
func (ow *originWrites) apply(c *Chunk, slot int, cx, cz int32) {
	for k := ow.off[slot]; k < ow.off[slot+1]; k++ {
		c.Blocks[ow.pos[k]] = ow.blk[k]
	}
	for _, be := range ow.be {
		if be.X>>4 == cx && be.Z>>4 == cz {
			be.Data = maps.Clone(be.Data)
			c.BlockEntities = append(c.BlockEntities, be)
		}
	}
}

// decoIDs are the blocks the decoration places.
type decoIDs struct {
	obsidian, chorusPlant, chorusFlower, bedrock, gateway uint32
	// ironBars by the sides they join: bit i for chorusDirs[i].
	ironBars [16]uint32
}

func (e *End) loadDecoIDs() error {
	for _, b := range []struct {
		dst   *uint32
		name  string
		props map[string]any
	}{
		{&e.deco.obsidian, "minecraft:obsidian", nil},
		{&e.deco.chorusPlant, "minecraft:chorus_plant", nil},
		// A branch that ends grows a dead flower.
		{&e.deco.chorusFlower, "minecraft:chorus_flower", map[string]any{"age": int32(5)}},
		// The gateway's bedrock burns forever (BDS sets the state to 1).
		{&e.deco.bedrock, "minecraft:bedrock", map[string]any{"infiniburn_bit": true}},
		{&e.deco.gateway, "minecraft:end_gateway", nil},
	} {
		id, ok := e.reg.Default(b.name)
		if b.props != nil {
			id, ok = e.reg.Lookup(b.name, b.props)
		}
		if !ok {
			return errMissing(b.name)
		}
		*b.dst = id
	}
	for join := range e.deco.ironBars {
		props := map[string]any{}
		for i, side := range [4]string{"north", "east", "south", "west"} {
			props["minecraft:connection_"+side] = join>>i&1 == 1
		}
		id, ok := e.reg.Lookup("minecraft:iron_bars", props)
		if !ok {
			return errMissing("minecraft:iron_bars")
		}
		e.deco.ironBars[join] = id
	}
	return nil
}

// spike is one of the ten obsidian pillars around the main island.
type spike struct {
	x, z, radius, height int32
	// guarded spikes have an iron-bar cage on top.
	guarded bool
}

// spikes are the pillars of a seed (sub_497D900): ten spots on a circle of
// radius 42, each given a size from 0 to 9 in an order shuffled from the
// world seed.
func spikes(seed int64) [10]spike {
	mt := rand.NewMT(uint32(seed))
	ids := [10]int32{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}
	// BDS swaps forward: entry i trades with one of entries 0..i.
	for i := uint32(1); i < 10; i++ {
		j := mt.Uint32() % (i + 1)
		ids[i], ids[j] = ids[j], ids[i]
	}
	var out [10]spike
	for i := range out {
		a := float32(float32(i)*0.31415927) + -3.1415927
		t := float32(a+a) * 10430.378
		sin := sineTable(uint16(int32(t)))
		cos := sineTable(uint16(int32(float32(t + 16384))))
		id := ids[i]
		out[i] = spike{
			x: int32(float32(cos * 42)), z: int32(float32(sin * 42)),
			radius: id/3 + 2, height: 3*id + 76, guarded: id == 1 || id == 2,
		}
	}
	return out
}

// sineTable is BDS's 65536-entry sine table.
func sineTable(i uint16) float32 {
	return float32(math.Sin(float64(i) * (2 * math.Pi / 65536)))
}

// populationSeed is BDS's decoration seed of a chunk.
func populationSeed(levelSeed int64, cx, cz int32) uint32 {
	s := uint32(levelSeed)
	m := rand.NewMT(s)
	a := m.Uint32()>>1 | 1
	b := m.Uint32()>>1 | 1
	return (b*uint32(cz) + a*uint32(cx)) ^ s
}

// decorate runs origin (ox, oz)'s decoration in r.
func (e *End) decorate(r *region, ox, oz int32) {
	mt := rand.NewMT(populationSeed(e.seed, ox, oz))
	// BDS draws once before the islands: every outer origin compared with
	// a BDS world matches with this draw and none without it.
	// VERIFY: where it comes from is not traced; the biome decoration step
	// (sub_5194680) runs first, though the End has no feature rules.
	mt.Uint32()
	for _, s := range e.spikes {
		if (s.x-s.radius)&^15 == ox*16 && (s.z-s.radius)&^15 == oz*16 {
			e.placeSpike(r, s)
		}
	}
	if int64(ox)*int64(ox)+int64(oz)*int64(oz) <= 4096 {
		return
	}
	x0, z0 := ox*16, oz*16
	h := e.ChunkIslandHeight(ox, oz)
	if h < -20 && mt.Uint32()%14 == 0 {
		x := int32(mt.Uint32()&15) + x0 + 8
		y := int32(mt.Uint32()&15) + 55
		z := int32(mt.Uint32()&15) + z0 + 8
		e.smallIsland(r, x, y, z, mt)
		if mt.Uint32()&3 == 0 {
			e.smallIsland(r, x, y, z, mt)
		}
	}
	if h <= 40 {
		return
	}
	for n := mt.Uint32() % 5; n > 0; n-- {
		x := (x0 | 8) + int32(mt.Uint32()&15)
		z := (z0 | 8) + int32(mt.Uint32()&15)
		y := r.height(x, z)
		if y <= 0 || r.at(x, y, z) != e.ids.air || r.at(x, y-1, z) != e.ids.endStone {
			continue
		}
		r.set(x, y, z, e.deco.chorusPlant)
		e.growChorus(r, x, y, z, x, z, mt, 8, 0)
	}
	if mt.Uint32()%700 == 0 {
		x := int32(mt.Uint32()&15) + x0 + 8
		z := int32(mt.Uint32()&15) + z0 + 8
		if y := r.height(x, z); y > 0 {
			y += 3 + int32(mt.Uint32()%7)
			e.gateway(r, x, y, z)
		}
	}
}

// placeSpike builds a pillar (sub_49C44B0): an obsidian disk from the
// bottom up to its height, the square around it cleared of everything
// from y 66 to ten above the top, and the cage of a guarded one.
func (e *End) placeSpike(r *region, s spike) {
	top := s.height + 10
	for x := s.x - s.radius; x <= s.x+s.radius; x++ {
		for y := min(int32(0), top); y <= max(top, 0); y++ {
			for z := s.z - s.radius; z <= s.z+s.radius; z++ {
				dx, dz := x-s.x, z-s.z
				switch {
				case dx*dx+dz*dz <= s.radius*s.radius+1 && y < s.height:
					r.set(x, y, z, e.deco.obsidian)
				case y >= 66:
					r.set(x, y, z, e.ids.air)
				}
			}
		}
	}
	if !s.guarded {
		return
	}
	// The ring around the top three layers, then a full lid. Each bar
	// joins the bars beside it: BDS sets the joins once the cage stands,
	// and the cage has nothing else around it but air.
	bar := func(dx, dy, dz int32) bool {
		if dx < -2 || dx > 2 || dz < -2 || dz > 2 || dy < 0 || dy > 3 {
			return false
		}
		return dy == 3 || abs32(dx) == 2 || abs32(dz) == 2
	}
	for dx := int32(-2); dx <= 2; dx++ {
		for dz := int32(-2); dz <= 2; dz++ {
			for dy := range int32(4) {
				if !bar(dx, dy, dz) {
					continue
				}
				var join uint8
				for i, d := range chorusDirs {
					if bar(dx+d[0], dy, dz+d[1]) {
						join |= 1 << i
					}
				}
				r.set(s.x+dx, s.height+dy, s.z+dz, e.deco.ironBars[join])
			}
		}
	}
}

// smallIsland is a small floating island (EndIslandFeature, sub_D40FBF0): end
// stone disks, each smaller than the one above it.
func (e *End) smallIsland(r *region, x, y, z int32, mt *rand.MT) {
	rad := float32(mt.IntN(3)) + 4
	for dy := int32(0); rad > 0.5; dy-- {
		lo := int32(math.Floor(float64(-rad)))
		hi := int32(math.Ceil(float64(rad)))
		lim := float32(rad+1) * float32(rad+1)
		for dx := lo; dx <= hi; dx++ {
			for dz := lo; dz <= hi; dz++ {
				if lim >= float32(dx*dx+dz*dz) {
					r.set(x+dx, y+dy, z+dz, e.ids.endStone)
				}
			}
		}
		rad -= float32(mt.IntN(2)) + 0.5
	}
}

// chorusDirs are the sides a chorus branch may grow toward (byte_1ADA5C1):
// north, east, south, west, as x and z steps.
var chorusDirs = [4][2]int32{{0, -1}, {1, 0}, {0, 1}, {-1, 0}}

// growChorus grows a chorus plant up from (x, y, z) (sub_C5B70C0): a stem,
// then up to four side branches that each grow the same way, within
// spread blocks of the root (rx, rz) and four levels deep; a stem that
// branches nowhere ends in a dead flower. A stem whose side is blocked
// stops without one.
func (e *End) growChorus(r *region, x, y, z, rx, rz int32, mt *rand.MT, spread, depth int32) {
	h := mt.IntN(4) + 1
	if depth == 0 {
		h++
	}
	for i := int32(1); i <= h; i++ {
		if !e.chorusClear(r, x, y+i, z, -1) {
			return
		}
		r.set(x, y+i, z, e.deco.chorusPlant)
	}
	placed := false
	if depth <= 3 {
		n := mt.IntN(4)
		if depth == 0 {
			n++
		}
		for range n {
			d := mt.IntN(4)
			bx, bz := x+chorusDirs[d][0], z+chorusDirs[d][1]
			by := y + h
			if abs32(bx-rx) >= spread || abs32(bz-rz) >= spread ||
				r.at(bx, by, bz) != e.ids.air || r.at(bx, by-1, bz) != e.ids.air ||
				!e.chorusClear(r, bx, by, bz, (d+2)%4) {
				continue
			}
			r.set(bx, by, bz, e.deco.chorusPlant)
			e.growChorus(r, bx, by, bz, rx, rz, mt, spread, depth+1)
			placed = true
		}
	}
	if !placed {
		r.set(x, y+h, z, e.deco.chorusFlower)
	}
}

// chorusClear reports whether the four sides of a block are air, but the
// side toward direction except (-1 for none).
func (e *End) chorusClear(r *region, x, y, z, except int32) bool {
	for d, s := range chorusDirs {
		if int32(d) != except && r.at(x+s[0], y, z+s[1]) != e.ids.air {
			return false
		}
	}
	return true
}

// gateway places an end gateway (EndGatewayFeature, sub_D40F3A0) in a
// bedrock frame: the gateway block in the middle, bedrock above and below
// it and around the layers next to it, air elsewhere in the 3x5x3 box. Its
// exit is the End's default exit point, (100, 50, 0), the value BDS gives
// a new gateway before it first ticks.
func (e *End) gateway(r *region, x, y, z int32) {
	for dx := int32(-1); dx <= 1; dx++ {
		for dy := int32(-2); dy <= 2; dy++ {
			for dz := int32(-1); dz <= 1; dz++ {
				b := e.ids.air
				switch {
				case dx == 0 && dz == 0 && dy == 0:
					b = e.deco.gateway
				case dy == 0:
				case dx == 0 && dz == 0 && abs32(dy) == 2:
					b = e.deco.bedrock
				case (dx == 0 || dz == 0) && abs32(dy) != 2:
					b = e.deco.bedrock
				}
				r.set(x+dx, y+dy, z+dz, b)
			}
		}
	}
	if _, ok := r.index(x, y, z); ok {
		r.be = append(r.be, structure.BlockEntity{X: x, Y: y, Z: z, Data: map[string]any{
			"id": "EndGateway", "x": x, "y": y, "z": z, "Age": int32(0),
			"ExitPortal": []any{int32(100), int32(50), int32(0)},
		}})
	}
}

// originWrites runs origin (ox, oz)'s decoration on the undecorated terrain
// neighbour provides.
func (e *End) originWrites(ox, oz int32, neighbour func(cx, cz int32) *Chunk) *originWrites {
	r, _ := e.regions.Get().(*region)
	if r == nil {
		r = &region{e: e}
	}
	defer e.regions.Put(r)
	r.reset(ox, oz, neighbour)
	e.decorate(r, ox, oz)
	return r.result()
}

// Decorate adds the decoration to c, the GenerateChunk output for chunk
// (chunkX, chunkZ). neighbour must return the unchanged GenerateChunk
// output of any chunk within 2 chunks, the target's own included (nil for
// one it cannot provide: it reads as air). Cache does the same with the
// neighbours kept. It is safe to call from many goroutines if neighbour is.
func (e *End) Decorate(chunkX, chunkZ int32, c *Chunk, neighbour func(cx, cz int32) *Chunk) {
	e.decorateWith(chunkX, chunkZ, c, func(ox, oz int32) *originWrites { return e.originWrites(ox, oz, neighbour) })
}

// decorateWith applies the placements of the nine origins around chunk
// (chunkX, chunkZ) to c.
func (e *End) decorateWith(chunkX, chunkZ int32, c *Chunk, writes func(ox, oz int32) *originWrites) {
	for ox := chunkX - 1; ox <= chunkX+1; ox++ {
		for oz := chunkZ - 1; oz <= chunkZ+1; oz++ {
			writes(ox, oz).apply(c, int(chunkX-ox+1)*3+int(chunkZ-oz+1), chunkX, chunkZ)
		}
	}
}

// Cache keeps recently generated undecorated chunks and the placements of
// recent origins, so each is made once instead of once per chunk that reads
// it. It is safe for concurrent use.
type Cache struct {
	e       *End
	terrain lru.LRU[*Chunk]
	writes  lru.LRU[*originWrites]
}

// NewCache returns a cache holding at most size undecorated chunks and the
// placements of at most size origins. Decorating a chunk reads the terrain
// of the 5x5 chunks around it.
func NewCache(e *End, size int) *Cache {
	size = max(size, 64)
	return &Cache{e: e, terrain: lru.New[*Chunk](size), writes: lru.New[*originWrites](size)}
}

// Terrain returns the undecorated chunk at (chunkX, chunkZ). It is shared:
// callers must not modify it.
func (c *Cache) Terrain(chunkX, chunkZ int32) *Chunk {
	return c.terrain.Get([2]int32{chunkX, chunkZ}, func() *Chunk {
		ch := new(Chunk)
		c.e.GenerateChunk(chunkX, chunkZ, ch)
		return ch
	})
}

// Generate writes the finished chunk at (chunkX, chunkZ), terrain and
// decoration, into out.
func (c *Cache) Generate(chunkX, chunkZ int32, out *Chunk) {
	t := c.Terrain(chunkX, chunkZ)
	out.Blocks = t.Blocks
	out.Biomes = t.Biomes
	out.BlockEntities = out.BlockEntities[:0]
	for _, be := range t.BlockEntities {
		be.Data = maps.Clone(be.Data)
		out.BlockEntities = append(out.BlockEntities, be)
	}
	c.e.decorateWith(chunkX, chunkZ, out, func(ox, oz int32) *originWrites {
		pos := [2]int32{ox, oz}
		ow := c.writes.Get(pos, func() *originWrites { return c.e.originWrites(ox, oz, c.Terrain) })
		// After the nine chunks they reach have taken them, they are
		// dropped: a chunk made again later recomputes them.
		if ow.uses.Add(1) == 9 {
			c.writes.Remove(pos)
		}
		return ow
	})
}
