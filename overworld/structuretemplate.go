package overworld

import (
	"encoding/json"
	"strings"
	"sync"

	"github.com/helix-bedrock/worldgen/rand"
	"github.com/helix-bedrock/worldgen/structure"
)

// structureTemplateFeature places an .mcstructure template (BDS's
// StructureTemplateFeature, place sub_D317830, written from BDS 1.26.52): a
// random quarter turn, around the template's centre when
// rotate_around_center is set, with layer ground_level at the origin. The
// origin, or a spot on a spiral out to adjustment_radius, must pass every
// constraint: leveled (sub_D2818D0), unburied (sub_D281FD0) and
// block_intersection (sub_D280A90). The constraints test their cells
// relative to the origin itself, not shifted down by ground_level as the
// placed blocks are.
type structureTemplateFeature struct {
	o      *Overworld
	path   string
	radius int32
	ground int32
	center bool

	leveled, unburied, intersect bool
	allow                        blockMatcher
	// motionOnly is only_check_intersection_for_motion_blocking_blocks.
	motionOnly bool

	once sync.Once
	t    *structure.MCStructure
	// The constraint cells, in template coordinates, built from the
	// template once (the constraint constructors sub_D281340, sub_D281A60,
	// sub_D280350).
	level, open, solidCells, otherCells [][3]int32
}

// rawStructureTemplate is the JSON of a minecraft:structure_template_feature.
type rawStructureTemplate struct {
	Name        string `json:"structure_name"`
	Radius      int32  `json:"adjustment_radius"`
	Ground      int32  `json:"ground_level"`
	Center      bool   `json:"rotate_around_center"`
	Constraints struct {
		Leveled   *struct{} `json:"leveled"`
		Unburied  *struct{} `json:"unburied"`
		Intersect *struct {
			MotionOnly bool              `json:"only_check_intersection_for_motion_blocking_blocks"`
			Allow      []json.RawMessage `json:"block_allowlist"`
		} `json:"block_intersection"`
	} `json:"constraints"`
}

func (o *Overworld) parseStructureTemplate(raw json.RawMessage) (placer, error) {
	var r rawStructureTemplate
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	name, ok := strings.CutPrefix(r.Name, "minecraft:")
	if !ok {
		return nil, nil
	}
	f := &structureTemplateFeature{o: o, path: "minecraft/" + name, radius: r.Radius, ground: r.Ground, center: r.Center,
		leveled: r.Constraints.Leveled != nil, unburied: r.Constraints.Unburied != nil}
	if c := r.Constraints.Intersect; c != nil {
		f.intersect, f.motionOnly, f.allow = true, c.MotionOnly, parseMatcher(c.Allow)
	}
	return f, nil
}

// load reads the template and builds the constraint cells. A template that
// fails to load leaves the feature placing nothing.
func (f *structureTemplateFeature) load() {
	t, err := f.o.st.LoadMCStructure(f.path)
	if err != nil {
		return
	}
	f.t = t
	o := f.o
	g := f.groundLayer()
	for x := int32(0); x < t.Size[0]; x++ {
		for z := int32(0); z < t.Size[2]; z++ {
			// Leveled: the ground under every filled cell of the ground
			// layer. Unburied: open air over every filled top cell.
			if !o.voidOrAir(t.Cell(x, g, z)) {
				f.level = append(f.level, [3]int32{x, -1, z})
			}
			if !o.voidOrAir(t.Cell(x, t.Size[1]-1, z)) {
				f.open = append(f.open, [3]int32{x, t.Size[1], z})
			}
		}
	}
	for x := int32(0); x < t.Size[0]; x++ {
		for y := int32(0); y < t.Size[1]; y++ {
			for z := int32(0); z < t.Size[2]; z++ {
				b := t.Cell(x, y, z)
				switch {
				case b == structure.VoidCell:
				case o.solidMaterial(b):
					f.solidCells = append(f.solidCells, [3]int32{x, y, z})
				default:
					f.otherCells = append(f.otherCells, [3]int32{x, y, z})
				}
			}
		}
	}
}

// groundLayer is ground_level clamped into the template's height.
func (f *structureTemplateFeature) groundLayer() int32 {
	if f.ground < 0 {
		return 0
	}
	return min(f.ground, max(f.t.Size[1], 2)-1)
}

// rotate turns a template offset by rot quarter turns as BDS does (90:
// (x, z) to (-z, x)).
func rotate(x, z int32, rot int) (int32, int32) {
	switch rot {
	case 1:
		return -z, x
	case 2:
		return -x, -z
	case 3:
		return z, -x
	}
	return x, z
}

func (f *structureTemplateFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	f.once.Do(f.load)
	if f.t == nil {
		return blockPos{}, false
	}
	rot := int(r.IntN(4))
	var px, pz int32
	if f.center {
		px, pz = -(f.t.Size[0] / 2), -(f.t.Size[2] / 2)
	}
	// The spiral of candidate offsets: runs of 1, 1, 2, 2, 3, 3... steps,
	// turning a quarter after each run.
	n := (2*f.radius + 1) * (2*f.radius + 1)
	var dx, dz int32
	sx, sz := int32(1), int32(0)
	run, tried := int32(1), int32(0)
	for tried < n {
		steps := max(run, 1)
		for range steps {
			if f.fits(w, x+dx, y, z+dz, rot, px, pz) {
				f.build(w, x+dx, y, z+dz, rot, px, pz)
				return blockPos{x + dx, y, z + dz}, true
			}
			dx, dz = dx+sx, dz+sz
		}
		if sz != 0 {
			run++
		}
		sx, sz = -sz, sx
		tried += steps
	}
	return blockPos{}, false
}

// at turns a template cell into a world position for an origin, rotation
// and pivot, as the constraints do.
func at(c [3]int32, x, y, z int32, rot int, px, pz int32) (int32, int32, int32) {
	rx, rz := rotate(c[0]+px, c[2]+pz, rot)
	return x + rx, y + c[1], z + rz
}

func (f *structureTemplateFeature) fits(w *featureWorld, x, y, z int32, rot int, px, pz int32) bool {
	o := w.o
	if f.leveled && !f.isLeveled(w, x, y, z, rot, px, pz) {
		return false
	}
	if f.unburied {
		for _, c := range f.open {
			wx, wy, wz := at(c, x, y, z, rot, px, pz)
			if b, _ := w.block(wx, wy, wz); b != o.ids.air {
				return false
			}
		}
	}
	if f.intersect {
		if !f.cellsAllowed(w, f.solidCells, x, y, z, rot, px, pz) || !f.motionOnly && !f.cellsAllowed(w, f.otherCells, x, y, z, rot, px, pz) {
			return false
		}
	}
	return true
}

// isLeveled reports whether, under every filled ground cell, the top of the
// ground (a solid block with a non-solid one above) is within two blocks of
// the cell below the ground layer.
// VERIFY: BDS's solid test here is material flags 1 and 3 and block flag
// 0x40000; solidMaterial stands in for it.
func (f *structureTemplateFeature) isLeveled(w *featureWorld, x, y, z int32, rot int, px, pz int32) bool {
	const tolerance = 2
	for _, c := range f.level {
		wx, wy, wz := at(c, x, y, z, rot, px, pz)
		wy -= tolerance
		cur := w.solidMaterialAt(wx, wy, wz)
		for k := -tolerance; ; k++ {
			prev := cur
			if k > tolerance {
				return false
			}
			wy++
			cur = w.solidMaterialAt(wx, wy, wz)
			if !cur && prev {
				break
			}
		}
	}
	return true
}

// build places the template's blocks, layer ground_level at the origin's
// height. Void cells keep the world's block.
func (f *structureTemplateFeature) build(w *featureWorld, x, y, z int32, rot int, px, pz int32) {
	t := f.t
	g := f.groundLayer()
	for tx := int32(0); tx < t.Size[0]; tx++ {
		for ty := int32(0); ty < t.Size[1]; ty++ {
			for tz := int32(0); tz < t.Size[2]; tz++ {
				b := t.Cell(tx, ty, tz)
				if b == structure.VoidCell {
					continue
				}
				wx, wy, wz := at([3]int32{tx, ty - g, tz}, x, y, z, rot, px, pz)
				w.set(wx, wy, wz, b)
			}
		}
	}
}

// voidOrAir reports whether a template cell is void or air.
func (o *Overworld) voidOrAir(b uint32) bool { return b == structure.VoidCell || b == o.ids.air }

// cellsAllowed reports whether every world block under template cells is
// one the template may intersect.
func (f *structureTemplateFeature) cellsAllowed(w *featureWorld, cells [][3]int32, x, y, z int32, rot int, px, pz int32) bool {
	for _, c := range cells {
		wx, wy, wz := at(c, x, y, z, rot, px, pz)
		if b, _ := w.block(wx, wy, wz); !f.allow.match(w.o, b) {
			return false
		}
	}
	return true
}
