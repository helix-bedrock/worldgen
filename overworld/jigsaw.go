package overworld

import (
	"strings"

	"github.com/helix-bedrock/worldgen/internal/mathx"

	"github.com/helix-bedrock/worldgen/rand"
	"github.com/helix-bedrock/worldgen/structure"
)

// Jigsaw structures (villages for now), after BDS 1.26.52:
//   start      sub_D2B36D0  start piece, moved onto the ground at its centre
//   loop       sub_D2B3CB0  per piece: its jigsaw blocks shuffled, then each
//                           tried against its target pool, then the fallback
//   fit        sub_D2B4120  candidates from a weighted shuffle of the pool,
//                           4 rotations shuffled, candidate jigsaws shuffled
// Shuffles are BDS's forward one: for i = 1..n-1, j = rand % (i+1), swap.
// VERIFY: the y rules and the free-space test follow the structure of
// sub_D2B4120 and Java's JigsawPlacement; both are to be compared block by
// block with BDS villages.

// jigsawAssembly holds the state of one structure being assembled.
type jigsawAssembly struct {
	o        *Overworld
	r        *rand.MT
	height   func(x, z int32) int32
	maxDepth int
	pieces   []*structure.Piece
}

// assembleJigsaw builds a structure from a start pool at pos (y ignored:
// the start piece sits on the ground at its centre), as BDS does
// (sub_D2B36D0): depth first, each new piece's children built before its
// parent's next jigsaw is tried.
func (o *Overworld) assembleJigsaw(start *structure.Pool, pos [3]int32, rotation int, maxDepth int, r *rand.MT, height func(x, z int32) int32) []*structure.Piece {
	if start == nil || len(start.Entries) == 0 {
		return nil
	}
	// The start element is drawn from the weight-expanded pool
	// (sub_D27F680).
	all := start.Expanded()
	el := all[r.IntN(int32(len(all)))]
	tpl := o.elementTemplate(el)
	if tpl == nil {
		return nil
	}
	p := &structure.Piece{Element: el, Template: tpl, Pos: pos, Rotation: rotation}
	p.BB = structure.TemplateBox(tpl.Template, pos, rotation)
	cx, cz := (p.BB.X0+p.BB.X1)/2, (p.BB.Z0+p.BB.Z1)/2
	// Ground level delta 1: the bottom layer replaces the surface block.
	dy := height(cx, cz) - 1 - p.BB.Y0
	p.Pos[1] += dy
	p.BB = p.BB.Raise(dy)
	p.Ground = 1
	a := &jigsawAssembly{o: o, r: r, height: height, maxDepth: maxDepth, pieces: []*structure.Piece{p}}
	a.children(p)
	return a.pieces
}

// children tries each jigsaw block of piece p (sub_D2B3CB0): the pool,
// unless p is at the last depth, then its fallback if the pool placed
// nothing. A placed piece's own children are built at once.
func (a *jigsawAssembly) children(p *structure.Piece) {
	if p.Depth > a.maxDepth {
		return
	}
	js := p.PlacedJigsaws()
	structure.Shuffle(js, a.r)
	// inner are the pieces placed inside p's box, which only they and p
	// itself limit.
	var inner []structure.Box
	for _, j := range js {
		pool := a.o.jigsaw.Pools[j.Pool]
		if pool == nil || len(pool.Entries) == 0 {
			continue
		}
		fallback := a.o.jigsaw.Pools[pool.Fallback]
		if fallback != nil && len(fallback.Entries) == 0 {
			fallback = nil
		}
		if p.Depth != a.maxDepth && a.fit(p, j, pool, &inner) {
			continue
		}
		if fallback != nil {
			a.fit(p, j, fallback, &inner)
		}
	}
}

// overlaps2D reports whether two boxes share a column (touching counts):
// BDS tests village pieces in x and z only.
func overlaps2D(b, c structure.Box) bool {
	return b.X1 >= c.X0 && b.X0 <= c.X1 && b.Z1 >= c.Z0 && b.Z0 <= c.Z1
}

// fit tries the elements of pool at jigsaw j of p (sub_D2B4120) and
// places the first that fits, with its children.
func (a *jigsawAssembly) fit(p *structure.Piece, j structure.JigsawBlock, pool *structure.Pool, inner *[]structure.Box) bool {
	dir := structure.JigsawDir(j.Facing)
	target := [3]int32{j.Pos[0] + dir[0], j.Pos[1] + dir[1], j.Pos[2] + dir[2]}
	insideParent := p.BB.Contains(target)
	surface := int32(-1 << 30)
	for _, el := range pool.WeightedShuffle(a.r) {
		// An empty element places nothing and ends the search, as a
		// success: the fallback is not tried either.
		if el.Template == "" && el.Feature == "" {
			return true
		}
		tpl := a.o.elementTemplate(el)
		if tpl == nil {
			continue
		}
		for _, rot := range a.rotations() {
			cand := &structure.Piece{Element: el, Template: tpl, Rotation: rot}
			cjs := cand.PlacedJigsaws()
			structure.Shuffle(cjs, a.r)
			for _, cj := range cjs {
				if cj.Facing != structure.Opposite(j.Facing) || cj.Attachment != j.Attachment {
					continue
				}
				if (j.Facing == "up" || j.Facing == "down") && j.Aligned && villageTop(j, p.Rotation) != villageTop(cj, rot) {
					continue
				}
				off := [3]int32{target[0] - cj.Pos[0], target[1] - cj.Pos[1], target[2] - cj.Pos[2]}
				bb := structure.TemplateBox(tpl.Template, off, rot)
				// The new box's bottom: from the parent's bottom when
				// both pieces are rigid, else from the ground at the
				// parent's jigsaw minus one, each plus the jigsaw's
				// height in the parent and its vertical direction.
				base := p.BB.Y0
				if p.Element.TerrainMatching || el.TerrainMatching {
					if surface == -1<<30 {
						surface = a.height(j.Pos[0], j.Pos[2])
					}
					base = surface - 1
				}
				y := dir[1] + base + j.Pos[1] - cj.Pos[1] - p.BB.Y0
				dy := y - bb.Y0
				bb = bb.Raise(dy)
				if insideParent {
					if bb.X0 < p.BB.X0 || bb.X1 > p.BB.X1 || bb.Z0 < p.BB.Z0 || bb.Z1 > p.BB.Z1 {
						continue
					}
					clear := true
					for _, q := range *inner {
						if overlaps2D(bb, q) {
							clear = false
							break
						}
					}
					if !clear {
						continue
					}
					*inner = append(*inner, bb)
				} else {
					clear := true
					for _, q := range a.pieces {
						if overlaps2D(bb, q.BB) {
							clear = false
							break
						}
					}
					if !clear {
						continue
					}
				}
				cand.Pos = [3]int32{off[0], off[1] + dy, off[2]}
				cand.BB = bb
				cand.Depth = p.Depth + 1
				// The joining jigsaw's height in the piece is its ground.
				cand.Ground = cj.Pos[1]
				a.pieces = append(a.pieces, cand)
				a.children(cand)
				return true
			}
		}
	}
	return false
}

// villageTop is the top of a vertical jigsaw turned by rot: an old
// template's jigsaw has no top and keeps north before the turn.
func villageTop(j structure.JigsawBlock, rot int) string {
	if j.Top != "" {
		return j.Top
	}
	return structure.RotateFacing("north", rot)
}

// rotations are the four turns in BDS's order (sub_D2B3520): 1, 2, 3, 0
// with each of the last three swapped with a random earlier one.
func (a *jigsawAssembly) rotations() [4]int {
	r := [4]int{1, 2, 3, 0}
	if a.r.Uint32()&1 == 0 {
		r[0], r[1] = r[1], r[0]
	}
	if i := a.r.Uint32() % 3; i != 2 {
		r[2], r[i] = r[i], r[2]
	}
	if i := a.r.Uint32() & 3; i != 3 {
		r[3], r[i] = r[i], r[3]
	}
	return r
}

// village is an assembled village and the chunk range it covers.
type village struct {
	pieces []*structure.Piece
	bb     structure.Box
}

// villageAt assembles the village starting in chunk (cx, cz), if any. Its
// heights come from the terrain fill without structures, as BDS's come from
// the generator's height map before any structure adjusts the terrain
// (sub_D2ACD80); the village's own beards then reshape the terrain.
// VERIFY: whether BDS's height map includes adjustments of structures
// assembled earlier.
func (o *Overworld) villageAt(cx, cz int32) *village {
	if v, ok := o.villages.Load([2]int32{cx, cz}); ok {
		vv, _ := v.(*village)
		return vv
	}
	var vil *village
	if o.jigsaw != nil && o.IsVillageChunk(cx, cz) {
		var r rand.MT
		// The start is built with the region's random after the spread
		// draws (VERIFY: sub_4FBBAD0's caller).
		rx, rz := floorDivRegion(cx, villageSpacing), floorDivRegion(cz, villageSpacing)
		structureSeed(&r, o.seed, rx, rz, villageSalt)
		for range 4 {
			r.IntN(villageSpacing - villageSeparation)
		}
		rotation := int(r.IntN(4))
		abandoned := r.Float() < 0.02
		y := prelimSurface(o.column(cx*4, cz*4))
		kind := o.villageKind(cx, cz)
		name := "minecraft:village/" + kind + "/town_centers"
		if abandoned {
			name = "minecraft:village/abandoned/" + kind + "/town_centers"
		}
		pieces := o.assembleJigsaw(o.jigsaw.Pools[name], [3]int32{cx * 16, y, cz * 16}, rotation, 6, &r, o.surfaceNoStructures)
		if len(pieces) > 0 {
			vil = &village{pieces: pieces, bb: pieces[0].BB}
			for _, p := range pieces[1:] {
				vil.bb = structure.NewBox(min(vil.bb.X0, p.BB.X0), min(vil.bb.Y0, p.BB.Y0), min(vil.bb.Z0, p.BB.Z0),
					max(vil.bb.X1, p.BB.X1), max(vil.bb.Y1, p.BB.Y1), max(vil.bb.Z1, p.BB.Z1))

			}
		}
	}
	v, _ := o.villages.LoadOrStore([2]int32{cx, cz}, vil)
	vil, _ = v.(*village)
	return vil
}

// placeVillages writes the parts of nearby villages inside chunk (cx, cz)
// into c. height gives surface heights from the undecorated terrain.
func (o *Overworld) placeVillages(cx, cz int32, c *Chunk, height func(x, z int32) int32) {
	if o.jigsaw == nil {
		return
	}
	// Villages reach up to 80 blocks from their start chunk.
	r0x, r1x := floorDivRegion(cx-6, villageSpacing), floorDivRegion(cx+6, villageSpacing)
	r0z, r1z := floorDivRegion(cz-6, villageSpacing), floorDivRegion(cz+6, villageSpacing)
	chunkBox := structure.NewBox(cx*16, MinY, cz*16, cx*16+15, MaxY-1, cz*16+15)
	for rx := r0x; rx <= r1x; rx++ {
		for rz := r0z; rz <= r1z; rz++ {
			sx, sz, ok := o.villagePick(rx, rz)
			if !ok {
				continue
			}
			v := o.villageAt(sx, sz)
			if v == nil || !v.bb.Intersects(chunkBox) {
				continue
			}
			for _, p := range v.pieces {
				if p.Element.Feature == "" && p.BB.Intersects(chunkBox) {
					o.placePiece(p, c, chunkBox, height)
					if o.opts.houseChests {
						o.placeHouseChest(p, c, chunkBox)
					}
				}
			}
		}
	}
}

// villagePick is the chunk a region's village would start in.
func (o *Overworld) villagePick(rx, rz int32) (int32, int32, bool) {
	var r rand.MT
	structureSeed(&r, o.seed, rx, rz, villageSalt)
	n := int32(villageSpacing - villageSeparation)
	a := r.IntN(n)
	x := rx*villageSpacing + (a+r.IntN(n))/2
	b := r.IntN(n)
	z := rz*villageSpacing + (b+r.IntN(n))/2
	return x, z, true
}

// placePiece writes the blocks of a piece that fall in chunkBox into c.
func (o *Overworld) placePiece(p *structure.Piece, c *Chunk, chunkBox structure.Box, height func(x, z int32) int32) {
	o.st.PlacePiece(p, chunkWorld{o, c}, chunkBox, height)
}

// chunkWorld places into an overworld chunk.
type chunkWorld struct {
	o *Overworld
	c *Chunk
}

func (w chunkWorld) Bounds() (int32, int32) { return MinY, MaxY }

func (w chunkWorld) Block(x, y, z int32) uint32 {
	return uint32(w.c.Blocks[int(y-MinY)<<8|int(z&15)<<4|int(x&15)])
}

func (w chunkWorld) Set(x, y, z int32, id uint32, water bool) bool {
	i := int(y-MinY)<<8 | int(z&15)<<4 | int(x&15)
	if w.o.floorBedrock(uint32(w.c.Blocks[i]), y) {
		return false
	}
	w.c.Blocks[i] = uint16(id)
	if water {
		w.c.Water[i>>6] |= 1 << (i & 63)
	} else {
		w.c.Water[i>>6] &^= 1 << (i & 63)
	}
	return true
}

func (w chunkWorld) Entity(be structure.BlockEntity) {
	w.c.BlockEntities = append(w.c.BlockEntities, be)
}

// isDwelling reports whether a houses-pool template is a building one lives
// or works in: the pools also hold farms, animal pens, lamp posts and
// decorations, which get no extra chest.
func isDwelling(template string) bool {
	name := template[strings.LastIndex(template, "/")+1:]
	for _, s := range []string{"farm", "pen", "lamp", "decoration", "accessory", "meeting_point", "pond", "well"} {
		if strings.Contains(name, s) {
			return false
		}
	}
	return true
}

// placeHouseChest puts the WithHouseChests chest of a house piece into the
// chunk, if its spot is inside it.
func (o *Overworld) placeHouseChest(p *structure.Piece, c *Chunk, chunkBox structure.Box) {
	i := strings.Index(p.Element.Template, "/houses/")
	if i < 0 || !isDwelling(p.Element.Template) {
		return
	}
	if pick := o.opts.houseChestsPick; pick != nil && !pick(p.Element.Template) {
		return
	}
	spot := p.Template.HouseChestSpot()
	if !spot.OK {
		return
	}
	rp := structure.RotatePos(spot.Pos, p.Rotation)
	x, y, z := rp[0]+p.Pos[0], rp[1]+p.Pos[1], rp[2]+p.Pos[2]
	if !chunkBox.Contains([3]int32{x, y, z}) {
		return
	}
	cs, _, ok := o.st.ConvertJavaState(structure.TemplateState{Name: "minecraft:chest", Props: map[string]string{"facing": structure.RotateFacing(spot.Facing, p.Rotation)}})
	if !ok {
		return
	}
	idx := int(y-MinY)<<8 | int(z&15)<<4 | int(x&15)
	if o.floorBedrock(uint32(c.Blocks[idx]), y) {
		return
	}
	c.Blocks[idx] = uint16(cs)
	kind := strings.TrimPrefix(p.Element.Template[:i], "village/")
	d := o.st.BlockEntityFor(cs, "minecraft:chest", x, y, z)
	be := structure.BlockEntity{X: x, Y: y, Z: z, Data: d, LootTable: "loot_tables/chests/village/village_" + kind + "_house.json"}
	d["LootTable"] = be.LootTable
	d["LootTableSeed"] = int32(structure.PositionSeed(o.seed, x, y, z))
	c.BlockEntities = append(c.BlockEntities, be)
}

// surfaceHeight is the y above the highest non-air block of a column in the
// undecorated terrain (Java's WORLD_SURFACE_WG).
func (o *Overworld) surfaceHeight(terrain func(cx, cz int32) *PackedChunk, x, z int32) int32 {
	c := terrain(x>>4, z>>4)
	if c == nil {
		return SeaLevel + 1
	}
	lx, lz := int(x&15), int(z&15)
	for sub := len(c.subs) - 1; sub >= 0; sub-- {
		// Whole sub-chunks of air (the sky) are skipped at once.
		if sb := &c.subs[sub]; sb.idx == nil && sb.wide == nil && uint32(sb.one) == o.ids.air {
			continue
		}
		for ly := 15; ly >= 0; ly-- {
			if c.at((sub<<4+ly)<<8|lz<<4|lx) != o.ids.air {
				return int32(sub<<4+ly) + MinY + 1
			}
		}
	}
	return MinY
}

// placeStructures writes the structures that reach chunk (cx, cz) into c,
// in BDS's order: trail ruins and trial chambers, villages, mineshafts, strongholds, ancient cities, scattered
// features, pillager outposts, woodland mansions, ocean monuments, ocean ruins, shipwrecks,
// then buried treasure. raw gives undecorated terrain.
// VERIFY: the order of structure types (sub_C970F80 walks the registry).
func (o *Overworld) placeStructures(cx, cz int32, c *Chunk, height func(x, z int32) int32, raw func(cx, cz int32) *PackedChunk) {
	o.placeDataJigsaws(cx, cz, c)
	o.placeVillages(cx, cz, c, height)
	o.placeMineshafts(cx, cz, c, raw)
	o.placeStrongholds(cx, cz, c, raw)
	o.placeCities(cx, cz, c)
	o.placeScattered(cx, cz, c, raw)
	o.placeOutposts(cx, cz, c, raw)
	o.placeMansions(cx, cz, c, raw)
	o.placeMonuments(cx, cz, c)
	o.placeRuins(cx, cz, c, raw)
	o.placeShips(cx, cz, c, raw)
	o.placeTreasure(cx, cz, c, raw(cx, cz))
	o.placePortals(cx, cz, c, raw)
}

// structuresReach reports whether a structure has a piece in chunk
// (cx, cz).
func (o *Overworld) structuresReach(cx, cz int32, height func(x, z int32) int32, raw func(cx, cz int32) *PackedChunk) bool {
	return o.dataJigsawsReach(cx, cz) || o.villagesReach(cx, cz) || o.mineshaftReaches(cx, cz) || o.strongholdReaches(cx, cz) || o.citiesReach(cx, cz) ||
		o.scatteredReaches(cx, cz) || o.ruinReaches(cx, cz) ||
		o.shipReaches(cx, cz) || o.isTreasureChunk(cx, cz) || o.monumentReaches(cx, cz) ||
		o.outpostReaches(cx, cz) || o.portalReaches(cx, cz) || o.mansionReaches(cx, cz, raw)
}

// villagesReach reports whether a village has a piece in chunk (cx, cz).
func (o *Overworld) villagesReach(cx, cz int32) bool {
	if o.jigsaw == nil {
		return false
	}
	chunkBox := structure.NewBox(cx*16, MinY, cz*16, cx*16+15, MaxY-1, cz*16+15)
	r0x, r1x := floorDivRegion(cx-6, villageSpacing), floorDivRegion(cx+6, villageSpacing)
	r0z, r1z := floorDivRegion(cz-6, villageSpacing), floorDivRegion(cz+6, villageSpacing)
	for rx := r0x; rx <= r1x; rx++ {
		for rz := r0z; rz <= r1z; rz++ {
			sx, sz, _ := o.villagePick(rx, rz)
			if v := o.villageAt(sx, sz); v != nil && v.bb.Intersects(chunkBox) {
				for _, p := range v.pieces {
					if p.BB.Intersects(chunkBox) {
						return true
					}
				}
			}
		}
	}
	return false
}

// structuredView wraps a raw terrain source with the structures placed, for
// Decorate without a TerrainCache. It keeps what it builds for its own
// lifetime and is not safe for concurrent use.
func (o *Overworld) structuredView(raw func(cx, cz int32) *PackedChunk) func(cx, cz int32) *PackedChunk {
	done := map[[2]int32]*PackedChunk{}
	height := func(x, z int32) int32 { return o.surfaceHeight(raw, x, z) }
	return func(cx, cz int32) *PackedChunk {
		k := [2]int32{cx, cz}
		if p, ok := done[k]; ok {
			return p
		}
		p := raw(cx, cz)
		if p != nil && o.structuresReach(cx, cz, height, raw) {
			full := new(Chunk)
			p.Unpack(full)
			o.placeStructures(cx, cz, full, height, raw)
			p = Pack(full)
		}
		done[k] = p
		return p
	}
}

// villageKind is the pool name part of a village starting in chunk
// (cx, cz), from the start biome's village_type.
func (o *Overworld) villageKind(cx, cz int32) string {
	y := prelimSurface(o.column(cx*4, cz*4))
	biome := o.BiomeAt(cx*16+8, y, cz*16+8)
	if int(biome) >= len(o.villageType) {
		return "plains"
	}
	// BDS's village types (sub_D27F680): desert, ice (snowy pools),
	// savanna, taiga; anything else is plains.
	switch t := o.villageType[biome]; t {
	case "desert", "savanna", "taiga":
		return t
	case "ice":
		return "snowy"
	}
	return "plains"
}

// NearestVillage finds the village start nearest to block (x, z), searching
// placement regions ring by ring up to maxRings out. kind limits it to one
// village type ("plains", "desert", "savanna", "snowy", "taiga"); empty
// takes any. It returns the centre of the start chunk and its preliminary
// surface height.
func (o *Overworld) NearestVillage(x, z int32, kind string, maxRings int32) (vx, vy, vz int32, ok bool) {
	rx0, rz0 := floorDivRegion(x>>4, villageSpacing), floorDivRegion(z>>4, villageSpacing)
	best := int64(-1)
	for ring := int32(0); ring <= maxRings; ring++ {
		for rx := rx0 - ring; rx <= rx0+ring; rx++ {
			for rz := rz0 - ring; rz <= rz0+ring; rz++ {
				if max(mathx.Abs(rx-rx0), mathx.Abs(rz-rz0)) != ring {
					continue
				}
				cx, cz, _ := o.villagePick(rx, rz)
				if !o.IsVillageChunk(cx, cz) || (kind != "" && o.villageKind(cx, cz) != kind) {
					continue
				}
				bx, bz := cx*16+8, cz*16+8
				d := int64(bx-x)*int64(bx-x) + int64(bz-z)*int64(bz-z)
				if best < 0 || d < best {
					best, vx, vz = d, bx, bz
					vy = prelimSurface(o.column(cx*4, cz*4))
				}
			}
		}
		// A start one ring further out can still be nearer; stop after it.
		if best >= 0 && ring > 0 {
			return vx, vy, vz, true
		}
	}
	return vx, vy, vz, best >= 0
}

// villageFeatures places the feature pieces of the villages standing in
// origin chunk (ox, oz) (hay, melon and pumpkin piles, trees, berry bushes),
// in piece order, with the structure post-process random: a Mersenne
// Twister seeded with the chunk's population seed. A template piece before
// them draws one number per chest it places in the chunk, for the chest's
// loot seed (sub_D2B7A10), as dataJigsawFeatures does for camps.
// VERIFY: the order of villages against other structures in the chunk.
func (o *Overworld) villageFeatures(w *featureWorld, ox, oz int32, pop uint32) {
	if o.jigsaw == nil {
		return
	}
	chunkBox := structure.NewBox(ox*16, MinY, oz*16, ox*16+15, MaxY-1, oz*16+15)
	var r *rand.MT
	r0x, r1x := floorDivRegion(ox-6, villageSpacing), floorDivRegion(ox+6, villageSpacing)
	r0z, r1z := floorDivRegion(oz-6, villageSpacing), floorDivRegion(oz+6, villageSpacing)
	for rx := r0x; rx <= r1x; rx++ {
		for rz := r0z; rz <= r1z; rz++ {
			sx, sz, _ := o.villagePick(rx, rz)
			v := o.villageAt(sx, sz)
			if v == nil || !v.bb.Intersects(chunkBox) {
				continue
			}
			for _, p := range v.pieces {
				if p.Element.Feature == "" {
					if n := chestsInChunk(p, ox, oz); n > 0 {
						if r == nil {
							r = rand.NewMT(pop)
						}
						for range n {
							r.Uint32()
						}
					}
					continue
				}
				if p.Pos[0]>>4 != ox || p.Pos[2]>>4 != oz {
					continue
				}
				f, ok := o.features.get(p.Element.Feature)
				if !ok {
					continue
				}
				if r == nil {
					r = rand.NewMT(pop)
				}
				f.place(w, p.Pos[0], p.Pos[1], p.Pos[2], r)
			}
		}
	}
}

// PieceInfo describes one placed jigsaw piece, for tools.
type PieceInfo struct {
	Template string
	// Box is the inclusive box x0, y0, z0, x1, y1, z1.
	Box      [6]int32
	Rotation int
	Depth    int
}

// VillagePieces returns the pieces, in assembly order, of the village
// that starts in the region holding chunk (cx, cz), if any. It is meant
// for tools.
func (o *Overworld) VillagePieces(cx, cz int32) []PieceInfo {
	if o.jigsaw == nil {
		return nil
	}
	sx, sz, _ := o.villagePick(floorDivRegion(cx, villageSpacing), floorDivRegion(cz, villageSpacing))
	v := o.villageAt(sx, sz)
	if v == nil {
		return nil
	}
	out := make([]PieceInfo, 0, len(v.pieces))
	for _, p := range v.pieces {
		name := p.Element.Template
		if name == "" {
			name = "feature:" + p.Element.Feature
		}
		out = append(out, PieceInfo{Template: name, Box: [6]int32{p.BB.X0, p.BB.Y0, p.BB.Z0, p.BB.X1, p.BB.Y1, p.BB.Z1},
			Rotation: p.Rotation, Depth: p.Depth})
	}
	return out
}
