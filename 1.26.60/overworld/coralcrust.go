package overworld

import (
	"fmt"
	"sync"

	"github.com/helix-bedrock/worldgen/1.26.60/rand"
	"github.com/helix-bedrock/worldgen/1.26.60/structure"
)

// coralCrustFeature lays the dead coral crusts and outcroppings of warm
// oceans from the coralcrust templates and grows live coral on their data
// markers (CoralCrustFeature, sub_4990390; marker pass sub_4991AA0).
type coralCrustFeature struct {
	coral *coralFeature
	// crusts and outcrops hold each template's palette per turn, with the
	// template's coral_block turned into its dead coral colour.
	crusts   [5]crustTemplate
	outcrops [6]crustTemplate
}

type crustTemplate struct {
	path string
	once [4]sync.Once
	conv [4][]structure.ConvertedState
}

// crustProbe and crustOrigin are BDS's per-crust, per-turn offsets
// (dword_1491D54..qword_1491DF0): the column the crust is tested on, and
// from it back to the template's corner. Turn 3 is BDS's fallback value.
func crustProbe() [5][4][2]int32 {
	return [5][4][2]int32{
		{{6, 6}, {-7, 6}, {-7, -7}, {6, -7}},
		{{6, 6}, {-6, 6}, {-7, -7}, {6, -7}},
		{{4, 4}, {-4, 4}, {-4, -4}, {4, -4}},
		{{4, 4}, {-4, 4}, {-4, -4}, {4, -4}},
		{{3, 6}, {-7, 3}, {-2, -7}, {7, -3}},
	}
}
func crustOrigin() [5][4][2]int32 {
	return [5][4][2]int32{
		{{-6, -6}, {7, -6}, {7, 7}, {-6, 7}},
		{{-6, -6}, {6, -6}, {7, 7}, {-6, 7}},
		{{-4, -4}, {4, -4}, {4, 4}, {-4, 4}},
		{{-4, -4}, {4, -4}, {4, 4}, {-4, 4}},
		{{-3, -6}, {7, -3}, {2, 7}, {-7, 3}},
	}
}

// outcropOffset is the outcropping's corner from the probe column, by
// outcropping and turn (sub_4991970).
func outcropOffset() [6][4][3]int32 {
	return [6][4][3]int32{
		{{-5, 0, -2}, {3, 0, -5}, {5, 0, 2}, {-2, 0, 5}},
		{{-7, -2, -3}, {3, -2, -6}, {6, -2, 3}, {-2, -2, 5}},
		{{-4, 0, -3}, {3, 0, -4}, {4, 0, 3}, {-3, 0, 4}},
		{{-3, 0, -3}, {3, 0, -3}, {3, 0, 3}, {-3, 0, 3}},
		{{-2, 0, -2}, {2, 0, -2}, {2, 0, 2}, {-2, 0, 2}},
		{{-5, 0, -5}, {6, 0, -6}, {5, 0, 5}, {-5, 0, 5}},
	}
}

func newCoralCrust(cf *coralFeature) *coralCrustFeature {
	f := &coralCrustFeature{coral: cf}
	for i := range f.crusts {
		f.crusts[i].path = fmt.Sprintf("coralcrust/crust%d", i+1)
	}
	for i := range f.outcrops {
		f.outcrops[i].path = fmt.Sprintf("coralcrust/outcropping%d", i+1)
	}
	return f
}

// palette converts a template's palette for a turn. BDS turns the
// templates' coral_block (variant dead_coral) into a dead coral block of a
// colour drawn from a random seeded with the milliseconds since the server
// started, once per template load (sub_D276010), so no two BDS runs agree;
// here the colour comes from the world seed and the template's name.
func (f *coralCrustFeature) palette(o *Overworld, t *crustTemplate, jt *structure.JigsawTemplate, rot int) []structure.ConvertedState {
	t.once[rot].Do(func() {
		conv := append([]structure.ConvertedState(nil), o.st.TemplateStates(jt, rot)...)
		h := uint32(2166136261)
		for i := 0; i < len(t.path); i++ {
			h = (h ^ uint32(t.path[i])) * 16777619
		}
		var r rand.MT
		r.Seed(uint32(o.seed) ^ uint32(o.seed>>32) ^ h)
		dead := f.coral.blocks[5+(r.Uint32()>>1)%5]
		for i, st := range jt.Template.Palette {
			if st.Name == "minecraft:coral_block" {
				conv[i] = structure.ConvertedState{ID: dead, OK: true}
			}
		}
		t.conv[rot] = conv
	})
	return t.conv[rot]
}

// crustPiece is one placed coral crust template: its variant, rotation
// and origin.
type crustPiece struct {
	crust, rot int32
	pos        blockPos
}

func (f *coralCrustFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	o := w.o
	crust, rot := r.IntN(5), r.IntN(4)
	pr := crustProbe()[crust][rot]
	p := blockPos{x + pr[0], y, z + pr[1]}
	limit := int32(SeaLevel - 9)
	for p[1] < limit {
		if b, ok := w.block(p[0], p[1], p[2]); ok && o.waterMaterial(b) {
			break
		}
		p[1]++
	}
	if p[1] >= limit {
		return blockPos{}, false
	}
	below, _ := w.block(p[0], p[1]-1, p[2])
	if at, _ := w.block(p[0], p[1], p[2]); !o.solidMaterial(below) || !o.waterMaterial(at) {
		return blockPos{}, false
	}
	if i := f.coral.coralIndex(below); i >= 0 && i < 5 {
		return blockPos{}, false
	}
	or := crustOrigin()[crust][rot]
	pieces := []crustPiece{{crust, rot, blockPos{p[0] + or[0], p[1], p[2] + or[1]}}}
	if r.IntN(4) != 0 {
		n := r.IntN(5) + 4
		for range n {
			c, cr := r.IntN(5), r.IntN(4)
			dx := r.IntN(9) + 3
			if r.IntN(2) == 0 {
				dx = -dx
			}
			dz := r.IntN(9) + 3
			if r.IntN(2) == 0 {
				dz = -dz
			}
			cp := crustProbe()[c][cr]
			q := blockPos{p[0] + dx + cp[0], p[1] - 1, p[2] + dz + cp[1]}
			if b, _ := w.block(q[0], q[1], q[2]); o.solidMaterial(b) {
				co := crustOrigin()[c][cr]
				pieces = append(pieces, crustPiece{c, cr, blockPos{q[0] + co[0], p[1], q[2] + co[1]}})
			}
		}
		if len(pieces) > 1 {
			for _, pc := range pieces {
				f.placeTemplate(w, &f.crusts[pc.crust], pc.pos, int(pc.rot), r)
			}
			return p, true
		}
	}
	// One crust alone, or a quarter of the time from the start: an
	// outcropping in the first crust's turn instead.
	oc := r.IntN(6)
	off := outcropOffset()[oc][rot]
	f.placeTemplate(w, &f.outcrops[oc], blockPos{p[0] + off[0], p[1] + off[1], p[2] + off[2]}, int(rot), r)
	return p, true
}

// placeTemplate places a template the way BDS's legacy templates go in,
// skipping structure blocks and jigsaws, then grows coral on its markers
// (the data structure blocks).
func (f *coralCrustFeature) placeTemplate(w *featureWorld, t *crustTemplate, at blockPos, rot int, r *rand.MT) {
	o := w.o
	jt := o.st.Template(t.path)
	if jt == nil {
		return
	}
	conv := f.palette(o, t, jt, rot)
	var markers []blockPos
	for _, tb := range jt.Template.Blocks {
		rp := structure.RotatePos(tb.Pos, rot)
		p := blockPos{at[0] + rp[0], at[1] + rp[1], at[2] + rp[2]}
		switch jt.Template.Palette[tb.State].Name {
		case blockStructureBlock:
			markers = append(markers, p)
			continue
		case blockJigsaw, blockStructureVoid:
			continue
		}
		if cs := conv[tb.State]; cs.OK {
			w.set(p[0], p[1], p[2], cs.ID)
		}
	}
	f.growMarkers(w, markers, r)
}

// growMarkers is BDS's marker pass (sub_4991AA0): live coral of one colour
// in short arms or a small cross on each marker, then fans on top and wall
// fans on the sides. VERIFY: BDS walks the markers in its hash map's
// order; they are taken in template order here.
func (f *coralCrustFeature) growMarkers(w *featureWorld, markers []blockPos, r *rand.MT) {
	cf := f.coral
	color := int(r.IntN(5))
	b := &coralBuild{f: cf, w: w, r: r, block: cf.blocks[color], color: color}
	for _, p := range markers {
		if !b.solidAt(up(p, -1)) {
			continue
		}
		if b.solidAt(p) {
			p = up(p, 1)
		}
		if r.IntN(4) != 0 {
			n := r.IntN(3)
			if !b.waterAt(p) {
				continue
			}
			b.put(p)
			axis := r.IntN(2)
			for i := int32(0); i <= n; i++ {
				var dir uint8
				switch {
				case axis != 0 && r.IntN(2) != 0:
					p[2]++
					dir = 2
				case axis != 0:
					p[2]--
					dir = 3
				case r.IntN(2) != 0:
					p[0]--
					dir = 5
				default:
					p[0]++
					dir = 4
				}
				if !b.underwater(p) || cf.coralIndex(b.at(p)) >= 0 {
					break
				}
				b.put(p)
				if r.IntN(4) == 0 {
					b.tops = append(b.tops, up(p, 1))
				}
				for _, k := range [3]int32{2, 2, 3} {
					if r.IntN(k) == 0 {
						b.sides = append(b.sides, coralSide{p, dir})
					}
				}
				axis ^= 1
			}
			continue
		}
		if !b.underwater(p) {
			continue
		}
		for _, d := range [4][2]int32{{1, 0}, {0, -1}, {-1, 0}, {0, 1}} {
			if q := (blockPos{p[0] + d[0], p[1], p[2] + d[1]}); cf.coralIndex(b.at(q)) < 0 {
				b.put(q)
			}
		}
		b.put(p)
		b.put(up(p, 1))
		if r.IntN(4) == 0 {
			b.tops = append(b.tops, up(p, 2))
		}
	}
	for _, p := range b.tops {
		if !b.waterAt(p) || b.waterAt(up(p, -1)) || !b.solidAt(up(p, -1)) {
			continue
		}
		fan := r.IntN(5)
		dir := r.IntN(2)
		w.set(p[0], p[1], p[2], cf.fans[fan][dir])
	}
	for _, s := range b.sides {
		base := b.at(s.pos)
		attach := cf.coralIndex(base) >= 0 || base == cf.stone
		row := coralExceptTable()[2]
		if s.dir >= 2 && s.dir <= 5 {
			row = coralExceptTable()[s.dir-2]
		}
		nd := row[r.IntN(3)]
		v := facingVec()[nd]
		t := blockPos{s.pos[0] + v[0], s.pos[1] + v[1], s.pos[2] + v[2]}
		if !b.waterAt(t) || !attach {
			continue
		}
		c := min(max(r.IntN(2)+2*r.IntN(3), 0), 4)
		w.set(t[0], t[1], t[2], cf.wallFans[c][wallFanDirection()[nd-2]])
	}
}

// solidAt reports whether the block at p has a solid material.
func (b *coralBuild) solidAt(p blockPos) bool { return b.w.o.solidMaterial(b.at(p)) }

// waterAt reports whether the block at p has water material.
func (b *coralBuild) waterAt(p blockPos) bool { return b.w.o.waterMaterial(b.at(p)) }
