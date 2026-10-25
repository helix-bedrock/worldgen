package structure

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/helix-bedrock/worldgen/1.26.52/internal/mathx"

	worldgen "github.com/helix-bedrock/worldgen/1.26.52"
	"github.com/helix-bedrock/worldgen/1.26.52/rand"
)

// ApplyRules runs a template's block rules on one block about to be placed
// at (x, y, z) over world: the first rule whose block matches, whose chance
// passes and whose spot is not protected decides the block. The random is
// fresh per block and drawn only for matching rules; a position test draws
// once more, measuring from pivot (the piece's position, VERIFY).
func (l *Lib) ApplyRules(rules []BlockRule, cs ConvertedState, x, y, z int32, world uint32, pivot [3]int32) ConvertedState {
	st, ok := l.reg.State(cs.ID)
	if !ok {
		return cs
	}
	var r rand.Java
	seeded := false
	for _, ru := range rules {
		if ru.Set == nil && ru.Block != st.Name || ru.Set != nil && !ru.Set[st.Name] {
			continue
		}
		pass := ru.Chance >= 1
		if !pass && (ru.Set != nil || ru.Chance > 0) {
			if !seeded {
				r.Seed(rand.RuleSeed(x, y, z))
				seeded = true
			}
			f := r.Float()
			// A single block passes on chance > f, a set on chance >= f.
			pass = ru.Chance > f || ru.Set != nil && ru.Chance == f
		}
		if !pass {
			continue
		}
		// VERIFY: the location test's own chance is taken as 1.
		if ws, ok := l.reg.State(world); ok && (ru.NotIn[ws.Name] || ru.At != "" && ws.Name != ru.At) {
			continue
		}
		if ru.pos != nil {
			if !seeded {
				r.Seed(rand.RuleSeed(x, y, z))
				seeded = true
			}
			d := [3]int32{x - pivot[0], y - pivot[1], z - pivot[2]}[ru.pos.axis]
			if !ru.pos.passes(mathx.Abs(d), r.Float()) {
				continue
			}
		}
		return ConvertedState{ru.Output, false, true}
	}
	return cs
}

// applyLegacyRules runs built-in block rules on one block about to be
// placed at (x, y, z) over world, as BDS's template placement does
// (sub_D2B7A10): every rule is tested in turn on the block the earlier ones
// left, and r is drawn by a chance input whose block matches and by a
// position test whose input and location pass.
func (l *Lib) applyLegacyRules(rules []BlockRule, cs ConvertedState, x, y, z int32, world uint32, pivot [3]int32, r *rand.MT) ConvertedState {
	st, ok := l.reg.State(cs.ID)
	if !ok {
		return cs
	}
	name := st.Name
	ws, wok := l.reg.State(world)
	for _, ru := range rules {
		if ru.Set == nil && ru.Block != name || ru.Set != nil && !ru.Set[name] {
			continue
		}
		pass := ru.Chance >= 1
		if !pass && ru.Chance > 0 {
			f := r.Float()
			// A single block passes on chance > f, a set on chance >= f.
			pass = ru.Chance > f || ru.Set != nil && ru.Chance == f
		}
		if !pass {
			continue
		}
		if wok && (ru.NotIn[ws.Name] || ru.At != "" && ws.Name != ru.At) {
			continue
		}
		if ru.pos != nil {
			d := [3]int32{x - pivot[0], y - pivot[1], z - pivot[2]}[ru.pos.axis]
			if !ru.pos.passes(mathx.Abs(d), r.Float()) {
				continue
			}
		}
		cs = ConvertedState{ru.Output, false, true}
		name = l.names[ru.Output]
	}
	return cs
}

// CappedRule is a capped processor: up to limit blocks of the piece that
// are block after the rules turn into output, picked in a shuffled order.
type CappedRule struct {
	Limit  int
	Block  string
	Output uint32
	Loot   string
}

// Expanded is a pool's elements, each repeated weight times.
func (p *Pool) Expanded() []*Element {
	var out []*Element
	for _, e := range p.Entries {
		for range e.Weight {
			out = append(out, e.Element)
		}
	}
	return out
}

// CappedPicks are the template blocks a piece's capped processors turn,
// by block index: computed once per piece from the whole template, in an
// order shuffled with a positional Xoroshiro at the piece's position.
// VERIFY: Java's CappedProcessor; the random (BDS's
// StructurePoolBlockPredicateCappedArcheologyBlockReplacement) is taken as
// the world seed's positional Xoroshiro.
func (l *Lib) CappedPicks(p *Piece) map[int]CappedRule {
	p.cappedOnce.Do(func() {
		t := p.Template.Template
		conv := l.TemplateStates(p.Template, p.Rotation)
		names := make([]string, len(t.Blocks))
		for i, b := range t.Blocks {
			cs := conv[b.State]
			if !cs.OK {
				continue
			}
			rp := RotatePos(b.Pos, p.Rotation)
			x, y, z := rp[0]+p.Pos[0], rp[1]+p.Pos[1], rp[2]+p.Pos[2]
			if len(p.Element.Rules) > 0 {
				cs = l.ApplyRules(p.Element.Rules, cs, x, y, z, l.air, p.Pos)
			}
			names[i] = l.BlockName(cs.ID)
		}
		picks := map[int]CappedRule{}
		for _, cr := range p.Element.Capped {
			r := rand.NewPositional(rand.Seed128(l.seed)).At(p.Pos[0], p.Pos[1], p.Pos[2])
			order := make([]int, len(t.Blocks))
			for i := range order {
				order[i] = i
			}
			for i := len(order); i > 1; i-- {
				j := r.IntN(int32(i))
				order[i-1], order[j] = order[j], order[i-1]
			}
			n := 0
			for _, i := range order {
				if n >= cr.Limit {
					break
				}
				if _, done := picks[i]; done || names[i] != cr.Block {
					continue
				}
				picks[i] = cr
				n++
			}
		}
		p.capped = picks
	})
	return p.capped
}

// JigsawData is the pools and elements of the jigsaw structures.
type JigsawData struct {
	Pools    map[string]*Pool
	Elements map[string]*Element
}

// BlockRule is one rule of a template's block rules: a block (or one of a
// set) turns into output with a chance, unless the world already holds a
// block of notIn there, or, with at set, only where the world holds at.
type BlockRule struct {
	Block  string
	Set    map[string]bool
	Chance float32
	NotIn  map[string]bool
	At     string
	// pos is the rule's position test, nil when it always passes.
	pos    *axisPosTest
	Output uint32
}

// axisPosTest is an axis_aligned_linear position test: a chance growing
// from minChance to maxChance with the block's distance from the piece
// along the axis, between minDist and maxDist.
type axisPosTest struct {
	minChance, maxChance float32
	minDist, maxDist     int32
	axis                 int // 0 x, 1 y, 2 z
}

// passes is the test (sub_D234BC0) for a block d blocks from the pivot
// along the axis with draw f. BDS returns early before the interpolation
// when f is at most a chance and the distance range is at least the
// corresponding distance bound, so a test from 0 to 100 passes on f <=
// maxChance at any distance.
func (t *axisPosTest) passes(d int32, f float32) bool {
	span := t.maxDist - t.minDist
	if span == 0 {
		return false
	}
	if t.minChance >= f && float32(t.minDist) >= float32(span) {
		return true
	}
	if t.maxChance >= f && float32(span) >= float32(t.maxDist) {
		return true
	}
	c := float32(d-t.minDist)/float32(span)*(t.maxChance-t.minChance) + t.minChance
	if t.maxChance < c {
		c = t.maxChance
	} else {
		c = max(c, t.minChance)
	}
	return c >= f
}

// Pool is a template pool: weighted elements and the pool to fall back on.
type Pool struct {
	Name, Fallback string
	Entries        []PoolEntry
}

// PoolEntry is an element of a pool with its weight.
type PoolEntry struct {
	Element *Element
	Weight  int32
}

// Element is one element a pool can place.
type Element struct {
	ID, Template, Feature string
	TerrainMatching       bool
	// rules are the block rules applied as the element is placed.
	Rules []BlockRule
	// Legacy marks rules that are BDS's built-in block rules rather than
	// data-driven processors: they are tested in the template placement on
	// one random for the whole piece.
	Legacy bool
	// capped are capped processors run after the rules (data-driven
	// structures).
	Capped []CappedRule
}

// JigsawTemplate is a template prepared for placement: its blocks converted
// per rotation lazily, and its jigsaw connectors.
type JigsawTemplate struct {
	Template *Template
	Jigsaws  []JigsawBlock
	// once and conv cache the converted palette per turn, then per turn
	// with each mirror (index rot+4*mirror).
	once [mirroredTurns]sync.Once
	conv [mirroredTurns][]ConvertedState

	// at indexes the blocks by position, built once.
	atOnce sync.Once
	at     map[[3]int32]int

	// chest is the spot WithHouseChests puts a chest in, computed once.
	chestOnce sync.Once
	chest     ChestSpot
}

// ChestSpot is where an extra chest goes in a house template, in template
// space; ok is false when the house has a chest or no room for one.
type ChestSpot struct {
	Pos    [3]int32
	Facing string
	OK     bool
}

// HouseChestSpot finds a floor spot for a chest inside a house template:
// air with air above, a solid floor below, walls somewhere in all four
// directions and a roof, not next to a door. When the house has a bed the
// spot must be beside the bed. The chest faces open air.
func (jt *JigsawTemplate) HouseChestSpot() ChestSpot {
	jt.chestOnce.Do(jt.findChestSpot)
	return jt.chest
}

// findChestSpot fills jt.chest; see HouseChestSpot.
func (jt *JigsawTemplate) findChestSpot() {
	t := jt.Template
	h := houseScan{names: map[[3]int32]string{}, size: t.Size}
	var beds [][3]int32
	for _, b := range t.Blocks {
		n := t.Palette[b.State].Name
		if strings.Contains(n, "chest") {
			return
		}
		h.names[b.Pos] = n
		if strings.HasSuffix(n, "_bed") {
			beds = append(beds, b.Pos)
		}
	}
	dirs := chestDirs()
	// Beside a bed first; then, or in a house without a bed, any floor
	// spot inside, those against a wall before the others.
	var besideBed, floor [][3]int32
	for _, b := range beds {
		for _, d := range dirs {
			besideBed = append(besideBed, [3]int32{b[0] + d.d[0], b[1], b[2] + d.d[1]})
		}
	}
	for y := int32(1); y < t.Size[1]-1; y++ {
		for z := int32(1); z < t.Size[2]-1; z++ {
			for x := int32(1); x < t.Size[0]-1; x++ {
				floor = append(floor, [3]int32{x, y, z})
			}
		}
	}
	passes := [...]chestPass{{besideBed, false, false}, {floor, true, false}, {floor, false, false}, {floor, false, true}}
	for _, pass := range passes {
		for _, p := range pass.cands {
			fits := h.ok(p)
			if pass.nearDoor {
				fits = h.okNearDoor(p)
			}
			if fits && (!pass.wall || h.againstWall(p)) {
				jt.chest = ChestSpot{Pos: p, Facing: h.face(p), OK: true}
				return
			}
		}
	}
}

// chestDir is a horizontal step and the chest facing that looks along it.
type chestDir struct {
	d      [2]int32
	facing string
}

// chestDirs returns the four horizontal directions in the order chests try
// them.
func chestDirs() [4]chestDir {
	return [4]chestDir{{[2]int32{0, -1}, "north"}, {[2]int32{0, 1}, "south"}, {[2]int32{-1, 0}, "west"}, {[2]int32{1, 0}, "east"}}
}

// wallDirs returns the four horizontal steps used to look for walls.
func wallDirs() [4][2]int32 {
	return [4][2]int32{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}
}

// notSolidParts returns the name parts of blocks a chest may not stand on
// or against: glass, doors, beds, plants, crops and the like.
func notSolidParts() [23]string {
	return [23]string{"door", "pane", "glass", "torch", "bed", "carpet", "flower", "wheat", "carrots", "potatoes",
		"beetroots", "short_grass", "tall_grass", "fern", "sapling", "fence", "wall", "stairs", "slab", "trapdoor", "lantern", "bell", "path"}
}

// chestPass is one round of the chest search: the candidate spots, whether
// the spot must be against a wall, and whether it may be diagonal to a door.
type chestPass struct {
	cands    [][3]int32
	wall     bool
	nearDoor bool
}

// houseScan answers block questions about a house template while looking
// for a chest spot.
type houseScan struct {
	names map[[3]int32]string
	size  [3]int32
}

// name is the block at p, air outside the template's blocks.
func (h *houseScan) name(p [3]int32) string {
	if n, ok := h.names[p]; ok {
		return n
	}
	return "minecraft:air"
}

// air reports whether p is air, cave air or structure void.
func (h *houseScan) air(p [3]int32) bool {
	n := h.name(p)
	return n == "minecraft:air" || n == "minecraft:structure_void" || n == "minecraft:cave_air"
}

// solid is a full block a chest may stand on or against: no glass,
// doors, beds, plants, crops or structure markers.
func (h *houseScan) solid(p [3]int32) bool {
	n := h.name(p)
	if h.air(p) || n == "minecraft:jigsaw" || n == "minecraft:structure_block" || n == "minecraft:farmland" {
		return false
	}
	for _, s := range notSolidParts() {
		if strings.Contains(n, s) {
			return false
		}
	}
	return true
}

// inside reports whether p has a wall in all four directions and a roof.
func (h *houseScan) inside(p [3]int32) bool {
	for _, d := range wallDirs() {
		q, hit := p, false
		for {
			q = [3]int32{q[0] + d[0], q[1], q[2] + d[1]}
			if q[0] < 0 || q[2] < 0 || q[0] >= h.size[0] || q[2] >= h.size[2] {
				break
			}
			if h.solid(q) || strings.Contains(h.name(q), "glass") || strings.Contains(h.name(q), "door") {
				hit = true
				break
			}
			if !h.air(q) {
				// Crops, fences and the like are not walls.
				continue
			}
		}
		if !hit {
			return false
		}
	}
	for y := p[1] + 1; y < h.size[1]; y++ {
		if !h.air([3]int32{p[0], y, p[2]}) {
			return true
		}
	}
	return false
}

// doorNear reports whether a door is in the 3x3 ring around p.
func (h *houseScan) doorNear(p [3]int32) bool {
	for dx := int32(-1); dx <= 1; dx++ {
		for dz := int32(-1); dz <= 1; dz++ {
			if strings.Contains(h.name([3]int32{p[0] + dx, p[1], p[2] + dz}), "door") {
				return true
			}
		}
	}
	return false
}

// ok reports whether a chest fits at p with no door around it.
func (h *houseScan) ok(p [3]int32) bool {
	return h.air(p) && h.air([3]int32{p[0], p[1] + 1, p[2]}) && h.solid([3]int32{p[0], p[1] - 1, p[2]}) &&
		!h.doorNear(p) && h.inside(p)
}

// face picks the side with open air, preferring the one away from
// a wall or the bed.
func (h *houseScan) face(p [3]int32) string {
	dirs := chestDirs()
	for _, d := range dirs {
		back := [3]int32{p[0] - d.d[0], p[1], p[2] - d.d[1]}
		if h.air([3]int32{p[0] + d.d[0], p[1], p[2] + d.d[1]}) && !h.air(back) {
			return d.facing
		}
	}
	for _, d := range dirs {
		if h.air([3]int32{p[0] + d.d[0], p[1], p[2] + d.d[1]}) {
			return d.facing
		}
	}
	return "north"
}

// againstWall reports whether p has a solid block beside it.
func (h *houseScan) againstWall(p [3]int32) bool {
	for _, d := range chestDirs() {
		if h.solid([3]int32{p[0] + d.d[0], p[1], p[2] + d.d[1]}) {
			return true
		}
	}
	return false
}

// beside reports whether p is right beside a door. Small houses may only
// have room diagonal to a door; a spot right beside a door is never used,
// so doorways stay clear.
func (h *houseScan) beside(p [3]int32) bool {
	for _, d := range chestDirs() {
		if strings.Contains(h.name([3]int32{p[0] + d.d[0], p[1], p[2] + d.d[1]}), "door") {
			return true
		}
	}
	return false
}

// okNearDoor is ok, but allows a door diagonal to p.
func (h *houseScan) okNearDoor(p [3]int32) bool {
	return h.air(p) && h.air([3]int32{p[0], p[1] + 1, p[2]}) && h.solid([3]int32{p[0], p[1] - 1, p[2]}) &&
		!h.beside(p) && h.inside(p)
}

// ConvertedState is a template state converted to a Bedrock block id.
type ConvertedState struct {
	ID    uint32
	Water bool
	OK    bool
}

// JigsawBlock is a jigsaw block of a template, with its joint settings.
type JigsawBlock struct {
	Pos        [3]int32
	Facing     string
	Top        string // the front_top orientation's top, for aligned joints
	Aligned    bool   // the joint must keep its top
	SelPrio    int32  // selection_priority: tried first when higher
	PlacePrio  int32  // placement_priority: its child is expanded first when higher
	Attachment string
	// name and target are the jigsaw's own name and the name it joins.
	Name, Target string
	Pool         string
	FinalState   string
}

// jigsawDoc is a jigsaw data file in bds (helix-data).
type jigsawDoc struct {
	Pools      []jigsawDocPool
	Elements   []jigsawDocElement
	BlockRules map[string][]jigsawDocRule `json:"block_rules"`
	BlockSets  map[string][]string        `json:"block_sets"`
}

// jigsawDocPool is a template pool in a jigsaw data file.
type jigsawDocPool struct {
	Name, Fallback string
	Elements       []jigsawDocPoolEntry
}

// jigsawDocPoolEntry is one weighted element of a pool.
type jigsawDocPoolEntry struct {
	Element string
	Weight  int32
}

// jigsawDocElement is a pool element in a jigsaw data file.
type jigsawDocElement struct {
	ID         string `json:"id"`
	Template   string
	Feature    string
	Projection string
	BlockRules string `json:"block_rules"`
}

// jigsawDocRule is one block rule in a jigsaw data file.
type jigsawDocRule struct {
	Input    jigsawDocRuleInput
	Location jigsawDocRuleLocation
	Position *jigsawDocRulePosition
	Output   string
}

// jigsawDocRuleInput is the input test of a block rule.
type jigsawDocRuleInput struct {
	Type, Block, Blocks string
	Probability         float32
}

// jigsawDocRuleLocation is the location test of a block rule.
type jigsawDocRuleLocation struct{ Type, Block, Blocks string }

// jigsawDocRulePosition is the position test of a block rule.
type jigsawDocRulePosition struct {
	Type      string
	MinChance float32 `json:"min_chance"`
	MaxChance float32 `json:"max_chance"`
	MinDist   int32   `json:"min_dist"`
	MaxDist   int32   `json:"max_dist"`
	Axis      string
}

// LoadJigsawData loads a jigsaw data file from bds in helix-data (villages.json,
// ancient_city.json). Rule outputs are looked up in reg.
func LoadJigsawData(file string, reg BlockLookup) (*JigsawData, error) {
	raw, err := worldgen.Data.ReadFile("bds/" + file)
	if err != nil {
		return nil, fmt.Errorf("worldgen: read %s: %w", file, err)
	}
	var doc jigsawDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("worldgen: parse %s: %w", file, err)
	}
	sets := map[string]map[string]bool{}
	for name, blocks := range doc.BlockSets {
		m := map[string]bool{}
		for _, b := range blocks {
			m[b] = true
		}
		sets[name] = m
	}
	rules := map[string][]BlockRule{}
	for name, rs := range doc.BlockRules {
		for _, r := range rs {
			out, ok := LookupState(reg, r.Output)
			if !ok {
				return nil, fmt.Errorf("worldgen: %s: rule %s outputs unknown block %s", file, name, r.Output)
			}
			br := BlockRule{Chance: r.Input.Probability, Output: out}
			switch r.Input.Type {
			case "random_block_match":
				br.Block = r.Input.Block
			case "random_blockset_match":
				br.Set = sets[r.Input.Blocks]
			case "block_match":
				br.Block, br.Chance = r.Input.Block, 1
			default:
				return nil, fmt.Errorf("worldgen: %s: rule %s has unknown input %s", file, name, r.Input.Type)
			}
			switch r.Location.Type {
			case "not_blocks":
				br.NotIn = sets[r.Location.Blocks]
			case "block_match":
				br.At = r.Location.Block
			}
			if ps := r.Position; ps != nil {
				if ps.Type != "axis_aligned_linear" {
					return nil, fmt.Errorf("worldgen: %s: rule %s has unknown position test %s", file, name, ps.Type)
				}
				axis := map[string]int{"x": 0, "y": 1, "z": 2}
				a, ok := axis[ps.Axis]
				if !ok {
					return nil, fmt.Errorf("worldgen: %s: rule %s has unknown axis %q", file, name, ps.Axis)
				}
				br.pos = &axisPosTest{ps.MinChance, ps.MaxChance, ps.MinDist, ps.MaxDist, a}
			}
			rules[name] = append(rules[name], br)
		}
	}
	d := &JigsawData{Pools: map[string]*Pool{}, Elements: map[string]*Element{}}
	for _, e := range doc.Elements {
		d.Elements[e.ID] = &Element{ID: e.ID, Template: e.Template, Feature: e.Feature,
			TerrainMatching: e.Projection == "terrain_matching", Rules: rules[e.BlockRules], Legacy: true}
	}
	for _, p := range doc.Pools {
		jp := &Pool{Name: p.Name, Fallback: p.Fallback}
		for _, e := range p.Elements {
			if el, ok := d.Elements[e.Element]; ok {
				jp.Entries = append(jp.Entries, PoolEntry{el, e.Weight})
			}
		}
		// The abandoned pools share names with nothing else; a name
		// registered twice keeps its first definition.
		if _, dup := d.Pools[p.Name]; !dup {
			d.Pools[p.Name] = jp
		}
	}
	return d, nil
}

// NBTInt reads an integer NBT value of any width, 0 if it is not one.
func NBTInt(v any) int32 {
	switch n := v.(type) {
	case int32:
		return n
	case int16:
		return int32(n)
	case int8:
		return int32(n)
	case uint8:
		return int32(n)
	case int64:
		return int32(n)
	}
	return 0
}

// BlockLookup finds a block's default state.
type BlockLookup interface {
	Default(name string) (uint32, bool)
	Lookup(name string, props map[string]any) (uint32, bool)
}

// LookupState finds a block written as name or name[key=value,...]: true
// and false are booleans, numbers numbers, anything else a string.
func LookupState(reg BlockLookup, s string) (uint32, bool) {
	name, rest, ok := strings.Cut(s, "[")
	if !ok {
		return reg.Default(s)
	}
	props := map[string]any{}
	for _, kv := range strings.Split(strings.TrimSuffix(rest, "]"), ",") {
		k, v, _ := strings.Cut(kv, "=")
		switch v {
		case "true":
			props[k] = true
		case "false":
			props[k] = false
		default:
			if n, err := strconv.ParseFloat(v, 64); err == nil {
				props[k] = n
			} else {
				props[k] = v
			}
		}
	}
	return reg.Lookup(name, props)
}

// Template returns the prepared template of an element, nil if it has none
// or the file does not exist (BDS references two missing village files).
func (l *Lib) Template(path string) *JigsawTemplate {
	if path == "" {
		return nil
	}
	if v, ok := l.templates.Load(path); ok {
		jt, _ := v.(*JigsawTemplate)
		return jt
	}
	var jt *JigsawTemplate
	tpl, err := l.loadAnyTemplate(l.resolve(path))
	if err == nil {
		jt = &JigsawTemplate{Template: tpl}
		for _, b := range tpl.Blocks {
			st := tpl.Palette[b.State]
			if st.Name != "minecraft:jigsaw" {
				continue
			}
			jb := JigsawBlock{Pos: b.Pos, Facing: st.Props["facing"]}
			// Newer templates give the jigsaw's front and top as
			// orientation (front_top).
			if or := st.Props["orientation"]; jb.Facing == "" && or != "" {
				jb.Facing, jb.Top, _ = strings.Cut(or, "_")
			}
			// Joints are rollable unless named aligned; vertical ones
			// default to aligned.
			switch b.NBT["joint"] {
			case "aligned":
				jb.Aligned = true
			case "rollable":
			default:
				jb.Aligned = jb.Facing == "up" || jb.Facing == "down"
			}
			jb.SelPrio = NBTInt(b.NBT["selection_priority"])
			jb.PlacePrio = NBTInt(b.NBT["placement_priority"])
			jb.Name, _ = b.NBT["name"].(string)
			jb.Target, _ = b.NBT["target"].(string)
			jb.Attachment, _ = b.NBT["attachement_type"].(string)
			if n, ok := b.NBT["name"].(string); ok && jb.Attachment == "" {
				jb.Attachment = n
			}
			jb.Pool, _ = b.NBT["target_pool"].(string)
			if p, ok := b.NBT["pool"].(string); ok && jb.Pool == "" {
				jb.Pool = p
			}
			jb.FinalState, _ = b.NBT["final_state"].(string)
			jt.Jigsaws = append(jt.Jigsaws, jb)
		}
	}
	v, _ := l.templates.LoadOrStore(path, jt)
	jt, _ = v.(*JigsawTemplate)
	return jt
}

// LoadAnyTemplate reads a template from its .nbt file, or else from its
// .mcstructure file.
func LoadAnyTemplate(path string) (*Template, error) { return (&Lib{}).loadAnyTemplate(path) }

// loadAnyTemplate is LoadAnyTemplate reading from the Lib's packs first.
func (l *Lib) loadAnyTemplate(path string) (*Template, error) {
	raw, err := l.readStructure(path, ".nbt")
	if err != nil {
		return l.loadMCTemplate(path)
	}
	return LoadTemplate(bytes.NewReader(raw))
}

// Piece is one placed element.
type Piece struct {
	Element  *Element
	Template *JigsawTemplate
	Pos      [3]int32
	Rotation int
	BB       Box
	Depth    int
	// ground is the piece's ground level above its bottom, for terrain
	// adjustments.
	Ground int32

	cappedOnce sync.Once
	capped     map[int]CappedRule
}

// PlacedJigsaws returns a piece's jigsaw blocks in world space.
func (p *Piece) PlacedJigsaws() []JigsawBlock {
	out := make([]JigsawBlock, len(p.Template.Jigsaws))
	for i, j := range p.Template.Jigsaws {
		rp := RotatePos(j.Pos, p.Rotation)
		j.Pos = [3]int32{rp[0] + p.Pos[0], rp[1] + p.Pos[1], rp[2] + p.Pos[2]}
		j.Facing = RotateFacing(j.Facing, p.Rotation)
		j.Top = RotateFacing(j.Top, p.Rotation)
		out[i] = j
	}
	return out
}

// Shuffle shuffles s in place the way BDS does, drawing from r.
func Shuffle[T any](s []T, r *rand.MT) {
	for i := 1; i < len(s); i++ {
		j := int(r.Uint32() % uint32(i+1))
		if j != i {
			s[i], s[j] = s[j], s[i]
		}
	}
}

// WeightedShuffle is a pool's elements, each repeated weight times, shuffled.
func (p *Pool) WeightedShuffle(r *rand.MT) []*Element {
	var out []*Element
	for _, e := range p.Entries {
		for range e.Weight {
			out = append(out, e.Element)
		}
	}
	Shuffle(out, r)
	return out
}

// World is what a piece is placed into: an overworld chunk, or a
// Nether chunk for the structures the two dimensions share.
type World interface {
	// bounds is the world's lowest y and the y above its highest.
	Bounds() (minY, maxY int32)
	Block(x, y, z int32) uint32
	// set places a block, holding water or not, and reports whether it
	// did (the bottom bedrock is never replaced).
	Set(x, y, z int32, id uint32, water bool) bool
	Entity(be BlockEntity)
}

// PlacePiece writes the blocks of a piece that fall in chunkBox into w.
func (l *Lib) PlacePiece(p *Piece, w World, chunkBox Box, height func(x, z int32) int32) {
	t := p.Template.Template
	rot := p.Rotation
	conv := l.TemplateStates(p.Template, rot)
	var picks map[int]CappedRule
	if len(p.Element.Capped) > 0 {
		picks = l.CappedPicks(p)
	}
	// Built-in block rules draw from a random of their own, made afresh for
	// every placement from the seed of the random the piece is placed with:
	// the population seed of the chunk being decorated (sub_D237F90,
	// sub_D2B7A10).
	var rr *rand.MT
	ruleSeed := l.populationSeed(chunkBox.X0>>4, chunkBox.Z0>>4)
	if p.Element.Legacy && len(p.Element.Rules) > 0 {
		rr = rand.NewMT(ruleSeed)
	}
	minY, maxY := w.Bounds()
	for bi, b := range t.Blocks {
		rp := RotatePos(b.Pos, rot)
		x, z := rp[0]+p.Pos[0], rp[2]+p.Pos[2]
		y := rp[1] + p.Pos[1]
		if p.Element.TerrainMatching {
			y = height(x, z) - 1 + b.Pos[1]
		}
		if !chunkBox.Contains([3]int32{x, y, z}) || y < minY || y >= maxY {
			continue
		}
		st := t.Palette[b.State]
		cs := conv[b.State]
		switch st.Name {
		case "minecraft:jigsaw":
			fs, _ := b.NBT["final_state"].(string)
			if t.Bedrock {
				// A name that is no Bedrock block places air, as the
				// bastion's mob jigsaws' "minecraft:blackstone_bricks" does.
				id, ok := LookupState(l.reg, fs)
				if !ok {
					id = l.air
				}
				cs = ConvertedState{id, false, true}
			} else {
				cs = l.ParseFinalState(fs, rot)
			}
		case "minecraft:structure_block", "minecraft:structure_void":
			// Data markers are handled by the element, not placed.
			if st.Name == "minecraft:structure_void" {
				continue
			}
			cs = ConvertedState{l.air, false, true}
		}
		if !cs.OK {
			continue
		}
		world := w.Block(x, y, z)
		switch {
		case len(p.Element.Rules) == 0:
		case p.Element.Legacy && st.Name == "minecraft:jigsaw":
			// A jigsaw's final state is placed apart from the template's
			// blocks, its rules tested on a random of its own made from the
			// same seed (sub_D23A770, sub_D2B9B40).
			cs = l.applyLegacyRules(p.Element.Rules, cs, x, y, z, world, p.Pos, rand.NewMT(ruleSeed))
		case p.Element.Legacy:
			cs = l.applyLegacyRules(p.Element.Rules, cs, x, y, z, world, p.Pos, rr)
		default:
			cs = l.ApplyRules(p.Element.Rules, cs, x, y, z, world, p.Pos)
		}
		loot := ""
		if cr, ok := picks[bi]; ok {
			cs, loot = ConvertedState{cr.Output, false, true}, cr.Loot
		}
		// A block that can hold water keeps the water it replaces, as in
		// BDS's template placement (sub_D2B7A10).
		wet := cs.Water || l.IsWater(world) && l.HoldsWater(cs.ID)
		if !w.Set(x, y, z, cs.ID, wet) {
			continue
		}
		if d := l.BlockEntityFor(cs.ID, st.Name, x, y, z); d != nil {
			be := BlockEntity{X: x, Y: y, Z: z, Data: d}
			if loot != "" {
				be.LootTable = loot
				d["LootTable"] = loot
				d["LootTableSeed"] = int32(PositionSeed(l.seed, x, y, z))
			} else if lt, ok := b.NBT["LootTable"].(string); ok && lt != "" {
				be.LootTable = "loot_tables/" + strings.TrimPrefix(lt, "minecraft:") + ".json"
				d["LootTable"] = be.LootTable
				d["LootTableSeed"] = int32(PositionSeed(l.seed, x, y, z))
			}
			w.Entity(be)
		}
	}
}

// TemplateStates returns a template's palette turned rot quarter turns and
// converted to Bedrock states, converting it once.
func (l *Lib) TemplateStates(jt *JigsawTemplate, rot int) []ConvertedState {
	return l.TemplateStatesMirror(jt, rot, MirrorNone)
}

// TemplateStatesMirror is templateStates with the palette first mirrored
// (mirrorZ or mirrorX).
func (l *Lib) TemplateStatesMirror(jt *JigsawTemplate, rot int, mirror uint8) []ConvertedState {
	k := rot&3 + quarterTurns*int(min(mirror, MirrorX))
	jt.once[k].Do(func() {
		conv := make([]ConvertedState, len(jt.Template.Palette))
		for i, st := range jt.Template.Palette {
			switch mirror {
			case MirrorZ:
				st.Props = mirrorPropsZ(st.Props)
			case MirrorX:
				st.Props = mirrorPropsX(st.Props)
			}
			st.Props = RotateProps(st.Props, rot)
			id, w, ok := l.ConvertJavaState(st)
			conv[i] = ConvertedState{id, w, ok}
		}
		jt.conv[k] = conv
	})
	return jt.conv[k]
}

// ParseFinalState converts a jigsaw block's final_state ("minecraft:x[a=b]").
func (l *Lib) ParseFinalState(s string, rot int) ConvertedState {
	if s == "" {
		return ConvertedState{l.air, false, true}
	}
	st := TemplateState{Name: s}
	if i := strings.IndexByte(s, '['); i >= 0 && strings.HasSuffix(s, "]") {
		st.Name = s[:i]
		st.Props = map[string]string{}
		for _, kv := range strings.Split(s[i+1:len(s)-1], ",") {
			if k, v, ok := strings.Cut(kv, "="); ok {
				st.Props[k] = v
			}
		}
	}
	st.Props = RotateProps(st.Props, rot)
	id, w, ok := l.ConvertJavaState(st)
	return ConvertedState{id, w, ok}
}

// RotateProps rotates the direction properties of a Java state.
func RotateProps(p map[string]string, rot int) map[string]string {
	if rot == 0 || len(p) == 0 {
		return p
	}
	out := make(map[string]string, len(p))
	for k, v := range p {
		out[k] = v
	}
	if f, ok := p["facing"]; ok {
		out["facing"] = RotateFacing(f, rot)
		// Older floor and ceiling levers name their axis.
		switch f {
		case "up_x", "up_z", "down_x", "down_z":
			if rot&1 == 1 {
				out["facing"] = map[string]string{"up_x": "up_z", "up_z": "up_x", "down_x": "down_z", "down_z": "down_x"}[f]
			}
		}
	}
	if a, ok := p["axis"]; ok && rot&1 == 1 {
		switch a {
		case "x":
			out["axis"] = "z"
		case "z":
			out["axis"] = "x"
		}
	}
	if r, ok := p["rotation"]; ok {
		var n int
		if _, err := fmt.Sscan(r, &n); err == nil {
			out["rotation"] = fmt.Sprint((n + rot*stepsPerQuarter) & rotationMask)
		}
	}
	// Fence and pane connections.
	if rot != 0 {
		sides := [4]string{p["north"], p["east"], p["south"], p["west"]}
		if sides != [4]string{} {
			for i, h := range Horizontal() {
				if v := sides[(i-rot+quarterTurns)&3]; v != "" {
					out[h] = v
				}
			}
		}
	}
	return out
}

// PositionSeed mixes the world seed and a block position into a seed for a
// container's loot (the game rolls the table with it when it is opened).
func PositionSeed(seed int64, x, y, z int32) uint32 {
	h := uint64(seed) ^ uint64(uint32(x))*seedMulX ^ uint64(uint32(y))*seedMulY ^ uint64(uint32(z))*seedMulZ
	h ^= h >> seedShiftA
	h *= seedMix
	h ^= h >> seedShiftB
	return uint32(h)
}

// The PositionSeed hash: odd 64-bit multipliers per axis (the golden ratio
// and xxHash64's primes), then a SplitMix64-style finish, so neighbouring
// containers get unrelated seeds.
const (
	seedMulX   = 0x9E3779B97F4A7C15
	seedMulY   = 0xC2B2AE3D27D4EB4F
	seedMulZ   = 0x165667B19E3779F9
	seedMix    = 0xBF58476D1CE4E5B9
	seedShiftA = 31
	seedShiftB = 29
)

// LoadLiquidHolders reads the blocks that can hold a liquid source
// (canContainLiquidSource, exported from BDS 1.26.52 block states).
func LoadLiquidHolders() (map[string]bool, error) {
	raw, err := worldgen.Data.ReadFile("bds/liquid_holders.json")
	if err != nil {
		return nil, fmt.Errorf("worldgen: read liquid holders: %w", err)
	}
	var names []string
	if err := json.Unmarshal(raw, &names); err != nil {
		return nil, fmt.Errorf("worldgen: parse liquid holders: %w", err)
	}
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m, nil
}

// StateAt returns the palette index of the template's block at a template
// position, indexing the blocks once.
func (jt *JigsawTemplate) StateAt(pos [3]int32) (int, bool) {
	jt.atOnce.Do(func() {
		jt.at = make(map[[3]int32]int, len(jt.Template.Blocks))
		for _, tb := range jt.Template.Blocks {
			jt.at[tb.Pos] = int(tb.State)
		}
	})
	st, ok := jt.at[pos]
	return st, ok
}
