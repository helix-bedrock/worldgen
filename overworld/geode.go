package overworld

import (
	"encoding/json"
	"math"

	"github.com/helix-bedrock/worldgen/noise"
	"github.com/helix-bedrock/worldgen/rand"
	"github.com/helix-bedrock/worldgen/structure"
)

// geodeFeature is GeodeFeature (place: sub_51C22E0, per-column lambda
// sub_51C4910). Written from BDS 1.26.52; the BDS quirks kept here are noted
// where they differ from Java's GeodeFeature.
type geodeFeature struct {
	filler, inner, alternate, middle, outer uint32
	placements                              [][6]uint32 // per placement, the state for each face
	outerMin, outerMax                      int32
	pointsMin, pointsMax                    int32
	offsetMin, offsetMax                    int32
	radius                                  int32
	crackOffset                             int32
	noiseMul, potentialChance, altChance    float32
	requireAlt                              bool
	invalid                                 map[uint32]bool
	notReplaceable                          map[uint32]bool
}

// geodeFaceNames are the minecraft:block_face values of BDS faces 0-5.
var geodeFaceNames = [6]string{"down", "up", "north", "south", "west", "east"}

// geodeBudFaces is the order BDS tries the faces for a bud (dword_15400DC).
var geodeBudFaces = [6]int{1, 0, 2, 3, 4, 5}

// rawGeode is the JSON of a minecraft:geode_feature.
type rawGeode struct {
	Placements []struct {
		Name string `json:"name"`
	} `json:"inner_placements"`
	MinOuter    int32   `json:"min_outer_wall_distance"`
	MaxOuter    int32   `json:"max_outer_wall_distance"`
	MinPoints   int32   `json:"min_distribution_points"`
	MaxPoints   int32   `json:"max_distribution_points"`
	MinOffset   int32   `json:"min_point_offset"`
	MaxOffset   int32   `json:"max_point_offset"`
	Radius      int32   `json:"max_radius"`
	CrackOffset float64 `json:"crack_point_offset"`
	NoiseMul    float32 `json:"noise_multiplier"`
	Potential   float32 `json:"use_potential_placements_chance"`
	AltChance   float32 `json:"use_alternate_layer0_chance"`
	RequireAlt  bool    `json:"placements_require_layer0_alternate"`
}

// rawGeodeLayers is the layer block names of a geode feature.
type rawGeodeLayers struct {
	Filler    string `json:"filler"`
	Inner     string `json:"inner_layer"`
	Alternate string `json:"alternate_inner_layer"`
	Middle    string `json:"middle_layer"`
	Outer     string `json:"outer_layer"`
}

func (o *Overworld) parseGeode(raw json.RawMessage) (placer, error) {
	var r rawGeode
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	var names rawGeodeLayers
	if err := json.Unmarshal(raw, &names); err != nil {
		return nil, err
	}
	f := &geodeFeature{filler: o.blockID(names.Filler), inner: o.blockID(names.Inner), alternate: o.blockID(names.Alternate),
		middle: o.blockID(names.Middle), outer: o.blockID(names.Outer), outerMin: r.MinOuter, outerMax: r.MaxOuter,
		pointsMin: r.MinPoints, pointsMax: r.MaxPoints, offsetMin: r.MinOffset, offsetMax: r.MaxOffset,
		radius: r.Radius, crackOffset: int32(r.CrackOffset), noiseMul: r.NoiseMul, potentialChance: r.Potential,
		altChance: r.AltChance, requireAlt: r.RequireAlt, invalid: map[uint32]bool{}, notReplaceable: map[uint32]bool{}}
	for _, p := range r.Placements {
		def, ok := o.reg.Default(p.Name)
		if !ok {
			continue
		}
		var faces [6]uint32
		for i, n := range geodeFaceNames {
			faces[i] = o.withProp(def, "minecraft:block_face", n)
		}
		f.placements = append(f.placements, faces)
	}
	// Points fail on bedrock, packed ice and blue ice by name, and on the
	// air, water, lava and ice materials (list xmmword_F4571C0: 0, 5, 6, 13).
	// VERIFY: the ice material taken as ice and frosted ice.
	for i := 0; i < o.reg.Len(); i++ {
		b := uint32(i)
		s, _ := o.reg.State(b)
		switch s.Name {
		case "minecraft:bedrock", "minecraft:packed_ice", "minecraft:blue_ice", "minecraft:air",
			blockWater, "minecraft:flowing_water", "minecraft:lava", "minecraft:flowing_lava",
			"minecraft:ice", "minecraft:frosted_ice":
			f.invalid[b] = true
		}
		for _, t := range o.reg.Tags(s.Name) {
			if t == "minecraft:not_feature_replaceable" {
				f.notReplaceable[b] = true
			}
		}
	}
	return f, nil
}

// fastInvSqrt is the bit-trick inverse square root BDS uses for the geode
// distance sums (0x5F3759DF and one Newton step), not a true square root.
func fastInvSqrt(x float32) float32 {
	y := math.Float32frombits(0x5F3759DF - math.Float32bits(x)>>1)
	return float32(float32(float32(float32(x*-0.5)*y)*y)+1.5) * y
}

// rangeDraw is BDS's draw for the geode's int ranges: min plus
// nextInt(max - min), so max itself is never drawn.
func rangeDraw(r *rand.MT, lo, hi int32) int32 {
	if hi > lo {
		return lo + r.IntN(hi-lo)
	}
	return lo
}

type geodePoint struct {
	x, y, z int32
	weight  uint32
}

// geodeRun is one placement's state.
type geodeRun struct {
	f      *geodeFeature
	w      *featureWorld
	r      *rand.MT
	n      *noise.Normal
	points []geodePoint
	cracks []geodePoint
	crack  bool
	// filling, inner, middle, outer and crack thresholds.
	thFill, thInner, thMiddle, thOuter, thCrack float32
	ops                                         []geodeOp
	spots                                       []blockPos
	placed                                      bool // BDS's result flag, set by a crack block
	oz                                          int32
}

type geodeOp struct {
	x, y, z int32
	b       uint32
}

// insideStructure reports whether a block is inside a structure piece BDS
// knows when placing a geode.
// VERIFY: BDS asks the dimension for the structure at the position
// (vfunc +336 of the structure source); taken here as ancient city and
// mineshaft piece boxes.
func (o *Overworld) insideStructure(x, y, z int32) bool {
	p := [3]int32{x, y, z}
	in := false
	o.forCityPieces(x>>4, z>>4, func(c *structure.Piece) bool {
		if c.BB.Contains(p) {
			in = true
			return false
		}
		return true
	})
	if in {
		return true
	}
	cx, cz := x>>4, z>>4
	for sx := cx - msReach; sx <= cx+msReach; sx++ {
		for sz := cz - msReach; sz <= cz+msReach; sz++ {
			ms := o.mineshaftAt(sx, sz)
			if ms == nil || !ms.bb.Contains(p) {
				continue
			}
			for _, q := range ms.pieces {
				if q.bb.Contains(p) {
					return true
				}
			}
		}
	}
	return false
}

func (f *geodeFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	o := w.o
	if o.insideStructure(x, y, z) {
		return blockPos{}, false
	}
	n := rangeDraw(r, f.pointsMin, f.pointsMax)
	g := &geodeRun{f: f, w: w, r: r, oz: z}
	g.n = noise.NewNormalMT(r, -4, []float32{1})
	d := float32(n) / float32(f.outerMax)
	// The filling threshold is 1/sqrt(1.7) as a constant.
	g.thFill = math.Float32frombits(1061443537)
	g.thInner = invSqrt32(d + 2.2)
	g.thMiddle = invSqrt32(d + 3.2)
	g.thOuter = invSqrt32(d + 4.2)
	crackBase := float32(r.Float()*0.5) + 2
	cd := d
	if n < 4 {
		cd = 0
	}
	g.thCrack = invSqrt32(cd + crackBase)
	// BDS compares with 0.95 directly (generate_crack_chance is not read).
	g.crack = r.Float() < 0.95
	// The invalid-block counter starts from the float bits of d instead of
	// 0, so the first point on an invalid block already fails.
	for i := int32(0); i < n; i++ {
		px := x + rangeDraw(r, f.outerMin, f.outerMax)
		py := y + rangeDraw(r, f.outerMin, f.outerMax)
		pz := z + rangeDraw(r, f.outerMin, f.outerMax)
		if b, _ := w.block(px, py, pz); f.invalid[b] {
			return blockPos{}, false
		}
		g.points = append(g.points, geodePoint{px, py, pz, uint32(rangeDraw(r, f.offsetMin, f.offsetMax))})
	}
	if g.crack {
		m := 2*n + 1
		var offs [3][3]int32
		switch r.IntN(4) {
		case 0:
			offs = [3][3]int32{{m, 7, 0}, {m, 5, 0}, {m, 1, 0}}
		case 1:
			offs = [3][3]int32{{0, 7, m}, {0, 5, m}, {0, 1, m}}
		case 2:
			offs = [3][3]int32{{m, 7, m}, {m, 5, m}, {m, 1, m}}
		default:
			offs = [3][3]int32{{0, 7, 0}, {0, 5, 0}, {0, 1, 0}}
		}
		for _, c := range offs {
			g.cracks = append(g.cracks, geodePoint{x + c[0], y + c[1], z + c[2], 0})
		}
	}
	g.sweep(x, y)
	for _, op := range g.ops {
		if b, _ := w.block(op.x, op.y, op.z); !f.notReplaceable[b] {
			w.set(op.x, op.y, op.z, op.b)
		}
	}
	if len(f.placements) > 0 {
		for _, s := range g.spots {
			p := f.placements[r.IntN(int32(len(f.placements)))]
			for _, face := range geodeBudFaces {
				st := faceStep[face]
				bx, by, bz := s[0]+st[0], s[1]+st[1], s[2]+st[2]
				nb, _ := w.block(bx, by, bz)
				if nb != o.ids.air && !o.waterMaterial(nb) {
					continue
				}
				// VERIFY: the bud's survival test (sub_C747C90) is taken as
				// the block it hangs from being sturdy toward it.
				if hold, _ := w.block(s[0], s[1], s[2]); !o.sturdyFace(hold, face) {
					continue
				}
				if !f.notReplaceable[nb] {
					w.set(bx, by, bz, p[face])
				}
				break
			}
		}
	}
	return blockPos{x, y, z}, true
}

// sweep walks the columns in BDS's order: x from the origin up, then from
// one below the origin down; for each x, y up from the origin and then down
// from one below it. Once a crack block is placed, a row with nothing in
// it ends the y walk, and an x with nothing in it ends the x walk.
func (g *geodeRun) sweep(ox, oy int32) {
	R := g.f.radius
	found := false
	if R >= 0 {
		for x := ox; x <= ox+R; x++ {
			if g.column(x, oy, &found) {
				break
			}
		}
	}
	for x := ox; x > ox-R; {
		x--
		if g.column(x, oy, &found) {
			break
		}
	}
}

// column walks the rows of column x, up from oy and then down from one
// below it. found is set once a crack block has been placed; column
// reports whether the x walk should stop: a crack was found and nothing
// was in this column.
func (g *geodeRun) column(x, oy int32, found *bool) bool {
	R := g.f.radius
	hit := false
	var crackUp, crackDown bool
	for y := oy; ; y++ {
		if g.row(x, y, &hit, &crackUp) || y >= oy+R {
			break
		}
	}
	for y := oy; y > oy-R; {
		y--
		if g.row(x, y, &hit, &crackDown) {
			break
		}
	}
	if crackUp || crackDown {
		*found = true
	}
	return !hit && *found
}

// row is the per-row lambda (sub_51C4910) over z from -radius to +radius
// around the origin. It reports whether the walk should stop here.
func (g *geodeRun) row(x, y int32, hit, crackSeen *bool) bool {
	f := g.f
	R := f.radius
	seen := false
	fx, fy := float32(x), float32(y)
	for z := g.oz - R; z <= g.oz+R; z++ {
		nv := g.n.Sample(fx, fy, float32(z)) * f.noiseMul
		var s float32
		for _, p := range g.points {
			dx, dy, dz := int64(x-p.x), int64(y-p.y), int64(z-p.z)
			s += fastInvSqrt(float32(p.weight) + float32(dx*dx+dy*dy+dz*dz))
		}
		v := float32(float32(len(g.points))*nv) + s
		last := z == g.oz+R
		if g.thOuter > v {
			if last {
				if !seen {
					if *crackSeen {
						*hit = true
						return true
					}
					return false
				}
				*hit = true
				return false
			}
			continue
		}
		g.cell(x, y, z, v, nv)
		if g.placed {
			*crackSeen = true
			seen = true
			if last {
				*hit = true
				return false
			}
			continue
		}
		if last {
			if !seen {
				if *crackSeen {
					*hit = true
					return true
				}
				return false
			}
			*hit = true
			return false
		}
	}
	return false
}

// cell handles one block at value v (already at least the outer threshold).
func (g *geodeRun) cell(x, y, z int32, v, nv float32) {
	f, w, r := g.f, g.w, g.r
	if g.crack {
		var c float32
		for _, p := range g.cracks {
			dx, dy, dz := int64(x-p.x), int64(y-p.y), int64(z-p.z)
			c += fastInvSqrt(float32(dx*dx+dy*dy+dz*dz) + float32(f.crackOffset))
		}
		if float32(float32(len(g.cracks))*nv)+c >= g.thCrack && g.thFill > v {
			if b, _ := w.block(x, y, z); !f.notReplaceable[b] {
				w.set(x, y, z, w.o.ids.air)
				g.placed = true
			}
			return
		}
	}
	switch {
	case v >= g.thFill:
		g.ops = append(g.ops, geodeOp{x, y, z, f.filler})
	case v >= g.thInner:
		alt := f.altChance > r.Float()
		if alt {
			g.ops = append(g.ops, geodeOp{x, y, z, f.alternate})
		} else {
			g.ops = append(g.ops, geodeOp{x, y, z, f.inner})
			if f.requireAlt {
				return
			}
		}
		if f.potentialChance > r.Float() {
			g.spots = append(g.spots, blockPos{x, y, z})
		}
	case v >= g.thMiddle:
		g.ops = append(g.ops, geodeOp{x, y, z, f.middle})
	default:
		g.ops = append(g.ops, geodeOp{x, y, z, f.outer})
	}
}

// invSqrt32 is 1/sqrt(v) rounded to float32.
func invSqrt32(v float32) float32 { return float32(1 / float32(math.Sqrt(float64(v)))) }
