package overworld

import (
	"encoding/json"
	"strings"

	"github.com/helix-bedrock/worldgen/biome"
	"github.com/helix-bedrock/worldgen/rand"
)

// The cave feature types: snap_to_surface, vegetation_patch and
// growing_plant from features/*.json, and the code-registered moss
// vegetation, dripleaf and single-face vine features lush caves use.
// Written from BDS 1.26.52.

// Block faces as BDS numbers them.
const (
	faceDown = iota
	faceUp
	faceNorth
	faceSouth
	faceWest
	faceEast
)

// faceDY is the y step of the vertical faces.
func faceDY() [2]int32 {
	return [2]int32{-1, 1}
}

// parseIntRange reads an intRange, zero when absent or malformed.
func parseIntRange(raw json.RawMessage) intRange {
	var r intRange
	if len(raw) > 0 && !jsonOK(raw, &r) {
		return intRange{}
	}
	return r
}

// jsonOK reports whether raw decodes into v. Callers that skip a malformed
// entry use it so the decode error is plainly dropped, not returned.
func jsonOK(raw json.RawMessage, v any) bool {
	return json.Unmarshal(raw, v) == nil
}

// sturdyFace reports whether a block can hold something on a face
// (BlockType::canProvideSupport with the center rule, vfunc +144).
// VERIFY: BDS asks each block type; the shape table was not read, so full
// cubes are told apart from thin and partial blocks by name here.
func (o *Overworld) sturdyFace(b uint32, face int) bool {
	_ = face
	if b == o.ids.air || !o.solidMaterial(b) || o.isWater(b) {
		return false
	}
	name := strings.TrimPrefix(o.blockName(b), "minecraft:")
	if isThinBlock(name) {
		return false
	}
	for _, s := range thinSuffixes() {
		if strings.HasSuffix(name, s) {
			return false
		}
	}
	return true
}

func thinSuffixes() [27]string {
	return [27]string{"_slab", "_stairs", "_fence", "_fence_gate", "_wall", "_pane", "_door", "_trapdoor",
		"_carpet", "_button", "_pressure_plate", "_sign", "_hanging_sign", "_bud", "_candle", "candle", "_rail", "rail",
		"_torch", "_lantern", "lantern", "_coral", "_coral_fan", "_wall_fan", "_head", "_skull", "_bars"}
}

// isThinBlock reports whether a block, named without its namespace, is
// thin or partial and so gives no sturdy face.
func isThinBlock(name string) bool {
	switch name {
	case "glow_lichen",
		"sculk_vein",
		"vine",
		"pointed_dripstone",
		"cave_vines",
		"cave_vines_body_with_berries",
		"cave_vines_head_with_berries",
		"spore_blossom",
		"azalea",
		"flowering_azalea",
		"big_dripleaf",
		"small_dripleaf_block",
		"hanging_roots",
		"moss_carpet",
		"pale_moss_carpet",
		"pale_hanging_moss",
		"amethyst_cluster",
		"chain",
		"iron_chain",
		"web",
		"ladder",
		"lever",
		"snow_layer",
		"bed",
		"cake",
		"flower_pot",
		"end_rod",
		"lightning_rod",
		"scaffolding",
		"bamboo",
		"cactus",
		"chest",
		"trapped_chest",
		"ender_chest",
		"enchanting_table",
		"daylight_detector",
		"daylight_detector_inverted",
		"sculk_sensor",
		"calibrated_sculk_sensor",
		"sculk_shrieker",
		"farmland",
		"grass_path",
		"dirt_path",
		"mud",
		"soul_sand",
		"honey_block":
		return true
	}
	return false
}

// withProp returns b with one property changed, or b when it has no such
// property or value.
func (o *Overworld) withProp(b uint32, prop string, v any) uint32 {
	s, ok := o.reg.State(b)
	if !ok {
		return b
	}
	if _, ok := s.Props[prop]; !ok {
		return b
	}
	props := make(map[string]any, len(s.Props))
	for k, p := range s.Props {
		switch t := p.(type) {
		case uint8:
			props[k] = float64(t)
		case int32:
			props[k] = float64(t)
		default:
			props[k] = p
		}
	}
	props[prop] = v
	if id, ok := o.reg.Lookup(s.Name, props); ok {
		return id
	}
	return b
}

// snapToSurfaceFeature moves its position up or down a column to a floor or
// ceiling and places a feature there (SnapToSurfaceFeature, sub_D313660
// with the column scan sub_D313B20 / sub_D2D3770).
type snapToSurfaceFeature struct {
	feat       featureRef
	rng        int32
	ceiling    bool
	horizontal bool
	// allowAir, allowNonAir and allowWater are allow_air_placement (default
	// true), allow_non_air_placement and allow_underwater_placement
	// (default false), from the constructor sub_4F66A10.
	allowAir, allowNonAir, allowWater bool
	surfaces                          blockMatcher
}

// rawSnapToSurface is the JSON of a minecraft:snap_to_surface_feature.
type rawSnapToSurface struct {
	Feature  string            `json:"feature_to_snap"`
	Range    int32             `json:"vertical_search_range"`
	Surface  string            `json:"surface"`
	Air      *bool             `json:"allow_air_placement"`
	NonAir   bool              `json:"allow_non_air_placement"`
	Water    bool              `json:"allow_underwater_placement"`
	Surfaces []json.RawMessage `json:"allowed_surface_blocks"`
}

func (o *Overworld) parseSnapToSurface(raw json.RawMessage, set *featureSet) (placer, error) {
	var r rawSnapToSurface
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	f := &snapToSurfaceFeature{feat: featureRef{r.Feature, set}, rng: r.Range, allowAir: r.Air == nil || *r.Air,
		allowNonAir: r.NonAir, allowWater: r.Water, surfaces: parseMatcher(r.Surfaces)}
	switch r.Surface {
	case "ceiling":
		f.ceiling = true
	case "floor", "":
	case "random_horizontal":
		f.horizontal = true
	default:
		// random draws a vertical direction the runtime does not model
		// yet; no vanilla feature uses it.
		return nil, nil
	}
	return f, nil
}

// passable is the scan's air/water test: air when air placement is allowed,
// water-material blocks when underwater placement is.
func (f *snapToSurfaceFeature) passable(o *Overworld, b uint32) bool {
	return f.allowAir && b == o.ids.air || f.allowWater && o.waterMaterial(b)
}

func (f *snapToSurfaceFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	o := w.o
	b, ok := w.block(x, y, z)
	if !ok {
		return blockPos{}, false
	}
	var p blockPos
	if f.horizontal {
		// The four horizontal faces in a shuffled order, tried until one
		// finds a surface: north, east, south, west, the first two
		// swapped on an even draw, then index 2 and 3 swapped with a drawn
		// earlier index unless the draw lands on themselves.
		dirs := [4]int{faceNorth, faceEast, faceSouth, faceWest}
		if r.Uint32()&1 == 0 {
			dirs[0], dirs[1] = dirs[1], dirs[0]
		}
		if i := r.Uint32() % 3; i != 2 {
			dirs[2], dirs[i] = dirs[i], dirs[2]
		}
		if i := r.Uint32() & 3; i != 3 {
			dirs[3], dirs[i] = dirs[i], dirs[3]
		}
		for _, d := range dirs {
			if p, ok = f.scan(w, x, y, z, d); ok {
				break
			}
		}
	} else {
		// From air the scan heads toward the surface; from inside the
		// ground it heads away from it, to the open block next to it.
		face := faceDown
		if f.ceiling != (b != o.ids.air && !o.waterMaterial(b)) {
			face = faceUp
		}
		p, ok = f.scan(w, x, y, z, face)
	}
	if !ok {
		return blockPos{}, false
	}
	return f.feat.place(w, p[0], p[1], p[2], r)
}

// faceStep is the block step toward each face.
func faceStep() [6][3]int32 {
	return [6][3]int32{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}
}

// scan walks from (x, y, z) toward face (sub_D313B20 with the column scan
// sub_D2D3770) and returns the open block next to the surface it finds.
func (f *snapToSurfaceFeature) scan(w *featureWorld, x, y, z int32, face int) (blockPos, bool) {
	o := w.o
	b, ok := w.block(x, y, z)
	if !ok {
		return blockPos{}, false
	}
	solid := b != o.ids.air && !o.waterMaterial(b)
	st := faceStep()[face]
	// The scan only runs on through blocks of the start's biome (the
	// wrapper lambda sub_D2DE640 compares the block target's biome, slot
	// 25).
	s := surfaceScan{f: f, w: w, solid: solid, startBiome: w.biome(x, y, z), face: face}
	if !s.cont(blockPos{x, y, z}, b) {
		return blockPos{}, false
	}
	p := blockPos{x + st[0], y + st[1], z + st[2]}
	var i int32
	for i < f.rng-1 {
		b, ok := w.block(p[0], p[1], p[2])
		if !ok || !s.cont(p, b) {
			break
		}
		p = blockPos{p[0] + st[0], p[1] + st[1], p[2] + st[2]}
		i++
	}
	if b, ok := w.block(p[0], p[1], p[2]); !ok || !s.surface(b) {
		return blockPos{}, false
	}
	if solid {
		i++
	}
	return blockPos{x + st[0]*i, y + st[1]*i, z + st[2]*i}, true
}

// surfaceScan is one snap-to-surface scan: whether it started in a solid
// block, the biome it started in and the face it walks toward.
type surfaceScan struct {
	f          *snapToSurfaceFeature
	w          *featureWorld
	solid      bool
	startBiome uint32
	face       int
}

// cont reports whether the scan goes on through block b at p.
func (s *surfaceScan) cont(p blockPos, b uint32) bool {
	o := s.w.o
	if s.w.biome(p[0], p[1], p[2]) != s.startBiome {
		return false
	}
	if s.solid {
		return s.f.allowNonAir && b != o.ids.air && !o.waterMaterial(b)
	}
	return s.f.passable(o, b)
}

// surface reports whether block b ends the scan as its surface.
func (s *surfaceScan) surface(b uint32) bool {
	o := s.w.o
	if !s.f.surfaces.empty() {
		return s.f.surfaces.match(o, b)
	}
	if s.solid {
		return s.f.passable(o, b)
	}
	return o.sturdyFace(b, s.face^1)
}

// vegetationPatchFeature lays a patch of ground blocks on a floor or
// ceiling and grows vegetation on it (VegetationPatchFeature, sub_D31FD70).
type vegetationPatchFeature struct {
	replace    blockMatcher
	ground     uint32
	vegetation featureRef
	ceiling    bool
	depth      intRange
	deepChance float32
	vertical   int32
	vegChance  float32
	radius     intRange
	edgeChance float32
	waterlog   bool
}

// rawVegetationPatch is the JSON of a minecraft:vegetation_patch_feature.
type rawVegetationPatch struct {
	Replace    []json.RawMessage `json:"replaceable_blocks"`
	Ground     json.RawMessage   `json:"ground_block"`
	Vegetation string            `json:"vegetation_feature"`
	Surface    string            `json:"surface"`
	Depth      json.RawMessage   `json:"depth"`
	Deep       float32           `json:"extra_deep_block_chance"`
	Vertical   int32             `json:"vertical_range"`
	VegChance  float32           `json:"vegetation_chance"`
	Radius     json.RawMessage   `json:"horizontal_radius"`
	Edge       float32           `json:"extra_edge_column_chance"`
	Waterlog   bool              `json:"waterlogged"`
}

func (o *Overworld) parseVegetationPatch(raw json.RawMessage, set *featureSet) (placer, error) {
	var r rawVegetationPatch
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	var gb biome.Block
	if err := json.Unmarshal(r.Ground, &gb); err != nil {
		return nil, err
	}
	ground, ok := o.lookupBlock(gb)
	if !ok {
		return nil, nil
	}
	return &vegetationPatchFeature{replace: parseMatcher(r.Replace), ground: ground, vegetation: featureRef{r.Vegetation, set},
		ceiling: r.Surface == "ceiling", depth: parseIntRange(r.Depth), deepChance: r.Deep, vertical: r.Vertical,
		vegChance: r.VegChance, radius: parseIntRange(r.Radius), edgeChance: r.Edge, waterlog: r.Waterlog}, nil
}

func (f *vegetationPatchFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	o := w.o
	rx := f.radius.draw(r)
	rz := f.radius.draw(r)
	// dir points into the surface: up for a ceiling, down for a floor.
	dir := faceDown
	if f.ceiling {
		dir = faceUp
	}
	dy := faceDY()[dir]
	var placed []blockPos
	if rx >= -1 && rz >= -1 {
		for i := -rx - 1; i <= rx+1; i++ {
			xEdge := i == -rx-1 || i == rx+1
			for k := -rz - 1; k <= rz+1; k++ {
				zEdge := k == -rz-1 || k == rz+1
				if xEdge && zEdge {
					continue
				}
				if (xEdge || zEdge) && (f.edgeChance == 0 || r.Float() > f.edgeChance) {
					continue
				}
				px, py, pz := x+i, y, z+k
				for n := int32(0); n < f.vertical; n++ {
					if b, _ := w.block(px, py, pz); b != o.ids.air {
						break
					}
					py += dy
				}
				for n := int32(0); n < f.vertical; n++ {
					if b, _ := w.block(px, py, pz); b == o.ids.air {
						break
					}
					py -= dy
				}
				gy := py + dy
				if b, _ := w.block(px, py, pz); b != o.ids.air {
					continue
				}
				if gb, _ := w.block(px, gy, pz); !o.sturdyFace(gb, dir^1) {
					continue
				}
				deep := int32(0)
				if f.deepChance > 0 && f.deepChance > r.Float() {
					deep = 1
				}
				depth := deep + f.depth.draw(r)
				if f.placeGround(w, px, gy, pz, dy, depth) {
					placed = append(placed, blockPos{px, gy, pz})
				}
			}
		}
	}
	veg := placed
	if f.waterlog {
		veg = nil
		for _, p := range placed {
			if f.enclosed(w, p) {
				veg = append(veg, p)
			}
		}
		for _, p := range veg {
			w.set(p[0], p[1], p[2], o.ids.water)
		}
	}
	if len(veg) == 0 {
		return blockPos{}, false
	}
	if f.vegChance > 0 {
		for _, p := range veg {
			if !(f.vegChance > r.Float()) {
				continue
			}
			vy := p[1]
			if f.waterlog {
				vy--
			}
			f.vegetation.place(w, p[0], vy-dy, p[2], r)
		}
	}
	return veg[len(veg)-1], true
}

// placeGround lays up to depth ground blocks from (x, y, z) into the
// surface. Blocks that already are the ground block are kept; it stops at
// a block it may not replace, which counts as placed past the first layer.
func (f *vegetationPatchFeature) placeGround(w *featureWorld, x, y, z, dy, depth int32) bool {
	o := w.o
	ground := o.blockName(f.ground)
	for i := int32(0); i < depth; i++ {
		b, _ := w.block(x, y+dy*i, z)
		if o.blockName(b) == ground {
			continue
		}
		if !f.replace.match(o, b) {
			return i != 0
		}
		w.set(x, y+dy*i, z, f.ground)
	}
	return true
}

// faceProbe is a neighbour offset and the face of that neighbour that must
// be sturdy.
type faceProbe struct {
	dx, dy, dz int32
	face       int
}

// poolWalls are the four sides and the bottom of a pool block, in the
// order they are checked.
func poolWalls() [5]faceProbe {
	return [5]faceProbe{{0, 0, -1, faceSouth}, {1, 0, 0, faceWest}, {0, 0, 1, faceNorth}, {-1, 0, 0, faceEast}, {0, -1, 0, faceUp}}
}

// enclosed reports whether a pool block is held in by sturdy blocks on its
// four sides and below.
func (f *vegetationPatchFeature) enclosed(w *featureWorld, p blockPos) bool {
	o := w.o
	for _, s := range poolWalls() {
		b, _ := w.block(p[0]+s.dx, p[1]+s.dy, p[2]+s.dz)
		if !o.sturdyFace(b, s.face) {
			return false
		}
	}
	return true
}

// weightedBlock is one entry of a weighted block list.
type weightedBlock struct {
	block  uint32
	weight float32
}

// pickWeighted draws an entry the way BDS does: the weights summed as
// floats truncated after each addition, one nextInt of the total (none when
// it is 0), then the first entry the draw falls into.
func pickWeighted[T any](r *rand.MT, list []T, weight func(T) float32) T {
	total := int32(0)
	for _, e := range list {
		total = int32(float32(total) + weight(e))
	}
	n := int32(0)
	if total != 0 {
		n = int32(r.Uint32() % uint32(total))
	}
	for i, e := range list {
		n = int32(float32(n) - weight(e))
		if n < 0 || i == len(list)-1 {
			return e
		}
	}
	return list[len(list)-1]
}

// growingPlantFeature grows a column of body blocks ending in a head
// (GrowingPlantFeature, sub_D2E0BF0): cave vines down from a ceiling, big
// dripleaves up from a floor.
type growingPlantFeature struct {
	heights    []weightedRange
	up         bool
	age        intRange
	body, head []weightedBlock
	allowWater bool
}

type weightedRange struct {
	r      intRange
	weight float32
}

// rawGrowingPlant is the JSON of a minecraft:growing_plant_feature.
type rawGrowingPlant struct {
	Heights    [][]json.RawMessage `json:"height_distribution"`
	Direction  string              `json:"growth_direction"`
	Age        json.RawMessage     `json:"age"`
	Body       [][]json.RawMessage `json:"body_blocks"`
	Head       [][]json.RawMessage `json:"head_blocks"`
	AllowWater bool                `json:"allow_water"`
}

func (o *Overworld) parseGrowingPlant(raw json.RawMessage) (placer, error) {
	var r rawGrowingPlant
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	f := &growingPlantFeature{up: strings.EqualFold(r.Direction, "up"), age: parseIntRange(r.Age), allowWater: r.AllowWater}
	for _, e := range r.Heights {
		if len(e) != 2 {
			return nil, nil
		}
		var wgt float32
		if !jsonOK(e[1], &wgt) {
			return nil, nil
		}
		f.heights = append(f.heights, weightedRange{parseIntRange(e[0]), wgt})
	}
	f.body, f.head = o.parseWeightedBlocks(r.Body), o.parseWeightedBlocks(r.Head)
	if len(f.heights) == 0 || len(f.body) == 0 || len(f.head) == 0 {
		return nil, nil
	}
	return f, nil
}

// parseWeightedBlocks reads a list of [block, weight] pairs; nil when an
// entry is malformed or its block is missing.
func (o *Overworld) parseWeightedBlocks(list [][]json.RawMessage) []weightedBlock {
	var out []weightedBlock
	for _, e := range list {
		if len(e) != 2 {
			return nil
		}
		var b biome.Block
		var wgt float32
		if json.Unmarshal(e[0], &b) != nil || json.Unmarshal(e[1], &wgt) != nil {
			return nil
		}
		id, ok := o.lookupBlock(b)
		if !ok {
			return nil
		}
		out = append(out, weightedBlock{id, wgt})
	}
	return out
}

// weightedBlockWeight is the weight of a weighted block entry.
func weightedBlockWeight(b weightedBlock) float32 { return b.weight }

func (f *growingPlantFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	o := w.o
	h := pickWeighted(r, f.heights, func(e weightedRange) float32 { return e.weight }).r.draw(r)
	age := f.age.draw(r)
	dy := int32(-1)
	if f.up {
		dy = 1
	}
	for i := int32(0); i < h; i++ {
		py := y + dy*i
		b, ok := w.block(x, py, z)
		if !ok {
			continue
		}
		if b != o.ids.air && !(f.allowWater && o.waterMaterial(b)) {
			continue
		}
		next, _ := w.block(x, py+dy, z)
		if i == h-1 || next != o.ids.air {
			head := pickWeighted(r, f.head, weightedBlockWeight).block
			if f.age.max != 0 {
				head = o.withProp(head, "growing_plant_age", float64(age))
			}
			w.set(x, py, z, head)
			return blockPos{x, py, z}, true
		}
		w.set(x, py, z, pickWeighted(r, f.body, weightedBlockWeight).block)
	}
	return blockPos{}, false
}

// vinesSingleFaceFeature hangs one vine on the first solid side of an air
// block, trying north, east, south, west (VinesSingleFaceFeature,
// sub_49EBFD0).
type vinesSingleFaceFeature struct {
	vine, invisibleBedrock uint32
}

// singleFaceVines are the vine_direction_bits of a vine held by a block to the
// north, east, south and west (sub_CEC5A70 with the table set at
// sub_CED62C0).
type vineSupport struct {
	dx, dz int32
	bits   float64
}

func singleFaceVines() [4]vineSupport {
	return [4]vineSupport{{0, -1, 4}, {1, 0, 8}, {0, 1, 1}, {-1, 0, 2}}
}

func (f *vinesSingleFaceFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	o := w.o
	if b, _ := w.block(x, y, z); b != o.ids.air {
		return blockPos{}, false
	}
	for _, s := range singleFaceVines() {
		b, _ := w.block(x+s.dx, y, z+s.dz)
		// VERIFY: BDS wants the block's solid flag (BlockLegacy +266 bit 2)
		// and a solid material; full blocks by name stand in for both.
		if b == f.invisibleBedrock || !o.sturdyFace(b, faceUp) {
			continue
		}
		w.set(x, y, z, o.withProp(f.vine, "vine_direction_bits", s.bits))
		return blockPos{x, y, z}, true
	}
	return blockPos{}, false
}

// mossVegetationFeature puts one weighted plant on a sturdy floor
// (VegetationFeature, sub_49EB6E0, with the moss list built at
// sub_49BC9C0): flowering azalea 4, azalea 7, moss carpet 25, short grass
// 50, tall grass 10. Double plants get their upper half.
type mossVegetationFeature struct {
	plants []weightedBlock
	double map[uint32]bool
}

func (o *Overworld) newMossVegetation(id func(string) uint32) *mossVegetationFeature {
	f := &mossVegetationFeature{double: map[uint32]bool{}}
	f.plants = append(f.plants, []weightedBlock{{id("minecraft:flowering_azalea"), 4}, {id("minecraft:azalea"), 7},
		{id("minecraft:moss_carpet"), 25}, {id(blockShortGrass), 50}, {id(blockTallGrass), 10}}...)
	f.double[id(blockTallGrass)] = true
	return f
}

// newPaleMossVegetation is the pale garden's VegetationFeature (list built at
// sub_49BC9C0): pale moss carpet 25, short grass 25, tall grass 10.
func (o *Overworld) newPaleMossVegetation(id func(string) uint32) *mossVegetationFeature {
	f := &mossVegetationFeature{double: map[uint32]bool{id(blockTallGrass): true}}
	f.plants = []weightedBlock{{id("minecraft:pale_moss_carpet"), 25}, {id(blockShortGrass), 25}, {id(blockTallGrass), 10}}
	return f
}

func (f *mossVegetationFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	o := w.o
	plant := pickWeighted(r, f.plants, func(b weightedBlock) float32 { return b.weight }).block
	if b, _ := w.block(x, y, z); b != o.ids.air {
		return blockPos{}, false
	}
	below, _ := w.block(x, y-1, z)
	if !o.sturdyFace(below, faceUp) || !o.plantSurvives(plant, below) {
		return blockPos{}, false
	}
	if f.double[plant] {
		w.set(x, y+1, z, o.withProp(plant, "upper_block_bit", true))
	}
	w.set(x, y, z, plant)
	return blockPos{x, y, z}, true
}

// plantSurvives is the plant's mayPlace on the block below (BlockLegacy
// vfunc +536).
// VERIFY: written from the blocks' vanilla rules, not read from BDS.
func (o *Overworld) plantSurvives(plant, below uint32) bool {
	switch o.blockName(plant) {
	case "minecraft:moss_carpet", "minecraft:pale_moss_carpet":
		return below != o.ids.air
	case "minecraft:azalea", "minecraft:flowering_azalea":
		return o.hasBlockTag(below, "dirt") || o.blockName(below) == "minecraft:clay" || o.blockName(below) == "minecraft:moss_block"
	case "minecraft:small_dripleaf_block", "minecraft:big_dripleaf":
		return o.hasBlockTag(below, "dirt") || o.blockName(below) == "minecraft:clay" || o.blockName(below) == "minecraft:moss_block"
	}
	return o.hasBlockTag(below, "dirt") || o.blockName(below) == "minecraft:moss_block"
}

// dripleafFeature is BDS's DripleafFeature (sub_49A13C0): one in five is a
// small dripleaf facing a random way (sub_CF29ED0), the rest a big
// dripleaf feature picked by direction.
type dripleafFeature struct {
	small uint32
	big   [4]featureRef
}

func (o *Overworld) newDripleaf(id func(string) uint32, set *featureSet) *dripleafFeature {
	f := &dripleafFeature{small: id("minecraft:small_dripleaf_block")}
	for i, d := range []string{"south", "west", "north", "east"} {
		f.big[i] = featureRef{"minecraft:big_dripleaf_" + d + "_feature", set}
	}
	return f
}

func (f *dripleafFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	if r.Float() < 0.2 {
		return f.placeSmall(w, x, y, z, r.IntN(4))
	}
	return f.big[r.IntN(4)].place(w, x, y, z, r)
}

// placeSmall places a two-block small dripleaf.
// VERIFY: the direction state and the survival rule (vfuncs +536/+544) are
// taken from the block's vanilla rules.
func (f *dripleafFeature) placeSmall(w *featureWorld, x, y, z, dir int32) (blockPos, bool) {
	o := w.o
	b, _ := w.block(x, y, z)
	above, _ := w.block(x, y+1, z)
	below, _ := w.block(x, y-1, z)
	if !o.airOrWater(b) || !o.airOrWater(above) || !o.plantSurvives(f.small, below) {
		return blockPos{}, false
	}
	lower := o.withProp(o.withProp(f.small, stateCardinalDirection, legacyDirections()[dir]), "upper_block_bit", false)
	w.set(x, y, z, lower)
	w.set(x, y+1, z, o.withProp(lower, "upper_block_bit", true))
	return blockPos{x, y, z}, true
}
