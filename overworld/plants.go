package overworld

import (
	"strings"

	"github.com/helix-bedrock/worldgen/rand"
)

// Plant features BDS registers in code (sub_4E2BB50), written from BDS 1.26.52.

// waterlilyFeature is BDS's WaterlilyFeature (place sub_4DE79B0): a lily pad in
// air right above water, not under the height map. It draws nothing.
type waterlilyFeature struct {
	lily uint32
}

func (f *waterlilyFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	o := w.o
	if b, ok := w.block(x, y, z); !ok || b != o.ids.air {
		return blockPos{}, false
	}
	// WaterlilyBlock's mayPlaceOn (sub_CE60440): minecraft:water below.
	if b, ok := w.block(x, y-1, z); !ok || b != o.ids.water {
		return blockPos{}, false
	}
	if y >= MaxY || y < w.heightmap(x, z) {
		return blockPos{}, false
	}
	w.set(x, y, z, f.lily)
	return blockPos{x, y, z}, true
}

// builtOver is BlockType's canBeBuiltOver (sub_C5CC550): the block's replaceable
// component, which snowReplaces lists.
func (o *Overworld) builtOver(b uint32) bool { return o.snowReplaces(b) }

// cactusFeature is BDS's CactusFeature (place sub_4988860): 10 tries in a box
// seven blocks out and three up and down, each a cactus column of 1 to 3 into
// air, and in new worlds a cactus flower on top one time in three.
type cactusFeature struct {
	cactus, flower uint32
	// flowers is set from a base game version check (sub_4E2BB50), on for
	// new worlds.
	flowers bool
}

func (f *cactusFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	o := w.o
	for range 10 {
		dz := drawDiff(r, 7)
		dy := drawDiff(r, 3)
		dx := drawDiff(r, 7)
		px, py, pz := x+dx, y+dy, z+dz
		if b, ok := w.block(px, py, pz); !ok || b != o.ids.air {
			continue
		}
		h := r.IntN(r.IntN(3)+1) + 1
		n := int32(0)
		for ; n < h; n++ {
			if !f.survives(w, px, py+n, pz) {
				break
			}
			w.set(px, py+n, pz, f.cactus)
		}
		if f.flowers && n != 0 && r.IntN(3) == 0 {
			if b, ok := w.block(px, py+n, pz); ok && b == o.ids.air {
				w.set(px, py+n, pz, f.flower)
			}
		}
	}
	return blockPos{x, y, z}, true
}

// survives is CactusBlock's canSurvive (sub_C5DA370): no solid block or lava on
// the four sides, no lava above, and cactus or sand below.
// VERIFY: the tag below (xmmword_F613DE8) is taken as sand.
func (f *cactusFeature) survives(w *featureWorld, x, y, z int32) bool {
	o := w.o
	for _, d := range [4][2]int32{{0, -1}, {0, 1}, {-1, 0}, {1, 0}} {
		if b, ok := w.block(x+d[0], y, z+d[1]); ok && (o.solidMaterial(b) || o.isLava(b)) {
			return false
		}
	}
	if b, ok := w.block(x, y+1, z); ok && o.isLava(b) {
		return false
	}
	below, ok := w.block(x, y-1, z)
	return ok && (below == f.cactus || o.hasBlockTag(below, "sand"))
}

// deadBushFeature is BDS's DeadBushFeature (place sub_499C930): from the origin it
// walks down through air and leaves to one block below the first other block,
// then makes 4 tries around there for a dead bush in air.
type deadBushFeature struct {
	bush uint32
}

func (f *deadBushFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	o := w.o
	// BDS reads the block and then steps down, so it stops one below.
	for y > MinY {
		b, ok := w.block(x, y, z)
		y--
		if !ok || b != o.ids.air && !o.hasBlockTag(b, "leaves") {
			break
		}
	}
	for range 4 {
		dz := drawDiff(r, 7)
		dy := drawDiff(r, 3)
		dx := drawDiff(r, 7)
		px, py, pz := x+dx, y+dy, z+dz
		if py < MinY || py >= MaxY {
			continue
		}
		if b, ok := w.block(px, py, pz); !ok || b != o.ids.air {
			continue
		}
		if below, ok := w.block(px, py-1, pz); ok && o.deadBushSoil(below) {
			w.set(px, py, pz, f.bush)
		}
	}
	return blockPos{x, y, z}, true
}

// deadBushSoil is DeadBushBlock's mayPlaceOn (sub_C577C90): sand, terracotta or
// dirt.
// VERIFY: BDS checks a block list and two tags; read here as these three.
func (o *Overworld) deadBushSoil(b uint32) bool {
	return o.hasBlockTag(b, "sand") || strings.HasSuffix(o.blockName(b), "terracotta") || o.hasBlockTag(b, "dirt")
}

// bambooFeature is BDS's BambooFeature (place sub_49856C0): a thick bamboo stalk
// into air on grass, podzol, dirt or rooted dirt, 5 to 16 tall, with leaves on its
// top three blocks when it is 3 or more tall.
type bambooFeature struct {
	stalk, large, small uint32
	soil                [4]uint32
}

func (o *Overworld) newBamboo() *bambooFeature {
	b, _ := o.reg.Default("minecraft:bamboo")
	b = o.withProp(b, "bamboo_stalk_thickness", "thick")
	f := &bambooFeature{stalk: b, large: o.withProp(b, "bamboo_leaf_size", "large_leaves"),
		small: o.withProp(b, "bamboo_leaf_size", "small_leaves")}
	for i, n := range []string{"minecraft:grass_block", "minecraft:podzol", "minecraft:dirt", "minecraft:dirt_with_roots"} {
		f.soil[i] = o.blockID(n)
	}
	return f
}

func (f *bambooFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	o := w.o
	if b, ok := w.block(x, y, z); !ok || b != o.ids.air || y < MinY || y >= MaxY {
		return blockPos{}, false
	}
	if below, ok := w.block(x, y-1, z); !ok || !inList(f.soil[:], below) {
		return blockPos{}, false
	}
	// The bound comes from a hash of the position: 8 to 12.
	a := uint32(3129871*x) ^ uint32(116129781*z)
	hb := uint8((a * (42317861*a + 11)) >> 24)
	n := r.IntN(int32(hb%5|8)) + 5
	py := y
	for range n {
		if b, ok := w.block(x, py, z); !ok || b != o.ids.air {
			break
		}
		w.set(x, py, z, f.stalk)
		py++
	}
	if py-y >= 3 {
		w.set(x, py, z, f.large)
		w.set(x, py-1, z, f.large)
		w.set(x, py-2, z, f.small)
	}
	return blockPos{x, y, z}, true
}

// podzolAreaFeature is BDS's PodzolAreaFeature (place sub_49C0EA0): a disc of
// radius 1 to 4 one block below the origin, turning dirt and grass blocks into
// podzol. It fails when it changed nothing.
type podzolAreaFeature struct {
	podzol, dirt uint32
}

func (f *podzolAreaFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	o := w.o
	n := r.IntN(4) + 1
	placed := false
	for bx := x - n; bx <= x+n; bx++ {
		for bz := z - n; bz <= z+n; bz++ {
			if (bx-x)*(bx-x)+(bz-z)*(bz-z) > n*n {
				continue
			}
			if b, ok := w.block(bx, y-1, bz); ok && (b == f.dirt || b == o.ids.grass) {
				w.set(bx, y-1, bz, f.podzol)
				placed = true
			}
		}
	}
	return blockPos{x, y, z}, placed
}

// vinesFeature is BDS's VinesFeature (place sub_49EB9F0): from y 64 up to the top
// of the chunk, a vine in each air block that has a support beside it (tried
// with faces north, south, west, east); a block in the way moves the column up
// to three blocks sideways.
type vinesFeature struct {
	single *vinesSingleFaceFeature
}

// vineFaces are the supports for faces 2 to 5 and the vine_direction_bits each
// gives (the face's opposite, byte_1491886, as a bit, qword_1ADFEF8).
type vineFace struct {
	dx, dz int32
	bits   float64
}

func vineFaces() [4]vineFace {
	return [4]vineFace{{0, 1, 1}, {0, -1, 4}, {1, 0, 8}, {-1, 0, 2}}
}

func (f *vinesFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	o := w.o
	top := w.chunkTop(x, z)
	if top == MinY {
		top = MaxY
	}
	px, pz := x, z
	for py := int32(64); py < top; py++ {
		if b, ok := w.block(px, py, pz); !ok || b != o.ids.air {
			px = x + drawDiff(r, 3)
			pz = z + drawDiff(r, 3)
			continue
		}
		for _, s := range vineFaces() {
			// VERIFY: VineBlock's mayPlace for a face is taken as the
			// single-face vine's support rule.
			b, _ := w.block(px+s.dx, py, pz+s.dz)
			if b == f.single.invisibleBedrock || !o.sturdyFace(b, faceUp) {
				continue
			}
			w.set(px, py, pz, o.withProp(f.single.vine, "vine_direction_bits", s.bits))
			break
		}
	}
	return blockPos{x, y, z}, true
}

// chunkTop is the y above the highest non-air block of the chunk holding a
// column, or MinY when it is empty.
// VERIFY: BDS adds the dimension's minimum height to a chunk field (+4056); it is
// taken to be the highest block.
func (w *featureWorld) chunkTop(x, z int32) int32 {
	c := w.chunk(x, z)
	if c == nil {
		return MinY
	}
	oc := w.overlayAt(x>>4, z>>4, false)
	cx, cz := x&^15, z&^15
	for s := Height/16 - 1; s >= 0; s-- {
		sub := &c.subs[s]
		empty := sub.idx == nil && sub.wide == nil && uint32(sub.one) == w.o.ids.air
		if empty && oc != nil {
			for _, v := range oc.set[s*64 : s*64+64] {
				if v != 0 {
					empty = false
					break
				}
			}
		}
		if empty {
			continue
		}
		for y := int32(MinY + s*16 + 15); y >= int32(MinY+s*16); y-- {
			for i := range int32(256) {
				if b, ok := w.block(cx+i&15, y, cz+i>>4); ok && b != w.o.ids.air {
					return y + 1
				}
			}
		}
	}
	return MinY
}

// fireflyBushFeature is BDS's FireflyBushClusterFeature (place sub_51BFC30): up to
// 20 tries for a firefly bush on the height map within four blocks of the origin
// (with water beside the block below when water is set), then 1 to 3 more around
// the first in up to 20 tries.
type fireflyBushFeature struct {
	bush  uint32
	water bool
}

func (f *fireflyBushFeature) mayPlace(w *featureWorld, x, y, z int32) bool {
	o := w.o
	if y < MinY || y >= MaxY {
		return false
	}
	// BushBlock's mayPlace (sub_C5D5A20): no liquid, a block that can be
	// built over, and the bush's ground below (the list BushBlock checks,
	// read as the dirt tag).
	b, ok := w.block(x, y, z)
	if !ok || o.isWater(b) || b == o.ids.lava || b == o.ids.flowingLava || !o.builtOver(b) {
		return false
	}
	below, ok := w.block(x, y-1, z)
	return ok && o.hasBlockTag(below, "dirt")
}

func (f *fireflyBushFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	o := w.o
	var p blockPos
	found := false
	for range 20 {
		px := x + r.IntN(9) - 4
		pz := z + r.IntN(9) - 4
		py := w.heightmap(px, pz)
		if !f.mayPlace(w, px, py, pz) {
			continue
		}
		if f.water {
			wet := false
			for _, d := range [4][2]int32{{0, -1}, {0, 1}, {1, 0}, {-1, 0}} {
				if b, ok := w.block(px+d[0], py-1, pz+d[1]); ok && o.isWater(b) {
					wet = true
					break
				}
			}
			if !wet {
				continue
			}
		}
		p, found = blockPos{px, py, pz}, true
		break
	}
	if !found {
		return blockPos{}, false
	}
	w.set(p[0], p[1], p[2], f.bush)
	left := r.IntN(3) + 1
	for i := 0; i < 20 && left > 0; i++ {
		px := p[0] + r.IntN(9) - 4
		pz := p[2] + r.IntN(9) - 4
		py := w.heightmap(px, pz)
		if f.mayPlace(w, px, py, pz) {
			w.set(px, py, pz, f.bush)
			left--
		}
	}
	return p, true
}

// doublePlantFeature is BDS's DoublePlantFeature placing a given plant (sub_49A1160):
// when the origin is no more than four blocks above the top of the chunks within
// seven blocks, 64 tries in a box seven blocks out and three up and down for the
// plant in air.
type doublePlantFeature struct{}

func (doublePlantFeature) place(w *featureWorld, x, y, z int32, r *rand.MT, plant uint32) bool {
	o := w.o
	top := int32(0)
	for cx := (x - 7) >> 4; cx <= (x+7)>>4; cx++ {
		for cz := (z - 7) >> 4; cz <= (z+7)>>4; cz++ {
			if t := w.chunkTop(cx<<4, cz<<4); t != MinY {
				top = max(top, t)
			}
		}
	}
	if top == 0 {
		top = MaxY
	}
	if y-4 > top {
		return false
	}
	placed := false
	for range 64 {
		dz := drawDiff(r, 7)
		dy := drawDiff(r, 3)
		dx := drawDiff(r, 7)
		px, py, pz := x+dx, y+dy, z+dz
		if b, ok := w.block(px, py, pz); !ok || b != o.ids.air || !doublePlantMayPlace(w, px, py, pz) {
			continue
		}
		w.set(px, py, pz, o.withProp(plant, "upper_block_bit", false))
		w.set(px, py+1, pz, o.withProp(plant, "upper_block_bit", true))
		placed = true
	}
	return placed
}

// doublePlantMayPlace is DoublePlantBlock's mayPlace (sub_C5831F0): not in a
// liquid, a block that can be built over, bush ground below, and air or water
// above.
func doublePlantMayPlace(w *featureWorld, x, y, z int32) bool {
	o := w.o
	if y < MinY || y >= MaxY {
		return false
	}
	b, ok := w.block(x, y, z)
	if !ok || o.isWater(b) || b == o.ids.lava || b == o.ids.flowingLava || !o.builtOver(b) {
		return false
	}
	if below, ok := w.block(x, y-1, z); !ok || !o.hasBlockTag(below, "dirt") {
		return false
	}
	above, ok := w.block(x, y+1, z)
	return ok && (above == o.ids.air || o.waterMaterial(above))
}

// forestFoliageFeature is BDS's LegacyForestFoliageFeature (place sub_51D1E10): a
// few groups of lilac, rose bush or peony, each five tries at a random column of
// the chunk and a random height below the height map plus 32. The forest has
// two fewer groups than the flower forest.
type forestFoliageFeature struct {
	fewer  int32 // 1 for the flower forest, 3 for the forest
	plants [3]uint32
}

func (f *forestFoliageFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	var dp doublePlantFeature
	for n := r.IntN(5) - f.fewer; n > 0; n-- {
		plant := f.plants[r.IntN(3)]
		for range 5 {
			px := x + r.IntN(16)
			pz := z + r.IntN(16)
			if h := w.heightmap(px, pz) + 32; h > 0 {
				dp.place(w, px, r.IntN(int32(uint16(h))), pz, r, plant)
			}
		}
	}
	return blockPos{x, y, z}, true
}

// eyeblossomFeature is BDS's EyeblossomFeature (place sub_51BFAE0): one eyeblossom
// in air on bush ground. BDS places it open when the level's time of day is
// 12601 to 23400 when the chunk is made (sub_C55F560), else closed; the
// generator has no clock, so it is placed closed, as at a new world's start.
type eyeblossomFeature struct {
	flower uint32
}

func (f *eyeblossomFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	o := w.o
	if b, ok := w.block(x, y, z); !ok || b != o.ids.air {
		return blockPos{}, false
	}
	if below, ok := w.block(x, y-1, z); !ok || !o.hasBlockTag(below, "dirt") {
		return blockPos{}, false
	}
	w.set(x, y, z, f.flower)
	return blockPos{x, y, z}, true
}

// rootSystemFeature is BDS's TrailToSurfaceFeature (place sub_49C5EE0), registered
// as minecraft:azalea_root_system_feature: from the origin it climbs, up to the
// height map and at most 184 blocks, to the first block on solid ground with room
// for the tree (two blocks above it free, not three of water), places the tree
// there, then roots in the column below and hanging roots under the origin.
type rootSystemFeature struct {
	tree           featureRef
	hanging, roots uint32
	// replace holds the block names the roots may take, in any state (BDS
	// compares block types).
	replace map[string]bool
	// space is the test for the block the tree goes in; nil is the
	// azalea's.
	space func(o *Overworld, b uint32) bool
}

// azaleaSpace is the azalea's space test (sub_D299D10): air, a replaceable
// block, water or leaves.
// VERIFY: plant materials pass only on a region check that was not read.
func azaleaSpace(o *Overworld, b uint32) bool {
	return b == o.ids.air || o.builtOver(b) || o.waterMaterial(b) || o.hasBlockTag(b, "leaves")
}

// sulfurSpringSpace is the sulfur spring trail's space test (sub_4FAE5F0):
// a block that can be built over (BlockType vfunc +672) and is not a
// liquid.
func sulfurSpringSpace(o *Overworld, b uint32) bool {
	return o.builtOver(b) && !o.liquidMaterial(b)
}

func (f *rootSystemFeature) free(w *featureWorld, b uint32) bool {
	if f.space != nil {
		return f.space(w.o, b)
	}
	return azaleaSpace(w.o, b)
}

func (f *rootSystemFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	h := w.heightmap(x, z)
	for k, py := 0, y; k < 184; k, py = k+1, py+1 {
		ok := f.treeFits(w, x, py, z, r)
		if ok {
			f.grow(w, x, y, z, py, r)
			return blockPos{x, py, z}, true
		}
		if py >= h {
			break
		}
	}
	return blockPos{}, false
}

// grow places the roots from the origin up to the tree and the hanging roots
// below the origin.
func (f *rootSystemFeature) grow(w *featureWorld, x, y, z, treeY int32, r *rand.MT) {
	o := w.o
	for py := y + 1; py <= treeY; py++ {
		for range 20 {
			px := x + r.IntN(3) - r.IntN(3)
			pz := z + r.IntN(3) - r.IntN(3)
			if b, ok := w.block(px, py, pz); ok && f.replace[o.blockName(b)] {
				w.set(px, py, pz, f.roots)
			}
		}
	}
	px, py, pz := x, y, z
	for range 20 {
		dx := r.IntN(3)
		dx -= r.IntN(3)
		dz := r.IntN(3)
		dz -= r.IntN(3)
		dy := r.IntN(2)
		dy -= r.IntN(2)
		px, py, pz = px+dx, py+dy, pz+dz
		if b, ok := w.block(px, py, pz); !ok || b != o.ids.air {
			continue
		}
		if above, ok := w.block(px, py+1, pz); ok && o.solidMaterial(above) {
			w.set(px, py, pz, f.hanging)
		}
	}
}

// melonFeature is BDS's MelonFeature (place sub_51D8410): 64 tries in a box seven
// blocks out and three up and down from the origin, each a melon on grass where
// the block can be built over.
type melonFeature struct {
	melon uint32
}

func (f *melonFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	o := w.o
	for range 64 {
		dz := drawDiff(r, 7)
		dy := drawDiff(r, 3)
		dx := drawDiff(r, 7)
		px, py, pz := x+dx, y+dy, z+dz
		if py < MinY || py >= MaxY {
			continue
		}
		if b, ok := w.block(px, py, pz); !ok || !o.builtOver(b) {
			continue
		}
		if b, ok := w.block(px, py-1, pz); ok && b == o.ids.grass {
			w.set(px, py, pz, f.melon)
		}
	}
	return blockPos{x, y, z}, true
}

// treeFits places the tree at (x, py, z) when the spot holds it: solid
// below, three free air or water blocks, not all water.
func (f *rootSystemFeature) treeFits(w *featureWorld, x, py, z int32, r *rand.MT) bool {
	o := w.o
	if b, ok := w.block(x, py-1, z); !ok || !o.solidMaterial(b) {
		return false
	}
	b0, ok0 := w.block(x, py, z)
	if !ok0 || !f.free(w, b0) || !(ok0 && o.emptyOrWater(b0)) {
		return false
	}
	b1, ok1 := w.block(x, py+1, z)
	if !(ok1 && o.emptyOrWater(b1)) {
		return false
	}
	b2, ok2 := w.block(x, py+2, z)
	if !(ok2 && o.emptyOrWater(b2)) || o.waterMaterial(b0) && o.waterMaterial(b1) && o.waterMaterial(b2) {
		return false
	}
	// BDS then lowers the tree by one or two blocks when the block
	// below is water; it was just tested solid, so the tree stays.
	_, placed := f.tree.place(w, x, py, z, r)
	return placed
}
