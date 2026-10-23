package overworld

import (
	"cmp"

	"github.com/helix-bedrock/worldgen/rand"
	"github.com/helix-bedrock/worldgen/structure"
)

// Village placement, from BDS 1.26.52: the feature is built by sub_4FBB600
// (called from sub_523B5C0) and a chunk is checked by sub_4FBB850.
const (
	villageSalt = 10387312
	// o.place[skVillage].spacing and o.place[skVillage].separation are the values for worlds of base
	// game version 1.18.0 and later; older worlds use 27 and 10
	// (sub_523B5C0, version constant written by sub_6C01520).
	villageSpacing    = 34
	villageSeparation = 8
	// villageBiomeReach is the radius of the start biome test, in blocks.
	villageBiomeReach = 2
	// villageNoSurfaceY is the height the biome test uses when the corner
	// column has no preliminary surface.
	villageNoSurfaceY = 1
)

// structureSeed seeds r for the placement region (rx, rz) of a structure
// (StructureFeature::setRandomSeedFor).
func structureSeed(r *rand.MT, seed int64, rx, rz, salt int32) {
	r.Seed(structure.RegionSeed(seed, uint32(salt), rx, rz))
}

// IsVillageChunk reports whether a village starts in chunk (chunkX, chunkZ):
// the chunk is its region's spread pick (two draws averaged per axis, x
// first), and the biomes 2 blocks around the chunk centre, at the
// preliminary surface level of the chunk's corner column, all have a
// village type.
func (o *Overworld) IsVillageChunk(chunkX, chunkZ int32) bool {
	if !o.isVillageSpreadPick(chunkX, chunkZ) {
		return false
	}
	if o.isForced(skVillage, chunkX, chunkZ) {
		return true
	}
	if !o.structOK(skVillage, chunkX, chunkZ) {
		return false
	}
	y := prelimSurface(o.column(chunkX*quartsPerChunk, chunkZ*quartsPerChunk))
	if y == noSurface {
		y = villageNoSurfaceY
	}
	return o.biomesOnly(chunkX*16+8, y, chunkZ*16+8, villageBiomeReach, o.villageBiome)
}

// isVillageSpreadPick reports whether the chunk is its placement region's
// pick, before the biome check.
func (o *Overworld) isVillageSpreadPick(chunkX, chunkZ int32) bool {
	var r rand.MT
	rx, rz := structure.Region(chunkX, o.place[skVillage].spacing), structure.Region(chunkZ, o.place[skVillage].spacing)
	if x, z, ok := o.forcedPick(skVillage, rx, rz); ok {
		return x == chunkX && z == chunkZ
	}
	structureSeed(&r, o.seed, rx, rz, o.place[skVillage].salt)
	n := o.place[skVillage].spacing - o.place[skVillage].separation
	a := r.IntN(n)
	x := rx*o.place[skVillage].spacing + (a+r.IntN(n))/2
	b := r.IntN(n)
	z := rz*o.place[skVillage].spacing + (b+r.IntN(n))/2
	return x == chunkX && z == chunkZ
}

// biomesOnly reports whether every biome sampled every 4 blocks within r of
// (x, y, z), y clamped to the world, is in the set (sub_493B030).
func (o *Overworld) biomesOnly(x, y, z, r int32, set []bool) bool {
	y0, y1 := max(y-r, MinY), min(y+r, MaxY-1)
	for bx := x - r; bx <= x+r; bx += biomeCellBlocks {
		for bz := z - r; bz <= z+r; bz += biomeCellBlocks {
			for by := y0; by <= y1; by += biomeCellBlocks {
				if b := o.BiomeAt(bx, by, bz); int(b) >= len(set) || !set[b] {
					return false
				}
			}
		}
	}
	return true
}

// locateCand is a structure start candidate of a locate search: its chunk
// and its squared distance from the search point.
type locateCand struct {
	cx, cz int32
	d      int64
}

// compareLocateCand orders locate candidates nearest first.
func compareLocateCand(a, b locateCand) int { return cmp.Compare(a.d, b.d) }

// rawChunks generates and keeps the undecorated terrain of the chunks a
// locate search reads.
type rawChunks struct {
	o      *Overworld
	chunks map[[2]int32]*PackedChunk
}

// newRawChunks returns an empty cache over o.
func newRawChunks(o *Overworld) *rawChunks {
	return &rawChunks{o: o, chunks: map[[2]int32]*PackedChunk{}}
}

// get returns chunk (cx, cz), generating it the first time.
func (r *rawChunks) get(cx, cz int32) *PackedChunk {
	k := [2]int32{cx, cz}
	if pc, ok := r.chunks[k]; ok {
		return pc
	}
	var ch Chunk
	r.o.GenerateChunk(cx, cz, &ch)
	pc := Pack(&ch)
	r.chunks[k] = pc
	return pc
}

// vanillaVillageChunk is IsVillageChunk without the Config's village
// changes, for the first strongholds, which BDS places next to villages:
// changing villages must not move them.
func (o *Overworld) vanillaVillageChunk(chunkX, chunkZ int32) bool {
	var r rand.MT
	rx, rz := structure.Region(chunkX, villageSpacing), structure.Region(chunkZ, villageSpacing)
	structureSeed(&r, o.seed, rx, rz, villageSalt)
	n := int32(villageSpacing - villageSeparation)
	a := r.IntN(n)
	x := rx*villageSpacing + (a+r.IntN(n))/2
	b := r.IntN(n)
	z := rz*villageSpacing + (b+r.IntN(n))/2
	if x != chunkX || z != chunkZ {
		return false
	}
	y := prelimSurface(o.column(chunkX*quartsPerChunk, chunkZ*quartsPerChunk))
	if y == noSurface {
		y = villageNoSurfaceY
	}
	return o.biomesOnly(chunkX*16+8, y, chunkZ*16+8, villageBiomeReach, o.vanillaVillageBiome)
}
