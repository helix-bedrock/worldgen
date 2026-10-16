package worldgen

import "github.com/helix-bedrock/worldgen/rand"

// Ancient cities, after BDS 1.26.52:
//   placement  sub_4E3E390  regions of 24 chunks with picks 8 apart (two
//                           draws averaged per axis), deep dark at
//                           (16cx, -27, 16cz)
//   start      sub_D2AAD50  rotation, then a city_center element; its
//                           minecraft:city_anchor jigsaw goes to
//                           (16cx, -28, 16cz)
//   assembly   sub_D2AB0E0  newer than the village one: jigsaws expanded
//                           first in, first out, 7 deep, within 128 blocks
//   expand     sub_D2B1720  jigsaws shuffled; the pool, or at the last
//                           depth only its fallback, tried
//   fit        sub_D2B1D00  free-space boxes kept per nesting level
//   rules      sub_D1BE660  block rules with a Java random per block
// The start draws continue the placement random, as for villages (VERIFY).

const (
	citySalt       = 20083232
	citySpacing    = 24
	citySeparation = 8
	cityMaxDepth   = 7
	cityMaxDist    = 128
	// cityAnchorY is where the start piece's anchor jigsaw goes; the
	// pieces' fixed height (vfunc 112 of AncientCityPiece) is one above.
	cityAnchorY = -28
	// cityReach is how many chunks a city reaches from its start chunk:
	// the free space is 128 blocks around the start piece's centre.
	cityReach = 12
)

// city is an assembled ancient city.
type city struct {
	pieces []*jigsawPiece
	bb     box
}

// cityPick is the chunk a region's city would start in, and the random
// after the pick, which the start continues.
func (o *Overworld) cityPick(rx, rz int32, r *rand.MT) (int32, int32) {
	structureSeed(r, o.seed, rx, rz, citySalt)
	n := int32(citySpacing - citySeparation)
	a := r.IntN(n)
	x := rx*citySpacing + (a+r.IntN(n))/2
	b := r.IntN(n)
	z := rz*citySpacing + (b+r.IntN(n))/2
	return x, z
}

// IsAncientCityChunk reports whether an ancient city starts in chunk
// (cx, cz).
func (o *Overworld) IsAncientCityChunk(cx, cz int32) bool {
	var r rand.MT
	px, pz := o.cityPick(floorDivRegion(cx, citySpacing), floorDivRegion(cz, citySpacing), &r)
	return px == cx && pz == cz && o.BiomeAt(cx*16, cityAnchorY+1, cz*16) == o.deepDark
}

// cityAt assembles the city of region (rx, rz), nil if it has none.
func (o *Overworld) cityAt(rx, rz int32) *city {
	key := [2]int32{rx, rz}
	if v, ok := o.cities.Load(key); ok {
		c, _ := v.(*city)
		return c
	}
	var c *city
	var r rand.MT
	cx, cz := o.cityPick(rx, rz, &r)
	if o.city != nil && o.BiomeAt(cx*16, cityAnchorY+1, cz*16) == o.deepDark {
		c = o.assembleCity(cx, cz, &r)
	}
	v, _ := o.cities.LoadOrStore(key, c)
	c, _ = v.(*city)
	return c
}

// cityShape is the free space of one nesting level: a piece must lie inside
// one of in and touch none of out.
type cityShape struct {
	in, out []box
}

func (s *cityShape) fits(bb box) bool {
	inside := false
	for _, b := range s.in {
		if bb.inside(b) {
			inside = true
			break
		}
	}
	if !inside {
		return false
	}
	for _, b := range s.out {
		if bb.intersects(b) {
			return false
		}
	}
	return true
}

// cityQueued is a placed piece waiting to have its jigsaws expanded, with
// the nesting level it was placed in.
type cityQueued struct {
	p     *jigsawPiece
	shape int
}

type cityAssembly struct {
	o       *Overworld
	r       *rand.MT
	shapes  []cityShape
	pieces  []*jigsawPiece
	queue   []cityQueued
	feature *jigsawTemplate
}

// assembleCity builds the city starting in chunk (cx, cz).
func (o *Overworld) assembleCity(cx, cz int32, r *rand.MT) *city {
	rot := int(r.IntN(4))
	pool := o.city.pools["minecraft:ancient_city/city_center"]
	if pool == nil || len(pool.entries) == 0 {
		return nil
	}
	el := pool.entries[r.IntN(int32(len(pool.entries)))].el
	tpl := o.template(el.template)
	if tpl == nil {
		return nil
	}
	at := [3]int32{cx * 16, 0, cz * 16}
	p := &jigsawPiece{el: el, tpl: tpl, pos: at, rotation: rot}
	js := p.placedJigsaws()
	shuffle(js, r)
	// BDS logs and uses no offset when the anchor is missing.
	var off [3]int32
	for _, j := range js {
		if j.name == "minecraft:city_anchor" {
			off = [3]int32{j.pos[0] - at[0], j.pos[1] - at[1], j.pos[2] - at[2]}
			break
		}
	}
	p.pos = [3]int32{at[0] - off[0], 0, at[2] - off[2]}
	p.bb = tplBox(tpl.tpl, p.pos, rot)
	// The free space is centred on the start piece before it is lowered.
	mx := p.bb.x0 + (p.bb.x1-p.bb.x0+1)/2
	my := p.bb.y0 + (p.bb.y1-p.bb.y0+1)/2
	mz := p.bb.z0 + (p.bb.z1-p.bb.z0+1)/2
	bounds := box{mx - cityMaxDist, my - cityMaxDist, mz - cityMaxDist, mx + cityMaxDist, my + cityMaxDist, mz + cityMaxDist}
	dy := cityAnchorY - (p.bb.y0 + off[1])
	p.pos[1] += dy
	p.bb = p.bb.raise(dy)
	p.ground = 1

	a := &cityAssembly{o: o, r: r, pieces: []*jigsawPiece{p},
		shapes: []cityShape{{in: []box{bounds}, out: []box{p.bb}}},
		queue:  []cityQueued{{p, 0}},
		feature: &jigsawTemplate{tpl: &Template{Size: [3]int32{1, 1, 1}},
			jigsaws: []jigsawBlock{{facing: "down", name: "minecraft:bottom", target: "minecraft:empty", pool: "minecraft:empty"}}},
	}
	for i := 0; i < len(a.queue); i++ {
		a.expand(a.queue[i])
	}
	c := &city{pieces: a.pieces, bb: p.bb}
	for _, q := range a.pieces[1:] {
		c.bb = box{min(c.bb.x0, q.bb.x0), min(c.bb.y0, q.bb.y0), min(c.bb.z0, q.bb.z0),
			max(c.bb.x1, q.bb.x1), max(c.bb.y1, q.bb.y1), max(c.bb.z1, q.bb.z1)}
	}
	return c
}

// expand tries every jigsaw of a queued piece.
func (a *cityAssembly) expand(q cityQueued) {
	if q.p.depth > cityMaxDepth {
		return
	}
	js := q.p.placedJigsaws()
	shuffle(js, a.r)
	// BDS then sorts by selection priority; city jigsaws have none.
	for _, j := range js {
		pool := a.o.city.pools[j.pool]
		if pool == nil || len(pool.entries) == 0 {
			continue
		}
		fallback := a.o.city.pools[pool.fallback]
		if fallback != nil && len(fallback.entries) == 0 {
			fallback = nil
		}
		if q.p.depth != cityMaxDepth && a.fit(q, j, pool) {
			continue
		}
		if fallback != nil {
			a.fit(q, j, fallback)
		}
	}
}

// template is an element's prepared template; feature elements are a
// single block with one jigsaw facing down.
func (a *cityAssembly) template(el *jigsawElement) *jigsawTemplate {
	if el.feature != "" {
		return a.feature
	}
	return a.o.template(el.template)
}

// fit attaches the first element of pool that fits to jigsaw j of the
// queued piece, and reports whether one did.
func (a *cityAssembly) fit(q cityQueued, j jigsawBlock, pool *jigsawPool) bool {
	cands := pool.weightedShuffle(a.r)
	dir := jigsawDir(j.facing)
	target := [3]int32{j.pos[0] + dir[0], j.pos[1] + dir[1], j.pos[2] + dir[2]}
	inside := q.p.bb.contains(target)
	want := opposite(j.facing)
	for _, el := range cands {
		tpl := a.template(el)
		if tpl == nil {
			// BDS stops at an empty element.
			return true
		}
		rots := []int{0, 1, 2, 3}
		shuffle(rots, a.r)
		for _, rot := range rots {
			cand := &jigsawPiece{el: el, tpl: tpl, rotation: rot}
			cjs := cand.placedJigsaws()
			shuffle(cjs, a.r)
			for _, cj := range cjs {
				// City joints are all rollable: only the fronts must face.
				if cj.facing != want || cj.name != j.target {
					continue
				}
				// A piece joined inside its parent goes into the next
				// nesting level, whose space grows by the parent's box.
				si := q.shape
				if inside {
					si++
					if si < len(a.shapes) {
						a.shapes[si].in = append(a.shapes[si].in, q.p.bb)
					} else {
						a.shapes = append(a.shapes, cityShape{in: []box{q.p.bb}})
					}
				}
				pos := [3]int32{target[0] - cj.pos[0], target[1] - cj.pos[1], target[2] - cj.pos[2]}
				bb := tplBox(tpl.tpl, pos, rot)
				if !a.shapes[si].fits(bb) {
					continue
				}
				a.shapes[si].out = append(a.shapes[si].out, bb)
				cand.pos, cand.bb, cand.depth = pos, bb, q.p.depth+1
				// The joining jigsaw's height in the piece is its ground.
				cand.ground = cj.pos[1]
				a.pieces = append(a.pieces, cand)
				a.queue = append(a.queue, cityQueued{cand, si})
				return true
			}
		}
	}
	return false
}

// applyRules runs a template's block rules on one block about to be placed
// at (x, y, z) over world: the first rule whose block matches, whose chance
// passes and whose spot is not protected decides the block. The random is
// fresh per block and drawn only for matching rules.
func (o *Overworld) applyRules(rules []blockRule, cs convertedState, x, y, z int32, world uint32) convertedState {
	st, ok := o.reg.State(cs.id)
	if !ok {
		return cs
	}
	var r rand.Java
	seeded := false
	for _, ru := range rules {
		if ru.set == nil && ru.block != st.Name || ru.set != nil && !ru.set[st.Name] {
			continue
		}
		pass := ru.chance >= 1
		if !pass && (ru.set != nil || ru.chance > 0) {
			if !seeded {
				r.Seed(rand.BlockSeed(x, y, z))
				seeded = true
			}
			f := r.Float()
			// A single block passes on chance > f, a set on chance >= f.
			pass = ru.chance > f || ru.set != nil && ru.chance == f
		}
		if !pass {
			continue
		}
		// VERIFY: the location test's own chance is taken as 1.
		if ws, ok := o.reg.State(world); ok && ru.notIn[ws.Name] {
			continue
		}
		return convertedState{ru.output, false, true}
	}
	return cs
}

// placeCities writes the pieces of ancient cities that reach chunk
// (cx, cz) into c.
// TODO: the sculk patches of the sculk elements (sculk_patch_feature_
// ancient_city) and the beard_box terrain adaptation around the pieces.
func (o *Overworld) placeCities(cx, cz int32, c *Chunk) {
	chunkBox := box{cx * 16, MinY, cz * 16, cx*16 + 15, MaxY - 1, cz*16 + 15}
	o.forCityPieces(cx, cz, func(p *jigsawPiece) bool {
		if p.el.feature == "" {
			o.placePiece(p, c, chunkBox, nil)
		}
		return true
	})
}

// citiesReach reports whether an ancient city has a piece in chunk
// (cx, cz).
func (o *Overworld) citiesReach(cx, cz int32) bool {
	found := false
	o.forCityPieces(cx, cz, func(*jigsawPiece) bool {
		found = true
		return false
	})
	return found
}

// forCityPieces calls f for every city piece in chunk (cx, cz), in
// placement order, until f returns false.
func (o *Overworld) forCityPieces(cx, cz int32, f func(*jigsawPiece) bool) {
	if o.city == nil {
		return
	}
	chunkBox := box{cx * 16, MinY, cz * 16, cx*16 + 15, MaxY - 1, cz*16 + 15}
	r0x, r1x := floorDivRegion(cx-cityReach, citySpacing), floorDivRegion(cx+cityReach, citySpacing)
	r0z, r1z := floorDivRegion(cz-cityReach, citySpacing), floorDivRegion(cz+cityReach, citySpacing)
	for rx := r0x; rx <= r1x; rx++ {
		for rz := r0z; rz <= r1z; rz++ {
			c := o.cityAt(rx, rz)
			if c == nil || !c.bb.intersects(chunkBox) {
				continue
			}
			for _, p := range c.pieces {
				if p.bb.intersects(chunkBox) && !f(p) {
					return
				}
			}
		}
	}
}

// AncientCity is where an ancient city is.
type AncientCity struct {
	// X, Y, Z is the start piece's anchor, on the floor of the city centre.
	X, Y, Z int32
	// Min and Max are the corners of the box around every piece.
	Min, Max [3]int32
}

// NearestAncientCity finds the ancient city whose start is nearest to block
// (x, z), searching placement regions ring by ring up to maxRings out. It
// is safe for concurrent use.
func (o *Overworld) NearestAncientCity(x, z, maxRings int32) (AncientCity, bool) {
	rx0, rz0 := floorDivRegion(x>>4, citySpacing), floorDivRegion(z>>4, citySpacing)
	var r rand.MT
	best, found := int64(-1), [2]int32{}
	for ring := int32(0); ring <= maxRings; ring++ {
		for rx := rx0 - ring; rx <= rx0+ring; rx++ {
			for rz := rz0 - ring; rz <= rz0+ring; rz++ {
				if max(abs32i(rx-rx0), abs32i(rz-rz0)) != ring {
					continue
				}
				cx, cz := o.cityPick(rx, rz, &r)
				if o.BiomeAt(cx*16, cityAnchorY+1, cz*16) != o.deepDark {
					continue
				}
				dx, dz := int64(cx*16-x), int64(cz*16-z)
				if d := dx*dx + dz*dz; best < 0 || d < best {
					best, found = d, [2]int32{rx, rz}
				}
			}
		}
		// A start one ring further out can still be nearer; stop after it.
		if best >= 0 && ring > 0 {
			break
		}
	}
	if best < 0 {
		return AncientCity{}, false
	}
	c := o.cityAt(found[0], found[1])
	if c == nil {
		return AncientCity{}, false
	}
	var r2 rand.MT
	cx, cz := o.cityPick(found[0], found[1], &r2)
	return AncientCity{X: cx * 16, Y: cityAnchorY + 1, Z: cz * 16,
		Min: [3]int32{c.bb.x0, c.bb.y0, c.bb.z0}, Max: [3]int32{c.bb.x1, c.bb.y1, c.bb.z1}}, true
}
