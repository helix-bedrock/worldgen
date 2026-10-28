package overworld

import (
	"github.com/helix-bedrock/worldgen/1.26.60/internal/mathx"
	"github.com/helix-bedrock/worldgen/1.26.60/rand"
)

// Poplar and fallen tree parts of minecraft:tree_feature (dappled forest's
// poplars, fallen logs in forests). Written from BDS 1.26.52:
// PoplarTreeTrunk sub_D2DB8C0, PoplarTreeCanopy sub_D2DAED0 with its layer
// helper sub_D2DB650, FallenTreeTrunk sub_D2D39F0.

// drawInclusive is the range draw these parts use: min + nextInt(max-min+1)
// when max > min (unlike intRange.draw, max is included).
func (r intRange) drawInclusive(rnd *rand.MT) int32 {
	if r.max > r.min {
		return r.min + rnd.IntN(r.max-r.min+1)
	}
	return r.min
}

// facingStep is the unit step of a horizontal facing (2 north, 3 south,
// 4 west, 5 east).
func facingStep(f uint8) (dx, dz int32) {
	switch f {
	case 2:
		return 0, -1
	case 3:
		return 0, 1
	case 4:
		return -1, 0
	case 5:
		return 1, 0
	}
	return 0, 0
}

// withAxis returns a log state with pillar_axis set, or the state itself
// when the block has no such axis.
func (o *Overworld) withAxis(b uint32, axis string) uint32 {
	s, ok := o.reg.State(b)
	if !ok {
		return b
	}
	if _, ok := s.Props["pillar_axis"]; !ok {
		return b
	}
	if id, ok := o.reg.Lookup(s.Name, map[string]any{"pillar_axis": axis}); ok {
		return id
	}
	return b
}

// poplarTrunk is PoplarTreeTrunk::place: a straight trunk of height h, the
// log decoration feature at every log, up to four side logs (branches)
// facing shuffled directions just below the canopy, and the canopy around
// the top `remaining` logs.
func (t *treeFeature) poplarTrunk(w *featureWorld, x, y, z, h int32, r *rand.MT) (blockPos, bool) {
	if !t.valid(w, x, y, z, h) || y >= MaxY-h-1 {
		return blockPos{}, false
	}
	t.placeBase(w, x, y-1, z)
	remaining := t.remaining.drawInclusive(r)
	for k := int32(0); k < h; k++ {
		ly := y + k
		if b, ok := w.block(x, ly, z); t.replaces(b, ok) {
			w.set(x, ly, z, t.log)
			t.trunkDeco.apply(w, x, ly, z, r)
		}
		if t.logDeco != nil {
			t.logDeco.place(w, x, ly, z, r)
		}
	}

	// The branch directions: north, east, south, west shuffled by three
	// raw draws.
	dirs := [4]uint8{2, 5, 3, 4}
	if r.Uint32()&1 == 0 {
		dirs[0], dirs[1] = 5, 2
	}
	if k := r.Uint32() % 3; k != 2 {
		dirs[2], dirs[k] = dirs[k], dirs[2]
	}
	if k := r.Uint32() & 3; k != 3 {
		dirs[3], dirs[k] = dirs[k], dirs[3]
	}
	n := t.branches.drawInclusive(r)
	trunk := h - remaining
	by := y + trunk - 1
	for i := int32(0); i < n && i < 4; i++ {
		d := dirs[i]
		dx, dz := facingStep(d)
		axis := "x"
		if d == 2 || d == 3 {
			axis = "z"
		}
		if b, ok := w.block(x+dx, by, z+dz); t.replaces(b, ok) {
			w.set(x+dx, by, z+dz, t.o.withAxis(t.log, axis))
			t.trunkDeco.apply(w, x+dx, by, z+dz, r)
		}
	}
	if t.canopy == canopyPoplar {
		t.poplarCanopy(w, x, y+trunk, z, r)
	}
	return blockPos{x, y, z}, true
}

// poplarCanopy is PoplarTreeCanopy::place around a trunk whose canopy part
// starts at y: square-ish layers of radius R (a weighted pick, minus one)
// with holes, narrower at the top and bottom, and log arms inside the
// widest layer.
func (t *treeFeature) poplarCanopy(w *featureWorld, x, y, z int32, r *rand.MT) bool {
	R := int32(0)
	if len(t.radii) > 0 {
		total := int32(0)
		for _, e := range t.radii {
			total += e[1]
		}
		v := int32(0)
		if total != 0 {
			v = r.IntN(total)
		}
		i := 0
		v -= t.radii[0][1]
		for v >= 0 && i+1 < len(t.radii) {
			i++
			v -= t.radii[i][1]
		}
		R = t.radii[i][0] - 1
	}
	h := t.canopyHeight.drawInclusive(r)
	flip := nextBool(r)

	t.poplarLayer(w, x, y, z, r, h, R-2, h-1, flip)
	t.poplarLayer(w, x, y, z, r, h, R-1, h-2, flip)
	t.poplarLayer(w, x, y, z, r, h, R-1, h-3, flip)
	for yo := h - 4; yo >= 1; yo-- {
		t.poplarLayer(w, x, y, z, r, h, R, yo, flip)
	}
	// Log arms: the straight lines out from the trunk at h-4, up to R-4
	// long, turned to their direction.
	for dx := -R; dx <= R; dx++ {
		for dz := -R; dz <= R; dz++ {
			if !(dx == 0 && R-mathx.Abs(dz) >= 4) && !(dz == 0 && R-mathx.Abs(dx) >= 4) {
				continue
			}
			if mathx.Abs(dx)+mathx.Abs(dz) > R+poplarSkew(dx, dz, flip, false)-2 {
				continue
			}
			axis := "z"
			if dz == 0 {
				axis = "x"
			}
			bx, ly, bz := x+dx, y+h-4, z+dz
			if b, ok := w.block(bx, ly, bz); t.replaces(b, ok) {
				w.set(bx, ly, bz, t.o.withAxis(t.branch, axis))
			}
		}
	}
	t.poplarLayer(w, x, y, z, r, h, R-1, 0, flip)
	bottom := int32(1)
	if R >= 3 {
		bottom = min(R-2, 2)
	}
	t.poplarLayer(w, x, y, z, r, h, bottom, -1, flip)
	return true
}

// poplarSkew is the diagonal bias of a poplar layer: the quadrants on one
// diagonal (picked by flip) reach one block further; on the top two layers
// the other quadrants are one shorter.
func poplarSkew(dx, dz int32, flip, top bool) int32 {
	var long bool
	if flip {
		long = (dx > 0 && dz > 0) || (dx < 0 && dz < 0)
	} else {
		long = (dx > 0 && dz < 0) || (dx < 0 && dz > 0)
	}
	switch {
	case long:
		return 1
	case top:
		return -1
	}
	return 0
}

// poplarLayer is sub_D2DB650: one layer of leaves at y+yo within the
// diamond |dx|+|dz| <= rad+skew-1, plus one more where a float beats
// side_hole_chance. The square's edge is left out on the top two layers.
func (t *treeFeature) poplarLayer(w *featureWorld, x, y, z int32, r *rand.MT, h, rad, yo int32, flip bool) {
	top := yo == h-1 || yo == h-2
	for dx := -rad; dx <= rad; dx++ {
		for dz := -rad; dz <= rad; dz++ {
			if top && (mathx.Abs(dz) == rad || mathx.Abs(dx) == rad) {
				continue
			}
			lim := rad + poplarSkew(dx, dz, flip, top) - 1
			if t.holeChance < r.Float() {
				lim++
			}
			if mathx.Abs(dx)+mathx.Abs(dz) > lim {
				continue
			}
			bx, ly, bz := x+dx, y+yo, z+dz
			if b, ok := w.block(bx, ly, bz); t.replaces(b, ok) {
				w.set(bx, ly, bz, t.leaf)
			}
		}
	}
}

// fallenTrunk is FallenTreeTrunk::place: a stump at the origin and, two or
// three blocks away in a random direction, a log lying on the ground if
// the space is free and at most two blocks in a row hang over air.
func (t *treeFeature) fallenTrunk(w *featureWorld, x, y, z, h int32, r *rand.MT) (blockPos, bool) {
	if !t.valid(w, x, y, z, h) {
		return blockPos{}, false
	}
	mod := t.modifier.draw(r)
	dir := uint8(r.IntN(4) + 2)
	dist := r.IntN(2) + 2
	stump := t.stump.draw(r)
	dx, dz := facingStep(dir)
	sx, sz := x+dx*dist, z+dz*dist
	n := h + mod - 2

	// Drop the log onto the ground: down while the block below is free.
	ly := y + stump
	for ly > MinY {
		b, ok := w.block(sx, ly-1, sz)
		if ok && !t.o.treeFree(b) {
			break
		}
		ly--
	}
	fits := true
	run := 0
	for i := int32(0); i < n; i++ {
		px, pz := sx+dx*i, sz+dz*i
		if b, ok := w.block(px, ly, pz); ok && !t.o.treeFree(b) {
			fits = false
		}
		below, ok := w.block(px, ly-1, pz)
		held := ok && t.o.solidMaterial(below)
		if !held && run >= 2 {
			fits = false
		}
		if held {
			run = 0
		} else {
			run++
		}
	}
	if fits {
		axis := "x"
		if dir == 2 || dir == 3 {
			axis = "z"
		}
		log := t.o.withAxis(t.log, axis)
		for i := int32(0); i < n; i++ {
			px, pz := sx+dx*i, sz+dz*i
			w.set(px, ly, pz, log)
			if t.logDeco != nil {
				t.logDeco.place(w, px, ly, pz, r)
			}
		}
	}
	placed := false
	top := y
	for k := int32(0); k < stump; k++ {
		if b, ok := w.block(x, y+k, z); t.replaces(b, ok) && y+k < MaxY {
			w.set(x, y+k, z, t.log)
			t.trunkDeco.apply(w, x, y+k, z, r)
			placed, top = true, y+k
		}
	}
	t.placeBase(w, x, y-1, z)
	if !placed {
		return blockPos{}, false
	}
	return blockPos{x, top + 1, z}, true
}

// treeFree reports whether a fallen log may lie where b is: air or a block
// BDS marks replaceable (BlockLegacy::canBeBuiltOver).
// VERIFY: the replaceable set is Java's "replaceable" block tag with
// Bedrock names; BDS's own flag was not exported.
func (o *Overworld) treeFree(b uint32) bool {
	switch b {
	case o.ids.air, o.ids.water, o.ids.flowingWater, o.ids.lava, o.ids.flowingLava:
		return true
	}
	switch o.blockName(b) {
	case blockShortGrass, blockTallGrass, blockFern, "minecraft:large_fern",
		blockDeadBush, blockSeagrass, "minecraft:fire", "minecraft:soul_fire",
		blockSnowLayer, blockVine, "minecraft:glow_lichen", "minecraft:resin_clump",
		"minecraft:light_block", blockStructureVoid, "minecraft:crimson_roots",
		"minecraft:warped_roots", "minecraft:nether_sprouts", "minecraft:hanging_roots",
		"minecraft:leaf_litter", "minecraft:short_dry_grass", "minecraft:tall_dry_grass", "minecraft:bush":
		return true
	}
	return false
}
