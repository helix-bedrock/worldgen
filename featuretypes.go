package worldgen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"math"
	"path"
	"regexp"
	"strings"

	"github.com/helix-bedrock/worldgen/biome"
	"github.com/helix-bedrock/worldgen/rand"
)

// The data-driven feature types (behavior pack features/*.json) the runtime
// implements, and the hard-coded legacy tree features vanilla rules name.
// Features refer to each other by identifier; a reference to a type the
// runtime does not implement places nothing and draws no random numbers.

// featureSet holds the loadable features by identifier.
type featureSet struct {
	byID map[string]placer
}

func (s *featureSet) get(id string) (placer, bool) {
	p, ok := s.byID[id]
	return p, ok
}

// featureRef names another feature; it is resolved when placed so features
// may refer to each other in any order.
type featureRef struct {
	id  string
	set *featureSet
}

func (f featureRef) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	p, ok := f.set.byID[f.id]
	if !ok {
		return blockPos{}, false
	}
	return p.place(w, x, y, z, r)
}

// stripComments removes // comments outside strings; a few vanilla files
// have them.
func stripComments(b []byte) []byte {
	out := make([]byte, 0, len(b))
	inStr := false
	for i := 0; i < len(b); i++ {
		c := b[i]
		if inStr {
			out = append(out, c)
			if c == '\\' && i+1 < len(b) {
				i++
				out = append(out, b[i])
			} else if c == '"' {
				inStr = false
			}
			continue
		}
		if c == '"' {
			inStr = true
		} else if c == '/' && i+1 < len(b) && b[i+1] == '/' {
			for i < len(b) && b[i] != '\n' {
				i++
			}
			out = append(out, '\n')
			continue
		}
		out = append(out, c)
	}
	return out
}

// loadFeatures reads every feature under dir and keeps the types the runtime
// implements.
func (o *Overworld) loadFeatures(fsys fs.FS, dir string) (*featureSet, error) {
	files, err := fs.Glob(fsys, path.Join(dir, "*.json"))
	if err != nil {
		return nil, fmt.Errorf("worldgen: list features: %w", err)
	}
	set := &featureSet{byID: map[string]placer{}}
	for _, f := range files {
		data, err := fs.ReadFile(fsys, f)
		if err != nil {
			return nil, fmt.Errorf("worldgen: read %s: %w", f, err)
		}
		data = stripComments(data)
		var top map[string]json.RawMessage
		if err := json.Unmarshal(data, &top); err != nil {
			return nil, fmt.Errorf("worldgen: decode %s: %w", f, err)
		}
		for typ, raw := range top {
			if !strings.HasPrefix(typ, "minecraft:") {
				continue
			}
			var d struct {
				Description struct {
					Identifier string `json:"identifier"`
				} `json:"description"`
			}
			if err := json.Unmarshal(raw, &d); err != nil {
				return nil, fmt.Errorf("worldgen: %s: %w", f, err)
			}
			p, err := o.parseFeature(typ, raw, set)
			if err != nil {
				return nil, fmt.Errorf("worldgen: %s: %w", f, err)
			}
			if p != nil {
				set.byID[d.Description.Identifier] = p
			}
		}
	}
	for id, lt := range legacyTrees {
		set.byID[id] = &legacyTreeFeature{def: lt, set: set}
	}
	set.byID["minecraft:legacy:swamp_foliage_feature"] = &swampFoliageFeature{set: set}
	o.builtinFeatures(set)
	return set, nil
}

func (o *Overworld) parseFeature(typ string, raw json.RawMessage, set *featureSet) (placer, error) {
	switch typ {
	case "minecraft:ore_feature":
		ore, _, err := o.parseOre(raw)
		if ore == nil {
			return nil, err
		}
		return ore, err
	case "minecraft:aggregate_feature":
		var r struct {
			Features []string `json:"features"`
			EarlyOut string   `json:"early_out"`
		}
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, err
		}
		f := &aggregateFeature{firstFailure: r.EarlyOut == "first_failure", none: r.EarlyOut == "" || r.EarlyOut == "none"}
		for _, id := range r.Features {
			f.feats = append(f.feats, featureRef{id, set})
		}
		return f, nil
	case "minecraft:sequence_feature":
		var r struct {
			Features []string `json:"features"`
		}
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, err
		}
		f := &sequenceFeature{}
		for _, id := range r.Features {
			f.feats = append(f.feats, featureRef{id, set})
		}
		return f, nil
	case "minecraft:weighted_random_feature":
		var r struct {
			Features [][]json.RawMessage `json:"features"`
		}
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, err
		}
		f := &weightedFeature{}
		for _, e := range r.Features {
			if len(e) != 2 {
				return nil, fmt.Errorf("weighted entry %v", e)
			}
			var id string
			var wgt float64
			if json.Unmarshal(e[0], &id) != nil || json.Unmarshal(e[1], &wgt) != nil {
				return nil, fmt.Errorf("weighted entry %s", e)
			}
			f.feats = append(f.feats, featureRef{id, set})
			f.weights = append(f.weights, float32(wgt))
		}
		return f, nil
	case "minecraft:scatter_feature":
		return o.parseScatter(raw, set)
	case "minecraft:single_block_feature":
		return o.parseSingleBlock(raw)
	case "minecraft:search_feature":
		var r struct {
			Places string `json:"places_feature"`
			Volume struct {
				Min [3]int32 `json:"min"`
				Max [3]int32 `json:"max"`
			} `json:"search_volume"`
			Axis     string `json:"search_axis"`
			Required int32  `json:"required_successes"`
		}
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, err
		}
		return &searchFeature{feat: featureRef{r.Places, set}, min: r.Volume.Min, max: r.Volume.Max, axis: r.Axis, required: max(r.Required, 1)}, nil
	case "minecraft:tree_feature":
		return o.parseTree(raw, set)
	case "minecraft:horizontal_tree_decoration_feature":
		return o.parseHorizontalDecoration(raw)
	}
	return nil, nil
}

// aggregateFeature places its features at the same position in order
// (AggregateFeature, sub_D4100E0). It succeeds when any of them was placed,
// with the position the last placed one returned. first_success stops at
// the first one placed; first_failure stops at a failure only while none
// has been placed yet, so "tree, then optional extras" keeps going once
// the tree grew.
type aggregateFeature struct {
	feats        []featureRef
	firstFailure bool
	none         bool
}

func (f *aggregateFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	var last blockPos
	any := false
	for _, c := range f.feats {
		p, ok := c.place(w, x, y, z, r)
		if ok {
			last, any = p, true
			if !f.firstFailure && !f.none {
				return last, true
			}
		} else if f.firstFailure && !any {
			return blockPos{}, false
		}
	}
	return last, any
}

// sequenceFeature places each feature where the previous one ended.
type sequenceFeature struct {
	feats []featureRef
}

func (f *sequenceFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	cur := blockPos{x, y, z}
	for _, c := range f.feats {
		p, ok := c.place(w, cur[0], cur[1], cur[2], r)
		if !ok {
			return blockPos{}, false
		}
		cur = p
	}
	return cur, true
}

// weightedFeature places one feature picked by weight (BDS sums the weights
// as floats truncated to int after each addition).
type weightedFeature struct {
	feats   []featureRef
	weights []float32
}

func (f *weightedFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	total := int32(0)
	for _, wt := range f.weights {
		total = int32(float32(total) + wt)
	}
	if total <= 0 {
		return blockPos{}, false
	}
	n := int32(r.Uint32() % uint32(total))
	for i, wt := range f.weights {
		n -= int32(wt)
		if n < 0 {
			return f.feats[i].place(w, x, y, z, r)
		}
	}
	return blockPos{}, false
}

// scatterAxis is one axis of a scatter: a distribution, or a fixed or
// jittered grid.
type scatterAxis struct {
	axis
	grid         bool
	step, offset int32
}

// scatterFeature places a feature at positions around its input, with the
// same chance/iteration/distribution machinery as feature rules, drawing
// from the feature random it is given.
type scatterFeature struct {
	feat       featureRef
	iterations molang
	num, den   int32
	percent    molang
	order      [3]int
	axes       [3]scatterAxis
	toFloor    bool
}

func (o *Overworld) parseScatter(raw json.RawMessage, set *featureSet) (placer, error) {
	var r struct {
		Places     string          `json:"places_feature"`
		Iterations json.RawMessage `json:"iterations"`
		Scatter    json.RawMessage `json:"scatter_chance"`
		Order      string          `json:"coordinate_eval_order"`
		X, Y, Z    json.RawMessage
		ToFloor    bool `json:"project_input_to_floor"`
		// Newer files nest the distribution in its own object.
		Dist *struct {
			Iterations json.RawMessage `json:"iterations"`
			Scatter    json.RawMessage `json:"scatter_chance"`
			Order      string          `json:"coordinate_eval_order"`
			X, Y, Z    json.RawMessage
		} `json:"distribution"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	if d := r.Dist; d != nil {
		r.Iterations, r.Scatter, r.Order, r.X, r.Y, r.Z = d.Iterations, d.Scatter, d.Order, d.X, d.Y, d.Z
	}
	f := &scatterFeature{feat: featureRef{r.Places, set}, order: [3]int{0, 1, 2}, toFloor: r.ToFloor, percent: molangNum(100)}
	var err error
	if f.iterations, err = molangValue(r.Iterations, 1); err != nil {
		return nil, nil //nolint:nilerr // a rule whose Molang is not supported yet is skipped
	}
	if len(r.Scatter) > 0 {
		var frac struct{ Numerator, Denominator int32 }
		if bytes.HasPrefix(bytes.TrimSpace(r.Scatter), []byte("{")) {
			if err := json.Unmarshal(r.Scatter, &frac); err != nil {
				return nil, err
			}
			f.num, f.den = frac.Numerator, frac.Denominator
		} else if f.percent, err = molangValue(r.Scatter, 100); err != nil {
			return nil, nil //nolint:nilerr // a rule whose Molang is not supported yet is skipped
		}
	}
	if r.Order != "" {
		var ok bool
		if f.order, ok = coordinateOrders[r.Order]; !ok {
			return nil, nil
		}
	}
	for i, a := range []json.RawMessage{r.X, r.Y, r.Z} {
		ax, ok := parseScatterAxis(a)
		if !ok {
			return nil, nil
		}
		f.axes[i] = ax
	}
	return f, nil
}

// molangValue reads a number or a Molang string; a missing value is def.
func molangValue(raw json.RawMessage, def float32) (molang, error) {
	if len(raw) == 0 {
		return molangNum(def), nil
	}
	var f float64
	if json.Unmarshal(raw, &f) == nil {
		return molangNum(f), nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	return parseMolang(s)
}

func parseScatterAxis(raw json.RawMessage) (scatterAxis, bool) {
	if len(raw) == 0 {
		return scatterAxis{}, true
	}
	if a, ok := parseAxis(raw); ok {
		return scatterAxis{axis: a}, true
	}
	var g struct {
		Distribution string  `json:"distribution"`
		Extent       []int32 `json:"extent"`
		Step         int32   `json:"step_size"`
		Offset       int32   `json:"grid_offset"`
	}
	if json.Unmarshal(raw, &g) != nil || g.Distribution != "fixed_grid" || len(g.Extent) != 2 {
		return scatterAxis{}, false
	}
	return scatterAxis{axis: axis{min: g.Extent[0], max: g.Extent[1]}, grid: true, step: max(g.Step, 1), offset: g.Offset}, true
}

func (f *scatterFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	if f.toFloor {
		for y > MinY {
			b, ok := w.block(x, y-1, z)
			if !ok || b != w.o.ids.air {
				break
			}
			y--
		}
	}
	env := &molangEnv{biomeTags: w.biomeTags(x, y, z)}
	if f.den > 0 {
		if f.num != f.den && r.IntN(f.den) >= f.num {
			return blockPos{}, false
		}
	} else if c := f.percent.eval(env); c < 100 {
		if c <= 0 || c <= r.Float()*100 {
			return blockPos{}, false
		}
	}
	n := int32(math.Round(float64(f.iterations.eval(env))))
	base := [3]int32{x, y, z}
	placed := false
	for i := n - 1; i >= 0; i-- {
		var pos [3]int32
		idx := i
		for _, a := range f.order {
			ax := f.axes[a]
			if ax.grid {
				rng := ax.max - ax.min + 1
				v := ax.offset + ax.step*idx
				pos[a] = base[a] + ax.min + v%rng
				idx = v / rng
				continue
			}
			pos[a] = base[a] + ax.sample(r)
		}
		if _, ok := f.feat.place(w, pos[0], pos[1], pos[2], r); ok {
			placed = true
		}
	}
	return blockPos{x, y, z}, placed
}

// searchFeature tries its feature at each position of a volume along an
// axis until it has been placed the required number of times.
type searchFeature struct {
	feat     featureRef
	min, max [3]int32
	axis     string
	required int32
}

func (f *searchFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	ax, dir := 1, int32(1)
	if len(f.axis) == 2 {
		ax = map[byte]int{'x': 0, 'y': 1, 'z': 2}[f.axis[1]]
		if f.axis[0] == '-' {
			dir = -1
		}
	}
	lo, hi := f.min[ax], f.max[ax]
	start, end := lo, hi
	if dir < 0 {
		start, end = hi, lo
	}
	got := int32(0)
	for v := start; ; v += dir {
		// Only one-dimensional search volumes are used by vanilla data.
		pos := [3]int32{x + f.min[0], y + f.min[1], z + f.min[2]}
		pos[ax] = [3]int32{x, y, z}[ax] + v
		if _, ok := f.feat.place(w, pos[0], pos[1], pos[2], r); ok {
			got++
			if got >= f.required {
				return blockPos{x, y, z}, true
			}
		}
		if v == end {
			break
		}
	}
	return blockPos{}, false
}

// blockMatcher matches blocks by name or by block tag.
type blockMatcher struct {
	names map[string]bool
	tags  []string
}

var anyTagArg = regexp.MustCompile(`'([^']+)'`)

// parseMatcher reads a list of block descriptors: names, {"name": ...} or
// {"tags": "query.any_tag('dirt', ...)"}.
func parseMatcher(raw []json.RawMessage) blockMatcher {
	m := blockMatcher{names: map[string]bool{}}
	for _, e := range raw {
		var s string
		if json.Unmarshal(e, &s) == nil {
			m.names[s] = true
			continue
		}
		var d struct {
			Name string `json:"name"`
			Tags string `json:"tags"`
		}
		if json.Unmarshal(e, &d) != nil {
			continue
		}
		if d.Name != "" {
			m.names[d.Name] = true
		}
		for _, t := range anyTagArg.FindAllStringSubmatch(d.Tags, -1) {
			m.tags = append(m.tags, strings.TrimPrefix(t[1], "minecraft:"))
		}
	}
	return m
}

func (m blockMatcher) empty() bool { return len(m.names) == 0 && len(m.tags) == 0 }

func (m blockMatcher) match(o *Overworld, id uint32) bool {
	name := o.blockName(id)
	if m.names[name] {
		return true
	}
	if len(m.tags) == 0 {
		return false
	}
	for _, t := range o.blockTags[name] {
		for _, want := range m.tags {
			if t == want {
				return true
			}
		}
	}
	return false
}

func (m blockMatcher) union(n blockMatcher) blockMatcher {
	out := blockMatcher{names: map[string]bool{}}
	for k := range m.names {
		out.names[k] = true
	}
	for k := range n.names {
		out.names[k] = true
	}
	out.tags = append(append(out.tags, m.tags...), n.tags...)
	return out
}

// singleBlockFeature places one block if its attachment rules allow it.
type singleBlockFeature struct {
	block uint32
	// soil is set when the block must stand on soil: a plant with
	// enforce_placement_rules or enforce_survivability_rules.
	soil       bool
	sand       bool
	mayReplace blockMatcher
	minAttach  int
	// hasAttach is set when the feature has may_attach_to.
	hasAttach bool
	// sides: 0 top, 1 bottom, 2 north (-z), 3 south (+z), 4 west (-x), 5 east (+x)
	attach  [6]blockMatcher
	hasSide [6]bool
	// rotated holds the block turned to each direction 0-3 when it auto
	// rotates toward the side it attaches to (cocoa).
	rotated []uint32
}

// rotationSides maps the direction state 0-3 to the attach side that holds
// the supporting block, read from BDS cocoa (log at z+ is direction 0, x- 1,
// z- 2, x+ 3).
var rotationSides = [4]int{3, 4, 2, 5}

func (o *Overworld) parseSingleBlock(raw json.RawMessage) (placer, error) {
	var r struct {
		Places     json.RawMessage   `json:"places_block"`
		Placement  bool              `json:"enforce_placement_rules"`
		Survive    bool              `json:"enforce_survivability_rules"`
		MayReplace []json.RawMessage `json:"may_replace"`
		Attach     *struct {
			Min    int             `json:"min_sides_must_attach"`
			Rotate *bool           `json:"auto_rotate"`
			Sides  json.RawMessage `json:"sides"`
			Top    json.RawMessage `json:"top"`
			Bottom json.RawMessage `json:"bottom"`
			North  json.RawMessage `json:"north"`
			South  json.RawMessage `json:"south"`
			West   json.RawMessage `json:"west"`
			East   json.RawMessage `json:"east"`
		} `json:"may_attach_to"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	var b biome.Block
	if err := json.Unmarshal(r.Places, &b); err != nil {
		// Weighted block lists draw randomness the runtime does not model
		// yet.
		return nil, nil //nolint:nilerr // skipped, not an error
	}
	id, ok := o.lookupBlock(b)
	if !ok {
		return nil, nil
	}
	f := &singleBlockFeature{block: id, mayReplace: parseMatcher(r.MayReplace)}
	if r.Placement || r.Survive {
		// Use the resolved name: data files may use old names (tallgrass).
		switch name := o.blockName(id); {
		case name == "minecraft:deadbush":
			f.sand = true
		case soilPlants[name]:
			f.soil = true
		}
	}
	if r.Attach != nil {
		f.hasAttach = true
		f.minAttach = r.Attach.Min
		list := func(raw json.RawMessage) (blockMatcher, bool) {
			if len(raw) == 0 {
				return blockMatcher{}, false
			}
			var arr []json.RawMessage
			if json.Unmarshal(raw, &arr) != nil {
				arr = []json.RawMessage{raw}
			}
			return parseMatcher(arr), true
		}
		for i, raw := range []json.RawMessage{r.Attach.Top, r.Attach.Bottom, r.Attach.North, r.Attach.South, r.Attach.West, r.Attach.East} {
			f.attach[i], f.hasSide[i] = list(raw)
		}
		if m, ok := list(r.Attach.Sides); ok {
			for i := 2; i < 6; i++ {
				if !f.hasSide[i] {
					f.attach[i], f.hasSide[i] = m, true
				}
			}
		}
		if r.Attach.Rotate == nil || *r.Attach.Rotate {
			if _, ok := b.States["direction"]; ok || o.hasState(b.Name, "direction") {
				for d := range 4 {
					st := map[string]any{}
					for k, v := range b.States {
						st[k] = v
					}
					st["direction"] = float64(d)
					id, ok := o.reg.Lookup(b.Name, st)
					if !ok {
						f.rotated = nil
						break
					}
					f.rotated = append(f.rotated, id)
				}
			}
		}
	}
	return f, nil
}

// soilPlants are the plants whose placement rule wants a dirt-tagged block
// below (grass, dirt, podzol, moss...).
// VERIFY: taken from the blocks' vanilla behaviour, not read from BDS.
var soilPlants = map[string]bool{}

func init() {
	for _, n := range strings.Fields(`large_fern tall_grass short_grass fern sunflower lilac rose_bush peony poppy dandelion
blue_orchid allium azure_bluet red_tulip orange_tulip white_tulip pink_tulip oxeye_daisy cornflower lily_of_the_valley
torchflower sweet_berry_bush bush firefly_bush wildflowers pink_petals leaf_litter open_eyeblossom closed_eyeblossom
cactus_flower short_dry_grass tall_dry_grass`) {
		soilPlants["minecraft:"+n] = true
	}
}

var sideOffsets = [6][3]int32{{0, 1, 0}, {0, -1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}

func (f *singleBlockFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	// BDS picks the block from its places_block list with one draw, even
	// when the list has a single entry (sub_D30BC80: nextInt of the summed
	// weights, before the attach and replace checks).
	r.Uint32()
	cur, ok := w.block(x, y, z)
	if !ok {
		return blockPos{}, false
	}
	if f.mayReplace.empty() {
		if cur != w.o.ids.air {
			return blockPos{}, false
		}
	} else if !f.mayReplace.match(w.o, cur) {
		return blockPos{}, false
	}
	if f.soil || f.sand {
		below, ok := w.block(x, y-1, z)
		if !ok {
			return blockPos{}, false
		}
		if f.soil && !w.o.hasBlockTag(below, "dirt") {
			return blockPos{}, false
		}
		if f.sand && !w.o.hasBlockTag(below, "sand") && !strings.HasSuffix(w.o.blockName(below), "terracotta") && !w.o.hasBlockTag(below, "dirt") {
			return blockPos{}, false
		}
	}
	if f.hasAttach {
		// BDS (sub_D30BC80): a listed top or bottom side must match; only
		// the four horizontal sides count toward min_sides_must_attach,
		// and a horizontal side with no list counts as attached
		// (sub_D30D330). A bee nest so needs leaves right above it.
		n := 0
		for i, d := range sideOffsets {
			if !f.hasSide[i] {
				if i >= 2 {
					n++
				}
				continue
			}
			b, ok := w.block(x+d[0], y+d[1], z+d[2])
			matched := ok && f.attach[i].match(w.o, b)
			if i < 2 && !matched {
				return blockPos{}, false
			}
			if i >= 2 && matched {
				n++
			}
		}
		if n < f.minAttach {
			return blockPos{}, false
		}
	}
	block := f.block
	for d, side := range rotationSides {
		if len(f.rotated) != 4 || !f.hasSide[side] {
			continue
		}
		o := sideOffsets[side]
		if b, ok := w.block(x+o[0], y+o[1], z+o[2]); ok && f.attach[side].match(w.o, b) {
			block = f.rotated[d]
			break
		}
	}
	w.set(x, y, z, block)
	return blockPos{x, y, z}, true
}

// legacyTree is one of BDS's hard-coded legacy:*_tree_feature: a tree count
// per chunk and a selector over a list of data-driven trees
// (LegacyTreeFeature, sub_51D7B00).
type legacyTree struct {
	count  float32
	trees  []string
	choose func(r *rand.MT) int
}

func nextBool(r *rand.MT) bool { return r.Uint32()>>27&1 != 0 }

// legacyTrees are the legacy tree features from BDS's constructor
// (sub_51D3EA0). Forest and mesa plateau use the leaf-litter variants, as
// BDS does in new worlds (its constructor flag; BDS forests have leaf
// litter in the bdsdiff test world).
var legacyTrees = map[string]legacyTree{
	"minecraft:legacy:bamboo_jungle_tree_feature": {30, []string{"fancy_oak_tree_feature", "jungle_bush_feature", "mega_jungle_tree_feature", "scatter_tall_grass_around_tree_feature"}, func(r *rand.MT) int {
		switch {
		case r.IntN(20) == 0:
			return 0
		case r.IntN(20) <= 2:
			return 1
		case r.IntN(10) > 6:
			return 3
		}
		return 2
	}},
	"minecraft:legacy:birch_forest_tree_feature": {10, []string{"select_birch_tree_feature", "birch_tree_with_beehive_feature"}, func(*rand.MT) int { return 0 }},
	"minecraft:legacy:birch_forest_mutated_tree_feature": {10, []string{"select_birch_tree_feature", "select_super_birch_tree_feature"}, func(r *rand.MT) int {
		if r.IntN(5) == 0 {
			return 0
		}
		if nextBool(r) {
			return 1
		}
		return 0
	}},
	"minecraft:legacy:extreme_hills_plus_trees_tree_feature": {3, []string{"select_spruce_tree_feature", "fancy_oak_tree_feature", "select_oak_tree_feature"}, func(r *rand.MT) int {
		if r.IntN(3) > 0 {
			return 0
		}
		if r.IntN(10) != 0 {
			return 2
		}
		return 1
	}},
	"minecraft:legacy:flower_forest_tree_feature": {6, []string{"select_birch_tree_feature", "fancy_oak_tree_with_optional_beehive_feature", "oak_tree_with_optional_beehive_feature"}, forestChoice},
	"minecraft:legacy:meadow_trees_feature": {0.01, []string{"fancy_oak_tree_with_beehive_feature", "super_birch_tree_with_beehive_feature"}, func(r *rand.MT) int {
		if nextBool(r) {
			return 0
		}
		return 1
	}},
	"minecraft:legacy:forest_tree_feature": {10, []string{"select_birch_tree_with_leaf_litter_feature", "fancy_oak_tree_with_leaf_litter_and_optional_beehive", "select_oak_tree_with_leaf_litter_feature"}, forestChoice},
	"minecraft:legacy:ice_tree_feature":    {0.1, []string{"select_spruce_tree_feature"}, func(*rand.MT) int { return 0 }},
	"minecraft:legacy:jungle_tree_feature": {50, []string{"fancy_oak_tree_feature", "jungle_bush_feature", "mega_jungle_tree_feature", "select_jungle_tree_feature"}, func(r *rand.MT) int {
		switch {
		case r.IntN(10) == 0:
			return 0
		case r.IntN(2) == 0:
			return 1
		case r.IntN(3) == 0:
			return 2
		}
		return 3
	}},
	"minecraft:legacy:jungle_edge_tree_feature": {2, []string{"fancy_oak_tree_with_optional_beehive_feature", "jungle_bush_feature", "select_jungle_tree_feature"}, func(r *rand.MT) int {
		if r.IntN(10) == 0 {
			return 0
		}
		if r.IntN(2) != 0 {
			return 2
		}
		return 1
	}},
	"minecraft:legacy:mesa_plateau_stone_tree_feature": {5, []string{"select_oak_tree_with_leaf_litter_feature"}, func(*rand.MT) int { return 0 }},
	"minecraft:legacy:plains_tree_feature": {0.05, []string{"fancy_oak_tree_with_optional_beehive_feature", "select_oak_tree_feature", "oak_tree_with_beehive_feature"}, func(r *rand.MT) int {
		if r.IntN(3) == 0 {
			return 0
		}
		return 1
	}},
	"minecraft:legacy:savanna_tree_feature":         {1, []string{"savanna_tree_feature", "select_oak_tree_feature"}, savannaChoice},
	"minecraft:legacy:savanna_mutated_tree_feature": {2, []string{"savanna_tree_feature", "select_oak_tree_feature"}, savannaChoice},
	"minecraft:legacy:taiga_tree_feature": {10, []string{"pine_tree_feature", "select_spruce_tree_feature"}, func(r *rand.MT) int {
		if r.IntN(3) == 0 {
			return 0
		}
		return 1
	}},
	"minecraft:legacy:mega_taiga_tree_feature": {10, []string{"mega_spruce_tree_feature", "mega_pine_tree_feature", "pine_tree_feature", "select_spruce_tree_feature"}, func(r *rand.MT) int {
		if r.IntN(3) != 0 {
			if r.IntN(3) != 0 {
				return 3
			}
			return 2
		}
		if r.IntN(13) != 0 {
			return 1
		}
		return 0
	}},
	"minecraft:legacy:redwood_taiga_mutated_tree_feature": {10, []string{"mega_spruce_tree_feature", "pine_tree_feature", "select_spruce_tree_feature"}, func(r *rand.MT) int {
		if r.IntN(3) != 0 {
			if r.IntN(3) != 0 {
				return 2
			}
			return 1
		}
		return 0
	}},
}

func forestChoice(r *rand.MT) int {
	if r.IntN(5) == 0 {
		return 0
	}
	if r.IntN(10) != 0 {
		return 2
	}
	return 1
}

func savannaChoice(r *rand.MT) int {
	if r.IntN(5) > 0 {
		return 0
	}
	return 1
}

type legacyTreeFeature struct {
	def legacyTree
	set *featureSet
}

// place draws the tree count, then per tree: the tree, z, x, and places it
// on the height map.
func (f *legacyTreeFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	c := f.def.count
	var n int32
	if r.IntN(10) != 0 {
		if c >= 1 {
			n = int32(c) + max(r.IntN(10), 7) - 7
		} else if c > r.Float() {
			n = 1
		}
	} else {
		n = int32(c * r.Float())
	}
	return blockPos{x, y, z}, placeLegacyTrees(w, x, z, n, r, f.set, func(r *rand.MT) string { return f.def.trees[f.def.choose(r)] })
}

// placeLegacyTrees is BDS's per-tree loop (sub_51D3D30): n times, choose
// the tree, draw z then x, place it on the height map, then place
// scatter_tall_grass_around_tree_feature at the same spot (skipped by BDS
// only for the old "legacy" world generator).
func placeLegacyTrees(w *featureWorld, x, z, n int32, r *rand.MT, set *featureSet, choose func(*rand.MT) string) bool {
	grass := featureRef{"minecraft:scatter_tall_grass_around_tree_feature", set}
	placed := false
	for ; n > 0; n-- {
		tree := featureRef{"minecraft:" + choose(r), set}
		tz := z + r.IntN(14) + 1
		tx := x + r.IntN(14) + 1
		ty := w.heightmap(tx, tz)
		if _, ok := tree.place(w, tx, ty, tz, r); ok {
			placed = true
		}
		grass.place(w, tx, ty, tz, r)
	}
	return placed
}

// swampFoliageFeature is minecraft:legacy:swamp_foliage_feature
// (LegacySwampFoliageFeature, place sub_51D39D0, constructor sub_51D3480):
// ten seagrass tries 54 blocks above the origin (they find no water but
// draw), then two to four swamp trees, one in twenty a huge mushroom.
type swampFoliageFeature struct {
	set *featureSet
}

func (f *swampFoliageFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	var n int32
	if r.IntN(10) != 0 {
		n = max(r.IntN(10), 7) - 5
	} else {
		n = int32(r.Float() * 2)
	}
	seagrass := featureRef{"minecraft:seagrass_feature", f.set}
	for range 10 {
		sx := x + r.IntN(16)
		sz := z + r.IntN(16)
		seagrass.place(w, sx, y+54, sz, r)
	}
	placeLegacyTrees(w, x, z, n, r, f.set, func(r *rand.MT) string {
		if r.IntN(20) == 0 {
			return "huge_mushroom_feature"
		}
		return "swamp_tree_feature"
	})
	return blockPos{x, y, z}, true
}
