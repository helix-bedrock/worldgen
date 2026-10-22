package overworld

import (
	"math"

	"github.com/helix-bedrock/worldgen/internal/mathx"

	"github.com/helix-bedrock/worldgen/rand"
)

// Ice spikes and ice patches of the ice spikes biome, written from BDS 1.26.52:
// IceSpikeFeature (sub_51CB360) and IcePatchFeature (sub_51CAD30), both built-in
// features (sub_4E2BB50). Both stand on snow blocks: the origin walks down through air
// and gives up unless it lands on snow.

type iceSpikeFeature struct {
	packed, snow, ice uint32
}

func (o *Overworld) newIceSpike() *iceSpikeFeature {
	return &iceSpikeFeature{packed: o.ids.packedIce, snow: o.blockID("minecraft:snow"), ice: o.ids.ice}
}

func (o *Overworld) blockID(name string) uint32 {
	b, _ := o.reg.Default(name)
	return b
}

// onSnow walks down from y through air (not below MinY+2) and reports the y it stopped
// at and whether the block there is snow.
func onSnow(w *featureWorld, x, y, z int32, snow uint32) (int32, bool) {
	for {
		b, ok := w.block(x, y, z)
		if !ok {
			return y, false
		}
		if b != w.o.ids.air || y <= MinY+2 {
			return y, b == snow
		}
		y--
	}
}

// spikeMay reports whether a spike may replace b: air, grass, dirt, snow and ice
// (and packed ice for the column below).
func (f *iceSpikeFeature) spikeMay(w *featureWorld, b uint32, packed bool) bool {
	switch b {
	case w.o.ids.air, w.o.ids.grass, f.snow, f.ice:
		return true
	case f.packed:
		return packed
	}
	return w.o.hasBlockTag(b, "dirt")
}

func (f *iceSpikeFeature) put(w *featureWorld, x, y, z int32, packed bool) {
	if b, ok := w.block(x, y, z); ok && f.spikeMay(w, b, packed) {
		w.set(x, y, z, f.packed)
	}
}

func (f *iceSpikeFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	y, ok := onSnow(w, x, y, z, f.snow)
	if !ok {
		return blockPos{}, false
	}
	y += r.IntN(4)
	h := r.IntN(4) + 7
	j := h/4 + r.IntN(2)
	if j >= 2 && r.IntN(60) == 0 {
		y += r.IntN(30) + 10
	}
	for k := range h {
		fr := (1 - float32(k)/float32(h)) * float32(j)
		l := int32(math.Ceil(float64(fr)))
		ff := fr * fr
		mirror := k != 0 && l >= 2
		for i := -l; i <= l; i++ {
			fi := float32(mathx.Abs(i)) - 0.25
			for jz := -l; jz <= l; jz++ {
				fj := float32(mathx.Abs(jz)) - 0.25
				if (i != 0 || jz != 0) && fi*fi+fj*fj > ff {
					continue
				}
				if (i == -l || i == l || jz == -l || jz == l) && r.Float() > 0.75 {
					continue
				}
				f.put(w, x+i, y+k, z+jz, false)
				if mirror {
					f.put(w, x+i, y-k, z+jz, false)
				}
			}
		}
	}
	// A column under the middle down to sea level - 13, broken into
	// stretches under the corners.
	k1 := min(max(j-1, 0), 1)
	for dx := -k1; dx <= k1; dx++ {
		for dz := -k1; dz <= k1; dz++ {
			cy := y - 1
			n := int32(50)
			if mathx.Abs(dx) == 1 && mathx.Abs(dz) == 1 {
				n = r.IntN(5)
			}
			for cy > SeaLevel-13 {
				b, ok := w.block(x+dx, cy, z+dz)
				if !ok || !f.spikeMay(w, b, true) {
					break
				}
				w.set(x+dx, cy, z+dz, f.packed)
				cy--
				if n <= 1 {
					cy -= r.IntN(5) + 1
					n = r.IntN(5)
				} else {
					n--
				}
			}
		}
	}
	return blockPos{x, y, z}, true
}

// icePatchFeature is a disc of packed ice through the dirt, snow and ice one block
// above to one below the snow it starts on.
type icePatchFeature struct {
	spike  *iceSpikeFeature
	radius int32
}

func (f *icePatchFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	s := f.spike
	y, ok := onSnow(w, x, y, z, s.snow)
	if !ok {
		return blockPos{}, false
	}
	n := r.IntN(f.radius-2) + 2
	for bx := x - n; bx <= x+n; bx++ {
		for bz := z - n; bz <= z+n; bz++ {
			if (bx-x)*(bx-x)+(bz-z)*(bz-z) > n*n {
				continue
			}
			for by := y - 1; by <= y+1; by++ {
				b, ok := w.block(bx, by, bz)
				if ok && (b == s.snow || b == s.ice || w.o.hasBlockTag(b, "dirt")) {
					w.set(bx, by, bz, s.packed)
				}
			}
		}
	}
	return blockPos{x, y, z}, true
}
