package overworld

import "github.com/helix-bedrock/worldgen/rand"

// Water and lava springs: single liquid sources set into cave walls
// (SpringFeature sub_49C58B0, LegacySpringsFeature sub_51D30F0, written from
// BDS 1.26.52). BDS lets them flow in block ticks after generation; the
// compare world has none flowing, so only the source is placed.

// Spring placement constants (sub_49C58B0, sub_51D30F0).
const (
	// springRockSides is how many of the five checked sides must be rock:
	// the fifth is the one open side.
	springRockSides = 4
	// waterSpringTries and lavaSpringTries are the attempts per chunk.
	waterSpringTries = 25
	lavaSpringTries  = 20
	// waterSpringRange is the height range of water springs above MinY.
	waterSpringRange = 256
	// lavaSpringFloor and lavaSpringRange give the lava spring height:
	// floor plus a triple nested draw that biases it to the bottom.
	lavaSpringFloor = 8
	lavaSpringRange = 240
)

// springFeature places a liquid source in a wall: rock above and below, the
// spot itself air or rock, and exactly one open side among the four sides
// and the block below.
type springFeature struct {
	liquid uint32
	// rock is what a spring may sit in (sub_49C5D30: stone, deepslate,
	// tuff, calcite, snow, powder snow, packed ice, dirt and coarse dirt on
	// the current base game version).
	rock map[string]bool
}

func (f *springFeature) isRock(w *featureWorld, x, y, z int32) bool {
	b, _ := w.block(x, y, z)
	return f.rock[w.o.blockName(b)]
}

func (f *springFeature) isAir(w *featureWorld, x, y, z int32) bool {
	b, ok := w.block(x, y, z)
	return ok && b == w.o.ids.air
}

func (f *springFeature) place(w *featureWorld, x, y, z int32, _ *rand.MT) (blockPos, bool) {
	if !f.isRock(w, x, y+1, z) || !f.isRock(w, x, y-1, z) {
		return blockPos{}, false
	}
	if !f.isAir(w, x, y, z) && !f.isRock(w, x, y, z) {
		return blockPos{}, false
	}
	rock, air := 0, 0
	for _, d := range [5][3]int32{{-1, 0, 0}, {1, 0, 0}, {0, 0, -1}, {0, 0, 1}, {0, -1, 0}} {
		if f.isRock(w, x+d[0], y+d[1], z+d[2]) {
			rock++
		}
		if f.isAir(w, x+d[0], y+d[1], z+d[2]) {
			air++
		}
	}
	if rock == springRockSides && air == 1 {
		w.set(x, y, z, f.liquid)
	}
	return blockPos{x, y, z}, true
}

// legacySpringsFeature tries 25 water springs and 20 lava springs in the
// chunk. The heights are the current base game version's: water anywhere
// from the bottom up 256 blocks, lava strongly biased to the bottom. Lava
// skips frozen oceans, ice spikes and the deep dark.
type legacySpringsFeature struct {
	water, lava *springFeature
}

func (f *legacySpringsFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	for range waterSpringTries {
		dz := r.IntN(16)
		dy := MinY + r.IntN(waterSpringRange)
		dx := r.IntN(16)
		f.water.place(w, x+dx, y+dy, z+dz, r)
	}
	for range lavaSpringTries {
		dz := r.IntN(16)
		dy := MinY + lavaSpringFloor + r.IntN(r.IntN(r.IntN(lavaSpringRange)))
		dx := r.IntN(16)
		px, py, pz := x+dx, y+dy, z+dz
		b := w.biome(px, py, pz)
		if b != ^uint32(0) && b == w.o.deepDark || w.o.biomeHas(b, "frozen") && w.o.biomeHas(b, "ocean") ||
			w.o.biomeHas(b, "ice_plains") && w.o.biomeHas(b, "mutated") {
			continue
		}
		f.lava.place(w, px, py, pz, r)
	}
	return blockPos{x, y, z}, true
}
