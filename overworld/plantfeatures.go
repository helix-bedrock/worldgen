package overworld

import (
	"encoding/json"
	"fmt"
	"math"

	"github.com/helix-bedrock/worldgen/biome"
	"github.com/helix-bedrock/worldgen/rand"
)

// Small plant features around trees, written from BDS 1.26.52:
// FlowerBedFeature (leaf_litter_feature, wildflowers_feature; place
// sub_51C0110), MushroomFeature (brown/red_mushroom_feature; sub_49B9020)
// and minecraft:horizontal_tree_decoration_feature (sub_51C9560, the shelf
// mushrooms on poplar and fallen logs).

// cardinalNames are the values of minecraft:cardinal_direction in BDS's
// enum order.
var cardinalNames = [4]string{"south", "west", "north", "east"}

// facingCardinal is the cardinal_direction of a horizontal facing.
func facingCardinal(f uint8) string {
	switch f {
	case 2:
		return "north"
	case 3:
		return "south"
	case 4:
		return "west"
	}
	return "east"
}

// flowerBedFeature places one flower bed block (leaf litter, wildflowers)
// with a random amount (growth 0-3) and direction, into air on a block that
// holds it.
type flowerBedFeature struct {
	states [4][4]uint32 // [growth][cardinal direction]
}

func (o *Overworld) newFlowerBed(name string) *flowerBedFeature {
	f := &flowerBedFeature{}
	def, _ := o.reg.Default(name)
	for g := range 4 {
		for d := range 4 {
			id, ok := o.reg.Lookup(name, map[string]any{"growth": int32(g), "minecraft:cardinal_direction": cardinalNames[d]})
			if !ok {
				id = def
			}
			f.states[g][d] = id
		}
	}
	return f
}

func (f *flowerBedFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	g := r.IntN(4)
	d := r.IntN(4)
	if b, ok := w.block(x, y, z); !ok || b != w.o.ids.air {
		return blockPos{}, false
	}
	if below, ok := w.block(x, y-1, z); !ok || !w.o.holdsPlant(below) {
		return blockPos{}, false
	}
	w.set(x, y, z, f.states[g][d])
	return blockPos{x, y, z}, true
}

// holdsPlant reports whether a block's top face holds a thin plant such as
// leaf litter or a mushroom (BlockLegacy::canProvideSupport upwards).
// VERIFY: BDS reads a support flag per block; taken to be solid blocks
// that fully block light, and leaves.
func (o *Overworld) holdsPlant(b uint32) bool {
	return o.solidMaterial(b) && o.reg.LightDampening(b) >= 15 || o.hasBlockTag(b, "leaves")
}

// mushroomFeature makes up to count tries around the origin; each puts the
// mushroom into air above a block that holds it, in the dark: on soil it
// always grows (VERIFY: the two soil lists were not read; taken to be
// mycelium and podzol), elsewhere the spot and twelve spots around it must
// be covered from the sky.
type mushroomFeature struct {
	block uint32
	count int
	soil  []uint32
}

func (f *mushroomFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	px, py, pz := x, y, z
	for range f.count {
		f.try(w, px, py, pz)
		dz := int32(r.Uint32() & 7)
		dz -= int32(r.Uint32() & 7)
		dy := int32(r.Uint32() & 3)
		dy -= int32(r.Uint32() & 3)
		dx := int32(r.Uint32() & 7)
		dx -= int32(r.Uint32() & 7)
		px, py, pz = x+dx, y+dy, z+dz
	}
	return blockPos{x, y, z}, true
}

// mushroomCover are the spots that must be covered, around the mushroom.
var mushroomCover = [13][2]int32{{0, 0}, {0, -1}, {1, 0}, {0, 1}, {-1, 0}, {1, -1}, {-1, -1}, {1, 1}, {-1, 1}, {0, -2}, {2, 0}, {0, 2}, {-2, 0}}

func (f *mushroomFeature) try(w *featureWorld, x, y, z int32) {
	if b, ok := w.block(x, y, z); !ok || b != w.o.ids.air {
		return
	}
	below, ok := w.block(x, y-1, z)
	if !ok || below == w.o.ids.air {
		return
	}
	if !inList(f.soil, below) {
		for _, c := range mushroomCover {
			if !covered(w, x+c[0], y, z+c[1]) {
				return
			}
		}
		if !w.o.holdsPlant(below) {
			return
		}
	}
	w.set(x, y, z, f.block)
}

// covered reports whether a spot is out of the sky (sub_49EC660): below the
// column's height map, or with a block that is not fully translucent
// within ten blocks above.
// VERIFY: BDS takes the height map from a chunk its region returns without
// a position; reading the spot's own column matches 12 of 14 mushrooms in
// the dappled forest test area, the decorated chunk's column fewer.
func covered(w *featureWorld, x, y, z int32) bool {
	if y < w.heightmap(x, z) {
		return true
	}
	for k := int32(1); k <= 10; k++ {
		if b, ok := w.block(x, y+k, z); ok && w.o.reg.Translucency(b) < 1 {
			return true
		}
	}
	return false
}

// horizontalDecoration is minecraft:horizontal_tree_decoration_feature: a
// block (a shelf mushroom) on a random side of a log, facing away from it.
type horizontalDecoration struct {
	name          string
	states        [4][2]uint32 // [facing-2][growth]
	barkSideOnly  bool
	allowAdjacent bool
}

func (o *Overworld) parseHorizontalDecoration(raw json.RawMessage) (placer, error) {
	var r struct {
		Block         biome.Block `json:"places_block"`
		BarkSideOnly  bool        `json:"bark_side_only"`
		AllowAdjacent bool        `json:"allow_adjacent"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	def, ok := o.lookupBlock(r.Block)
	if !ok {
		return nil, fmt.Errorf("decoration block %s missing", r.Block.Name)
	}
	s, _ := o.reg.State(def)
	_, hasDir := s.Props["minecraft:cardinal_direction"]
	_, hasGrowth := s.Props["growth"]
	if !hasDir || !hasGrowth {
		// BDS logs the missing state and places nothing.
		return &horizontalDecoration{}, nil
	}
	f := &horizontalDecoration{name: s.Name, barkSideOnly: r.BarkSideOnly, allowAdjacent: r.AllowAdjacent}
	for i := range 4 {
		for g := range 2 {
			id, ok := o.reg.Lookup(s.Name, map[string]any{"minecraft:cardinal_direction": facingCardinal(uint8(i + 2)), "growth": int32(g)})
			if !ok {
				id = def
			}
			f.states[i][g] = id
		}
	}
	return f, nil
}

func (f *horizontalDecoration) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	if f.name == "" {
		return blockPos{}, false
	}
	face := uint8(r.IntN(4) + 2)
	dx, dz := facingStep(face)
	tx, tz := x+dx, z+dz
	tb, ok := w.block(tx, y, tz)
	empty := ok && tb == w.o.ids.air
	alone := true
	if !f.allowAdjacent {
		for _, p := range [10][3]int32{
			{x, y, z - 1}, {x + 1, y, z}, {x, y, z + 1}, {x - 1, y, z},
			{tx, y + 1, tz}, {tx, y - 1, tz}, {tx, y, tz - 1}, {tx, y, tz + 1}, {tx - 1, y, tz}, {tx + 1, y, tz},
		} {
			if b, ok := w.block(p[0], p[1], p[2]); ok && w.o.blockName(b) == f.name {
				alone = false
				break
			}
		}
	}
	side := true
	if f.barkSideOnly {
		lb, _ := w.block(x, y, z)
		s, _ := w.o.reg.State(lb)
		axis, ok := s.Props["pillar_axis"].(string)
		if !ok {
			return blockPos{}, false
		}
		if (face == 4 || face == 5) && axis == "x" {
			return blockPos{}, false
		}
		side = !(axis == "z" && (face == 2 || face == 3))
	}
	if !side || !alone || !empty {
		return blockPos{}, false
	}
	g := r.IntN(2)
	w.set(tx, y, tz, f.states[face-2][g])
	return blockPos{tx, y, tz}, true
}

// Flowers and sweet berry bushes, written from BDS 1.26.52:
// LegacyFlowerFeature (minecraft:legacy:*_flower_feature; place
// sub_51D18F0, flower choice sub_D2D6C50), the meadow flower feature
// (sub_51D7E00) and CentralSpikedFeature (sweet_berry_bush_feature;
// sub_498F7D0). Each makes one try in new worlds (sub_4E2BB50 picks the
// count by base game version, as for mushrooms).

type flowerKind uint8

const (
	flowersFlowerForest flowerKind = iota
	flowersForest
	flowersOverworld
	flowersPlains
	flowersSwamp
)

// legacyFlowerFeature picks one flower for the call, then makes count tries
// around the origin, which must be air.
type legacyFlowerFeature struct {
	kind  flowerKind
	count int
	// forest is the flower forest list in BDS's flower type order; the
	// other kinds use the named fields.
	forest                                       [11]uint32
	poppy, dandelion, lily, orchid, bluet, daisy uint32
	cornflower                                   uint32
	tulips                                       [4]uint32
}

func (o *Overworld) newLegacyFlower(kind flowerKind, id func(string) uint32) *legacyFlowerFeature {
	f := &legacyFlowerFeature{kind: kind, count: 1,
		poppy: id("minecraft:poppy"), dandelion: id("minecraft:dandelion"), lily: id("minecraft:lily_of_the_valley"),
		orchid: id("minecraft:blue_orchid"), bluet: id("minecraft:azure_bluet"), daisy: id("minecraft:oxeye_daisy"),
		cornflower: id("minecraft:cornflower")}
	for i, t := range []string{"red", "orange", "white", "pink"} {
		f.tulips[i] = id("minecraft:" + t + "_tulip")
	}
	f.forest = [11]uint32{f.poppy, f.orchid, id("minecraft:allium"), f.bluet, f.tulips[0], f.tulips[1], f.tulips[2],
		f.tulips[3], f.daisy, f.cornflower, f.lily}
	return f
}

// flowerNoise is the one-octave simplex noise (seed 2345) the flower
// features build for themselves; it is the same noise as the frozen-ocean
// temperature's second noise.
func (o *Overworld) flowerNoise(x, z float32) float32 {
	n := o.surfNoise.frozenB
	return n.Sum2(x, z) * n.Scale
}

// noiseIndex maps a noise value in about [-1, 1] to an index below n.
func noiseIndex(v float32, max float32, n int) int {
	t := (v + 1) * 0.5
	if t > max {
		t = max
	}
	return int(float32(math.Max(float64(t), 0)) * float32(n))
}

func (f *legacyFlowerFeature) pick(w *featureWorld, x, z int32, r *rand.MT) uint32 {
	switch f.kind {
	case flowersFlowerForest:
		b := f.forest[noiseIndex(w.o.flowerNoise(float32(x)/48, float32(z)/48), math.Float32frombits(0x3F7FF972), len(f.forest))]
		if b == f.orchid {
			return f.poppy
		}
		return b
	case flowersForest:
		if nextBool(r) {
			return f.lily
		}
		return f.dandelion
	case flowersPlains:
		if w.o.flowerNoise(float32(x)/200, float32(z)/200) < -0.8 {
			return f.tulips[r.IntN(4)]
		}
		if r.IntN(4) == 0 {
			return f.dandelion
		}
		switch r.IntN(4) {
		case 0:
			return f.poppy
		case 1:
			return f.bluet
		case 2:
			return f.daisy
		}
		return f.cornflower
	case flowersSwamp:
		return f.orchid
	}
	if r.IntN(3) == 0 {
		return f.dandelion
	}
	return f.poppy
}

func (f *legacyFlowerFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	if b, ok := w.block(x, y, z); !ok || b != w.o.ids.air {
		return blockPos{}, false
	}
	flower := f.pick(w, x, z, r)
	px, py, pz := x, y, z
	for range f.count {
		if b, ok := w.block(px, py, pz); ok && b == w.o.ids.air && w.o.onSoil(w, px, py, pz) {
			w.set(px, py, pz, flower)
		}
		dz := int32(r.Uint32() & 7)
		dz -= int32(r.Uint32() & 7)
		dy := int32(r.Uint32() & 3)
		dy -= int32(r.Uint32() & 3)
		dx := int32(r.Uint32() & 7)
		dx -= int32(r.Uint32() & 7)
		px, py, pz = x+dx, y+dy, z+dz
	}
	return blockPos{x, y, z}, true
}

// onSoil reports whether a plant that grows on dirt may stand here (the
// flowers' canSurvive): a dirt-tagged block below.
func (o *Overworld) onSoil(w *featureWorld, x, y, z int32) bool {
	below, ok := w.block(x, y-1, z)
	return ok && o.hasBlockTag(below, "dirt")
}

// meadowFlowerFeature places one plant chosen by noise alone: how many
// candidates, which ones, and which candidate (sub_51D7E00). It always
// reports failure, as BDS does.
type meadowFlowerFeature struct {
	blocks [8]uint32
}

func (f *meadowFlowerFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	const scale = 0.0005
	fx, fz := float32(x), float32(z)
	v := (w.o.flowerNoise(fx*scale, fz*scale) + 1) * 0.5
	n := 1
	switch {
	case v > 1:
		n = 4
	case v >= 0:
		n = int(v*3 + 1)
	}
	var picks [4]uint32
	for i := range n {
		t := w.o.flowerNoise(float32(x+234349*int32(i))*scale, fz*scale)
		picks[i] = f.blocks[noiseIndex(t, math.Float32frombits(0x3F7FFFFE), 8)]
	}
	k := 0
	if n != 1 {
		k = noiseIndex(w.o.flowerNoise(fx*0.1, fz*0.1), math.Float32frombits(0x3F7FFFFE), n)
	}
	if w.o.onSoil(w, x, y, z) {
		if b, ok := w.block(x, y, z); ok && b == w.o.ids.air {
			w.set(x, y, z, picks[k])
		}
	}
	return blockPos{}, false
}

// sweetBerryFeature is CentralSpikedFeature with sweet berry bushes: count
// tries, each into air on grass, at a random growth.
type sweetBerryFeature struct {
	growth [8]uint32
	grass  uint32
	count  int
}

func (o *Overworld) newSweetBerry() *sweetBerryFeature {
	f := &sweetBerryFeature{count: 1}
	f.grass, _ = o.reg.Default("minecraft:grass_block")
	def, _ := o.reg.Default("minecraft:sweet_berry_bush")
	for g := range 8 {
		id, ok := o.reg.Lookup("minecraft:sweet_berry_bush", map[string]any{"growth": int32(g)})
		if !ok {
			id = def
		}
		f.growth[g] = id
	}
	return f
}

func (f *sweetBerryFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	px, py, pz := x, y, z
	for range f.count {
		if b, ok := w.block(px, py, pz); ok && b == w.o.ids.air {
			if below, ok := w.block(px, py-1, pz); ok && below == f.grass {
				w.set(px, py, pz, f.growth[r.IntN(8)])
			}
		}
		dx := int32(r.Uint32() & 7)
		dx -= int32(r.Uint32() & 7)
		dz := int32(r.Uint32() & 7)
		dz -= int32(r.Uint32() & 7)
		dy := int32(r.Uint32() & 3)
		dy -= int32(r.Uint32() & 3)
		px, py, pz = x+dx, y+dy, z+dz
	}
	return blockPos{x, y, z}, true
}
