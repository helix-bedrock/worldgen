package overworld

import "math"

// The world spawn search, as BDS's overworld picks a new world's spawn: it
// looks for a column whose climate is inland (continentalness from -0.1099)
// and away from the river band (weirdness outside -0.1599..0.1599), so a
// world starts on land, not in an ocean or a river, and not far from 0, 0.
//
// Each column is scored by its distance from 0, 0 squared plus its climate
// miss shifted up 22 bits, so any column that fits the climate beats any
// that does not. Rings around 0, 0 are walked from 512 to 2048 blocks out,
// 512 apart, with points 512 blocks apart along each ring; then rings around
// the best point from 32 to 512 blocks out, 32 apart. The lowest score
// wins. The spawn height is left to be found later.
//
// BDS also has a biome search that runs before this one, only with a
// feature toggle on; it is not ported.

// spawnTarget is a climate box the spawn aims for: [min, max] of
// temperature, humidity, continentalness, erosion, depth and weirdness, in
// the x10000 units of the biome search.
type spawnTarget [6][2]int64

// Climate bounds of the spawn targets.
const (
	climateFull       = 10000
	spawnInlandMin    = -1099
	spawnWeirdnessMin = 1599
)

// spawnTargets are BDS's two spawn climates: inland, with weirdness on
// either side of the river band. Depth aims at 0, the surface.
func spawnTargets() [2]spawnTarget {
	return [2]spawnTarget{
		{{-climateFull, climateFull}, {-climateFull, climateFull}, {spawnInlandMin, climateFull},
			{-climateFull, climateFull}, {0, 0}, {-climateFull, -spawnWeirdnessMin}},
		{{-climateFull, climateFull}, {-climateFull, climateFull}, {spawnInlandMin, climateFull},
			{-climateFull, climateFull}, {0, 0}, {spawnWeirdnessMin, climateFull}},
	}
}

// Spawn search rings: the coarse rings around 0, 0 and the fine ones around
// the best coarse point, each the first radius, the step between rings and
// points, and the last radius.
const (
	spawnCoarseStep = 512
	spawnCoarseMax  = 2048
	spawnFineStep   = 32
	spawnFineMax    = 512
	// spawnClimateShift puts the climate miss above any distance.
	spawnClimateShift = 22
	// fullTurn is BDS's 2*pi as a float32.
	fullTurn = float32(2 * math.Pi)
)

// SpawnPosition is the x and z of a new world's spawn, found as BDS's
// overworld finds it. It reads the climate only, no chunks.
// Runs: anywhere; it keeps no state.
func (o *Overworld) SpawnPosition() (x, z int32) {
	best := o.spawnScore(0, 0)
	x, z, best = o.spawnRings(0, 0, spawnCoarseStep, spawnCoarseMax, x, z, best)
	x, z, _ = o.spawnRings(x, z, spawnFineStep, spawnFineMax, x, z, best)
	return x, z
}

// spawnRings walks rings around (cx, cz) from step to last blocks out and
// returns the best point found, starting from (bx, bz) with score best.
func (o *Overworld) spawnRings(cx, cz, step, last, bx, bz int32, best int64) (int32, int32, int64) {
	angle := float32(0)
	for r := step; r <= last; {
		x := cx + int32(float32(math.Sin(float64(angle)))*float32(r))
		z := cz + int32(float32(math.Cos(float64(angle)))*float32(r))
		if s := o.spawnScore(x, z); s < best {
			bx, bz, best = x, z, s
		}
		next := float32(float64(step)/float64(float32(r))) + angle
		if next > fullTurn {
			angle, r = 0, r+step
		} else {
			angle = next
		}
	}
	return bx, bz, best
}

// spawnScore is a column's score: its distance from 0, 0 squared plus its
// climate miss, shifted above any distance.
func (o *Overworld) spawnScore(x, z int32) int64 {
	dist := uint32(x)*uint32(x) + uint32(z)*uint32(z) //nolint:gosec // BDS squares in 32 bits
	return int64(dist) + o.spawnClimateMiss(x, z)<<spawnClimateShift
}

// spawnClimateMiss is how far the climate at block (x, z) is from the
// nearest spawn target: the sum of squared distances outside each range.
func (o *Overworld) spawnClimateMiss(x, z int32) int64 {
	col := o.column(x>>2, z>>2)
	// BDS passes the climate through float32 on the way to the search.
	var p [6]int64
	for i, q := range [...]int64{col.climate[0], col.climate[1], col.climate[2], col.climate[3], 0, col.climate[4]} {
		p[i] = int64(float64(float32(float64(q)/climateFull)) * climateFull)
	}
	best := int64(math.MaxInt64)
	for _, t := range spawnTargets() {
		var miss int64
		for i, v := range p {
			d := rangeMiss(v, t[i][0], t[i][1])
			miss += d * d
		}
		best = min(best, miss)
	}
	return best
}

// rangeMiss is how far v lies outside [lo, hi], 0 inside.
func rangeMiss(v, lo, hi int64) int64 {
	switch {
	case v > hi:
		return v - hi
	case v < lo:
		return lo - v
	}
	return 0
}
