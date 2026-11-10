package overworld

import (
	"fmt"
	"math"

	"github.com/helix-bedrock/worldgen/rand"
	"github.com/helix-bedrock/worldgen/structure"
)

// LiveWorld is a running world a feature is placed into outside of generation, as BDS places
// a tree when a sapling grows. Blocks are state indices in the generator's registry. Block's
// ok is false where the world has no loaded chunk; the feature treats it as BDS treats an
// unloaded block.
type LiveWorld interface {
	// Block returns the block at a position.
	Block(x, y, z int32) (state uint32, ok bool)
	// Biome returns the biome ID at a position.
	Biome(x, y, z int32) (id uint32, ok bool)
	// Height is the y above the column's highest fully opaque block.
	Height(x, z int32) int32
}

// PlacedBlock is one block a feature placed into a live world.
type PlacedBlock struct {
	X, Y, Z int32
	Block   uint32
}

// Placed is what a feature placed into a live world, in placement order: blocks (a position
// may appear more than once; the last write wins), block entities and pending block ticks.
type Placed struct {
	Blocks   []PlacedBlock
	Entities []structure.BlockEntity
	Ticks    []Tick
}

// PlaceFeature places the feature id ("minecraft:" may be left out) at (x, y, z) into a live
// world, drawing from r, and returns what it placed and whether the feature reports success.
// Nothing is written to w: the caller applies Placed. A failed feature may still have placed
// blocks, as BDS's do into the level; BDS keeps them. The feature sees w plus its own writes,
// and nothing of generation (no chunk overlays, no structure guards). An unknown feature
// places nothing. It may be called from many goroutines at once with different worlds.
func (o *Overworld) PlaceFeature(id string, x, y, z int32, r *rand.MT, w LiveWorld) (p Placed, ok bool) {
	if w == nil || r == nil || o.features == nil {
		return Placed{}, false
	}
	feat, found := o.features.get(fullID(id))
	if !found {
		return Placed{}, false
	}
	fw := &featureWorld{o: o, ox: x >> 4, oz: z >> 4, lastX: math.MaxInt32, live: w,
		overlay: map[[2]int32]*overlayChunk{}, tops: map[[2]int32]int32{}}
	defer fw.release()
	defer func() {
		if rec := recover(); rec != nil {
			ok = false
			if c := o.opts.cfg; c != nil && c.Logger != nil {
				c.Logger.Error("worldgen: live feature panicked", "feature", id, "panic", fmt.Sprint(rec), "x", x, "y", y, "z", z)
			}
		}
		p = fw.placed
	}()
	_, ok = feat.place(fw, x, y, z, r)
	return fw.placed, ok
}
