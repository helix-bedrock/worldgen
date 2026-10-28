package overworld

import (
	"fmt"
	"log/slog"

	"github.com/helix-bedrock/worldgen/1.26.60/blocks"
	"github.com/helix-bedrock/worldgen/1.26.60/rand"
)

// Feature is a feature written in Go, registered with
// FeatureConfig.Register. Place puts it at a position and reports whether
// it placed anything. It is called from many goroutines at once, with a
// different World each time; it must draw only from r (seeded from the
// rule's ID and the chunk, as vanilla features are) so the world comes out
// the same on any number of cores. A panic is recovered and logged to
// Config.Logger, and counts as placing nothing.
type Feature interface {
	Place(w World, x, y, z int32, r *rand.MT) bool
}

// World is what a Feature sees: the chunk's undecorated terrain, with the
// structures, plus what this chunk's features have placed so far. Blocks
// are block state indices in the generator's registry; look names up once
// (Registry().Default("minecraft:stone")), not per block.
type World interface {
	// Block returns the block at a position; ok is false outside the
	// world's height or the chunks features may reach (up to 3 chunks).
	Block(x, y, z int32) (state uint32, ok bool)
	// SetBlock places a block. Writes outside the reach are dropped.
	SetBlock(x, y, z int32, state uint32)
	// Height is the y above the column's highest fully opaque block.
	Height(x, z int32) int32
	// Biome returns the biome ID at a position.
	Biome(x, y, z int32) uint32
	// Registry is the block list.
	Registry() *blocks.Registry
}

// Block implements World.
func (w *featureWorld) Block(x, y, z int32) (uint32, bool) { return w.block(x, y, z) }

// SetBlock implements World.
func (w *featureWorld) SetBlock(x, y, z int32, state uint32) {
	if int(state) >= w.o.reg.Len() {
		return
	}
	w.set(x, y, z, state)
}

// Height implements World.
func (w *featureWorld) Height(x, z int32) int32 { return w.heightmap(x, z) }

// Biome implements World.
func (w *featureWorld) Biome(x, y, z int32) uint32 { return w.biome(x, y, z) }

// Registry implements World.
func (w *featureWorld) Registry() *blocks.Registry { return w.o.reg }

// userFeature runs a registered Go feature as a placer.
type userFeature struct {
	id  string
	f   Feature
	log *slog.Logger
}

func (u userFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (pos blockPos, ok bool) {
	defer func() {
		if p := recover(); p != nil {
			ok = false
			if u.log != nil {
				u.log.Error("worldgen: feature panicked", "feature", u.id, "panic", fmt.Sprint(p), "x", x, "y", y, "z", z)
			}
		}
	}()
	return blockPos{x, y, z}, u.f.Place(w, x, y, z, r)
}

// noFeature is a disabled feature: it places nothing.
type noFeature struct{}

func (noFeature) place(*featureWorld, int32, int32, int32, *rand.MT) (blockPos, bool) {
	return blockPos{}, false
}
