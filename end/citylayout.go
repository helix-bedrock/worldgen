package end

import (
	"github.com/helix-bedrock/worldgen/rand"
	"github.com/helix-bedrock/worldgen/structure"
)

// cityPiece is one template of a city.
type cityPiece struct {
	name string
	pos  [3]int32
	rot  int
	// overwrite places the template's air too; otherwise it is skipped.
	overwrite bool
	box       structure.Box
}

// city is a laid-out End city: its pieces in placement order.
type city struct {
	pieces []cityPiece
	box    structure.Box
}

// cityGen is one of the four generators; the order is BDS's.
type cityGen int

const (
	genTower cityGen = iota
	genFatTower
	genBridge
	genHouseTower
)

// cityLayout lays out the pieces of a city: the start's house, then the
// generators, each drawing from r.
type cityLayout struct {
	e      *End
	r      *rand.MT
	pieces []cityPiece
	// shipBuilt is the bridge generator's flag: one ship per city.
	shipBuilt bool
}

// bool is BDS's nextBoolean: bit 27 of a draw (sub_ED4A020).
func (l *cityLayout) bool() bool { return l.r.Uint32()&0x8000000 != 0 }

// piece adds a template at pos.
func (l *cityLayout) piece(name string, pos [3]int32, rot int, overwrite bool) cityPiece {
	p := cityPiece{name: name, pos: pos, rot: rot, overwrite: overwrite}
	if jt := l.e.st.Template("endcity/" + name); jt != nil {
		s := jt.Template.Size
		a := structure.RotatePos([3]int32{s[0] - 1, s[1] - 1, s[2] - 1}, rot)
		p.box = structure.NewBox(pos[0]+min(0, a[0]), pos[1], pos[2]+min(0, a[2]),
			pos[0]+max(0, a[0]), pos[1]+a[1], pos[2]+max(0, a[2]))
	}
	l.pieces = append(l.pieces, p)
	return p
}

// add places a template joined to parent at offset, turned by the
// parent's turn (StructureTemplate's connected position, sub_D2B50C0).
func (l *cityLayout) add(parent cityPiece, offset [3]int32, name string, rot int, overwrite bool) cityPiece {
	o := structure.RotatePos(offset, parent.rot)
	return l.piece(name, [3]int32{parent.pos[0] + o[0], parent.pos[1] + o[1], parent.pos[2] + o[2]}, rot, overwrite)
}

// children runs a generator below the depth limit (sub_4E41EA0): when it
// fails, the pieces it added are dropped; when it succeeds, one more draw
// follows (Java's depth tag).
func (l *cityLayout) children(g cityGen, depth int32, parent cityPiece, offset [3]int32) bool {
	if depth >= 9 {
		return false
	}
	n := len(l.pieces)
	var ok bool
	switch g {
	case genTower:
		ok = l.tower(depth, parent)
	case genFatTower:
		ok = l.fatTower(depth, parent)
	case genBridge:
		ok = l.bridge(depth, parent)
	default:
		ok = l.houseTower(depth, parent, offset)
	}
	if !ok {
		l.pieces = l.pieces[:n]
		return false
	}
	l.r.Uint32()
	return true
}

// cityBridge is where a bridge leaves a tower, and its turn.
type cityBridge struct {
	rot int
	pos [3]int32
}

// towerBridges are a tower's (qword_F456CD0), fatTowerBridges a fat
// tower's (qword_F456CF0).
var (
	towerBridges    = [4]cityBridge{{0, [3]int32{1, -1, 0}}, {1, [3]int32{6, -1, 1}}, {3, [3]int32{0, -1, 5}}, {2, [3]int32{5, -1, 6}}}
	fatTowerBridges = [4]cityBridge{{0, [3]int32{4, -1, 0}}, {1, [3]int32{12, -1, 4}}, {3, [3]int32{0, -1, 8}}, {2, [3]int32{8, -1, 12}}}
)

// tower is the tower generator (sub_4E42570): a base, two to four pieces,
// then bridges from one of the pieces, or a fat tower on top.
func (l *cityLayout) tower(depth int32, parent cityPiece) bool {
	rot := parent.rot
	dx := l.r.IntN(2)
	dz := l.r.IntN(2)
	p := l.add(parent, [3]int32{3 + dx, -3, 3 + dz}, "tower_base", rot, true)
	p = l.add(p, [3]int32{0, 7, 0}, "tower_piece", rot, true)
	var from cityPiece
	bridged := l.r.IntN(3) == 0
	if bridged {
		from = p
	}
	n := l.r.IntN(3)
	for i := int32(0); i <= n; i++ {
		p = l.add(p, [3]int32{0, 4, 0}, "tower_piece", rot, true)
		if i < n && l.bool() {
			from, bridged = p, true
		}
	}
	if !bridged {
		if depth != 7 {
			return l.children(genFatTower, depth+1, p, [3]int32{})
		}
		l.add(p, [3]int32{-1, 4, -1}, "tower_top", rot, true)
		return true
	}
	for _, b := range towerBridges {
		if !l.bool() {
			continue
		}
		end := l.add(from, b.pos, "bridge_end", (rot+b.rot)%4, true)
		l.children(genBridge, depth+1, end, [3]int32{})
	}
	l.add(p, [3]int32{-1, 4, -1}, "tower_top", rot, true)
	return true
}

// fatTower is the fat tower generator (sub_4E43EF0): a base, a middle, up
// to two more middles with bridges, and a top.
func (l *cityLayout) fatTower(depth int32, parent cityPiece) bool {
	rot := parent.rot
	p := l.add(parent, [3]int32{-3, 4, -3}, "fat_tower_base", rot, true)
	p = l.add(p, [3]int32{0, 4, 0}, "fat_tower_middle", rot, true)
	for i := 0; i < 2 && l.r.IntN(3) != 0; i++ {
		p = l.add(p, [3]int32{0, 8, 0}, "fat_tower_middle", rot, true)
		for _, b := range fatTowerBridges {
			if !l.bool() {
				continue
			}
			end := l.add(p, b.pos, "bridge_end", (rot+b.rot)%4, true)
			l.children(genBridge, depth+1, end, [3]int32{})
		}
	}
	l.add(p, [3]int32{-2, 8, -2}, "fat_tower_top", rot, true)
	return true
}

// bridge is the bridge generator (sub_4E45340): one to four bridge parts,
// then the city's one ship or a house tower, and a bridge end facing back.
func (l *cityLayout) bridge(depth int32, parent cityPiece) bool {
	rot := parent.rot
	n := l.r.IntN(4) + 1
	p := l.add(parent, [3]int32{0, 0, -4}, "bridge_piece", rot, true)
	y := int32(0)
	for range n {
		// Each test draws: a flat piece, else steep stairs, else gentle.
		if l.bool() {
			p = l.add(p, [3]int32{0, y, -4}, "bridge_piece", rot, true)
			y = 0
			continue
		}
		if l.bool() {
			p = l.add(p, [3]int32{0, y, -4}, "bridge_steep_stairs", rot, true)
		} else {
			p = l.add(p, [3]int32{0, y, -8}, "bridge_gentle_stairs", rot, true)
		}
		y = 4
	}
	if !l.shipBuilt && l.r.IntN(10-depth) == 0 {
		sx := l.r.IntN(8)
		sz := l.r.IntN(10)
		l.add(p, [3]int32{sx - 8, y, sz - 70}, "ship", rot, true)
		l.shipBuilt = true
	} else if !l.children(genHouseTower, depth+1, p, [3]int32{-3, y + 1, -11}) {
		return false
	}
	l.add(p, [3]int32{4, y, 0}, "bridge_end", (rot+2)%4, true)
	return true
}

// houseTower is the house tower generator (sub_4E46930): a house of one to
// three floors, the taller ones with a tower on top.
func (l *cityLayout) houseTower(depth int32, parent cityPiece, offset [3]int32) bool {
	rot := parent.rot
	p := l.add(parent, offset, "base_floor", rot, true)
	switch l.r.IntN(3) {
	case 0:
		l.add(p, [3]int32{-1, 4, -1}, "base_roof", rot, true)
	case 1:
		p = l.add(p, [3]int32{-1, 0, -1}, "second_floor_2", rot, false)
		p = l.add(p, [3]int32{-1, 8, -1}, "second_roof", rot, false)
		l.children(genTower, depth+1, p, [3]int32{})
	default:
		p = l.add(p, [3]int32{-1, 0, -1}, "second_floor_2", rot, false)
		p = l.add(p, [3]int32{-1, 4, -1}, "third_floor_c", rot, false)
		p = l.add(p, [3]int32{-1, 8, -1}, "third_roof", rot, true)
		l.children(genTower, depth+1, p, [3]int32{})
	}
	return true
}

// layCity lays out the city starting in chunk (cx, cz) at height y. The
// generators draw from the placement random, after the four draws that
// picked the chunk (sub_4E400E0 hands it on to the start).
func (e *End) layCity(cx, cz int32, rot int, y int32) *city {
	l := &cityLayout{e: e, r: e.cityRandom(structure.Region(cx, citySpacing), structure.Region(cz, citySpacing))}
	for range 4 {
		l.r.IntN(citySpacing - citySeparation)
	}
	p := l.piece("base_floor", [3]int32{cx*16 | 8, y, cz*16 | 8}, rot, true)
	p = l.add(p, [3]int32{-1, 0, -1}, "second_floor", rot, false)
	p = l.add(p, [3]int32{-1, 4, -1}, "third_floor", rot, false)
	p = l.add(p, [3]int32{-1, 8, -1}, "third_roof", rot, true)
	l.children(genTower, 1, p, [3]int32{})
	c := &city{pieces: l.pieces, box: l.pieces[0].box}
	for _, p := range c.pieces {
		c.box = structure.NewBox(min(c.box.X0, p.box.X0), min(c.box.Y0, p.box.Y0), min(c.box.Z0, p.box.Z0),
			max(c.box.X1, p.box.X1), max(c.box.Y1, p.box.Y1), max(c.box.Z1, p.box.Z1))
	}
	return c
}
