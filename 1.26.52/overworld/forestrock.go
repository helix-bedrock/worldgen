package overworld

import "github.com/helix-bedrock/worldgen/1.26.52/rand"

// Forest rocks of the giant taigas, written from BDS 1.26.52: legacy:forest_rock_feature
// (LegacyForestRockFeature, place sub_51D2470) drops zero to two rocks at random spots
// of the chunk on the heightmap; each is minecraft:forest_rock_feature, a BlockBlobFeature
// (place sub_4987380) of mossy cobblestone with start radius 0, registered in
// sub_4E2BB50.

// Forest rock constants (sub_51D2470, sub_4987380).
const (
	// forestRockMaxCount bounds the rocks per chunk (zero to two).
	forestRockMaxCount = 3
	// blobFloorGap keeps the blob above the bottom of the world.
	blobFloorGap = 3
	// blobBalls is the number of balls in one blob.
	blobBalls = 3
	// blobRadiusScale and blobRadiusBias turn the three radii into the
	// ball's radius: their mean plus a half.
	blobRadiusScale = 0.333
	blobRadiusBias  = 0.5
)

type legacyForestRockFeature struct {
	rock featureRef
}

func (f *legacyForestRockFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	for n := r.IntN(forestRockMaxCount); n > 0; n-- {
		dz := r.IntN(chunkEdge)
		dx := r.IntN(chunkEdge)
		px, pz := x+dx, z+dz
		f.rock.place(w, px, w.heightmap(px, pz), pz, r)
	}
	return blockPos{x, y, z}, true
}

// blockBlobFeature is three overlapping balls of one block, each a little off the last,
// on the first grass, dirt or stone below the origin.
// VERIFY: the ground list (xmmword_F60D690) is taken as grass, the dirt tag and stone,
// as in Java's old block blob.
type blockBlobFeature struct {
	block  uint32
	radius int32
}

func (f *blockBlobFeature) ground(w *featureWorld, b uint32) bool {
	return b == w.o.ids.grass || b == w.o.ids.stone || w.o.hasBlockTag(b, "dirt")
}

func (f *blockBlobFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	for y > MinY+blobFloorGap {
		b, ok := w.block(x, y-1, z)
		if ok && b != w.o.ids.air && f.ground(w, b) {
			break
		}
		y--
	}
	if y <= MinY+blobFloorGap || f.radius < 0 {
		return blockPos{}, y > MinY+blobFloorGap
	}
	cx, cy, cz := x, y, z
	for range blobBalls {
		rx := f.radius + r.IntN(2)
		ry := f.radius + r.IntN(2)
		rz := f.radius + r.IntN(2)
		d := float32(rx+ry+rz)*blobRadiusScale + blobRadiusBias
		dd := d * d
		for bx := cx - rx; bx <= cx+rx; bx++ {
			for by := cy - ry; by <= cy+ry; by++ {
				for bz := cz - rz; bz <= cz+rz; bz++ {
					if dd >= float32((bx-cx)*(bx-cx)+(by-cy)*(by-cy)+(bz-cz)*(bz-cz)) {
						w.set(bx, by, bz, f.block)
					}
				}
			}
		}
		ox := r.IntN(2*f.radius + 2)
		oy := r.IntN(2)
		oz := r.IntN(2*f.radius + 2)
		cx += ox - f.radius - 1
		cy -= oy
		cz += oz - f.radius - 1
	}
	return blockPos{x, y, z}, true
}
