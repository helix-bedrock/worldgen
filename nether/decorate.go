package nether

import (
	"encoding/json"
	"fmt"
	"maps"
	"sync/atomic"

	"github.com/helix-bedrock/worldgen"
	"github.com/helix-bedrock/worldgen/feature"
	"github.com/helix-bedrock/worldgen/internal/lru"
	"github.com/helix-bedrock/worldgen/rand"
	"github.com/helix-bedrock/worldgen/structure"
)

// Decoration is BDS's NetherGenerator postProcess (sub_51650F0): a fixed
// sequence of features run from each chunk, the origin, on the 3x3 chunks
// around it. Springs, fire, glowstone and mushrooms sit at the origin's
// corner plus 8 (Java's old offset), so they reach into the chunks east
// and south; the ores sit inside the origin and reach a few blocks further.
//
// As in the overworld (see featureWorld there), an origin's features see
// the undecorated terrain plus their own placements only, never another
// origin's, so a chunk's decoration does not depend on the order chunks are
// made in. BDS instead sees whatever its neighbours placed first.

// regionSize is the width of the 3x3 chunks an origin decorates.
const regionSize = 48

// region is the block view one origin's features read and write: the
// undecorated terrain of the 3x3 chunks around it, plus its placements.
type region struct {
	n      *Nether
	x0, z0 int32 // world coordinates of the north-west corner
	// biome is the origin's biome for its feature rules: BDS takes the one
	// at the chunk's stored maximum corner (LevelChunk+68, after the
	// minimum), the south-east column.
	biome  uint32
	blocks [regionSize * regionSize * Height]uint16
	// slots holds the placements in each of the 3x3 chunks, in order.
	slots [9]writeList
}

// writeList is an origin's placements in one chunk: the Chunk index and
// the state of each.
type writeList struct {
	pos []uint16
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
		return r.n.ids.air
	}
	return uint32(r.blocks[i])
}

// set places a block; it is dropped outside the region and the world
// height.
func (r *region) set(x, y, z int32, b uint32) {
	i, ok := r.index(x, y, z)
	if !ok {
		return
	}
	r.blocks[i] = uint16(b)
	lx, lz := x-r.x0, z-r.z0
	l := &r.slots[(lx>>4)*3+(lz>>4)]
	l.pos = append(l.pos, uint16(y)<<8|uint16(lz&15)<<4|uint16(lx&15))
	l.blk = append(l.blk, uint16(b))
}

// load fills the region around origin (ox, oz) from the undecorated
// terrain. A chunk neighbour cannot provide reads as air.
func (r *region) load(ox, oz int32, neighbour func(cx, cz int32) *Chunk) {
	r.x0, r.z0 = (ox-1)*16, (oz-1)*16
	for i := range r.slots {
		r.slots[i].pos = r.slots[i].pos[:0]
		r.slots[i].blk = r.slots[i].blk[:0]
	}
	air := uint16(r.n.ids.air)
	r.biome = ^uint32(0)
	if c := neighbour(ox, oz); c != nil {
		r.biome = c.Biome(15, 15)
	}
	for cx := range int32(3) {
		for cz := range int32(3) {
			c := neighbour(ox-1+cx, oz-1+cz)
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
	pos  []uint16
	blk  []uint16
	uses atomic.Int32
}

func (r *region) result() *originWrites {
	n := 0
	for i := range r.slots {
		n += len(r.slots[i].pos)
	}
	ow := &originWrites{pos: make([]uint16, 0, n), blk: make([]uint16, 0, n)}
	for i := range r.slots {
		ow.off[i] = int32(len(ow.pos))
		ow.pos = append(ow.pos, r.slots[i].pos...)
		ow.blk = append(ow.blk, r.slots[i].blk...)
	}
	ow.off[9] = int32(n)
	return ow
}

// apply writes the placements in chunk slot to c.
func (ow *originWrites) apply(c *Chunk, slot int) {
	for k := ow.off[slot]; k < ow.off[slot+1]; k++ {
		c.Blocks[ow.pos[k]] = ow.blk[k]
	}
}

// decoIDs are the blocks the decoration places or looks for.
type decoIDs struct {
	fire, soulFire, soulSoil, soulSand, glowstone, basalt, blackstone uint32
	brownMushroom, redMushroom                                        uint32
	goldOre, quartzOre, magma, gravel, ancientDebris                  uint32
}

// blockTarget is a block name and the field its default state is loaded
// into.
type blockTarget struct {
	dst  *uint32
	name string
}

func (n *Nether) loadDecoIDs() error {
	for _, b := range []blockTarget{
		{&n.deco.fire, blockFire},
		{&n.deco.soulFire, blockSoulFire},
		{&n.deco.soulSoil, "minecraft:soul_soil"},
		{&n.deco.soulSand, blockSoulSand},
		{&n.deco.glowstone, "minecraft:glowstone"},
		{&n.deco.basalt, blockBasalt},
		{&n.deco.blackstone, "minecraft:blackstone"},
		{&n.deco.brownMushroom, blockBrownMushroom},
		{&n.deco.redMushroom, blockRedMushroom},
		{&n.deco.goldOre, "minecraft:nether_gold_ore"},
		{&n.deco.quartzOre, "minecraft:quartz_ore"},
		{&n.deco.magma, "minecraft:magma"},
		{&n.deco.gravel, "minecraft:gravel"},
		{&n.deco.ancientDebris, "minecraft:ancient_debris"},
	} {
		id, ok := n.reg.Default(b.name)
		if !ok {
			return fmt.Errorf("worldgen: block %s missing from the block list", b.name)
		}
		*b.dst = id
	}
	n.basaltStates = make([]bool, n.reg.Len())
	for i := range n.basaltStates {
		if s, ok := n.reg.State(uint32(i)); ok && s.Name == blockBasalt {
			n.basaltStates[i] = true
		}
	}
	return n.loadTopSupport()
}

// populationSeed is BDS's per-chunk decoration seed, the same as the
// overworld's: a Mersenne Twister on the level seed gives two odd
// multipliers for the chunk position, XORed with the seed again.
func populationSeed(levelSeed int64, chunkX, chunkZ int32) uint32 {
	s := uint32(levelSeed)
	m := rand.NewMT(s)
	a := m.Uint32()>>1 | 1
	b := m.Uint32()>>1 | 1
	return (b*uint32(chunkZ) + a*uint32(chunkX)) ^ s
}

// decoDraw draws feature positions in an origin chunk.
type decoDraw struct {
	mt     *rand.MT
	bx, bz int32
}

// pos draws a position x, y, z: x and z in the chunk plus off, y from yn
// values plus y0.
func (d *decoDraw) pos(off, yn, y0 int32) (int32, int32, int32) {
	x := d.bx + off + d.mt.IntN(16)
	y := d.mt.IntN(yn) + y0
	z := d.bz + off + d.mt.IntN(16)
	return x, y, z
}

// decoOre places times ore veins of count at positions drawn by d, each
// turning the replace blocks into place.
func (n *Nether) decoOre(r *region, d *decoDraw, count int32, place uint32, replace []uint32, times int, yn, y0 int32) {
	for range times {
		x, y, z := d.pos(0, yn, y0)
		feature.OreVein(count, x, y, z, d.mt, func(bx, by, bz int32) {
			cur := r.at(bx, by, bz)
			if cur == place {
				return
			}
			for _, m := range replace {
				if cur == m {
					r.set(bx, by, bz, place)
					return
				}
			}
		})
	}
}

// decorate runs origin (ox, oz)'s features on r, in BDS's order.
func (n *Nether) decorate(r *region, ox, oz int32) {
	mt := rand.NewMT(populationSeed(n.seed, ox, oz))
	d := decoDraw{mt: mt, bx: ox * 16, bz: oz * 16}
	for range 8 {
		x, y, z := d.pos(8, 120, 4)
		n.spring(r, x, y, z, false)
	}
	for i := mt.IntN(mt.IntN(10)+1) + 1; i > 0; i-- {
		x, y, z := d.pos(8, 120, 4)
		n.firePatch(r, x, y, z, mt)
	}
	for i := mt.IntN(mt.IntN(10) + 1); i > 0; i-- {
		x, y, z := d.pos(8, 120, 4)
		n.glowstoneBlob(r, x, y, z, mt)
	}
	for range 10 {
		x, y, z := d.pos(8, 128, 0)
		n.glowstoneBlob(r, x, y, z, mt)
	}
	for _, b := range [2]uint32{n.deco.brownMushroom, n.deco.redMushroom} {
		if mt.IntN(1) == 0 {
			x, y, z := d.pos(8, 128, 0)
			n.mushroomPatch(r, x, y, z, b, mt)
		}
	}
	netherrack := []uint32{n.ids.netherrack}
	// The counts and blocks are VanillaFeatures' registrations
	// (sub_4E2BB50); BDS gates gold ore, blackstone and ancient debris on
	// the world's base game version, which every new world passes.
	n.decoOre(r, &d, 10, n.deco.goldOre, netherrack, 10, 108, 10)
	n.decoOre(r, &d, 14, n.deco.quartzOre, netherrack, 16, 108, 10)
	for range 16 {
		x, y, z := d.pos(0, 108, 10)
		n.spring(r, x, y, z, true)
	}
	n.decoOre(r, &d, 28, n.deco.magma, []uint32{n.ids.netherrack, n.deco.blackstone}, 9, 14, 23)
	n.decoOre(r, &d, 33, n.deco.gravel, netherrack, 2, 32, 5)
	n.decoOre(r, &d, 33, n.deco.blackstone, netherrack, 2, 32, 5)
	// The random starts over for the biomes' feature rules and the
	// ancient debris.
	mt.Seed(populationSeed(n.seed, ox, oz))
	// One draw happens between the reseed and the debris: matching BDS's
	// debris positions needs exactly one (113 of 120 hit with it, 3 without).
	// VERIFY: which call in the rule passes (sub_5163DB0) makes it.
	mt.Uint32()
	n.runRules(r, ox, oz, r.biome, mt)
	for range 2 {
		x, y, z := d.pos(0, 16, 8)
		n.debris(r, x, y, z, 0, 4, mt)
	}
	for range 3 {
		x, y, z := d.pos(0, 112, 8)
		n.debris(r, x, y, z, 0, 3, mt)
	}
}

// spring is BDS's NetherSpringFeature (sub_49BE520): lava where netherrack
// is above and, of the four sides and below, four are netherrack and one
// is air, or all five are netherrack. A hidden spring needs all five.
func (n *Nether) spring(r *region, x, y, z int32, hidden bool) {
	nr := n.ids.netherrack
	if r.at(x, y+1, z) != nr {
		return
	}
	if cur := r.at(x, y, z); cur != n.ids.air && cur != nr {
		return
	}
	rock, air := 0, 0
	for _, d := range [5][3]int32{{-1, 0, 0}, {1, 0, 0}, {0, 0, -1}, {0, 0, 1}, {0, -1, 0}} {
		switch r.at(x+d[0], y+d[1], z+d[2]) {
		case nr:
			rock++
		case n.ids.air:
			air++
		}
	}
	if (air == 1 && !hidden && rock == 4) || rock == 5 {
		r.set(x, y, z, n.ids.lava)
	}
}

// pairOffset draws two raw values and returns the difference of their low
// bits (mask 7: -7 to 7), BDS's triangular scatter.
func pairOffset(mt *rand.MT, mask uint32) int32 {
	a := int32(mt.Uint32() & mask)
	return a - int32(mt.Uint32()&mask)
}

// firePatch is BDS's fire patch (sub_49BC4C0): 64 tries scattered around
// the position, z, then y, then x; fire on netherrack, soul fire on soul
// soil.
func (n *Nether) firePatch(r *region, x, y, z int32, mt *rand.MT) {
	for range 64 {
		dz := pairOffset(mt, 7)
		dy := pairOffset(mt, 3)
		dx := pairOffset(mt, 7)
		px, py, pz := x+dx, y+dy, z+dz
		if r.at(px, py, pz) != n.ids.air {
			continue
		}
		switch r.at(px, py-1, pz) {
		case n.ids.netherrack:
			r.set(px, py, pz, n.deco.fire)
		case n.deco.soulSoil:
			r.set(px, py, pz, n.deco.soulFire)
		}
	}
}

// glowstoneBlob is BDS's glowstone feature (sub_51C9160): glowstone in air
// under netherrack, basalt or blackstone, then 1500 tries below it, each
// kept where exactly one of the six neighbours is glowstone.
func (n *Nether) glowstoneBlob(r *region, x, y, z int32, mt *rand.MT) {
	if r.at(x, y, z) != n.ids.air {
		return
	}
	if above := r.at(x, y+1, z); above != n.ids.netherrack && above != n.deco.blackstone && !n.basaltStates[above] {
		return
	}
	g := n.deco.glowstone
	r.set(x, y, z, g)
	for range 1500 {
		dz := pairOffset(mt, 7)
		dy := mt.IntN(12)
		dx := pairOffset(mt, 7)
		px, py, pz := x+dx, y-dy, z+dz
		if r.at(px, py, pz) != n.ids.air {
			continue
		}
		count := 0
		for _, d := range [6][3]int32{{-1, 0, 0}, {1, 0, 0}, {0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}} {
			if r.at(px+d[0], py+d[1], pz+d[2]) == g {
				count++
			}
		}
		if count == 1 {
			r.set(px, py, pz, g)
		}
	}
}

// mushroomPatch is BDS's 64-try plant patch (sub_49B9020): the first try is
// the position itself, each later one scattered around it, z, then y, then
// x. A mushroom goes in air on a block that holds it.
func (n *Nether) mushroomPatch(r *region, x, y, z int32, b uint32, mt *rand.MT) {
	px, py, pz := x, y, z
	for range 64 {
		if r.at(px, py, pz) == n.ids.air {
			if below := r.at(px, py-1, pz); below != n.ids.air && n.holdsMushroom(below) {
				r.set(px, py, pz, b)
			}
		}
		dz := pairOffset(mt, 7)
		dy := pairOffset(mt, 3)
		dx := pairOffset(mt, 7)
		px, py, pz = x+dx, y+dy, z+dz
	}
}

// holdsMushroom is the mushroom's support rule for the block below. In the
// Nether the cover test BDS makes first (sub_49EC660) always passes under
// the bedrock roof.
// VERIFY: BDS reads a flag of the block below (Block+113); this takes the
// states whose top face is sturdy.
func (n *Nether) holdsMushroom(b uint32) bool {
	switch b {
	case n.ids.lava, n.ids.flowingLava, n.ids.water, n.ids.flowingWater, n.deco.fire, n.deco.soulFire:
		return false
	}
	return int(b) < len(n.topSupport) && n.topSupport[b]
}

// debris is BDS's NoSurfaceOreFeature (sub_49BEAD0) with ancient debris:
// min + nextInt(max-min) tries, try i a walk of 2i random steps from the
// position; within 6 blocks of it, debris replaces netherrack that touches
// no air.
func (n *Nether) debris(r *region, x, y, z, lo, hi int32, mt *rand.MT) {
	count := lo
	if lo < hi-1 {
		count += mt.IntN(hi - lo)
	}
	steps := [6][3]int32{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}
	for i := range count {
		px, py, pz := x, y, z
		for range 2 * i {
			d := steps[mt.IntN(6)]
			px, py, pz = px+d[0], py+d[1], pz+d[2]
		}
		dx, dy, dz := int64(px-x), int64(py-y), int64(pz-z)
		if dx*dx+dy*dy+dz*dz > 36 || r.at(px, py, pz) != n.ids.netherrack {
			continue
		}
		exposed := false
		for _, d := range steps {
			if r.at(px+d[0], py+d[1], pz+d[2]) == n.ids.air {
				exposed = true
				break
			}
		}
		if !exposed {
			r.set(px, py, pz, n.deco.ancientDebris)
		}
	}
}

// originWrites runs origin (ox, oz)'s decoration on the undecorated terrain
// neighbour provides.
func (n *Nether) originWrites(ox, oz int32, neighbour func(cx, cz int32) *Chunk) *originWrites {
	r, _ := n.regions.Get().(*region)
	if r == nil {
		r = &region{n: n}
	}
	defer n.regions.Put(r)
	if n.skip.Decoration {
		return &originWrites{}
	}
	r.load(ox, oz, neighbour)
	n.decorate(r, ox, oz)
	return r.result()
}

// Decorate adds the decoration to c, the GenerateChunk output for chunk
// (chunkX, chunkZ), and joins its fences. neighbour must return the
// unchanged GenerateChunk output of any chunk within 3 chunks, the target's
// own included (nil for one it cannot provide: it reads as air); only a
// fence on the chunk's side needs more than 2. Cache does the same with the
// neighbours kept. It is safe to call from many goroutines if neighbour is.
func (n *Nether) Decorate(chunkX, chunkZ int32, c *Chunk, neighbour func(cx, cz int32) *Chunk) {
	w := originSource{n: n, neighbour: neighbour}
	n.decorateWith(chunkX, chunkZ, c, w.writes)
	n.connectFences(c, chunkX, chunkZ, w.decorated)
	n.finishChunk(c)
}

// originSource computes origin writes from a chunk's undecorated
// neighbours, without a cache.
type originSource struct {
	n         *Nether
	neighbour func(cx, cz int32) *Chunk
}

// writes is the placements of origin (ox, oz).
func (w originSource) writes(ox, oz int32) *originWrites {
	return w.n.originWrites(ox, oz, w.neighbour)
}

// decorated is the decorated chunk (cx, cz), without its fences joined.
func (w originSource) decorated(cx, cz int32) *Chunk {
	return w.n.decorated(cx, cz, w.neighbour, w.writes)
}

// decorateWith applies the placements of the nine origins around chunk
// (chunkX, chunkZ) to c.
func (n *Nether) decorateWith(chunkX, chunkZ int32, c *Chunk, writes func(ox, oz int32) *originWrites) {
	for ox := chunkX - 1; ox <= chunkX+1; ox++ {
		for oz := chunkZ - 1; oz <= chunkZ+1; oz++ {
			writes(ox, oz).apply(c, int(chunkX-ox+1)*3+int(chunkZ-oz+1))
		}
	}
}

// decorated is the decorated chunk (cx, cz), without its fences joined,
// for joining a neighbour's fences; nil where terrain has none.
func (n *Nether) decorated(cx, cz int32, terrain func(cx, cz int32) *Chunk, writes func(ox, oz int32) *originWrites) *Chunk {
	t := terrain(cx, cz)
	if t == nil {
		return nil
	}
	c := new(Chunk)
	c.Blocks = t.Blocks
	n.decorateWith(cx, cz, c, writes)
	return c
}

// Cache keeps recently generated undecorated chunks and the placements of
// recent origins, so each is made once instead of once per chunk that reads
// it. It is safe for concurrent use.
type Cache struct {
	n       *Nether
	terrain lru.LRU[*Chunk]
	writes  lru.LRU[*originWrites]
}

// NewCache returns a cache holding at most size undecorated chunks and the
// placements of at most size origins. Decorating a chunk reads the terrain
// of the 5x5 chunks around it.
func NewCache(n *Nether, size int) *Cache {
	size = max(size, 64)
	return &Cache{n: n, terrain: lru.New[*Chunk](size), writes: lru.New[*originWrites](size)}
}

// Terrain returns the undecorated chunk at (chunkX, chunkZ). It is shared:
// callers must not modify it.
func (c *Cache) Terrain(chunkX, chunkZ int32) *Chunk {
	return c.terrain.Get([2]int32{chunkX, chunkZ}, func() *Chunk {
		ch := new(Chunk)
		c.n.GenerateChunk(chunkX, chunkZ, ch)
		return ch
	})
}

// Generate writes the finished chunk at (chunkX, chunkZ), terrain and
// decoration, into out.
func (c *Cache) Generate(chunkX, chunkZ int32, out *Chunk) {
	t := c.Terrain(chunkX, chunkZ)
	*out = *t
	// The terrain is shared: its block entities are copied, data too.
	out.BlockEntities = make([]structure.BlockEntity, len(t.BlockEntities))
	for i, be := range t.BlockEntities {
		be.Data = maps.Clone(be.Data)
		out.BlockEntities[i] = be
	}
	c.n.decorateWith(chunkX, chunkZ, out, func(ox, oz int32) *originWrites {
		pos := [2]int32{ox, oz}
		ow := c.origin(ox, oz)
		// After the nine chunks they reach have taken them, they are
		// dropped: a chunk made again later recomputes them.
		if ow.uses.Add(1) == 9 {
			c.writes.Remove(pos)
		}
		return ow
	})
	// BDS joins the fences after the decoration: a fence joins the
	// glowstone grown beside it. A neighbour read for that does not count
	// as a use of its origins.
	c.n.connectFences(out, chunkX, chunkZ, func(cx, cz int32) *Chunk {
		return c.n.decorated(cx, cz, c.Terrain, c.origin)
	})
	c.n.finishChunk(out)
}

// origin returns the placements of origin (ox, oz).
func (c *Cache) origin(ox, oz int32) *originWrites {
	return c.writes.Get([2]int32{ox, oz}, func() *originWrites { return c.n.originWrites(ox, oz, c.Terrain) })
}

// loadTopSupport reads the block states whose top face is sturdy (exported
// from BDS, as the overworld uses them).
func (n *Nether) loadTopSupport() error {
	raw, err := worldgen.Data.ReadFile("data/bds/top_support.json")
	if err != nil {
		return fmt.Errorf("worldgen: read top support: %w", err)
	}
	var hashes []uint32
	if err := json.Unmarshal(raw, &hashes); err != nil {
		return fmt.Errorf("worldgen: parse top support: %w", err)
	}
	n.topSupport = make([]bool, n.reg.Len())
	for _, h := range hashes {
		if i, ok := n.reg.ByHash(h); ok {
			n.topSupport[i] = true
		}
	}
	return nil
}
