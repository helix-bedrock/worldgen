package worldgen

import (
	"encoding/json"
	"fmt"

	"github.com/helix-bedrock/worldgen/biome"
	"github.com/helix-bedrock/worldgen/rand"
)

// Mangrove trees, written from BDS 1.26.52: MangroveTreeRoots (sub_D2D7D50, root walk
// sub_D2D8900), MangroveTreeTrunk (sub_D2D8C90, height sub_D2D9630) and
// MangroveTreeCanopy (sub_D2D6F90). The tree feature draws the trunk height, lets the
// roots lift the trunk's base, then grows trunk and canopy (sub_D2E8DE0). The shapes
// are Java's mangrove root placer, upwards-branching trunk and random spread foliage,
// drawn with Bedrock's random.

type mangroveTree struct {
	// trunk
	base, randA, randB    int32
	branchLen, branchStep intRange // inclusive ranges
	// roots
	rootWidth, rootLength int32
	rootOffset            intRange // max exclusive
	root, muddyRoot, mud  uint32
	aboveRoot             uint32
	aboveChance           chance
	hasAbove              bool
	rootsThrough          blockMatcher
	// canopy
	canopyHeight, canopyRadius intRange
	attempts                   int32
	leaves                     []uint32
	weights                    []float32
	leafDeco                   decoration
	hanging                    uint32
	hangingChance              chance
	hasHanging                 bool
}

type rawMangrove struct {
	Roots *struct {
		Width     int32             `json:"max_root_width"`
		Length    int32             `json:"max_root_length"`
		Offset    intRange          `json:"y_offset"`
		Root      biome.Block       `json:"root_block"`
		Muddy     biome.Block       `json:"muddy_root_block"`
		Mud       biome.Block       `json:"mud_block"`
		Through   []json.RawMessage `json:"roots_may_grow_through"`
		AboveRoot *struct {
			Block  biome.Block `json:"above_root_block"`
			Chance chance      `json:"above_root_chance"`
		} `json:"above_root"`
	} `json:"mangrove_roots"`
	Trunk *struct {
		Block  biome.Block `json:"trunk_block"`
		Height struct {
			Base  int32 `json:"base"`
			RandA int32 `json:"height_rand_a"`
			RandB int32 `json:"height_rand_b"`
		} `json:"trunk_height"`
		Branches struct {
			Length intRange `json:"branch_length"`
			Steps  intRange `json:"branch_steps"`
		} `json:"branches"`
	} `json:"mangrove_trunk"`
	Canopy *struct {
		Height   intRange            `json:"canopy_height"`
		Radius   intRange            `json:"canopy_radius"`
		Attempts int32               `json:"leaf_placement_attempts"`
		Leaves   [][]json.RawMessage `json:"leaf_blocks"`
		Deco     *rawDecoration      `json:"canopy_decoration"`
		Hanging  *biome.Block        `json:"hanging_block"`
		Chance   *chance             `json:"hanging_block_placement_chance"`
	} `json:"mangrove_canopy"`
}

// parseMangrove fills the mangrove parts of t; ok is false when the tree has no
// mangrove trunk.
func (o *Overworld) parseMangrove(raw json.RawMessage, t *treeFeature) (bool, error) {
	var r rawMangrove
	if err := json.Unmarshal(raw, &r); err != nil {
		return false, err
	}
	if r.Trunk == nil {
		return false, nil
	}
	m := &mangroveTree{base: r.Trunk.Height.Base, randA: r.Trunk.Height.RandA, randB: r.Trunk.Height.RandB,
		branchLen: r.Trunk.Branches.Length, branchStep: r.Trunk.Branches.Steps}
	log, ok := o.lookupBlock(r.Trunk.Block)
	if !ok {
		return false, fmt.Errorf("trunk block %s missing", r.Trunk.Block.Name)
	}
	t.log = log
	if rt := r.Roots; rt != nil {
		m.rootWidth, m.rootLength, m.rootOffset = rt.Width, rt.Length, rt.Offset
		m.root, _ = o.lookupBlock(rt.Root)
		m.muddyRoot, _ = o.lookupBlock(rt.Muddy)
		m.mud, _ = o.lookupBlock(rt.Mud)
		m.rootsThrough = parseMatcher(rt.Through)
		if a := rt.AboveRoot; a != nil {
			m.aboveRoot, m.hasAbove = o.lookupBlock(a.Block)
			m.aboveChance = a.Chance
		}
	}
	if c := r.Canopy; c != nil {
		m.canopyHeight, m.canopyRadius, m.attempts = c.Height, c.Radius, c.Attempts
		for _, e := range c.Leaves {
			if len(e) != 2 {
				return false, fmt.Errorf("leaf entry %s", e)
			}
			var b biome.Block
			var wgt float32
			if json.Unmarshal(e[0], &b) != nil || json.Unmarshal(e[1], &wgt) != nil {
				return false, fmt.Errorf("leaf entry %s", e)
			}
			id, ok := o.lookupBlock(b)
			if !ok {
				return false, fmt.Errorf("leaf block %s missing", b.Name)
			}
			m.leaves, m.weights = append(m.leaves, id), append(m.weights, wgt)
		}
		var err error
		if m.leafDeco, err = o.parseDecoration(c.Deco); err != nil {
			return false, err
		}
		if c.Hanging != nil && c.Chance != nil {
			m.hanging, m.hasHanging = o.lookupBlock(*c.Hanging)
			m.hangingChance = *c.Chance
		}
	}
	t.trunkKind, t.mangrove = trunkMangrove, m
	return true, nil
}

// rootFree is the root placement test: roots_may_grow_through, else what a trunk may
// grow into. VERIFY: BDS's fallback reads the replaceable flag and material types.
func (t *treeFeature) rootFree(w *featureWorld, x, y, z int32) bool {
	b, ok := w.block(x, y, z)
	if !ok {
		return false
	}
	m := t.mangrove
	if m.rootsThrough.empty() {
		return true
	}
	return m.rootsThrough.match(t.o, b) || t.o.treeFree(b) || t.o.hasBlockTag(b, "leaves")
}

func (t *treeFeature) mangrovePlace(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	m := t.mangrove
	h := m.base + r.IntN(m.randA+1) + r.IntN(m.randB+1)
	ox, oy, oz := x, y, z
	if m.root != 0 {
		var ok bool
		if ox, oy, oz, ok = t.mangroveRoots(w, x, y, z, h, r); !ok {
			return blockPos{}, false
		}
	}
	return t.mangroveTrunk(w, ox, oy, oz, h, r)
}

// rootDirs are the four sides the roots spread to: north, east, south, west.
var rootDirs = [4][2]int32{{0, -1}, {1, 0}, {0, 1}, {-1, 0}}

func (t *treeFeature) mangroveRoots(w *featureWorld, x, y, z, h int32, r *rand.MT) (int32, int32, int32, bool) {
	m := t.mangrove
	off := m.rootOffset.draw(r)
	ty := y + off
	if !t.valid(w, x, y, z, off+h) {
		return 0, 0, 0, false
	}
	for cy := y; cy < ty; cy++ {
		if !t.rootFree(w, x, cy, z) {
			return 0, 0, 0, false
		}
	}
	roots := []blockPos{{x, ty - 1, z}}
	for _, d := range rootDirs {
		start := blockPos{x + d[0], ty, z + d[1]}
		var list []blockPos
		t.mangroveRootWalk(w, start, d, blockPos{x, ty, z}, &list, 0, r)
		if len(list) == 0 || len(list) > int(m.rootLength) {
			return 0, 0, 0, false
		}
		roots = append(roots, list...)
		roots = append(roots, start)
	}
	for _, p := range roots {
		if !t.rootFree(w, p[0], p[1], p[2]) {
			continue
		}
		if b, _ := w.block(p[0], p[1], p[2]); b == m.mud {
			w.set(p[0], p[1], p[2], m.muddyRoot)
			continue
		}
		w.set(p[0], p[1], p[2], m.root)
		if m.hasAbove && m.aboveChance.roll(r) {
			if b, ok := w.block(p[0], p[1]+1, p[2]); ok && b == w.o.ids.air {
				w.set(p[0], p[1]+1, p[2], m.aboveRoot)
			}
		}
	}
	return x, ty, z, true
}

// mangroveRootWalk is the recursive root simulation: from pos the root goes down, or
// out and down near the width limit, with a 0.2 skew chance; it fails (empties the
// list) past the length limit.
func (t *treeFeature) mangroveRootWalk(w *featureWorld, pos blockPos, d [2]int32, origin blockPos, list *[]blockPos, layer int32, r *rand.MT) bool {
	m := t.mangrove
	if layer == m.rootLength || len(*list) > int(m.rootLength) {
		*list = (*list)[:0]
		return false
	}
	below := blockPos{pos[0], pos[1] - 1, pos[2]}
	side := blockPos{pos[0] + d[0], pos[1], pos[2] + d[1]}
	dist := absI(origin[0]-pos[0]) + absI(origin[1]-pos[1]) + absI(origin[2]-pos[2])
	var cands []blockPos
	if m.rootWidth-3 >= dist || m.rootWidth < dist {
		c := below
		if m.rootWidth >= dist && r.Float() >= 0.2 && r.Uint32()&0x8000000 != 0 {
			c = side
		}
		cands = []blockPos{c}
	} else {
		cands = []blockPos{below}
		if r.Float() < 0.2 {
			cands = append(cands, blockPos{side[0], side[1] - 1, side[2]})
		}
	}
	for _, c := range cands {
		if !t.rootFree(w, c[0], c[1], c[2]) {
			continue
		}
		*list = append(*list, c)
		if !t.mangroveRootWalk(w, c, d, origin, list, layer+1, r) {
			return false
		}
	}
	return true
}

// mangroveTrunk grows the trunk with a branch chance at each log below the top, then
// the canopy around the top and every branch log.
func (t *treeFeature) mangroveTrunk(w *featureWorld, x, y, z, h int32, r *rand.MT) (blockPos, bool) {
	m := t.mangrove
	var spots []blockPos
	for i := range h {
		ly := y + i
		if t.acaciaFree(w, x, ly, z) {
			w.set(x, ly, z, t.log)
			// VERIFY: BDS decides each branch with a coin flip (Random
			// vfunc +40) and does not read branch_chance.
			if i < h-1 && r.Uint32()&0x8000000 != 0 {
				dir := r.IntN(4)
				k := m.branchLen.drawIncl(r)
				l := k - m.branchLen.drawIncl(r) - 1
				steps := m.branchStep.drawIncl(r)
				var dx, dz int32
				switch dir {
				case 0:
					dz = -1
				case 1:
					dz = 1
				case 2:
					dx = -1
				case 3:
					dx = 1
				}
				j := max(0, l)
				endY := j + ly
				bx, bz := x, z
				if l < h && steps > 0 {
					for {
						if j != 0 {
							bx, bz = bx+dx, bz+dz
							endY = j + ly
							if t.acaciaFree(w, bx, j+ly, bz) {
								w.set(bx, j+ly, bz, t.log)
								endY++
							}
							spots = append(spots, blockPos{bx, j + ly, bz})
						}
						j++
						if j >= h {
							break
						}
						steps--
						if steps <= 0 {
							break
						}
					}
				}
				if endY-ly > 1 {
					spots = append(spots, blockPos{bx, endY, bz}, blockPos{bx, endY - 2, bz})
				}
			}
		}
		if i == h-1 {
			spots = append(spots, blockPos{x, ly + 1, z})
		}
	}
	if len(m.leaves) > 0 {
		t.mangroveCanopy(w, spots, r)
	}
	return blockPos{x, y, z}, true
}

func (t *treeFeature) mangroveCanopy(w *featureWorld, spots []blockPos, r *rand.MT) {
	m := t.mangrove
	ch := m.canopyHeight.drawAlways(r)
	cr := m.canopyRadius.drawAlways(r)
	var placed []blockPos
	for _, p := range spots {
		for range m.attempts {
			total := int32(0)
			for _, wgt := range m.weights {
				total = int32(float32(total) + wgt)
			}
			v := int32(0)
			if total != 0 {
				v = r.IntN(total)
			}
			i := 0
			v = int32(float32(v) - m.weights[0])
			for v >= 0 && i+1 < len(m.leaves) {
				i++
				v = int32(float32(v) - m.weights[i])
			}
			a := r.IntN(cr)
			px := p[0] + a - r.IntN(cr)
			e := r.IntN(ch)
			py := p[1] + e - r.IntN(ch)
			g := r.IntN(cr)
			pz := p[2] + g - r.IntN(cr)
			if t.acaciaFree(w, px, py, pz) {
				w.set(px, py, pz, m.leaves[i])
				placed = append(placed, blockPos{px, py, pz})
			}
		}
	}
	if len(placed) == 0 {
		return
	}
	for i := 1; i < len(placed); i++ {
		if j := int(r.Uint32() % uint32(i+1)); j != i {
			placed[i], placed[j] = placed[j], placed[i]
		}
	}
	for _, p := range placed {
		m.leafDeco.apply(w, p[0], p[1], p[2], r)
	}
	if !m.hasHanging {
		return
	}
	// Hanging propagules: two air blocks below a leaf, and none next to
	// another (BDS defaults, as Java's attached-to-leaves decorator:
	// 2 empty blocks, exclusion radius 1 across and 0 up).
	// VERIFY: the three numbers are read as Java's; their BDS fields
	// (+468, +472, +476) were not traced to a constructor.
	taken := map[blockPos]bool{}
	for _, p := range placed {
		below := blockPos{p[0], p[1] - 1, p[2]}
		if taken[below] {
			continue
		}
		if !m.hangingChance.roll(r) {
			continue
		}
		free := true
		for k := int32(1); k <= 2; k++ {
			if b, ok := w.block(p[0], p[1]-k, p[2]); !ok || b != w.o.ids.air {
				free = false
				break
			}
		}
		if !free {
			continue
		}
		for dx := int32(-1); dx <= 1; dx++ {
			for dz := int32(-1); dz <= 1; dz++ {
				taken[blockPos{below[0] + dx, below[1], below[2] + dz}] = true
			}
		}
		w.set(below[0], below[1], below[2], m.hanging)
	}
}
