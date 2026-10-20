package worldgen

import (
	"encoding/json"
	"strings"

	"github.com/helix-bedrock/worldgen/biome"
	"github.com/helix-bedrock/worldgen/rand"
)

// partiallyExposedBlobFeature replaces the blocks around a floor that are
// buried on every side but one (PartiallyExposedBlobFeature, sub_49BED10).
type partiallyExposedBlobFeature struct {
	block  uint32
	radius int32
	chance float32
	// exposed is the face left unchecked, in BDS's face order (down, up,
	// north, south, west, east).
	exposed int
}

func (o *Overworld) parsePartiallyExposedBlob(raw json.RawMessage) (placer, error) {
	var r struct {
		Places json.RawMessage `json:"places_block"`
		Radius int32           `json:"placement_radius_around_floor"`
		Chance float32         `json:"placement_probability_per_valid_position"`
		Face   string          `json:"exposed_face"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	var b biome.Block
	if err := json.Unmarshal(r.Places, &b); err != nil {
		return nil, nil //nolint:nilerr // skipped, not an error
	}
	id, ok := o.lookupBlock(b)
	if !ok {
		return nil, nil
	}
	face, ok := map[string]int{"down": faceDown, "up": faceUp, "north": faceNorth, "south": faceSouth,
		"west": faceWest, "east": faceEast}[r.Face]
	if !ok {
		face = faceUp
	}
	return &partiallyExposedBlobFeature{block: id, radius: r.Radius, chance: r.Chance, exposed: face}, nil
}

// buried is the blob's per-block test: neither air nor water, and no water
// in the block's second layer.
func (f *partiallyExposedBlobFeature) buried(w *featureWorld, x, y, z int32) bool {
	b, ok := w.block(x, y, z)
	if !ok || b == w.o.ids.air || w.o.waterMaterial(b) {
		return false
	}
	// VERIFY: the second layer is not stored while features run; the
	// blocks that keep water in it are taken to be holding water.
	name := w.o.blockName(b)
	return !(strings.HasSuffix(name, "_coral_fan") || strings.HasSuffix(name, "_coral_wall_fan") || name == "minecraft:sea_pickle")
}

func (f *partiallyExposedBlobFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	cy := y - 1
	placed := false
	// Every block within the radius on each axis, nearest first by
	// Manhattan distance (sub_D794130): one draw per block.
	rad := f.radius
	for depth := int32(0); depth <= 3*rad; depth++ {
		mx := min(depth, rad)
		for dx := -mx; dx <= mx; dx++ {
			my := min(depth-iabs(dx), rad)
			for dy := -my; dy <= my; dy++ {
				dz := depth - iabs(dx) - iabs(dy)
				if dz > rad {
					continue
				}
				if f.try(w, x+dx, cy+dy, z+dz, r) {
					placed = true
				}
				if dz != 0 && f.try(w, x+dx, cy+dy, z-dz, r) {
					placed = true
				}
			}
		}
	}
	return blockPos{x, cy, z}, placed
}

func (f *partiallyExposedBlobFeature) try(w *featureWorld, x, y, z int32, r *rand.MT) bool {
	if r.Float() > f.chance || !f.buried(w, x, y, z) {
		return false
	}
	for face, st := range faceStep {
		if face != f.exposed && !f.buried(w, x+st[0], y+st[1], z+st[2]) {
			return false
		}
	}
	w.set(x, y, z, f.block)
	return true
}

// bubbleColumns grows the bubble columns above magma. BDS makes them in the
// block ticks that run after generation (magma queues one when water is
// above it); its generated worlds hold a column through every water block
// straight above each magma block, up to the first block that is not water.
func (o *Overworld) bubbleColumns(c *Chunk) {
	if o.ids.magma == 0 || o.ids.bubbleColumn == 0 {
		return
	}
	for i := 0; i < len(c.Blocks)-256; i++ {
		if uint32(c.Blocks[i]) != o.ids.magma {
			continue
		}
		for j := i + 256; j < len(c.Blocks) && uint32(c.Blocks[j]) == o.ids.water; j += 256 {
			c.Blocks[j] = uint16(o.ids.bubbleColumn)
		}
	}
}

// seaAnemoneFeature puts a live coral or coral fan on the floor below a
// position (SeaAnemoneFeature, sub_49C1E50).
type seaAnemoneFeature struct {
	coral  *coralFeature
	plants [5]uint32
}

func (f *seaAnemoneFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	o := w.o
	limit := int32(SeaLevel - 15)
	solid := func(y int32) bool {
		b, _ := w.block(x, y, z)
		return o.solidMaterial(b)
	}
	for y > limit && !solid(y-1) {
		y--
	}
	// VERIFY: BDS also asks whether tube coral may go here (BlockSource
	// vfunc +88); taken to be yes in water.
	if b, _ := w.block(x, y, z); !o.waterMaterial(b) || !solid(y-1) || y <= limit {
		return blockPos{}, false
	}
	if r.IntN(4) != 0 {
		w.set(x, y, z, f.plants[r.IntN(5)])
	} else {
		fan := r.IntN(5)
		w.set(x, y, z, f.coral.fans[fan][r.IntN(2)])
	}
	return blockPos{x, y, z}, true
}
