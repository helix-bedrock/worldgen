package overworld

import (
	"cmp"
	"slices"

	"github.com/helix-bedrock/worldgen/rand"
)

// Ocean monuments, written from BDS 1.26.52: the start (sub_4E5EAA0,
// sub_4E5EC70), MonumentBuilding (constructor sub_4E5FB80, postProcess
// sub_4E63B10, wing sub_4E6B560) and its rooms (entry sub_4E6C260, simple
// sub_4E6CC20, simple top sub_4E6EC20, double Y sub_4E6F990, double X
// sub_4E70800, double Z sub_4E72500, double XY sub_4E74600, double YZ
// sub_4E77080, core sub_4E79C40, wing sub_4E7A850, penthouse sub_4E7B8B0).
// The layout is Java's OceanMonumentPieces; Bedrock differs in:
//   - the start's random is reseeded from the world seed and two floats
//     scaled by the chunk position;
//   - the room list is shuffled by a fresh random seeded like the start's
//     (index i swaps with nextInt(i), from the end);
//   - water boxes set water below sea level and air above without looking
//     at the block already there;
//   - the double Y room's floor takes its opening from the south side.
//
// No guardians are spawned.
// VERIFY: blocks against a BDS monument (bdsdiff).

// monument piece kinds.
const (
	monBuilding = iota
	monEntry
	monCore
	monSimple
	monSimpleTop
	monDoubleX
	monDoubleY
	monDoubleZ
	monDoubleXY
	monDoubleYZ
	monWing
	monPenthouse
)

// Room directions (Bedrock's 3D data values) and their opposites.
const (
	dDown = iota
	dUp
	dNorth
	dSouth
	dWest
	dEast
)

var monOpposite = [6]int{dUp, dDown, dSouth, dNorth, dEast, dWest}

// monRoom is a room definition of the monument's grid.
type monRoom struct {
	index   int
	conn    [6]*monRoom
	open    uint8
	claimed bool
	source  bool
	scan    int
}

func (r *monRoom) has(d int) bool { return r.open>>d&1 != 0 }

func (r *monRoom) setConnection(d int, o *monRoom) {
	r.conn[d] = o
	o.conn[monOpposite[d]] = r
}

func (r *monRoom) updateOpenings() {
	r.open = 0
	for d, c := range r.conn {
		if c != nil {
			r.open |= 1 << d
		}
	}
}

func (r *monRoom) findSource(scan int) bool {
	if r.source {
		return true
	}
	r.scan = scan
	for d, c := range r.conn {
		if c != nil && r.has(d) && c.scan != scan && c.findSource(scan) {
			return true
		}
	}
	return false
}

// monPiece is a piece of a laid-out monument.
type monPiece struct {
	kind   int
	bb     box
	room   *monRoom
	design int32
}

// monument is a laid-out monument.
type monument struct {
	orient int32
	pieces []monPiece // the building first, then its children in order
}

// monumentStartSeed is the seed of the start's random (sub_4E5EC70).
func (o *Overworld) monumentStartSeed(cx, cz int32) uint32 {
	var r rand.MT
	r.Seed(uint32(o.seed))
	f1 := r.Float()
	f2 := r.Float()
	return uint32(o.seed) ^ uint32(int64(f2*float32(cz))) ^ uint32(int64(f1*float32(cx)))
}

// monumentAt lays out the monument starting in chunk (cx, cz), nil if none
// does. It is computed once per start.
func (o *Overworld) monumentAt(cx, cz int32) *monument {
	k := [2]int32{cx, cz}
	if v, ok := o.monuments.Load(k); ok {
		m, _ := v.(*monument)
		return m
	}
	var m *monument
	if o.isMonumentChunk(cx, cz) {
		m = o.layMonument(cx, cz)
	}
	o.monuments.Store(k, m)
	return m
}

func (o *Overworld) layMonument(cx, cz int32) *monument {
	seed := o.monumentStartSeed(cx, cz)
	var r rand.MT
	r.Seed(seed)
	m := &monument{orient: r.IntN(4) + 2}
	x, z := cx*16-21, cz*16-21
	building := monPiece{kind: monBuilding, bb: box{x, SeaLevel - 24, z, x + 57, SeaLevel - 2, z + 57}}

	// The room grid: two 5x4 floors and a 3x2 top floor.
	var rooms [75]*monRoom
	add := func(i int) { rooms[i] = &monRoom{index: i} }
	for i := 0; i < 5; i++ {
		for zz := 0; zz < 4; zz++ {
			add(i + 5*zz)
		}
	}
	for i := 0; i < 5; i++ {
		for zz := 0; zz < 4; zz++ {
			add(25 + i + 5*zz)
		}
	}
	for i := 1; i < 4; i++ {
		for zz := 0; zz < 2; zz++ {
			add(50 + i + 5*zz)
		}
	}
	// BDS swaps the direction of a link along z, so north is z+1 in the
	// grid (matching the rooms' boxes, which grow towards z+1).
	step := [6][3]int{{0, -1, 0}, {0, 1, 0}, {0, 0, 1}, {0, 0, -1}, {-1, 0, 0}, {1, 0, 0}}
	for xi := 0; xi < 5; xi++ {
		for zi := 0; zi < 5; zi++ {
			for yi := 0; yi < 3; yi++ {
				rm := rooms[xi+5*zi+25*yi]
				if rm == nil {
					continue
				}
				for _, d := range [6]int{1, 0, 2, 3, 4, 5} {
					nx, ny, nz := xi+step[d][0], yi+step[d][1], zi+step[d][2]
					if nx < 0 || nx >= 5 || nz < 0 || nz >= 5 || ny < 0 || ny >= 3 {
						continue
					}
					if nb := rooms[nx+5*nz+25*ny]; nb != nil {
						rm.setConnection(d, nb)
					}
				}
			}
		}
	}
	top, left, right := &monRoom{index: 1003}, &monRoom{index: 1001}, &monRoom{index: 1002}
	rooms[52].setConnection(dUp, top)
	rooms[25].setConnection(dSouth, left)
	rooms[29].setConnection(dSouth, right)
	top.claimed, left.claimed, right.claimed = true, true, true
	source := rooms[2]
	source.source = true
	core := rooms[10+r.IntN(4)]
	core.claimed = true
	e, n := core.conn[dEast], core.conn[dNorth]
	e.claimed, n.claimed = true, true
	e.conn[dNorth].claimed = true
	core.conn[dUp].claimed = true
	e.conn[dUp].claimed = true
	n.conn[dUp].claimed = true
	e.conn[dNorth].conn[dUp].claimed = true

	var list []*monRoom
	for _, rm := range rooms {
		if rm != nil {
			rm.updateOpenings()
			list = append(list, rm)
		}
	}
	top.updateOpenings()
	var sr rand.MT
	sr.Seed(seed)
	for i := len(list) - 1; i >= 1; i-- {
		j := sr.IntN(int32(i))
		list[i], list[j] = list[j], list[i]
	}
	scan := 1
	for _, rm := range list {
		removed := 0
		for try := 0; try < 5 && removed < 2; try++ {
			k := int(r.IntN(6))
			if !rm.has(k) {
				continue
			}
			opp := monOpposite[k]
			other := rm.conn[k]
			rm.open &^= 1 << k
			other.open &^= 1 << opp
			a := scan
			scan++
			if rm.findSource(a) {
				b := scan
				scan++
				if other.findSource(b) {
					removed++
					continue
				}
			}
			rm.open |= 1 << k
			other.open |= 1 << opp
		}
	}
	list = append(list, top, left, right)
	source.claimed = true

	// Children, in BDS's order: entry, core, the fitted rooms, then the
	// wings and the penthouse (added after the rooms are moved).
	var children []monPiece
	room := func(kind int, rm *monRoom, sx, sy, sz int32) monPiece {
		return monPiece{kind: kind, bb: m.roomBox(rm, sx, sy, sz), room: rm}
	}
	children = append(children, room(monEntry, source, 1, 1, 1), room(monCore, core, 2, 2, 2))
	for _, rm := range list {
		if rm.claimed || rm.index > 74 {
			continue
		}
		var p monPiece
		switch {
		case rm.has(dEast) && !rm.conn[dEast].claimed && rm.has(dUp) && !rm.conn[dUp].claimed &&
			rm.conn[dEast].has(dUp) && !rm.conn[dEast].conn[dUp].claimed:
			rm.claimed = true
			rm.conn[dEast].claimed = true
			rm.conn[dUp].claimed = true
			rm.conn[dEast].conn[dUp].claimed = true
			p = room(monDoubleXY, rm, 2, 2, 1)
		case rm.has(dNorth) && !rm.conn[dNorth].claimed && rm.has(dUp) && !rm.conn[dUp].claimed &&
			rm.conn[dNorth].has(dUp) && !rm.conn[dNorth].conn[dUp].claimed:
			rm.claimed = true
			rm.conn[dNorth].claimed = true
			rm.conn[dUp].claimed = true
			rm.conn[dNorth].conn[dUp].claimed = true
			p = room(monDoubleYZ, rm, 1, 2, 2)
		case rm.has(dNorth) && !rm.conn[dNorth].claimed:
			rm.claimed = true
			rm.conn[dNorth].claimed = true
			p = room(monDoubleZ, rm, 1, 1, 2)
		case rm.has(dEast) && !rm.conn[dEast].claimed:
			rm.claimed = true
			rm.conn[dEast].claimed = true
			p = room(monDoubleX, rm, 2, 1, 1)
		case rm.has(dUp) && !rm.conn[dUp].claimed:
			rm.claimed = true
			rm.conn[dUp].claimed = true
			p = room(monDoubleY, rm, 1, 2, 1)
		case rm.open&0x3E == 0:
			rm.claimed = true
			p = room(monSimpleTop, rm, 1, 1, 1)
		default:
			rm.claimed = true
			p = room(monSimple, rm, 1, 1, 1)
			p.design = r.IntN(3)
		}
		children = append(children, p)
	}
	b := &monBuild{bb: building.bb, orient: m.orient}
	dx, dz := b.wx(9, 22), b.wz(9, 22)
	for i := range children {
		c := &children[i].bb
		c.x0, c.x1 = c.x0+dx, c.x1+dx
		c.y0, c.y1 = c.y0+building.bb.y0, c.y1+building.bb.y0
		c.z0, c.z1 = c.z0+dz, c.z1+dz
	}
	corners := func(ax, ay, az, bx, by, bz int32) box {
		x0, z0, x1, z1 := b.wx(ax, az), b.wz(ax, az), b.wx(bx, bz), b.wz(bx, bz)
		return box{min(x0, x1), b.wy(ay), min(z0, z1), max(x0, x1), b.wy(by), max(z0, z1)}
	}
	design := int32(r.Uint32()>>1) & 1
	children = append(children,
		monPiece{kind: monWing, bb: corners(1, 1, 1, 23, 8, 21), design: design},
		monPiece{kind: monWing, bb: corners(34, 1, 1, 56, 8, 21), design: design ^ 1},
		monPiece{kind: monPenthouse, bb: corners(22, 13, 22, 35, 17, 35)})
	m.pieces = append([]monPiece{building}, children...)
	return m
}

// roomBox is a room piece's box from its grid index (sub_4E5F3D0), before
// the building moves it.
func (m *monument) roomBox(rm *monRoom, sx, sy, sz int32) box {
	i := int32(rm.index)
	gx, gz, gy := i%5, i/5%5, i/25
	w, d := sx, sz
	if m.orient != 2 && m.orient != 3 {
		w, d = sz, sx
	}
	var x, z int32
	switch m.orient {
	case 4:
		x, z = 1-8*(sz+gz), 8*gx
	case 3:
		x, z = 8*gx, 8*gz
	case 2:
		x, z = 8*gx, 1-8*(sz+gz)
	default:
		x, z = 8*gz, 8*gx
	}
	return box{x, 4 * gy, z, x + 8*w - 1, 4*gy + 4*sy - 1, z + 8*d - 1}
}

// monumentsReach calls f for each monument with its building in chunk
// (cx, cz). A building spans chunks sx-2..sx+2 of its start.
func (o *Overworld) monumentsReach(cx, cz int32, f func(m *monument)) {
	chunk := box{cx * 16, MinY, cz * 16, cx*16 + 15, MaxY - 1, cz*16 + 15}
	for rx := floorDivRegion(cx-2, monumentSpacing); rx <= floorDivRegion(cx+2, monumentSpacing); rx++ {
		for rz := floorDivRegion(cz-2, monumentSpacing); rz <= floorDivRegion(cz+2, monumentSpacing); rz++ {
			sx, sz := o.monumentPick(rx, rz)
			if sx < cx-2 || sx > cx+2 || sz < cz-2 || sz > cz+2 {
				continue
			}
			if m := o.monumentAt(sx, sz); m != nil && m.pieces[0].bb.intersects(chunk) {
				f(m)
			}
		}
	}
}

// monumentReaches reports whether a monument has blocks in chunk (cx, cz).
func (o *Overworld) monumentReaches(cx, cz int32) bool {
	found := false
	o.monumentsReach(cx, cz, func(*monument) { found = true })
	return found
}

// placeMonuments writes the monuments that reach chunk (cx, cz) into c.
func (o *Overworld) placeMonuments(cx, cz int32, c *Chunk) {
	o.monumentsReach(cx, cz, func(m *monument) {
		b := &monBuild{scBuild: scBuild{o: o, c: c, cbox: box{cx * 16, MinY, cz * 16, cx*16 + 15, MaxY - 1, cz*16 + 15}},
			orient: m.orient}
		b.ids()
		b.gray, _ = o.reg.Default("minecraft:prismarine")
		b.light, _ = o.reg.Default("minecraft:prismarine_bricks")
		b.black, _ = o.reg.Default("minecraft:dark_prismarine")
		b.lamp, _ = o.reg.Default("minecraft:sea_lantern")
		// The pieces draw from the chunk's random.
		b.r.Seed(populationSeed(o.seed, cx, cz))
		b.bb = m.pieces[0].bb
		b.building()
		for _, p := range m.pieces[1:] {
			if !p.bb.intersects(b.cbox) {
				continue
			}
			b.bb = p.bb
			switch p.kind {
			case monEntry:
				b.entry(p.room)
			case monCore:
				b.core()
			case monSimple:
				b.simple(p.room, p.design)
			case monSimpleTop:
				b.simpleTop(p.room)
			case monDoubleX:
				b.doubleX(p.room)
			case monDoubleY:
				b.doubleY(p.room)
			case monDoubleZ:
				b.doubleZ(p.room)
			case monDoubleXY:
				b.doubleXY(p.room)
			case monDoubleYZ:
				b.doubleYZ(p.room)
			case monWing:
				b.wing(p.design)
			case monPenthouse:
				b.penthouse()
			}
		}
	})
}

// monBuild builds monument pieces into one chunk, turning piece positions
// by the monument's orientation (OceanMonumentPiece getWorldX/Z, sub_4E5F600
// and sub_4E5F650).
type monBuild struct {
	scBuild
	bb                       box
	orient                   int32
	gray, light, black, lamp uint32
}

func (b *monBuild) wx(x, z int32) int32 {
	switch b.orient {
	case 2, 3:
		return b.bb.x0 + x
	case 4:
		return b.bb.x1 - z
	case 5:
		return b.bb.x0 + z
	}
	return x
}

func (b *monBuild) wz(x, z int32) int32 {
	switch b.orient {
	case 4, 5:
		return b.bb.z0 + x
	case 3:
		return b.bb.z0 + z
	case 2:
		return b.bb.z1 - z
	}
	return z
}

func (b *monBuild) wy(y int32) int32 { return b.bb.y0 + y }

// at is the block at a piece position, air outside the chunk.
func (b *monBuild) at(x, y, z int32) uint32 {
	p := [3]int32{b.wx(x, z), b.wy(y), b.wz(x, z)}
	if !b.cbox.contains(p) {
		return b.air
	}
	return b.c.Block(int(p[0]&15), int(p[1]), int(p[2]&15))
}

// set is placeBlock (sub_D2604A0).
func (b *monBuild) set(s uint32, x, y, z int32) {
	wx, wy, wz := b.wx(x, z), b.wy(y), b.wz(x, z)
	if b.setWorld(wx, wy, wz, s) {
		b.dropEntity([3]int32{wx, wy, wz})
	}
}

// box is generateBox (sub_D260890) with one block for edge and inside.
func (b *monBuild) box(x0, y0, z0, x1, y1, z1 int32, s uint32) {
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			for z := z0; z <= z1; z++ {
				b.set(s, x, y, z)
			}
		}
	}
}

// wa sets water below sea level and air above.
func (b *monBuild) wa(x, y, z int32) {
	if b.wy(y) < SeaLevel {
		b.set(b.o.ids.water, x, y, z)
	} else {
		b.set(b.air, x, y, z)
	}
}

// waBox is wa over a box, y outermost.
func (b *monBuild) waBox(x0, y0, z0, x1, y1, z1 int32) {
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			for z := z0; z <= z1; z++ {
				b.wa(x, y, z)
			}
		}
	}
}

// ifw places s where the block is water (generateBoxOnFillOnly).
func (b *monBuild) ifw(s uint32, x0, y, z0, x1, z1 int32) {
	for x := x0; x <= x1; x++ {
		for z := z0; z <= z1; z++ {
			if b.o.isWater(b.at(x, y, z)) {
				b.set(s, x, y, z)
			}
		}
	}
}

// hits reports whether the piece area between two corners reaches the
// chunk (chunkIntersects).
func (b *monBuild) hits(x0, z0, x1, z1 int32) bool {
	ax, az, bx, bz := b.wx(x0, z0), b.wz(x0, z0), b.wx(x1, z1), b.wz(x1, z1)
	return b.cbox.x1 >= min(ax, bx) && b.cbox.x0 <= max(ax, bx) && b.cbox.z1 >= min(az, bz) && b.cbox.z0 <= max(az, bz)
}

// floor is generateDefaultFloor (sub_4E5F680).
func (b *monBuild) floor(x, z int32, open bool) {
	if !open {
		b.box(x, 0, z, x+7, 0, z+7, b.gray)
		return
	}
	b.box(x, 0, z, x+2, 0, z+7, b.gray)
	b.box(x+5, 0, z, x+7, 0, z+7, b.gray)
	b.box(x+3, 0, z, x+4, 0, z+2, b.gray)
	b.box(x+3, 0, z+5, x+4, 0, z+7, b.gray)
	b.box(x+3, 0, z+2, x+4, 0, z+2, b.light)
	b.box(x+3, 0, z+5, x+4, 0, z+5, b.light)
	b.box(x+2, 0, z+3, x+2, 0, z+4, b.light)
	b.box(x+5, 0, z+3, x+5, 0, z+4, b.light)
}

// building is MonumentBuilding's own blocks (sub_4E63B10).
func (b *monBuild) building() {
	b.genWing(false, 0)
	b.genWing(true, 33)
	if b.hits(22, 5, 35, 17) {
		b.waBox(25, 0, 0, 32, 8, 20)
		for z := int32(5); z != 21; z += 4 {
			b.box(24, 2, z, 24, 4, z, b.light)
			b.box(22, 4, z, 23, 4, z, b.light)
			b.set(b.light, 25, 5, z)
			b.set(b.light, 26, 6, z)
			b.set(b.lamp, 26, 5, z)
			b.box(33, 2, z, 33, 4, z, b.light)
			b.box(34, 4, z, 35, 4, z, b.light)
			b.set(b.light, 32, 5, z)
			b.set(b.light, 31, 6, z)
			b.set(b.lamp, 31, 5, z)
			b.box(27, 6, z, 30, 6, z, b.gray)
		}
	}
	if b.hits(15, 20, 42, 21) {
		b.box(15, 0, 21, 42, 0, 21, b.gray)
		b.waBox(26, 1, 21, 31, 3, 21)
		b.box(21, 12, 21, 36, 12, 21, b.gray)
		b.box(17, 11, 21, 40, 11, 21, b.gray)
		b.box(16, 10, 21, 41, 10, 21, b.gray)
		b.box(15, 7, 21, 42, 9, 21, b.gray)
		b.box(16, 6, 21, 41, 6, 21, b.gray)
		b.box(17, 5, 21, 40, 5, 21, b.gray)
		b.box(21, 4, 21, 36, 4, 21, b.gray)
		b.box(22, 3, 21, 26, 3, 21, b.gray)
		b.box(31, 3, 21, 35, 3, 21, b.gray)
		b.box(23, 2, 21, 25, 2, 21, b.gray)
		b.box(32, 2, 21, 34, 2, 21, b.gray)
		b.box(28, 4, 20, 29, 4, 21, b.light)
		for _, p := range [...][2]int32{{27, 3}, {30, 3}, {26, 2}, {31, 2}, {25, 1}, {32, 1}} {
			b.set(b.light, p[0], p[1], 21)
		}
		for _, p := range [...][2]int32{{28, 6}, {29, 6}, {27, 7}, {30, 7}, {26, 8}, {31, 8}, {25, 9}, {32, 9},
			{24, 10}, {33, 10}, {23, 11}, {34, 11}, {22, 12}, {35, 12}, {28, 9}, {29, 9}, {27, 10}, {30, 10},
			{26, 11}, {31, 11}, {25, 12}, {32, 12}, {28, 12}, {29, 12}, {22, 8}, {22, 9}, {35, 8}, {35, 9},
			{20, 8}, {20, 9}, {37, 8}, {37, 9}, {18, 8}, {18, 9}, {39, 8}, {39, 9}} {
			b.set(b.black, p[0], p[1], 21)
		}
		b.waBox(15, 13, 21, 42, 15, 21)
		b.waBox(15, 1, 21, 15, 6, 21)
		b.waBox(16, 1, 21, 16, 5, 21)
		b.waBox(17, 1, 21, 20, 4, 21)
		b.waBox(21, 1, 21, 21, 3, 21)
		b.waBox(22, 1, 21, 22, 2, 21)
		b.wa(23, 1, 21)
		b.wa(24, 1, 21)
		b.waBox(42, 1, 21, 42, 6, 21)
		b.waBox(41, 1, 21, 41, 5, 21)
		b.waBox(37, 1, 21, 40, 4, 21)
		b.waBox(36, 1, 21, 36, 3, 21)
		b.wa(33, 1, 21)
		b.wa(34, 1, 21)
		b.wa(35, 1, 21)
		b.wa(35, 2, 21)
	}
	if b.hits(21, 21, 36, 36) {
		b.box(21, 0, 22, 36, 0, 36, b.gray)
		b.waBox(21, 1, 22, 36, 23, 36)
		for i := int32(0); i < 4; i++ {
			l, h := 21+i, 36-i
			b.box(l, 13+i, l, h, 13+i, l, b.light)
			b.box(l, 13+i, h, h, 13+i, h, b.light)
			b.box(l, 13+i, l+1, l, 13+i, h-1, b.light)
			b.box(h, 13+i, l+1, h, 13+i, h-1, b.light)
		}
		b.box(25, 16, 25, 32, 16, 32, b.gray)
		b.box(25, 17, 25, 25, 19, 25, b.light)
		b.box(32, 17, 25, 32, 19, 25, b.light)
		b.box(25, 17, 32, 25, 19, 32, b.light)
		b.box(32, 17, 32, 32, 19, 32, b.light)
		b.set(b.light, 26, 20, 26)
		b.set(b.light, 27, 21, 27)
		b.set(b.lamp, 27, 20, 27)
		b.set(b.light, 26, 20, 31)
		b.set(b.light, 27, 21, 30)
		b.set(b.lamp, 27, 20, 30)
		b.set(b.light, 31, 20, 31)
		b.set(b.light, 30, 21, 30)
		b.set(b.lamp, 30, 20, 30)
		b.set(b.light, 31, 20, 26)
		b.set(b.light, 30, 21, 27)
		b.set(b.lamp, 30, 20, 27)
		b.box(28, 21, 27, 29, 21, 27, b.gray)
		b.box(27, 21, 28, 27, 21, 29, b.gray)
		b.box(28, 21, 30, 29, 21, 30, b.gray)
		b.box(30, 21, 28, 30, 21, 29, b.gray)
	}
	if b.hits(0, 21, 6, 58) {
		b.box(0, 0, 21, 6, 0, 57, b.gray)
		b.waBox(0, 1, 21, 6, 7, 57)
		b.box(4, 4, 21, 6, 4, 53, b.gray)
		b.sideSteps(0, 1)
		for _, z := range [...]int32{23, 26, 29, 32, 35, 38, 41, 44, 47, 50, 52} {
			b.set(b.light, 5, 5, z)
		}
		b.sideSteps(0, 1)
		b.box(4, 1, 52, 6, 3, 52, b.gray)
		b.box(5, 1, 51, 5, 3, 53, b.gray)
	}
	if b.hits(51, 21, 58, 58) {
		b.box(51, 0, 21, 57, 0, 57, b.gray)
		b.waBox(51, 1, 21, 57, 7, 57)
		b.box(51, 4, 21, 53, 4, 53, b.gray)
		b.sideSteps(57, -1)
		for _, z := range [...]int32{23, 26, 29, 32, 35, 38, 41, 44, 47, 50, 52} {
			b.set(b.light, 52, 5, z)
		}
		b.box(51, 1, 52, 53, 3, 52, b.gray)
		b.box(52, 1, 51, 52, 3, 53, b.gray)
	}
	if b.hits(0, 51, 57, 57) {
		b.box(7, 0, 51, 50, 0, 57, b.gray)
		b.waBox(7, 1, 51, 50, 10, 57)
		for i := int32(0); i < 4; i++ {
			b.box(1+i, 1+i, 57-i, 56-i, 1+i, 57-i, b.light)
		}
	}
	if b.hits(7, 21, 13, 54) {
		b.box(7, 0, 21, 13, 0, 50, b.gray)
		b.waBox(7, 1, 21, 13, 10, 50)
		b.box(11, 8, 21, 13, 8, 53, b.gray)
		for i := int32(0); i < 4; i++ {
			b.box(7+i, 5+i, 21, 7+i, 5+i, 54, b.light)
		}
		for z := int32(21); z <= 45; z += 3 {
			b.set(b.light, 12, 9, z)
		}
	}
	if b.hits(44, 21, 50, 54) {
		b.box(44, 0, 21, 50, 0, 50, b.gray)
		b.waBox(44, 1, 21, 50, 10, 50)
		b.box(44, 8, 21, 46, 8, 53, b.gray)
		for i := int32(0); i < 4; i++ {
			b.box(50-i, 5+i, 21, 50-i, 5+i, 54, b.light)
		}
		for z := int32(21); z <= 45; z += 3 {
			b.set(b.light, 45, 9, z)
		}
	}
	if b.hits(8, 44, 49, 54) {
		b.box(14, 0, 44, 43, 0, 50, b.gray)
		b.waBox(14, 1, 44, 43, 10, 50)
		for x := int32(12); x <= 45; x += 3 {
			b.set(b.light, x, 9, 45)
			b.set(b.light, x, 9, 52)
			switch x {
			case 12, 18, 24, 33, 39, 45:
				b.set(b.light, x, 9, 47)
				b.set(b.light, x, 9, 50)
				b.set(b.light, x, 10, 45)
				b.set(b.light, x, 10, 46)
				b.set(b.light, x, 10, 51)
				b.set(b.light, x, 10, 52)
				b.set(b.light, x, 11, 47)
				b.set(b.light, x, 11, 50)
				b.set(b.light, x, 12, 48)
				b.set(b.light, x, 12, 49)
			}
		}
		b.box(8, 5, 54, 49, 5, 54, b.gray)
		b.box(9, 6, 54, 48, 6, 54, b.gray)
		b.box(10, 7, 54, 47, 7, 54, b.gray)
		b.box(11, 8, 54, 46, 8, 54, b.light)
		b.box(14, 8, 44, 43, 8, 53, b.gray)
	}
	if b.hits(14, 21, 20, 43) {
		b.box(14, 0, 21, 20, 0, 43, b.gray)
		b.waBox(14, 1, 22, 20, 14, 43)
		b.box(18, 12, 22, 20, 12, 39, b.gray)
		b.box(18, 12, 21, 20, 12, 21, b.light)
		for i := int32(0); i < 4; i++ {
			b.box(14+i, 9+i, 21, 14+i, 9+i, 43-i, b.light)
		}
		for z := int32(23); z <= 38; z += 3 {
			b.set(b.light, 19, 13, z)
		}
	}
	if b.hits(37, 21, 43, 43) {
		b.box(37, 0, 21, 43, 0, 43, b.gray)
		b.waBox(37, 1, 22, 43, 14, 43)
		b.box(37, 12, 22, 39, 12, 39, b.gray)
		b.box(37, 12, 21, 39, 12, 21, b.light)
		for i := int32(0); i < 4; i++ {
			b.box(43-i, 9+i, 21, 43-i, 9+i, 43-i, b.light)
		}
		for z := int32(23); z <= 38; z += 3 {
			b.set(b.light, 38, 13, z)
		}
	}
	if b.hits(15, 37, 42, 43) {
		b.box(21, 0, 37, 36, 0, 43, b.gray)
		b.waBox(21, 1, 37, 36, 14, 43)
		b.box(21, 12, 37, 36, 12, 39, b.gray)
		for i := int32(0); i < 4; i++ {
			b.box(15+i, 9+i, 43-i, 42-i, 9+i, 43-i, b.light)
		}
		for x := int32(21); x <= 36; x += 3 {
			b.set(b.light, x, 13, 38)
		}
	}
	// Pillars under the building, each column filled down to the floor.
	for j := int32(0); j < 7; j++ {
		x := 9 * j
		k := int32(0)
		for {
			if k == 0 && j == 3 {
				k = 6
			}
			l := 9 * k
			for i := int32(0); i < 4; i++ {
				for dz := int32(0); dz < 4; dz++ {
					b.set(b.light, x+i, 0, l+dz)
					b.down(b.light, x+i, -1, l+dz)
				}
			}
			if j != 0 && j != 6 {
				if k > 0 {
					break
				}
				k += 6
			} else {
				if k >= 6 {
					break
				}
				k++
			}
		}
	}
	// The water ring around the building.
	for j := int32(0); j < 5; j++ {
		b.waBox(-1-j, 2*j, -1-j, -1-j, 23, 58+j)
		b.waBox(58+j, 2*j, -1-j, 58+j, 23, 58+j)
		b.waBox(-j, 2*j, -1-j, 57+j, 23, -1-j)
		b.waBox(-j, 2*j, 58+j, 57+j, 23, 58+j)
	}
}

// sideSteps is the stepped edge of a side wing: light bricks rising one
// block per step inward from x0 (dir 1 east, -1 west).
func (b *monBuild) sideSteps(x0, dir int32) {
	for i := int32(0); i < 4; i++ {
		x := x0 + dir*i
		b.box(x, 1+i, 21, x, 1+i, 57-i, b.light)
	}
}

// down is fillColumnDown (sub_D255920) at a piece position.
func (b *monBuild) down(s uint32, x, y, z int32) {
	b.fillDownWorld(s, b.wx(x, z), b.wy(y), b.wz(x, z))
}

// genWing is generateWing (sub_4E6B560).
func (b *monBuild) genWing(flip bool, xo int32) {
	if !b.hits(xo, 0, xo+23, 20) {
		return
	}
	b.box(xo, 0, 0, xo+24, 0, 20, b.gray)
	b.waBox(xo, 1, 0, xo+24, 10, 20)
	for i := int32(0); i < 4; i++ {
		v := xo + 16 - i
		b.box(xo+i, i+1, i, xo+i, i+1, 20, b.light)
		b.box(xo+i+7, i+5, i+7, xo+i+7, i+5, 20, b.light)
		b.box(v+1, i+5, i+7, v+1, i+5, 20, b.light)
		b.box(v+8, i+1, i, v+8, i+1, 20, b.light)
		b.box(xo+i+1, i+1, i, v+7, i+1, i, b.light)
		b.box(xo+i+8, i+5, i+7, v, i+5, i+7, b.light)
	}
	b.box(xo+4, 4, 4, xo+6, 4, 20, b.gray)
	b.box(xo+7, 4, 4, xo+17, 4, 6, b.gray)
	b.box(xo+18, 4, 4, xo+20, 4, 20, b.gray)
	b.box(xo+11, 8, 11, xo+13, 8, 20, b.gray)
	b.set(b.light, xo+12, 9, 12)
	b.set(b.light, xo+12, 9, 15)
	b.set(b.light, xo+12, 9, 18)
	a, c := xo+5, xo+19
	if flip {
		a, c = xo+19, xo+5
	}
	for _, z := range [...]int32{20, 17, 14, 11, 8, 5} {
		b.set(b.light, a, 5, z)
	}
	for _, z := range [...]int32{19, 16, 13, 10, 7} {
		b.set(b.light, c, 5, z)
	}
	if flip {
		b.set(b.light, xo+7, 5, 5)
		b.set(b.light, xo+10, 5, 5)
		b.set(b.light, xo+13, 5, 5)
		b.set(b.light, xo+16, 5, 5)
	} else {
		b.set(b.light, xo+17, 5, 5)
		b.set(b.light, xo+14, 5, 5)
		b.set(b.light, xo+11, 5, 5)
		b.set(b.light, xo+8, 5, 5)
	}
	b.set(b.light, c, 5, 5)
	b.box(xo+11, 1, 12, xo+13, 7, 12, b.gray)
	b.box(xo+12, 1, 11, xo+12, 7, 13, b.gray)
}

// entry is OceanMonumentEntryRoom (sub_4E6C260).
func (b *monBuild) entry(rm *monRoom) {
	b.box(0, 3, 0, 2, 3, 7, b.light)
	b.box(5, 3, 0, 7, 3, 7, b.light)
	b.box(0, 2, 0, 1, 2, 7, b.light)
	b.box(6, 2, 0, 7, 2, 7, b.light)
	b.box(0, 1, 0, 0, 1, 7, b.light)
	b.box(7, 1, 0, 7, 1, 7, b.light)
	b.box(0, 1, 7, 7, 3, 7, b.light)
	b.box(1, 1, 0, 2, 3, 0, b.light)
	b.box(5, 1, 0, 6, 3, 0, b.light)
	if rm.has(dNorth) {
		b.waBox(3, 1, 7, 4, 2, 7)
	}
	if rm.has(dWest) {
		b.waBox(0, 1, 3, 1, 2, 4)
	}
	if rm.has(dEast) {
		b.waBox(6, 1, 3, 7, 2, 4)
	}
}

// core is OceanMonumentCoreRoom (sub_4E79C40).
func (b *monBuild) core() {
	b.ifw(b.gray, 1, 8, 0, 14, 14)
	b.box(0, 7, 0, 0, 7, 15, b.light)
	b.box(15, 7, 0, 15, 7, 15, b.light)
	b.box(1, 7, 0, 15, 7, 0, b.light)
	b.box(1, 7, 15, 14, 7, 15, b.light)
	for y := int32(1); y < 7; y++ {
		s := b.light
		if y&3 == 2 {
			s = b.gray
		}
		b.box(0, y, 0, 0, y, 1, s)
		b.box(0, y, 6, 0, y, 9, s)
		b.box(0, y, 14, 0, y, 15, s)
		b.box(15, y, 0, 15, y, 1, s)
		b.box(15, y, 6, 15, y, 9, s)
		b.box(15, y, 14, 15, y, 15, s)
		b.box(1, y, 0, 1, y, 0, s)
		b.box(6, y, 0, 9, y, 0, s)
		b.box(14, y, 0, 14, y, 0, s)
		b.box(1, y, 15, 14, y, 15, s)
	}
	b.box(6, 3, 6, 9, 6, 9, b.black)
	gold, _ := b.o.reg.Default("minecraft:gold_block")
	b.box(7, 4, 7, 8, 5, 8, gold)
	for _, p := range [...][3]int32{{6, 3, 6}, {6, 3, 9}, {9, 3, 6}, {9, 3, 9}, {6, 6, 6}, {6, 6, 9}, {9, 6, 6}, {9, 6, 9}} {
		b.set(b.lamp, p[0], p[1], p[2])
	}
	for _, q := range [...][6]int32{
		{5, 1, 6, 5, 2, 6}, {5, 1, 9, 5, 2, 9}, {10, 1, 6, 10, 2, 6}, {10, 1, 9, 10, 2, 9},
		{6, 1, 5, 6, 2, 5}, {9, 1, 5, 9, 2, 5}, {6, 1, 10, 6, 2, 10}, {9, 1, 10, 9, 2, 10},
		{5, 2, 5, 5, 6, 5}, {5, 2, 10, 5, 6, 10}, {10, 2, 5, 10, 6, 5}, {10, 2, 10, 10, 6, 10},
		{5, 7, 1, 5, 7, 6}, {10, 7, 1, 10, 7, 6}, {5, 7, 9, 5, 7, 14}, {10, 7, 9, 10, 7, 14},
		{1, 7, 5, 6, 7, 5}, {1, 7, 10, 6, 7, 10}, {9, 7, 5, 14, 7, 5}, {9, 7, 10, 14, 7, 10},
		{2, 1, 2, 2, 1, 3}, {3, 1, 2, 3, 1, 2}, {13, 1, 2, 13, 1, 3}, {12, 1, 2, 12, 1, 2},
		{2, 1, 12, 2, 1, 13}, {3, 1, 13, 3, 1, 13}, {13, 1, 12, 13, 1, 13}, {12, 1, 13, 12, 1, 13},
	} {
		b.box(q[0], q[1], q[2], q[3], q[4], q[5], b.light)
	}
}

// hole is a 2x2 water opening in a room wall from y: across x from x0 on
// the south and north sides, across z from z0 on the west and east sides.
func (b *monBuild) hole(d int, y, x0, z0 int32) {
	switch d {
	case dSouth, dNorth:
		b.waBox(x0, y, z0, x0+1, y+1, z0)
	default:
		b.waBox(x0, y, z0, x0, y+1, z0+1)
	}
}

// simple is OceanMonumentSimpleRoom (sub_4E6CC20).
func (b *monBuild) simple(rm *monRoom, design int32) {
	if rm.index >= 25 {
		b.floor(0, 0, rm.has(dDown))
	}
	if rm.conn[dUp] == nil {
		b.ifw(b.gray, 1, 4, 1, 6, 6)
	}
	pillar := false
	if design != 0 && b.r.Uint32()&0x8000000 != 0 && rm.open&3 == 0 {
		n := 0
		for _, d := range [...]int{dNorth, dSouth, dEast, dWest} {
			if rm.has(d) {
				n++
			}
		}
		pillar = n >= 2
	}
	switch design {
	case 0:
		b.box(0, 1, 0, 2, 1, 2, b.light)
		b.box(0, 3, 0, 2, 3, 2, b.light)
		b.box(0, 2, 0, 0, 2, 2, b.gray)
		b.box(1, 2, 0, 2, 2, 0, b.gray)
		b.set(b.lamp, 1, 2, 1)
		b.box(5, 1, 0, 7, 1, 2, b.light)
		b.box(5, 3, 0, 7, 3, 2, b.light)
		b.box(7, 2, 0, 7, 2, 2, b.gray)
		b.box(5, 2, 0, 6, 2, 0, b.gray)
		b.set(b.lamp, 6, 2, 1)
		b.box(0, 1, 5, 2, 1, 7, b.light)
		b.box(0, 3, 5, 2, 3, 7, b.light)
		b.box(0, 2, 5, 0, 2, 7, b.gray)
		b.box(1, 2, 7, 2, 2, 7, b.gray)
		b.set(b.lamp, 1, 2, 6)
		b.box(5, 1, 5, 7, 1, 7, b.light)
		b.box(5, 3, 5, 7, 3, 7, b.light)
		b.box(7, 2, 5, 7, 2, 7, b.gray)
		b.box(5, 2, 7, 6, 2, 7, b.gray)
		b.set(b.lamp, 6, 2, 6)
		if rm.has(dSouth) {
			b.box(3, 3, 0, 4, 3, 0, b.light)
		} else {
			b.box(3, 3, 0, 4, 3, 1, b.light)
			b.box(3, 2, 0, 4, 2, 0, b.gray)
			b.box(3, 1, 0, 4, 1, 1, b.light)
		}
		if rm.has(dNorth) {
			b.box(3, 3, 7, 4, 3, 7, b.light)
		} else {
			b.box(3, 3, 6, 4, 3, 7, b.light)
			b.box(3, 2, 7, 4, 2, 7, b.gray)
			b.box(3, 1, 6, 4, 1, 7, b.light)
		}
		if rm.has(dWest) {
			b.box(0, 3, 3, 0, 3, 4, b.light)
		} else {
			b.box(0, 3, 3, 1, 3, 4, b.light)
			b.box(0, 2, 3, 0, 2, 4, b.gray)
			b.box(0, 1, 3, 1, 1, 4, b.light)
		}
		if rm.has(dEast) {
			b.box(7, 3, 3, 7, 3, 4, b.light)
		} else {
			b.box(6, 3, 3, 7, 3, 4, b.light)
			b.box(7, 2, 3, 7, 2, 4, b.gray)
			b.box(6, 1, 3, 7, 1, 4, b.light)
		}
	case 1:
		b.box(2, 1, 2, 2, 3, 2, b.light)
		b.box(2, 1, 5, 2, 3, 5, b.light)
		b.box(5, 1, 5, 5, 3, 5, b.light)
		b.box(5, 1, 2, 5, 3, 2, b.light)
		b.set(b.lamp, 2, 2, 2)
		b.set(b.lamp, 2, 2, 5)
		b.set(b.lamp, 5, 2, 5)
		b.set(b.lamp, 5, 2, 2)
		b.box(0, 1, 0, 1, 3, 0, b.light)
		b.box(0, 1, 1, 0, 3, 1, b.light)
		b.box(0, 1, 7, 1, 3, 7, b.light)
		b.box(0, 1, 6, 0, 3, 6, b.light)
		b.box(6, 1, 7, 7, 3, 7, b.light)
		b.box(7, 1, 6, 7, 3, 6, b.light)
		b.box(6, 1, 0, 7, 3, 0, b.light)
		b.box(7, 1, 1, 7, 3, 1, b.light)
		for _, p := range [...][2]int32{{1, 0}, {0, 1}, {1, 7}, {0, 6}, {6, 7}, {7, 6}, {6, 0}, {7, 1}} {
			b.set(b.gray, p[0], 2, p[1])
		}
		if !rm.has(dSouth) {
			b.box(1, 3, 0, 6, 3, 0, b.light)
			b.box(1, 2, 0, 6, 2, 0, b.gray)
			b.box(1, 1, 0, 6, 1, 0, b.light)
		}
		if !rm.has(dNorth) {
			b.box(1, 3, 7, 6, 3, 7, b.light)
			b.box(1, 2, 7, 6, 2, 7, b.gray)
			b.box(1, 1, 7, 6, 1, 7, b.light)
		}
		if !rm.has(dWest) {
			b.box(0, 3, 1, 0, 3, 6, b.light)
			b.box(0, 2, 1, 0, 2, 6, b.gray)
			b.box(0, 1, 1, 0, 1, 6, b.light)
		}
		if !rm.has(dEast) {
			b.box(7, 3, 1, 7, 3, 6, b.light)
			b.box(7, 2, 1, 7, 2, 6, b.gray)
			b.box(7, 1, 1, 7, 1, 6, b.light)
		}
	case 2:
		b.ringRoom()
		if rm.has(dSouth) {
			b.hole(dSouth, 1, 3, 0)
		}
		if rm.has(dNorth) {
			b.hole(dNorth, 1, 3, 7)
		}
		if rm.has(dWest) {
			b.hole(dWest, 1, 0, 3)
		}
		if rm.has(dEast) {
			b.hole(dEast, 1, 7, 3)
		}
	}
	if pillar {
		b.box(3, 1, 3, 4, 1, 4, b.light)
		b.box(3, 2, 3, 4, 2, 4, b.gray)
		b.box(3, 3, 3, 4, 3, 4, b.light)
	}
}

// ringRoom is the dark-striped wall of the simple room's third design and
// the simple top room.
func (b *monBuild) ringRoom() {
	for _, y := range [...]int32{1, 2, 3} {
		s := b.light
		if y == 2 {
			s = b.black
		}
		b.box(0, y, 0, 0, y, 7, s)
		b.box(7, y, 0, 7, y, 7, s)
		b.box(1, y, 0, 6, y, 0, s)
		b.box(1, y, 7, 6, y, 7, s)
	}
	b.box(0, 1, 3, 0, 2, 4, b.black)
	b.box(7, 1, 3, 7, 2, 4, b.black)
	b.box(3, 1, 0, 4, 2, 0, b.black)
	b.box(3, 1, 7, 4, 2, 7, b.black)
}

// simpleTop is OceanMonumentSimpleTopRoom, the sponge room (sub_4E6EC20).
func (b *monBuild) simpleTop(rm *monRoom) {
	if rm.index >= 25 {
		b.floor(0, 0, rm.has(dDown))
	}
	if rm.conn[dUp] == nil {
		b.ifw(b.gray, 1, 4, 1, 6, 6)
	}
	sponge, _ := b.o.reg.Default("minecraft:wet_sponge")
	for x := int32(1); x <= 6; x++ {
		for z := int32(1); z <= 6; z++ {
			if b.r.IntN(3) != 0 {
				y := int32(3)
				if b.r.IntN(4) == 0 {
					y = 2
				}
				b.box(x, y, z, x, 3, z, sponge)
			}
		}
	}
	b.ringRoom()
	if rm.has(dSouth) {
		b.hole(dSouth, 1, 3, 0)
	}
}

// doubleY is OceanMonumentDoubleYRoom (sub_4E6F990).
func (b *monBuild) doubleY(rm *monRoom) {
	if rm.index >= 25 {
		b.floor(0, 0, rm.has(dSouth))
	}
	if up := rm.conn[dUp]; up != nil && up.conn[dUp] == nil {
		b.ifw(b.gray, 1, 8, 1, 6, 6)
	}
	b.box(0, 4, 0, 0, 4, 7, b.light)
	b.box(7, 4, 0, 7, 4, 7, b.light)
	b.box(1, 4, 0, 6, 4, 0, b.light)
	b.box(1, 4, 7, 6, 4, 7, b.light)
	b.box(2, 4, 1, 2, 4, 2, b.light)
	b.box(1, 4, 2, 1, 4, 2, b.light)
	b.box(5, 4, 1, 5, 4, 2, b.light)
	b.box(6, 4, 2, 6, 4, 2, b.light)
	b.box(2, 4, 5, 2, 4, 6, b.light)
	b.box(1, 4, 5, 1, 4, 5, b.light)
	b.box(5, 4, 5, 5, 4, 6, b.light)
	b.box(6, 4, 5, 6, 4, 5, b.light)
	r := rm
	for i := int32(1); i <= 5; i += 4 {
		if r != nil {
			if r.has(dSouth) {
				b.box(2, i, 0, 2, i+2, 0, b.light)
				b.box(5, i, 0, 5, i+2, 0, b.light)
				b.box(3, i+2, 0, 4, i+2, 0, b.light)
			} else {
				b.box(0, i, 0, 7, i+2, 0, b.light)
				b.box(0, i+1, 0, 7, i+1, 0, b.gray)
			}
			if r.has(dNorth) {
				b.box(2, i, 7, 2, i+2, 7, b.light)
				b.box(5, i, 7, 5, i+2, 7, b.light)
				b.box(3, i+2, 7, 4, i+2, 7, b.light)
			} else {
				b.box(0, i, 7, 7, i+2, 7, b.light)
				b.box(0, i+1, 7, 7, i+1, 7, b.gray)
			}
			if r.has(dWest) {
				b.box(0, i, 2, 0, i+2, 2, b.light)
				b.box(0, i, 5, 0, i+2, 5, b.light)
				b.box(0, i+2, 3, 0, i+2, 4, b.light)
			} else {
				b.box(0, i, 0, 0, i+2, 7, b.light)
				b.box(0, i+1, 0, 0, i+1, 7, b.gray)
			}
			if r.has(dEast) {
				b.box(7, i, 2, 7, i+2, 2, b.light)
				b.box(7, i, 5, 7, i+2, 5, b.light)
				b.box(7, i+2, 3, 7, i+2, 4, b.light)
			} else {
				b.box(7, i, 0, 7, i+2, 7, b.light)
				b.box(7, i+1, 0, 7, i+1, 7, b.gray)
			}
		}
		if r != nil {
			r = r.conn[dUp]
		}
	}
}

// doubleX is OceanMonumentDoubleXRoom (sub_4E70800).
func (b *monBuild) doubleX(rm *monRoom) {
	e := rm.conn[dEast]
	if rm.index >= 25 {
		b.floor(8, 0, e.has(dDown))
		b.floor(0, 0, rm.has(dDown))
	}
	if rm.conn[dUp] == nil {
		b.ifw(b.gray, 1, 4, 1, 7, 6)
	}
	if e.conn[dUp] == nil {
		b.ifw(b.gray, 8, 4, 1, 14, 6)
	}
	for _, y := range [...]int32{3, 2, 1} {
		s := b.light
		if y == 2 {
			s = b.gray
		}
		b.box(0, y, 0, 0, y, 7, s)
		b.box(15, y, 0, 15, y, 7, s)
		b.box(1, y, 0, 15, y, 0, s)
		b.box(1, y, 7, 14, y, 7, s)
	}
	b.box(5, 1, 0, 10, 1, 4, b.light)
	b.box(6, 2, 0, 9, 2, 3, b.gray)
	b.box(5, 3, 0, 10, 3, 4, b.light)
	b.set(b.lamp, 6, 2, 3)
	b.set(b.lamp, 9, 2, 3)
	if rm.has(dSouth) {
		b.hole(dSouth, 1, 3, 0)
	}
	if rm.has(dNorth) {
		b.hole(dNorth, 1, 3, 7)
	}
	if rm.has(dWest) {
		b.hole(dWest, 1, 0, 3)
	}
	if e.has(dSouth) {
		b.hole(dSouth, 1, 11, 0)
	}
	if e.has(dNorth) {
		b.hole(dNorth, 1, 11, 7)
	}
	if e.has(dEast) {
		b.hole(dEast, 1, 15, 3)
	}
}

// doubleZ is OceanMonumentDoubleZRoom (sub_4E72500).
func (b *monBuild) doubleZ(rm *monRoom) {
	n := rm.conn[dNorth]
	if rm.index >= 25 {
		b.floor(0, 8, n.has(dDown))
		b.floor(0, 0, rm.has(dDown))
	}
	if rm.conn[dUp] == nil {
		b.ifw(b.gray, 1, 4, 1, 6, 7)
	}
	if n.conn[dUp] == nil {
		b.ifw(b.gray, 1, 4, 8, 6, 14)
	}
	for _, y := range [...]int32{3, 2, 1} {
		s := b.light
		if y == 2 {
			s = b.gray
		}
		b.box(0, y, 0, 0, y, 15, s)
		b.box(7, y, 0, 7, y, 15, s)
		b.box(1, y, 0, 7, y, 0, s)
		b.box(1, y, 15, 6, y, 15, s)
	}
	for _, q := range [...][6]int32{
		{1, 1, 1, 1, 1, 2}, {6, 1, 1, 6, 1, 2}, {1, 3, 1, 1, 3, 2}, {6, 3, 1, 6, 3, 2},
		{1, 1, 13, 1, 1, 14}, {6, 1, 13, 6, 1, 14}, {1, 3, 13, 1, 3, 14}, {6, 3, 13, 6, 3, 14},
		{2, 1, 6, 2, 3, 6}, {5, 1, 6, 5, 3, 6}, {2, 1, 9, 2, 3, 9}, {5, 1, 9, 5, 3, 9},
		{3, 2, 6, 4, 2, 6}, {3, 2, 9, 4, 2, 9}, {2, 2, 7, 2, 2, 8}, {5, 2, 7, 5, 2, 8},
	} {
		b.box(q[0], q[1], q[2], q[3], q[4], q[5], b.light)
	}
	b.set(b.lamp, 2, 2, 5)
	b.set(b.lamp, 5, 2, 5)
	b.set(b.lamp, 2, 2, 10)
	b.set(b.lamp, 5, 2, 10)
	b.set(b.light, 2, 3, 5)
	b.set(b.light, 5, 3, 5)
	b.set(b.light, 2, 3, 10)
	b.set(b.light, 5, 3, 10)
	if rm.has(dSouth) {
		b.hole(dSouth, 1, 3, 0)
	}
	if rm.has(dEast) {
		b.hole(dEast, 1, 7, 3)
	}
	if rm.has(dWest) {
		b.hole(dWest, 1, 0, 3)
	}
	if n.has(dNorth) {
		b.hole(dNorth, 1, 3, 15)
	}
	if n.has(dWest) {
		b.hole(dWest, 1, 0, 11)
	}
	if n.has(dEast) {
		b.hole(dEast, 1, 7, 11)
	}
}

// doubleXY is OceanMonumentDoubleXYRoom (sub_4E74600).
func (b *monBuild) doubleXY(rm *monRoom) {
	e := rm.conn[dEast]
	u, eu := rm.conn[dUp], e.conn[dUp]
	if rm.index >= 25 {
		b.floor(8, 0, e.has(dDown))
		b.floor(0, 0, rm.has(dDown))
	}
	if u.conn[dUp] == nil {
		b.ifw(b.gray, 1, 8, 1, 7, 6)
	}
	if eu.conn[dUp] == nil {
		b.ifw(b.gray, 8, 8, 1, 14, 6)
	}
	for y := int32(1); y <= 7; y++ {
		s := b.light
		if y&3 == 2 {
			s = b.gray
		}
		b.box(0, y, 0, 0, y, 7, s)
		b.box(15, y, 0, 15, y, 7, s)
		b.box(1, y, 0, 15, y, 0, s)
		b.box(1, y, 7, 14, y, 7, s)
	}
	for _, q := range [...][6]int32{
		{2, 1, 3, 2, 7, 4}, {3, 1, 2, 4, 7, 2}, {3, 1, 5, 4, 7, 5}, {13, 1, 3, 13, 7, 4},
		{11, 1, 2, 12, 7, 2}, {11, 1, 5, 12, 7, 5}, {5, 1, 3, 5, 3, 4}, {10, 1, 3, 10, 3, 4},
		{5, 7, 2, 10, 7, 5}, {5, 5, 2, 5, 7, 2}, {10, 5, 2, 10, 7, 2}, {5, 5, 5, 5, 7, 5},
		{10, 5, 5, 10, 7, 5},
	} {
		b.box(q[0], q[1], q[2], q[3], q[4], q[5], b.light)
	}
	b.set(b.light, 6, 6, 2)
	b.set(b.light, 9, 6, 2)
	b.set(b.light, 6, 6, 5)
	b.set(b.light, 9, 6, 5)
	b.box(5, 4, 3, 6, 4, 4, b.light)
	b.box(9, 4, 3, 10, 4, 4, b.light)
	b.set(b.lamp, 5, 4, 2)
	b.set(b.lamp, 5, 4, 5)
	b.set(b.lamp, 10, 4, 2)
	b.set(b.lamp, 10, 4, 5)
	if rm.has(dSouth) {
		b.hole(dSouth, 1, 3, 0)
	}
	if rm.has(dNorth) {
		b.hole(dNorth, 1, 3, 7)
	}
	if rm.has(dWest) {
		b.hole(dWest, 1, 0, 3)
	}
	if e.has(dSouth) {
		b.hole(dSouth, 1, 11, 0)
	}
	if e.has(dNorth) {
		b.hole(dNorth, 1, 11, 7)
	}
	if e.has(dEast) {
		b.hole(dEast, 1, 15, 3)
	}
	if u.has(dSouth) {
		b.hole(dSouth, 5, 3, 0)
	}
	if u.has(dNorth) {
		b.hole(dNorth, 5, 3, 7)
	}
	if u.has(dWest) {
		b.hole(dWest, 5, 0, 3)
	}
	if eu.has(dSouth) {
		b.hole(dSouth, 5, 11, 0)
	}
	if eu.has(dNorth) {
		b.hole(dNorth, 5, 11, 7)
	}
	if eu.has(dEast) {
		b.hole(dEast, 5, 15, 3)
	}
}

// doubleYZ is OceanMonumentDoubleYZRoom (sub_4E77080).
func (b *monBuild) doubleYZ(rm *monRoom) {
	n := rm.conn[dNorth]
	u, nu := rm.conn[dUp], n.conn[dUp]
	if rm.index >= 25 {
		b.floor(0, 8, n.has(dDown))
		b.floor(0, 0, rm.has(dDown))
	}
	if u.conn[dUp] == nil {
		b.ifw(b.gray, 1, 8, 1, 6, 7)
	}
	if nu.conn[dUp] == nil {
		b.ifw(b.gray, 1, 8, 8, 6, 14)
	}
	for y := int32(1); y <= 7; y++ {
		s := b.light
		if y&3 == 2 {
			s = b.gray
		}
		b.box(0, y, 0, 0, y, 15, s)
		b.box(7, y, 0, 7, y, 15, s)
		b.box(1, y, 0, 6, y, 0, s)
		b.box(1, y, 15, 6, y, 15, s)
	}
	for y := int32(1); y <= 7; y++ {
		s := b.black
		if y == 2 || y == 6 {
			s = b.lamp
		}
		b.box(3, y, 7, 4, y, 8, s)
	}
	if rm.has(dSouth) {
		b.hole(dSouth, 1, 3, 0)
	}
	if rm.has(dEast) {
		b.hole(dEast, 1, 7, 3)
	}
	if rm.has(dWest) {
		b.hole(dWest, 1, 0, 3)
	}
	if n.has(dNorth) {
		b.hole(dNorth, 1, 3, 15)
	}
	if n.has(dWest) {
		b.hole(dWest, 1, 0, 11)
	}
	if n.has(dEast) {
		b.hole(dEast, 1, 7, 11)
	}
	if u.has(dSouth) {
		b.hole(dSouth, 5, 3, 0)
	}
	if u.has(dEast) {
		b.hole(dEast, 5, 7, 3)
		b.box(5, 4, 2, 6, 4, 5, b.light)
		b.box(6, 1, 2, 6, 3, 2, b.light)
		b.box(6, 1, 5, 6, 3, 5, b.light)
	}
	if u.has(dWest) {
		b.hole(dWest, 5, 0, 3)
		b.box(1, 4, 2, 2, 4, 5, b.light)
		b.box(1, 1, 2, 1, 3, 2, b.light)
		b.box(1, 1, 5, 1, 3, 5, b.light)
	}
	if nu.has(dNorth) {
		b.hole(dNorth, 5, 3, 15)
	}
	if nu.has(dWest) {
		b.hole(dWest, 5, 0, 11)
		b.box(1, 4, 10, 2, 4, 13, b.light)
		b.box(1, 1, 10, 1, 3, 10, b.light)
		b.box(1, 1, 13, 1, 3, 13, b.light)
	}
	if nu.has(dEast) {
		b.hole(dEast, 5, 7, 11)
		b.box(5, 4, 10, 6, 4, 13, b.light)
		b.box(6, 1, 10, 6, 3, 10, b.light)
		b.box(6, 1, 13, 6, 3, 13, b.light)
	}
}

// wing is OceanMonumentWingRoom (sub_4E7A850).
func (b *monBuild) wing(design int32) {
	if design == 1 {
		b.box(9, 3, 18, 13, 3, 20, b.light)
		b.box(9, 0, 18, 9, 2, 18, b.light)
		b.box(13, 0, 18, 13, 2, 18, b.light)
		for _, x := range [...]int32{9, 13} {
			b.set(b.light, x, 6, 20)
			b.set(b.lamp, x, 5, 20)
			b.set(b.light, x, 4, 20)
		}
		b.box(7, 3, 7, 15, 3, 14, b.light)
		for _, x := range [...]int32{10, 12} {
			b.box(x, 0, 10, x, 6, 10, b.light)
			b.box(x, 0, 12, x, 6, 12, b.light)
			b.set(b.lamp, x, 0, 10)
			b.set(b.lamp, x, 0, 12)
			b.set(b.lamp, x, 4, 10)
			b.set(b.lamp, x, 4, 12)
		}
		b.box(8, 0, 7, 8, 2, 7, b.light)
		b.box(8, 0, 14, 8, 2, 14, b.light)
		b.box(14, 0, 7, 14, 2, 7, b.light)
		b.box(14, 0, 14, 14, 2, 14, b.light)
		b.box(8, 3, 8, 8, 3, 13, b.black)
		b.box(14, 3, 8, 14, 3, 13, b.black)
		return
	}
	if design != 0 {
		return
	}
	for _, q := range [...][6]int32{
		{10, 3, 20, 12, 3, 20}, {9, 2, 19, 13, 2, 20}, {8, 1, 18, 14, 1, 20}, {7, 0, 17, 15, 0, 20},
		{7, 0, 6, 15, 0, 16}, {6, 0, 6, 6, 3, 20}, {16, 0, 6, 16, 3, 20}, {7, 1, 7, 7, 1, 20},
		{15, 1, 7, 15, 1, 20}, {7, 1, 6, 9, 3, 6}, {13, 1, 6, 15, 3, 6}, {8, 1, 7, 9, 1, 7},
		{13, 1, 7, 14, 1, 7}, {9, 0, 5, 13, 0, 5},
	} {
		b.box(q[0], q[1], q[2], q[3], q[4], q[5], b.light)
	}
	b.box(10, 0, 7, 12, 0, 7, b.black)
	b.box(8, 0, 10, 8, 0, 12, b.black)
	b.box(14, 0, 10, 14, 0, 12, b.black)
	for _, z := range [...]int32{18, 15, 12, 9} {
		b.set(b.lamp, 6, 3, z)
		b.set(b.lamp, 16, 3, z)
	}
	b.set(b.lamp, 10, 0, 10)
	b.set(b.lamp, 12, 0, 10)
	b.set(b.lamp, 10, 0, 12)
	b.set(b.lamp, 12, 0, 12)
	b.set(b.lamp, 8, 3, 6)
	b.set(b.lamp, 14, 3, 6)
	for _, p := range [...][2]int32{{4, 4}, {18, 4}, {4, 18}, {18, 18}} {
		b.set(b.light, p[0], 2, p[1])
		b.set(b.lamp, p[0], 1, p[1])
		b.set(b.light, p[0], 0, p[1])
	}
	b.set(b.light, 9, 7, 20)
	b.set(b.light, 13, 7, 20)
	b.box(6, 0, 21, 7, 4, 21, b.light)
	b.box(15, 0, 21, 16, 4, 21, b.light)
}

// penthouse is OceanMonumentPenthouse (sub_4E7B8B0).
func (b *monBuild) penthouse() {
	b.box(2, -1, 2, 11, -1, 11, b.light)
	b.box(0, -1, 0, 1, -1, 11, b.gray)
	b.box(12, -1, 0, 13, -1, 11, b.gray)
	b.box(2, -1, 0, 11, -1, 1, b.gray)
	b.box(2, -1, 12, 11, -1, 13, b.gray)
	b.box(0, 0, 0, 0, 0, 13, b.light)
	b.box(13, 0, 0, 13, 0, 13, b.light)
	b.box(1, 0, 0, 12, 0, 0, b.light)
	b.box(1, 0, 13, 12, 0, 13, b.light)
	for _, v := range [...]int32{2, 5, 8, 11} {
		b.set(b.lamp, 0, 0, v)
		b.set(b.lamp, 13, 0, v)
		b.set(b.lamp, v, 0, 0)
	}
	b.box(2, 0, 3, 4, 0, 9, b.light)
	b.box(9, 0, 3, 11, 0, 9, b.light)
	b.box(4, 0, 9, 9, 0, 11, b.light)
	b.set(b.light, 5, 0, 8)
	b.set(b.light, 8, 0, 8)
	b.set(b.light, 10, 0, 10)
	b.set(b.light, 3, 0, 10)
	b.box(3, 0, 3, 3, 0, 7, b.black)
	b.box(10, 0, 3, 10, 0, 7, b.black)
	b.box(6, 0, 10, 7, 0, 10, b.black)
	for _, q := range [...][2]int32{{3, 2}, {3, 5}, {3, 8}, {10, 2}, {10, 5}, {10, 8}} {
		b.box(q[0], 0, q[1], q[0], 2, q[1], b.light)
	}
	b.box(5, 0, 10, 5, 2, 10, b.light)
	b.box(8, 0, 10, 8, 2, 10, b.light)
	b.box(6, -1, 7, 7, -1, 8, b.black)
	b.waBox(6, -1, 3, 7, -1, 4)
}

// NearestMonument finds the ocean monument nearest to block (x, z),
// searching maxRegions placement regions (32 chunks each) around it, and
// returns its building's corner at its floor. It is safe for concurrent
// use.
func (o *Overworld) NearestMonument(x, z int32, maxRegions int32) (Scattered, bool) {
	type cand struct {
		cx, cz int32
		d      int64
	}
	var cands []cand
	rx0, rz0 := floorDivRegion(x>>4, monumentSpacing), floorDivRegion(z>>4, monumentSpacing)
	for rx := rx0 - maxRegions; rx <= rx0+maxRegions; rx++ {
		for rz := rz0 - maxRegions; rz <= rz0+maxRegions; rz++ {
			cx, cz := o.monumentPick(rx, rz)
			dx, dz := int64(cx*16+8-x), int64(cz*16+8-z)
			cands = append(cands, cand{cx, cz, dx*dx + dz*dz})
		}
	}
	slices.SortFunc(cands, func(a, b cand) int { return cmp.Compare(a.d, b.d) })
	for _, c := range cands {
		if m := o.monumentAt(c.cx, c.cz); m != nil {
			bb := m.pieces[0].bb
			return Scattered{Kind: "ocean_monument", X: (bb.x0 + bb.x1) / 2, Y: bb.y1, Z: (bb.z0 + bb.z1) / 2}, true
		}
	}
	return Scattered{}, false
}
