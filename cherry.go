package worldgen

import (
	"github.com/helix-bedrock/worldgen/biome"
	"github.com/helix-bedrock/worldgen/rand"
)

// Cherry trees, written from BDS 1.26.52: CherryTreeTrunk (sub_D2D2770, branch builder
// sub_D2D3250) and CherryTreeCanopy (sub_D2D1DA0, rows sub_D2D1FC0, hanging leaves
// sub_D2D2350). It is the same shape as Java's cherry trunk and foliage placers, drawn
// with Bedrock's random.

type cherryTrunk struct {
	base      int32
	intervals []int32
	// weights of one branch, two branches, two branches and the trunk.
	weights            [3]int32
	length, start, end intRange // inclusive ranges
	canopy             *cherryCanopy
}

type cherryCanopy struct {
	leaf                      uint32
	height, radius            intRange
	wideHole, cornerHole      chance
	hanging, hangingExtension chance
}

type rawCherryCanopy struct {
	Leaf             biome.Block `json:"leaf_block"`
	Height           intRange    `json:"height"`
	Radius           intRange    `json:"radius"`
	WideHole         chance      `json:"wide_bottom_layer_hole_chance"`
	CornerHole       chance      `json:"corner_hole_chance"`
	Hanging          chance      `json:"hanging_leaves_chance"`
	HangingExtension chance      `json:"hanging_leaves_extension_chance"`
}

type rawCherryTrunk struct {
	Block  biome.Block `json:"trunk_block"`
	Height struct {
		Base      int32   `json:"base"`
		Intervals []int32 `json:"intervals"`
	} `json:"trunk_height"`
	Branches struct {
		Weights struct {
			One       int32 `json:"one_branch"`
			Two       int32 `json:"two_branches"`
			WithTrunk int32 `json:"two_branches_and_trunk"`
		} `json:"tree_type_weights"`
		Length intRange `json:"branch_horizontal_length"`
		Start  intRange `json:"branch_start_offset_from_top"`
		End    intRange `json:"branch_end_offset_from_top"`
		Canopy struct {
			Cherry *rawCherryCanopy `json:"cherry_canopy"`
		} `json:"branch_canopy"`
	} `json:"branches"`
}

func (o *Overworld) parseCherryCanopy(raw *rawCherryCanopy) *cherryCanopy {
	if raw == nil {
		return nil
	}
	c := &cherryCanopy{height: raw.Height, radius: raw.Radius, wideHole: raw.WideHole, cornerHole: raw.CornerHole,
		hanging: raw.Hanging, hangingExtension: raw.HangingExtension}
	c.leaf, _ = o.lookupBlock(raw.Leaf)
	return c
}

// drawIncl draws min + nextInt(max - min + 1) when max > min, as the cherry
// parts do (their ranges include max).
func (r intRange) drawIncl(rnd *rand.MT) int32 {
	if r.max > r.min {
		return r.min + rnd.IntN(r.max-r.min+1)
	}
	return r.min
}

func (c *cherryTrunk) height(r *rand.MT) int32 {
	h := c.base
	for _, iv := range c.intervals {
		h += r.IntN(iv + 1)
	}
	return h
}

// Branch directions by index: +z, -x, -z, +x.
var cherryDX, cherryDZ = [4]int32{0, -1, 0, 1}, [4]int32{1, 0, -1, 0}

func (t *treeFeature) cherryTrunk(w *featureWorld, x, y, z, h int32, r *rand.MT) (blockPos, bool) {
	c := &t.cherry
	if !t.valid(w, x, y, z, h) || y >= MaxY-h-1 {
		return blockPos{}, false
	}
	t.placeBase(w, x, y-1, z)
	kind := int32(-1)
	if total := c.weights[0] + c.weights[1] + c.weights[2]; total > 0 {
		v := r.IntN(total)
		for i, wt := range c.weights {
			if v -= wt; v < 0 {
				kind = int32(i)
				break
			}
		}
	}
	firstRaw := h - 1 + c.start.drawIncl(r)
	first := max(0, firstRaw)
	// The second branch starts one lower in the range (its max is
	// exclusive), and is pushed above the first when they would meet.
	s2 := c.start.min
	if c.start.min < c.start.max-1 {
		s2 += r.IntN(c.start.max - c.start.min)
	}
	second := max(0, h-1+s2)
	if firstRaw <= second {
		second++
	}
	dir := r.IntN(4)
	axis := "x"
	if dir&^2 == 0 {
		axis = "z"
	}
	sideLog := t.o.withAxis(t.log, axis)
	var trunkLen int32
	switch {
	case kind == 2:
		trunkLen = h
	case kind > 0:
		trunkLen = max(first, second) + 1
	default:
		trunkLen = first + 1
	}
	for i := range trunkLen {
		if t.acaciaFree(w, x, y+i, z) {
			w.set(x, y+i, z, t.log)
		}
	}
	var tops []blockPos
	if kind == 2 {
		tops = append(tops, blockPos{x, y + trunkLen, z})
	}
	dx, dz := cherryDX[dir], cherryDZ[dir]
	tops = append(tops, t.cherryBranch(w, x, y, z, h, dx, dz, first, first < trunkLen-1, sideLog, r))
	if kind > 0 {
		tops = append(tops, t.cherryBranch(w, x, y, z, h, -dx, -dz, second, second < trunkLen-1, sideLog, r))
	}
	if c.canopy != nil {
		for _, p := range tops {
			t.cherryLeaves(w, c.canopy, p, r)
		}
	}
	return blockPos{x, y, z}, true
}

// cherryBranch grows one branch out from the trunk at start and returns where its
// canopy goes: one step out (two when it extends the trunk or ends below its start),
// then steps toward the end point, going up or down with a chance that grows with the
// height still to climb.
func (t *treeFeature) cherryBranch(w *featureWorld, x, y, z, h, dx, dz, start int32, extends bool, sideLog uint32, r *rand.MT) blockPos {
	c := &t.cherry
	endY := h - 1 + c.end.drawIncl(r)
	if endY < start {
		extends = true
	}
	n := c.length.drawIncl(r)
	if extends {
		n++
	}
	tx, ty, tz := x+n*dx, y+endY, z+n*dz
	mx, my, mz := x, y+start, z
	put := func(b uint32) {
		if t.acaciaFree(w, mx, my, mz) {
			w.set(mx, my, mz, b)
		}
	}
	mx, mz = mx+dx, mz+dz
	if extends {
		put(sideLog)
		mx, mz = mx+dx, mz+dz
	}
	put(sideLog)
	step := int32(-1)
	if ty > my {
		step = 1
	}
	for mx != tx || my != ty || mz != tz {
		dist := absI(tx-mx) + absI(ty-my) + absI(tz-mz)
		f := float32(absI(ty-my)) / float32(dist)
		if f <= r.Float() {
			mx, mz = mx+dx, mz+dz
			put(sideLog)
		} else {
			my += step
			put(t.log)
		}
	}
	return blockPos{tx, ty + 1, tz}
}

func (t *treeFeature) cherryLeaves(w *featureWorld, c *cherryCanopy, p blockPos, r *rand.MT) {
	rad := c.radius.drawIncl(r)
	h := c.height.drawIncl(r)
	i := rad - 1
	t.cherryRow(w, c, p, h-3, i-2, r)
	t.cherryRow(w, c, p, h-4, i-1, r)
	for ly := h - 5; ly >= 0; ly-- {
		t.cherryRow(w, c, p, ly, i, r)
	}
	t.cherryHanging(w, c, p, -1, i, r)
	t.cherryHanging(w, c, p, -2, i-1, r)
}

func (t *treeFeature) cherryPut(w *featureWorld, x, y, z int32, leaf uint32) bool {
	b, ok := w.block(x, y, z)
	if !ok || !t.replaces(b, ok) {
		return false
	}
	w.set(x, y, z, leaf)
	return true
}

// cherryRow is one square layer of leaves with its edge and corners thinned.
func (t *treeFeature) cherryRow(w *featureWorld, c *cherryCanopy, p blockPos, ly, rng int32, r *rand.MT) {
	for dx := -rng; dx <= rng; dx++ {
		for dz := -rng; dz <= rng; dz++ {
			ax, az := absI(dx), absI(dz)
			if ly == -1 && (ax == rng || az == rng) && c.wideHole.roll(r) {
				continue
			}
			corner := ax == rng && az == rng
			if rng < 3 {
				if corner && c.cornerHole.roll(r) {
					continue
				}
			} else {
				if corner {
					continue
				}
				if ax+az > 2*rng-2 && c.cornerHole.roll(r) {
					continue
				}
			}
			t.cherryPut(w, p[0]+dx, p[1]+ly, p[2]+dz, c.leaf)
		}
	}
}

// cherryHanging is a row plus leaves hanging one or two blocks below its edge.
func (t *treeFeature) cherryHanging(w *featureWorld, c *cherryCanopy, p blockPos, ly, rng int32, r *rand.MT) {
	t.cherryRow(w, c, p, ly, rng, r)
	// The perpendicular side of each direction (byte_1ADA5B7).
	perp := [4]int{3, 0, 1, 2}
	for d := range 4 {
		ox, oz := p[0]+rng*cherryDX[perp[d]], p[2]+rng*cherryDZ[perp[d]]
		for k := -rng; k < rng; k++ {
			x, z := ox+cherryDX[d]*k, oz+cherryDZ[d]*k
			if b, ok := w.block(x, p[1]+ly, z); !ok || b != c.leaf {
				continue
			}
			if !c.hanging.roll(r) {
				continue
			}
			if !t.cherryPut(w, x, p[1]+ly-1, z, c.leaf) {
				continue
			}
			if c.hangingExtension.roll(r) {
				t.cherryPut(w, x, p[1]+ly-2, z, c.leaf)
			}
		}
	}
}
