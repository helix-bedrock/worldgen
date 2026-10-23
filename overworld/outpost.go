package overworld

import (
	"slices"

	"github.com/helix-bedrock/worldgen/rand"
	"github.com/helix-bedrock/worldgen/structure"
)

// Pillager outposts, written from BDS 1.26.52: PillagerOutpostFeature
// (constructor sub_4E82040, chunk test sub_4E83110, start sub_4E83390) and
// PillagerOutpostPiece (constructor sub_4E84580, postProcess sub_4E84CE0,
// markers sub_4E85170). Bedrock builds outposts from template pieces, not
// jigsaw pools:
//   - the watchtower at the start chunk's corner, turned a random quarter,
//     with its overgrown overlay at integrity 0.05;
//   - four feature spots around it, each kept 9 times in 10, with its own
//     turn and one of seven templates.
//
// Bedrock differs from Java: there is no check for nearby villages, and a
// piece's height comes from five height-map samples (middle and the four
// edge middles), the lowest raised to sea level: the tower sits between
// the highest and lowest (or 4 under the highest when they are more than 8
// apart), a feature at its middle and only when they are within 8.
// No pillagers, captain, iron golems or allays are spawned; the allay
// marker still draws its count.
// VERIFY: heights in BDS come from the live world of the first chunk that
// builds the piece; here from the undecorated terrain.

// Pillager outpost placement (sub_4E83110): triangular spread.
const (
	outpostSalt       = 165745296
	outpostSpacing    = 80
	outpostSeparation = 24
)

// outpostBiomes are the biomes an outpost starts in (sub_4E82040).
func outpostBiomes() []string {
	return []string{"plains", "sunflower_plains", "savanna", "ice_plains", "taiga", "taiga_hills",
		"cold_taiga", "cold_taiga_hills", "desert", "meadow", "frozen_peaks", "jagged_peaks", "stony_peaks",
		"snowy_slopes", "cherry_grove", "grove"}
}

// outpostFeatures are the feature templates in BDS's table order
// (off_EE8E5B8).
func outpostFeatures() [7]string {
	return [...]string{"feature_cage1", "feature_cage2", "feature_cage_with_allays", "feature_logs",
		"feature_targets", "feature_tent1", "feature_tent2"}
}

// Pillager outpost layout (sub_4E83390, sub_4E84CE0).
const (
	// outpostSpreadDraws is how many draws the region spread made before
	// the start continues the same random.
	outpostSpreadDraws = 4
	// outpostOvergrownIntegrity is the overgrown overlay's integrity.
	outpostOvergrownIntegrity = 0.05
	// outpostSpotInset is how far inside the turned tower's corner the
	// feature spots are measured from.
	outpostSpotInset = 8
	// outpostSpotNear and outpostSpotFar are a feature spot's offset on an
	// axis from that point, toward the high or the low side.
	outpostSpotNear = 12
	outpostSpotFar  = -20
	// outpostSpotJitter bounds the random shift of each spot on each axis.
	outpostSpotJitter = 8
	// outpostFeatureChance is the chance a feature spot is kept.
	outpostFeatureChance = 0.9
	// outpostProbeY is the placeholder height BDS checks for liquid under
	// a feature's samples, plus one.
	outpostProbeY = 90
	// outpostFeatureSpread is the most the samples under a feature may
	// differ.
	outpostFeatureSpread = 8
	// outpostTowerSpread is the sample spread from which the tower stops
	// sitting midway and sits outpostTowerDrop under the highest sample.
	outpostTowerSpread = 9
	outpostTowerDrop   = 4
	// outpostAllayDraws is the bound the allay marker draws its count with.
	outpostAllayDraws = 3
	// outpostReachChunks is how far, in chunks, an outpost's pieces reach
	// from its start.
	outpostReachChunks = 3
)

// outpostPiece is one template of an outpost.
type outpostPiece struct {
	path      string
	x, z      int32
	rot       int
	integrity float32
	feature   bool
}

// outpostPick is the chunk an outpost region picks.
func (o *Overworld) outpostPick(rx, rz int32) (int32, int32) {
	var r rand.MT
	structureSeed(&r, o.seed, rx, rz, outpostSalt)
	n := int32(outpostSpacing - outpostSeparation)
	a := r.IntN(n)
	x := rx*outpostSpacing + (a+r.IntN(n))/2
	b := r.IntN(n)
	return x, rz*outpostSpacing + (b+r.IntN(n))/2
}

// isOutpostChunk reports whether a pillager outpost starts in chunk
// (cx, cz).
func (o *Overworld) isOutpostChunk(cx, cz int32) bool {
	x, z := o.outpostPick(structure.Region(cx, outpostSpacing), structure.Region(cz, outpostSpacing))
	if x != cx || z != cz {
		return false
	}
	b := o.BiomeAt(cx*16+8, o.structureY(cx, cz, noSurfaceY), cz*16+8)
	return int(b) < len(o.outpostBiome) && o.outpostBiome[b]
}

// outpostAt returns the pieces of the outpost starting in chunk (cx, cz),
// nil if none does (sub_4E83390). It is computed once per start.
func (o *Overworld) outpostAt(cx, cz int32) []outpostPiece {
	k := [2]int32{cx, cz}
	if v, ok := o.outposts.Load(k); ok {
		p, _ := v.([]outpostPiece)
		return p
	}
	var pieces []outpostPiece
	if o.isOutpostChunk(cx, cz) {
		// The start continues the region's random after the spread draws,
		// as villages do (the rotation matches BDS's outposts).
		var r rand.MT
		structureSeed(&r, o.seed, structure.Region(cx, outpostSpacing), structure.Region(cz, outpostSpacing), outpostSalt)
		for range outpostSpreadDraws {
			r.IntN(outpostSpacing - outpostSeparation)
		}
		rot := int(r.IntN(structRotations))
		x, z := cx*16, cz*16
		pieces = append(pieces,
			outpostPiece{"pillageroutpost/watchtower", x, z, rot, 1, false},
			outpostPiece{"pillageroutpost/watchtower_overgrown", x, z, rot, outpostOvergrownIntegrity, false})
		// The spots are measured from inside the turned tower's corner.
		mx, mz := min(x, x+ruinMainX()[rot])+outpostSpotInset, min(z, z+ruinMainZ()[rot])+outpostSpotInset
		var spots [4][2]int32
		for i, s := range [4][2]int32{{outpostSpotNear, outpostSpotNear}, {outpostSpotFar, outpostSpotNear},
			{outpostSpotNear, outpostSpotFar}, {outpostSpotFar, outpostSpotFar}} {
			a := r.IntN(outpostSpotJitter)
			b := r.IntN(outpostSpotJitter)
			spots[i] = [2]int32{a + s[0], b + s[1]}
		}
		for _, s := range spots {
			if r.Float() >= outpostFeatureChance {
				continue
			}
			frot := int(r.IntN(structRotations))
			feats := outpostFeatures()
			pieces = append(pieces, outpostPiece{"pillageroutpost/" + feats[r.IntN(int32(len(feats)))], mx + s[0], mz + s[1], frot, 1, true})
		}
	}
	o.outposts.Store(k, pieces)
	return pieces
}

// outpostBox is the area a piece covers.
func (o *Overworld) outpostBox(p outpostPiece) (structure.Box, *structure.JigsawTemplate) {
	jt := o.st.Template(p.path)
	if jt == nil {
		return structure.Box{}, nil
	}
	e := structure.RotatePos([3]int32{jt.Template.Size[0] - 1, 0, jt.Template.Size[2] - 1}, p.rot)
	return structure.NewBox(min(p.x, p.x+e[0]), MinY, min(p.z, p.z+e[2]), max(p.x, p.x+e[0]), MaxY-1, max(p.z, p.z+e[2])), jt
}

// outpostsReach calls f for each outpost with a piece in chunk (cx, cz).
// Pieces reach at most 3 chunks from the start.
func (o *Overworld) outpostsReach(cx, cz int32, f func(pieces []outpostPiece)) {
	chunk := structure.NewBox(cx*16, MinY, cz*16, cx*16+15, MaxY-1, cz*16+15)
	for rx := structure.Region(cx-outpostReachChunks, outpostSpacing); rx <= structure.Region(cx+outpostReachChunks, outpostSpacing); rx++ {
		for rz := structure.Region(cz-outpostReachChunks, outpostSpacing); rz <= structure.Region(cz+outpostReachChunks, outpostSpacing); rz++ {
			sx, sz := o.outpostPick(rx, rz)
			if sx < cx-outpostReachChunks || sx > cx+outpostReachChunks || sz < cz-outpostReachChunks || sz > cz+outpostReachChunks {
				continue
			}
			pieces := o.outpostAt(sx, sz)
			for _, p := range pieces {
				if bb, jt := o.outpostBox(p); jt != nil && bb.Intersects(chunk) {
					f(pieces)
					break
				}
			}
		}
	}
}

// outpostReaches reports whether an outpost has a piece in chunk (cx, cz).
func (o *Overworld) outpostReaches(cx, cz int32) bool {
	found := false
	o.outpostsReach(cx, cz, func([]outpostPiece) { found = true })
	return found
}

// outpostY is a piece's bottom y, ok false when a feature is not placed
// (sub_4E84CE0).
func (o *Overworld) outpostY(p outpostPiece, bb structure.Box, raw func(cx, cz int32) *PackedChunk) (int32, bool) {
	hw, hd := (bb.X1-bb.X0+1)/2, (bb.Z1-bb.Z0+1)/2
	samples := [5][2]int32{
		{bb.X0 + hw, bb.Z0 + hd}, {bb.X0 + 2*hw, bb.Z0 + hd}, {bb.X0 + hw, bb.Z0 + 2*hd},
		{bb.X0, bb.Z0 + hd}, {bb.X0 + hw, bb.Z0},
	}
	var hs [5]int32
	lo, hi := int32(MaxY), int32(MinY)
	for i, s := range samples {
		hs[i] = o.opaqueTop(raw, s[0], s[1])
		lo, hi = min(lo, hs[i]), max(hi, hs[i])
	}
	lo = max(lo, SeaLevel)
	if p.feature {
		// BDS looks under the samples at its placeholder height.
		for _, s := range samples {
			if o.liquidMaterial(o.rawBlock(raw, s[0], outpostProbeY-1, s[1])) {
				return 0, false
			}
		}
		if hi-lo > outpostFeatureSpread {
			return 0, false
		}
		return hs[0], true
	}
	if hi-lo < outpostTowerSpread {
		return (hi + lo) / 2, true
	}
	return hi - outpostTowerDrop, true
}

// placeOutposts writes the outposts that reach chunk (cx, cz) into c. raw
// gives undecorated terrain.
func (o *Overworld) placeOutposts(cx, cz int32, c *Chunk, raw func(cx, cz int32) *PackedChunk) {
	var b *scBuild
	o.outpostsReach(cx, cz, func(pieces []outpostPiece) {
		if b == nil {
			b = &scBuild{o: o, c: c, cbox: structure.NewBox(cx*16, MinY, cz*16, cx*16+15, MaxY-1, cz*16+15)}
			b.ids()
			// The chunk's random rolls the allays and the chest seed.
			b.r.Seed(populationSeed(o.seed, cx, cz))
		}
		for _, p := range pieces {
			b.outpostPiece(p, raw)
		}
	})
}

// outpostPiece places one outpost template into the chunk, then handles
// its markers.
func (b *scBuild) outpostPiece(p outpostPiece, raw func(cx, cz int32) *PackedChunk) {
	o := b.o
	bb, jt := o.outpostBox(p)
	if jt == nil || !bb.Intersects(b.cbox) {
		return
	}
	y0, ok := o.outpostY(p, bb, raw)
	if !ok {
		return
	}
	conv := o.st.TemplateStates(jt, p.rot)
	var ir rand.MT
	ir.Seed(populationSeed(o.seed, b.cbox.X0>>4, b.cbox.Z0>>4))
	var markers []templateMarker
	for _, tb := range jt.Template.Blocks {
		st := jt.Template.Palette[tb.State]
		rp := structure.RotatePos(tb.Pos, p.rot)
		pos := [3]int32{p.x + rp[0], y0 + rp[1], p.z + rp[2]}
		switch st.Name {
		case blockStructureBlock:
			if m, _ := tb.NBT["metadata"].(string); m != "" {
				markers = append(markers, templateMarker{pos, m})
			}
			continue
		case blockJigsaw, blockStructureVoid:
			continue
		}
		if p.integrity < 1 && float64(p.integrity) < float64(ir.Uint32())*0x1p-32 {
			continue
		}
		cs := conv[tb.State]
		if !cs.OK || !b.cbox.Contains(pos) || !b.setWorld(pos[0], pos[1], pos[2], cs.ID) {
			continue
		}
		b.dropEntity(pos)
		if cs.Water && o.holdsLiquid[o.blockName(cs.ID)] {
			i := (pos[1]-MinY)<<8 | (pos[2]&15)<<4 | pos[0]&15
			b.c.Water[i>>waterWordShift] |= 1 << (i & waterBitMask)
		}
		if d := o.st.BlockEntityFor(cs.ID, st.Name, pos[0], pos[1], pos[2]); d != nil {
			b.c.BlockEntities = append(b.c.BlockEntities, structure.BlockEntity{X: pos[0], Y: pos[1], Z: pos[2], Data: d})
		}
	}
	for _, m := range markers {
		if !b.cbox.Contains(m.pos) {
			continue
		}
		switch m.name {
		case "cage", "pillager", "captain":
			if b.setWorld(m.pos[0], m.pos[1], m.pos[2], b.air) {
				b.dropEntity(m.pos)
			}
		case "Group of Allays":
			if b.setWorld(m.pos[0], m.pos[1], m.pos[2], b.air) {
				b.dropEntity(m.pos)
			}
			b.r.IntN(outpostAllayDraws)
		case "topChest":
			cp := [3]int32{m.pos[0], m.pos[1] - 1, m.pos[2]}
			for i := range b.c.BlockEntities {
				be := &b.c.BlockEntities[i]
				if be.X == cp[0] && be.Y == cp[1] && be.Z == cp[2] && o.blockName(b.c.Block(int(cp[0]&15), int(cp[1]), int(cp[2]&15))) == blockChest {
					be.LootTable = "loot_tables/chests/pillager_outpost.json"
					be.Data["LootTable"] = be.LootTable
					be.Data["LootTableSeed"] = int32(b.r.Uint32() >> 1)
				}
			}
		}
	}
}

// NearestOutpost finds the pillager outpost nearest to block (x, z),
// searching maxRegions placement regions (80 chunks each) around it, and
// returns its watchtower's corner and bottom. It is safe for concurrent
// use.
func (o *Overworld) NearestOutpost(x, z int32, maxRegions int32) (Scattered, bool) {
	var cands []locateCand
	rx0, rz0 := structure.Region(x>>4, outpostSpacing), structure.Region(z>>4, outpostSpacing)
	for rx := rx0 - maxRegions; rx <= rx0+maxRegions; rx++ {
		for rz := rz0 - maxRegions; rz <= rz0+maxRegions; rz++ {
			cx, cz := o.outpostPick(rx, rz)
			dx, dz := int64(cx*16+8-x), int64(cz*16+8-z)
			cands = append(cands, locateCand{cx, cz, dx*dx + dz*dz})
		}
	}
	slices.SortFunc(cands, compareLocateCand)
	raw := newRawChunks(o)
	for _, c := range cands {
		pieces := o.outpostAt(c.cx, c.cz)
		if len(pieces) == 0 {
			continue
		}
		bb, jt := o.outpostBox(pieces[0])
		if jt == nil {
			continue
		}
		y, _ := o.outpostY(pieces[0], bb, raw.get)
		return Scattered{Kind: "pillager_outpost", X: (bb.X0 + bb.X1) / 2, Y: y, Z: (bb.Z0 + bb.Z1) / 2}, true
	}
	return Scattered{}, false
}

// rawBlock is the block at (x, y, z) of the terrain raw gives; air where
// raw has no chunk.
func (o *Overworld) rawBlock(raw func(cx, cz int32) *PackedChunk, x, y, z int32) uint32 {
	c := raw(x>>4, z>>4)
	if c == nil {
		return o.ids.air
	}
	return c.at(int(y-MinY)<<8 | int(z&15)<<4 | int(x&15))
}

// opaqueTop is one above the highest block of column (x, z) that is not air
// and blocks all light, or MinY.
func (o *Overworld) opaqueTop(raw func(cx, cz int32) *PackedChunk, x, z int32) int32 {
	for y := int32(MaxY - 1); y >= MinY; y-- {
		if s := o.rawBlock(raw, x, y, z); s != o.ids.air && o.reg.LightDampening(s) >= fullLightDampening {
			return y + 1
		}
	}
	return MinY
}

// fullLightDampening is the light dampening of a block no light passes.
const fullLightDampening = 15
