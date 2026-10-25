package overworld

import (
	"math"

	"github.com/helix-bedrock/worldgen/1.26.52/internal/mathx"

	"github.com/helix-bedrock/worldgen/1.26.52/rand"
)

// Mega (2x2) tree parts of minecraft:tree_feature, written from BDS
// 1.26.52: MegaTreeTrunk (place sub_D2D9A00, height sub_D2DAAB0),
// MegaTreeCanopy (sub_D2D9880), MegaPineTreeCanopy (sub_D2D9670), their
// layer helper (sub_D299860) and the base cluster (sub_D298920 /
// sub_D298DB0) that rings giant spruces with podzol.

type megaTrunk struct {
	width     int32
	base      int32
	intervals []int32
	// branches (jungle giants)
	branchLen      int32
	branchInterval intRange
	slope          float32
	altMin, altMax float32
	branchCanopy   *megaCanopy
}

// megaCanopy is mega_canopy (pine=false) or mega_pine_canopy.
type megaCanopy struct {
	pine       bool
	height     intRange
	baseRadius int32
	coreWidth  int32
	hasCore    bool
	simplify   bool
	step       float32 // radius_step_modifier (pine)
	leaf       uint32
}

// megaHeight is base plus nextInt(interval) for each interval.
func (m *megaTrunk) megaHeight(r *rand.MT) int32 {
	h := m.base
	for _, iv := range m.intervals {
		h += r.IntN(iv)
	}
	return h
}

// megaLayer is sub_D299860: a layer of leaves around (x, y, z) extended by
// the core (ox, oz). Simplified layers keep dx^2+dz^2 <= r^2; others keep a
// block within r of any of the four core corners.
func (t *treeFeature) megaLayer(w *featureWorld, x, y, z, rad, ox, oz int32, simplify bool, leaf uint32) {
	r2 := rad * rad
	for dx := -rad; dx < rad+ox; dx++ {
		for dz := -rad; dz < rad+oz; dz++ {
			if simplify {
				if dx*dx+dz*dz > r2 {
					continue
				}
			} else if dz*dz+dx*dx > r2 && (dz-1)*(dz-1)+(dx-1)*(dx-1) > r2 &&
				dx*dx+(dz-1)*(dz-1) > r2 && (dx-1)*(dx-1)+dz*dz > r2 {
				continue
			}
			if b, ok := w.block(x+dx, y, z+dz); t.replaces(b, ok) {
				w.set(x+dx, y, z+dz, leaf)
			}
		}
	}
}

// place puts the canopy on a trunk top at (x, y, z); the trunk width
// (tw, tw) is the core unless core_width is set.
func (c *megaCanopy) place(t *treeFeature, w *featureWorld, x, y, z, tw int32, r *rand.MT) {
	h := c.height.draw(r)
	ox, oz := tw, tw
	if c.hasCore {
		ox, oz = c.coreWidth, c.coreWidth
	}
	if c.pine {
		fh := float32(h)
		prev := int32(0)
		for d := h; d >= 0; d-- {
			ly := y - d
			rad := c.baseRadius + int32(math.Floor(float64(float32(d)/fh*c.step)))
			bonus := int32(0)
			if d > 0 && rad == prev && ly&1 == 0 {
				bonus = 1
			}
			prev = rad
			t.megaLayer(w, x, ly, z, rad+bonus, ox, oz, false, c.leaf)
		}
		return
	}
	if h == 0 {
		return
	}
	for k := h - 1; k >= 0; k-- {
		t.megaLayer(w, x, y-k, z, k+c.baseRadius+c.coreWidth/2, ox, oz, c.simplify, c.leaf)
	}
}

// Decoration side masks (sub_D299270): west, east, north, south.
type sideMask [4]bool

// Mega tree constants (MegaTreeTrunk, sub_D298DB0).
const (
	// megaBranchOffset puts a branch at the middle of the 2x2 trunk and
	// rounds it to the nearest block.
	megaBranchOffset = 1.5
	// clusterGrid is the side of the grid the extra podzol clusters are
	// drawn on: only its outer ring is used. clusterCenter is the grid's
	// offset from the trunk.
	clusterGrid   = 8
	clusterCenter = 3
	// clusterColumnTop and clusterColumnDepth bound the podzol column
	// above and below y.
	clusterColumnTop   = 2
	clusterColumnDepth = 3
)

// megaTrunk is MegaTreeTrunk::place.
func (t *treeFeature) megaTrunk(w *featureWorld, x, y, z, h int32, r *rand.MT) (blockPos, bool) {
	m := &t.mega
	if y <= MinY || y+h >= MaxY {
		return blockPos{}, false
	}
	if !t.mayGrowOn.empty() {
		if b, ok := w.block(x, y-1, z); !ok || !t.mayGrowOn.match(t.o, b) {
			return blockPos{}, false
		}
	}
	for k := int32(0); k < h+2; k++ {
		rad := m.width
		if k == 0 {
			rad = max(mathx.Abs(m.width-1), 1)
		}
		ly := y + k
		if ly < MinY || ly >= MaxY {
			return blockPos{}, false
		}
		if t.below.empty() {
			continue
		}
		for dx := -rad; dx <= rad; dx++ {
			for dz := -rad; dz <= rad; dz++ {
				if b, ok := w.block(x+dx, ly, z+dz); !ok || !t.below.match(t.o, b) {
					return blockPos{}, false
				}
			}
		}
	}
	if t.megaCanopy != nil {
		t.megaCanopy.place(t, w, x, y+h, z, m.width, r)
	}
	if m.branchLen > 0 {
		fh := float32(h)
		by := int32(m.altMax*fh) + y - m.branchInterval.draw(r)
		for by > y+int32(m.altMin*fh) {
			f := r.Float()
			bx, bz := x+1, z+1
			if m.branchLen != 0 {
				base := float32(int32(float32(by-1) - float32(m.branchLen-1)*m.slope))
				a := (f*pi32 + f*pi32) * sinTableRadian
				cos, sin := sinTable[uint16(int32(a+sinTableQuarter))], sinTable[uint16(int32(a))]
				for i := int32(0); i < m.branchLen; i++ {
					fi := float32(i)
					bx = x + int32(cos*fi+megaBranchOffset)
					bz = z + int32(sin*fi+megaBranchOffset)
					w.set(bx, int32(fi*m.slope+base), bz, t.log)
				}
			}
			if m.branchCanopy != nil {
				m.branchCanopy.place(t, w, bx, by, bz, 1, r)
			}
			by -= m.branchInterval.draw(r)
		}
	}
	for dx := int32(0); dx < m.width; dx++ {
		for dz := int32(0); dz < m.width; dz++ {
			if b, ok := w.block(x+dx, y, z+dz); t.replaces(b, ok) {
				w.set(x+dx, y, z+dz, t.log)
			}
		}
	}
	for k := int32(1); k < h-1; k++ {
		for dx := int32(0); dx < m.width; dx++ {
			for dz := int32(0); dz < m.width; dz++ {
				px, py, pz := x+dx, y+k, z+dz
				if b, ok := w.block(px, py, pz); !t.replaces(b, ok) {
					continue
				}
				var mask sideMask
				mask[0] = dx == 0
				mask[1] = dx != 0 && dx == m.width-1
				mask[2] = dz == 0
				mask[3] = dz != 0 && dz == m.width-1
				w.set(px, py, pz, t.log)
				t.trunkDeco.applyMask(w, px, py, pz, r, mask)
			}
		}
	}
	if b, ok := w.block(x, y+h-1, z); t.replaces(b, ok) {
		w.set(x, y+h-1, z, t.log)
	}
	for dx := int32(0); dx < m.width; dx++ {
		for dz := int32(0); dz < m.width; dz++ {
			t.placeBase(w, x+dx, y-1, z+dz)
		}
	}
	if t.clusters > 0 {
		t.baseClusters(w, x, y-1, z, m.width, r)
	}
	return blockPos{x, y, z}, true
}

// baseClusters is sub_D298920: podzol (the first base block) rings around
// the four trunk corners, then num_clusters rings at random edge spots of
// an 8x8 grid around the trunk.
func (t *treeFeature) baseClusters(w *featureWorld, x, y, z, width int32, r *rand.MT) {
	if len(t.baseBlocks) == 0 {
		return
	}
	t.clusterRing(w, x-1, z-1, y)
	t.clusterRing(w, x+width, z-1, y)
	t.clusterRing(w, x-1, z+width, y)
	t.clusterRing(w, x+width, z+width, y)
	for range t.clusters {
		v := r.IntN(clusterGrid * clusterGrid)
		col := v % clusterGrid
		if v>>3 == clusterGrid-1 || v < clusterGrid || col == 0 || col == clusterGrid-1 {
			t.clusterRing(w, x+col-clusterCenter, z+v/clusterGrid-clusterCenter, y)
		}
	}
}

// clusterColumn is sub_D298DB0: from two above down to three below y, the
// first block that is podzol already ends the search; the first one the
// cluster may replace becomes podzol.
func (t *treeFeature) clusterColumn(w *featureWorld, x, y, z int32) {
	pod := t.baseBlocks[0]
	podName := t.o.blockName(pod)
	for dy := int32(clusterColumnTop); dy >= -clusterColumnDepth; dy-- {
		b, ok := w.block(x, y+dy, z)
		if !ok {
			return
		}
		if t.o.blockName(b) == podName {
			return
		}
		if t.clusterReplace.empty() || t.clusterReplace.match(t.o, b) {
			w.set(x, y+dy, z, pod)
			return
		}
	}
}

// clusterRing is one base cluster: a ring of clusterRadius around (cx, cz)
// with its corners cut, each spot a cluster column.
func (t *treeFeature) clusterRing(w *featureWorld, cx, cz, y int32) {
	rad := t.clusterRadius
	for dx := -rad; dx <= rad; dx++ {
		for dz := -rad; dz <= rad; dz++ {
			if mathx.Abs(dx) == rad && mathx.Abs(dz) == rad {
				continue
			}
			t.clusterColumn(w, cx+dx, y, cz+dz)
		}
	}
}
