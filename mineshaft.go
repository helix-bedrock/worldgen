package worldgen

import (
	"github.com/helix-bedrock/worldgen/rand"
)

// Mineshafts, written from BDS 1.26.52: the placement test (sub_4E497D0),
// the start (sub_4E49990), the piece factory (sub_4E4A190) and the pieces
// (room sub_4E4AD40/sub_4E4B360, corridor sub_4E4B6A0/sub_4E4C0E0,
// crossing sub_4E4E660/sub_4E4E970, stairs sub_4E4F5E0/sub_4E4F670). The
// layout is Java's MineshaftPieces with Bedrock's random and these
// differences, all kept: the room's size draws z, y, x; a badlands room
// branches only half the time; no piece is built in the deep dark; new
// worlds get no dirt room floor; the cobweb spots, pillar reach (10 down,
// 20 up) and torch states are Bedrock's own.
//
// BDS builds the pieces of a chunk with a Mersenne Twister seeded with the
// chunk's population seed, reseeded for each structure type
// (sub_C970F80), and reads only that chunk's blocks except where noted.

// Mineshaft piece kinds.
const (
	msRoom uint8 = iota
	msCorridor
	msCrossing
	msStairs
)

// msNone is the orientation of pieces placed in world coordinates.
const msNone = -1

type msPiece struct {
	kind   uint8
	bb     box
	orient int32 // 0 south, 1 west, 2 north, 3 east; msNone for rooms and crossings
	dir    int32 // a crossing's direction
	depth  int32

	rails, spider bool  // corridor
	sections      int32 // corridor: 5-block sections
	twoFloors     bool  // crossing
	entrances     []box // room: the openings to its children
}

// mineshaft is an assembled mineshaft and the box around all its pieces.
type mineshaft struct {
	pieces []*msPiece
	bb     box
	mesa   bool
}

// msReach is how many chunks from its start chunk a mineshaft can reach:
// a piece starts at most 80 blocks from the start room's corner and runs
// up to 20 blocks further.
const msReach = 7

// isMineshaftChunk is the placement test: a chance of 0.004 in every
// chunk, fewer within 80 chunks of the world origin. The seed mixes the
// chunk in with XOR and multipliers that are not made odd, unlike the
// population seed.
func (o *Overworld) isMineshaftChunk(r *rand.MT, cx, cz int32) bool {
	s := uint32(o.seed)
	r.Seed(s)
	a := r.Uint32() >> 1
	b := r.Uint32() >> 1
	r.Seed(s ^ uint32(cx)*a ^ uint32(cz)*b)
	r.Uint32()
	f := r.Float()
	n := r.IntN(80)
	return f < 0.004 && n < max(abs32i(cx), abs32i(cz))
}

// mineshaftAt assembles the mineshaft starting in chunk (cx, cz), if any.
// Starts are kept once built; chunks without one are not stored.
func (o *Overworld) mineshaftAt(cx, cz int32) *mineshaft {
	var r rand.MT
	if !o.isMineshaftChunk(&r, cx, cz) {
		return nil
	}
	if v, ok := o.mineshafts.Load([2]int32{cx, cz}); ok {
		ms, _ := v.(*mineshaft)
		return ms
	}
	ms := o.buildMineshaft(cx, cz, &r)
	v, _ := o.mineshafts.LoadOrStore([2]int32{cx, cz}, ms)
	ms, _ = v.(*mineshaft)
	return ms
}

// buildMineshaft is the start: a room at y 50 and the pieces grown from it,
// then moved to its final height. r continues from the placement test.
func (o *Overworld) buildMineshaft(cx, cz int32, r *rand.MT) *mineshaft {
	ms := &mineshaft{}
	// VERIFY: the badlands test (sub_C7CD780) is assumed to be the mesa
	// biome tag, at y 0 of the chunk corner.
	if b := o.BiomeAt(cx*16, 0, cz*16); int(b) < len(o.biomeTags) {
		for _, t := range o.biomeTags[b] {
			if t == "mesa" {
				ms.mesa = true
			}
		}
	}
	dz := r.IntN(6)
	dy := r.IntN(6)
	dx := r.IntN(6)
	room := &msPiece{kind: msRoom, orient: msNone,
		bb: box{cx*16 + 2, 50, cz*16 + 2, cx*16 + 9 + dx, 54 + dy, cz*16 + 9 + dz}}
	ms.pieces = []*msPiece{room}
	o.msChildren(ms, room, r)
	ms.bb = room.bb
	for _, p := range ms.pieces[1:] {
		ms.bb = box{min(ms.bb.x0, p.bb.x0), min(ms.bb.y0, p.bb.y0), min(ms.bb.z0, p.bb.z0),
			max(ms.bb.x1, p.bb.x1), max(ms.bb.y1, p.bb.y1), max(ms.bb.z1, p.bb.z1)}
	}
	var dyAll int32
	if ms.mesa {
		// Badlands mineshafts sit around the surface height.
		// VERIFY: the height query is assumed to be the preliminary
		// surface at the box centre's quart.
		y := int32(SeaLevel)
		midX := ms.bb.x0 + (ms.bb.x1-ms.bb.x0+1)/2
		midZ := ms.bb.z0 + (ms.bb.z1-ms.bb.z0+1)/2
		if h := prelimSurface(o.column(midX>>2, midZ>>2)); y < h {
			y += r.IntN(h - y + 1)
		}
		dyAll = y - (ms.bb.y0 + (ms.bb.y1-ms.bb.y0+1)/2)
	} else {
		top := ms.bb.y1 - ms.bb.y0 + MinY + 2
		if lim := int32(SeaLevel - 10); lim > top {
			top += r.IntN(lim - top)
		}
		dyAll = top - ms.bb.y1
	}
	ms.bb = ms.bb.raise(dyAll)
	for _, p := range ms.pieces {
		p.bb = p.bb.raise(dyAll)
		for i := range p.entrances {
			p.entrances[i] = p.entrances[i].raise(dyAll)
		}
	}
	return ms
}

// msChildren adds the pieces that grow from p.
func (o *Overworld) msChildren(ms *mineshaft, p *msPiece, r *rand.MT) {
	b := p.bb
	switch p.kind {
	case msRoom:
		chance := float32(1)
		if ms.mesa {
			chance = 0.5
		}
		if r.Float() > chance {
			return
		}
		ys := max(b.y1-b.y0+1, 6) - 4
		if b.y1-b.y0+1 < 6 {
			ys = 1
		}
		xs, zs := b.x1-b.x0+1, b.z1-b.z0+1
		side := func(span int32, at func(j, y int32) (*msPiece, box)) {
			for j := int32(0); j < span; j += 4 {
				k := r.IntN(span)
				if j+k+3 > span {
					break
				}
				j += k
				y := b.y0 + r.IntN(ys) + 1
				if c, e := at(j, y); c != nil {
					p.entrances = append(p.entrances, e)
				}
			}
		}
		side(xs, func(j, y int32) (*msPiece, box) {
			c := o.msPiece(ms, r, b.x0+j, y, b.z0-1, 2, p.depth)
			if c == nil {
				return nil, box{}
			}
			return c, box{c.bb.x0, c.bb.y0, b.z0, c.bb.x1, c.bb.y1, b.z0 + 1}
		})
		side(xs, func(j, y int32) (*msPiece, box) {
			c := o.msPiece(ms, r, b.x0+j, y, b.z1+1, 0, p.depth)
			if c == nil {
				return nil, box{}
			}
			return c, box{c.bb.x0, c.bb.y0, b.z1 - 1, c.bb.x1, c.bb.y1, b.z1}
		})
		side(zs, func(j, y int32) (*msPiece, box) {
			c := o.msPiece(ms, r, b.x0-1, y, b.z0+j, 1, p.depth)
			if c == nil {
				return nil, box{}
			}
			return c, box{b.x0, c.bb.y0, c.bb.z0, b.x0 + 1, c.bb.y1, c.bb.z1}
		})
		side(zs, func(j, y int32) (*msPiece, box) {
			c := o.msPiece(ms, r, b.x1+1, y, b.z0+j, 3, p.depth)
			if c == nil {
				return nil, box{}
			}
			return c, box{b.x1 - 1, c.bb.y0, c.bb.z0, b.x1, c.bb.y1, c.bb.z1}
		})
	case msCorridor:
		d := p.depth
		j := r.IntN(4)
		y := b.y0 - 1 + r.IntN(3)
		switch p.orient {
		case 0:
			switch {
			case j <= 1:
				o.msPiece(ms, r, b.x0, y, b.z1+1, 0, d)
			case j == 2:
				o.msPiece(ms, r, b.x0-1, y, b.z1-3, 1, d)
			default:
				o.msPiece(ms, r, b.x1+1, y, b.z1-3, 3, d)
			}
		case 1:
			switch {
			case j <= 1:
				o.msPiece(ms, r, b.x0-1, y, b.z0, 1, d)
			case j == 2:
				o.msPiece(ms, r, b.x0, y, b.z0-1, 2, d)
			default:
				o.msPiece(ms, r, b.x0, y, b.z1+1, 0, d)
			}
		case 2:
			switch {
			case j <= 1:
				o.msPiece(ms, r, b.x0, y, b.z0-1, 2, d)
			case j == 2:
				o.msPiece(ms, r, b.x0-1, y, b.z0, 1, d)
			default:
				o.msPiece(ms, r, b.x1+1, y, b.z0, 3, d)
			}
		case 3:
			switch {
			case j <= 1:
				o.msPiece(ms, r, b.x1+1, y, b.z0, 3, d)
			case j == 2:
				o.msPiece(ms, r, b.x1-3, y, b.z0-1, 2, d)
			default:
				o.msPiece(ms, r, b.x1-3, y, b.z1+1, 0, d)
			}
		}
		if d > 7 {
			return
		}
		if p.orient&^2 != 0 {
			for k := b.x0 + 6; k <= b.x1; k += 5 {
				switch r.IntN(5) {
				case 1:
					o.msPiece(ms, r, k-3, b.y0, b.z1+1, 0, d+1)
				case 0:
					o.msPiece(ms, r, k-3, b.y0, b.z0-1, 2, d+1)
				}
			}
		} else {
			for k := b.z0 + 6; k <= b.z1; k += 5 {
				switch r.IntN(5) {
				case 1:
					o.msPiece(ms, r, b.x1+1, b.y0, k-3, 3, d+1)
				case 0:
					o.msPiece(ms, r, b.x0-1, b.y0, k-3, 1, d+1)
				}
			}
		}
	case msCrossing:
		d := p.depth
		north := func() { o.msPiece(ms, r, b.x0+1, b.y0, b.z0-1, 2, d) }
		south := func() { o.msPiece(ms, r, b.x0+1, b.y0, b.z1+1, 0, d) }
		west := func() { o.msPiece(ms, r, b.x0-1, b.y0, b.z0+1, 1, d) }
		east := func() { o.msPiece(ms, r, b.x1+1, b.y0, b.z0+1, 3, d) }
		switch p.dir {
		case 0:
			south()
			west()
			east()
		case 1:
			north()
			south()
			west()
		case 2:
			north()
			west()
			east()
		case 3:
			north()
			south()
			east()
		}
		if p.twoFloors {
			if nextBool(r) {
				o.msPiece(ms, r, b.x0+1, b.y0+4, b.z0-1, 2, d)
			}
			if nextBool(r) {
				o.msPiece(ms, r, b.x0-1, b.y0+4, b.z0+1, 1, d)
			}
			if nextBool(r) {
				o.msPiece(ms, r, b.x1+1, b.y0+4, b.z0+1, 3, d)
			}
			if nextBool(r) {
				o.msPiece(ms, r, b.x0+1, b.y0+4, b.z1+1, 0, d)
			}
		}
	case msStairs:
		d := p.depth
		switch p.orient {
		case 0:
			o.msPiece(ms, r, b.x0, b.y0, b.z1+1, 0, d)
		case 1:
			o.msPiece(ms, r, b.x0-1, b.y0, b.z0, 1, d)
		case 2:
			o.msPiece(ms, r, b.x0, b.y0, b.z0-1, 2, d)
		case 3:
			o.msPiece(ms, r, b.x1+1, b.y0, b.z0, 3, d)
		}
	}
}

// msCollides reports whether bb overlaps a piece already placed.
func msCollides(ms *mineshaft, bb box) bool {
	for _, p := range ms.pieces {
		if p.bb.intersects(bb) {
			return true
		}
	}
	return false
}

// msPiece is the factory: a crossing (20%), stairs (10%) or corridor at
// (x, y, z) heading dir, if it fits; it is added and grown at once.
func (o *Overworld) msPiece(ms *mineshaft, r *rand.MT, x, y, z, dir, depth int32) *msPiece {
	if depth > 8 {
		return nil
	}
	start := ms.pieces[0].bb
	if abs32i(x-start.x0) > 80 || abs32i(z-start.z0) > 80 {
		return nil
	}
	var p *msPiece
	switch n := r.IntN(100); {
	case n >= 80:
		t := r.IntN(4)
		var bb box
		switch dir {
		case 0:
			bb = box{x - 1, y, z, x + 3, y, z + 4}
		case 1:
			bb = box{x - 4, y, z - 1, x, y, z + 3}
		case 2:
			bb = box{x - 1, y, z - 4, x + 3, y, z}
		default:
			bb = box{x, y, z - 1, x + 4, y, z + 3}
		}
		bb.y1 = y + 2
		if t == 0 {
			bb.y1 = y + 6
		}
		if msCollides(ms, bb) {
			return nil
		}
		p = &msPiece{kind: msCrossing, bb: bb, orient: msNone, dir: dir, depth: depth + 1, twoFloors: t == 0}
	case n < 70:
		var bb box
		found := false
		for k := r.IntN(3) + 2; k > 0; k-- {
			l := 5 * k
			switch dir {
			case 0:
				bb = box{x, y, z, x + 2, y + 2, z + l - 1}
			case 1:
				bb = box{x + 1 - l, y, z, x, y + 2, z + 2}
			case 2:
				bb = box{x, y, z + 1 - l, x + 2, y + 2, z}
			default:
				bb = box{x, y, z, x - 1 + l, y + 2, z + 2}
			}
			if !msCollides(ms, bb) {
				found = true
				break
			}
		}
		if !found {
			return nil
		}
		p = &msPiece{kind: msCorridor, bb: bb, orient: dir, depth: depth + 1}
		p.rails = r.IntN(3) == 0
		p.spider = !p.rails && r.IntN(23) == 0
		if dir&^2 == 0 {
			p.sections = (bb.z1 - bb.z0 + 1) / 5
		} else {
			p.sections = (bb.x1 - bb.x0 + 1) / 5
		}
	default:
		var bb box
		switch dir {
		case 0:
			bb = box{x, y - 5, z, x + 2, y + 2, z + 8}
		case 1:
			bb = box{x - 8, y - 5, z, x, y + 2, z + 2}
		case 2:
			bb = box{x, y - 5, z - 8, x + 2, y + 2, z}
		default:
			bb = box{x, y - 5, z, x + 8, y + 2, z + 2}
		}
		if msCollides(ms, bb) {
			return nil
		}
		p = &msPiece{kind: msStairs, bb: bb, orient: dir, depth: depth + 1}
	}
	ms.pieces = append(ms.pieces, p)
	o.msChildren(ms, p, r)
	return p
}

// worldPos turns piece-local coordinates into world coordinates.
func (p *msPiece) worldPos(x, y, z int32) (int32, int32, int32) {
	b := p.bb
	switch p.orient {
	case 0:
		return b.x0 + x, b.y0 + y, b.z0 + z
	case 1:
		return b.x1 - z, b.y0 + y, b.z0 + x
	case 2:
		return b.x0 + x, b.y0 + y, b.z1 - z
	case 3:
		return b.x0 + z, b.y0 + y, b.z0 + x
	}
	return x, y, z
}

// msBuild is the state of building mineshaft pieces into one chunk.
type msBuild struct {
	o       *Overworld
	c       *Chunk
	cx, cz  int32
	cbox    box
	raw     func(cx, cz int32) *PackedChunk
	r       rand.MT
	ms      *mineshaft
	heights [256]int32 // the chunk's height map, MinY-1 until read

	log, planks, fence      uint32
	web, torch, rail, chain uint32
	spawner                 uint32
}

// get returns a block. Outside the chunk it reads the neighbour's terrain
// without structures.
func (b *msBuild) get(x, y, z int32) uint32 {
	if y < MinY || y >= MaxY {
		return b.o.ids.air
	}
	if x>>4 == b.cx && z>>4 == b.cz {
		return b.c.Block(int(x&15), int(y), int(z&15))
	}
	if b.raw == nil {
		return b.o.ids.air
	}
	n := b.raw(x>>4, z>>4)
	if n == nil {
		return b.o.ids.air
	}
	return n.at(int(y-MinY)<<8 | int(z&15)<<4 | int(x&15))
}

// boxGet is a piece's getBlock: air outside the chunk.
func (b *msBuild) boxGet(x, y, z int32) uint32 {
	if !b.cbox.contains([3]int32{x, y, z}) {
		return b.o.ids.air
	}
	return b.c.Block(int(x&15), int(y), int(z&15))
}

func (b *msBuild) set(x, y, z int32, s uint32) {
	if !b.cbox.contains([3]int32{x, y, z}) {
		return
	}
	b.c.set(int(x&15), int(y), int(z&15), s)
	i := (y-MinY)<<8 | (z&15)<<4 | x&15
	b.c.Water[i>>6] &^= 1 << (i & 63)
}

// height is the chunk's height map: one above the highest fully opaque
// block. Columns outside the chunk use the neighbour's terrain.
func (b *msBuild) height(x, z int32) int32 {
	if x>>4 != b.cx || z>>4 != b.cz {
		for y := int32(MaxY - 1); y >= MinY; y-- {
			if s := b.get(x, y, z); s != b.o.ids.air && b.o.reg.LightDampening(s) >= 15 {
				return y + 1
			}
		}
		return MinY
	}
	i := (z&15)<<4 | x&15
	if h := b.heights[i]; h >= MinY {
		return h
	}
	h := int32(MinY)
	for y := int32(MaxY - 1); y >= MinY; y-- {
		if s := b.c.Block(int(x&15), int(y), int(z&15)); s != b.o.ids.air && b.o.reg.LightDampening(s) >= 15 {
			h = y + 1
			break
		}
	}
	b.heights[i] = h
	return h
}

// below reports whether world (x, y, z) is under the height map (Java's
// isInterior).
func (b *msBuild) below(x, y, z int32) bool {
	if y >= MaxY {
		return false
	}
	return y < MinY || y < b.height(x, z)
}

// place is a piece's placeBlock: inside the chunk, and never over the
// mineshaft's own wood, chain or bedrock.
func (b *msBuild) place(p *msPiece, s uint32, x, y, z int32) {
	wx, wy, wz := p.worldPos(x, y, z)
	if !b.cbox.contains([3]int32{wx, wy, wz}) {
		return
	}
	switch b.c.Block(int(wx&15), int(wy), int(wz&15)) {
	case b.log, b.planks, b.fence, b.chain, b.o.ids.bedrock:
		return
	}
	b.set(wx, wy, wz, s)
}

// fill is generateBox: edge on the box's faces, inner inside.
func (b *msBuild) fill(p *msPiece, x0, y0, z0, x1, y1, z1 int32, edge, inner uint32) {
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			for z := z0; z <= z1; z++ {
				s := inner
				if y == y0 || y == y1 || x == x0 || x == x1 || z == z0 || z == z1 {
					s = edge
				}
				b.place(p, s, x, y, z)
			}
		}
	}
}

// maybeFill is generateMaybeBox: a float is drawn for every block; at or
// under chance the block is placed (with interior, only under the height
// map one block up).
func (b *msBuild) maybeFill(p *msPiece, chance float32, x0, y0, z0, x1, y1, z1 int32, edge, inner uint32, interior bool) {
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			for z := z0; z <= z1; z++ {
				if b.r.Float() > chance {
					continue
				}
				if interior {
					if wx, wy, wz := p.worldPos(x, y+1, z); !b.below(wx, wy, wz) {
						continue
					}
				}
				s := inner
				if y == y0 || y == y1 || x == x0 || x == x1 || z == z0 || z == z1 {
					s = edge
				}
				b.place(p, s, x, y, z)
			}
		}
	}
}

// edgesLiquid is the piece's skip test: liquid on the faces of its box
// (grown by one, cut to the chunk), or the deep dark at its middle.
func (b *msBuild) edgesLiquid(p *msPiece) bool {
	x0, y0, z0 := max(p.bb.x0-1, b.cbox.x0), max(p.bb.y0-1, b.cbox.y0), max(p.bb.z0-1, b.cbox.z0)
	x1, y1, z1 := min(p.bb.x1+1, b.cbox.x1), min(p.bb.y1+1, b.cbox.y1), min(p.bb.z1+1, b.cbox.z1)
	mx, my, mz := (x0+x1)/2, (y0+y1)/2, (z0+z1)/2
	if my >= MinY && my < MaxY && mx>>4 == b.cx && mz>>4 == b.cz &&
		b.c.Biome(int(mx&15), int(my), int(mz&15)) == b.o.deepDark {
		return true
	}
	for x := x0; x <= x1; x++ {
		for y := y0; y <= y1; y++ {
			for z := z0; z <= z1; z++ {
				if x != x0 && x != x1 && y != y0 && y != y1 && z != z0 && z != z1 {
					continue
				}
				if b.o.liquidMaterial(b.get(x, y, z)) {
					return true
				}
			}
		}
	}
	return false
}

// placeMineshafts writes the pieces of nearby mineshafts that reach chunk
// (cx, cz) into c. raw gives the terrain of neighbouring chunks.
func (o *Overworld) placeMineshafts(cx, cz int32, c *Chunk, raw func(cx, cz int32) *PackedChunk) {
	b := msBuild{o: o, c: c, cx: cx, cz: cz, raw: raw,
		cbox: box{cx * 16, MinY, cz * 16, cx*16 + 15, MaxY - 1, cz*16 + 15}}
	for i := range b.heights {
		b.heights[i] = MinY - 1
	}
	seeded := false
	for sx := cx - msReach; sx <= cx+msReach; sx++ {
		for sz := cz - msReach; sz <= cz+msReach; sz++ {
			ms := o.mineshaftAt(sx, sz)
			if ms == nil || !ms.bb.intersects(b.cbox) {
				continue
			}
			if !seeded {
				b.r.Seed(populationSeed(o.seed, cx, cz))
				seeded = true
			}
			b.build(ms)
		}
	}
}

// mineshaftReaches reports whether a mineshaft has a piece in chunk
// (cx, cz).
func (o *Overworld) mineshaftReaches(cx, cz int32) bool {
	cbox := box{cx * 16, MinY, cz * 16, cx*16 + 15, MaxY - 1, cz*16 + 15}
	for sx := cx - msReach; sx <= cx+msReach; sx++ {
		for sz := cz - msReach; sz <= cz+msReach; sz++ {
			if ms := o.mineshaftAt(sx, sz); ms != nil && ms.bb.intersects(cbox) {
				for _, p := range ms.pieces {
					if p.bb.intersects(cbox) {
						return true
					}
				}
			}
		}
	}
	return false
}

// build places one mineshaft's pieces that reach the chunk, in order.
func (b *msBuild) build(ms *mineshaft) {
	o := b.o
	id := func(name string) uint32 {
		s, _ := o.reg.Default(name)
		return s
	}
	b.ms = ms
	if ms.mesa {
		b.log, b.planks, b.fence = id("minecraft:dark_oak_log"), id("minecraft:dark_oak_planks"), id("minecraft:dark_oak_fence")
	} else {
		b.log, b.planks, b.fence = id("minecraft:oak_log"), id("minecraft:oak_planks"), id("minecraft:oak_fence")
	}
	b.web, b.torch, b.rail = id("minecraft:web"), id("minecraft:torch"), id("minecraft:rail")
	b.chain, b.spawner = id("minecraft:iron_chain"), id("minecraft:mob_spawner")
	for _, p := range ms.pieces {
		if !p.bb.intersects(b.cbox) || b.edgesLiquid(p) {
			continue
		}
		switch p.kind {
		case msRoom:
			b.buildRoom(p)
		case msCorridor:
			b.buildCorridor(p)
		case msCrossing:
			b.buildCrossing(p)
		case msStairs:
			b.buildStairs(p)
		}
	}
}

func (b *msBuild) buildRoom(p *msPiece) {
	air := b.o.ids.air
	bb := p.bb
	b.fill(p, bb.x0, bb.y0+1, bb.z0, bb.x1, min(bb.y0+3, bb.y1), bb.z1, air, air)
	for _, e := range p.entrances {
		b.fill(p, e.x0, e.y1-2, e.z0, e.x1, e.y1, e.z1, air, air)
	}
	// The upper half sphere of air above the floor space.
	y0 := bb.y0 + 4
	if bb.y1 < y0 || bb.x1 < bb.x0 || bb.z1 < bb.z0 {
		return
	}
	fy := float32(bb.y1 - y0 + 1)
	hx := float32(bb.x1-bb.x0+1) * 0.5
	mx := float32(bb.x0) + hx
	hz := float32(bb.z1-bb.z0+1) * 0.5
	mz := float32(bb.z0) + hz
	for y := y0; y <= bb.y1; y++ {
		dy := float32(y-y0) / fy
		for x := bb.x0; x <= bb.x1; x++ {
			dx := (float32(x) - mx) / hx
			s := dx*dx + dy*dy
			for z := bb.z0; z <= bb.z1; z++ {
				dz := (float32(z) - mz) / hz
				if dz*dz+s <= 1.05 {
					b.place(p, air, x, y, z)
				}
			}
		}
	}
}

func (b *msBuild) buildStairs(p *msPiece) {
	air := b.o.ids.air
	b.fill(p, 0, 5, 0, 2, 7, 1, air, air)
	b.fill(p, 0, 0, 7, 2, 2, 8, air, air)
	for i := int32(0); i < 5; i++ {
		y0 := 5 - i - 1
		if i == 4 {
			y0 = 1
		}
		b.fill(p, 0, y0, 2+i, 2, 7-i, 2+i, air, air)
	}
}

func (b *msBuild) buildCrossing(p *msPiece) {
	air := b.o.ids.air
	bb := p.bb
	if p.twoFloors {
		b.fill(p, bb.x0+1, bb.y0, bb.z0, bb.x1-1, bb.y0+2, bb.z1, air, air)
		b.fill(p, bb.x0, bb.y0, bb.z0+1, bb.x1, bb.y0+2, bb.z1-1, air, air)
		b.fill(p, bb.x0+1, bb.y1-2, bb.z0, bb.x1-1, bb.y1, bb.z1, air, air)
		b.fill(p, bb.x0, bb.y1-2, bb.z0+1, bb.x1, bb.y1, bb.z1-1, air, air)
		b.fill(p, bb.x0+1, bb.y0+3, bb.z0+1, bb.x1-1, bb.y0+3, bb.z1-1, air, air)
	} else {
		b.fill(p, bb.x0+1, bb.y0, bb.z0, bb.x1-1, bb.y1, bb.z1, air, air)
		b.fill(p, bb.x0, bb.y0, bb.z0+1, bb.x1, bb.y1, bb.z1-1, air, air)
	}
	for _, c := range [4][2]int32{{bb.x0 + 1, bb.z0 + 1}, {bb.x0 + 1, bb.z1 - 1}, {bb.x1 - 1, bb.z0 + 1}, {bb.x1 - 1, bb.z1 - 1}} {
		if b.boxGet(c[0], bb.y1+1, c[1]) != air {
			b.fill(p, c[0], bb.y0, c[1], c[0], bb.y1, c[1], b.planks, air)
		}
	}
	// Planks over air (or chain) under the floor.
	// VERIFY: BDS writes these through the world, not only inside the
	// chunk being built; here they stay inside it.
	for x := bb.x0; x <= bb.x1; x++ {
		for z := bb.z0; z <= bb.z1; z++ {
			if !b.cbox.contains([3]int32{x, bb.y0, z}) || !b.below(x, bb.y0, z) {
				continue
			}
			if s := b.get(x, bb.y0-1, z); s == air || s == b.chain {
				b.set(x, bb.y0-1, z, b.planks)
			}
		}
	}
}

// torchStates are the minecraft:torch_facing_direction values BDS gives the
// two support torches, by corridor orientation.
var torchStates = [4][2]string{{"south", "north"}, {"west", "east"}, {"north", "south"}, {"east", "west"}}

func (b *msBuild) buildCorridor(p *msPiece) {
	o := b.o
	air := o.ids.air
	m := 5*p.sections - 1
	b.fill(p, 0, 0, 0, 2, 1, m, air, air)
	b.maybeFill(p, 0.8, 0, 2, 0, 2, 2, m, air, air, false)
	if p.spider {
		b.maybeFill(p, 0.6, 0, 0, 0, 2, 1, m, b.web, air, true)
	}
	spiderPlaced := false
	for n := int32(0); n < p.sections; n++ {
		at := 5*n + 2
		b.support(p, at)
		// Bedrock's cobweb spots around the support.
		b.cobweb(p, 0, at, 0.1)
		b.cobweb(p, 2, at-1, 0.1)
		b.cobweb(p, 0, at+1, 0.1)
		b.cobweb(p, 2, at+1, 0.1)
		b.cobweb(p, 0, at-2, 0.05)
		b.cobweb(p, 2, at-2, 0.05)
		b.cobweb(p, 0, at+2, 0.05)
		b.cobweb(p, 2, at+2, 0.05)
		if !p.spider || spiderPlaced {
			continue
		}
		// VERIFY: BDS remembers the spawner on the piece, across chunks;
		// each chunk here starts without one.
		s := at - 1 + b.r.IntN(3)
		x, y, z := p.worldPos(1, 0, s)
		if !b.cbox.contains([3]int32{x, y, z}) || !b.below(x, y, z) {
			continue
		}
		spiderPlaced = true
		b.set(x, y, z, b.spawner)
		if d := o.blockEntityFor(b.spawner, "", x, y, z); d != nil {
			d["EntityIdentifier"] = "minecraft:cave_spider"
			b.c.BlockEntities = append(b.c.BlockEntities, BlockEntity{X: x, Y: y, Z: z, Data: d})
		}
	}
	// Planks over air (or chain) under the floor.
	// VERIFY: as for crossings, BDS writes these through the world.
	length := max(5*p.sections, 1)
	for x := int32(0); x <= 2; x++ {
		for z := int32(0); z < length; z++ {
			wx, wy, wz := p.worldPos(x, 0, z)
			if !b.cbox.contains([3]int32{wx, wy, wz}) || !b.below(wx, wy, wz) {
				continue
			}
			if s := b.get(wx, wy-1, wz); s == air || s == b.chain {
				b.set(wx, wy-1, wz, b.planks)
			}
		}
	}
	pillars := [][2]int32{{0, 2}, {2, 2}}
	if p.sections >= 2 {
		pillars = append(pillars, [2]int32{0, m - 2}, [2]int32{2, m - 2})
	}
	for _, c := range pillars {
		wx, wy, wz := p.worldPos(c[0], -1, c[1])
		if b.boxGet(wx, wy, wz) == b.planks {
			b.pillar(wx, wy, wz)
		}
	}
	if !p.rails || p.sections <= 0 {
		return
	}
	dir := int32(0)
	if p.orient&^2 != 0 {
		dir = 1
	}
	rail, ok := o.reg.Lookup("minecraft:rail", map[string]any{"rail_direction": dir})
	if !ok {
		rail = b.rail
	}
	for z := int32(0); z < length; z++ {
		wx, wy, wz := p.worldPos(1, -1, z)
		s := b.boxGet(wx, wy, wz)
		// VERIFY: BDS tests a block flag (+113) assumed to be "solid".
		if s == air || !o.solidMaterial(s) {
			continue
		}
		chance := float32(0.9)
		if b.below(wx, wy+1, wz) {
			chance = 0.7
		}
		if chance > b.r.Float() {
			b.place(p, rail, 1, 0, z)
		}
	}
}

// support places a beam at local z when the three blocks over it are not
// air: two fence posts, and planks across (on three of four, a full beam
// with a chance of a torch on each side).
func (b *msBuild) support(p *msPiece, z int32) {
	air := b.o.ids.air
	ax, ay, az := p.worldPos(0, 3, z)
	cx, _, cz := p.worldPos(2, 3, z)
	for x := min(ax, cx); x <= max(ax, cx); x++ {
		for zz := min(az, cz); zz <= max(az, cz); zz++ {
			if b.get(x, ay, zz) == air {
				return
			}
		}
	}
	b.fill(p, 0, 0, z, 0, 1, z, b.fence, air)
	b.fill(p, 2, 0, z, 2, 1, z, b.fence, air)
	if b.r.IntN(4) == 0 {
		b.fill(p, 0, 2, z, 0, 2, z, b.planks, air)
		b.fill(p, 2, 2, z, 2, 2, z, b.planks, air)
		return
	}
	b.fill(p, 0, 2, z, 2, 2, z, b.planks, air)
	states := torchStates[p.orient&3]
	t1, ok := b.o.reg.Lookup("minecraft:torch", map[string]any{"torch_facing_direction": states[0]})
	if !ok {
		t1 = b.torch
	}
	if b.r.Float() < 0.05 {
		b.place(p, t1, 1, 2, z-1)
	}
	t2, ok := b.o.reg.Lookup("minecraft:torch", map[string]any{"torch_facing_direction": states[1]})
	if !ok {
		t2 = b.torch
	}
	if b.r.Float() < 0.05 {
		b.place(p, t2, 1, 2, z+1)
	}
}

// cobweb is sub_4E4BB40: when local (x, 3, z) is under the height map, a
// web at (x, 2, z-1) with the chance, if two of its six neighbours in the
// chunk are not air.
func (b *msBuild) cobweb(p *msPiece, x, z int32, chance float32) {
	if tx, ty, tz := p.worldPos(x, 3, z); !b.below(tx, ty, tz) {
		return
	}
	if b.r.Float() >= chance {
		return
	}
	wx, wy, wz := p.worldPos(x, 2, z-1)
	n := 0
	for _, d := range [6][3]int32{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}} {
		if b.boxGet(wx+d[0], wy+d[1], wz+d[2]) != b.o.ids.air {
			n++
			if n == 2 {
				b.place(p, b.web, x, 2, z-1)
				return
			}
		}
	}
}

// replaceableForPillar is sub_D261FF0: blocks a pillar or chain passes
// through.
func (b *msBuild) replaceableForPillar(s uint32) bool {
	if s == b.o.ids.air || b.o.liquidMaterial(s) {
		return true
	}
	switch b.o.blockName(s) {
	case "minecraft:glow_lichen", "minecraft:seagrass", "minecraft:short_grass", "minecraft:fern":
		return true
	}
	return false
}

// pillar is sub_4E4E1F0: under a support end at (x, y, z), a log column
// down to the first solid block within 10, or else a fence and chain up to
// a ceiling within 20.
func (b *msBuild) pillar(x, y, z int32) {
	o := b.o
	down, up := true, true
	for j := int32(1); down || up; j++ {
		if down {
			s := b.get(x, y-j, z)
			lava := s == o.ids.lava || s == o.ids.flowingLava
			if !b.replaceableForPillar(s) || lava {
				if !lava && s != b.rail {
					for yy := y - j + 1; yy < y; yy++ {
						b.set(x, yy, z, b.log)
					}
					return
				}
				down = false
			} else {
				down = j <= 10 && y-j > MinY+1
			}
		}
		if up {
			s := b.get(x, y+j, z)
			// VERIFY: BDS tests the block's hanging support (sub_CC56A20);
			// a fully opaque block is assumed to hold a chain.
			if !b.replaceableForPillar(s) && o.reg.LightDampening(s) >= 15 {
				b.set(x, y+1, z, b.fence)
				for yy := y + 2; yy < y+j; yy++ {
					b.set(x, yy, z, b.chain)
				}
				return
			}
			up = b.replaceableForPillar(s) && j < 21 && y+j < MaxY-1
		}
	}
}

// Mineshaft is where a mineshaft is: its start room and the box around
// all its pieces.
type Mineshaft struct {
	// X, Y, Z is the middle of the start room's floor space.
	X, Y, Z int32
	// Min and Max are the corners of the box around every piece.
	Min, Max [3]int32
	// Badlands mineshafts are built of dark oak and sit near the surface.
	Badlands bool
}

// NearestMineshaft finds the mineshaft whose start chunk is nearest to
// block (x, z), searching up to maxChunks chunks out. Pieces can still be
// skipped where they would touch liquid or the deep dark, as in BDS. It is
// safe for concurrent use.
func (o *Overworld) NearestMineshaft(x, z, maxChunks int32) (Mineshaft, bool) {
	cx0, cz0 := x>>4, z>>4
	var r rand.MT
	best, found := int64(-1), [2]int32{}
	for ring := int32(0); ring <= maxChunks; ring++ {
		for cx := cx0 - ring; cx <= cx0+ring; cx++ {
			for cz := cz0 - ring; cz <= cz0+ring; cz++ {
				if max(abs32i(cx-cx0), abs32i(cz-cz0)) != ring || !o.isMineshaftChunk(&r, cx, cz) {
					continue
				}
				dx, dz := int64(cx*16+8-x), int64(cz*16+8-z)
				if d := dx*dx + dz*dz; best < 0 || d < best {
					best, found = d, [2]int32{cx, cz}
				}
			}
		}
		// A start one ring further out can still be nearer; stop after it.
		if best >= 0 && ring > 0 && int64(ring-1)*16*int64(ring-1)*16 > best {
			break
		}
	}
	if best < 0 {
		return Mineshaft{}, false
	}
	ms := o.mineshaftAt(found[0], found[1])
	if ms == nil {
		return Mineshaft{}, false
	}
	room := ms.pieces[0].bb
	return Mineshaft{
		X: (room.x0 + room.x1) / 2, Y: room.y0 + 1, Z: (room.z0 + room.z1) / 2,
		Min: [3]int32{ms.bb.x0, ms.bb.y0, ms.bb.z0}, Max: [3]int32{ms.bb.x1, ms.bb.y1, ms.bb.z1},
		Badlands: ms.mesa,
	}, true
}
