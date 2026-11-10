package overworld

// Constants shared by the structure files.
const (
	// structRotations is how many quarter turns a structure piece can take.
	structRotations = 4
	// boxUnset is a coordinate far past any world position: an empty
	// bounding box starts at boxUnset and ends at -boxUnset so the first
	// piece sets both corners.
	boxUnset = 1 << 30
)

// Chunk.Water is a bitset of 64-bit words, one bit per block index.
const (
	// waterWordShift turns a block index into its word in Chunk.Water.
	waterWordShift = 6
	// waterBitMask picks a block index's bit within its word.
	waterBitMask = 63
)

// Biome grid sizes the structure biome tests use.
const (
	// quartsPerChunk is how many 4-block biome cells span a chunk.
	quartsPerChunk = 4
	// biomeCellBlocks is the side of a biome cell in blocks; biome tests
	// sample once per cell.
	biomeCellBlocks = 4
)

// noSurfaceY is the height a structure's biome test uses when the start
// column has no surface (the none argument of structureY).
const noSurfaceY = 1

// Structure piece rotations, in clockwise quarter turns.
const (
	rotNone = 0
	rot90   = 1
	rot180  = 2
	rot270  = 3
)

// chunkLastBlock is the offset of a chunk's last block from its first on
// an axis.
const chunkLastBlock = 15
