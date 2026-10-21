package overworld

import (
	"encoding/json"
	"math"

	"github.com/helix-bedrock/worldgen/rand"
)

// Faces are numbered as BDS numbers them: 0 down, 1 up, 2 north (-z),
// 3 south (+z), 4 west (-x), 5 east (+x); faceStep holds their steps.

// oppositeFace is the face on the other side of each face.
var oppositeFace = [6]int{1, 0, 3, 2, 5, 4}

// faceBits is each face's bit in multi_face_direction_bits.
var faceBits = [6]uint8{1, 2, 16, 4, 8, 32}

// spreadFaceOrder is the face order BDS walks when it spreads a multiface
// block from every face (sub_CF69030 callers) or removes faces (sub_CF43F70).
var spreadFaceOrder = [6]int{1, 0, 2, 3, 4, 5}

// shuffledFaces is Direction::allShuffled (sub_B4AAAA0): five raw draws
// insert faces 1 to 5 into [0].
func shuffledFaces(r *rand.MT) [6]int {
	a := [6]int{1, 0, 2, 3, 4, 5}
	if r.Uint32()&1 == 0 {
		a[0], a[1] = 0, 1
	}
	for i := uint32(2); i < 6; i++ {
		var j uint32
		if i == 3 {
			j = r.Uint32() & 3
		} else {
			j = r.Uint32() % (i + 1)
		}
		if j != i {
			a[i] = a[j]
			a[j] = int(i)
		}
	}
	return a
}

// multifaceBlock is one block with multi_face_direction_bits (sculk vein,
// glow lichen): its state for each bit set and the bits of each state.
type multifaceBlock struct {
	states [64]uint32
	bits   map[uint32]uint8
	// vein selects the sculk vein spread config (SculkVeinSpreaderConfig,
	// vtable off_F33E3D8); otherwise the default config (off_F34B7D8).
	vein bool
}

func (o *Overworld) newMultifaceBlock(name string) *multifaceBlock {
	def, ok := o.reg.Default(name)
	if !ok {
		return nil
	}
	m := &multifaceBlock{bits: map[uint32]uint8{}, vein: name == "minecraft:sculk_vein"}
	for i := range m.states {
		m.states[i] = def
		if id := o.withProp(def, "multi_face_direction_bits", float64(i)); id != def || i == 0 {
			m.states[i] = id
		}
		m.bits[m.states[i]] = uint8(i)
	}
	return m
}

func (m *multifaceBlock) is(b uint32) bool {
	_, ok := m.bits[b]
	return ok
}

// hasFace is MultifaceBlock::hasFace (sub_CF66420); false for other blocks.
func (m *multifaceBlock) hasFace(b uint32, face int) bool {
	bits, ok := m.bits[b]
	return ok && bits&faceBits[face] != 0
}

// sculkBlocks are the states the sculk features place and test.
type sculkBlocks struct {
	sculk, catalyst, sensor, shrieker, shriekerSummon, movingBlock uint32
	vein                                                           *multifaceBlock
	// replace is the world-generation spreader's replaceable set
	// (sub_CF48280). BDS tests the block pointer, so only each name's
	// default state matches.
	replace map[uint32]bool
}

// sculkReplaceableWorldGen is sub_CF48280's list, in order.
var sculkReplaceableWorldGen = []string{
	"stone", "granite", "diorite", "andesite", "tuff", "deepslate", "grass_block", "podzol", "dirt", "coarse_dirt",
	"mycelium", "dirt_with_roots", "moss_block", "pale_moss_block", "hardened_clay", "white_terracotta",
	"orange_terracotta", "magenta_terracotta", "light_blue_terracotta", "yellow_terracotta", "lime_terracotta",
	"pink_terracotta", "gray_terracotta", "light_gray_terracotta", "cyan_terracotta", "purple_terracotta",
	"blue_terracotta", "brown_terracotta", "green_terracotta", "red_terracotta", "black_terracotta",
	"crimson_nylium", "warped_nylium", "netherrack", "basalt", "blackstone", "sand", "red_sand", "gravel",
	"soul_sand", "soul_soil", "calcite", "smooth_basalt", "clay", "dripstone_block", "end_stone", "sandstone",
	"red_sandstone", "mud", "muddy_mangrove_roots", "deepslate_bricks", "deepslate_tiles", "cobbled_deepslate",
	"cracked_deepslate_bricks", "cracked_deepslate_tiles", "polished_deepslate", "sulfur", "cinnabar",
}

func (o *Overworld) newSculkBlocks() *sculkBlocks {
	id := func(n string) uint32 {
		b, _ := o.reg.Default("minecraft:" + n)
		return b
	}
	s := &sculkBlocks{sculk: id("sculk"), catalyst: id("sculk_catalyst"), sensor: id("sculk_sensor"),
		shrieker: id("sculk_shrieker"), movingBlock: id("moving_block"), vein: o.newMultifaceBlock("minecraft:sculk_vein"),
		replace: map[uint32]bool{}}
	s.shriekerSummon = o.withProp(s.shrieker, "can_summon", true)
	for _, n := range sculkReplaceableWorldGen {
		if b, ok := o.reg.Default("minecraft:" + n); ok {
			s.replace[b] = true
		}
	}
	return s
}

// fireOrLava stands for BDS's lava (6) and fire (10) materials.
func (o *Overworld) fireOrLava(b uint32) bool {
	if o.isLava(b) {
		return true
	}
	n := o.blockName(b)
	return n == "minecraft:fire" || n == "minecraft:soul_fire"
}

// regrowable is sub_CF42810: a water-material block only when it is a
// source; any other block when it can be built over and is not lava or
// fire.
// VERIFY: "can be built over" is IBlockWorldGenAPI vfunc +152, read here as
// BlockLegacy::canBeBuiltOver through treeFree's list.
func (o *Overworld) regrowable(b uint32) bool {
	if o.waterMaterial(b) {
		return b == o.ids.water
	}
	return o.treeFree(b) && !o.fireOrLava(b)
}

// multifaceReplaceable is the default spread config's stateCanBeReplaced
// (sub_CF694D0): the same block, air, or a water source.
func (o *Overworld) multifaceReplaceable(m *multifaceBlock, b uint32) bool {
	return m.is(b) || b == o.ids.air || b == o.ids.water
}

// canSpreadInto is the spread config's test of a target (vtable +24).
func (o *Overworld) canSpreadInto(w *featureWorld, sb *sculkBlocks, m *multifaceBlock, x, y, z int32, face int) bool {
	b, _ := w.block(x, y, z)
	if !m.vein {
		return o.multifaceReplaceable(m, b)
	}
	// SculkVeinSpreaderConfig (sub_CF4B610).
	st := faceStep[face]
	nb, _ := w.block(x+st[0], y+st[1], z+st[2])
	if nb == sb.catalyst || nb == sb.movingBlock || nb == sb.sculk {
		return false
	}
	return o.regrowable(b) || o.multifaceReplaceable(m, b)
}

// placementState is MultifaceBlock::getStateForPlacement (sub_CF65FE0): cur
// with face added when the block on that face can hold it, else cur.
func (o *Overworld) placementState(w *featureWorld, m *multifaceBlock, cur uint32, x, y, z int32, face int) uint32 {
	var bits uint8
	if b, ok := m.bits[cur]; ok {
		if b&faceBits[face] != 0 {
			return cur
		}
		bits = b
	}
	st := faceStep[face]
	nb, _ := w.block(x+st[0], y+st[1], z+st[2])
	if !o.sturdyFace(nb, oppositeFace[face]) {
		return cur
	}
	return m.states[bits|faceBits[face]]
}

// spreadPos is where a multiface block spreads to and the face it takes.
type spreadPos struct {
	x, y, z int32
	face    int
}

// spreadTarget is MultifaceSpreader::getSpreadFromFaceTowardDirection
// (sub_CF680F0) with the three spread types BDS gives both configs (same
// position, same plane, wrap around), or only the first when samePosOnly.
func (o *Overworld) spreadTarget(w *featureWorld, sb *sculkBlocks, m *multifaceBlock, state uint32, x, y, z int32,
	face, dir int, samePosOnly bool) (spreadPos, bool) {
	other := m.vein && !m.is(state)
	if !other && !m.hasFace(state, face) || dir == face || dir == oppositeFace[face] {
		return spreadPos{}, false
	}
	if !other && m.hasFace(state, dir) {
		return spreadPos{}, false
	}
	d, f := faceStep[dir], faceStep[face]
	cands := [3]spreadPos{
		{x, y, z, dir},
		{x + d[0], y + d[1], z + d[2], face},
		{x + d[0] + f[0], y + d[1] + f[1], z + d[2] + f[2], oppositeFace[dir]},
	}
	n := 3
	if samePosOnly {
		n = 1
	}
	for _, c := range cands[:n] {
		if !o.canSpreadInto(w, sb, m, c.x, c.y, c.z, c.face) {
			continue
		}
		cur, _ := w.block(c.x, c.y, c.z)
		if o.placementState(w, m, cur, c.x, c.y, c.z, c.face) != cur {
			return c, true
		}
	}
	return spreadPos{}, false
}

// spreadTo places the multiface block at a spread target.
func (o *Overworld) spreadTo(w *featureWorld, m *multifaceBlock, p spreadPos) bool {
	cur, _ := w.block(p.x, p.y, p.z)
	ns := o.placementState(w, m, cur, p.x, p.y, p.z, p.face)
	if ns == cur {
		return false
	}
	w.set(p.x, p.y, p.z, ns)
	return true
}

// spreadAll spreads from every face toward every direction
// (sub_CF69030 for each face) and reports whether anything was placed.
func (o *Overworld) spreadAll(w *featureWorld, sb *sculkBlocks, m *multifaceBlock, state uint32, x, y, z int32, samePosOnly bool) bool {
	n := 0
	for _, face := range spreadFaceOrder {
		for _, dir := range spreadFaceOrder {
			if p, ok := o.spreadTarget(w, sb, m, state, x, y, z, face, dir, samePosOnly); ok && o.spreadTo(w, m, p) {
				n++
			}
		}
	}
	return n > 0
}

// removeVeinFace is MultifaceBlock::removeFace (sub_CF67AA0) without a
// level: the vein loses a face, or turns to air with none left.
func (o *Overworld) removeVeinFace(w *featureWorld, sb *sculkBlocks, cur uint32, x, y, z int32, face int) {
	bits := sb.vein.bits[cur] &^ faceBits[face]
	if bits != 0 {
		w.set(x, y, z, sb.vein.states[bits])
		return
	}
	w.set(x, y, z, o.ids.air)
}

// veinDischarged is SculkVeinBlock's onDischarged (sub_CF43F70): faces
// resting on sculk are removed.
func (o *Overworld) veinDischarged(w *featureWorld, sb *sculkBlocks, x, y, z int32) {
	for _, face := range spreadFaceOrder {
		cur, _ := w.block(x, y, z)
		if !sb.vein.hasFace(cur, face) {
			continue
		}
		st := faceStep[face]
		if nb, _ := w.block(x+st[0], y+st[1], z+st[2]); nb == sb.sculk {
			o.removeVeinFace(w, sb, cur, x, y, z, face)
		}
	}
}

// Cursor faces: a vein's direction bits, or one of these.
const (
	cursorFacesNone  = -1 // the start value: spread in place only
	cursorFacesSculk = -2 // set on sculk: spread everywhere
)

// sculkCursor is a SculkChargeCursor (28 bytes in BDS).
type sculkCursor struct {
	x, y, z               int32
	charge, update, decay int32
	faces                 int32
}

// sculkSpreader is SculkSpreader as the world-generation constructor makes
// it (sub_CF48190(1, 50, 1, 5, 10)).
type sculkSpreader struct {
	cost, noGrowth, decayRate, addDecay int32
	cursors                             []sculkCursor
	next                                []sculkCursor
}

// sculkNeighbours are the 18 non-corner neighbour offsets in BDS's order
// (qword_1C7B808) before shuffling.
var sculkNeighbours = [18][3]int32{
	{-1, -1, 0}, {0, -1, -1}, {0, -1, 0}, {0, -1, 1}, {1, -1, 0}, {-1, 0, -1}, {-1, 0, 0}, {-1, 0, 1}, {0, 0, -1},
	{0, 0, 1}, {1, 0, -1}, {1, 0, 0}, {1, 0, 1}, {-1, 1, 0}, {0, 1, -1}, {0, 1, 0}, {0, 1, 1}, {1, 1, 0},
}

// sculkKind is the behaviour a cursor's block has.
type sculkKind uint8

const (
	sculkDefault sculkKind = iota
	sculkKindVein
	sculkKindSculk
)

func (sb *sculkBlocks) kind(b uint32) sculkKind {
	switch {
	case b == sb.sculk:
		return sculkKindSculk
	case sb.vein.is(b):
		return sculkKindVein
	}
	return sculkDefault
}

func (sp *sculkSpreader) addCursors(x, y, z, charge int32) {
	for charge > 0 {
		c := min(charge, 1000)
		if len(sp.cursors) < 32 {
			sp.cursors = append(sp.cursors, sculkCursor{x: x, y: y, z: z, charge: c, decay: 1, faces: cursorFacesNone})
		}
		charge -= c
	}
}

// update is SculkSpreader::updateCursors (sub_CF493C0) for world
// generation. Merging cursors that meet only applies outside world
// generation, so every cursor still charged is kept, in order.
func (o *Overworld) updateSculk(w *featureWorld, sb *sculkBlocks, sp *sculkSpreader, ox, oy, oz int32, r *rand.MT, spreadVeins bool) {
	if len(sp.cursors) == 0 {
		return
	}
	sp.next = sp.next[:0]
	for _, c := range sp.cursors {
		if c.charge <= 0 {
			continue
		}
		if c.update > 0 {
			c.update--
			sp.next = append(sp.next, c)
			continue
		}
		b, _ := w.block(c.x, c.y, c.z)
		kind := sb.kind(b)
		if spreadVeins && o.attemptSpreadVein(w, sb, kind, b, c.x, c.y, c.z, c.faces) && kind != sculkKindSculk {
			b, _ = w.block(c.x, c.y, c.z)
			kind = sb.kind(b)
		}
		c.charge = o.useCharge(w, sb, sp, kind, &c, ox, oy, oz, r, spreadVeins)
		if c.charge <= 0 {
			if kind == sculkKindVein {
				o.veinDischarged(w, sb, c.x, c.y, c.z)
			}
			continue
		}
		var order [18]uint8
		for i := range order {
			order[i] = uint8(i)
		}
		for i := uint32(1); i < 18; i++ {
			if j := r.Uint32() % (i + 1); j != i {
				order[i], order[j] = order[j], order[i]
			}
		}
		if nx, ny, nz, ok := o.sculkMove(w, sb, c.x, c.y, c.z, &order); ok {
			if kind == sculkKindVein {
				o.veinDischarged(w, sb, c.x, c.y, c.z)
			}
			c.x, c.y, c.z = nx, ny, nz
			// BDS measures in 3D here; the limit is 15 blocks.
			dx, dy, dz := int64(nx-ox), int64(ny-oy), int64(nz-oz)
			if dx*dx+dy*dy+dz*dz > 224 {
				continue
			}
			b, _ = w.block(nx, ny, nz)
		}
		switch sb.kind(b) {
		case sculkKindSculk:
			c.faces = cursorFacesSculk
			kind = sculkKindSculk
		case sculkKindVein:
			c.faces = int32(sb.vein.bits[b])
			kind = sculkKindVein
		}
		c.update = 1
		if kind == sculkDefault {
			c.decay = max(c.decay, 1) - 1
		} else {
			c.decay = 1
		}
		sp.next = append(sp.next, c)
	}
	sp.cursors, sp.next = sp.next, sp.cursors
}

// sculkMove finds where a cursor moves next among the shuffled reachable
// sculk and vein neighbours: the first vein resting on a replaceable block,
// else the last of them.
func (o *Overworld) sculkMove(w *featureWorld, sb *sculkBlocks, x, y, z int32, order *[18]uint8) (int32, int32, int32, bool) {
	var mx, my, mz int32
	found := false
	for _, i := range order {
		d := sculkNeighbours[i]
		nx, ny, nz := x+d[0], y+d[1], z+d[2]
		b, _ := w.block(nx, ny, nz)
		kind := sb.kind(b)
		if kind == sculkDefault {
			continue
		}
		if abs32i(d[0])+abs32i(d[1])+abs32i(d[2]) == 2 && !o.sculkUnobstructed(w, x, y, z, d) {
			continue
		}
		mx, my, mz, found = nx, ny, nz, true
		if kind == sculkKindVein && o.veinOnReplaceable(w, sb, b, nx, ny, nz) {
			break
		}
	}
	return mx, my, mz, found
}

// sculkUnobstructed is the diagonal move test: one of the two blocks the
// move passes beside must not be sturdy toward it.
func (o *Overworld) sculkUnobstructed(w *featureWorld, x, y, z int32, d [3]int32) bool {
	var a, b int
	switch {
	case d[0] == 0:
		a, b = signFace(d[1], 0, 1), signFace(d[2], 2, 3)
	case d[1] == 0:
		a, b = signFace(d[0], 4, 5), signFace(d[2], 2, 3)
	default:
		a, b = signFace(d[0], 4, 5), signFace(d[1], 0, 1)
	}
	for _, f := range [2]int{a, b} {
		st := faceStep[f]
		nb, _ := w.block(x+st[0], y+st[1], z+st[2])
		if !o.sturdyFace(nb, oppositeFace[f]) {
			return true
		}
	}
	return false
}

func signFace(v int32, neg, pos int) int {
	if v > 0 {
		return pos
	}
	return neg
}

// veinOnReplaceable reports whether a vein has a face on a block the
// spreader may replace, checking faces up, down, north, south, west, east.
func (o *Overworld) veinOnReplaceable(w *featureWorld, sb *sculkBlocks, b uint32, x, y, z int32) bool {
	for _, face := range spreadFaceOrder {
		if !sb.vein.hasFace(b, face) {
			continue
		}
		st := faceStep[face]
		if nb, _ := w.block(x+st[0], y+st[1], z+st[2]); sb.replace[nb] {
			return true
		}
	}
	return false
}

// attemptSpreadVein is SculkBehavior::attemptSpreadVein for each kind
// (sub_CF423B0 default, sub_CF429C0 sculk, sub_CF433F0 vein).
func (o *Overworld) attemptSpreadVein(w *featureWorld, sb *sculkBlocks, kind sculkKind, b uint32, x, y, z int32, faces int32) bool {
	if kind != sculkDefault || faces == cursorFacesSculk {
		return o.spreadAll(w, sb, sb.vein, b, x, y, z, false)
	}
	if faces == cursorFacesNone {
		return o.spreadAll(w, sb, sb.vein, b, x, y, z, true)
	}
	if !o.regrowable(b) {
		return false
	}
	w.set(x, y, z, sb.vein.states[uint8(faces)&63])
	return true
}

// useCharge is SculkBehavior::attemptUseCharge for each kind.
func (o *Overworld) useCharge(w *featureWorld, sb *sculkBlocks, sp *sculkSpreader, kind sculkKind, c *sculkCursor,
	ox, oy, oz int32, r *rand.MT, spreadVeins bool) int32 {
	switch kind {
	case sculkKindSculk:
		return o.sculkUseCharge(w, sb, sp, c, ox, oy, oz, r)
	case sculkKindVein:
		return o.veinUseCharge(w, sb, sp, c, r, spreadVeins)
	}
	if c.decay > 0 {
		return c.charge
	}
	return 0
}

// sculkUseCharge is the sculk block's attemptUseCharge (sub_CF42BC0): away
// from the origin it may grow a sensor or shrieker on top, paying the
// growth cost; otherwise the charge sometimes decays.
func (o *Overworld) sculkUseCharge(w *featureWorld, sb *sculkBlocks, sp *sculkSpreader, c *sculkCursor, ox, oy, oz int32, r *rand.MT) int32 {
	charge := c.charge
	if r.IntN(sp.decayRate) != 0 {
		return charge
	}
	fx := float32(ox) - float32(c.x)
	fy := float32(oy) - float32(c.y)
	fz := float32(oz) - float32(c.z)
	dist := float32(fz*fz) + float32(float32(fx*fx)+float32(fy*fy))
	r2 := float32(float32(sp.noGrowth) * float32(sp.noGrowth))
	near := r2 > dist
	if !near {
		if above, _ := w.block(c.x, c.y+1, c.z); (above == o.ids.air || o.blockName(above) == "minecraft:water") && o.growthsNear(w, sb, c.x, c.y, c.z) < 3 {
			if r.IntN(sp.cost) < charge {
				g := sb.shriekerSummon
				if r.IntN(11) != 0 {
					g = sb.sensor
				}
				w.set(c.x, c.y+1, c.z, g)
			}
			return max(charge-sp.cost, 0)
		}
	}
	if r.IntN(sp.addDecay) != 0 {
		return charge
	}
	pen := int32(1)
	if !near {
		dx, dy, dz := int64(c.x-ox), int64(c.y-oy), int64(c.z-oz)
		d := float32(dx*dx + dy*dy + dz*dz)
		if float32(sp.noGrowth*sp.noGrowth) < d {
			s := float32(math.Sqrt(float64(d)))
			t := float32(s - 4)
			q := float32(float32(t*t) / 400)
			if q > 1 {
				q = 1
			}
			if v := int32(float32(float32(float32(charge)*q) * 0.5)); v >= 2 {
				pen = v
			}
		}
	}
	return charge - pen
}

// growthsNear counts sculk sensors and shriekers from (x-4, y, z-4) to
// (x+4, y+2, z+4).
// VERIFY: the box is taken as inclusive, as BlockSource counts.
func (o *Overworld) growthsNear(w *featureWorld, sb *sculkBlocks, x, y, z int32) int {
	n := 0
	for gx := x - 4; gx <= x+4; gx++ {
		for gy := y; gy <= y+2; gy++ {
			for gz := z - 4; gz <= z+4; gz++ {
				if b, _ := w.block(gx, gy, gz); b == sb.sensor || b == sb.shrieker || b == sb.shriekerSummon {
					n++
				}
			}
		}
	}
	return n
}

// veinUseCharge is the sculk vein's attemptUseCharge (sub_CF435F0): a vein
// face resting on a replaceable block turns that block to sculk and spreads
// veins around it.
func (o *Overworld) veinUseCharge(w *featureWorld, sb *sculkBlocks, sp *sculkSpreader, c *sculkCursor, r *rand.MT, spreadVeins bool) int32 {
	decay := func() int32 {
		if r.IntN(sp.decayRate) == 0 {
			return int32(math.Floor(float64(float32(float32(c.charge) * 0.5))))
		}
		return c.charge
	}
	if !spreadVeins {
		return decay()
	}
	cur, _ := w.block(c.x, c.y, c.z)
	for _, d := range shuffledFaces(r) {
		if !sb.vein.hasFace(cur, d) {
			continue
		}
		st := faceStep[d]
		nx, ny, nz := c.x+st[0], c.y+st[1], c.z+st[2]
		if nb, _ := w.block(nx, ny, nz); !sb.replace[nb] {
			continue
		}
		w.set(nx, ny, nz, sb.sculk)
		opp := oppositeFace[d]
		for _, f := range spreadFaceOrder {
			if f == opp {
				continue
			}
			s := faceStep[f]
			o.veinDischarged(w, sb, nx+s[0], ny+s[1], nz+s[2])
		}
		o.spreadAll(w, sb, sb.vein, sb.sculk, nx, ny, nz, false)
		return c.charge - 1
	}
	return decay()
}

// sculkPatchFeature is SculkPatchFeature (place: sub_D302240).
type sculkPatchFeature struct {
	sb                         *sculkBlocks
	on                         blockMatcher
	central                    uint32
	hasCentral                 bool
	centralChance              float32
	cursors, charge, attempts  int32
	growthRounds, spreadRounds int32
	extra                      intRange
}

func (o *Overworld) parseSculkPatch(raw json.RawMessage) (placer, error) {
	var r struct {
		On            []json.RawMessage `json:"can_place_sculk_patch_on"`
		Central       string            `json:"central_block"`
		CentralChance float32           `json:"central_block_placement_chance"`
		Cursors       int32             `json:"cursor_count"`
		Charge        int32             `json:"charge_amount"`
		Attempts      int32             `json:"spread_attempts"`
		Growth        int32             `json:"growth_rounds"`
		Spread        int32             `json:"spread_rounds"`
		Extra         json.RawMessage   `json:"extra_growth_chance"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	sb := o.newSculkBlocks()
	if sb.vein == nil {
		return nil, nil
	}
	f := &sculkPatchFeature{sb: sb, on: parseMatcher(r.On), centralChance: r.CentralChance, cursors: r.Cursors,
		charge: r.Charge, attempts: r.Attempts, growthRounds: r.Growth, spreadRounds: r.Spread, extra: parseIntRange(r.Extra, 0)}
	if r.Central != "" {
		f.central, f.hasCentral = o.reg.Default(r.Central)
	}
	return f, nil
}

func (o *Overworld) solidBlock(b uint32) bool { return b != o.ids.air && o.solidMaterial(b) }

func (f *sculkPatchFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	o, sb := w.o, f.sb
	b, ok := w.block(x, y, z)
	if !ok {
		return blockPos{}, false
	}
	if b != sb.sculk && !sb.vein.is(b) {
		if b != o.ids.air && b != o.ids.water {
			return blockPos{}, false
		}
		found := false
		for _, face := range spreadFaceOrder {
			st := faceStep[face]
			nb, _ := w.block(x+st[0], y+st[1], z+st[2])
			if f.on.empty() && o.solidBlock(nb) || !f.on.empty() && f.on.match(o, nb) {
				found = true
				break
			}
		}
		if !found {
			return blockPos{}, false
		}
	}
	sp := &sculkSpreader{cost: 50, noGrowth: 1, decayRate: 5, addDecay: 10}
	for round := range f.growthRounds + f.spreadRounds {
		for range f.cursors {
			sp.addCursors(x, y, z, f.charge)
		}
		spreadVeins := round < f.spreadRounds
		for range f.attempts {
			o.updateSculk(w, sb, sp, x, y, z, r, spreadVeins)
		}
		sp.cursors = sp.cursors[:0]
	}
	if f.centralChance >= r.Float() && f.hasCentral {
		if below, _ := w.block(x, y-1, z); o.solidBlock(below) {
			w.set(x, y, z, f.central)
		}
	}
	n := f.extra.min
	if f.extra.max >= f.extra.min {
		n += r.IntN(f.extra.max - f.extra.min + 1)
	}
	for range n {
		gx := x + r.IntN(5) - 2
		gz := z + r.IntN(5) - 2
		if g, _ := w.block(gx, y, gz); g != o.ids.air {
			continue
		}
		if below, _ := w.block(gx, y-1, gz); o.sturdyFace(below, 1) {
			w.set(gx, y, gz, sb.shriekerSummon)
		}
	}
	return blockPos{x, y, z}, true
}

// multifaceFeature is MultifaceFeature (place: sub_D2EC400), used by sculk
// veins and glow lichen.
type multifaceFeature struct {
	sb     *sculkBlocks
	m      *multifaceBlock
	on     blockMatcher
	dirs   []int
	search int32
	chance float32
}

func (o *Overworld) parseMultiface(raw json.RawMessage) (placer, error) {
	var r struct {
		Search  int32             `json:"search_range"`
		Block   string            `json:"places_block"`
		Floor   bool              `json:"can_place_on_floor"`
		Ceiling bool              `json:"can_place_on_ceiling"`
		Wall    bool              `json:"can_place_on_wall"`
		Chance  float32           `json:"chance_of_spreading"`
		On      []json.RawMessage `json:"can_place_on"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	m := o.newMultifaceBlock(r.Block)
	if m == nil {
		return nil, nil
	}
	f := &multifaceFeature{sb: o.newSculkBlocks(), m: m, on: parseMatcher(r.On), search: r.Search, chance: r.Chance}
	// The flag setters append in the order the fields are read: floor
	// adds down, ceiling up, wall north, east, south, west.
	if r.Floor {
		f.dirs = append(f.dirs, 0)
	}
	if r.Ceiling {
		f.dirs = append(f.dirs, 1)
	}
	if r.Wall {
		f.dirs = append(f.dirs, 2, 5, 3, 4)
	}
	return f, nil
}

func (f *multifaceFeature) airOrWater(o *Overworld, b uint32) bool {
	return b == o.ids.air || o.waterMaterial(b)
}

func (f *multifaceFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	o := w.o
	b, ok := w.block(x, y, z)
	if !ok || !f.airOrWater(o, b) {
		return blockPos{}, false
	}
	// BDS shuffles with a thread-local random seeded from the OS, so this
	// order can never match BDS; it uses its own random so the feature's
	// random stays in step.
	tr := w.shuffleRandom()
	var dirs [6]int
	n := copy(dirs[:], f.dirs)
	for i := uint32(1); i < uint32(n); i++ {
		if j := tr.Uint32() % (i + 1); j != i {
			dirs[i], dirs[j] = dirs[j], dirs[i]
		}
	}
	if f.placeIfValid(w, x, y, z, r, dirs[:n]) {
		return blockPos{x, y, z}, true
	}
	if f.search <= 0 {
		return blockPos{}, false
	}
	for _, d := range dirs[:n] {
		// BDS steps from the origin every time, so the search range only
		// repeats the same test.
		st := faceStep[d]
		px, py, pz := x+st[0], y+st[1], z+st[2]
		nb, _ := w.block(px, py, pz)
		if !f.airOrWater(o, nb) && !f.m.is(nb) {
			continue
		}
		if f.placeIfValid(w, px, py, pz, r, dirs[:n]) {
			return blockPos{px, py, pz}, true
		}
	}
	return blockPos{}, false
}

// placeIfValid is sub_D2ECF80: the block goes on the first face, in dirs
// order, that rests on an allowed block, then may spread once.
// VERIFY: BDS also skips a neighbour in a chunk not yet far enough along;
// all neighbours are taken as ready here.
func (f *multifaceFeature) placeIfValid(w *featureWorld, x, y, z int32, r *rand.MT, dirs []int) bool {
	o := w.o
	cur, _ := w.block(x, y, z)
	face := -1
	for _, d := range dirs {
		st := faceStep[d]
		if nb, _ := w.block(x+st[0], y+st[1], z+st[2]); f.on.match(o, nb) {
			face = d
			break
		}
	}
	if face < 0 {
		return false
	}
	ns := o.placementState(w, f.m, cur, x, y, z, face)
	if ns == cur {
		return false
	}
	w.set(x, y, z, ns)
	if f.chance > r.Float() {
		// MultifaceSpreader::spreadFromFaceTowardRandomDirection
		// (sub_CF689E0), with the thread-local random.
		for _, dir := range shuffledFaces(w.shuffleRandom()) {
			if p, ok := o.spreadTarget(w, f.sb, f.m, ns, x, y, z, face, dir, false); ok && o.spreadTo(w, f.m, p) {
				break
			}
		}
	}
	return true
}

// surfaceThresholdFeature is SurfaceRelativeThresholdFeature (place:
// sub_D31EBF0): its feature is placed only at least a distance below the
// surface.
// VERIFY: BDS reads the surface from a cache at quarter resolution, taken
// here as the preliminary surface level.
type surfaceThresholdFeature struct {
	feat featureRef
	min  int32
}

func (f *surfaceThresholdFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	s := prelimSurface(w.o.column(x>>2, z>>2))
	if s == noSurface || int32(int16(s))-f.min <= y {
		return blockPos{}, false
	}
	p, ok := f.feat.set.get(f.feat.id)
	if !ok {
		return blockPos{}, false
	}
	return p.place(w, x, y, z, r)
}
