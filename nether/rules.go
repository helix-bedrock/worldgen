package nether

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"github.com/helix-bedrock/worldgen"
	"github.com/helix-bedrock/worldgen/biome"
	"github.com/helix-bedrock/worldgen/feature"
	"github.com/helix-bedrock/worldgen/rand"
)

// The Nether biomes' feature rules run in postProcess through the
// generator's rule step (vtable slot 56, sub_5163DB0): for each placement
// pass, the rules of one biome per chunk, the biome at the chunk's stored
// position, with no per-position biome check. Each rule draws its
// positions and its feature from Mersenne Twisters of its own, seeded from
// the chunk's population seed and the rule's name, as in the overworld.

// netherPasses are the placement passes in BDS's order (the overworld's).
var netherPasses = []string{
	"first_pass", "underground_pass", "after_underground_pass", "before_surface_pass",
	"surface_pass", "after_surface_pass", "before_sky_pass", "sky_pass", "after_sky_pass", "final_pass",
}

// placeFunc places a feature at a position, drawing from its random.
type placeFunc func(n *Nether, r *region, x, y, z int32, mt *rand.MT)

// netherRule is one feature_rules file whose feature the Nether builds.
type netherRule struct {
	id         string
	hash       uint32
	place      placeFunc
	iterations int32
	// num/den is the scatter chance; den 0 places every time.
	num, den int32
	order    [3]int
	// axes are uniform [lo, hi) offsets from the chunk corner (y from 0);
	// lo == hi is a constant.
	axes [3][2]int32
}

// coordinateOrders maps coordinate_eval_order to the axis order (see the
// overworld's); a rule without one evaluates x, z, y.
var coordinateOrders = map[string][3]int{
	"xyz": {0, 1, 2}, "xzy": {0, 2, 1}, "yxz": {1, 0, 2},
	"yzx": {1, 2, 0}, "zxy": {2, 0, 1}, "zyx": {2, 1, 0},
}

// netherFeatures are the features the Nether rules place that are built.
func (n *Nether) netherFeatures() (map[string]placeFunc, error) {
	feats := map[string]placeFunc{
		"minecraft:_huge_crimson_fungus_feature": func(n *Nether, r *region, x, y, z int32, mt *rand.MT) {
			n.hugeFungus(r, x, y, z, mt, &n.crimson)
		},
		"minecraft:_huge_warped_fungus_feature": func(n *Nether, r *region, x, y, z int32, mt *rand.MT) {
			n.hugeFungus(r, x, y, z, mt, &n.warped)
		},
		"minecraft:_weeping_vines_cluster_feature":  (*Nether).weepingVinesCluster,
		"minecraft:_twisting_vines_cluster_feature": (*Nether).twistingVinesCluster,
	}
	for _, id := range []string{"minecraft:crimson_fungus_scatter_feature", "minecraft:warped_fungus_scatter_feature",
		"minecraft:crimson_roots_scatter_feature", "minecraft:warped_roots_scatter_feature",
		"minecraft:nether_sprouts_scatter_feature"} {
		p, err := n.loadPlantScatter(id)
		if err != nil {
			return nil, err
		}
		feats[id] = p.place
	}
	return feats, nil
}

// loadRules reads the feature rules and attaches the ones with a built
// feature to the Nether biomes their filter accepts, by pass, in file name
// order (as the overworld keeps them).
func (n *Nether) loadRules(biomeIDs map[string]uint32) error {
	defs, err := biome.LoadDefs(worldgen.Data, "data/vanilla/biomes")
	if err != nil {
		return err
	}
	files, err := fs.Glob(worldgen.Data, "data/vanilla/feature_rules/*.json")
	if err != nil {
		return fmt.Errorf("worldgen: list feature rules: %w", err)
	}
	slices.Sort(files)
	feats, err := n.netherFeatures()
	if err != nil {
		return err
	}
	n.rules = map[uint32][][]*netherRule{}
	for _, f := range files {
		data, err := fs.ReadFile(worldgen.Data, f)
		if err != nil {
			return fmt.Errorf("worldgen: read %s: %w", f, err)
		}
		var raw struct {
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
					X          json.RawMessage `json:"x"`
					Y          json.RawMessage `json:"y"`
					Z          json.RawMessage `json:"z"`
				} `json:"distribution"`
			} `json:"minecraft:feature_rules"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return fmt.Errorf("worldgen: decode %s: %w", f, err)
		}
		r := raw.Rules
		place, ok := feats[r.Description.PlacesFeature]
		if !ok {
			continue
		}
		pass := slices.Index(netherPasses, r.Conditions.Pass)
		if pass < 0 {
			continue
		}
		rule := &netherRule{id: r.Description.Identifier, hash: feature.RuleHash(r.Description.Identifier), place: place, order: [3]int{0, 2, 1}}
		if err := json.Unmarshal(r.Distribution.Iterations, &rule.iterations); err != nil {
			return fmt.Errorf("worldgen: rule %s: iterations: %w", rule.id, err)
		}
		if len(r.Distribution.Scatter) > 0 {
			var frac struct{ Numerator, Denominator int32 }
			if err := json.Unmarshal(r.Distribution.Scatter, &frac); err != nil || frac.Denominator <= 0 {
				return fmt.Errorf("worldgen: rule %s: scatter chance %s not supported", rule.id, r.Distribution.Scatter)
			}
			rule.num, rule.den = frac.Numerator, frac.Denominator
		}
		if r.Distribution.Order != "" {
			if rule.order, ok = coordinateOrders[r.Distribution.Order]; !ok {
				return fmt.Errorf("worldgen: rule %s: coordinate order %q", rule.id, r.Distribution.Order)
			}
		}
		for i, a := range []json.RawMessage{r.Distribution.X, r.Distribution.Y, r.Distribution.Z} {
			var err error
			if rule.axes[i], err = parseUniform(a); err != nil {
				return fmt.Errorf("worldgen: rule %s: %w", rule.id, err)
			}
		}
		for name, id := range biomeIDs {
			d, ok := defs[name]
			if !ok || !slices.Contains(d.Tags, "nether") {
				continue
			}
			ok, err := feature.FilterAccepts(r.Conditions.Filter, d.Tags)
			if err != nil {
				return fmt.Errorf("worldgen: rule %s: %w", rule.id, err)
			}
			if !ok {
				continue
			}
			if n.rules[id] == nil {
				n.rules[id] = make([][]*netherRule, len(netherPasses))
			}
			n.rules[id][pass] = append(n.rules[id][pass], rule)
		}
	}
	return nil
}

// parseUniform reads a constant or a uniform extent axis.
func parseUniform(raw json.RawMessage) ([2]int32, error) {
	var c int32
	if json.Unmarshal(raw, &c) == nil {
		return [2]int32{c, c}, nil
	}
	var a struct {
		Distribution string   `json:"distribution"`
		Extent       [2]int32 `json:"extent"`
	}
	if err := json.Unmarshal(raw, &a); err != nil || a.Distribution != "uniform" {
		return [2]int32{}, fmt.Errorf("axis %s not supported", strings.TrimSpace(string(raw)))
	}
	return a.Extent, nil
}

// runRules runs the feature rules of biome b for origin (ox, oz).
func (n *Nether) runRules(r *region, ox, oz int32, b uint32) {
	passes := n.rules[b]
	if passes == nil {
		return
	}
	pop := populationSeed(n.seed, ox, oz)
	var ruleMT, featMT rand.MT
	base := [3]int32{ox * 16, 0, oz * 16}
	for _, rules := range passes {
		for _, rule := range rules {
			seed := feature.RuleSeed(pop, rule.hash)
			ruleMT.Seed(seed)
			if rule.den > 0 && rule.num != rule.den && ruleMT.IntN(rule.den) >= rule.num {
				continue
			}
			featMT.Seed(seed)
			for range rule.iterations {
				var pos [3]int32
				for _, a := range rule.order {
					lo, hi := rule.axes[a][0], rule.axes[a][1]
					pos[a] = base[a] + lo
					if hi > lo {
						pos[a] += ruleMT.IntN(hi - lo)
					}
				}
				rule.place(n, r, pos[0], pos[1], pos[2], &featMT)
			}
		}
	}
}
