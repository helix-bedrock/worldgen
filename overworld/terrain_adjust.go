package overworld

import (
	"math"

	"github.com/helix-bedrock/worldgen/structure"
)

// Terrain adjustments around structure pieces, after BDS 1.26.52
// (FeatureTerrainAdjustments). Ancient city pieces register a "beard and
// shaver" (setBeardAndShaver, sub_D1EDCA0, with 1.0 and 5.0) in every
// chunk their box reaches within 12 blocks; the terrain fill (sub_5188B10)
// adds the adjustment (sub_D1EE730) to each block's density after the
// noodle caves and before the aquifer. Below a piece's ground level it
// fills, above it carves, which leaves the city standing in a cave.

// beardKernel is BDS's 24x24x24 beard kernel (sub_D1EC2F0), indexed
// (y+12) + 24(x+12) + 576(z+12), computed in float32 as BDS does, with the
// fast inverse square root.
func beardKernel() []float32 {
	k := make([]float32, 24*24*24)
	e := float32(2.7182817)
	for z := range int32(24) {
		for x := range int32(24) {
			r2 := (z-12)*(z-12) + (x-12)*(x-12)
			r16 := float32(r2) * 0.0625
			r1 := float32(r2) * 0.5
			for y := range int32(24) {
				fy := float32(float64(y-12) + 0.5)
				y2 := float32(fy * fy)
				ex := float32(math.Pow(float64(e), float64(-(float32(y2*0.0625) + r16))))
				h := float32(float32(y2*0.5) + r1)
				g := math.Float32frombits(0x5F3759DF - math.Float32bits(h)>>1)
				inv := float32(float32(1.5-float32(float32(h*0.5)*g)*g) * g)
				k[y+24*x+576*z] = float32(float32(-fy*inv)*0.5) * ex
			}
		}
	}
	return k
}

// kernelAt is the kernel at an offset, 0 outside [-12, 11] on any axis.
func (o *Overworld) kernelAt(dx, dy, dz int32) float32 {
	if dx < -12 || dx > 11 || dy < -12 || dy > 11 || dz < -12 || dz > 11 {
		return 0
	}
	return o.kernel[dy+24*dx+576*dz+7212]
}

// beardShaver is one registered beard and shaver: the piece box widened by
// 2, its ground level, and a sphere around its middle.
type beardShaver struct {
	x0, y0, z0, x1, y1, z1 int32
	ground, cx, cz         int32
	radius                 float32
}

const (
	// shaverBelow and shaverAbove widen the sphere below and above the
	// ground level (the two floats of setBeardAndShaver for cities).
	shaverBelow = 1.0
	shaverAbove = 5.0
)

func newBeardShaver(bb structure.Box, groundDelta int32) beardShaver {
	w, d := bb.X1-bb.X0+5, bb.Z1-bb.Z0+5
	return beardShaver{
		x0: bb.X0 - 2, y0: bb.Y0, z0: bb.Z0 - 2, x1: bb.X1 + 2, y1: bb.Y1, z1: bb.Z1 + 2,
		ground: bb.Y0 + groundDelta, cx: w/2 + bb.X0 - 2, cz: d/2 + bb.Z0 - 2,
		radius: max(float32(d)*0.5, float32(w)*0.5),
	}
}

// cityAdjustments lists the beard and shavers registered in chunk
// (chunkX, chunkZ), in the order BDS registers them: by city, then by piece
// in assembly order. It is nil when there are none.
func (o *Overworld) cityAdjustments(chunkX, chunkZ int32) []beardShaver {
	if o.city == nil {
		return nil
	}
	var out []beardShaver
	r := int32(cityReach + 1)
	r0x, r1x := structure.Region(chunkX-r, citySpacing), structure.Region(chunkX+r, citySpacing)
	r0z, r1z := structure.Region(chunkZ-r, citySpacing), structure.Region(chunkZ+r, citySpacing)
	for rx := r0x; rx <= r1x; rx++ {
		for rz := r0z; rz <= r1z; rz++ {
			c := o.cityAt(rx, rz)
			if c == nil {
				continue
			}
			for _, p := range c.pieces {
				// A piece is registered in the chunks its box reaches within
				// 12 blocks (sub_D1EC770).
				if (p.BB.X0-12)>>4 <= chunkX && chunkX <= (p.BB.X1+12)>>4 &&
					(p.BB.Z0-12)>>4 <= chunkZ && chunkZ <= (p.BB.Z1+12)>>4 {
					out = append(out, newBeardShaver(p.BB, p.Ground))
				}
			}
		}
	}
	return out
}

// beardBox is one registered beard (sub_D1ED140, kind 0 of a jigsaw
// piece: every rigid village piece): the piece box widened by 2 on x and z,
// and its ground level.
type beardBox struct {
	x0, z0, x1, z1, ground int32
}

func newBeardBox(bb structure.Box, groundDelta int32) beardBox {
	return beardBox{x0: bb.X0 - 2, z0: bb.Z0 - 2, x1: bb.X1 + 2, z1: bb.Z1 + 2, ground: bb.Y0 + groundDelta}
}

// villageBeards lists the beards registered in chunk (chunkX, chunkZ): the
// rigid pieces of the villages that reach it within 12 blocks, by village
// region, then in assembly order. It is nil when there are none.
func (o *Overworld) villageBeards(chunkX, chunkZ int32) []beardBox {
	if o.jigsaw == nil {
		return nil
	}
	var out []beardBox
	r0x, r1x := structure.Region(chunkX-7, villageSpacing), structure.Region(chunkX+7, villageSpacing)
	r0z, r1z := structure.Region(chunkZ-7, villageSpacing), structure.Region(chunkZ+7, villageSpacing)
	for rx := r0x; rx <= r1x; rx++ {
		for rz := r0z; rz <= r1z; rz++ {
			sx, sz, _ := o.villagePick(rx, rz)
			v := o.villageAt(sx, sz)
			if v == nil {
				continue
			}
			for _, p := range v.pieces {
				// Feature pieces get no beard (BDS registers only pieces
				// whose element places a template, element vfunc +40).
				if p.Element.TerrainMatching || p.Element.Feature != "" {
					continue
				}
				if (p.BB.X0-12)>>4 <= chunkX && chunkX <= (p.BB.X1+12)>>4 &&
					(p.BB.Z0-12)>>4 <= chunkZ && chunkZ <= (p.BB.Z1+12)>>4 {
					out = append(out, newBeardBox(p.BB, p.Ground))
				}
			}
		}
	}
	return out
}

// adjust is the density added at block (x, y, z) by the beard and shavers,
// then the beards (the first two loops of sub_D1EE730).
func (o *Overworld) adjust(bs []beardShaver, beards []beardBox, x, y, z int32) float32 {
	var sum float32
	for i := range bs {
		b := &bs[i]
		dx := max(x-b.x1, b.x0-x, 0)
		dz := max(z-b.z1, b.z0-z, 0)
		if dx > 11 || dz > 11 {
			// The kernel is 0 there; adding 0 leaves the sum as it is.
			continue
		}
		cy := b.ground
		if y > b.ground {
			cy = y
		}
		if y > b.y1 {
			cy = b.y1
		}
		dy := y - cy
		k := o.kernelAt(dx, dy, dz)
		if k == 0 {
			continue
		}
		fx, fy, fz := float32(x-b.cx), float32(dy), float32(z-b.cz)
		dist := float32(math.Sqrt(float64(float32(float32(fz*fz) + float32(float32(fx*fx)+float32(fy*fy))))))
		r := b.radius + shaverBelow
		if y > b.ground {
			r = b.radius + shaverAbove
		}
		var w float32
		if r > 0 && dist <= r {
			w = float32(1.5 - float32(dist/r))
		}
		sum = float32(sum + float32(k*w))
	}
	for i := range beards {
		b := &beards[i]
		dx := max(b.x0-x, x-b.x1, 0)
		dz := max(b.z0-z, z-b.z1, 0)
		sum = float32(sum + float32(o.kernelAt(dx, y-b.ground, dz)*0.8))
	}
	return sum
}
