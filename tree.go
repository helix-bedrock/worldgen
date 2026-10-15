package worldgen

import (
	"encoding/json"
	"fmt"

	"github.com/helix-bedrock/worldgen/biome"
	"github.com/helix-bedrock/worldgen/rand"
)

// minecraft:tree_feature (BDS VanillaTreeFeature, driver sub_D2E8DE0): the
// height is drawn, then the trunk places its logs and calls the canopy at
// its top. Implemented parts: the simple trunk and the simple, spruce and
// pine canopies. Trees using other parts are not loaded yet.

// intRange is a {range_min, range_max} pair; a plain number gives min ==
// max.
type intRange struct{ min, max int32 }

func (r *intRange) UnmarshalJSON(b []byte) error {
	var n int32
	if json.Unmarshal(b, &n) == nil {
		r.min, r.max = n, n
		return nil
	}
	var v struct {
		Min int32 `json:"range_min"`
		Max int32 `json:"range_max"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	r.min, r.max = v.Min, v.Max
	return nil
}

// draw is BDS's range draw: min + nextInt(max - min) when min < max - 1,
// so max is exclusive.
func (r intRange) draw(rnd *rand.MT) int32 {
	if r.min < r.max-1 {
		return r.min + rnd.IntN(r.max-r.min)
	}
	return r.min
}

// chance is a decoration or variation chance: a fraction, or a percent.
type chance struct {
	num, den uint32
	frac     bool
	percent  float32
}

func (c *chance) UnmarshalJSON(b []byte) error {
	var f float32
	if json.Unmarshal(b, &f) == nil {
		c.percent = f
		return nil
	}
	var v struct{ Numerator, Denominator uint32 }
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	c.frac, c.num, c.den = true, v.Numerator, v.Denominator
	return nil
}

// roll reports whether the chance passes, drawing like BDS: a fraction
// draws a raw value unless it is n/n, a percent draws a float unless it is
// at least 100 or not positive.
func (c chance) roll(r *rand.MT) bool {
	if c.frac {
		if c.den == 0 {
			return false
		}
		return c.num == c.den || r.Uint32()%c.den < c.num
	}
	if c.percent >= 100 {
		return true
	}
	return c.percent > 0 && c.percent > r.Float()*100
}

// decoration is trunk_decoration / canopy_decoration: vines (or other
// blocks) on the four sides of each placed block.
type decoration struct {
	set   bool
	c     chance
	block uint32 // base state; vines get their direction bits per side
	vine  bool
	steps intRange
	dir   int32 // 0 down, 1 up, 2 outward
}

type rawDecoration struct {
	Chance chance          `json:"decoration_chance"`
	Block  json.RawMessage `json:"decoration_block"`
	Steps  *intRange       `json:"num_steps"`
	Dir    string          `json:"step_direction"`
}

func (o *Overworld) parseDecoration(raw *rawDecoration) (decoration, error) {
	if raw == nil {
		return decoration{}, nil
	}
	var b biome.Block
	if err := json.Unmarshal(raw.Block, &b); err != nil {
		return decoration{}, err
	}
	id, ok := o.lookupBlock(b)
	if !ok {
		return decoration{}, fmt.Errorf("decoration block %s missing", b.Name)
	}
	d := decoration{set: true, c: raw.Chance, block: id, vine: b.Name == "minecraft:vine", steps: intRange{1, 1}}
	if raw.Steps != nil {
		d.steps = *raw.Steps
	}
	switch raw.Dir {
	case "up":
		d.dir = 1
	case "out", "outwards", "outward":
		d.dir = 2
	}
	return d, nil
}

// vineSides are the four sides decorations look at, in BDS's order (x-1,
// x+1, z-1, z+1), with the vine_direction_bits that face the block.
var vineSides = [4]struct {
	dx, dz int32
	bits   int32
}{{-1, 0, 8}, {1, 0, 2}, {0, -1, 1}, {0, 1, 4}}

// apply rolls each side and hangs a column of the decoration block into
// air (sub_D299270 / sub_D299070).
func (d *decoration) apply(w *featureWorld, x, y, z int32, r *rand.MT) {
	if !d.set {
		return
	}
	for _, s := range vineSides {
		if !d.c.roll(r) {
			continue
		}
		px, py, pz := x+s.dx, y, z+s.dz
		if b, ok := w.block(px, py, pz); !ok || b != w.o.ids.air {
			continue
		}
		block := d.block
		if d.vine {
			if id, ok := w.o.reg.Lookup("minecraft:vine", map[string]any{"vine_direction_bits": float64(s.bits)}); ok {
				block = id
			}
		}
		n := d.steps.min
		if d.steps.max > d.steps.min {
			n += r.IntN(d.steps.max - d.steps.min + 1)
		}
		step := [3]int32{0, -1, 0}
		switch d.dir {
		case 1:
			step = [3]int32{0, 1, 0}
		case 2:
			step = [3]int32{s.dx, 0, s.dz}
		}
		for ; n > 0; n-- {
			w.set(px, py, pz, block)
			px, py, pz = px+step[0], py+step[1], pz+step[2]
			if b, ok := w.block(px, py, pz); !ok || b != w.o.ids.air {
				break
			}
		}
	}
}

type canopyKind uint8

const (
	canopySimple canopyKind = iota
	canopySpruce
	canopyPine
)

// treeFeature is a tree with a simple trunk.
type treeFeature struct {
	o *Overworld

	mayGrowOn  blockMatcher
	baseBlocks []uint32
	// below is what the trunk may replace under its original base (sunk
	// trees) and what the space check accepts; above is what it may
	// replace from the base up.
	// VERIFY: BDS keeps two lists (tree +0x68 and +0x50); reading them as
	// may_replace + may_grow_through and may_replace matches its checks.
	below, above blockMatcher

	height    intRange
	modifier  intRange
	submerge  int32
	log       uint32
	trunkDeco decoration

	canopy     canopyKind
	leaf       uint32
	canopyDeco decoration
	// simple canopy
	offMin, offMax int32
	minWidth       int32
	rise, run      int32
	variation      []chance
	// spruce canopy
	lower, upper, maxRadius intRange
	// pine canopy
	pineHeight intRange
	baseRadius int32
}

func (o *Overworld) parseTree(raw json.RawMessage) (placer, error) {
	var r struct {
		Trunk *struct {
			Height   intRange        `json:"trunk_height"`
			Modifier *intRange       `json:"height_modifier"`
			Block    biome.Block     `json:"trunk_block"`
			Deco     *rawDecoration  `json:"trunk_decoration"`
			Submerge json.RawMessage `json:"can_be_submerged"`
		} `json:"trunk"`
		Canopy *struct {
			Offset struct {
				Min int32 `json:"min"`
				Max int32 `json:"max"`
			} `json:"canopy_offset"`
			MinWidth *int32 `json:"min_width"`
			Slope    *struct {
				Rise int32 `json:"rise"`
				Run  int32 `json:"run"`
			} `json:"canopy_slope"`
			Variation json.RawMessage `json:"variation_chance"`
			Leaf      biome.Block     `json:"leaf_block"`
			Deco      *rawDecoration  `json:"canopy_decoration"`
		} `json:"canopy"`
		Spruce *struct {
			Lower     intRange    `json:"lower_offset"`
			Upper     intRange    `json:"upper_offset"`
			MaxRadius intRange    `json:"max_radius"`
			Leaf      biome.Block `json:"leaf_block"`
		} `json:"spruce_canopy"`
		Pine *struct {
			Height     intRange    `json:"canopy_height"`
			BaseRadius int32       `json:"base_radius"`
			Leaf       biome.Block `json:"leaf_block"`
		} `json:"pine_canopy"`
		MayGrowOn      []json.RawMessage `json:"may_grow_on"`
		BaseBlock      []biome.Block     `json:"base_block"`
		MayGrowThrough []json.RawMessage `json:"may_grow_through"`
		MayReplace     []json.RawMessage `json:"may_replace"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	if r.Trunk == nil {
		return nil, nil
	}
	t := &treeFeature{o: o, mayGrowOn: parseMatcher(r.MayGrowOn), height: r.Trunk.Height}
	replace := parseMatcher(r.MayReplace)
	t.above = replace
	t.below = replace.union(parseMatcher(r.MayGrowThrough))
	for _, b := range r.BaseBlock {
		if id, ok := o.lookupBlock(b); ok {
			t.baseBlocks = append(t.baseBlocks, id)
		}
	}
	if r.Trunk.Modifier != nil {
		t.modifier = *r.Trunk.Modifier
	}
	if len(r.Trunk.Submerge) > 0 {
		var s struct {
			MaxDepth int32 `json:"max_depth"`
		}
		if json.Unmarshal(r.Trunk.Submerge, &s) == nil {
			t.submerge = s.MaxDepth
		}
	}
	var ok bool
	if t.log, ok = o.lookupBlock(r.Trunk.Block); !ok {
		return nil, fmt.Errorf("trunk block %s missing", r.Trunk.Block.Name)
	}
	var err error
	if t.trunkDeco, err = o.parseDecoration(r.Trunk.Deco); err != nil {
		return nil, err
	}
	var leaf biome.Block
	switch {
	case r.Canopy != nil:
		c := r.Canopy
		t.canopy, leaf = canopySimple, c.Leaf
		t.offMin, t.offMax = c.Offset.Min, c.Offset.Max
		t.minWidth, t.rise, t.run = 1, 1, 2
		if c.MinWidth != nil {
			t.minWidth = *c.MinWidth
		}
		if c.Slope != nil && c.Slope.Run != 0 {
			t.rise, t.run = c.Slope.Rise, c.Slope.Run
		}
		var list []chance
		if json.Unmarshal(c.Variation, &list) != nil {
			var one chance
			if len(c.Variation) > 0 {
				if err := json.Unmarshal(c.Variation, &one); err != nil {
					return nil, err
				}
				for range t.offMax - t.offMin + 1 {
					list = append(list, one)
				}
			}
		}
		t.variation = list
		if t.canopyDeco, err = o.parseDecoration(c.Deco); err != nil {
			return nil, err
		}
	case r.Spruce != nil:
		t.canopy, leaf = canopySpruce, r.Spruce.Leaf
		t.lower, t.upper, t.maxRadius = r.Spruce.Lower, r.Spruce.Upper, r.Spruce.MaxRadius
	case r.Pine != nil:
		t.canopy, leaf = canopyPine, r.Pine.Leaf
		t.pineHeight, t.baseRadius = r.Pine.Height, r.Pine.BaseRadius
	default:
		return nil, nil
	}
	if t.leaf, ok = o.lookupBlock(leaf); !ok {
		return nil, fmt.Errorf("leaf block %s missing", leaf.Name)
	}
	return t, nil
}

func (t *treeFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	h := t.height.draw(r)
	return t.trunk(w, x, y, z, h, r)
}

// trunk is SimpleTreeTrunk::place (sub_D297980).
func (t *treeFeature) trunk(w *featureWorld, x, y, z, h int32, r *rand.MT) (blockPos, bool) {
	origin := y
	for d := t.submerge; d > 0; d-- {
		if !t.below.empty() {
			b, ok := w.block(x, y-1, z)
			if !ok || !t.below.match(t.o, b) {
				break
			}
		}
		y--
	}
	if !t.valid(w, x, y, z, h) {
		return blockPos{}, false
	}
	h += t.modifier.draw(r)
	if h <= 0 {
		return blockPos{}, false
	}
	top, any := int32(0), false
	for k := int32(0); k < h; k++ {
		ly := y + k
		list := t.above
		if ly < origin {
			list = t.below
		}
		if !list.empty() {
			b, ok := w.block(x, ly, z)
			if !ok || !list.match(t.o, b) {
				continue
			}
		}
		if ly < MinY || ly >= MaxY {
			break
		}
		w.set(x, ly, z, t.log)
		t.trunkDeco.apply(w, x, ly, z, r)
		top, any = ly, true
	}
	if !any {
		return blockPos{}, false
	}
	if !t.leaves(w, x, top+1, z, r) {
		return blockPos{}, false
	}
	if len(t.baseBlocks) > 0 {
		if b, ok := w.block(x, y-1, z); ok && !containsID(t.baseBlocks, b) {
			w.set(x, y-1, z, t.baseBlocks[0])
		}
	}
	return blockPos{x, top + 1, z}, true
}

func containsID(list []uint32, id uint32) bool {
	for _, v := range list {
		if v == id {
			return true
		}
	}
	return false
}

// valid is the trunk space check (sub_D298050): inside the world, on a block
// it may grow on, and the space around the trunk free.
func (t *treeFeature) valid(w *featureWorld, x, y, z, h int32) bool {
	if y <= MinY || y+h >= MaxY {
		return false
	}
	if !t.mayGrowOn.empty() {
		b, ok := w.block(x, y-1, z)
		if !ok || !t.mayGrowOn.match(t.o, b) {
			return false
		}
	}
	if t.below.empty() {
		return true
	}
	for ly := y; ly <= y+h+1; ly++ {
		rad := int32(1)
		if ly == y {
			rad = 0
		}
		if ly >= y+h-1 {
			rad = 2
		}
		for bx := x - rad; bx <= x+rad; bx++ {
			for bz := z - rad; bz <= z+rad; bz++ {
				b, ok := w.block(bx, ly, bz)
				if !ok || !t.below.match(t.o, b) {
					return false
				}
			}
		}
	}
	return true
}

// leafSpot reports whether a canopy may put a leaf here: air or a
// replaceable plant (BDS material checks 0, 7 and 8).
func (t *treeFeature) leafSpot(w *featureWorld, b uint32) bool {
	if b == w.o.ids.air {
		return true
	}
	switch w.o.blockName(b) {
	case "minecraft:snow_layer", "minecraft:short_grass", "minecraft:tall_grass", "minecraft:fern", "minecraft:large_fern", "minecraft:deadbush", "minecraft:vine":
		return true
	}
	return false
}

func (t *treeFeature) leaves(w *featureWorld, x, y, z int32, r *rand.MT) bool {
	switch t.canopy {
	case canopySimple:
		return t.simpleCanopy(w, x, y, z, r)
	case canopySpruce:
		return t.spruceCanopy(w, x, y, z, r)
	case canopyPine:
		return t.pineCanopy(w, x, y, z, r)
	}
	return false
}

// simpleCanopy is SimpleTreeCanopy (sub_D2DD1B0).
func (t *treeFeature) simpleCanopy(w *featureWorld, x, top, z int32, r *rand.MT) bool {
	slope := float32(t.rise) / float32(t.run)
	radius := func(dy int32) int32 {
		return t.minWidth + int32(float32(t.offMax)*slope) - int32(float32(dy)*slope)
	}
	placed := false
	for ly := top + t.offMin; ly <= top+t.offMax; ly++ {
		dy := ly - top
		rad := radius(dy)
		for bx := x - rad; bx <= x+rad; bx++ {
			for bz := z - rad; bz <= z+rad; bz++ {
				if absI(bx-x) == rad && absI(bz-z) == rad {
					i := dy - t.offMin
					if int(i) < len(t.variation) && t.variation[i].skipCorner(r) {
						continue
					}
				}
				if b, ok := w.block(bx, ly, bz); ok && t.leafSpot(w, b) {
					w.set(bx, ly, bz, t.leaf)
					placed = true
				}
			}
		}
	}
	if t.canopyDeco.set && placed {
		for ly := top + t.offMin; ly <= top+t.offMax; ly++ {
			rad := radius(ly - top)
			for bx := x - rad; bx <= x+rad; bx++ {
				for bz := z - rad; bz <= z+rad; bz++ {
					if b, ok := w.block(bx, ly, bz); ok && b == t.leaf {
						t.canopyDeco.apply(w, bx, ly, bz, r)
					}
				}
			}
		}
	}
	return placed
}

// skipCorner is the variation chance of a canopy corner: the chance to
// leave the corner out.
func (c chance) skipCorner(r *rand.MT) bool {
	if c.frac {
		return c.den != 0 && (c.num == c.den || r.Uint32()%c.den < c.num)
	}
	return c.percent >= 100 || (c.percent > 0 && c.percent > r.Float()*100)
}

// spruceCanopy is SpruceTreeCanopy (sub_D298430): layers from the top down
// to lower_offset above the base, the radius growing and resetting.
func (t *treeFeature) spruceCanopy(w *featureWorld, x, top, z int32, r *rand.MT) bool {
	// Find the trunk's base: walk down until the block below may be grown on.
	base := top
	if !t.mayGrowOn.empty() {
		for {
			b, ok := w.block(x, base-1, z)
			if ok && t.mayGrowOn.match(t.o, b) {
				break
			}
			if base <= MinY {
				return false
			}
			base--
		}
	}
	lower := t.lower.draw(r)
	upper := t.upper.draw(r)
	maxR := t.maxRadius.draw(r)
	rad := r.IntN(t.maxRadius.max - t.maxRadius.min)
	layers := top + upper - (lower + base) + 1
	if layers <= 0 {
		return false
	}
	limit := t.maxRadius.max - t.maxRadius.min - 1
	first := int32(0)
	placed := false
	for i := int32(0); i < layers; i++ {
		ly := top + upper - i
		if t.disk(w, x, ly, z, rad, true, false) {
			placed = true
		}
		if rad >= limit {
			rad = first
			first = 1
			limit = min(limit+1, maxR)
		} else {
			rad++
		}
	}
	return placed
}

// disk places a square of leaves with its corners cut when rad > 0. With
// replaceList the leaves go where may_replace allows (pine); otherwise into
// air and plants (spruce).
func (t *treeFeature) disk(w *featureWorld, x, y, z, rad int32, cutCorners, replaceList bool) bool {
	placed := false
	for bx := x - rad; bx <= x+rad; bx++ {
		for bz := z - rad; bz <= z+rad; bz++ {
			if cutCorners && rad != 0 && absI(bx-x) == rad && absI(bz-z) == rad {
				continue
			}
			b, ok := w.block(bx, y, bz)
			if !ok {
				continue
			}
			if replaceList {
				if !t.above.empty() && !t.above.match(t.o, b) {
					continue
				}
			} else if !t.leafSpot(w, b) {
				continue
			}
			w.set(bx, y, bz, t.leaf)
			placed = true
		}
	}
	return placed
}

// pineCanopy is PineTreeCanopy (sub_D2DAB10): from one above the top down,
// the radius grows to its maximum, shrinking once near the bottom.
func (t *treeFeature) pineCanopy(w *featureWorld, x, top, z int32, r *rand.MT) bool {
	h := t.pineHeight.draw(r)
	maxR := t.baseRadius + r.IntN(h+1)
	rad := int32(0)
	placed := false
	for dy := int32(1); dy >= 1-h; dy-- {
		if t.disk(w, x, top+dy, z, rad, true, true) {
			placed = true
		}
		next := rad + 1
		if rad >= maxR {
			next = rad
		}
		if dy == 2-h && rad > 0 {
			next = rad - 1
		}
		rad = next
	}
	return placed
}

func absI(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}
