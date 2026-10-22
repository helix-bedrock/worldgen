package overworld

import (
	"math"

	"github.com/helix-bedrock/worldgen/rand"
	"github.com/helix-bedrock/worldgen/structure"
)

// Strongholds, written from BDS 1.26.52: the placement (the first three,
// sub_4E9F2D0, and the grid test, sub_4E9EFC0), the start (sub_4EA01A0),
// the child picker (sub_4FB07E0), the piece factory (sub_4FAF3F0), the
// portal room fallback (sub_4FB2150), the small door (sub_4FB1330) and the
// pieces' builds. The layout is Java's StrongholdPieces with Bedrock's
// random and these differences, all kept:
//   - the first three strongholds sit on village chunks found around a
//     spiral, and the rest come from a 200-chunk grid, a quarter of the
//     cells holding one;
//   - every child pick copies the parent random's state, so the start
//     random only moves when a pending piece is chosen;
//   - when no portal room was placed, the last piece is swapped for one
//     instead of the layout being thrown away;
//   - boxes replace air too, prison cells have no iron doors and the
//     weights are Bedrock's (the portal room is 10, not 20).

// Stronghold piece kinds.
const (
	shStraight uint8 = iota
	shPrisonHall
	shLeftTurn
	shRightTurn
	shRoomCrossing
	shStraightStairsDown
	shStairsDown
	shFiveCrossing
	shChestCorridor
	shLibrary
	shPortalRoom
	shFiller
	shNone uint8 = 255
)

// Small door kinds.
const (
	shOpening int32 = iota
	shWoodDoor
	shGrates
	shIronDoor
)

type shPiece struct {
	kind   uint8
	bb     structure.Box
	orient int32 // 0 south, 1 west, 2 north, 3 east
	depth  int32
	door   int32 // the entry door

	// Straight: left, right. FiveCrossing: leftHigh, leftLow, rightHigh,
	// rightLow (BDS's flag bytes in order).
	open     [4]bool
	roomType int32 // RoomCrossing
	tall     bool  // Library
	source   bool  // the start StairsDown
	steps    int32 // Filler
}

// worldPos turns piece-local coordinates into world coordinates.
func (p *shPiece) worldPos(x, y, z int32) (int32, int32, int32) {
	b := p.bb
	switch p.orient {
	case 0:
		return b.X0 + x, b.Y0 + y, b.Z0 + z
	case 1:
		return b.X1 - z, b.Y0 + y, b.Z0 + x
	case 2:
		return b.X0 + x, b.Y0 + y, b.Z1 - z
	case 3:
		return b.X0 + z, b.Y0 + y, b.Z0 + x
	}
	return x, y, z
}

// stronghold is an assembled stronghold and the box around its pieces.
type stronghold struct {
	pieces []*shPiece
	bb     structure.Box
	portal *shPiece
}

// shReach is how many chunks from its start chunk a stronghold can reach:
// a piece starts at most 112 blocks from the start's corner and runs up to
// 16 blocks further.
const shReach = 9

// strongholdFirst returns the first three strongholds' chunks: a spiral
// from a random angle and distance, each stop searching 16 by 16 chunks
// for a village start.
func (o *Overworld) strongholdFirst() [][2]int32 {
	o.shFirstOnce.Do(func() {
		var r rand.MT
		r.Seed(uint32(o.seed))
		angle := float32(r.Float() * 6.2831855)
		dist := r.IntN(16) + 40
		for i := 0; i < 1000 && len(o.shFirst) < 3; i++ {
			a := float32(angle * 10430.378)
			fx := float32(float32(dist) * sinTable[uint16(int32(float32(a+16384)))])
			fz := float32(float32(dist) * sinTable[uint16(int32(a))])
			cx, cz := int32(math.Floor(float64(fx))), int32(math.Floor(float64(fz)))
			found := false
			for x := cx - 8; x <= cx+7 && !found; x++ {
				for z := cz - 8; z <= cz+7; z++ {
					if o.IsVillageChunk(x, z) {
						o.shFirst = append(o.shFirst, [2]int32{x, z})
						found = true
						break
					}
				}
			}
			if found {
				angle = float32(angle + 1.8849558)
				dist += 8
			} else {
				angle = float32(angle + 0.7853982)
				dist += 4
			}
		}
	})
	return o.shFirst
}

// strongholdCell returns the grid start of the 200-chunk cell holding
// chunk (cx, cz), if the cell has one.
func (o *Overworld) strongholdCell(cx, cz int32) (int32, int32, bool) {
	gx := int32(math.Floor(float64(float32(cx) / 200)))
	gz := int32(math.Floor(float64(float32(cz) / 200)))
	mx, mz := gx*200+100, gz*200+100
	if uint32(mx*mx+mz*mz) < 100 {
		return 0, 0, false
	}
	var r rand.MT
	r.Seed(uint32(o.seed) + 97858791 + uint32(mx*-1683231919) + uint32(mz*-1100435783))
	x := r.IntN(100) + 200*gx + 50
	z := r.IntN(100) + 200*gz + 50
	return x, z, r.Float64() < 0.25
}

// isStrongholdChunk is the placement test. There is no biome check.
func (o *Overworld) isStrongholdChunk(cx, cz int32) bool {
	for _, c := range o.strongholdFirst() {
		if c == [2]int32{cx, cz} {
			return true
		}
	}
	if uint32(cx*cx+cz*cz) < 100 {
		return false
	}
	x, z, ok := o.strongholdCell(cx, cz)
	return ok && x == cx && z == cz
}

// strongholdsNear calls f with every stronghold start within reach chunks
// of chunk (cx, cz).
func (o *Overworld) strongholdsNear(cx, cz, reach int32, f func(sx, sz int32)) {
	for _, c := range o.strongholdFirst() {
		if abs32i(c[0]-cx) <= reach && abs32i(c[1]-cz) <= reach {
			f(c[0], c[1])
		}
	}
	g0x, g1x := int32(math.Floor(float64(cx-reach)/200)), int32(math.Floor(float64(cx+reach)/200))
	g0z, g1z := int32(math.Floor(float64(cz-reach)/200)), int32(math.Floor(float64(cz+reach)/200))
	for gx := g0x; gx <= g1x; gx++ {
		for gz := g0z; gz <= g1z; gz++ {
			x, z, ok := o.strongholdCell(gx*200, gz*200)
			if !ok || abs32i(x-cx) > reach || abs32i(z-cz) > reach || uint32(x*x+z*z) < 100 {
				continue
			}
			first := false
			for _, c := range o.strongholdFirst() {
				first = first || c == [2]int32{x, z}
			}
			if !first {
				f(x, z)
			}
		}
	}
}

// strongholdAt assembles the stronghold starting in chunk (cx, cz), if any.
func (o *Overworld) strongholdAt(cx, cz int32) *stronghold {
	if !o.isStrongholdChunk(cx, cz) {
		return nil
	}
	if v, ok := o.strongholds.Load([2]int32{cx, cz}); ok {
		s, _ := v.(*stronghold)
		return s
	}
	s := o.layStronghold(cx, cz)
	v, _ := o.strongholds.LoadOrStore([2]int32{cx, cz}, s)
	s, _ = v.(*stronghold)
	return s
}

// shWeight is one entry of the piece table.
type shWeight struct {
	kind                  uint8
	weight, max, minDepth int32
	placed                int32
}

// shLayout is the state of laying out one stronghold.
type shLayout struct {
	pieces  []*shPiece
	pending []*shPiece
	weights []shWeight
	prev    uint8
	imposed uint8
	start   structure.Box
	portal  *shPiece
}

// layStronghold is the start: a StairsDown at y 64 and the pieces grown
// from it, then moved under the sea level.
// VERIFY: the start random is assumed to be seeded with the start chunk's
// population seed, as for the other structure starts.
func (o *Overworld) layStronghold(cx, cz int32) *stronghold {
	var r rand.MT
	r.Seed(populationSeed(o.seed, cx, cz))
	l := &shLayout{prev: shNone, imposed: shNone, weights: []shWeight{
		{shStraight, 40, 0, 0, 0}, {shPrisonHall, 5, 5, 0, 0}, {shLeftTurn, 20, 0, 0, 0}, {shRightTurn, 20, 0, 0, 0},
		{shRoomCrossing, 10, 6, 0, 0}, {shStraightStairsDown, 5, 5, 0, 0}, {shStairsDown, 5, 5, 0, 0},
		{shFiveCrossing, 5, 4, 0, 0}, {shChestCorridor, 5, 4, 0, 0}, {shLibrary, 10, 2, 5, 0}, {shPortalRoom, 10, 1, 6, 0},
	}}
	start := &shPiece{kind: shStairsDown, source: true, orient: r.IntN(4),
		bb: structure.NewBox(cx*16+2, 64, cz*16+2, cx*16+6, 74, cz*16+6)}
	l.start = start.bb
	l.pieces = []*shPiece{start}
	l.children(start, &r)
	for len(l.pending) > 0 {
		i := r.IntN(int32(len(l.pending)))
		p := l.pending[i]
		l.pending = append(l.pending[:i], l.pending[i+1:]...)
		l.children(p, &r)
	}
	if l.portal == nil {
		l.portalFallback()
	}
	s := &stronghold{pieces: l.pieces, portal: l.portal}
	s.bb = s.pieces[0].bb
	for _, p := range s.pieces[1:] {
		s.bb = structure.NewBox(min(s.bb.X0, p.bb.X0), min(s.bb.Y0, p.bb.Y0), min(s.bb.Z0, p.bb.Z0),
			max(s.bb.X1, p.bb.X1), max(s.bb.Y1, p.bb.Y1), max(s.bb.Z1, p.bb.Z1))

	}
	top := s.bb.Y1 - s.bb.Y0 + MinY + 2
	if lim := int32(SeaLevel - 10); lim > top {
		top += r.IntN(lim - top)
	}
	dy := top - s.bb.Y1
	s.bb = s.bb.Raise(dy)
	for _, p := range s.pieces {
		p.bb = p.bb.Raise(dy)
	}
	return s
}

// portalFallback swaps the last piece for a portal room at the end of the
// one before it, when that is high enough.
func (l *shLayout) portalFallback() {
	if len(l.pieces) < 2 {
		return
	}
	last := l.pieces[len(l.pieces)-1]
	l.pieces = l.pieces[:len(l.pieces)-1]
	prev := l.pieces[len(l.pieces)-1]
	b := prev.bb
	var bb structure.Box
	switch last.orient {
	case 0:
		mid := (b.X0 + b.X1) / 2
		bb = structure.NewBox(mid-3, b.Y0, b.Z1, mid+7, b.Y0+7, b.Z1+15)
	case 1:
		mid := (b.Z0 + b.Z1) / 2
		bb = structure.NewBox(b.X0-15, b.Y0, mid-5, b.X0, b.Y0+7, mid+5)
	case 2:
		mid := (b.X0 + b.X1) / 2
		bb = structure.NewBox(mid-5, b.Y0, b.Z0-15, mid+5, b.Y0+7, b.Z0)
	case 3:
		mid := (b.Z0 + b.Z1) / 2
		bb = structure.NewBox(b.X1, b.Y0, mid-3, b.X1+15, b.Y0+7, mid+7)
	default:
		bb = structure.NewBox(-4, b.Y0, 0, 6, b.Y0+7, 15)
	}
	if b.Y0 < 11 {
		return
	}
	l.portal = &shPiece{kind: shPortalRoom, bb: bb, orient: last.orient, depth: prev.depth}
	l.pieces = append(l.pieces, l.portal)
}

// orientedBox is a piece box at (x, y, z) heading o, with the side and
// height offsets and the size of the piece seen facing south.
func orientedBox(x, y, z, ox, oy, w, h, d, o int32) structure.Box {
	y0, y1 := y+oy, y+oy+h-1
	switch o {
	case 1:
		return structure.NewBox(x-d+1, y0, z+ox, x, y1, z+ox+w-1)
	case 2:
		return structure.NewBox(x+ox, y0, z-d+1, x+ox+w-1, y1, z)
	case 3:
		return structure.NewBox(x, y0, z+ox, x+d-1, y1, z+ox+w-1)
	}
	return structure.NewBox(x+ox, y0, z, x+ox+w-1, y1, z+d-1)
}

func (l *shLayout) collides(bb structure.Box) bool {
	for _, p := range l.pieces {
		if p.bb.Intersects(bb) {
			return true
		}
	}
	return false
}

// shDoor draws a piece's entry door.
func shDoor(r *rand.MT) int32 {
	switch r.IntN(5) {
	case 2:
		return shWoodDoor
	case 3:
		return shGrates
	case 4:
		return shIronDoor
	}
	return shOpening
}

// factory makes a piece of kind k at (x, y, z) heading o if its box is
// high enough and free.
func (l *shLayout) factory(k uint8, r *rand.MT, x, y, z, o, depth int32) *shPiece {
	fits := func(bb structure.Box) bool { return bb.Y0 > 10 && !l.collides(bb) }
	var bb structure.Box
	switch k {
	case shStraight, shChestCorridor:
		bb = orientedBox(x, y, z, -1, -1, 5, 5, 7, o)
	case shLeftTurn, shRightTurn:
		bb = orientedBox(x, y, z, -1, -1, 5, 5, 5, o)
	case shPrisonHall:
		bb = orientedBox(x, y, z, -1, -1, 9, 5, 11, o)
	case shRoomCrossing:
		bb = orientedBox(x, y, z, -4, -1, 11, 7, 11, o)
	case shStairsDown:
		bb = orientedBox(x, y, z, -1, -7, 5, 11, 5, o)
	case shStraightStairsDown:
		bb = orientedBox(x, y, z, -1, -7, 5, 11, 8, o)
	case shFiveCrossing:
		bb = orientedBox(x, y, z, -4, -3, 10, 9, 11, o)
	case shPortalRoom:
		bb = orientedBox(x, y, z, -4, -1, 11, 8, 16, o)
	case shLibrary:
		bb = orientedBox(x, y, z, -4, -1, 14, 11, 15, o)
		if !fits(bb) {
			bb = orientedBox(x, y, z, -4, -1, 14, 6, 15, o)
		}
	default:
		return nil
	}
	if !fits(bb) {
		return nil
	}
	p := &shPiece{kind: k, bb: bb, orient: o, depth: depth}
	if k != shPortalRoom {
		p.door = shDoor(r)
	}
	switch k {
	case shStraight:
		p.open[0] = r.IntN(2) == 0
		p.open[1] = r.IntN(2) == 0
	case shRoomCrossing:
		p.roomType = r.IntN(5)
	case shFiveCrossing:
		p.open[1] = nextBool(r)
		p.open[0] = nextBool(r)
		p.open[3] = nextBool(r)
		p.open[2] = r.IntN(3) > 0
	case shLibrary:
		p.tall = bb.Y1-bb.Y0+1 >= 7
	}
	return p
}

// next is the child picker: the imposed piece, else up to five weighted
// tries, else a filler corridor up to the piece in the way.
func (l *shLayout) next(r *rand.MT, x, y, z, o, depth int32) {
	if depth > 50 || abs32i(x-l.start.X0) > 112 || abs32i(z-l.start.Z0) > 112 {
		return
	}
	cp := *r
	d := depth + 1
	if l.imposed != shNone {
		k := l.imposed
		l.imposed = shNone
		if p := l.factory(k, &cp, x, y, z, o, d); p != nil {
			l.add(p)
			return
		}
	}
	var total int32
	room := false
	for _, w := range l.weights {
		total += w.weight
		room = room || (w.max > 0 && w.placed < w.max)
	}
	if total <= 0 || !room {
		return
	}
	for range 5 {
		n := int32(cp.Uint32() % uint32(total))
		for i := 0; i < len(l.weights); i++ {
			w := &l.weights[i]
			n -= w.weight
			if n >= 0 {
				continue
			}
			if (w.max != 0 && w.placed >= w.max) || d < w.minDepth {
				break
			}
			if w.kind == l.prev && len(l.weights) > 1 {
				break
			}
			p := l.factory(w.kind, &cp, x, y, z, o, d)
			if p == nil {
				continue
			}
			w.placed++
			l.prev = w.kind
			if w.max != 0 && w.placed >= w.max {
				l.weights = append(l.weights[:i], l.weights[i+1:]...)
			}
			l.add(p)
			return
		}
	}
	if p := l.filler(x, y, z, o, d); p != nil {
		l.add(p)
	}
}

func (l *shLayout) add(p *shPiece) {
	if p.kind == shPortalRoom {
		l.portal = p
	}
	l.pieces = append(l.pieces, p)
	l.pending = append(l.pending, p)
}

// filler is the corridor that runs up to the piece in the way, three, two
// or one block long, when that piece's floor is level with it.
func (l *shLayout) filler(x, y, z, o, depth int32) *shPiece {
	var bb structure.Box
	switch o {
	case 0:
		bb = structure.NewBox(x-1, y-1, z, x+3, y+3, z+3)
	case 1:
		bb = structure.NewBox(x-3, y-1, z-1, x, y+3, z+3)
	case 2:
		bb = structure.NewBox(x-1, y-1, z-3, x+3, y+3, z)
	default:
		bb = structure.NewBox(x, y-1, z-1, x+3, y+3, z+3)
	}
	var hit *shPiece
	for _, p := range l.pieces {
		if p.bb.Intersects(bb) {
			hit = p
			break
		}
	}
	if hit == nil || hit.bb.Y0 != y-1 {
		return nil
	}
	h := hit.bb
	// within is the flat test of the piece in the way against a column
	// span; the spans can be empty, as in BDS.
	within := func(x0, z0, x1, z1 int32) bool {
		return h.Z0 <= z1 && h.Z1 >= z0 && h.X1 >= x0 && h.X0 <= x1
	}
	length := int32(3)
	var x0, z0, x1, z1 int32
	switch o {
	case 0:
		x0, z0, x1, z1 = x-1, z, x+3, z+1
	case 1:
		x0, z0, x1, z1 = x-1, z-1, x, z+3
	case 2:
		x0, z0, x1, z1 = x-1, z-1, x+3, z
	default:
		x0, z0, x1, z1 = x, z-1, x+1, z+3
	}
	if within(x0, z0, x1, z1) {
		length = 2
		if o == 1 || o == 3 {
			x0, z0, x1, z1 = x, z-1, x, z+3
		} else {
			x0, z0, x1, z1 = x-1, z, x+3, z
		}
		if within(x0, z0, x1, z1) {
			length = 1
			switch o {
			case 0:
				x0, z0, x1, z1 = x-1, z, x+3, z-1
			case 1:
				x0, z0, x1, z1 = x+1, z-1, x, z+3
			case 2:
				x0, z0, x1, z1 = x-1, z+1, x+3, z
			default:
				x0, z0, x1, z1 = x, z-1, x-1, z+3
			}
			if within(x0, z0, x1, z1) {
				return nil
			}
		}
	}
	x0, x1, z0, z1 = x-1, x+3, z-1, z+3
	switch o {
	case 0:
		z0, z1 = z, z-1+length
	case 1:
		x0, x1 = x-length+1, x
	case 2:
		z0, z1 = z-length+1, z
	default:
		x0, x1 = x, x-1+length
	}
	if x1 < x0 || y < 3 || z1 < z0 {
		return nil
	}
	p := &shPiece{kind: shFiller, orient: o, depth: depth, bb: structure.NewBox(x0, y-1, z0, x1, y+3, z1)}
	if o&1 == 0 {
		p.steps = z1 - z0 + 1
	} else {
		p.steps = x1 - x0 + 1
	}
	return p
}

// forward, left and right grow a child from a door of p.
func (l *shLayout) forward(p *shPiece, r *rand.MT, xOff int32) {
	b := p.bb
	switch p.orient {
	case 0:
		l.next(r, b.X0+xOff, b.Y0+1, b.Z1+1, 0, p.depth)
	case 1:
		l.next(r, b.X0-1, b.Y0+1, b.Z0+xOff, 1, p.depth)
	case 2:
		l.next(r, b.X0+xOff, b.Y0+1, b.Z0-1, 2, p.depth)
	case 3:
		l.next(r, b.X1+1, b.Y0+1, b.Z0+xOff, 3, p.depth)
	}
}

func (l *shLayout) left(p *shPiece, r *rand.MT, yOff, hOff int32) {
	b := p.bb
	switch p.orient {
	case 0, 2:
		l.next(r, b.X0-1, b.Y0+yOff, b.Z0+hOff, 1, p.depth)
	case 1, 3:
		l.next(r, b.X0+hOff, b.Y0+yOff, b.Z0-1, 2, p.depth)
	}
}

func (l *shLayout) right(p *shPiece, r *rand.MT, yOff, hOff int32) {
	b := p.bb
	switch p.orient {
	case 0, 2:
		l.next(r, b.X1+1, b.Y0+yOff, b.Z0+hOff, 3, p.depth)
	case 1, 3:
		l.next(r, b.X0+hOff, b.Y0+yOff, b.Z1+1, 0, p.depth)
	}
}

// children is addChildren.
func (l *shLayout) children(p *shPiece, r *rand.MT) {
	switch p.kind {
	case shStraight:
		l.forward(p, r, 1)
		if p.open[0] {
			l.left(p, r, 1, 2)
		}
		if p.open[1] {
			l.right(p, r, 1, 2)
		}
	case shLeftTurn, shRightTurn:
		toLeft := p.orient == 2 || p.orient == 3
		if p.kind == shRightTurn {
			toLeft = !toLeft
		}
		if toLeft {
			l.left(p, r, 1, 1)
		} else {
			l.right(p, r, 1, 1)
		}
	case shRoomCrossing:
		l.forward(p, r, 4)
		l.left(p, r, 1, 4)
		l.right(p, r, 1, 4)
	case shFiveCrossing:
		i, j := int32(3), int32(5)
		if p.orient == 1 || p.orient == 2 {
			i, j = 5, 3
		}
		l.forward(p, r, 5)
		if p.open[1] {
			l.left(p, r, i, 1)
		}
		if p.open[0] {
			l.left(p, r, j, 7)
		}
		if p.open[3] {
			l.right(p, r, i, 1)
		}
		if p.open[2] {
			l.right(p, r, j, 7)
		}
	case shStairsDown:
		if p.source {
			l.imposed = shFiveCrossing
		}
		l.forward(p, r, 1)
	case shPrisonHall, shChestCorridor, shStraightStairsDown:
		l.forward(p, r, 1)
	}
}

// placeStrongholds writes the pieces of nearby strongholds that reach
// chunk (cx, cz) into c (connectBlocks joins their fences and iron bars).
// raw gives the terrain of neighbouring chunks.
func (o *Overworld) placeStrongholds(cx, cz int32, c *Chunk, raw func(cx, cz int32) *PackedChunk) {
	cbox := structure.NewBox(cx*16, MinY, cz*16, cx*16+15, MaxY-1, cz*16+15)
	o.strongholdsNear(cx, cz, shReach, func(sx, sz int32) {
		s := o.strongholdAt(sx, sz)
		if s == nil || !s.bb.Intersects(cbox) {
			return
		}
		o.buildStronghold(s, cx, cz, c)
	})
}

// buildStronghold builds the pieces of s that reach chunk (cx, cz) into c,
// with the chunk's random.
func (o *Overworld) buildStronghold(s *stronghold, cx, cz int32, c *Chunk) {
	cbox := structure.NewBox(cx*16, MinY, cz*16, cx*16+15, MaxY-1, cz*16+15)
	b := &shBuild{scBuild: scBuild{o: o, c: c, cbox: cbox}}
	b.ids()
	b.r.Seed(populationSeed(o.seed, cx, cz))
	for _, p := range s.pieces {
		if p.bb.Intersects(cbox) {
			b.p = p
			b.build()
		}
	}
}

// strongholdReaches reports whether a stronghold has a piece in chunk
// (cx, cz).
func (o *Overworld) strongholdReaches(cx, cz int32) bool {
	cbox := structure.NewBox(cx*16, MinY, cz*16, cx*16+15, MaxY-1, cz*16+15)
	hit := false
	o.strongholdsNear(cx, cz, shReach, func(sx, sz int32) {
		if s := o.strongholdAt(sx, sz); s != nil && !hit && s.bb.Intersects(cbox) {
			for _, p := range s.pieces {
				if p.bb.Intersects(cbox) {
					hit = true
					return
				}
			}
		}
	})
	return hit
}

// shBuild builds stronghold pieces into one chunk.
type shBuild struct {
	scBuild
	p *shPiece

	stoneBricks, infested, cracked, mossyBricks uint32
	slab, smoothSlab, smoothDouble              uint32
	bars, planks, fence, shelf, web, cobble     uint32
	water, lava, portal                         uint32
}

func (b *shBuild) id(name string) uint32 {
	s, _ := b.o.reg.Default("minecraft:" + name)
	return s
}

func (b *shBuild) ids() {
	b.scBuild.ids()
	b.stoneBricks, b.infested = b.id("stone_bricks"), b.id("infested_stone_bricks")
	b.cracked, b.mossyBricks = b.id("cracked_stone_bricks"), b.id("mossy_stone_bricks")
	b.slab, b.smoothSlab, b.smoothDouble = b.id("stone_brick_slab"), b.id("smooth_stone_slab"), b.id("smooth_stone_double_slab")
	b.bars, b.planks, b.fence = b.id("iron_bars"), b.id("oak_planks"), b.id("oak_fence")
	b.shelf, b.web = b.id("bookshelf"), b.id("web")
	b.water, b.lava, b.portal = b.id("water"), b.id("lava"), b.id("end_portal")
}

// put is placeBlock: a piece position, inside the chunk only.
func (b *shBuild) put(s uint32, x, y, z int32) {
	wx, wy, wz := b.p.worldPos(x, y, z)
	if b.setWorld(wx, wy, wz, s) {
		b.dropEntity([3]int32{wx, wy, wz})
	}
}

// at is getBlock: air outside the chunk.
func (b *shBuild) at(x, y, z int32) uint32 {
	wx, wy, wz := b.p.worldPos(x, y, z)
	if !b.cbox.Contains([3]int32{wx, wy, wz}) {
		return b.air
	}
	return b.c.Block(int(wx&15), int(wy), int(wz&15))
}

// box is generateBox with one block for the faces and one inside. Air is
// replaced too.
func (b *shBuild) box(x0, y0, z0, x1, y1, z1 int32, edge, inner uint32) {
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			for z := z0; z <= z1; z++ {
				s := inner
				if y == y0 || y == y1 || x == x0 || x == x1 || z == z0 || z == z1 {
					s = edge
				}
				b.put(s, x, y, z)
			}
		}
	}
}

// stones is generateBox with the stone brick selector: on the faces a
// float picks cracked, mossy, infested or plain stone bricks; inside is
// air. The float is drawn whether or not the block is in the chunk.
func (b *shBuild) stones(x0, y0, z0, x1, y1, z1 int32) {
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			for z := z0; z <= z1; z++ {
				s := b.air
				if y == y0 || y == y1 || x == x0 || x == x1 || z == z0 || z == z1 {
					switch f := b.r.Float(); {
					case f < 0.2:
						s = b.cracked
					case f < 0.5:
						s = b.mossyBricks
					case f < 0.55:
						s = b.infested
					default:
						s = b.stoneBricks
					}
				}
				b.put(s, x, y, z)
			}
		}
	}
}

// maybeBox is generateMaybeBox: a float for every block, placed at or
// under chance.
func (b *shBuild) maybeBox(chance float32, x0, y0, z0, x1, y1, z1 int32, s uint32) {
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			for z := z0; z <= z1; z++ {
				if b.r.Float() <= chance {
					b.put(s, x, y, z)
				}
			}
		}
	}
}

// Orientation tables of sub_D254F10, by the state a block has, for piece
// orientations 0, 1 and 3 (2 leaves values unchanged).
var (
	shFacing = [4][4]int32{{3, 2, 5, 4}, {4, 5, 3, 2}, {2, 3, 4, 5}, {5, 4, 2, 3}} // facing_direction 2..5
	shWeirdo = [4][4]int32{{0, 1, 3, 2}, {2, 3, 0, 1}, {0, 1, 2, 3}, {2, 3, 1, 0}}
	shTorch  = [6]string{"unknown", "west", "east", "north", "south", "top"}
	shCard   = [4]string{"south", "west", "north", "east"}
)

func (b *shBuild) facing(v int32) int32 {
	if v < 2 || v > 5 {
		return v
	}
	return shFacing[b.p.orient&3][v-2]
}

func (b *shBuild) weirdo(v int32) int32 { return shWeirdo[b.p.orient&3][v&3] }

// cardinal turns a door's cardinal_direction index. Doors carry the
// rotating flag BDS checks after the named states.
// VERIFY: the flag (block type byte 265, bit 2) is assumed set for doors.
func (b *shBuild) cardinal(v int32) int32 {
	switch b.p.orient {
	case 0:
		return [4]int32{2, 3, 0, 1}[v&3]
	case 1:
		return (v + 1) & 3
	case 3:
		return (v - 1) & 3
	}
	return v
}

func (b *shBuild) torch(v int32) uint32 {
	return b.o.state("minecraft:torch", map[string]any{"torch_facing_direction": shTorch[v]})
}

// sideTorch is the torch value of a west-wall torch: 1 for pieces running
// north-south, else 3.
func (b *shBuild) sideTorch() int32 {
	if b.p.orient&^2 == 0 {
		return 1
	}
	return 3
}

func shTable(t [4]int32, o int32) int32 {
	if o >= 0 && o <= 3 {
		return t[o]
	}
	return 3
}

// smallDoor is the door between pieces at (x, y, z), facing along z.
func (b *shBuild) smallDoor(kind, x, y, z int32) {
	frame := func(s uint32) {
		b.put(s, x, y, z)
		b.put(s, x, y+1, z)
		b.put(s, x, y+2, z)
		b.put(s, x+1, y+2, z)
		b.put(s, x+2, y+2, z)
		b.put(s, x+2, y+1, z)
		b.put(s, x+2, y, z)
	}
	door := func(name string) {
		d := shCard[b.cardinal(3)]
		b.put(b.o.state("minecraft:"+name, map[string]any{"minecraft:cardinal_direction": d}), x+1, y, z)
		b.put(b.o.state("minecraft:"+name, map[string]any{"minecraft:cardinal_direction": d, "upper_block_bit": uint8(1)}), x+1, y+1, z)
	}
	switch kind {
	case shWoodDoor:
		frame(b.stoneBricks)
		door("wooden_door")
	case shGrates:
		b.put(b.air, x+1, y, z)
		b.put(b.air, x+1, y+1, z)
		frame(b.bars)
	case shIronDoor:
		frame(b.stoneBricks)
		door("iron_door")
		b.put(b.o.state("minecraft:stone_button", map[string]any{"facing_direction": b.facing(2)}), x+2, y+1, z+1)
		b.put(b.o.state("minecraft:stone_button", map[string]any{"facing_direction": b.facing(3)}), x+2, y+1, z-1)
	default:
		b.box(x, y, z, x+2, y+2, z, b.air, b.air)
	}
}

// chest is createChest: a chest facing west (BDS leaves chests unturned)
// with a loot table, unless a chest is already there.
func (b *shBuild) chest(x, y, z, facing int32, table string) {
	wx, wy, wz := b.p.worldPos(x, y, z)
	if !b.cbox.Contains([3]int32{wx, wy, wz}) || b.o.blockName(b.at(x, y, z)) == "minecraft:chest" {
		return
	}
	b.put(b.o.state("minecraft:chest", map[string]any{"minecraft:cardinal_direction": cardinalOf[facing]}), x, y, z)
	d := b.o.st.BlockEntityFor(b.c.Block(int(wx&15), int(wy), int(wz&15)), "", wx, wy, wz)
	if d == nil {
		return
	}
	table = "loot_tables/chests/" + table + ".json"
	d["LootTable"] = table
	d["LootTableSeed"] = int32(b.r.Uint32() >> 1)
	b.c.BlockEntities = append(b.c.BlockEntities, structure.BlockEntity{X: wx, Y: wy, Z: wz, Data: d, LootTable: table})
}

func (b *shBuild) build() {
	switch b.p.kind {
	case shStairsDown:
		b.buildStairsDown()
	case shStraight:
		b.buildStraight()
	case shLeftTurn, shRightTurn:
		b.buildTurn()
	case shPrisonHall:
		b.buildPrison()
	case shChestCorridor:
		b.buildChestCorridor()
	case shStraightStairsDown:
		b.buildStraightStairs()
	case shRoomCrossing:
		b.buildRoomCrossing()
	case shFiveCrossing:
		b.buildFiveCrossing()
	case shLibrary:
		b.buildLibrary()
	case shPortalRoom:
		b.buildPortalRoom()
	case shFiller:
		b.buildFiller()
	}
}

func (b *shBuild) buildStairsDown() {
	b.stones(0, 0, 0, 4, 10, 4)
	b.smallDoor(b.p.door, 1, 7, 0)
	b.smallDoor(shOpening, 1, 1, 4)
	sb, sl := b.stoneBricks, b.slab
	for _, s := range [...]struct {
		s       uint32
		x, y, z int32
	}{
		{sb, 2, 6, 1}, {sb, 1, 5, 1}, {sl, 1, 6, 1}, {sb, 1, 5, 2}, {sb, 1, 4, 3}, {sl, 1, 5, 3},
		{sb, 2, 4, 3}, {sb, 3, 3, 3}, {sl, 3, 4, 3}, {sb, 3, 3, 2}, {sb, 3, 2, 1}, {sl, 3, 3, 1},
		{sb, 2, 2, 1}, {sb, 1, 1, 1}, {sl, 1, 2, 1}, {sb, 1, 1, 2}, {sl, 1, 1, 3},
	} {
		b.put(s.s, s.x, s.y, s.z)
	}
}

func (b *shBuild) buildStraight() {
	p := b.p
	b.stones(0, 0, 0, 4, 4, 6)
	b.smallDoor(p.door, 1, 1, 0)
	b.smallDoor(shOpening, 1, 1, 6)
	west := b.torch(b.sideTorch())
	east := b.torch(shTable([4]int32{2, 4, 2, 4}, p.orient))
	b.maybeTorch(west, 1, 1)
	b.maybeTorch(east, 3, 1)
	b.maybeTorch(west, 1, 5)
	b.maybeTorch(east, 3, 5)
	if p.open[0] {
		b.box(0, 1, 2, 0, 3, 4, b.air, b.air)
	}
	if p.open[1] {
		b.box(4, 1, 2, 4, 3, 4, b.air, b.air)
	}
}

// maybeTorch places a wall torch at height 2 one time in ten.
func (b *shBuild) maybeTorch(s uint32, x, z int32) {
	if b.r.Float() < 0.1 {
		b.put(s, x, 2, z)
	}
}

func (b *shBuild) buildTurn() {
	p := b.p
	b.stones(0, 0, 0, 4, 4, 4)
	b.smallDoor(p.door, 1, 1, 0)
	toLeft := p.orient == 2 || p.orient == 3
	if p.kind == shRightTurn {
		toLeft = !toLeft
	}
	if toLeft {
		b.box(0, 1, 1, 0, 3, 3, b.air, b.air)
	} else {
		b.box(4, 1, 1, 4, 3, 3, b.air, b.air)
	}
}

func (b *shBuild) buildPrison() {
	b.stones(0, 0, 0, 8, 4, 10)
	b.smallDoor(b.p.door, 1, 1, 0)
	b.box(1, 1, 10, 3, 3, 10, b.air, b.air)
	for _, z := range [...]int32{1, 3, 7, 9} {
		b.stones(4, 1, z, 4, 3, z)
	}
	b.box(4, 1, 4, 4, 3, 6, b.bars, b.bars)
	b.box(5, 1, 5, 7, 3, 5, b.bars, b.bars)
	b.put(b.bars, 4, 3, 2)
	b.put(b.bars, 4, 3, 8)
}

func (b *shBuild) buildChestCorridor() {
	b.stones(0, 0, 0, 4, 4, 6)
	b.smallDoor(b.p.door, 1, 1, 0)
	b.smallDoor(shOpening, 1, 1, 6)
	b.box(3, 1, 2, 3, 1, 4, b.stoneBricks, b.air)
	for _, q := range [...][3]int32{{3, 1, 1}, {3, 1, 5}, {3, 2, 2}, {3, 2, 4}, {2, 1, 2}, {2, 1, 3}, {2, 1, 4}} {
		b.put(b.slab, q[0], q[1], q[2])
	}
	b.chest(3, 2, 3, 4, "stronghold_corridor")
}

func (b *shBuild) buildStraightStairs() {
	stair := b.o.state("minecraft:stone_stairs", map[string]any{"weirdo_direction": b.weirdo(2)})
	b.stones(0, 0, 0, 4, 10, 7)
	b.smallDoor(b.p.door, 1, 7, 0)
	b.smallDoor(shOpening, 1, 1, 7)
	for i := int32(0); i < 6; i++ {
		b.put(stair, 1, 6-i, 1+i)
		b.put(stair, 2, 6-i, 1+i)
		b.put(stair, 3, 6-i, 1+i)
		if i < 5 {
			b.put(b.stoneBricks, 1, 5-i, 1+i)
			b.put(b.stoneBricks, 2, 5-i, 1+i)
			b.put(b.stoneBricks, 3, 5-i, 1+i)
		}
	}
}

func (b *shBuild) buildRoomCrossing() {
	p := b.p
	b.stones(0, 0, 0, 10, 6, 10)
	b.smallDoor(p.door, 4, 1, 0)
	b.box(4, 1, 10, 6, 3, 10, b.air, b.air)
	b.box(0, 1, 4, 0, 3, 6, b.air, b.air)
	b.box(10, 1, 4, 10, 3, 6, b.air, b.air)
	sb := b.stoneBricks
	switch p.roomType {
	case 0:
		b.put(sb, 5, 1, 5)
		b.put(sb, 5, 2, 5)
		b.put(sb, 5, 3, 5)
		b.put(b.torch(shTable([4]int32{2, 4, 2, 4}, p.orient)), 4, 3, 5)
		b.put(b.torch(b.sideTorch()), 6, 3, 5)
		b.put(b.torch(shTable([4]int32{4, 1, 3, 2}, p.orient)), 5, 3, 4)
		v := int32(3)
		if p.orient >= 1 && p.orient <= 3 {
			v = [3]int32{2, 4, 1}[p.orient-1]
		}
		b.put(b.torch(v), 5, 3, 6)
		for _, q := range [...][2]int32{{4, 4}, {4, 5}, {4, 6}, {6, 4}, {6, 5}, {6, 6}, {5, 4}, {5, 6}} {
			b.put(b.smoothSlab, q[0], 1, q[1])
		}
	case 1:
		for _, q := range [...][2]int32{{3, 3}, {7, 3}, {3, 3}, {3, 7}, {3, 4}, {7, 4}, {4, 3}, {4, 7}, {3, 5}, {7, 5},
			{5, 3}, {5, 7}, {3, 6}, {7, 6}, {6, 3}, {6, 7}, {3, 7}, {7, 7}, {7, 3}, {7, 7}} {
			b.put(sb, q[0], 1, q[1])
		}
		b.put(sb, 5, 1, 5)
		b.put(sb, 5, 2, 5)
		b.put(sb, 5, 3, 5)
		b.put(b.water, 5, 4, 5)
	case 2:
		cob := b.cobble
		for z := int32(1); z <= 9; z++ {
			b.put(cob, 1, 3, z)
			b.put(cob, 9, 3, z)
		}
		for x := int32(1); x <= 9; x++ {
			b.put(cob, x, 3, 1)
			b.put(cob, x, 3, 9)
		}
		for _, q := range [...][3]int32{{5, 1, 4}, {5, 1, 6}, {5, 3, 4}, {5, 3, 6}, {4, 1, 5}, {6, 1, 5}, {4, 3, 5}, {6, 3, 5}} {
			b.put(cob, q[0], q[1], q[2])
		}
		for y := int32(1); y <= 3; y++ {
			b.put(cob, 4, y, 4)
			b.put(cob, 6, y, 4)
			b.put(cob, 4, y, 6)
			b.put(cob, 6, y, 6)
		}
		b.put(b.torch(0), 5, 3, 5)
		for z := int32(2); z <= 8; z++ {
			b.put(b.planks, 2, 3, z)
			b.put(b.planks, 3, 3, z)
			if z <= 3 || z >= 7 {
				b.put(b.planks, 4, 3, z)
				b.put(b.planks, 5, 3, z)
				b.put(b.planks, 6, 3, z)
			}
			b.put(b.planks, 7, 3, z)
			b.put(b.planks, 8, 3, z)
		}
		ladder := b.o.state("minecraft:ladder", map[string]any{"facing_direction": b.facing(4)})
		b.put(ladder, 9, 1, 3)
		b.put(ladder, 9, 2, 3)
		b.put(ladder, 9, 3, 3)
		b.chest(3, 4, 8, 4, "stronghold_crossing")
	}
}

func (b *shBuild) buildFiveCrossing() {
	p := b.p
	b.stones(0, 0, 0, 9, 8, 10)
	b.smallDoor(p.door, 4, 3, 0)
	if p.open[1] { // leftLow
		b.box(0, 3, 1, 0, 5, 3, b.air, b.air)
	}
	if p.open[3] { // rightLow
		b.box(9, 3, 1, 9, 5, 3, b.air, b.air)
	}
	if p.open[0] { // leftHigh
		b.box(0, 5, 7, 0, 7, 9, b.air, b.air)
	}
	if p.open[2] { // rightHigh
		b.box(9, 5, 7, 9, 7, 9, b.air, b.air)
	}
	b.box(5, 1, 10, 7, 3, 10, b.air, b.air)
	b.stones(1, 2, 1, 8, 2, 6)
	b.stones(4, 1, 5, 4, 4, 9)
	b.stones(8, 1, 5, 8, 4, 9)
	b.stones(1, 4, 7, 3, 4, 9)
	b.stones(1, 3, 5, 3, 3, 6)
	b.box(1, 3, 4, 3, 3, 4, b.slab, b.slab)
	b.box(1, 4, 6, 3, 4, 6, b.slab, b.slab)
	b.stones(5, 1, 7, 7, 1, 8)
	b.box(5, 1, 9, 7, 1, 9, b.slab, b.slab)
	b.box(5, 2, 7, 7, 2, 7, b.slab, b.slab)
	b.box(4, 5, 7, 4, 5, 9, b.slab, b.slab)
	b.box(8, 5, 7, 8, 5, 9, b.slab, b.slab)
	b.box(5, 5, 7, 7, 5, 9, b.smoothDouble, b.smoothDouble)
	b.put(b.torch(shTable([4]int32{4, 1, 3, 2}, p.orient)), 6, 5, 6)
}

func (b *shBuild) buildLibrary() {
	p := b.p
	h := int32(5)
	if p.tall {
		h = 10
	}
	b.stones(0, 0, 0, 13, h, 14)
	b.smallDoor(p.door, 4, 1, 0)
	b.maybeBox(0.07, 2, 1, 1, 11, 4, 13, b.web)
	west := b.torch(b.sideTorch())
	east := b.torch(shTable([4]int32{2, 4, 2, 4}, p.orient))
	for z := int32(1); z <= 13; z++ {
		s := b.shelf
		if (z-1)%4 == 0 {
			s = b.planks
		}
		b.box(1, 1, z, 1, 4, z, s, s)
		b.box(12, 1, z, 12, 4, z, s, s)
		if s == b.planks {
			b.put(west, 2, 3, z)
			b.put(east, 11, 3, z)
		}
		if p.tall {
			b.box(1, 6, z, 1, 9, z, s, s)
			b.box(12, 6, z, 12, 9, z, s, s)
		}
	}
	for z := int32(3); z <= 11; z += 2 {
		b.box(3, 1, z, 4, 3, z, b.shelf, b.shelf)
		b.box(6, 1, z, 7, 3, z, b.shelf, b.shelf)
		b.box(9, 1, z, 10, 3, z, b.shelf, b.shelf)
	}
	if p.tall {
		pl, fe := b.planks, b.fence
		b.box(1, 5, 1, 3, 5, 13, pl, pl)
		b.box(10, 5, 1, 12, 5, 13, pl, pl)
		b.box(4, 5, 1, 9, 5, 2, pl, pl)
		b.box(4, 5, 12, 9, 5, 13, pl, pl)
		b.put(pl, 9, 5, 11)
		b.put(pl, 8, 5, 11)
		b.put(pl, 9, 5, 10)
		b.box(3, 6, 2, 3, 6, 12, fe, fe)
		b.box(10, 6, 2, 10, 6, 10, fe, fe)
		b.box(4, 6, 2, 9, 6, 2, fe, fe)
		b.box(4, 6, 12, 8, 6, 12, fe, fe)
		b.put(fe, 9, 6, 11)
		b.put(fe, 8, 6, 11)
		b.put(fe, 9, 6, 10)
		ladder := b.o.state("minecraft:ladder", map[string]any{"facing_direction": b.facing(3)})
		for y := int32(1); y <= 7; y++ {
			b.put(ladder, 10, y, 13)
		}
		for _, q := range [...][3]int32{{6, 9, 7}, {7, 9, 7}, {6, 8, 7}, {7, 8, 7}, {6, 7, 7}, {7, 7, 7}, {5, 7, 7},
			{8, 7, 7}, {6, 7, 6}, {6, 7, 8}, {7, 7, 6}, {7, 7, 8}} {
			b.put(fe, q[0], q[1], q[2])
		}
		top := b.torch(5)
		for _, q := range [...][3]int32{{5, 8, 7}, {8, 8, 7}, {6, 8, 6}, {6, 8, 8}, {7, 8, 6}, {7, 8, 8}} {
			b.put(top, q[0], q[1], q[2])
		}
	}
	b.chest(3, 3, 5, 4, "stronghold_library")
	if p.tall {
		b.chest(12, 8, 1, 2, "stronghold_library")
	}
}

func (b *shBuild) buildPortalRoom() {
	p := b.p
	b.stones(0, 0, 0, 10, 7, 15)
	b.smallDoor(shGrates, 4, 1, 0)
	b.stones(1, 6, 1, 1, 6, 14)
	b.stones(9, 6, 1, 9, 6, 14)
	b.stones(2, 6, 1, 8, 6, 2)
	b.stones(2, 6, 14, 8, 6, 14)
	b.stones(1, 1, 1, 2, 1, 4)
	b.stones(8, 1, 1, 9, 1, 4)
	b.box(1, 1, 1, 1, 1, 3, b.lava, b.lava)
	b.box(9, 1, 1, 9, 1, 3, b.lava, b.lava)
	b.stones(3, 1, 8, 7, 1, 12)
	b.box(4, 1, 9, 6, 1, 11, b.lava, b.lava)
	for z := int32(3); z <= 13; z += 2 {
		b.box(0, 3, z, 0, 4, z, b.bars, b.bars)
		b.box(10, 3, z, 10, 4, z, b.bars, b.bars)
	}
	for x := int32(2); x <= 8; x += 2 {
		b.box(x, 3, 15, x, 4, 15, b.bars, b.bars)
	}
	stair := b.o.state("minecraft:stone_brick_stairs", map[string]any{"weirdo_direction": b.weirdo(3)})
	b.stones(4, 1, 5, 6, 1, 7)
	b.stones(4, 2, 6, 6, 2, 7)
	b.stones(4, 3, 7, 6, 3, 7)
	for x := int32(4); x <= 6; x++ {
		b.put(stair, x, 1, 4)
		b.put(stair, x, 2, 5)
		b.put(stair, x, 3, 6)
	}
	o := p.orient
	dirs := [4]int32{2, 0, 3, 1}
	if o >= 0 && o <= 3 {
		dirs = [4]int32{o, [4]int32{2, 3, 0, 1}[o], [4]int32{3, 0, 3, 0}[o], [4]int32{1, 2, 1, 2}[o]}
	}
	frames := [12]struct {
		x, z int32
		dir  int32
	}{
		{4, 8, dirs[0]}, {5, 8, dirs[0]}, {6, 8, dirs[0]},
		{4, 12, dirs[1]}, {5, 12, dirs[1]}, {6, 12, dirs[1]},
		{3, 9, dirs[2]}, {3, 10, dirs[2]}, {3, 11, dirs[2]},
		{7, 9, dirs[3]}, {7, 10, dirs[3]}, {7, 11, dirs[3]},
	}
	all := true
	for _, f := range frames {
		eye := b.r.Float() > 0.9
		all = all && eye
		b.put(b.o.state("minecraft:end_portal_frame", map[string]any{
			"minecraft:cardinal_direction": shCard[f.dir&3], "end_portal_eye_bit": uint8(b2i(eye))}), f.x, 3, f.z)
	}
	if all {
		for x := int32(4); x <= 6; x++ {
			for z := int32(9); z <= 11; z++ {
				b.put(b.portal, x, 3, z)
			}
		}
	}
	wx, wy, wz := p.worldPos(5, 3, 6)
	if b.cbox.Contains([3]int32{wx, wy, wz}) {
		sp := b.id("mob_spawner")
		if b.setWorld(wx, wy, wz, sp) {
			b.dropEntity([3]int32{wx, wy, wz})
			if d := b.o.st.BlockEntityFor(sp, "", wx, wy, wz); d != nil {
				d["EntityIdentifier"] = "minecraft:silverfish"
				b.c.BlockEntities = append(b.c.BlockEntities, structure.BlockEntity{X: wx, Y: wy, Z: wz, Data: d})
			}
		}
	}
}

func (b *shBuild) buildFiller() {
	for i := int32(0); i < b.p.steps; i++ {
		for x := int32(0); x <= 4; x++ {
			b.put(b.stoneBricks, x, 0, i)
			b.put(b.stoneBricks, x, 4, i)
		}
		for y := int32(1); y <= 3; y++ {
			b.put(b.stoneBricks, 0, y, i)
			for x := int32(1); x <= 3; x++ {
				b.put(b.air, x, y, i)
			}
			b.put(b.stoneBricks, 4, y, i)
		}
	}
}

// NearestStronghold finds the stronghold whose start chunk is nearest to
// block (x, z), searching maxCells 200-chunk grid cells out, and returns
// a spot inside its portal room (or its start, if it has none). It is
// safe for concurrent use.
func (o *Overworld) NearestStronghold(x, z, maxCells int32) (Scattered, bool) {
	best, found := int64(-1), [2]int32{}
	try := func(cx, cz int32) {
		dx, dz := int64(cx*16+8-x), int64(cz*16+8-z)
		if d := dx*dx + dz*dz; best < 0 || d < best {
			best, found = d, [2]int32{cx, cz}
		}
	}
	for _, c := range o.strongholdFirst() {
		try(c[0], c[1])
	}
	gx0, gz0 := int32(math.Floor(float64(x>>4)/200)), int32(math.Floor(float64(z>>4)/200))
	for gx := gx0 - maxCells; gx <= gx0+maxCells; gx++ {
		for gz := gz0 - maxCells; gz <= gz0+maxCells; gz++ {
			if cx, cz, ok := o.strongholdCell(gx*200, gz*200); ok && uint32(cx*cx+cz*cz) >= 100 {
				try(cx, cz)
			}
		}
	}
	if best < 0 {
		return Scattered{}, false
	}
	s := o.strongholdAt(found[0], found[1])
	if s == nil {
		return Scattered{}, false
	}
	p := s.portal
	if p == nil {
		p = s.pieces[0]
	}
	px, py, pz := p.worldPos(5, 1, 2)
	if p.kind != shPortalRoom {
		px, py, pz = p.worldPos(2, 1, 2)
	}
	return Scattered{Kind: "stronghold", X: px, Y: py, Z: pz}, true
}
