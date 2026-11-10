package overworld

import "github.com/helix-bedrock/worldgen/rand"

// Huge mushrooms (minecraft:huge_mushroom_feature, a built-in feature), written from BDS
// 1.26.52 HugeMushroomFeature (sub_D2E7E20, placer sub_D2E8090). They grow in mushroom
// fields and dark forests.
//
// The cap blocks carry huge_mushroom_bits: 1-9 are the cap's faces, laid out like a
// numpad (1 = -x -z corner, 5 = top, 9 = +x +z corner), 0 is the inside and 10 is the
// stem.

// huge_mushroom_bits values the placer uses.
const (
	capInside   = 0
	capCornerNW = 1
	capCornerNE = 3
	capTop      = 5
	capCornerSW = 7
	capCornerSE = 9
	capStem     = 10
	// capRowStep is the bits step from one z row of the numpad to the next.
	capRowStep = 3
	// mushroomBitsCount is the number of huge_mushroom_bits values.
	mushroomBitsCount = 11
)

// Huge mushroom shape constants (sub_D2E8090).
const (
	// mushroomMinHeight and mushroomHeightRange give the stem height,
	// doubled with chance 1 in mushroomTallChance.
	mushroomMinHeight   = 4
	mushroomHeightRange = 3
	mushroomTallChance  = 12
	// mushroomCapRadius is the brown cap's radius and the width of the
	// space checked from 4 blocks up.
	mushroomCapRadius = 3
	// mushroomDomeDrop is how far the red dome reaches below the top, and
	// mushroomDomeRadius its radius below the top layer.
	mushroomDomeDrop   = 3
	mushroomDomeRadius = 2
)

type hugeMushroomFeature struct {
	brown, red, stem [mushroomBitsCount]uint32 // by huge_mushroom_bits
	water            uint32
	plants           [2]uint32 // small brown and red mushrooms: a huge one may grow from them
	soil             []uint32
}

func (o *Overworld) newHugeMushroom() *hugeMushroomFeature {
	f := &hugeMushroomFeature{water: o.ids.water}
	for i := range mushroomBitsCount {
		f.brown[i], _ = o.reg.Lookup("minecraft:brown_mushroom_block", map[string]any{"huge_mushroom_bits": int32(i)})
		f.red[i], _ = o.reg.Lookup("minecraft:red_mushroom_block", map[string]any{"huge_mushroom_bits": int32(i)})
		f.stem[i], _ = o.reg.Lookup("minecraft:mushroom_stem", map[string]any{"huge_mushroom_bits": int32(i)})
	}
	f.plants[0], _ = o.reg.Default("minecraft:brown_mushroom")
	f.plants[1], _ = o.reg.Default("minecraft:red_mushroom")
	// VERIFY: sub_D2E89C0 tests a block tag list plus two blocks; taken as the
	// dirt-like blocks.
	for _, n := range []string{"dirt", "coarse_dirt", "grass_block", "podzol", "mycelium", "dirt_with_roots", "moss_block", "mud", "muddy_mangrove_roots"} {
		if b, ok := o.reg.Default("minecraft:" + n); ok {
			f.soil = append(f.soil, b)
		}
	}
	return f
}

func (f *hugeMushroomFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	typ := r.IntN(2) // 0 brown (flat cap), 1 red (dome)
	h := r.IntN(mushroomHeightRange) + mushroomMinHeight
	if r.IntN(mushroomTallChance) == 0 {
		h *= 2
	}
	if y <= MinY || y+h+1 >= MaxY {
		return blockPos{}, false
	}
	// The space above must be air or leaves, wider from 4 blocks up; the
	// origin may also hold a small mushroom.
	for cy := y; cy <= y+h+1; cy++ {
		rad := int32(0)
		if cy > y+mushroomCapRadius {
			rad = mushroomCapRadius
		}
		for cx := x - rad; cx <= x+rad; cx++ {
			for cz := z - rad; cz <= z+rad; cz++ {
				b, ok := w.block(cx, cy, cz)
				if !ok {
					return blockPos{}, false
				}
				if b == w.o.ids.air || w.o.hasBlockTag(b, "leaves") {
					continue
				}
				if cx == x && cy == y && cz == z && (b == f.plants[0] || b == f.plants[1]) {
					continue
				}
				return blockPos{}, false
			}
		}
	}
	base := y
	below, _ := w.block(x, y-1, z)
	if !inList(f.soil, below) {
		// One or two blocks of water over soil: the stem reaches down
		// through them. VERIFY: the block BDS tests here (qword_F613F00) is
		// taken as water, and the extra check sub_C8E3060 is left out.
		if below != f.water {
			return blockPos{}, false
		}
		b2, _ := w.block(x, y-2, z)
		b3, _ := w.block(x, y-3, z)
		switch {
		case inList(f.soil, b2):
			base, h = y-1, h+1
		case b2 == f.water && inList(f.soil, b3):
			base, h = y-2, h+2
		default:
			return blockPos{}, false
		}
	}
	top := base + h
	capB := &f.brown
	if typ == 1 {
		capB = &f.red
	}
	start := top
	if typ == 1 {
		start = top - mushroomDomeDrop
	}
	for cy := start; cy <= top; cy++ {
		rad := int32(mushroomCapRadius)
		if typ == 1 {
			rad = 1
			if cy < top {
				rad = mushroomDomeRadius
			}
		}
		x0, x1, z0, z1 := x-rad, x+rad, z-rad, z+rad
		for cx := x0; cx <= x1; cx++ {
			for cz := z0; cz <= z1; cz++ {
				bits := int32(capTop)
				if cx == x0 {
					bits--
				}
				if cx == x1 {
					bits++
				}
				if cz == z0 {
					bits -= capRowStep
				}
				if cz == z1 {
					bits += capRowStep
				}
				if typ == 0 || cy < top {
					if (cx == x0 || cx == x1) && (cz == z0 || cz == z1) {
						continue
					}
					switch {
					case cx == x-(rad-1) && cz == z0, cx == x0 && cz == z-(rad-1):
						bits = capCornerNW
					case cx == x+(rad-1) && cz == z0, cx == x1 && cz == z-(rad-1):
						bits = capCornerNE
					case cx == x-(rad-1) && cz == z1, cx == x0 && cz == z+(rad-1):
						bits = capCornerSW
					case cx == x+(rad-1) && cz == z1, cx == x1 && cz == z+(rad-1):
						bits = capCornerSE
					}
				}
				if bits == capTop && cy < top {
					bits = capInside
				}
				if bits == capInside && base < top-1 {
					continue
				}
				if b, ok := w.block(cx, cy, cz); ok && !w.o.solidMaterial(b) {
					w.set(cx, cy, cz, capB[bits])
				}
			}
		}
	}
	for i := range h {
		if b, ok := w.block(x, base+i, z); ok && !w.o.solidMaterial(b) {
			w.set(x, base+i, z, f.stem[capStem])
		}
	}
	return blockPos{x, y, z}, true
}
