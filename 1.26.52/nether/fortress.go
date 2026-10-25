package nether

import (
	"slices"

	"github.com/helix-bedrock/worldgen/1.26.52/internal/mathx"
	"github.com/helix-bedrock/worldgen/1.26.52/rand"
	"github.com/helix-bedrock/worldgen/1.26.52/structure"
)

// Nether fortresses, written from BDS 1.26.52: the placement shared with
// bastions (sub_4FD0B70), the fortress feature's biome list (its
// constructor, sub_4E4F860), the start (sub_4E4FFB0), the piece generator
// (sub_4E513B0), the end filler (sub_4E51210) and the pieces' children and
// builds (their vtables' slots 4 and 5). The layout is Java's
// NetherFortressPieces with Bedrock's random and these differences, all
// kept:
//   - a child that would start more than 112 blocks from the start is an
//     end filler that is kept (Java makes one and drops it);
//   - the T balcony fills six rows of columns down, not five;
//   - the castle entrance's and stalk room's top corner fences are placed
//     as BDS lists them, (0, 13, 0) twice;
//   - the end filler draws its blocks from a Mersenne Twister of its own,
//     raw values modulo the size.
//
// A piece's blocks go into the chunks it reaches with a Mersenne Twister
// seeded with each chunk's population seed (only the chests' loot seeds
// draw from it), on the terrain before decoration, as the overworld's
// structures. The lava BDS's pieces leave is only given block ticks, which
// change nothing here.

// Fortress piece kinds; nbStart is the bridge crossing the fortress starts
// with.
const (
	nbStart uint8 = iota
	nbBridgeStraight
	nbBridgeCrossing
	nbRoomCrossing
	nbStairsRoom
	nbMonsterThrone
	nbCastleEntrance
	nbEndFiller
	nbCorridor
	nbCorridorCrossing
	nbRightTurn
	nbLeftTurn
	nbCorridorStairs
	nbTBalcony
	nbStalkRoom
	nbNone uint8 = 255
)

// complexSpacing and complexRange are the grid of the fortress and bastion
// starts: one candidate chunk in each 30 by 30 chunks, 26 wide.
const (
	complexSpacing = 30
	complexRange   = 26
	// complexKinds and fortressKinds: a start is a fortress when a draw
	// below complexKinds is below fortressKinds, else a bastion.
	complexKinds  = 6
	fortressKinds = 2
	complexSalt   = 30084232
	// nbReach is how many chunks from its start chunk a fortress can reach:
	// a piece starts at most 112 blocks from the start's corner and runs up
	// to 18 further.
	nbReach = 9
)

// box is a block box, both corners inside.
type box struct{ x0, y0, z0, x1, y1, z1 int32 }

func (b box) intersects(c box) bool {
	return b.x0 <= c.x1 && b.x1 >= c.x0 && b.y0 <= c.y1 && b.y1 >= c.y0 && b.z0 <= c.z1 && b.z1 >= c.z0
}

func (b box) contains(x, y, z int32) bool {
	return x >= b.x0 && x <= b.x1 && y >= b.y0 && y <= b.y1 && z >= b.z0 && z <= b.z1
}

// orientedBox is a piece box at (x, y, z) heading o (0 south, 1 west, 2
// north, 3 east), with the side and height offsets and the size of the
// piece seen facing south.
func orientedBox(x, y, z, ox, oy, w, h, d, o int32) box {
	y0, y1 := y+oy, y+oy+h-1
	switch o {
	case 1:
		return box{x - d + 1, y0, z + ox, x, y1, z + ox + w - 1}
	case 2:
		return box{x + ox, y0, z - d + 1, x + ox + w - 1, y1, z}
	case 3:
		return box{x, y0, z + ox, x + d - 1, y1, z + ox + w - 1}
	}
	return box{x + ox, y0, z, x + ox + w - 1, y1, z + d - 1}
}

// complexStart is the candidate chunk of grid region (rx, rz) and the
// random it was drawn from, which the start goes on with.
func (n *Nether) complexStart(rx, rz int32, r *rand.MT) (int32, int32) {
	r.Seed(structure.RegionSeed(n.seed, complexSalt, rx, rz))
	cx := complexSpacing*rx + r.IntN(complexRange)
	cz := complexSpacing*rz + r.IntN(complexRange)
	return cx, cz
}

// complexBiomesOK is the biome test (the biome source's area test, radius
// 2 around the chunk's corner): every column sampled is in ok.
func (n *Nether) complexBiomesOK(cx, cz int32, ok func(b uint32) bool) bool {
	for _, x := range [2]int32{cx*16 - 2, cx*16 + 2} {
		for _, z := range [2]int32{cz*16 - 2, cz*16 + 2} {
			if !ok(n.biomes.column(x, z)) {
				return false
			}
		}
	}
	return true
}

// fortressChunk reports whether a fortress starts in chunk (cx, cz) and
// leaves r where the start goes on. A fortress may stand in every Nether
// biome; a third of the grid's starts are fortresses, the rest bastions.
func (n *Nether) fortressChunk(cx, cz int32, r *rand.MT) bool {
	rx, rz := mathx.FloorDiv(cx, complexSpacing), mathx.FloorDiv(cz, complexSpacing)
	sx, sz := n.complexStart(rx, rz, r)
	if at, ok := n.forcedIn(gridComplex, rx, rz); ok {
		// The kind draw still happens: the start goes on after it.
		r.IntN(complexKinds)
		return at.kind == skFortress && at.chunk == [2]int32{cx, cz}
	}
	if sx != cx || sz != cz || !n.gate.OK(skFortress, cx, cz) {
		return false
	}
	if !n.complexBiomesOK(cx, cz, func(uint32) bool { return true }) {
		return false
	}
	return r.IntN(complexKinds) < fortressKinds
}

type nbPiece struct {
	kind   uint8
	bb     box
	orient int32 // 0 south, 1 west, 2 north, 3 east
	depth  int32
	chest  bool   // turns: holds a chest
	seed   uint32 // end filler: its random's seed
}

// worldPos turns piece-local coordinates into world coordinates.
func (p *nbPiece) worldPos(x, y, z int32) (int32, int32, int32) {
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

// fortress is an assembled fortress and the box around its pieces.
type fortress struct {
	pieces []*nbPiece
	bb     box
}

// nbWeight is one entry of a piece list.
type nbWeight struct {
	kind        uint8
	weight, max int32
	row         bool // may follow a piece of its own kind
	placed      int32
}

type nbLayout struct {
	pieces, pending []*nbPiece
	bridge, castle  []nbWeight
	prev            uint8
	start           box
}

// fortressAt assembles the fortress starting in chunk (cx, cz), if any.
func (n *Nether) fortressAt(cx, cz int32) *fortress {
	var r rand.MT
	if !n.fortressChunk(cx, cz, &r) {
		return nil
	}
	if v, ok := n.fortresses.Load([2]int32{cx, cz}); ok {
		f, _ := v.(*fortress)
		return f
	}
	f := n.layFortress(cx, cz, &r)
	v, _ := n.fortresses.LoadOrStore([2]int32{cx, cz}, f)
	f, _ = v.(*fortress)
	return f
}

// layFortress is the start: a bridge crossing at y 64 and the pieces grown
// from it, then moved to a random height between 48 and 70.
func (n *Nether) layFortress(cx, cz int32, r *rand.MT) *fortress {
	l := &nbLayout{prev: nbNone,
		bridge: []nbWeight{
			{nbBridgeStraight, 30, 0, true, 0}, {nbBridgeCrossing, 10, 4, false, 0}, {nbRoomCrossing, 10, 4, false, 0},
			{nbStairsRoom, 10, 3, false, 0}, {nbMonsterThrone, 5, 2, false, 0}, {nbCastleEntrance, 5, 1, false, 0},
		},
		castle: []nbWeight{
			{nbCorridor, 25, 0, true, 0}, {nbCorridorCrossing, 15, 5, false, 0}, {nbRightTurn, 5, 10, false, 0},
			{nbLeftTurn, 5, 10, false, 0}, {nbCorridorStairs, 10, 3, true, 0}, {nbTBalcony, 7, 2, false, 0},
			{nbStalkRoom, 5, 2, false, 0},
		},
	}
	start := &nbPiece{kind: nbStart, orient: r.IntN(4)}
	start.bb = box{cx*16 + 2, 64, cz*16 + 2, cx*16 + 20, 73, cz*16 + 20}
	l.start = start.bb
	l.pieces = []*nbPiece{start}
	l.children(start, r)
	for len(l.pending) > 0 {
		i := r.IntN(int32(len(l.pending)))
		p := l.pending[i]
		l.pending = append(l.pending[:i], l.pending[i+1:]...)
		l.children(p, r)
	}
	f := &fortress{pieces: l.pieces, bb: l.pieces[0].bb}
	for _, p := range f.pieces[1:] {
		f.bb = box{min(f.bb.x0, p.bb.x0), min(f.bb.y0, p.bb.y0), min(f.bb.z0, p.bb.z0),
			max(f.bb.x1, p.bb.x1), max(f.bb.y1, p.bb.y1), max(f.bb.z1, p.bb.z1)}
	}
	// Moved to a random height: the box's bottom between 48 and 70 minus
	// its height (the dimension's sea level plus 16 and 38).
	y := int32(SeaLevel + 16)
	if span := f.bb.y0 + SeaLevel + 38 - f.bb.y1 - (SeaLevel + 16); span >= 2 {
		y += r.IntN(span)
	}
	dy := y - f.bb.y0
	f.bb.y0 += dy
	f.bb.y1 += dy
	for _, p := range f.pieces {
		p.bb.y0 += dy
		p.bb.y1 += dy
	}
	return f
}

func (l *nbLayout) collides(bb box) bool {
	for _, p := range l.pieces {
		if p.bb.intersects(bb) {
			return true
		}
	}
	return false
}

func (l *nbLayout) add(p *nbPiece) {
	l.pieces = append(l.pieces, p)
	l.pending = append(l.pending, p)
}

// filler is the end filler at (x, y, z) heading o, if its box is high
// enough and free.
func (l *nbLayout) filler(r *rand.MT, x, y, z, o, depth int32) *nbPiece {
	bb := orientedBox(x, y, z, -1, -3, 5, 10, 8, o)
	if bb.y0 < 11 || l.collides(bb) {
		return nil
	}
	return &nbPiece{kind: nbEndFiller, bb: bb, orient: o, depth: depth, seed: r.Uint32() >> 1}
}

// create makes a piece of kind k at (x, y, z) heading o if its box is high
// enough and free.
func (l *nbLayout) create(k uint8, r *rand.MT, x, y, z, o, depth int32) *nbPiece {
	var bb box
	switch k {
	case nbBridgeStraight:
		bb = orientedBox(x, y, z, -1, -3, 5, 10, 19, o)
	case nbBridgeCrossing:
		bb = orientedBox(x, y, z, -8, -3, 19, 10, 19, o)
	case nbRoomCrossing:
		bb = orientedBox(x, y, z, -2, 0, 7, 9, 7, o)
	case nbStairsRoom:
		bb = orientedBox(x, y, z, -2, 0, 7, 11, 7, o)
	case nbMonsterThrone:
		bb = orientedBox(x, y, z, -2, 0, 7, 8, 9, o)
	case nbCastleEntrance, nbStalkRoom:
		bb = orientedBox(x, y, z, -5, -3, 13, 14, 13, o)
	case nbCorridor, nbCorridorCrossing, nbRightTurn, nbLeftTurn:
		bb = orientedBox(x, y, z, -1, 0, 5, 7, 5, o)
	case nbCorridorStairs:
		bb = orientedBox(x, y, z, -1, -7, 5, 14, 10, o)
	case nbTBalcony:
		bb = orientedBox(x, y, z, -3, 0, 9, 7, 9, o)
	default:
		return nil
	}
	if bb.y0 < 11 || l.collides(bb) {
		return nil
	}
	p := &nbPiece{kind: k, bb: bb, orient: o, depth: depth}
	if k == nbRightTurn || k == nbLeftTurn {
		p.chest = r.IntN(3) == 0
	}
	return p
}

// next picks and adds the piece at (x, y, z) heading o, from the castle
// list or the bridge list.
func (l *nbLayout) next(r *rand.MT, x, y, z, o, depth int32, castle bool) {
	if mathx.Abs(x-l.start.x0) > 112 || mathx.Abs(z-l.start.z0) > 112 {
		if p := l.filler(r, x, y, z, o, depth); p != nil {
			l.add(p)
		}
		return
	}
	weights := &l.bridge
	if castle {
		weights = &l.castle
	}
	total, open := int32(0), false
	for _, w := range *weights {
		open = open || w.max > 0 && w.placed < w.max
		total += w.weight
	}
	if depth <= 29 && open && total > 0 {
		for range 5 {
			left := r.IntN(total)
			ws := *weights
			for i := range ws {
				w := &ws[i]
				if left -= w.weight; left >= 0 {
					continue
				}
				if w.max != 0 && w.placed >= w.max || w.kind == l.prev && !w.row {
					break
				}
				p := l.create(w.kind, r, x, y, z, o, depth+1)
				if p == nil {
					continue
				}
				w.placed++
				l.prev = w.kind
				if w.max != 0 && w.placed >= w.max {
					*weights = slices.Delete(ws, i, i+1)
				}
				l.add(p)
				return
			}
		}
	}
	if p := l.filler(r, x, y, z, o, depth+1); p != nil {
		l.add(p)
	}
}

// forward, left and right add a child beyond the piece's far end or its
// sides.
func (l *nbLayout) forward(p *nbPiece, r *rand.MT, xOff, yOff int32, castle bool) {
	b := p.bb
	switch p.orient {
	case 0:
		l.next(r, b.x0+xOff, b.y0+yOff, b.z1+1, 0, p.depth, castle)
	case 1:
		l.next(r, b.x0-1, b.y0+yOff, b.z0+xOff, 1, p.depth, castle)
	case 2:
		l.next(r, b.x0+xOff, b.y0+yOff, b.z0-1, 2, p.depth, castle)
	case 3:
		l.next(r, b.x1+1, b.y0+yOff, b.z0+xOff, 3, p.depth, castle)
	}
}

func (l *nbLayout) left(p *nbPiece, r *rand.MT, yOff, zOff int32, castle bool) {
	b := p.bb
	switch p.orient {
	case 0, 2:
		l.next(r, b.x0-1, b.y0+yOff, b.z0+zOff, 1, p.depth, castle)
	case 1, 3:
		l.next(r, b.x0+zOff, b.y0+yOff, b.z0-1, 2, p.depth, castle)
	}
}

func (l *nbLayout) right(p *nbPiece, r *rand.MT, yOff, zOff int32, castle bool) {
	b := p.bb
	switch p.orient {
	case 0, 2:
		l.next(r, b.x1+1, b.y0+yOff, b.z0+zOff, 3, p.depth, castle)
	case 1, 3:
		l.next(r, b.x0+zOff, b.y0+yOff, b.z1+1, 0, p.depth, castle)
	}
}

// children adds a piece's children, in BDS's order.
func (l *nbLayout) children(p *nbPiece, r *rand.MT) {
	switch p.kind {
	case nbStart, nbBridgeCrossing:
		l.forward(p, r, 8, 3, false)
		l.left(p, r, 3, 8, false)
		l.right(p, r, 3, 8, false)
	case nbBridgeStraight:
		l.forward(p, r, 1, 3, false)
	case nbRoomCrossing:
		l.forward(p, r, 2, 0, false)
		l.left(p, r, 0, 2, false)
		l.right(p, r, 0, 2, false)
	case nbStairsRoom:
		l.right(p, r, 6, 2, false)
	case nbCastleEntrance:
		l.forward(p, r, 5, 3, true)
	case nbStalkRoom:
		l.forward(p, r, 5, 3, true)
		l.forward(p, r, 5, 11, true)
	case nbCorridor, nbCorridorStairs:
		l.forward(p, r, 1, 0, true)
	case nbCorridorCrossing:
		l.forward(p, r, 1, 0, true)
		l.left(p, r, 0, 1, true)
		l.right(p, r, 0, 1, true)
	case nbRightTurn:
		l.right(p, r, 0, 1, true)
	case nbLeftTurn:
		l.left(p, r, 0, 1, true)
	case nbTBalcony:
		off := int32(1)
		if p.orient == 1 || p.orient == 2 {
			off = 5
		}
		l.left(p, r, 0, off, r.IntN(8) > 0)
		l.right(p, r, 0, off, r.IntN(8) > 0)
	}
}

// NearestFortress finds the fortress start nearest to block (x, z),
// searching the 30-chunk grid ring by ring up to maxRings out. It returns
// the start piece's middle, at its height.
func (n *Nether) NearestFortress(x, z, maxRings int32) (fx, fy, fz int32, found bool) {
	rx0, rz0 := mathx.FloorDiv(x>>4, complexSpacing), mathx.FloorDiv(z>>4, complexSpacing)
	best, foundRing := int64(-1), int32(-1)
	var r rand.MT
	for ring := int32(0); ring <= maxRings; ring++ {
		for rx := rx0 - ring; rx <= rx0+ring; rx++ {
			for rz := rz0 - ring; rz <= rz0+ring; rz++ {
				if mathx.Abs(rx-rx0) != ring && mathx.Abs(rz-rz0) != ring {
					continue
				}
				cx, cz := n.complexPick(rx, rz, &r)
				if !n.fortressChunk(cx, cz, &r) {
					continue
				}
				f := n.fortressAt(cx, cz)
				if f == nil {
					continue
				}
				s := f.pieces[0].bb
				mx, mz := (s.x0+s.x1)/2, (s.z0+s.z1)/2
				d := int64(mx-x)*int64(mx-x) + int64(mz-z)*int64(mz-z)
				if best < 0 || d < best {
					best, fx, fy, fz, found = d, mx, s.y0+5, mz, true
				}
			}
		}
		// A start one ring further out can still be nearer; two cannot.
		if found && foundRing < 0 {
			foundRing = ring
		}
		if found && ring > foundRing {
			return
		}
	}
	return
}
