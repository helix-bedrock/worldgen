package structure

import "github.com/helix-bedrock/worldgen/1.26.60/internal/mathx"

// The multipliers of a placement region's coordinates in its seed.
const (
	regionMulX = 1724254968
	regionMulZ = 245998635
)

// RegionSeed is the Mersenne Twister seed of placement region (rx, rz) of
// a structure with the given salt (StructureFeature::setRandomSeedFor).
// With seed and salt 0 it is the part of a chunk's seed that its
// coordinates give.
func RegionSeed(seed int64, salt uint32, rx, rz int32) uint32 {
	return uint32(seed) + salt - regionMulZ*uint32(rz) - regionMulX*uint32(rx)
}

// Region is the placement region of chunk coordinate c on a grid of the
// given spacing, in chunks.
func Region(c, spacing int32) int32 { return mathx.FloorDiv(c, spacing) }
