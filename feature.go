package worldgen

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"math"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/helix-bedrock/worldgen/biome"
	"github.com/helix-bedrock/worldgen/rand"
)

// Feature rules (behavior pack feature_rules/*.json) decide where features
// are placed after the terrain is built. The rules and the features they
// place are Mojang's data; this file is the runtime that reads them, written
// from BDS 1.26.52's decorate step.
//
// Per chunk BDS seeds a population seed from the level seed and the chunk
// position. Every rule then gets its own Mersenne Twister seeded from that
// seed and the hash of the rule's name, so rules do not disturb each other's
// randomness: a missing feature type does not shift the others.

// passOrder is the order BDS runs placement passes in.
var passOrder = []string{
	"first_pass", "underground_pass", "after_underground_pass", "before_surface_pass",
	"surface_pass", "after_surface_pass", "before_sky_pass", "sky_pass", "after_sky_pass", "final_pass",
}

// placer is a feature type the runtime can place.
type placer interface {
	// place puts the feature at a position and reports where it ended (for
	// sequence features) and whether anything was placed.
	place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool)
}

type blockPos [3]int32

type distKind uint8

const (
	distConstant distKind = iota
	distUniform
	distGaussian
	distInverseGaussian
	distFixedGrid
	distJitteredGrid
	distTriangle
	// distHeightmap is query.heightmap(variable.worldx, variable.worldz).
	distHeightmap
	// distAboveTopSolid is query.above_top_solid(variable.worldx,
	// variable.worldz).
	distAboveTopSolid
)

// molang reports whether BDS evaluates the axis as Molang. A rule with any
// Molang axis takes BDS's slow path, which also stores each coordinate in
// variable.worldx/y/z as it goes.
func (a axis) molang() bool { return a.kind == distHeightmap || a.kind == distAboveTopSolid }

type axis struct {
	kind     distKind
	min, max int32
}

// featureRule is one feature_rules file resolved against the features.
type featureRule struct {
	id      string
	hash    uint32
	pass    int
	feature placer
	// iterations is fixed; rules whose iteration count is Molang the
	// runtime cannot evaluate yet are not loaded.
	iterations int32
	// iterMolang, when set, computes the iterations per chunk.
	iterMolang molang
	// scatter chance: either a fraction num/den, or a percent.
	num, den int32
	percent  float32
	order    [3]int // axis indices in evaluation order (0 x, 1 y, 2 z)
	axes     [3]axis
}

// coordinateOrders maps coordinate_eval_order to the axis order. BDS's enum
// is xyz, xzy, yxz, yzx, zxy, zyx (sub_C81F600).
var coordinateOrders = map[string][3]int{
	"xyz": {0, 1, 2}, "xzy": {0, 2, 1}, "yxz": {1, 0, 2},
	"yzx": {1, 2, 0}, "zxy": {2, 0, 1}, "zyx": {2, 1, 0},
}

// defaultOrder is the order used when a rule gives no coordinate_eval_order:
// xzy, so y can depend on the x and z just drawn. Checked against BDS
// chunks: with xyz, plains grass and sand discs land elsewhere.
var defaultOrder = [3]int{0, 2, 1}

// featureRules are all loaded rules, attached to biomes.
type featureRules struct {
	// byBiome[id][pass] lists a biome's rules for a pass in load order.
	byBiome [][][]*featureRule
}

type rawRule struct {
	Rules struct {
		Description struct {
			Identifier    string `json:"identifier"`
			PlacesFeature string `json:"places_feature"`
		} `json:"description"`
		Conditions struct {
			Pass   string          `json:"placement_pass"`
			Filter json.RawMessage `json:"minecraft:biome_filter"`
		} `json:"conditions"`
		Distribution struct {
			Iterations json.RawMessage `json:"iterations"`
			Scatter    json.RawMessage `json:"scatter_chance"`
			Order      string          `json:"coordinate_eval_order"`
			X, Y, Z    json.RawMessage
		} `json:"distribution"`
	} `json:"minecraft:feature_rules"`
}

// ruleHash is the hashed-string hash BDS keeps with a rule's name: 64-bit
// FNV-1 (multiply, then XOR), of which the rule seed uses the low 32 bits.
func ruleHash(s string) uint32 {
	h := uint64(0xCBF29CE484222325)
	for i := 0; i < len(s); i++ {
		h = (h * 0x100000001B3) ^ uint64(s[i])
	}
	return uint32(h)
}

// loadFeatureRules reads every rule under dir and attaches it to the biomes
// its filter accepts. Rules whose feature type is not implemented are left
// out.
func (o *Overworld) loadFeatureRules(fsys fs.FS, dir string, defs map[string]*biome.Def, biomeIDs map[string]uint32, features *featureSet) error {
	files, err := fs.Glob(fsys, path.Join(dir, "*.json"))
	if err != nil {
		return fmt.Errorf("worldgen: list feature rules: %w", err)
	}
	slices.Sort(files)
	var maxID uint32
	for _, id := range biomeIDs {
		maxID = max(maxID, id)
	}
	fr := &featureRules{byBiome: make([][][]*featureRule, maxID+1)}
	for _, f := range files {
		data, err := fs.ReadFile(fsys, f)
		if err != nil {
			return fmt.Errorf("worldgen: read %s: %w", f, err)
		}
		var raw rawRule
		if err := json.Unmarshal(data, &raw); err != nil {
			return fmt.Errorf("worldgen: decode %s: %w", f, err)
		}
		r := raw.Rules
		pass := slices.Index(passOrder, r.Conditions.Pass)
		if pass < 0 {
			continue
		}
		feat, ok := features.get(r.Description.PlacesFeature)
		if !ok {
			continue
		}
		rule := &featureRule{id: r.Description.Identifier, hash: ruleHash(r.Description.Identifier), pass: pass, feature: feat, order: defaultOrder}
		if it, ok := constInt(r.Distribution.Iterations); ok {
			if it <= 0 {
				continue
			}
			rule.iterations = it
		} else {
			var src string
			if json.Unmarshal(r.Distribution.Iterations, &src) != nil {
				continue
			}
			m, err := parseMolang(src)
			if err != nil {
				continue
			}
			rule.iterMolang = m
		}
		if len(r.Distribution.Scatter) > 0 {
			var frac struct{ Numerator, Denominator int32 }
			if json.Unmarshal(r.Distribution.Scatter, &frac) == nil && frac.Denominator > 0 {
				rule.num, rule.den = frac.Numerator, frac.Denominator
			} else if v, ok := constFloat(r.Distribution.Scatter); ok {
				rule.percent = v
			} else {
				var src string
				if json.Unmarshal(r.Distribution.Scatter, &src) != nil {
					continue
				}
				m, err := parseMolang(src)
				if err != nil {
					continue
				}
				// Only the world's legacy flag and constants appear here.
				rule.percent = m.eval(&molangEnv{})
			}
		} else {
			rule.percent = 100
		}
		if r.Distribution.Order != "" {
			rule.order, ok = coordinateOrders[r.Distribution.Order]
			if !ok {
				continue
			}
		}
		good := true
		for i, a := range []json.RawMessage{r.Distribution.X, r.Distribution.Y, r.Distribution.Z} {
			if rule.axes[i], ok = parseAxis(a); !ok {
				good = false
			}
		}
		if !good {
			continue
		}
		for name, id := range biomeIDs {
			d, ok := defs[name]
			if !ok {
				continue
			}
			if ok, err := filterAccepts(r.Conditions.Filter, d.Tags); err != nil {
				return fmt.Errorf("worldgen: rule %s: %w", rule.id, err)
			} else if ok {
				if fr.byBiome[id] == nil {
					fr.byBiome[id] = make([][]*featureRule, len(passOrder))
				}
				fr.byBiome[id][pass] = append(fr.byBiome[id][pass], rule)
			}
		}
	}
	// Rules keep file name order. BDS reads pack rules first, then the
	// definitions/feature_rules folder in directory order (sub_D403EB0);
	// against BDS chunks, file name order fits best, except that BDS places
	// the sand disc before the clay disc. See notes/TODO.md.
	o.rules = fr
	return nil
}

func constFloat(raw json.RawMessage) (float32, bool) {
	var f float64
	if json.Unmarshal(raw, &f) == nil {
		return float32(f), true
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if f, err := strconv.ParseFloat(strings.TrimSpace(s), 32); err == nil {
			return float32(f), true
		}
	}
	return 0, false
}

// constInt reads a number or a numeric Molang string, rounded as BDS does.
func constInt(raw json.RawMessage) (int32, bool) {
	f, ok := constFloat(raw)
	if !ok {
		return 0, false
	}
	return int32(math.Round(float64(f))), true
}

func parseAxis(raw json.RawMessage) (axis, bool) {
	if v, ok := constInt(raw); ok {
		return axis{kind: distConstant, min: v, max: v}, true
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		switch strings.ReplaceAll(s, " ", "") {
		case "query.heightmap(variable.worldx,variable.worldz)":
			return axis{kind: distHeightmap}, true
		case "query.above_top_solid(variable.worldx,variable.worldz)":
			return axis{kind: distAboveTopSolid}, true
		}
	}
	var a struct {
		Distribution string            `json:"distribution"`
		Extent       []json.RawMessage `json:"extent"`
	}
	if json.Unmarshal(raw, &a) != nil || len(a.Extent) != 2 {
		return axis{}, false
	}
	lo, ok1 := constInt(a.Extent[0])
	hi, ok2 := constInt(a.Extent[1])
	if !ok1 || !ok2 {
		return axis{}, false
	}
	k := map[string]distKind{
		"uniform": distUniform, "gaussian": distGaussian, "inverse_gaussian": distInverseGaussian,
		"triangle": distTriangle,
	}
	kind, ok := k[a.Distribution]
	if !ok {
		// fixed_grid and jittered_grid keep per-axis state; not needed by
		// any rule the runtime loads yet.
		return axis{}, false
	}
	return axis{kind: kind, min: lo, max: hi}, true
}

// filterAccepts evaluates a biome filter against a biome's tags. A list is
// all_of; objects are any_of, all_of, none_of or a has_biome_tag test.
func filterAccepts(raw json.RawMessage, tags []string) (bool, error) {
	if len(raw) == 0 || string(raw) == `""` {
		return false, nil
	}
	var list []json.RawMessage
	if json.Unmarshal(raw, &list) == nil {
		for _, e := range list {
			ok, err := filterAccepts(e, tags)
			if err != nil || !ok {
				return false, err
			}
		}
		return true, nil
	}
	var obj struct {
		AnyOf    []json.RawMessage `json:"any_of"`
		AllOf    []json.RawMessage `json:"all_of"`
		NoneOf   []json.RawMessage `json:"none_of"`
		Test     string            `json:"test"`
		Operator string            `json:"operator"`
		Value    string            `json:"value"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return false, fmt.Errorf("biome filter %s: %w", raw, err)
	}
	switch {
	case obj.AnyOf != nil:
		for _, e := range obj.AnyOf {
			ok, err := filterAccepts(e, tags)
			if err != nil {
				return false, err
			}
			if ok {
				return true, nil
			}
		}
		return false, nil
	case obj.AllOf != nil:
		return filterAccepts(mustList(obj.AllOf), tags)
	case obj.NoneOf != nil:
		for _, e := range obj.NoneOf {
			ok, err := filterAccepts(e, tags)
			if err != nil || ok {
				return false, err
			}
		}
		return true, nil
	case obj.Test == "has_biome_tag":
		has := slices.Contains(tags, obj.Value)
		switch obj.Operator {
		case "", "==", "equals":
			return has, nil
		case "!=", "not":
			return !has, nil
		}
		return false, fmt.Errorf("biome filter operator %q", obj.Operator)
	}
	return false, fmt.Errorf("biome filter %s not supported", raw)
}

func mustList(l []json.RawMessage) json.RawMessage {
	b, _ := json.Marshal(l)
	return b
}

// populationSeed is BDS's per-chunk feature seed (postProcess): a Mersenne
// Twister seeded with the level seed gives two odd multipliers, combined
// with the chunk position and XORed with the seed again. nextInt() is the
// raw output shifted right by one.
func populationSeed(levelSeed int64, chunkX, chunkZ int32) uint32 {
	s := uint32(levelSeed)
	m := rand.NewMT(s)
	a := m.Uint32()>>1 | 1
	b := m.Uint32()>>1 | 1
	return (b*uint32(chunkZ) + a*uint32(chunkX)) ^ s
}

// ruleSeed combines the population seed with a rule's hash
// (boost::hash_combine).
func ruleSeed(pop, hash uint32) uint32 {
	return pop ^ ((pop >> 2) + (pop << 6) + hash + 0x9E3779B9)
}

// sample draws one axis value.
func (a axis) sample(r *rand.MT) int32 {
	switch a.kind {
	case distUniform:
		if a.max <= a.min {
			return a.min
		}
		return a.min + r.IntN(a.max-a.min)
	case distTriangle:
		d := a.max - a.min
		return a.min + r.IntN(d/2+1) + r.IntN(d-d/2+1)
	case distGaussian:
		h := uint32((a.max - a.min) / 2)
		mid := a.min + int32(h)
		if a.max-a.min+1 < 3 || h == 0 {
			return mid
		}
		return mid + int32(r.Uint32()%h) - int32(r.Uint32()%h)
	case distInverseGaussian:
		h := uint32((a.max - a.min) / 2)
		if h == 0 {
			return a.min
		}
		first := int32(r.Uint32() % h)
		d := first - int32(r.Uint32()%h)
		switch {
		case d < 0:
			return a.min - d
		case d > 0:
			return a.max - d
		}
		if r.Uint32()&1 != 0 {
			return a.max
		}
		return a.min
	}
	return a.min
}

// featureWorld is the block view one origin chunk's features place into.
// They see the undecorated terrain plus their own placements only
// (overlay), never other chunks' decorations. So a feature reaching over a
// chunk border makes the same decisions whichever chunk is being built, its
// parts in neighbouring chunks always agree, and an origin's placements can
// be computed once and applied to each chunk they reach. BDS instead sees
// whatever its neighbours placed first, which depends on chunk load order.
type featureWorld struct {
	o         *Overworld
	ox, oz    int32 // the origin chunk
	slots     [16]writeList
	neighbour func(cx, cz int32) *PackedChunk
	lastX     int32
	lastZ     int32
	lastChunk *PackedChunk

	// overlay holds the current origin's placements, one array per chunk
	// column they touch (a map lookup per block read was most of the
	// decoration time); tops is the highest opaque block each placement put
	// in a column.
	overlay  map[[2]int32]*overlayChunk
	lastOvX  int32
	lastOvZ  int32
	lastOv   *overlayChunk
	lastOvOK bool
	tops     map[[2]int32]int32
}

// overlayChunk holds one origin's placements in one chunk column: set has a
// bit per block that was placed, blocks the placed states.
type overlayChunk struct {
	set    [16 * 16 * Height / 64]uint64
	blocks [16 * 16 * Height]uint32
}

// overlayAt returns the overlay of a chunk column, creating it if create is
// set; otherwise it is nil when this origin placed nothing there.
func (w *featureWorld) overlayAt(cx, cz int32, create bool) *overlayChunk {
	if w.lastOvOK && cx == w.lastOvX && cz == w.lastOvZ && (w.lastOv != nil || !create) {
		return w.lastOv
	}
	oc := w.overlay[[2]int32{cx, cz}]
	if oc == nil && create {
		oc, _ = w.o.overlays.Get().(*overlayChunk)
		if oc == nil {
			oc = new(overlayChunk)
		}
		w.overlay[[2]int32{cx, cz}] = oc
	}
	w.lastOvX, w.lastOvZ, w.lastOv, w.lastOvOK = cx, cz, oc, true
	return oc
}

// release returns the overlay arrays to the pool.
func (w *featureWorld) release() {
	for k, oc := range w.overlay {
		clear(oc.set[:])
		w.o.overlays.Put(oc)
		delete(w.overlay, k)
	}
	w.lastOv, w.lastOvOK = nil, false
}

// chunk returns the undecorated chunk holding a column.
func (w *featureWorld) chunk(x, z int32) *PackedChunk {
	cx, cz := x>>4, z>>4
	if w.lastChunk != nil && cx == w.lastX && cz == w.lastZ {
		return w.lastChunk
	}
	c := w.neighbour(cx, cz)
	w.lastX, w.lastZ, w.lastChunk = cx, cz, c
	return c
}

// block returns the block at a world position; ok is false outside the
// world height or when the chunk is not available.
func (w *featureWorld) block(x, y, z int32) (uint32, bool) {
	if y < MinY || y >= MaxY {
		return 0, false
	}
	i := int(y-MinY)<<8 | int(z&15)<<4 | int(x&15)
	if oc := w.overlayAt(x>>4, z>>4, false); oc != nil && oc.set[i>>6]&(1<<(i&63)) != 0 {
		return oc.blocks[i], true
	}
	c := w.chunk(x, z)
	if c == nil {
		return 0, false
	}
	return c.at(i), true
}

// set places a block for the current origin, and into the target chunk if
// it is inside it.
func (w *featureWorld) set(x, y, z int32, b uint32) {
	if y < MinY || y >= MaxY {
		return
	}
	oi := int(y-MinY)<<8 | int(z&15)<<4 | int(x&15)
	oc := w.overlayAt(x>>4, z>>4, true)
	oc.set[oi>>6] |= 1 << (oi & 63)
	oc.blocks[oi] = b
	if b != w.o.ids.air && w.o.reg.LightDampening(b) >= 15 {
		col := [2]int32{x, z}
		if t, ok := w.tops[col]; !ok || y > t {
			w.tops[col] = y
		}
	}
	if sx, sz := x>>4-w.ox+1, z>>4-w.oz+1; uint32(sx) < 4 && uint32(sz) < 4 {
		l := &w.slots[sx*4+sz]
		l.pos = append(l.pos, uint32(oi))
		l.blk = append(l.blk, uint16(b))
	}
}

// writeList is an origin's placements in one chunk, in placement order: the
// Chunk index and the state of each.
type writeList struct {
	pos []uint32
	blk []uint16
}

// featureWrites is everything one origin chunk's features place, for the
// 4x4 chunks it can reach: from one chunk west/north of the origin to two
// east/south (Decorate runs the origins two chunks west/north to one
// east/south of a chunk). It is immutable once made.
type featureWrites struct {
	off [17]int32 // slot i holds pos/blk[off[i]:off[i+1]]
	// uses counts the chunks that took these placements (TerrainCache).
	uses atomic.Int32
	pos  []uint32
	blk  []uint16
}

// originFeatures runs the rules of origin chunk (ox, oz). It returns nil
// when neighbour cannot provide the origin.
func (o *Overworld) originFeatures(ox, oz int32, neighbour func(cx, cz int32) *PackedChunk) *featureWrites {
	origin := neighbour(ox, oz)
	if origin == nil || o.rules == nil {
		return nil
	}
	w := &featureWorld{o: o, ox: ox, oz: oz, neighbour: neighbour, lastX: math.MaxInt32,
		overlay: map[[2]int32]*overlayChunk{}, tops: map[[2]int32]int32{}}
	defer w.release()
	o.decorateFrom(w, ox, oz, origin)
	n := 0
	for i := range w.slots {
		n += len(w.slots[i].pos)
	}
	fw := &featureWrites{pos: make([]uint32, 0, n), blk: make([]uint16, 0, n)}
	for i := range w.slots {
		fw.off[i] = int32(len(fw.pos))
		fw.pos = append(fw.pos, w.slots[i].pos...)
		fw.blk = append(fw.blk, w.slots[i].blk...)
	}
	fw.off[16] = int32(n)
	return fw
}

// applyWrites applies the placements an origin made in chunk slot to c,
// whose undecorated terrain is base.
func (o *Overworld) applyWrites(c *Chunk, base *PackedChunk, fw *featureWrites, slot int) {
	if fw == nil {
		return
	}
	from, to := fw.off[slot], fw.off[slot+1]
	for k := from; k < to; k++ {
		i, b := fw.pos[k], fw.blk[k]
		// A plant an earlier origin placed here stays: this origin cannot
		// see it, but in BDS the feature would have found the spot taken.
		// Without this, half of a large fern could be replaced by a fern.
		if cur := c.Blocks[i]; o.soilPlant[b] && o.soilPlant[cur] && base != nil && base.at(int(i)) != uint32(cur) {
			continue
		}
		c.Blocks[i] = b
	}
}

// heightmap is the y above the highest block of a column that is fully
// opaque (light dampening 15), as BDS's height map: air, plants, snow
// layers, leaves and water do not count.
func (w *featureWorld) heightmap(x, z int32) int32 {
	c := w.chunk(x, z)
	if c == nil {
		return MinY
	}
	h := int32(MinY)
	lx, lz := int(x&15), int(z&15)
	for y := MaxY - 1; y >= MinY; y-- {
		if b := c.at((y-MinY)<<8 | lz<<4 | lx); b != w.o.ids.air && w.o.reg.LightDampening(b) >= 15 {
			h = int32(y + 1)
			break
		}
	}
	if t, ok := w.tops[[2]int32{x, z}]; ok && t+1 > h {
		h = t + 1
	}
	return h
}

// aboveTopSolid is query.above_top_solid: the y above the highest block of
// a column whose material is solid, looking through leaves (BDS's
// LevelChunk::getAboveTopSolidBlock, sub_C9F7970, as the query calls it).
func (w *featureWorld) aboveTopSolid(x, z int32) int32 {
	for y := int32(MaxY - 1); y >= MinY; y-- {
		b, ok := w.block(x, y, z)
		if !ok {
			return MinY
		}
		if w.o.solidMaterial(b) {
			return y + 1
		}
	}
	return MinY
}

// biomeTags returns the tags of the biome at a position.
func (w *featureWorld) biomeTags(x, y, z int32) []string {
	c := w.chunk(x, z)
	if c == nil || y < MinY || y >= MaxY {
		return nil
	}
	b := c.Biome(int(x&15), int(y), int(z&15))
	if int(b) < len(w.o.biomeTags) {
		return w.o.biomeTags[b]
	}
	return nil
}

// Decorate runs the feature rules on c, a copy of the terrain of chunk
// (chunkX, chunkZ) from GenerateChunk. Features started in nearby chunks
// reach into c, so neighbour must return the unchanged GenerateChunk output
// of any chunk within 3 chunks, the target's own included (it may return nil for a chunk it cannot provide; features then
// skip it). It is safe to call from many goroutines if neighbour is.
func (o *Overworld) Decorate(chunkX, chunkZ int32, c *Chunk, neighbour func(cx, cz int32) *PackedChunk) {
	o.decorate(chunkX, chunkZ, c, neighbour(chunkX, chunkZ), func(ox, oz int32) *featureWrites {
		return o.originFeatures(ox, oz, neighbour)
	}, neighbour)
}

// decorate applies the placements of the origins that reach chunk
// (chunkX, chunkZ), then freezes it. An ore can start up to 35 blocks
// east/south of its chunk's corner and 5 blocks west/north of it, so the
// origins run from two chunks west/north to one east/south.
func (o *Overworld) decorate(chunkX, chunkZ int32, c *Chunk, base *PackedChunk, writes func(ox, oz int32) *featureWrites, terrain func(cx, cz int32) *PackedChunk) {
	if o.rules == nil {
		return
	}
	o.placeVillages(chunkX, chunkZ, c, func(x, z int32) int32 { return o.surfaceHeight(terrain, x, z) })
	for ox := chunkX - 2; ox <= chunkX+1; ox++ {
		for oz := chunkZ - 2; oz <= chunkZ+1; oz++ {
			o.applyWrites(c, base, writes(ox, oz), int(chunkX-ox+1)*4+int(chunkZ-oz+1))
		}
	}
	o.freeze(chunkX, chunkZ, c)
	o.keepWater(c, base)
	o.dropOverwrittenBlockEntities(c)
}

// keepWater marks the blocks that features put where the terrain had source
// water and that can hold water. BDS keeps the water as the block's second
// layer; without it the client draws an air pocket around the plant.
func (o *Overworld) keepWater(c *Chunk, base *PackedChunk) {
	clear(c.Water[:])
	if base == nil {
		return
	}
	for s := range base.subs {
		if sub := &base.subs[s]; sub.idx == nil && sub.wide == nil && uint32(sub.one) != o.ids.water {
			continue
		}
		for i := s << 12; i < (s+1)<<12; i++ {
			if b := uint32(c.Blocks[i]); base.at(i) == o.ids.water && b != o.ids.water && waterloggable(o.blockName(b)) {
				c.Water[i>>6] |= 1 << (i & 63)
			}
		}
	}
}

// waterloggable reports whether a block keeps the water it is placed in.
// VERIFY: these are the blocks BDS 1.26.52 worlds hold with water in their
// second layer after generation (seagrass, kelp, coral fans and wall fans,
// sea pickles, glow lichen, chests); BDS decides by the block's
// can-contain-liquid flag, whose table was not read.
func waterloggable(name string) bool {
	switch name {
	case "minecraft:seagrass", "minecraft:kelp", "minecraft:sea_pickle", "minecraft:glow_lichen", "minecraft:chest":
		return true
	}
	return strings.HasSuffix(name, "_coral_fan") || strings.HasSuffix(name, "_coral_wall_fan")
}

// chunkBiomes lists the distinct biomes of a chunk in BDS's order: sub-chunk
// by sub-chunk from the bottom, in order of first appearance.
func chunkBiomes(c *Chunk) []uint32 {
	var out []uint32
	for _, b := range c.Biomes {
		if !slices.Contains(out, b) {
			out = append(out, b)
		}
	}
	return out
}

// decorateFrom runs the rules of the chunk at (ox, oz) whose biomes origin
// holds.
func (o *Overworld) decorateFrom(w *featureWorld, ox, oz int32, origin *PackedChunk) {
	pop := populationSeed(o.seed, ox, oz)
	bs := origin.biomeList
	single := len(bs) == 1
	var rule, feat rand.MT
	for pass := range passOrder {
		for _, b := range bs {
			if int(b) >= len(o.rules.byBiome) || o.rules.byBiome[b] == nil {
				continue
			}
			// variable.worldx/y/z live in the Molang environment BDS makes
			// once per pass and biome (sub_C7544E0): unset (0) at first,
			// then the last coordinates a slow-path rule evaluated. An axis
			// evaluated before another reads that axis' previous value, as
			// the y of rules in xyz order does for z.
			var world [3]int32
			for _, r := range o.rules.byBiome[b][pass] {
				seed := ruleSeed(pop, r.hash)
				rule.Seed(seed)
				n := r.iterations
				if r.den > 0 {
					if r.num != r.den && rule.IntN(r.den) >= r.num {
						continue
					}
				} else if r.percent < 100 {
					if r.percent <= 0 || r.percent <= rule.Float()*100 {
						continue
					}
				}
				if r.iterMolang != nil {
					env := &molangEnv{originX: float32(ox * 16), originZ: float32(oz * 16), noise: o.queryNoise}
					n = int32(math.Round(float64(r.iterMolang.eval(env))))
				}
				// The feature random is a second generator with the same seed.
				feat.Seed(seed)
				base := [3]int32{ox * 16, 0, oz * 16}
				slow := r.axes[0].molang() || r.axes[1].molang() || r.axes[2].molang()
				for i := n - 1; i >= 0; i-- {
					var pos [3]int32
					for _, a := range r.order {
						switch r.axes[a].kind {
						case distHeightmap:
							pos[a] = w.heightmap(world[0], world[2])
						case distAboveTopSolid:
							pos[a] = w.aboveTopSolid(world[0], world[2])
						default:
							pos[a] = base[a] + r.axes[a].sample(&rule)
						}
						if slow {
							world[a] = pos[a]
						}
					}
					if !single {
						if pos[1] < MinY || pos[1] >= MaxY || origin.Biome(int(pos[0]&15), int(pos[1]), int(pos[2]&15)) != b {
							continue
						}
					}
					r.feature.place(w, pos[0], pos[1], pos[2], &feat)
				}
			}
		}
	}
}

// queryNoise is Molang's query.noise(x, z) in feature rules.
// VERIFY: assumed to be the biome-info simplex noise (one octave, Mersenne
// Twister seeded 2345) that the frozen-ocean temperature also uses, as in
// Java; the plains grass and flower rules use it like Java does.
func (o *Overworld) queryNoise(x, z float32) float32 {
	return o.surfNoise.frozenB.Sum2(x, z)
}

// dropOverwrittenBlockEntities removes the block entities of structure
// blocks a later feature replaced (a tree's leaves over a village bed).
func (o *Overworld) dropOverwrittenBlockEntities(c *Chunk) {
	kept := c.BlockEntities[:0]
	for _, be := range c.BlockEntities {
		b := c.Block(int(be.X&15), int(be.Y), int(be.Z&15))
		if o.blockEntityFor(b, "", be.X, be.Y, be.Z) != nil {
			kept = append(kept, be)
		}
	}
	clear(c.BlockEntities[len(kept):])
	c.BlockEntities = kept
}
