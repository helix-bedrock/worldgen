package overworld

import (
	"math"

	"github.com/helix-bedrock/worldgen/internal/mathx"

	"github.com/helix-bedrock/worldgen/rand"
)

// Fancy (big oak) tree parts of minecraft:tree_feature, written from BDS
// 1.26.52: FancyTreeTrunk (place sub_D2D4490, height sub_D2D5750) and
// FancyTreeCanopy (sub_D2D40F0 with the disk helper sub_D2D4250). The
// trunk is the classic big tree: foliage clusters around the upper part,
// a straight trunk, and sloping branch logs from the trunk to each cluster.

type fancyTrunk struct {
	base, variance  int32
	width           int32
	scale           float32 // trunk height / available height
	slope           float32 // branch slope
	density         float32 // foliage cluster density
	minAltitude     float32 // branches start at least this fraction up
	foliageAltitude float32 // no foliage below this fraction
	widthScale      float32
}

// fancyNode is a foliage cluster and the height of the branch that feeds it.
type fancyNode struct {
	x, y, z, branchY int32
}

// fancyLine reports whether every block on the line from (x0,y0,z0) to
// (x1,y1,z1) is one the tree may grow through, stepping as BDS does: the
// longest axis in unit steps, the others rounded.
func (t *treeFeature) fancyLine(w *featureWorld, x0, y0, z0, x1, y1, z1 int32) bool {
	if t.below.empty() {
		return true
	}
	dx, dy, dz := float32(x1-x0), float32(y1-y0), float32(z1-z0)
	steps := max(max(mathx.AbsFloat(dx), mathx.AbsFloat(dy)), mathx.AbsFloat(dz))
	n := int32(steps)
	if n == 0 {
		// VERIFY: BDS divides by zero here and probes a far-off position;
		// taken to pass.
		return true
	}
	rx, ry, rz := dx/steps, dy/steps, dz/steps
	for i := int32(0); i <= n; i++ {
		f := float32(i)
		b, ok := w.block(x0+int32(f*rx+0.5), y0+int32(f*ry+0.5), z0+int32(f*rz+0.5))
		if !ok || !t.below.match(t.o, b) {
			return false
		}
	}
	return true
}

// fancyTrunk is FancyTreeTrunk::place for a tree of height h at (x, y, z).
func (t *treeFeature) fancyTrunk(w *featureWorld, x, y, z, h int32, r *rand.MT) (blockPos, bool) {
	f := &t.fancy
	if !t.mayGrowOn.empty() {
		if b, ok := w.block(x, y-1, z); !ok || !t.mayGrowOn.match(t.o, b) {
			return blockPos{}, false
		}
	}
	// The available height: up to the first block the trunk may not grow
	// through, if that is above the base height.
	height := h
	if !t.below.empty() {
		for i := int32(0); i <= max(h-1, 0); i++ {
			b, ok := w.block(x, y+i, z)
			if !ok || !t.below.match(t.o, b) {
				if i <= f.base {
					return blockPos{}, false
				}
				height = i
				break
			}
		}
	}
	fh := float32(height)
	trunk := height - 1
	if th := int32(f.scale * fh); height > th {
		trunk = th
	}
	clusters := int32(1)
	d := f.density * fh / float32(f.variance+1)
	if c := int32(d*d + 1.382); c >= 2 {
		clusters = c
	}
	top := y + trunk
	leafSpan := (f.variance + 1) / 3
	start := height - leafSpan
	nodes := []fancyNode{{x, y + start, z, top}}
	half := 0.5 * fh
	for layer := start; layer >= 0; layer-- {
		if f.foliageAltitude*fh > float32(layer) {
			continue
		}
		adj := half - float32(layer)
		var dist float32
		switch {
		case adj == 0:
			dist = half
		case half <= mathx.AbsFloat(adj):
			dist = 0
		default:
			dist = float32(math.Sqrt(float64(half*half - adj*adj)))
		}
		radius := dist * 0.5
		for range clusters {
			a := r.Float()
			b := r.Float()
			angle := (b + b) * 3.1415927 * 10430.378
			reach := (a + 0.328) * (f.widthScale * radius)
			fx := x + int32(sinTable[uint16(int32(angle))]*reach+0.5)
			fy := y + layer - 1
			fz := z + int32(reach*sinTable[uint16(int32(angle+16384))]+0.5)
			if !t.fancyLine(w, fx, fy, fz, fx, fy+leafSpan, fz) {
				continue
			}
			ddx, ddz := x-fx, z-fz
			dist := int32(math.Sqrt(float64(ddx*ddx + ddz*ddz)))
			by := float32(fy) - float32(dist)*f.slope
			branchY := int32(by)
			if by > float32(top) {
				branchY = top
			}
			if !t.fancyLine(w, x, branchY, z, fx, fy, fz) {
				continue
			}
			nodes = append(nodes, fancyNode{fx, fy, fz, branchY})
		}
	}
	if t.canopy == canopyFancy {
		for _, n := range nodes {
			t.fancyCanopy(w, n.x, n.y, n.z)
		}
	}
	for i := int32(0); i < f.width; i++ {
		for j := int32(0); j < f.width; j++ {
			for ly := y; ly <= top; ly++ {
				w.set(x+i, ly, z+j, t.log)
			}
		}
	}
	logX, logZ := t.o.withAxis(t.log, "x"), t.o.withAxis(t.log, "z")
	for _, n := range nodes {
		bx, bz := x, z
		if n.x > x {
			bx = x + f.width - 1
		}
		if n.z > z {
			bz = z + f.width - 1
		}
		if float32(n.branchY-y) < f.minAltitude*fh {
			continue
		}
		dx, dy, dz := float32(n.x-bx), float32(n.y-n.branchY), float32(n.z-bz)
		steps := max(max(mathx.AbsFloat(dx), mathx.AbsFloat(dy)), mathx.AbsFloat(dz))
		cnt := int32(steps)
		if cnt == 0 {
			// BDS divides by zero and places off in the void.
			continue
		}
		rx, ry, rz := dx/steps, dy/steps, dz/steps
		for i := int32(0); i <= cnt; i++ {
			fi := float32(i)
			ox, oz := int32(fi*rx+0.5), int32(fi*rz+0.5)
			log := t.log
			switch ax, az := mathx.Abs(ox), mathx.Abs(oz); {
			case ax == 0 && az == 0:
			case ax >= az:
				log = logX
			default:
				log = logZ
			}
			w.set(bx+ox, n.branchY+int32(fi*ry+0.5), bz+oz, log)
		}
	}
	return blockPos{x, y, z}, true
}

// fancyCanopy is FancyTreeCanopy::place at a foliage cluster: height
// layers of round disks, one smaller at the bottom and the top.
func (t *treeFeature) fancyCanopy(w *featureWorld, x, y, z int32) bool {
	h, rad := t.fancyHeight, t.fancyRadius
	if h == 0 {
		return false
	}
	placed := t.fancyDisk(w, x, y, z, rad-1)
	for i := int32(1); i < h; i++ {
		r := rad
		if i == h-1 {
			r--
		}
		if t.fancyDisk(w, x, y+i, z, r) {
			placed = true
		}
	}
	return placed
}

// fancyDisk is sub_D2D4250: leaves where (|dx|+0.5)^2 + (|dz|+0.5)^2 <= r^2.
func (t *treeFeature) fancyDisk(w *featureWorld, x, y, z, r int32) bool {
	placed := false
	r2 := float64(r * r)
	for dx := -r; dx <= r; dx++ {
		fx := float32(mathx.Abs(dx)) + 0.5
		ex := float64(fx * fx)
		for dz := -r; dz <= r; dz++ {
			fz := float32(mathx.Abs(dz)) + 0.5
			if r2 < float64(fz*fz)+ex {
				continue
			}
			if b, ok := w.block(x+dx, y, z+dz); t.replaces(b, ok) {
				w.set(x+dx, y, z+dz, t.leaf)
				placed = true
			}
		}
	}
	return placed
}
