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

// blockID is the default state of block name, or 0 when the block list
// lacks it.
func (o *Overworld) blockID(name string) uint32 {
	b, _ := o.reg.Default(name)
	return b
}

// Ice spike and ice patch constants (sub_51CB360, sub_51CAD30).
const (
	// spikeFloorGap keeps the walk down above the bottom of the world.
	spikeFloorGap = 2
	// spikeLift is the random lift of the spike's base.
	spikeLift = 4
	// spikeMinHeight and spikeHeightRange give the spike height.
	spikeMinHeight   = 7
	spikeHeightRange = 4
	// spikeWidthDivisor turns the height into the base width.
	spikeWidthDivisor = 4
	// A spike at least spikeTallWidth wide becomes a tall one with chance
	// 1 in spikeTallChance, lifted spikeTallMinLift plus up to
	// spikeTallLiftRange.
	spikeTallWidth     = 2
	spikeTallChance    = 60
	spikeTallLiftRange = 30
	spikeTallMinLift   = 10
	// spikeMirrorWidth is the layer width from which a layer is mirrored
	// downwards.
	spikeMirrorWidth = 2
	// spikeRoundOffset rounds the disc of each layer.
	spikeRoundOffset = 0.25
	// spikeEdgeKeep is the chance above which an edge block is skipped.
	spikeEdgeKeep = 0.75
	// spikeSolidColumn is a run longer than any column: the middle column
	// is never broken.
	spikeSolidColumn = 50
	// spikeCornerRun bounds the runs and gaps of the corner columns.
	spikeCornerRun = 5
	// spikeColumnDepth is how far below sea level the column goes.
	spikeColumnDepth = 13
	// icePatchMinRadius is the smallest ice patch radius.
	icePatchMinRadius = 2
)

// onSnow walks down from y through air (not below MinY+2) and reports the y it stopped
// at and whether the block there is snow.
func onSnow(w *featureWorld, x, y, z int32, snow uint32) (int32, bool) {
	for {
		b, ok := w.block(x, y, z)
		if !ok {
			return y, false
		}
		if b != w.o.ids.air || y <= MinY+spikeFloorGap {
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
	y += r.IntN(spikeLift)
	h := r.IntN(spikeHeightRange) + spikeMinHeight
	j := h/spikeWidthDivisor + r.IntN(2)
	if j >= spikeTallWidth && r.IntN(spikeTallChance) == 0 {
		y += r.IntN(spikeTallLiftRange) + spikeTallMinLift
	}
	for k := range h {
		fr := (1 - float32(k)/float32(h)) * float32(j)
		l := int32(math.Ceil(float64(fr)))
		ff := fr * fr
		mirror := k != 0 && l >= spikeMirrorWidth
		for i := -l; i <= l; i++ {
			fi := float32(mathx.Abs(i)) - spikeRoundOffset
			for jz := -l; jz <= l; jz++ {
				fj := float32(mathx.Abs(jz)) - spikeRoundOffset
				if (i != 0 || jz != 0) && fi*fi+fj*fj > ff {
					continue
				}
				if (i == -l || i == l || jz == -l || jz == l) && r.Float() > spikeEdgeKeep {
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
			n := int32(spikeSolidColumn)
			if mathx.Abs(dx) == 1 && mathx.Abs(dz) == 1 {
				n = r.IntN(spikeCornerRun)
			}
			for cy > SeaLevel-spikeColumnDepth {
				b, ok := w.block(x+dx, cy, z+dz)
				if !ok || !f.spikeMay(w, b, true) {
					break
				}
				w.set(x+dx, cy, z+dz, f.packed)
				cy--
				if n <= 1 {
					cy -= r.IntN(spikeCornerRun) + 1
					n = r.IntN(spikeCornerRun)
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
	n := r.IntN(f.radius-icePatchMinRadius) + icePatchMinRadius
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
