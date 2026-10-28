package overworld

import (
	"math"

	"github.com/helix-bedrock/worldgen/1.26.60/internal/mathx"

	"github.com/helix-bedrock/worldgen/1.26.60/rand"
)

// Speleothems: the code-registered pointed dripstone, dripstone cluster and
// large dripstone features (BDS 1.26.52 SpeleothemFeature sub_49C3DD0,
// SpeleothemClusterFeature sub_49C2D60, LargeSpeleothemFeature sub_51D0860,
// set up in sub_4E2BB50; in 1.26.60 the cluster is sub_4BC4520, the large
// feature sub_53FAB40, set up in sub_4C2EBE0). Sulfur caves and the ice
// caves' icicles use the same classes with their own blocks.

// speleothemBlocks are the blocks a speleothem feature places and grows
// from.
type speleothemBlocks struct {
	base, tip uint32
	// replace is what the base block may replace (dirt, coarse dirt, stone,
	// tuff and deepslate for dripstone, xmmword_F60D630).
	replace []uint32
	// tips are the pointed blocks a large speleothem builds over (pointed
	// dripstone, sulfur spike and icicle, xmmword_FF122D8).
	tips []uint32
	// attach, when set, is applied to the first block of a pointed column,
	// the one against the ceiling or floor (icicle clusters set
	// attached_bit, the std::function of SpeleothemClusterFeature).
	attach func(uint32) uint32
}

// Dripstone thickness states by BDS's index (sub_4DE8B70).
func dripstoneThickness() [5]string {
	return [5]string{"tip", "frustum", "middle", "base", "merge"}
}

const (
	thickTip = iota
	thickFrustum
	thickMiddle
	thickBase
	thickMerge
)

func (o *Overworld) isLava(b uint32) bool { return b == o.ids.lava || b == o.ids.flowingLava }

// emptyOrWater is air or a water-material block.
func (o *Overworld) emptyOrWater(b uint32) bool { return b == o.ids.air || o.waterMaterial(b) }

// tipState is the pointed block hanging down or standing up with a
// thickness.
func (s *speleothemBlocks) tipState(o *Overworld, up bool, thick int) uint32 {
	b := o.withProp(s.tip, "hanging", !up)
	return o.withProp(b, "dripstone_thickness", dripstoneThickness()[thick])
}

// growTip grows a pointed column of height blocks from (x, y, z), up or
// down, base first and the tip last, stopping at the first block that is
// not air or water (sub_4DE8DA0).
func (s *speleothemBlocks) growTip(w *featureWorld, x, y, z int32, up bool, height int32, merge bool) {
	o := w.o
	if height <= 0 {
		return
	}
	if b, _ := w.block(x, y, z); !o.emptyOrWater(b) || o.isLava(b) {
		return
	}
	tip := thickTip
	if merge {
		tip = thickMerge
	}
	first := thickBase
	switch height {
	case 2:
		first = thickFrustum
	case 1:
		first = tip
	}
	st := s.tipState(o, up, first)
	if s.attach != nil {
		st = s.attach(st)
	}
	w.set(x, y, z, st)
	dy := int32(-1)
	if up {
		dy = 1
	}
	y += dy
	for n := height; n > 1; n-- {
		if b, _ := w.block(x, y, z); !o.emptyOrWater(b) || o.isLava(b) {
			return
		}
		th := thickMiddle
		switch n {
		case 3:
			th = thickFrustum
		case 2:
			th = tip
		}
		w.set(x, y, z, s.tipState(o, up, th))
		y += dy
	}
}

// replaceBase puts the base block at (x, y, z) if it may replace what is
// there.
func (s *speleothemBlocks) replaceBase(w *featureWorld, x, y, z int32) {
	if b, _ := w.block(x, y, z); inList(s.replace, b) {
		w.set(x, y, z, s.base)
	}
}

// pointedFeature is one pointed dripstone with a patch of dripstone blocks
// at its root (sub_49C3DD0).
type pointedFeature struct {
	speleothemBlocks
}

func (f *pointedFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	o := w.o
	// The tip points away from a block it can grow from: up from one
	// below, down from one above, a coin flip when both are (sub_4DE9050).
	below, _ := w.block(x, y-1, z)
	above, _ := w.block(x, y+1, z)
	var up bool
	switch b, a := f.grows(below), f.grows(above); {
	case b && a:
		up = nextBool(r)
	case b:
		up = true
	case a:
		up = false
	default:
		return blockPos{}, false
	}
	ry := y + 1
	if up {
		ry = y - 1
	}
	f.replaceBase(w, x, ry, z)
	for _, st := range faceStep() {
		if r.Float() < 0.3 {
			continue
		}
		p := blockPos{x + st[0], ry + st[1], z + st[2]}
		f.replaceBase(w, p[0], p[1], p[2])
		if nextBool(r) {
			continue
		}
		d := faceStep()[r.IntN(6)]
		p = blockPos{p[0] + d[0], p[1] + d[1], p[2] + d[2]}
		f.replaceBase(w, p[0], p[1], p[2])
		if nextBool(r) {
			continue
		}
		d = faceStep()[r.IntN(6)]
		p = blockPos{p[0] + d[0], p[1] + d[1], p[2] + d[2]}
		f.replaceBase(w, p[0], p[1], p[2])
	}
	tall := r.Float()
	ty := y - 1
	if up {
		ty = y + 1
	}
	h := int32(1)
	if b, _ := w.block(x, ty, z); o.emptyOrWater(b) && tall < 0.2 {
		h = 2
	}
	f.growTip(w, x, y, z, up, h, false)
	return blockPos{x, ry, z}, true
}

// columnScan finds the ceiling above and the floor below (x, y, z) within
// rng blocks through blocks that pass cont, ending at blocks that pass
// surface (sub_D2D3600). ok is false when the start itself fails cont.
func columnScan(w *featureWorld, x, y, z, rng int32, cont, surface func(uint32) bool) (ceil, floor int32, hasCeil, hasFloor, ok bool) {
	b, _ := w.block(x, y, z)
	if !cont(b) {
		return 0, 0, false, false, false
	}
	if i, ok := columnWalk(w, x, y, z, rng, 1, cont, surface); ok {
		ceil, hasCeil = y+i+1, true
	}
	if i, ok := columnWalk(w, x, y, z, rng, -1, cont, surface); ok {
		floor, hasFloor = y-i-1, true
	}
	return ceil, floor, hasCeil, hasFloor, true
}

// clusterFeature is a patch of stalactites, stalagmites, base block layers
// and small pools (BDS 1.26.60 SpeleothemClusterFeature sub_4BC4520, set up
// through sub_4C41670). Since 1.26.60 every setting comes from the
// registration: dripstone uses height 3-5, radius 2-7 on each axis, wetness
// clamped normal (0.1, 0.3, 0.1, 1.0), density 0.3-0.7 and a scan range of
// 12; icicles use radius 6, no wetness, a scan range of 32, ceilings only
// and air only.
type clusterFeature struct {
	speleothemBlocks
	stone []uint32 // what a pool may border (stone, deepslate, tuff)
	// minHeight and maxHeight bound the tallest point (3 and 5 for
	// dripstone and icicles, 1 and 4 for sulfur).
	minHeight, maxHeight int32
	// The pool chance is a gaussian times wetScale plus wetBase, clamped
	// to [wetMin, wetMax] (0.1, 0.3, 0.1, 1 for dripstone; all 0 for sulfur
	// and icicles, which never pool).
	wetBase, wetScale, wetMin, wetMax float32
	// minRadius and maxRadius bound the patch's half size on each axis.
	minRadius, maxRadius int32
	// minDensity and maxDensity bound the chance of a column getting a
	// pointed block.
	minDensity, maxDensity float32
	// scan is how far a column looks for its ceiling and floor.
	scan int32
	// sides picks which ends of a column grow.
	sides clusterSides
	// airOnly makes water count as a wall: the feature starts and grows in
	// air only (icicles).
	airOnly bool
}

// clusterSides are the column ends a cluster grows from (BDS's mode at
// +84 of the feature).
type clusterSides int32

const (
	// clusterBoth grows stalactites and stalagmites.
	clusterBoth clusterSides = iota
	// clusterFloor drops the ceiling of every column.
	clusterFloor
	// clusterCeiling drops the floor of every column (icicles).
	clusterCeiling
)

// newDripstoneCluster returns the dripstone and sulfur cluster settings
// that did not change from 1.26.52.
func newDripstoneCluster(b speleothemBlocks, stone []uint32, minH, maxH int32) *clusterFeature {
	return &clusterFeature{speleothemBlocks: b, stone: stone, minHeight: minH, maxHeight: maxH,
		minRadius: 2, maxRadius: 7, minDensity: 0.3, maxDensity: 0.7, scan: 12, sides: clusterBoth}
}

// open is the cluster's test for room to grow: air, or air and water.
func (f *clusterFeature) open(o *Overworld) func(uint32) bool {
	if f.airOnly {
		return func(b uint32) bool { return b == o.ids.air }
	}
	return o.emptyOrWater
}

// randRange draws min + n with n in [0, max-min], as BDS draws a range; an
// empty range draws nothing.
func randRange(r *rand.MT, lo, hi int32) int32 {
	if hi >= lo {
		return lo + r.IntN(hi-lo+1)
	}
	return lo
}

func (f *clusterFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	o := w.o
	open := f.open(o)
	if b, _ := w.block(x, y, z); !open(b) {
		return blockPos{}, false
	}
	height := randRange(r, f.minHeight, f.maxHeight)
	wet := float32(float32(r.GaussianFloat()*f.wetScale) + f.wetBase)
	if f.wetMax < wet {
		wet = f.wetMax
	} else {
		wet = max(wet, f.wetMin)
	}
	density := r.Float()
	var xr, zr int32
	if f.maxRadius >= f.minRadius {
		xr = randRange(r, f.minRadius, f.maxRadius)
		zr = randRange(r, f.minRadius, f.maxRadius)
	} else {
		xr, zr = f.minRadius, f.minRadius
	}
	if xr < 0 || zr < 0 {
		return blockPos{}, true
	}
	density = float32(float32(density*(f.maxDensity-f.minDensity)) + f.minDensity)
	wall := speleothemWall{&f.speleothemBlocks, o}.solid
	for dx := -xr; dx <= xr; dx++ {
		for dz := -zr; dz <= zr; dz++ {
			k := min(xr-mathx.Abs(dx), zr-mathx.Abs(dz))
			v := float32(float64(float32(k)) / 3.0)
			// The column chance rises from 0.1 at the edge to 1 three
			// blocks in.
			chance := float64(float32(0.1))
			if v >= 0 {
				chance = 1
				if v <= 1 {
					chance = float64(float32(float32(v*0.9) + 0.1))
				}
			}
			cx, cz := x+dx, z+dz
			ceil, floor, hasCeil, hasFloor, ok := columnScan(w, cx, y, cz, f.scan, open, wall)
			if !ok || !hasCeil && !hasFloor {
				continue
			}
			switch f.sides {
			case clusterFloor:
				hasCeil = false
			case clusterCeiling:
				hasFloor = false
			}
			if !hasCeil && !hasFloor {
				continue
			}
			if wet > r.Float() && hasFloor && f.canPool(w, cx, floor, cz) {
				w.set(cx, floor, cz, o.ids.water)
				floor--
			}
			var stal, stag int32
			if d := r.Float64(); hasCeil && chance > d {
				if b, _ := w.block(cx, ceil, cz); !o.isLava(b) {
					f.layers(w, cx, ceil, cz, 1, r.IntN(2)+1)
					maxH := height
					if hasFloor {
						maxH = min(height, ceil-floor)
					}
					if r.Float() <= density {
						half := float32(maxH) * 0.5
						stal = clusterHeight(r, mathx.Abs(dz)+mathx.Abs(dx), maxH, float32(float32((0-half)*(float32(mathx.Abs(dz)+mathx.Abs(dx))*0.125))+half))
					}
				}
			}
			adjust := hasCeil && hasFloor
			if b, _ := w.block(cx, floor, cz); chance <= r.Float64() || !hasFloor || o.isLava(b) {
				stag = 0
			} else {
				f.layers(w, cx, floor, cz, -1, r.IntN(2)+1)
				if hasCeil {
					stag = max(stal+r.IntN(3)-1, 0)
				} else {
					adjust = false
					if r.Float() <= density {
						half := float32(height) * 0.5
						t := float32(mathx.Abs(dx)+mathx.Abs(dz)) * 0.125
						stag = clusterHeight(r, mathx.Abs(dx)+mathx.Abs(dz), height, float32(float32((0-half)*t)+half))
					}
				}
			}
			if adjust && ceil-stal <= stag+floor {
				lo := max(floor+1, ceil-stal)
				hi := min(stag+floor, ceil-1)
				if lo <= hi+1 {
					lo += r.IntN(hi - lo + 2)
				}
				stag, stal = lo-floor-1, ceil-lo
			}
			// Since 1.26.60 each column is cut to the open blocks it has
			// before the merge is decided.
			if hasCeil {
				stal = openRun(w, cx, ceil-1, cz, -1, stal, open)
			}
			if hasFloor {
				stag = openRun(w, cx, floor+1, cz, 1, stag, open)
			}
			merge := nextBool(r) && hasCeil && stal > 0 && stag > 0 && hasFloor && stal+stag == ceil-floor-1
			if hasCeil {
				f.growTip(w, cx, ceil-1, cz, false, stal, merge)
			}
			if hasFloor {
				f.growTip(w, cx, floor+1, cz, true, stag, merge)
			}
		}
	}
	return blockPos{x, y, z}, true
}

// openRun counts the open blocks from (x, y, z) along dy, up to n.
func openRun(w *featureWorld, x, y, z, dy, n int32, open func(uint32) bool) int32 {
	for i := int32(0); i < n; i++ {
		if b, _ := w.block(x, y+i*dy, z); !open(b) {
			return i
		}
	}
	return max(n, 0)
}

// layers lays up to n+1 base blocks from (x, y, z) into the ceiling (dy 1)
// or floor (dy -1), stopping at a block it may not replace.
func (f *clusterFeature) layers(w *featureWorld, x, y, z, dy, n int32) {
	for k := int32(0); ; k++ {
		if b, _ := w.block(x, y, z); !inList(f.replace, b) {
			return
		}
		w.set(x, y, z, f.base)
		y += dy
		if k == n {
			return
		}
	}
}

// canPool reports whether the floor block at (x, y, z) may become water:
// not already water or dripstone, no water above, held in by stone,
// deepslate, tuff or water on its four sides and below (sub_4DE87E0).
func (f *clusterFeature) canPool(w *featureWorld, x, y, z int32) bool {
	o := w.o
	b, _ := w.block(x, y, z)
	if b == f.base || b == f.tip || o.isWater(b) {
		return false
	}
	if a, _ := w.block(x, y+1, z); o.isWater(a) {
		return false
	}
	for _, p := range [5][3]int32{{0, 0, -1}, {1, 0, 0}, {0, 0, 1}, {-1, 0, 0}, {0, -1, 0}} {
		n, _ := w.block(x+p[0], y+p[1], z+p[2])
		if !inList(f.stone, n) && !o.waterMaterial(n) {
			return false
		}
	}
	return true
}

// largeFeature is a large stalactite and stalagmite pair between a floor
// and ceiling (sub_51D0860). Settings, hard-coded: search range 30, column
// radius 3-18 up to 0.33 of the cave height, stalactite bluntness 0.3-0.9,
// stalagmite bluntness 0.4-1.0, height scale 0.4-2.0, wind 0-0.3 when the
// radius is at least 4 and both bluntnesses at least 0.6.
type largeFeature struct {
	speleothemBlocks
}

// largeTip is one half of a large speleothem.
type largeTip struct {
	x, y, z int32
	up      bool
	radius  int32
	blunt   float32
	scale   float32
}

// wind leans a large speleothem: blocks move by the wind times their
// distance from the origin height.
type wind struct {
	on     bool
	y      int32
	dx, dz float32
}

func (wd *wind) offset(x, y, z int32) (int32, int32, int32) {
	if !wd.on {
		return x, y, z
	}
	d := float32(wd.y - y)
	return x + int32(float32(wd.dx*d)), y, z + int32(float32(d*wd.dz))
}

// largeHeight is the height of a large speleothem at a distance from its
// axis (sub_4DE8AB0).
func largeHeight(dist, radius, scale, blunt float32) float64 {
	x := float64(float32(float64(float32(max(blunt, dist)/radius)) * 0.384))
	a := math.Pow(x, 4.0/3.0) * 0.75
	b := math.Pow(x, 2.0/3.0)
	return float64(radius) * (math.Max((a-b+math.Log(x)*(-1.0/3.0))*float64(scale), 0) / 0.384)
}

func (f *largeFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	o := w.o
	if b, _ := w.block(x, y, z); !o.emptyOrWater(b) {
		return blockPos{}, false
	}
	ceil, floor, hasCeil, hasFloor, ok := columnScan(w, x, y, z, 30, o.emptyOrWater, speleothemWall{&f.speleothemBlocks, o}.solid)
	if !ok || !hasCeil || !hasFloor {
		return blockPos{}, false
	}
	h := ceil - floor - 1
	if h < 4 {
		return blockPos{}, false
	}
	maxR := min(max(int32(float32(h)*0.33), 3), 18)
	radius := 3 + r.IntN(maxR-3+1)
	top := largeTip{x: x, y: ceil - 1, z: z, radius: radius}
	top.blunt = lerpRounded(r.Float(), 0.3, 0.9)
	top.scale = lerpRounded(r.Float(), 0.4, 2.0)
	bot := largeTip{x: x, y: floor + 1, z: z, up: true, radius: radius}
	bot.blunt = lerpRounded(r.Float(), 0.4, 1.0)
	bot.scale = lerpRounded(r.Float(), 0.4, 2.0)
	var wd wind
	if radius >= 4 && top.blunt >= 0.6 && bot.blunt >= 0.6 {
		speed := lerpRounded(r.Float(), 0, 0.3)
		a := float32(float32(float32(r.Float()*pi32)+0) * sinTableRadian)
		wd = wind{on: true, y: y, dx: float32(sinTable[uint16(int32(float32(a+sinTableQuarter)))] * speed), dz: float32(speed * sinTable[uint16(int32(a))])}
	}
	okTop := f.moveBack(w, &top, &wd)
	okBot := f.moveBack(w, &bot, &wd)
	placed := false
	if okTop && top.y-int32(float32(largeHeight(0, float32(top.radius), top.scale, top.blunt))) > MinY {
		f.build(w, &top, &wd, r)
		placed = true
	}
	if okBot && bot.y+int32(float32(largeHeight(0, float32(bot.radius), bot.scale, bot.blunt))) < MaxY {
		f.build(w, &bot, &wd, r)
		placed = true
	}
	return blockPos{x, y, z}, placed
}

// moveBack moves a half back into the rock until its base fits, halving
// the radius when it cannot (sub_51D0DE0).
func (f *largeFeature) moveBack(w *featureWorld, t *largeTip, wd *wind) bool {
	o := w.o
	if t.radius < 2 {
		return false
	}
	for {
		h := int32(largeHeight(0, float32(t.radius), t.scale, max(t.blunt, 0)))
		n := min(int32(10), h)
		for j := int32(0); h > 0 && j < n; j++ {
			dy := j
			if t.up {
				dy = -j
			}
			py := t.y + dy
			if b, _ := w.block(t.x, py, t.z); o.isLava(b) {
				return false
			}
			if f.fits(w, t.x, py, t.z, t.radius, wd) {
				t.y = py
				return true
			}
		}
		old := t.radius
		t.radius /= 2
		if old <= 3 {
			return false
		}
	}
}

// fits reports whether the base fits at (x, y, z): solid there and on a
// ring of the radius around it (sub_4DE8490), tested after the wind moved
// the spot.
func (f *largeFeature) fits(w *featureWorld, x, y, z, radius int32, wd *wind) bool {
	o := w.o
	x, y, z = wd.offset(x, y, z)
	if b, _ := w.block(x, y, z); o.openOrLava(b) {
		return false
	}
	rf := float32(radius)
	step := float32(6.0 / float64(rf))
	for a := float32(0); ; {
		i := float32(a * sinTableRadian)
		qz := z + int32(float32(sinTable[uint16(int32(i))]*rf))
		qx := x + int32(float32(sinTable[uint16(int32(float32(i+sinTableQuarter)))]*rf))
		if b, _ := w.block(qx, y, qz); o.openOrLava(b) {
			return false
		}
		a = float32(a + step)
		if a >= twoPi32 {
			return true
		}
	}
}

// build places a half's columns, widest at the base (sub_51D1030).
func (f *largeFeature) build(w *featureWorld, t *largeTip, wd *wind, r *rand.MT) {
	o := w.o
	dy := int32(-1)
	if t.up {
		dy = 1
	}
	rf := float32(t.radius)
	for dx := -t.radius; dx <= t.radius; dx++ {
		for dz := -t.radius; dz <= t.radius; dz++ {
			dist := float32(math.Sqrt(float64(float32(dx*dx + dz*dz))))
			if !(dist <= rf) {
				continue
			}
			h := int32(float32(largeHeight(dist, rf, t.scale, t.blunt)))
			if h <= 0 {
				continue
			}
			if r.Float() < 0.2 {
				h = int32(float32(float32(float32(r.Float()*0.19999999)+0.8) * float32(h)))
			}
			cx, cz := t.x+dx, t.z+dz
			limit := int32(math.MaxInt32)
			if t.up {
				limit = w.heightmap(cx, cz)
			}
			if h <= 0 || limit <= t.y {
				continue
			}
			entered := false
			y := t.y
			for k := int32(0); ; {
				px, py, pz := wd.offset(cx, y, cz)
				b, _ := w.block(px, py, pz)
				switch {
				case o.emptyOrWater(b) || o.isLava(b) || inList(f.tips, b):
					w.set(px, py, pz, f.base)
					entered = true
				case entered:
					k = h
				}
				if k++; k >= h {
					break
				}
				y += dy
				if limit <= y {
					break
				}
			}
		}
	}
}

// grows reports whether a speleothem can grow from block b.
func (s *speleothemBlocks) grows(b uint32) bool { return inList(s.replace, b) || b == s.base }

// speleothemWall is the surface test of a speleothem column scan: blocks
// it grows from, and lava.
type speleothemWall struct {
	s *speleothemBlocks
	o *Overworld
}

func (v speleothemWall) solid(b uint32) bool { return v.s.grows(b) || v.o.isLava(b) }

// openOrLava reports whether a block is air, water or lava.
func (o *Overworld) openOrLava(b uint32) bool { return o.emptyOrWater(b) || o.isLava(b) }

// lerpRounded is lo + t*(hi-lo), each step rounded to float32 as BDS does.
func lerpRounded(t, lo, hi float32) float32 { return float32(float32(t*float32(hi-lo)) + lo) }

// columnWalk walks from (x, y, z) in steps of dy through at most rng-1
// blocks that pass cont. It returns the steps taken and whether the block
// it stopped at passes surface.
func columnWalk(w *featureWorld, x, y, z, rng, dy int32, cont, surface func(uint32) bool) (int32, bool) {
	py := y + dy
	var i int32
	for i < rng-1 {
		b, _ := w.block(x, py, z)
		if !cont(b) {
			break
		}
		py += dy
		i++
	}
	b, _ := w.block(x, py, z)
	return i, surface(b)
}

// clusterHeight is the length of a cluster speleothem dist blocks from
// the centre: a gaussian times 3 plus bias (no bias past 8 blocks), at
// most maxH.
func clusterHeight(r *rand.MT, dist int32, maxH int32, bias float32) int32 {
	t := float32(dist) * 0.125
	g := r.GaussianFloat()
	var b float32
	if !(1 < t) {
		b = bias
	}
	v := float32(float32(g*3) + b)
	if float32(maxH) < v {
		return maxH
	}
	return int32(max(v, 0))
}
