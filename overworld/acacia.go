package overworld

import (
	"github.com/helix-bedrock/worldgen/internal/mathx"
	"github.com/helix-bedrock/worldgen/rand"
)

// Acacia-trunk trees of minecraft:tree_feature (savanna acacias, azaleas,
// dark and pale oaks), written from BDS 1.26.52: AcaciaTreeTrunk (place
// sub_D2D0AE0, height sub_D2D1D40), AcaciaTreeCanopy (sub_D2D0440),
// RoofedTreeCanopy (sub_D2DC7E0) and RandomSpreadTreeCanopy (sub_D2DC200).

type acaciaTrunk struct {
	width        int32
	base         int32
	intervals    []int32
	minCanopy    int32 // trunk blocks from this height up feed the canopy
	leanHeight   intRange
	leanSteps    intRange
	leanLength   intRange
	diagonal     bool
	branchLen    intRange
	branchPos    intRange
	branchChance chance
	branchCanopy *acaciaCanopy
}

type acaciaCanopy struct {
	size     int32
	simplify bool
	leaf     uint32
}

type roofedCanopy struct {
	height       int32
	core         int32
	hasCore      bool
	outer, inner int32
	leaf         uint32
}

type randomSpreadCanopy struct {
	height, radius intRange
	attempts       int32
	leaves         []uint32
	weights        []float32
}

// leanX and leanZ are the lean directions: south, west, north, east.
var (
	leanX = [4]int32{0, -1, 0, 1}
	leanZ = [4]int32{1, 0, -1, 0}
)

// acaciaFree is sub_D299C30: a trunk or branch block may go where
// may_replace allows, or into air, liquids and replaceable plants.
// VERIFY: BDS tests a replaceable flag and material types; treeFree lists
// the same blocks by name.
func (t *treeFeature) acaciaFree(w *featureWorld, x, y, z int32) bool {
	if t.above.empty() {
		return true
	}
	b, ok := w.block(x, y, z)
	if !ok {
		return false
	}
	return t.above.match(t.o, b) || t.o.treeFree(b)
}

func (a *acaciaTrunk) height(r *rand.MT) int32 {
	h := a.base
	for _, iv := range a.intervals {
		h += r.IntN(iv)
	}
	return h
}

// acaciaTrunk is AcaciaTreeTrunk::place.
func (t *treeFeature) acaciaTrunk(w *featureWorld, x, y, z, h int32, r *rand.MT) (blockPos, bool) {
	a := &t.acacia
	if !t.valid(w, x, y, z, h) || y >= MaxY-h-1 {
		return blockPos{}, false
	}
	for i := int32(0); i < a.width; i++ {
		for j := int32(0); j < a.width; j++ {
			t.placeBase(w, x+i, y-1, z+j)
		}
	}
	dir := r.IntN(4)
	leanStart := h - a.leanHeight.draw(r)
	steps := a.leanSteps.draw(r)
	extra := a.leanLength.draw(r)
	cx, cy, cz := x, y, z
	var trunk []blockPos
	for k := int32(0); k < h+extra; k++ {
		if k >= leanStart && steps > 0 {
			cx, cz = cx+leanX[dir], cz+leanZ[dir]
			steps--
		}
		if k < h {
			cy = y + k
		}
		if !t.acaciaFree(w, cx, cy, cz) {
			continue
		}
		for i := int32(0); i < a.width; i++ {
			for j := int32(0); j < a.width; j++ {
				px, py, pz := x+i, y+k, z+j
				if a.diagonal {
					px, py, pz = cx+i, cy, cz+j
				}
				if !t.acaciaFree(w, px, py, pz) {
					continue
				}
				mask := sideMask{i == 0, i != 0 && i == a.width-1, j == 0, j != 0 && j == a.width-1}
				w.set(px, py, pz, t.log)
				t.trunkDeco.applyMask(w, px, py, pz, r, mask)
				if k >= a.minCanopy {
					trunk = append(trunk, blockPos{px, py, pz})
				}
			}
		}
	}
	if len(trunk) == 0 {
		// BDS places the canopy at the world origin here; nothing a
		// region near this tree can hold.
		return blockPos{x, y, z}, true
	}
	last := trunk[len(trunk)-1]
	switch t.canopy {
	case canopyAcacia:
		t.acaciaCanopy.place(t, w, last[0], last[1], last[2])
	case canopyRoofed:
		t.roofedCanopy.place(w, last[0], last[1], last[2], a.width, r)
	case canopyRandomSpread:
		t.spreadCanopy.place(t, w, trunk, r)
	}
	if !a.diagonal {
		for bx := int32(-1); bx <= a.width; bx++ {
			for bz := int32(-1); bz <= a.width; bz++ {
				if bx >= 0 && bz >= 0 && bx < a.width && bz < a.width {
					continue
				}
				if !a.branchChance.roll(r) {
					continue
				}
				n := a.branchLen.draw(r)
				p := a.branchPos.draw(r)
				for d := int32(0); d < n; d++ {
					py := last[1] - d - p
					if t.acaciaFree(w, x+bx, py, z+bz) {
						w.set(x+bx, py, z+bz, t.log)
					}
				}
				if a.branchCanopy != nil {
					a.branchCanopy.place(t, w, last[0]+bx, last[1]-p, last[2]+bz)
				}
			}
		}
		return blockPos{x, y, z}, true
	}
	// A second branch, in another direction than the lean.
	d2 := r.IntN(4)
	if d2 == dir || !a.branchChance.roll(r) {
		return blockPos{x, y, z}, true
	}
	p := a.branchPos.draw(r)
	n := a.branchLen.draw(r)
	k := leanStart - p
	if k >= h || n <= 0 {
		return blockPos{x, y, z}, true
	}
	bx, bz := x, z
	placed, endY := false, int32(0)
	for {
		if k > 0 {
			bx, bz = bx+leanX[d2], bz+leanZ[d2]
			py := y + k
			if t.acaciaFree(w, bx, py, bz) {
				w.set(bx, py, bz, t.log)
				t.trunkDeco.apply(w, bx, py, bz, r)
				placed, endY = true, py
				if k+1 >= h || n <= 1 {
					break
				}
				k++
				n--
				continue
			}
		}
		k++
		if k >= h {
			break
		}
		n--
		if n <= 0 {
			break
		}
	}
	if placed && a.branchCanopy != nil {
		a.branchCanopy.place(t, w, bx, endY, bz)
	}
	return blockPos{x, y, z}, true
}

// place is AcaciaTreeCanopy::place on a trunk top at (x, y, z): a flat top
// layer above and a wider square below, its corners cut.
func (c *acaciaCanopy) place(t *treeFeature, w *featureWorld, x, y, z int32) {
	s := c.size
	big := max(s, 2)
	if s < 3 {
		big = 2
	}
	top := max(s, 1)
	put := func(px, py, pz int32) {
		if b, ok := w.block(px, py, pz); t.replaces(b, ok) {
			w.set(px, py, pz, c.leaf)
		}
	}
	if c.simplify {
		for dx := 1 - top; dx < top; dx++ {
			for dz := 1 - top; dz < top; dz++ {
				put(x+dx, y+1, z+dz)
			}
		}
	} else {
		for dx := 2 - big; dx < big-1; dx++ {
			for dz := 2 - big; dz < big-1; dz++ {
				put(x+dx, y+1, z+dz)
			}
		}
		put(x+top-1, y+1, z)
		put(x+1-top, y+1, z)
		put(x, y+1, z+top-1)
		put(x, y+1, z+1-top)
	}
	for dx := -s; dx <= s; dx++ {
		for dz := -s; dz <= s; dz++ {
			if mathx.Abs(dx) == s && mathx.Abs(dz) == s {
				continue
			}
			put(x+dx, y, z+dz)
		}
	}
}

// place is RoofedTreeCanopy::place at (x, y, z) with a w x w trunk core:
// mirrored rings one below and height above, a core cap on half the trees,
// then height layers of a rounded square. Leaves go only into air.
func (c *roofedCanopy) place(w *featureWorld, x, y, z, width int32, r *rand.MT) {
	cx, cz := width, width
	if c.hasCore {
		cx, cz = c.core, c.core
	}
	put := func(px, py, pz int32) {
		if b, ok := w.block(px, py, pz); ok && b == w.o.ids.air {
			w.set(px, py, pz, c.leaf)
		}
	}
	ro := c.outer
	for dx := -ro; dx <= 0; dx++ {
		mx := cx - 1 - dx
		for dz := -ro; dz <= 0; dz++ {
			mz := cz - 1 - dz
			put(x+dx, y-1, z+dz)
			put(x+mx, y-1, z+dz)
			put(x+dx, y-1, z+mz)
			put(x+mx, y-1, z+mz)
			if (dz > 1-ro || dx > -ro) && (dx != 1-ro || dz != -ro) {
				ty := y + c.height
				put(x+dx, ty, z+dz)
				put(x+mx, ty, z+dz)
				put(x+dx, ty, z+mz)
				put(x+mx, ty, z+mz)
			}
		}
	}
	if nextBool(r) {
		for i := int32(0); i < cx; i++ {
			for j := int32(0); j < cz; j++ {
				put(x+i, y+c.height+1, z+j)
			}
		}
	}
	ri := c.inner
	for layer := int32(0); layer < c.height; layer++ {
		for dx := -ri; dx < cx+ri; dx++ {
			for dz := -ri; dz < cz+ri; dz++ {
				switch {
				case dx == -ri && dz == -ri, dx == -ri && dz == ri+cz-1,
					dx == ri+cx-1 && (dz == ri+cz-1 || dz == -ri):
					continue
				}
				if mathx.Abs(dx) >= ri && mathx.Abs(dz) >= ri {
					continue
				}
				put(x+dx, y+layer, z+dz)
			}
		}
	}
}

// drawAlways is the canopy range draw that always draws: min +
// nextInt(max-min+1) when max >= min.
func (r intRange) drawAlways(rnd *rand.MT) int32 {
	if r.max < r.min {
		return r.min
	}
	return r.min + rnd.IntN(r.max-r.min+1)
}

// place is RandomSpreadTreeCanopy::place: around every trunk block that
// feeds the canopy, attempts tries at a weighted leaf block and a random
// offset within the canopy's height and radius.
func (c *randomSpreadCanopy) place(t *treeFeature, w *featureWorld, trunk []blockPos, r *rand.MT) {
	h := c.height.drawAlways(r)
	rad := c.radius.drawAlways(r)
	if c.attempts == 0 || len(c.leaves) == 0 {
		return
	}
	for _, p := range trunk {
		for range c.attempts {
			total := int32(0)
			for _, wgt := range c.weights {
				total = int32(float32(total) + wgt)
			}
			v := int32(0)
			if total != 0 {
				v = r.IntN(total)
			}
			i := 0
			v = int32(float32(v) - c.weights[0])
			for v >= 0 && i+1 < len(c.leaves) {
				i++
				v = int32(float32(v) - c.weights[i])
			}
			a, b := r.IntN(rad), r.IntN(rad)
			e, f := r.IntN(h), r.IntN(h)
			g, k := r.IntN(rad), r.IntN(rad)
			px, py, pz := p[0]+b+a-rad+1, p[1]+f+e-h+1, p[2]+k+g-rad+1
			if t.acaciaFree(w, px, py, pz) {
				w.set(px, py, pz, c.leaves[i])
			}
		}
	}
}
