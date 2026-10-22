package overworld

import (
	"encoding/json"
	"fmt"

	"github.com/helix-bedrock/worldgen/internal/mathx"

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

// rawIntRange is the object form of an intRange.
type rawIntRange struct {
	Min int32 `json:"range_min"`
	Max int32 `json:"range_max"`
}

// rawFraction is the fraction form of a chance.
type rawFraction struct{ Numerator, Denominator uint32 }

// rawSubmerge is a trunk's can_be_submerged object.
type rawSubmerge struct {
	MaxDepth int32 `json:"max_depth"`
}

func (r *intRange) UnmarshalJSON(b []byte) error {
	var n int32
	if json.Unmarshal(b, &n) == nil {
		r.min, r.max = n, n
		return nil
	}
	var v rawIntRange
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
	var v rawFraction
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
	d := decoration{set: true, c: raw.Chance, block: id, vine: b.Name == blockVine, steps: intRange{1, 1}}
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
type vineSide struct {
	dx, dz int32
	bits   int32
}

func vineSides() [4]vineSide {
	return [4]vineSide{{-1, 0, 8}, {1, 0, 2}, {0, -1, 1}, {0, 1, 4}}
}

// apply rolls each side and hangs a column of the decoration block into
// air (sub_D299270 / sub_D299070).
func (d *decoration) apply(w *featureWorld, x, y, z int32, r *rand.MT) {
	d.applyMask(w, x, y, z, r, sideMask{true, true, true, true})
}

// applyMask is apply for the sides in m only (west, east, north, south);
// BDS rolls the chance only for those sides.
func (d *decoration) applyMask(w *featureWorld, x, y, z int32, r *rand.MT, m sideMask) {
	if !d.set {
		return
	}
	for i, s := range vineSides() {
		if !m[i] {
			continue
		}
		if !d.c.roll(r) {
			continue
		}
		px, py, pz := x+s.dx, y, z+s.dz
		if b, ok := w.block(px, py, pz); !ok || b != w.o.ids.air {
			continue
		}
		block := d.block
		if d.vine {
			if id, ok := w.o.reg.Lookup(blockVine, map[string]any{"vine_direction_bits": float64(s.bits)}); ok {
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
	canopyPoplar
	canopyFancy
	canopyMega
	canopyAcacia
	canopyRoofed
	canopyRandomSpread
)

type trunkKind uint8

const (
	trunkSimple trunkKind = iota
	trunkPoplar
	trunkFallen
	trunkFancy
	trunkMega
	trunkAcacia
	trunkCherry
	trunkMangrove
)

// treeFeature is a tree: a trunk and, except for fallen trees, a canopy.
type treeFeature struct {
	o *Overworld

	trunkKind trunkKind
	// logDeco is log_decoration_feature, placed at each log of poplar and
	// fallen trunks; nil when unset.
	logDeco *featureRef
	// poplar trunk
	remaining, branches intRange
	// fallen trunk; modifier is shared with the simple trunk
	stump intRange

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
	// acacia trunk and its canopies
	acacia       acaciaTrunk
	cherry       cherryTrunk
	mangrove     *mangroveTree
	acaciaCanopy *acaciaCanopy
	roofedCanopy *roofedCanopy
	spreadCanopy *randomSpreadCanopy
	// mega trunk and canopy
	mega       megaTrunk
	megaCanopy *megaCanopy
	// base_cluster (podzol around giant spruces)
	clusters, clusterRadius int32
	clusterReplace          blockMatcher
	// fancy trunk and canopy
	fancy                    fancyTrunk
	fancyHeight, fancyRadius int32
	// poplar canopy: radii are {radius, weight} pairs
	radii        [][2]int32
	canopyHeight intRange
	holeChance   float32
	branch       uint32
}

// rawTree is the JSON of a minecraft:tree_feature.
type rawTree struct {
	Trunk *struct {
		Height   intRange        `json:"trunk_height"`
		Modifier *intRange       `json:"height_modifier"`
		Block    biome.Block     `json:"trunk_block"`
		Deco     *rawDecoration  `json:"trunk_decoration"`
		Submerge json.RawMessage `json:"can_be_submerged"`
	} `json:"trunk"`
	Poplar *struct {
		Height    *intRange      `json:"trunk_height"`
		Block     biome.Block    `json:"trunk_block"`
		Remaining *intRange      `json:"remaining_trunk_height_above_branches"`
		Branches  *intRange      `json:"amount_of_foliage_support_branches"`
		Deco      *rawDecoration `json:"trunk_decoration"`
		LogDeco   string         `json:"log_decoration_feature"`
	} `json:"poplar_trunk"`
	Fallen *struct {
		Length   *intRange      `json:"log_length"`
		Block    biome.Block    `json:"trunk_block"`
		Deco     *rawDecoration `json:"trunk_decoration"`
		LogDeco  string         `json:"log_decoration_feature"`
		Modifier *intRange      `json:"height_modifier"`
		Stump    *intRange      `json:"stump_height"`
	} `json:"fallen_trunk"`
	Fancy *struct {
		Height struct {
			Base     int32   `json:"base"`
			Variance int32   `json:"variance"`
			Scale    float32 `json:"scale"`
		} `json:"trunk_height"`
		Width    *int32      `json:"trunk_width"`
		Block    biome.Block `json:"trunk_block"`
		Branches struct {
			Slope       float32 `json:"slope"`
			Density     float32 `json:"density"`
			MinAltitude float32 `json:"min_altitude_factor"`
		} `json:"branches"`
		WidthScale      *float32 `json:"width_scale"`
		FoliageAltitude *float32 `json:"foliage_altitude_factor"`
	} `json:"fancy_trunk"`
	Mega *struct {
		Width  *int32 `json:"trunk_width"`
		Height struct {
			Base      int32   `json:"base"`
			Intervals []int32 `json:"intervals"`
		} `json:"trunk_height"`
		Block    biome.Block    `json:"trunk_block"`
		Deco     *rawDecoration `json:"trunk_decoration"`
		Branches *struct {
			Length   int32     `json:"branch_length"`
			Slope    *float32  `json:"branch_slope"`
			Interval *intRange `json:"branch_interval"`
			Altitude struct {
				Min float32 `json:"min"`
				Max float32 `json:"max"`
			} `json:"branch_altitude_factor"`
			Canopy struct {
				Mega *rawMegaCanopy `json:"mega_canopy"`
			} `json:"branch_canopy"`
		} `json:"branches"`
	} `json:"mega_trunk"`
	Acacia *struct {
		Width  *int32 `json:"trunk_width"`
		Height struct {
			Base      int32   `json:"base"`
			Intervals []int32 `json:"intervals"`
			MinCanopy *int32  `json:"min_height_for_canopy"`
		} `json:"trunk_height"`
		Block biome.Block    `json:"trunk_block"`
		Deco  *rawDecoration `json:"trunk_decoration"`
		Lean  *struct {
			Diagonal *bool     `json:"allow_diagonal_growth"`
			Height   *intRange `json:"lean_height"`
			Steps    *intRange `json:"lean_steps"`
			Length   *intRange `json:"lean_length"`
		} `json:"trunk_lean"`
		Branches *struct {
			Chance   *chance   `json:"branch_chance"`
			Length   *intRange `json:"branch_length"`
			Position *intRange `json:"branch_position"`
			Canopy   struct {
				Acacia *rawAcaciaCanopy `json:"acacia_canopy"`
			} `json:"branch_canopy"`
		} `json:"branches"`
	} `json:"acacia_trunk"`
	AcaciaCanopy *rawAcaciaCanopy `json:"acacia_canopy"`
	Cherry       *rawCherryTrunk  `json:"cherry_trunk"`
	CherryCanopy *rawCherryCanopy `json:"cherry_canopy"`
	RoofedCanopy *struct {
		Height int32       `json:"canopy_height"`
		Core   *int32      `json:"core_width"`
		Outer  int32       `json:"outer_radius"`
		Inner  int32       `json:"inner_radius"`
		Leaf   biome.Block `json:"leaf_block"`
	} `json:"roofed_canopy"`
	SpreadCanopy *struct {
		Height   intRange            `json:"canopy_height"`
		Radius   intRange            `json:"canopy_radius"`
		Attempts int32               `json:"leaf_placement_attempts"`
		Leaves   [][]json.RawMessage `json:"leaf_blocks"`
	} `json:"random_spread_canopy"`
	MegaCanopy     *rawMegaCanopy `json:"mega_canopy"`
	MegaPineCanopy *rawMegaCanopy `json:"mega_pine_canopy"`
	BaseCluster    *struct {
		MayReplace []json.RawMessage `json:"may_replace"`
		Clusters   int32             `json:"num_clusters"`
		Radius     int32             `json:"cluster_radius"`
	} `json:"base_cluster"`
	FancyCanopy *struct {
		Height int32       `json:"height"`
		Radius int32       `json:"radius"`
		Leaf   biome.Block `json:"leaf_block"`
	} `json:"fancy_canopy"`
	PoplarCanopy *struct {
		Radius []struct {
			Value  int32 `json:"value"`
			Weight int32 `json:"weight"`
		} `json:"radius"`
		Height *intRange   `json:"height"`
		Hole   float32     `json:"side_hole_chance"`
		Leaf   biome.Block `json:"leaf_block"`
		Branch biome.Block `json:"branch_block"`
	} `json:"poplar_canopy"`
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

func (o *Overworld) parseTree(raw json.RawMessage, set *featureSet) (placer, error) {
	var r rawTree
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	t := &treeFeature{o: o, mayGrowOn: parseMatcher(r.MayGrowOn)}
	replace := parseMatcher(r.MayReplace)
	t.above = replace
	t.below = replace.union(parseMatcher(r.MayGrowThrough))
	for _, b := range r.BaseBlock {
		if id, ok := o.lookupBlock(b); ok {
			t.baseBlocks = append(t.baseBlocks, id)
		}
	}
	if ok, err := o.parseMangrove(raw, t); err != nil || ok {
		if !ok {
			return nil, err
		}
		return t, nil
	}
	// Defaults are those of BDS's trunk constructors (sub_4D92160,
	// sub_4C4E410) for fields the JSON leaves out.
	var trunkBlock biome.Block
	var trunkDeco *rawDecoration
	logDeco := ""
	switch {
	case r.Trunk != nil:
		t.trunkKind, trunkBlock, trunkDeco = trunkSimple, r.Trunk.Block, r.Trunk.Deco
		t.height = r.Trunk.Height
		if r.Trunk.Modifier != nil {
			t.modifier = *r.Trunk.Modifier
		}
		if len(r.Trunk.Submerge) > 0 {
			var s rawSubmerge
			if json.Unmarshal(r.Trunk.Submerge, &s) == nil {
				t.submerge = s.MaxDepth
			}
		}
	case r.Poplar != nil:
		p := r.Poplar
		t.trunkKind, trunkBlock, trunkDeco, logDeco = trunkPoplar, p.Block, p.Deco, p.LogDeco
		t.height, t.remaining, t.branches = intRange{7, 9}, intRange{4, 4}, intRange{1, 4}
		if p.Height != nil {
			t.height = *p.Height
		}
		if p.Remaining != nil {
			t.remaining = *p.Remaining
		}
		if p.Branches != nil {
			t.branches = *p.Branches
		}
	case r.Fancy != nil:
		fc := r.Fancy
		t.trunkKind, trunkBlock = trunkFancy, fc.Block
		// Defaults from the constructor (sub_4C6F6F0).
		t.fancy = fancyTrunk{base: 5, variance: 12, width: 1, scale: 0.618, slope: 0.381, density: 1,
			minAltitude: 0.2, foliageAltitude: 0.3, widthScale: 1}
		if fc.Height.Base != 0 || fc.Height.Variance != 0 {
			t.fancy.base, t.fancy.variance, t.fancy.scale = fc.Height.Base, fc.Height.Variance, fc.Height.Scale
		}
		if fc.Width != nil {
			t.fancy.width = *fc.Width
		}
		if fc.Branches.Slope != 0 || fc.Branches.Density != 0 {
			t.fancy.slope, t.fancy.density, t.fancy.minAltitude = fc.Branches.Slope, fc.Branches.Density, fc.Branches.MinAltitude
		}
		if fc.WidthScale != nil {
			t.fancy.widthScale = *fc.WidthScale
		}
		if fc.FoliageAltitude != nil {
			t.fancy.foliageAltitude = *fc.FoliageAltitude
		}
	case r.Acacia != nil:
		ac := r.Acacia
		t.trunkKind, trunkBlock, trunkDeco = trunkAcacia, ac.Block, ac.Deco
		// Defaults from the constructor (sub_4ADA0C0).
		t.acacia = acaciaTrunk{width: 1, base: 5, minCanopy: 3, leanHeight: intRange{1, 4}, leanSteps: intRange{1, 4},
			diagonal: true, branchLen: intRange{1, 5}, branchPos: intRange{1, 3}}
		if ac.Width != nil {
			t.acacia.width = *ac.Width
		}
		if ac.Height.Base != 0 || len(ac.Height.Intervals) > 0 {
			t.acacia.base, t.acacia.intervals = ac.Height.Base, ac.Height.Intervals
		}
		if ac.Height.MinCanopy != nil {
			t.acacia.minCanopy = *ac.Height.MinCanopy
		}
		if l := ac.Lean; l != nil {
			if l.Diagonal != nil {
				t.acacia.diagonal = *l.Diagonal
			}
			if l.Height != nil {
				t.acacia.leanHeight = *l.Height
			}
			if l.Steps != nil {
				t.acacia.leanSteps = *l.Steps
			}
			if l.Length != nil {
				t.acacia.leanLength = *l.Length
			}
		}
		if b := ac.Branches; b != nil {
			if b.Chance != nil {
				t.acacia.branchChance = *b.Chance
			}
			if b.Length != nil {
				t.acacia.branchLen = *b.Length
			}
			if b.Position != nil {
				t.acacia.branchPos = *b.Position
			}
			if b.Canopy.Acacia != nil {
				c, err := o.parseAcaciaCanopy(b.Canopy.Acacia)
				if err != nil {
					return nil, err
				}
				t.acacia.branchCanopy = c
			}
		}
	case r.Cherry != nil:
		ch := r.Cherry
		t.trunkKind, trunkBlock = trunkCherry, ch.Block
		b := ch.Branches
		t.cherry = cherryTrunk{base: ch.Height.Base, intervals: ch.Height.Intervals,
			weights: [3]int32{b.Weights.One, b.Weights.Two, b.Weights.WithTrunk},
			length:  b.Length, start: b.Start, end: b.End}
		t.cherry.canopy = o.parseCherryCanopy(r.CherryCanopy)
		if t.cherry.canopy == nil {
			t.cherry.canopy = o.parseCherryCanopy(b.Canopy.Cherry)
		}
	case r.Mega != nil:
		mg := r.Mega
		t.trunkKind, trunkBlock, trunkDeco = trunkMega, mg.Block, mg.Deco
		// Defaults from the constructor (sub_4CB1F60).
		t.mega = megaTrunk{width: 2, base: 5, slope: 0.5}
		if mg.Width != nil {
			t.mega.width = *mg.Width
		}
		if mg.Height.Base != 0 || len(mg.Height.Intervals) > 0 {
			t.mega.base, t.mega.intervals = mg.Height.Base, mg.Height.Intervals
		}
		if b := mg.Branches; b != nil {
			t.mega.branchLen = b.Length
			if b.Slope != nil {
				t.mega.slope = *b.Slope
			}
			if b.Interval != nil {
				t.mega.branchInterval = *b.Interval
			}
			t.mega.altMin, t.mega.altMax = b.Altitude.Min, b.Altitude.Max
			if b.Canopy.Mega != nil {
				c, err := o.parseMegaCanopy(b.Canopy.Mega, false)
				if err != nil {
					return nil, err
				}
				t.mega.branchCanopy = c
			}
		}
	case r.Fallen != nil:
		f := r.Fallen
		t.trunkKind, trunkBlock, trunkDeco, logDeco = trunkFallen, f.Block, f.Deco, f.LogDeco
		// VERIFY: the JSON names of the two optional ranges (constructor
		// fields +16 and +24) were not read from the schema; vanilla files
		// set neither.
		t.height, t.modifier, t.stump = intRange{5, 8}, intRange{0, 0}, intRange{1, 1}
		if f.Length != nil {
			t.height = *f.Length
		}
		if f.Modifier != nil {
			t.modifier = *f.Modifier
		}
		if f.Stump != nil {
			t.stump = *f.Stump
		}
	default:
		return nil, nil
	}
	if bc := r.BaseCluster; bc != nil {
		t.clusters, t.clusterRadius, t.clusterReplace = bc.Clusters, bc.Radius, parseMatcher(bc.MayReplace)
	}
	if logDeco != "" {
		t.logDeco = &featureRef{logDeco, set}
	}
	var ok bool
	if t.log, ok = o.lookupBlock(trunkBlock); !ok {
		return nil, fmt.Errorf("trunk block %s missing", trunkBlock.Name)
	}
	var err error
	if t.trunkDeco, err = o.parseDecoration(trunkDeco); err != nil {
		return nil, err
	}
	if t.trunkKind == trunkFallen || t.trunkKind == trunkCherry {
		return t, nil
	}
	if t.trunkKind == trunkAcacia {
		switch {
		case r.AcaciaCanopy != nil:
			c, err := o.parseAcaciaCanopy(r.AcaciaCanopy)
			if err != nil {
				return nil, err
			}
			t.canopy, t.acaciaCanopy = canopyAcacia, c
		case r.RoofedCanopy != nil:
			rc := r.RoofedCanopy
			c := &roofedCanopy{height: rc.Height, outer: rc.Outer, inner: rc.Inner}
			if rc.Core != nil {
				c.core, c.hasCore = *rc.Core, true
			}
			if c.leaf, ok = o.lookupBlock(rc.Leaf); !ok {
				return nil, fmt.Errorf("leaf block %s missing", rc.Leaf.Name)
			}
			t.canopy, t.roofedCanopy = canopyRoofed, c
		case r.SpreadCanopy != nil:
			sc := r.SpreadCanopy
			c := &randomSpreadCanopy{height: sc.Height, radius: sc.Radius, attempts: sc.Attempts}
			for _, e := range sc.Leaves {
				if len(e) != 2 {
					return nil, fmt.Errorf("leaf entry %s", e)
				}
				var b biome.Block
				var wgt float32
				if json.Unmarshal(e[0], &b) != nil || json.Unmarshal(e[1], &wgt) != nil {
					return nil, fmt.Errorf("leaf entry %s", e)
				}
				id, ok := o.lookupBlock(b)
				if !ok {
					return nil, fmt.Errorf("leaf block %s missing", b.Name)
				}
				c.leaves, c.weights = append(c.leaves, id), append(c.weights, wgt)
			}
			t.canopy, t.spreadCanopy = canopyRandomSpread, c
		}
		return t, nil
	}
	if t.trunkKind == trunkMega {
		var err error
		switch {
		case r.MegaCanopy != nil:
			t.megaCanopy, err = o.parseMegaCanopy(r.MegaCanopy, false)
		case r.MegaPineCanopy != nil:
			t.megaCanopy, err = o.parseMegaCanopy(r.MegaPineCanopy, true)
		}
		if err != nil {
			return nil, err
		}
		return t, nil
	}
	var leaf biome.Block
	switch {
	case r.FancyCanopy != nil:
		t.canopy, leaf = canopyFancy, r.FancyCanopy.Leaf
		t.fancyHeight, t.fancyRadius = r.FancyCanopy.Height, r.FancyCanopy.Radius
	case r.PoplarCanopy != nil:
		c := r.PoplarCanopy
		t.canopy, leaf = canopyPoplar, c.Leaf
		for _, e := range c.Radius {
			t.radii = append(t.radii, [2]int32{e.Value, e.Weight})
		}
		t.canopyHeight = intRange{5, 6}
		if c.Height != nil {
			t.canopyHeight = *c.Height
		}
		t.holeChance = c.Hole
		if t.branch, ok = o.lookupBlock(c.Branch); !ok {
			return nil, fmt.Errorf("branch block %s missing", c.Branch.Name)
		}
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
	switch t.trunkKind {
	case trunkPoplar:
		return t.poplarTrunk(w, x, y, z, h, r)
	case trunkFallen:
		return t.fallenTrunk(w, x, y, z, h, r)
	case trunkAcacia:
		return t.acaciaTrunk(w, x, y, z, t.acacia.height(r), r)
	case trunkCherry:
		return t.cherryTrunk(w, x, y, z, t.cherry.height(r), r)
	case trunkMangrove:
		return t.mangrovePlace(w, x, y, z, r)
	case trunkMega:
		return t.megaTrunk(w, x, y, z, t.mega.megaHeight(r), r)
	case trunkFancy:
		return t.fancyTrunk(w, x, y, z, t.fancy.base+r.IntN(t.fancy.variance), r)
	}
	return t.trunk(w, x, y, z, h, r)
}

// placeBase replaces the block under a trunk with the first base_block
// unless it already is one (sub_D298330).
func (t *treeFeature) placeBase(w *featureWorld, x, y, z int32) {
	if len(t.baseBlocks) == 0 {
		return
	}
	if b, ok := w.block(x, y, z); ok && !containsID(t.baseBlocks, b) {
		w.set(x, y, z, t.baseBlocks[0])
	}
}

// replaces reports whether a trunk or canopy may put a block where b is:
// anything when may_replace is empty, else what it lists.
func (t *treeFeature) replaces(b uint32, ok bool) bool {
	return t.above.empty() || (ok && t.above.match(t.o, b))
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
	t.placeBase(w, x, y-1, z)
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
	case blockSnowLayer, blockShortGrass, blockTallGrass, blockFern, "minecraft:large_fern", blockDeadBush, blockVine:
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
	case canopyPoplar:
		return t.poplarCanopy(w, x, y, z, r)
	case canopyFancy:
		return t.fancyCanopy(w, x, y, z)
	}
	return false
}

// simpleCanopyRadius is the simple canopy's radius dy blocks above the
// trunk top.
func (t *treeFeature) simpleCanopyRadius(dy int32, slope float32) int32 {
	return t.minWidth + int32(float32(t.offMax)*slope) - int32(float32(dy)*slope)
}

// simpleCanopy is SimpleTreeCanopy (sub_D2DD1B0).
func (t *treeFeature) simpleCanopy(w *featureWorld, x, top, z int32, r *rand.MT) bool {
	slope := float32(t.rise) / float32(t.run)
	placed := false
	for ly := top + t.offMin; ly <= top+t.offMax; ly++ {
		dy := ly - top
		rad := t.simpleCanopyRadius(dy, slope)
		for bx := x - rad; bx <= x+rad; bx++ {
			for bz := z - rad; bz <= z+rad; bz++ {
				if mathx.Abs(bx-x) == rad && mathx.Abs(bz-z) == rad {
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
			rad := t.simpleCanopyRadius(ly-top, slope)
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
			if cutCorners && rad != 0 && mathx.Abs(bx-x) == rad && mathx.Abs(bz-z) == rad {
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

type rawMegaCanopy struct {
	Height     intRange    `json:"canopy_height"`
	BaseRadius int32       `json:"base_radius"`
	CoreWidth  *int32      `json:"core_width"`
	Simplify   bool        `json:"simplify_canopy"`
	Step       float32     `json:"radius_step_modifier"`
	Leaf       biome.Block `json:"leaf_block"`
}

func (o *Overworld) parseMegaCanopy(raw *rawMegaCanopy, pine bool) (*megaCanopy, error) {
	c := &megaCanopy{pine: pine, height: raw.Height, baseRadius: raw.BaseRadius, simplify: raw.Simplify, step: raw.Step}
	if raw.CoreWidth != nil {
		c.coreWidth, c.hasCore = *raw.CoreWidth, true
	}
	var ok bool
	if c.leaf, ok = o.lookupBlock(raw.Leaf); !ok {
		return nil, fmt.Errorf("leaf block %s missing", raw.Leaf.Name)
	}
	return c, nil
}

type rawAcaciaCanopy struct {
	Size     int32       `json:"canopy_size"`
	Simplify bool        `json:"simplify_canopy"`
	Leaf     biome.Block `json:"leaf_block"`
}

func (o *Overworld) parseAcaciaCanopy(raw *rawAcaciaCanopy) (*acaciaCanopy, error) {
	c := &acaciaCanopy{size: raw.Size, simplify: raw.Simplify}
	var ok bool
	if c.leaf, ok = o.lookupBlock(raw.Leaf); !ok {
		return nil, fmt.Errorf("leaf block %s missing", raw.Leaf.Name)
	}
	return c, nil
}
