package overworld

// Shared sizes of the chunk layout used by the terrain and feature code.
const (
	// chunkEdge is the width of a chunk in blocks along x and z.
	chunkEdge = 16
	// chunkColumns is the number of block columns in a chunk.
	chunkColumns = chunkEdge * chunkEdge
	// subChunkBlocks is the number of blocks in a 16x16x16 sub-chunk, and
	// subChunkShift its log2: the sub-chunk of a block index is index >>
	// subChunkShift.
	subChunkBlocks = 4096
	subChunkShift  = 12
	// subChunkHeight is the height of a sub-chunk in blocks.
	subChunkHeight = 16
)

// Angle constants of BDS's float32 math and its 65536-entry sine table
// (feature.SinTable).
const (
	// pi32 and twoPi32 are pi and 2*pi rounded to float32, as BDS writes
	// them.
	pi32    = 3.1415927
	twoPi32 = 6.2831855
	// sinTableRadian is the number of sine table entries per radian
	// (65536 / 2pi).
	sinTableRadian = 10430.378
	// sinTableQuarter is a quarter turn in table entries: sin at a plus
	// sinTableQuarter is cos at a.
	sinTableQuarter = 16384
	// sinTableSize is the number of entries in the sine table.
	sinTableSize = 65536
)
