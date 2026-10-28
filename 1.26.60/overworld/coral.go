package overworld

import (
	"github.com/helix-bedrock/worldgen/1.26.60/rand"
)

// Coral reefs, written from BDS 1.26.52's CoralFeature (place: sub_4993470,
// shape builder sub_4993E00, arm sub_4999B70) with the structure taken from
// the 1.21.3 build, which still has the function names (_buildPlantLike,
// _buildSpire, _buildSmallClump, _buildHand, _buildPlantArm,
// _starFormation, _starCorners, _placeSideDecorations). Both builds draw
// the same random numbers in the same order.

// Facings as BDS numbers them: 2 north (-z), 3 south (+z), 4 west (-x),
// 5 east (+x).
func facingVec() [6][3]int32 {
	return [6][3]int32{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}
}
func facingOpposite() [6]uint8 {
	return [6]uint8{1, 0, 3, 2, 5, 4}
}
func facingClockwise() [4]uint8 {
	return [4]uint8{5, 4, 2, 3}
} // by facing - 2
func facingCounterCW() [4]uint8 {
	return [4]uint8{4, 5, 3, 2}
}
func coralExceptTable() [4][3]uint8 {
	return [4][3]uint8{{5, 3, 4}, {5, 2, 4}, {5, 3, 2}, {2, 3, 4}}
} // by facing - 2
// wallFanDirection is the coral_direction state for a wall fan facing
// away from its block (by facing - 2).
func wallFanDirection() [4]int32 {
	return [4]int32{2, 3, 0, 1}
}

// coralThird picks the plant's third arm direction from the two sides the
// first two did not use, in BDS's table order (byte_148FC70..7A).
func coralThird(a, b uint8, r *rand.MT) uint8 {
	set := uint8(1)<<a | uint8(1)<<b
	var pair [2]uint8
	switch set {
	case 1<<2 | 1<<5:
		pair = [2]uint8{3, 4}
	case 1<<2 | 1<<3:
		pair = [2]uint8{5, 4}
	case 1<<2 | 1<<4:
		pair = [2]uint8{3, 5}
	case 1<<3 | 1<<5:
		pair = [2]uint8{2, 4}
	case 1<<4 | 1<<5:
		pair = [2]uint8{3, 2}
	default: // 3 and 4
		pair = [2]uint8{2, 5}
	}
	return pair[r.IntN(2)]
}

type coralFeature struct {
	// blocks are the coral blocks by colour (tube, brain, bubble, fire,
	// horn), then the dead ones.
	blocks   [10]uint32
	fans     [5][2]uint32 // by colour and coral_fan_direction
	wallFans [5][4]uint32 // by colour and coral_direction
	stone    uint32
}

type coralSide struct {
	pos blockPos
	dir uint8
}

type coralBuild struct {
	f     *coralFeature
	w     *featureWorld
	r     *rand.MT
	block uint32
	color int
	tops  []blockPos
	sides []coralSide
}

func (f *coralFeature) coralIndex(b uint32) int {
	for i, c := range f.blocks {
		if c == b {
			return i
		}
	}
	return -1
}

func (f *coralFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	o := w.o
	limit := int32(SeaLevel - 4)
	for y < limit {
		if b, ok := w.block(x, y, z); ok && o.waterMaterial(b) {
			break
		}
		y++
	}
	if y >= limit {
		return blockPos{}, false
	}
	below, _ := w.block(x, y-1, z)
	at, _ := w.block(x, y, z)
	if !o.solidMaterial(below) || !o.waterMaterial(at) {
		return blockPos{}, false
	}
	if i := f.coralIndex(below); i >= 0 && i < 5 {
		return blockPos{}, false
	}
	color := int(r.IntN(5))
	b := &coralBuild{f: f, w: w, r: r, block: f.blocks[color], color: color}
	b.build(blockPos{x, y, z})

	for _, p := range b.tops {
		at, _ := w.block(p[0], p[1], p[2])
		if !o.waterMaterial(at) {
			continue
		}
		if below, _ := w.block(p[0], p[1]-1, p[2]); o.waterMaterial(below) || !o.solidMaterial(below) {
			continue
		}
		fan := r.IntN(5)
		dir := r.IntN(2)
		w.set(p[0], p[1], p[2], f.fans[fan][dir])
	}
	for _, s := range b.sides {
		base, _ := w.block(s.pos[0], s.pos[1], s.pos[2])
		attach := f.coralIndex(base) >= 0 || base == f.stone
		row := coralExceptTable()[2] // BDS's default row (byte_148FC6D)
		if s.dir >= 2 && s.dir <= 5 {
			row = coralExceptTable()[s.dir-2]
		}
		nd := row[r.IntN(3)]
		v := facingVec()[nd]
		tx, ty, tz := s.pos[0]+v[0], s.pos[1]+v[1], s.pos[2]+v[2]
		t, _ := w.block(tx, ty, tz)
		if !o.waterMaterial(t) || !attach {
			continue
		}
		// VERIFY: BDS also asks whether the wall fan may be placed in the
		// water there (BlockSource vfunc +88); taken to be always yes.
		c := r.IntN(2) + 2*r.IntN(3)
		c = min(max(c, 0), 4)
		w.set(tx, ty, tz, f.wallFans[c][wallFanDirection()[nd-2]])
	}
	return blockPos{x, y, z}, true
}

// build runs BDS's shape switch, whose cases fall through: 0 builds a
// plant, a spire and a clump or hand; 1 a spire and a clump or hand; 2 a
// clump or hand.
func (b *coralBuild) build(p blockPos) {
	kind := b.r.IntN(3)
	if kind == 0 {
		b.plantLike(p)
	}
	if kind <= 1 {
		b.spire(p)
	}
	if b.r.IntN(7) != 0 {
		b.smallClump(p)
	} else {
		b.hand(p)
	}
}

func (b *coralBuild) at(p blockPos) uint32 {
	v, _ := b.w.block(p[0], p[1], p[2])
	return v
}

// canOverwrite is false only for a coral block of another colour.
func (b *coralBuild) canOverwrite(p blockPos) bool {
	i := b.f.coralIndex(b.at(p))
	return i < 0 || i == b.color
}

func (b *coralBuild) put(p blockPos) { b.w.set(p[0], p[1], p[2], b.block) }

// underwater is BDS's "may this coral block go here in water" check
// (BlockSource vfunc +88) as it works for a solid block: water at the
// position or right above it.
func (b *coralBuild) underwater(p blockPos) bool {
	o := b.w.o
	return o.waterMaterial(b.at(p)) || o.waterMaterial(b.at(blockPos{p[0], p[1] + 1, p[2]}))
}

func (b *coralBuild) solidBelow(p blockPos) bool {
	return b.w.o.solidMaterial(b.at(blockPos{p[0], p[1] - 1, p[2]}))
}

func (b *coralBuild) setOnSolid(p blockPos) bool {
	if !b.canOverwrite(p) || !b.solidBelow(p) {
		return false
	}
	b.put(p)
	return true
}

func add(p blockPos, d uint8) blockPos {
	v := facingVec()[d]
	return blockPos{p[0] + v[0], p[1] + v[1], p[2] + v[2]}
}

func up(p blockPos, n int32) blockPos { return blockPos{p[0], p[1] + n, p[2]} }

// starFormation places the centre, then rolls for the four sides at the
// offset; a side that cannot be placed ends it.
func (b *coralBuild) starFormation(p blockPos, chance float32, off int32) {
	if !b.canOverwrite(p) {
		return
	}
	b.put(p)
	for _, d := range [4][2]int32{{off, 0}, {-off, 0}, {0, off}, {0, -off}} {
		if b.r.Float() < chance && !b.setOnSolid(blockPos{p[0] + d[0], p[1], p[2] + d[1]}) {
			return
		}
	}
}

// starCorners rolls for the corners; BDS checks (-off, +off) twice and never
// (-off, -off).
func (b *coralBuild) starCorners(p blockPos, chance float32, off int32) {
	for _, d := range [4][2]int32{{off, off}, {-off, off}, {-off, off}, {off, -off}} {
		if b.r.Float() < chance && !b.setOnSolid(blockPos{p[0] + d[0], p[1], p[2] + d[1]}) {
			return
		}
	}
}

func (b *coralBuild) side(p blockPos, d uint8) {
	b.sides = append(b.sides, coralSide{p, d})
}

func (b *coralBuild) plantLike(s blockPos) {
	rad := b.r.IntN(2) + 1
	width := 2*rad + 1
	for lvl := int32(0); lvl <= rad; lvl, width = lvl+1, width-2 {
		if !b.underwater(blockPos{s[0], s[1] + lvl + 1, s[2]}) {
			return
		}
		if !b.canOverwrite(s) {
			return
		}
		b.put(s)
		for k := int32(1); k <= width/2; k++ {
			p := up(s, lvl)
			b.starFormation(p, float32(k)*0.1+0.5-float32(lvl)*0.15, lvl)
			if rad == 2 && lvl == 0 {
				b.starCorners(p, 0.25, k)
			}
		}
	}
	top := up(s, rad)
	if !b.underwater(up(top, 1)) {
		return
	}
	first := uint8(b.r.IntN(4) + 2)
	second := coralExceptTable()[first-2][b.r.IntN(3)]
	third := coralThird(first, second, b.r)
	dirs := [3]uint8{first, second, third}
	if !b.canOverwrite(top) {
		return
	}
	b.put(top)
	b.arm(add(top, first), first, false)
	n := b.r.IntN(2) + 2
	for i := int32(1); i < n && int(i) < len(dirs); i++ {
		p := up(top, i)
		if !b.canOverwrite(p) {
			return
		}
		b.put(p)
		b.arm(add(p, dirs[i]), dirs[i], i == n-1) //nolint:gosec // the loop stops at len(dirs)
	}
}

// armBlock places one arm block if the water allows it and may record a
// side-fan spot for it.
func (b *coralBuild) armBlock(p blockPos) bool {
	if !b.underwater(up(p, 1)) || !b.canOverwrite(p) {
		return false
	}
	b.put(p)
	d := uint8(b.r.Uint32()&3) + 2
	if b.r.Float() < 0.4 {
		b.side(p, d)
	}
	return true
}

func (b *coralBuild) arm(p blockPos, dir uint8, last bool) {
	var kind int32
	if last {
		kind = int32(b.r.Uint32()&1) + 1
	} else {
		kind = b.r.IntN(3)
	}
	switch kind {
	case 0:
		// Two blocks stepping out and up, then two straight up, then one
		// back over.
		for i := int32(0); i < 2; i++ {
			q := up(p, i)
			if !b.underwater(up(q, 1)) || !b.canOverwrite(q) {
				return
			}
			b.put(q)
			p = add(p, dir)
		}
		p = up(p, 1)
		for i := 0; i < 2; i++ {
			p = up(p, 1)
			if !b.armBlock(p) {
				return
			}
		}
		p = add(up(p, 1), facingOpposite()[dir])
		if !b.underwater(up(p, 1)) || !b.canOverwrite(p) {
			return
		}
		b.put(p)
		if b.r.Float() < 0.2 {
			b.tops = append(b.tops, up(p, 1))
		}
	case 1:
		for i := int32(0); i < 2; i++ {
			if !b.armBlock(up(p, i)) {
				return
			}
		}
		q := up(add(p, dir), 1)
		for i := int32(0); i < 3; i++ {
			if !b.armBlock(up(q, i)) {
				return
			}
			if i == 2 && b.r.Float() < 0.2 {
				b.tops = append(b.tops, up(q, 3))
				return
			}
		}
	case 2:
		if !b.canOverwrite(p) {
			return
		}
		b.put(p)
		p = add(up(p, 1), dir)
		for i := int32(0); i < 2; i++ {
			if !b.armBlock(up(p, i)) {
				return
			}
		}
		turn := facingCounterCW()[dir-2]
		if b.r.Uint32()>>27&1 != 0 {
			turn = facingClockwise()[dir-2]
		}
		q := up(add(p, turn), 1)
		for i := int32(0); i < 2; i++ {
			if !b.armBlock(up(q, i)) {
				return
			}
			if i == 1 && b.r.Float() < 0.2 {
				b.tops = append(b.tops, up(q, 2))
				return
			}
		}
	}
}

// coralArm is one side block around a spire's stem: placed unless a draw
// of nextInt(n) is 0, at horizontal offset d.
type coralArm struct {
	n int32
	d [2]int32
}

// coralClawArms are the spire's side blocks in draw order.
func coralClawArms() [4]coralArm {
	return [4]coralArm{{2, [2]int32{1, 0}}, {3, [2]int32{-1, 0}}, {4, [2]int32{0, 1}}, {3, [2]int32{0, -1}}}
}

func (b *coralBuild) spire(o blockPos) {
	s := 2*b.r.IntN(2) + 3
	corner := blockPos{o[0] - s/2, o[1], o[2] - s/2}
	for a := int32(0); a < s; a++ {
		for c := int32(0); c < s; c++ {
			if (a == 0 || a == s-1) && (c == 0 || c == s-1) {
				continue
			}
			q := blockPos{corner[0] + a, o[1], corner[2] + c}
			if b.r.IntN(5) == 0 || !b.solidBelow(q) {
				continue
			}
			if !b.canOverwrite(q) {
				return
			}
			b.put(q)
		}
	}
	// BDS places the base corner too.
	if b.canOverwrite(corner) {
		b.put(corner)
	}
	c := up(o, 1)
	if !b.underwater(up(c, 1)) || !b.canOverwrite(c) {
		return
	}
	b.put(c)
	for _, s := range coralClawArms() {
		if b.r.IntN(s.n) == 0 {
			continue
		}
		q := blockPos{c[0] + s.d[0], c[1], c[2] + s.d[1]}
		if !b.canOverwrite(q) {
			return
		}
		b.put(q)
	}
	c = up(c, 1)
	b.starFormation(c, 0.33, 1)
	c = up(c, 1)
	b.starFormation(c, 0.7, 1)
	c = up(c, 1)
	n := b.r.IntN(2) + 1
	for i := int32(0); i < n; i++ {
		q := up(c, i)
		if !b.underwater(up(q, 1)) || !b.canOverwrite(q) {
			break
		}
		b.put(q)
		d := uint8(b.r.IntN(4)) + 2
		if b.r.Float() < 0.4 {
			// BDS records the column's base, not this block.
			b.side(c, d)
		}
		if i == n-1 {
			if b.r.Float() < 0.4 {
				b.tops = append(b.tops, up(c, n))
			}
			break
		}
	}
}

func (b *coralBuild) smallClump(o blockPos) {
	o2 := b.w.o
	for a := int32(0); a < 3; a++ {
		for c := int32(0); c < 3; c++ {
			q := blockPos{o[0] - 1 + a, o[1], o[2] - 1 + c}
			if b.r.IntN(3) == 0 {
				continue
			}
			if o2.waterMaterial(b.at(up(q, -1))) {
				continue
			}
			if !b.canOverwrite(q) {
				return
			}
			b.put(q)
		}
	}
	c := up(o, 1)
	if !b.underwater(up(c, 1)) || !b.canOverwrite(c) {
		return
	}
	b.put(c)
	for _, d := range [4][2]int32{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
		if b.r.IntN(4) == 0 {
			continue
		}
		q := blockPos{c[0] + d[0], c[1], c[2] + d[1]}
		if o2.waterMaterial(b.at(up(q, -1))) {
			continue
		}
		if !b.canOverwrite(q) {
			return
		}
		b.put(q)
		if b.r.Float() < 0.2 {
			b.tops = append(b.tops, up(q, 1))
		}
	}
	if b.underwater(up(c, 2)) {
		q := up(c, 1)
		if !b.canOverwrite(q) {
			return
		}
		b.put(q)
		if b.r.Float() < 0.2 {
			b.tops = append(b.tops, up(c, 2))
		}
	}
	if b.r.Float() < 0.25 && b.underwater(up(c, 3)) {
		q := up(c, 2)
		if b.canOverwrite(q) {
			b.put(q)
			if b.r.Float() < 0.1 {
				b.tops = append(b.tops, up(c, 3))
			}
		}
	}
}

func (b *coralBuild) hand(o blockPos) {
	dir := uint8(b.r.IntN(4)) + 2
	cw := facingClockwise()[dir-2]
	ccw := facingOpposite()[cw]
	wide := b.r.IntN(2) != 0
	h := b.r.IntN(3) + 1
	if !b.underwater(up(o, h+2)) {
		return
	}
	for i := int32(0); i < h; i++ {
		q := up(o, i)
		if !b.canOverwrite(q) {
			return
		}
		b.put(q)
	}
	t := up(o, h)
	a, c := t, t
	if wide {
		if b.r.Uint32()>>27&1 != 0 {
			a = add(a, cw)
			if !b.canOverwrite(a) {
				return
			}
			b.put(a)
			a = add(a, cw)
		} else {
			a = add(a, cw)
			c = add(c, ccw)
			if !b.canOverwrite(c) {
				return
			}
			b.put(c)
		}
		c = add(c, ccw)
	} else {
		a = add(a, cw)
		c = add(c, ccw)
	}
	if !b.canOverwrite(a) {
		return
	}
	b.put(a)
	if !b.canOverwrite(c) {
		return
	}
	b.put(c)
	fingers := [3]blockPos{add(t, dir), add(up(a, 1), dir), add(up(c, 1), dir)}
	for k := range fingers {
		n := int32(b.r.Uint32()&1) + 2
		if k != 0 {
			n += 2
		}
		for range n {
			p := fingers[k]
			if !b.canOverwrite(p) {
				return
			}
			b.put(p)
			if b.r.Float() < 0.1 {
				b.tops = append(b.tops, up(p, 1))
			}
			if b.r.Float() < 0.6 {
				b.side(p, cw)
			}
			if b.r.Float() < 0.6 {
				b.side(p, ccw)
			}
			if b.r.Float() < 0.25 && b.underwater(up(p, 2)) {
				p = up(p, 1)
			}
			fingers[k] = add(p, dir)
		}
	}
}

// coralHangFeature is BDS's CoralHangFeature (place: sub_499C280): a tube
// or brain wall fan on the side of a coral block or stone, facing water.
type coralHangFeature struct {
	coral *coralFeature
}

func (f *coralHangFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	o := w.o
	limit := int32(SeaLevel - 13)
	py := y
	for {
		if b, ok := w.block(x, py, z); ok && o.solidMaterial(b) {
			break
		}
		if py <= limit {
			break
		}
		py--
	}
	base, _ := w.block(x, py, z)
	if !o.solidMaterial(base) {
		return blockPos{}, false
	}
	if f.coral.coralIndex(base) < 0 && base != f.coral.stone {
		return blockPos{}, false
	}
	// Up to four tries in a shuffled order: a pick from the remaining sides
	// that is not open water is swapped to the end and dropped.
	dirs := [4]uint8{5, 3, 4, 2}
	dir := uint8(2)
	for n := int32(4); n > 0; n-- {
		i := r.IntN(n)
		v := facingVec()[dirs[i]]
		if b, _ := w.block(x+v[0], py+v[1], z+v[2]); o.waterMaterial(b) {
			dir = dirs[i]
			break
		}
		dirs[i], dirs[n-1] = dirs[n-1], dirs[i]
	}
	v := facingVec()[dir]
	if b, _ := w.block(x+v[0], py+v[1], z+v[2]); !o.waterMaterial(b) {
		return blockPos{}, false
	}
	// VERIFY: BDS asks whether a wall fan may go at the rule's position
	// plus the direction (BlockSource vfunc +88); taken to be always yes.
	color := 0
	if r.IntN(2) > 0 {
		color = 1
	}
	w.set(x+v[0], py+v[1], z+v[2], f.coral.wallFans[color][wallFanDirection()[dir-2]])
	return blockPos{x, py, z}, true
}
