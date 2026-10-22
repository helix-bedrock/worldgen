package overworld

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"math"
	"path"
	"slices"
	"strings"
	"sync"

	"github.com/helix-bedrock/worldgen"

	"github.com/helix-bedrock/worldgen/biome"
	"github.com/helix-bedrock/worldgen/rand"
	"github.com/helix-bedrock/worldgen/structure"
)

// Data-driven jigsaw structures (br::worldgen::JigsawStructure), written
// from BDS 1.26.52: trail ruins and abandoned camps from the vanilla
// worldgen JSON files, and trial chambers, which BDS defines in code (sub_D228C00) with pools built
// in sub_5092590 (data/bds/trial_chambers.json).
//
//   start      sub_D1FF960 / sub_D1FE510: the start height, then a turn
//              (nextInt(4)) and a start element, placed at the chunk
//              corner, then raised to the surface at its centre when the
//              structure projects to the height map
//   assembly   sub_D21B1E0: Java's JigsawPlacement with Bedrock's random:
//              pieces expanded by placement priority, each one's jigsaws
//              shuffled and sorted by selection priority, the pool (not at
//              the last depth) and its fallback each weight-expanded and
//              shuffled, the turns shuffled, the candidate's jigsaws
//              shuffled and sorted
//   terrain    bury (sub_D1EE730, third list): the terrain grows up to 6
//              blocks around every piece where it is not solid
//
// The shuffles here are backward Fisher-Yates (j = rand % i for i = n..2),
// unlike the village assembler's.

// djDef is one data-driven jigsaw structure.
type djDef struct {
	id                  string
	pools               map[string]*structure.Pool
	start               string
	maxDepth            int
	maxH, maxV          int32
	padBottom, padTop   int32
	minY0, maxY0        int32 // the start height range, absolute
	project             bool  // raise the start to the world surface
	bury                bool
	beard               bool     // beard_thin
	biomeTags           []string // every one must match
	biomes              []bool   // instead of biomeTags
	spacing, separation int32
	salt                int32
	aliases             []djAlias
	starts              sync.Map // [2]int32 -> *djStructure
	reach               int32    // chunks a structure can reach from its start chunk
	kind                string   // the /tpstructure name
	// variants are the structures of a structure set with more than one
	// (abandoned camps): the set's placement picks one per start.
	variants []djVariant
}

type djVariant struct {
	d      *djDef
	weight int32
}

// djAlias is a pool alias bound once per structure (Java's pool_aliases):
// one group of (alias, target) pairs is picked, or one target.
type djAlias struct {
	groups [][][2]string
}

// djStructure is an assembled structure.
type djStructure struct {
	pieces []*structure.Piece
	bb     structure.Box
	// start is the start position: the start piece's centre at the start
	// height (what BDS's locate reports).
	start [3]int32
}

// djSetJSON is a vanilla minecraft:structure_set file.
type djSetJSON struct {
	Set struct {
		Placement struct {
			Salt, Separation, Spacing int32
		}
		Structures []struct {
			Structure string
			Weight    int32
		}
	} `json:"minecraft:structure_set"`
}

// loadTrailRuins reads the trail ruins from the vanilla worldgen files.
func (o *Overworld) loadTrailRuins() (*djDef, error) {
	root := "data/vanilla/worldgen"
	var set djSetJSON
	if err := readJSON(root+"/structure_sets/trail_ruins.json", &set); err != nil {
		return nil, err
	}
	pools, err := o.loadPools(root, "trail_ruins")
	if err != nil {
		return nil, err
	}
	d, err := loadDJStructure(root+"/structures/trail_ruins.json", pools)
	if err != nil {
		return nil, err
	}
	d.kind = "trail_ruins"
	d.spacing, d.separation, d.salt = set.Set.Placement.Spacing, set.Set.Placement.Separation, set.Set.Placement.Salt
	return d, nil
}

// loadAbandonedCamps reads the abandoned camps: one structure set with a
// structure per biome group, all sharing the camp pools.
func (o *Overworld) loadAbandonedCamps() (*djDef, error) {
	root := "data/vanilla/worldgen"
	var set djSetJSON
	if err := readJSON(root+"/structure_sets/abandoned_camp.json", &set); err != nil {
		return nil, err
	}
	pools, err := o.loadPools(root, "abandoned_camp")
	if err != nil {
		return nil, err
	}
	d := &djDef{id: "minecraft:abandoned_camp", kind: "abandoned_camp", spacing: set.Set.Placement.Spacing,
		separation: set.Set.Placement.Separation, salt: set.Set.Placement.Salt}
	for _, e := range set.Set.Structures {
		name, _ := strings.CutPrefix(e.Structure, "minecraft:")
		v, err := loadDJStructure(root+"/structures/"+name+".json", pools)
		if err != nil {
			return nil, err
		}
		d.variants = append(d.variants, djVariant{v, e.Weight})
		d.reach = max(d.reach, v.reach)
		d.bury = d.bury || v.bury
		d.beard = d.beard || v.beard
	}
	return d, nil
}

// loadDJStructure reads a minecraft:jigsaw structure file.
func loadDJStructure(file string, pools map[string]*structure.Pool) (*djDef, error) {
	var st struct {
		J struct {
			Description struct{ Identifier string }
			Filters     []struct{ Value string } `json:"biome_filters"`
			Adapt       string                   `json:"terrain_adaptation"`
			Start       string                   `json:"start_pool"`
			Depth       int                      `json:"max_depth"`
			Dist        struct {
				Horizontal, Vertical int32
			} `json:"max_distance_from_center"`
			Height struct {
				Value struct {
					Absolute int32
				}
			} `json:"start_height"`
			Padding    int32  `json:"dimension_padding"`
			Projection string `json:"heightmap_projection"`
		} `json:"minecraft:jigsaw"`
	}
	if err := readJSON(file, &st); err != nil {
		return nil, err
	}
	d := &djDef{id: st.J.Description.Identifier, start: st.J.Start, maxDepth: st.J.Depth,
		maxH: st.J.Dist.Horizontal, maxV: st.J.Dist.Vertical, minY0: st.J.Height.Value.Absolute, maxY0: st.J.Height.Value.Absolute,
		padBottom: st.J.Padding, padTop: st.J.Padding,
		project: st.J.Projection != "", bury: st.J.Adapt == "bury", beard: st.J.Adapt == "beard_thin", pools: pools}
	for _, f := range st.J.Filters {
		d.biomeTags = append(d.biomeTags, f.Value)
	}
	// The whole structure lies within the free space around the start.
	d.reach = (d.maxH+48)/16 + 1
	return d, nil
}

// loadPools reads the template pools of one folder, with the processors
// they name.
func (o *Overworld) loadPools(root, dir string) (map[string]*structure.Pool, error) {
	procs, err := o.loadProcessors(root + "/processors")
	if err != nil {
		return nil, err
	}
	pools := map[string]*structure.Pool{}
	err = fs.WalkDir(worldgen.Data, root+"/template_pools/"+dir, func(p string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() || path.Ext(p) != ".json" {
			return err
		}
		var tp struct {
			P struct {
				Description struct{ Identifier string }
				Fallback    string
				Elements    []struct {
					Element struct {
						Type       string `json:"element_type"`
						Location   string
						Feature    string
						Processors string
					}
					Weight int32
				}
			} `json:"minecraft:template_pool"`
		}
		if err := readJSON(p, &tp); err != nil {
			return err
		}
		pool := &structure.Pool{Name: tp.P.Description.Identifier, Fallback: tp.P.Fallback}
		if pool.Fallback == "" {
			pool.Fallback = "minecraft:empty"
		}
		for _, el := range tp.P.Elements {
			je := &structure.Element{ID: el.Element.Location, Template: el.Element.Location}
			if pr := procs[el.Element.Processors]; pr != nil {
				je.Rules, je.Capped = pr.rules, pr.capped
			}
			switch el.Element.Type {
			case "minecraft:empty_pool_element":
				je.Template = ""
			case "minecraft:feature_pool_element":
				je.ID, je.Template, je.Feature = el.Element.Feature, "", el.Element.Feature
			}
			pool.Entries = append(pool.Entries, structure.PoolEntry{Element: je, Weight: el.Weight})
		}
		pools[pool.Name] = pool
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("worldgen: %s pools: %w", dir, err)
	}
	return pools, nil
}

func readJSON(p string, v any) error {
	raw, err := worldgen.Data.ReadFile(p)
	if err != nil {
		return fmt.Errorf("worldgen: read %s: %w", p, err)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("worldgen: parse %s: %w", p, err)
	}
	return nil
}

type processorList struct {
	rules  []structure.BlockRule
	capped []structure.CappedRule
}

// loadProcessors reads the processor lists of a folder: rule processors
// (random_block_match and block_match inputs) and capped rule processors.
func (o *Overworld) loadProcessors(dir string) (map[string]*processorList, error) {
	files, err := fs.Glob(worldgen.Data, dir+"/*.json")
	if err != nil {
		return nil, fmt.Errorf("worldgen: list processors: %w", err)
	}
	type rule struct {
		Modifier struct {
			LootTable string `json:"loot_table"`
		} `json:"block_entity_modifier"`
		Input struct {
			Type        string `json:"predicate_type"`
			Block       string
			Probability float32
		} `json:"input_predicate"`
		Output biome.Block `json:"output_state"`
	}
	type proc struct {
		Type     string `json:"processor_type"`
		Rules    []rule
		Limit    int
		Delegate *struct {
			Rules []rule
		}
	}
	out := map[string]*processorList{}
	for _, f := range files {
		var doc struct {
			L struct {
				Description struct{ Identifier string }
				Processors  []proc
			} `json:"minecraft:processor_list"`
		}
		if err := readJSON(f, &doc); err != nil {
			return nil, err
		}
		pl := &processorList{}
		for _, p := range doc.L.Processors {
			switch p.Type {
			case "minecraft:rule":
				for _, r := range p.Rules {
					id, ok := o.lookupBlock(r.Output)
					if !ok {
						continue
					}
					chance := r.Input.Probability
					if r.Input.Type == "minecraft:block_match" {
						chance = 1
					}
					pl.rules = append(pl.rules, structure.BlockRule{Block: r.Input.Block, Chance: chance, Output: id})
				}
			case "minecraft:capped":
				if p.Delegate == nil {
					continue
				}
				for _, r := range p.Delegate.Rules {
					id, ok := o.lookupBlock(r.Output)
					if !ok {
						continue
					}
					pl.capped = append(pl.capped, structure.CappedRule{Limit: p.Limit, Block: r.Input.Block, Output: id, Loot: r.Modifier.LootTable})
				}
			}
		}
		out[doc.L.Description.Identifier] = pl
	}
	return out, nil
}

// loadTrialChambers builds the trial chambers from BDS's code-defined
// settings (sub_D228C00) and the extracted pools.
func (o *Overworld) loadTrialChambers() (*djDef, error) {
	raw, err := worldgen.Data.ReadFile("data/bds/trial_chambers.json")
	if err != nil {
		return nil, fmt.Errorf("worldgen: read trial_chambers.json: %w", err)
	}
	var doc struct {
		Processors map[string][]struct {
			Block       string
			Probability float32
			Output      string
			Lit         bool
		}
		Pools []struct {
			Name, Fallback string
			Elements       []struct {
				Template   string
				Weight     int32
				Processors string
			}
		}
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("worldgen: parse trial_chambers.json: %w", err)
	}
	procs := map[string][]structure.BlockRule{}
	for name, rs := range doc.Processors {
		for _, r := range rs {
			props := map[string]any{}
			if r.Lit {
				props["lit"] = uint8(1)
			}
			procs[name] = append(procs[name], structure.BlockRule{Block: r.Block, Chance: r.Probability, Output: o.state(r.Output, props)})
		}
	}
	d := &djDef{id: "minecraft:trial_chambers", kind: "trial_chambers", start: "minecraft:trial_chambers/chamber/end",
		maxDepth: 20, maxH: 116, maxV: 116, padBottom: 10, padTop: 10, minY0: -40, maxY0: -20, bury: true,
		spacing: 34, separation: 12, salt: 94251327, pools: map[string]*structure.Pool{}}
	elements := map[string]*structure.Element{}
	element := func(tpl, procName string) *structure.Element {
		k := tpl + "|" + procName
		if e := elements[k]; e != nil {
			return e
		}
		e := &structure.Element{ID: tpl, Template: tpl, Rules: procs[procName]}
		elements[k] = e
		return e
	}
	for _, p := range doc.Pools {
		pool := &structure.Pool{Name: p.Name, Fallback: p.Fallback}
		for _, e := range p.Elements {
			pool.Entries = append(pool.Entries, structure.PoolEntry{Element: element(e.Template, e.Processors), Weight: e.Weight})
		}
		d.pools[p.Name] = pool
	}
	// The pool aliases (sub_D293FC0): the spawner contents, bound once per
	// chamber. Their targets are one-template pools of the same name.
	alias := func(groups ...[][2]string) djAlias { return djAlias{groups: groups} }
	tc := "minecraft:trial_chambers/spawner/"
	pair := func(kind string) [][2]string {
		return [][2]string{{tc + "contents/ranged", tc + "ranged/" + kind}, {tc + "contents/slow_ranged", tc + "slow_ranged/" + kind}}
	}
	one := func(from, to string) [][2]string { return [][2]string{{tc + "contents/" + from, tc + to}} }
	d.aliases = []djAlias{
		alias(pair("skeleton"), pair("stray"), pair("poison_skeleton")),
		alias(one("melee", "melee/zombie"), one("melee", "melee/husk"), one("melee", "melee/spider")),
		alias(one("small_melee", "small_melee/slime"), one("small_melee", "small_melee/cave_spider"),
			one("small_melee", "small_melee/silverfish"), one("small_melee", "small_melee/baby_zombie")),
	}
	for _, a := range d.aliases {
		for _, g := range a.groups {
			for _, pr := range g {
				name := pr[1]
				d.pools[name] = &structure.Pool{Name: name, Fallback: "minecraft:empty",
					Entries: []structure.PoolEntry{{Element: element(strings.TrimPrefix(name, "minecraft:"), ""), Weight: 1}}}
			}
		}
	}
	// The biomes BDS lists for trial chambers (qword_1C9F310): every
	// Overworld biome of its list but the deep dark.
	ids := []uint32{48, 49, 16, 27, 28, 156, 155, 192, 26, 44, 30, 31, 158, 45, 47, 43, 24, 41, 2, 17, 130, 188, 3, 20,
		131, 34, 162, 132, 4, 18, 46, 183, 11, 185, 13, 12, 140, 182, 21, 23, 151, 22, 149, 10, 42, 187, 191, 186, 32, 33,
		37, 165, 39, 167, 38, 166, 14, 15, 0, 193, 1, 161, 160, 7, 29, 157, 35, 163, 36, 164, 184, 25, 189, 129, 6, 134,
		5, 19, 133, 40}
	d.biomes = make([]bool, slices.Max(ids)+1)
	for _, id := range ids {
		d.biomes[id] = true
	}
	d.reach = (d.maxH+48)/16 + 1
	return d, nil
}

// pick is the start chunk of placement region (rx, rz): a linear random
// spread drawn with Java's random seeded as Java's setLargeFeatureWithSalt
// does (sub_D1D2570), x then z.
func (d *djDef) pick(o *Overworld, rx, rz int32) (int32, int32) {
	var r rand.Java
	r.Seed(int64(d.salt) + o.seed + 341873128712*int64(rx) + 132897987541*int64(rz))
	n := d.spacing - d.separation
	x := rx*d.spacing + r.IntN(n)
	return x, rz*d.spacing + r.IntN(n)
}

// at assembles the structure starting in chunk (cx, cz), nil if none does.
func (d *djDef) at(o *Overworld, cx, cz int32) *djStructure {
	if x, z := d.pick(o, floorDivRegion(cx, d.spacing), floorDivRegion(cz, d.spacing)); x != cx || z != cz {
		return nil
	}
	if v, ok := d.starts.Load([2]int32{cx, cz}); ok {
		s, _ := v.(*djStructure)
		return s
	}
	s := d.assemble(o, cx, cz)
	v, _ := d.starts.LoadOrStore([2]int32{cx, cz}, s)
	s, _ = v.(*djStructure)
	return s
}

// near calls f with the structures that start within reach of chunk
// (cx, cz).
func (d *djDef) near(o *Overworld, cx, cz int32, f func(s *djStructure)) {
	r0x, r1x := floorDivRegion(cx-d.reach, d.spacing), floorDivRegion(cx+d.reach, d.spacing)
	r0z, r1z := floorDivRegion(cz-d.reach, d.spacing), floorDivRegion(cz+d.reach, d.spacing)
	for rx := r0x; rx <= r1x; rx++ {
		for rz := r0z; rz <= r1z; rz++ {
			sx, sz := d.pick(o, rx, rz)
			if abs32i(sx-cx) > d.reach || abs32i(sz-cz) > d.reach {
				continue
			}
			if s := d.at(o, sx, sz); s != nil {
				f(s)
			}
		}
	}
}

// intN is a random that draws bounded ints.
type intN interface{ IntN(n int32) int32 }

// shuffleBack is BDS's backward Fisher-Yates shuffle.
func shuffleBack[T any](s []T, r intN) {
	for i := len(s); i > 1; i-- {
		j := r.IntN(int32(i))
		s[i-1], s[j] = s[j], s[i-1]
	}
}

// djRegion is free space: inside in, clear of out.
type djRegion struct {
	in  structure.Box
	out []structure.Box
}

func (g *djRegion) fits(bb structure.Box) bool {
	if !bb.Inside(g.in) {
		return false
	}
	for _, b := range g.out {
		if bb.Intersects(b) {
			return false
		}
	}
	return true
}

type djState struct {
	p      *structure.Piece
	region *djRegion
	prio   int32
}

type djAssembly struct {
	o      *Overworld
	d      *djDef
	r      *rand.Java
	pools  map[string]string // bound aliases
	pieces []*structure.Piece
	queue  []djState
}

func (a *djAssembly) pool(name string) *structure.Pool {
	if to, ok := a.pools[name]; ok {
		name = to
	}
	if name == "minecraft:empty" {
		return &emptyPool
	}
	return a.d.pools[name]
}

// emptyPool is minecraft:empty: a pool with no elements.
var emptyPool = structure.Pool{Name: "minecraft:empty", Fallback: "minecraft:empty"}

// sortedJigsaws are a piece's jigsaws shuffled, then sorted by selection
// priority, highest first (sub_D1EEED0).
func (a *djAssembly) sortedJigsaws(p *structure.Piece) []structure.JigsawBlock {
	js := p.PlacedJigsaws()
	shuffleBack(js, a.r)
	slices.SortStableFunc(js, func(x, y structure.JigsawBlock) int { return int(y.SelPrio) - int(x.SelPrio) })
	return js
}

// assemble builds the structure starting in chunk (cx, cz). Its random is
// Java's, seeded as Java's setLargeFeatureSeed does: the start height,
// turn and element and every draw of the assembly come from it (checked
// against BDS's trial chamber starts on the comparison seed).
func (d *djDef) assemble(o *Overworld, cx, cz int32) *djStructure {
	if len(d.variants) == 0 {
		return d.assembleOne(o, cx, cz)
	}
	// A set of several structures draws one by weight and tries it; one
	// that fails (its biome, its height) drops out and the draw repeats
	// (the structure set loop of BDS's createStructures).
	// VERIFY: read from the named 1.21.3 build, not yet in 1.26.52.
	var r rand.Java
	largeFeatureSeed(&r, o.seed, cx, cz)
	vs := slices.Clone(d.variants)
	var total int32
	for _, v := range vs {
		total += v.weight
	}
	for len(vs) > 0 && total > 0 {
		n := r.IntN(total)
		i := 0
		for ; i < len(vs)-1; i++ {
			if n -= vs[i].weight; n < 0 {
				break
			}
		}
		if s := vs[i].d.assembleOne(o, cx, cz); s != nil {
			return s
		}
		total -= vs[i].weight
		vs = slices.Delete(vs, i, i+1)
	}
	return nil
}

// largeFeatureSeed seeds r as Java's setLargeFeatureSeed does.
func largeFeatureSeed(r *rand.Java, seed int64, cx, cz int32) {
	r.Seed(seed)
	a := int64(r.Next(32))<<32 + int64(r.Next(32))
	b := int64(r.Next(32))<<32 + int64(r.Next(32))
	r.Seed(int64(cx)*a ^ int64(cz)*b ^ seed)
}

// assembleOne builds one structure starting in chunk (cx, cz).
func (d *djDef) assembleOne(o *Overworld, cx, cz int32) *djStructure {
	var r rand.Java
	largeFeatureSeed(&r, o.seed, cx, cz)
	y := d.minY0
	if d.maxY0 > d.minY0 {
		y += r.IntN(d.maxY0 - d.minY0 + 1)
	}
	rot := int(r.IntN(4))
	start := d.pools[d.start]
	if start == nil {
		return nil
	}
	list := start.Expanded()
	if len(list) == 0 {
		return nil
	}
	el := list[r.IntN(int32(len(list)))]
	tpl := o.st.Template(el.Template)
	if tpl == nil {
		return nil
	}
	p := &structure.Piece{Element: el, Template: tpl, Pos: [3]int32{cx * 16, y, cz * 16}, Rotation: rot, Ground: 1}
	p.BB = structure.TemplateBox(tpl.Template, p.Pos, rot)
	mx := p.BB.X0 + (p.BB.X1-p.BB.X0+1)>>1
	mz := p.BB.Z0 + (p.BB.Z1-p.BB.Z0+1)>>1
	if d.project {
		y += o.surfaceNoStructures(mx, mz)
	}
	// The piece's ground (one above its bottom) goes to y.
	dy := y - 1 - p.BB.Y0
	p.Pos[1] += dy
	p.BB = p.BB.Raise(dy)
	if p.BB.Y0 < MinY+d.padBottom || p.BB.Y1 > MaxY-1-d.padTop {
		return nil
	}
	// The biome is checked at the start position.
	b := o.BiomeAt(mx, y, mz)
	for _, t := range d.biomeTags {
		if !o.biomeHas(b, t) {
			return nil
		}
	}
	if d.biomes != nil && (int(b) >= len(d.biomes) || !d.biomes[b]) {
		return nil
	}
	a := &djAssembly{o: o, d: d, r: &r, pieces: []*structure.Piece{p}, pools: map[string]string{}}
	if len(d.aliases) > 0 {
		// The aliases are bound with Java's legacy positional random
		// (sub_D23EC10): Random(world seed), forked with one nextLong,
		// then at the start, before any projection to the surface.
		var jr, ar rand.Java
		jr.Seed(o.seed)
		fork := int64(jr.Next(32))<<32 + int64(jr.Next(32))
		ar.Seed(rand.BlockSeed(cx*16, y, cz*16) ^ fork)
		for _, al := range d.aliases {
			for _, pr := range al.groups[ar.IntN(int32(len(al.groups)))] {
				a.pools[pr[0]] = pr[1]
			}
		}
	}
	root := &djRegion{
		in: structure.NewBox(mx-d.maxH, max(y-d.maxV, MinY+d.padBottom), mz-d.maxH,
			mx+d.maxH, min(y+d.maxV, MaxY-1-d.padTop), mz+d.maxH),

		out: []structure.Box{p.BB},
	}
	a.queue = []djState{{p, root, 0}}
	for len(a.queue) > 0 {
		// The highest placement priority first, in insertion order.
		best := 0
		for i, q := range a.queue {
			if q.prio > a.queue[best].prio {
				best = i
			}
		}
		q := a.queue[best]
		a.queue = append(a.queue[:best], a.queue[best+1:]...)
		a.children(q)
	}
	s := &djStructure{pieces: a.pieces, bb: p.BB, start: [3]int32{mx, y, mz}}
	for _, q := range a.pieces[1:] {
		s.bb = structure.NewBox(min(s.bb.X0, q.BB.X0), min(s.bb.Y0, q.BB.Y0), min(s.bb.Z0, q.BB.Z0),
			max(s.bb.X1, q.BB.X1), max(s.bb.Y1, q.BB.Y1), max(s.bb.Z1, q.BB.Z1))

	}
	return s
}

// children tries every jigsaw of a placed piece.
func (a *djAssembly) children(q djState) {
	p := q.p
	var inner *djRegion
	for _, j := range a.sortedJigsaws(p) {
		pool := a.pool(j.Pool)
		if pool == nil || len(pool.Entries) == 0 {
			continue
		}
		fallback := a.pool(pool.Fallback)
		if fallback == nil {
			continue
		}
		dir := structure.JigsawDir(j.Facing)
		target := [3]int32{j.Pos[0] + dir[0], j.Pos[1] + dir[1], j.Pos[2] + dir[2]}
		region := q.region
		if p.BB.Contains(target) {
			if inner == nil {
				inner = &djRegion{in: p.BB}
			}
			region = inner
		}
		var cands []*structure.Element
		if p.Depth != a.d.maxDepth {
			l := pool.Expanded()
			shuffleBack(l, a.r)
			cands = l
		}
		l := fallback.Expanded()
		shuffleBack(l, a.r)
		cands = append(cands, l...)
		a.fit(q, j, target, region, cands)
	}
}

// fit attaches the first candidate that fits to jigsaw j.
func (a *djAssembly) fit(q djState, j structure.JigsawBlock, target [3]int32, region *djRegion, cands []*structure.Element) {
	want := structure.Opposite(j.Facing)
	for _, el := range cands {
		if el.Template == "" && el.Feature == "" {
			return
		}
		tpl := a.o.elementTemplate(el)
		if tpl == nil {
			return
		}
		rots := []int{0, 1, 2, 3}
		shuffleBack(rots, a.r)
		for _, rot := range rots {
			cand := &structure.Piece{Element: el, Template: tpl, Rotation: rot}
			for _, cj := range a.sortedJigsaws(cand) {
				if cj.Facing != want || cj.Name != j.Target || j.Aligned && cj.Top != j.Top {
					continue
				}
				pos := [3]int32{target[0] - cj.Pos[0], target[1] - cj.Pos[1], target[2] - cj.Pos[2]}
				bb := structure.TemplateBox(tpl.Template, pos, rot)
				if !region.fits(bb) {
					continue
				}
				region.out = append(region.out, bb)
				cand.Pos, cand.BB, cand.Depth = pos, bb, q.p.Depth+1
				// The joining jigsaw's height in the piece is its
				// ground, as for villages.
				// VERIFY: for data-driven structures.
				cand.Ground = cj.Pos[1]
				a.pieces = append(a.pieces, cand)
				if cand.Depth <= a.d.maxDepth {
					a.queue = append(a.queue, djState{cand, region, j.PlacePrio})
				}
				return
			}
		}
	}
}

// featureTemplate stands in for every feature pool element: size 0, so its
// box is empty and blocks no other piece (BDS's FeaturePoolElement getSize,
// sub_D231C90, returns 0), with one jigsaw at its origin facing down, named
// minecraft:bottom and joining nothing (sub_D231DD0), so the assembly's
// draws run as they should. The features are placed during decoration.
var featureTemplate = &structure.JigsawTemplate{Template: &structure.Template{},
	Jigsaws: []structure.JigsawBlock{{Facing: "down", Top: "south", Name: "minecraft:bottom", Target: "minecraft:empty", Pool: "minecraft:empty",
		Attachment: "minecraft:bottom"}}}

// elementTemplate is the template an element places.
func (o *Overworld) elementTemplate(el *structure.Element) *structure.JigsawTemplate {
	if el.Feature != "" {
		return featureTemplate
	}
	return o.st.Template(el.Template)
}

// dataJigsawBeards lists the beards of beard_thin data-driven structures
// near chunk (cx, cz). BDS keeps these in its classic list (sub_D1F06A0),
// which works as Java's beard_thin: each piece's own box, not widened as a
// village's is, with its ground one block above its bottom. Checked against
// BDS at two abandoned camps (765 of 768 columns the same height at one).
func (o *Overworld) dataJigsawBeards(cx, cz int32) []beardBox {
	var out []beardBox
	for _, d := range o.dataJigsaws {
		if !d.beard {
			continue
		}
		d.near(o, cx, cz, func(s *djStructure) {
			for _, p := range s.pieces {
				if p.Element.Feature != "" {
					continue
				}
				if (p.BB.X0-12)>>4 <= cx && cx <= (p.BB.X1+12)>>4 && (p.BB.Z0-12)>>4 <= cz && cz <= (p.BB.Z1+12)>>4 {
					out = append(out, beardBox{x0: p.BB.X0, z0: p.BB.Z0, x1: p.BB.X1, z1: p.BB.Z1, ground: p.BB.Y0 + 1})
				}
			}
		})
	}
	return out
}

// surfaceNoStructures is the world surface (the y above the highest
// non-air block, water included) of the terrain fill without structure
// terrain adjustments.
func (o *Overworld) surfaceNoStructures(x, z int32) int32 {
	h := o.plainHeights.Get([2]int32{x >> 4, z >> 4}, func() *[256]int16 { return o.plainHeightMap(x>>4, z>>4) })
	return int32(h[(z&15)<<4|x&15])
}

// plainHeightMap is surfaceNoStructures for every column of chunk (cx, cz).
func (o *Overworld) plainHeightMap(cx, cz int32) *[256]int16 {
	var cols [gridXZ * gridXZ]column
	for i := range gridXZ {
		for k := range gridXZ {
			cols[i*gridXZ+k] = o.column(cx*4+int32(i), cz*4+int32(k))
		}
	}
	c := new(Chunk)
	g := new(grids)
	o.fillDensity(cx, cz, &cols, g)
	o.fillBlocksAdj(cx, cz, g, newAquifer(o, cx, cz), c, nil, nil)
	h := new([256]int16)
	for i := range h {
		h[i] = MinY
		for y := MaxY - 1; y >= MinY; y-- {
			if c.Block(i&15, y, i>>4) != o.ids.air {
				h[i] = int16(y + 1)
				break
			}
		}
	}
	return h
}

// buryBoxes lists the piece boxes of buried structures near chunk
// (cx, cz), for the terrain fill.
// VERIFY: pieces are registered within 12 blocks of the chunk, as the
// city's beards are; trial chambers' encapsulate is taken as bury.
func (o *Overworld) buryBoxes(cx, cz int32) []structure.Box {
	var out []structure.Box
	area := structure.NewBox(cx*16-12, MinY, cz*16-12, cx*16+27, MaxY-1, cz*16+27)
	for _, d := range o.dataJigsaws {
		if !d.bury {
			continue
		}
		d.near(o, cx, cz, func(s *djStructure) {
			if !s.bb.Intersects(area) {
				return
			}
			for _, p := range s.pieces {
				if p.BB.Intersects(area) {
					out = append(out, p.BB)
				}
			}
		})
	}
	return out
}

// buryAt is the bury density at (x, y, z): 1 - d/6 for each box within 6
// blocks, d the whole distance to the box with y halved (sub_D1EE730).
func buryAt(bs []structure.Box, x, y, z int32) float32 {
	var sum float32
	for _, b := range bs {
		cy := min(max(y, b.Y0), b.Y1)
		dx := max(b.X0-x, x-b.X1, 0)
		dz := max(b.Z0-z, z-b.Z1, 0)
		dy := int32(float32(y-cy) * 0.5)
		if dx > 6 || dz > 6 || dy > 6 || dy < -6 {
			continue
		}
		dist := float32(int32(math.Sqrt(float64(dy*dy + dx*dx + dz*dz))))
		f := dist / 6
		if f <= 1 {
			sum = float32(sum + (1 - max(f, 0)))
		}
	}
	return sum
}

// placeDataJigsaws writes the pieces of data-driven jigsaw structures that
// reach chunk (cx, cz) into c.
func (o *Overworld) placeDataJigsaws(cx, cz int32, c *Chunk) {
	chunkBox := structure.NewBox(cx*16, MinY, cz*16, cx*16+15, MaxY-1, cz*16+15)
	for _, d := range o.dataJigsaws {
		d.near(o, cx, cz, func(s *djStructure) {
			if !s.bb.Intersects(chunkBox) {
				return
			}
			for _, p := range s.pieces {
				if p.BB.Intersects(chunkBox) {
					o.placePiece(p, c, chunkBox, nil)
				}
			}
		})
	}
}

// dataJigsawsReach reports whether a data-driven jigsaw structure has a
// piece in chunk (cx, cz).
func (o *Overworld) dataJigsawsReach(cx, cz int32) bool {
	chunkBox := structure.NewBox(cx*16, MinY, cz*16, cx*16+15, MaxY-1, cz*16+15)
	hit := false
	for _, d := range o.dataJigsaws {
		d.near(o, cx, cz, func(s *djStructure) {
			if hit || !s.bb.Intersects(chunkBox) {
				return
			}
			for _, p := range s.pieces {
				if p.BB.Intersects(chunkBox) {
					hit = true
					return
				}
			}
		})
	}
	return hit
}

// NearestDataJigsaw finds the trail ruins, trial chambers or abandoned camp
// ("trail_ruins", "trial_chambers", "abandoned_camp") whose start chunk is nearest to block (x, z),
// searching maxRegions placement regions out. It is safe for concurrent
// use.
func (o *Overworld) NearestDataJigsaw(kind string, x, z, maxRegions int32) (Scattered, bool) {
	var d *djDef
	for _, dd := range o.dataJigsaws {
		if dd.kind == kind {
			d = dd
		}
	}
	if d == nil {
		return Scattered{}, false
	}
	type cand struct {
		cx, cz int32
		dist   int64
	}
	var cands []cand
	rx0, rz0 := floorDivRegion(x>>4, d.spacing), floorDivRegion(z>>4, d.spacing)
	for rx := rx0 - maxRegions; rx <= rx0+maxRegions; rx++ {
		for rz := rz0 - maxRegions; rz <= rz0+maxRegions; rz++ {
			cx, cz := d.pick(o, rx, rz)
			dx, dz := int64(cx*16+8-x), int64(cz*16+8-z)
			cands = append(cands, cand{cx, cz, dx*dx + dz*dz})
		}
	}
	slices.SortFunc(cands, func(a, b cand) int {
		switch {
		case a.dist < b.dist:
			return -1
		case a.dist > b.dist:
			return 1
		}
		return 0
	})
	for _, c := range cands {
		if s := d.at(o, c.cx, c.cz); s != nil {
			return Scattered{Kind: kind, X: s.start[0], Y: s.start[1], Z: s.start[2]}, true
		}
	}
	return Scattered{}, false
}

// dataJigsawFeatures places the features of the data-driven structures'
// feature pieces standing in chunk (ox, oz), such as an abandoned camp's
// trees. BDS runs structure pieces after reseeding the chunk's population
// random (OverworldGenerator::postProcess, before postProcessStructures),
// and a feature piece places its feature at its own position with that
// random (FeaturePoolElement::place), as cityFeatures does.
// The features may not change any cell the structure's templates fill, its
// air included, so a camp's trees never grow through the camp. This is a
// Helix rule, not BDS's: BDS lets leaves fill a template's air.
// VERIFY: read from the named 1.21.3 build; the old structures run first
// with the same random and are taken to draw nothing in a camp's chunk.
// Against BDS the trees stand on the right spots, but BDS's heights differ
// and it leaves out one of camp (-844, 2222)'s three trees.
func (o *Overworld) dataJigsawFeatures(w *featureWorld, ox, oz int32, pop uint32) {
	var r *rand.MT
	for _, d := range o.dataJigsaws {
		d.near(o, ox, oz, func(s *djStructure) {
			defer func() { w.guard = nil }()
			for _, p := range s.pieces {
				if p.Element.Feature == "" {
					// A template piece draws one number per chest it
					// places in this chunk, for the chest's loot seed
					// (LegacyStructureTemplate::placeInWorld), before
					// the pieces after it.
					if n := chestsInChunk(p, ox, oz); n > 0 {
						if r == nil {
							r = rand.NewMT(pop)
						}
						for range n {
							r.Uint32()
						}
					}
					continue
				}
				if p.Pos[0]>>4 != ox || p.Pos[2]>>4 != oz {
					continue
				}
				f, ok := o.features.get(p.Element.Feature)
				if !ok {
					continue
				}
				if r == nil {
					r = rand.NewMT(pop)
				}
				if w.guard == nil {
					w.guard = structureCells(s)
				}
				f.place(w, p.Pos[0], p.Pos[1], p.Pos[2], r)
			}
		})
	}
}

// chestsInChunk counts the chests template piece p places in chunk
// (cx, cz).
func chestsInChunk(p *structure.Piece, cx, cz int32) int {
	if p.Template == nil {
		return 0
	}
	n := 0
	for _, b := range p.Template.Template.Blocks {
		if name := p.Template.Template.Palette[b.State].Name; name != "minecraft:chest" && name != "minecraft:trapped_chest" {
			continue
		}
		rp := structure.RotatePos(b.Pos, p.Rotation)
		if (rp[0]+p.Pos[0])>>4 == cx && (rp[2]+p.Pos[2])>>4 == cz {
			n++
		}
	}
	return n
}

// structureCells lists every cell the templates of a structure fill.
func structureCells(s *djStructure) map[[3]int32]bool {
	cells := map[[3]int32]bool{}
	for _, p := range s.pieces {
		if p.Element.Feature != "" || p.Template == nil {
			continue
		}
		for _, b := range p.Template.Template.Blocks {
			if p.Template.Template.Palette[b.State].Name == "minecraft:structure_void" {
				continue
			}
			rp := structure.RotatePos(b.Pos, p.Rotation)
			cells[[3]int32{rp[0] + p.Pos[0], rp[1] + p.Pos[1], rp[2] + p.Pos[2]}] = true
		}
	}
	return cells
}

// campCells lists the cells of the abandoned camps whose box comes within
// 16 blocks of chunk (cx, cz), the reach of a tree from a neighbour; nil
// when there are none.
func (o *Overworld) campCells(cx, cz int32) map[[3]int32]bool {
	var cells map[[3]int32]bool
	area := structure.NewBox(cx*16-16, MinY, cz*16-16, cx*16+31, MaxY-1, cz*16+31)
	for _, d := range o.dataJigsaws {
		if d.kind != "abandoned_camp" {
			continue
		}
		d.near(o, cx, cz, func(s *djStructure) {
			if !s.bb.Intersects(area) {
				return
			}
			if cells == nil {
				cells = map[[3]int32]bool{}
			}
			for c := range structureCells(s) {
				cells[c] = true
			}
		})
	}
	return cells
}

// JigsawPieces returns the pieces, in assembly order, of the first
// data-driven jigsaw structure whose id contains id (such as
// "trial_chambers") that reaches chunk (cx, cz), if any. It is meant for
// tools.
func (o *Overworld) JigsawPieces(id string, cx, cz int32) []PieceInfo {
	var out []PieceInfo
	for _, d := range o.dataJigsaws {
		if !strings.Contains(d.id, id) {
			continue
		}
		d.near(o, cx, cz, func(s *djStructure) {
			if out != nil {
				return
			}
			out = make([]PieceInfo, 0, len(s.pieces))
			for _, p := range s.pieces {
				name := p.Element.Template
				if name == "" {
					name = p.Element.Feature
				}
				out = append(out, PieceInfo{Template: name, Box: [6]int32{p.BB.X0, p.BB.Y0, p.BB.Z0, p.BB.X1, p.BB.Y1, p.BB.Z1},
					Rotation: p.Rotation, Depth: p.Depth})
			}
		})
		if out != nil {
			return out
		}
	}
	return out
}
