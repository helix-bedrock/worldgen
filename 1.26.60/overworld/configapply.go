package overworld

import (
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/helix-bedrock/worldgen/1.26.60/biome"
)

// climateScale is the fixed-point scale of climate parameters (biome.Param).
const climateScale = 10000

// maxSuggestions is how many close names an unknown-name error lists.
const maxSuggestions = 3

// unknownName is the error for a name that matches nothing, with the
// closest known names.
func unknownName(what, name string, known []string) error {
	type cand struct {
		name string
		dist int
	}
	cands := make([]cand, 0, len(known))
	for _, k := range known {
		cands = append(cands, cand{k, editDistance(name, k)})
	}
	slices.SortFunc(cands, func(a, b cand) int { return a.dist - b.dist })
	var near []string
	for i := 0; i < len(cands) && i < maxSuggestions; i++ {
		near = append(near, cands[i].name)
	}
	if len(near) == 0 {
		return fmt.Errorf("worldgen: unknown %s %q", what, name)
	}
	return fmt.Errorf("worldgen: unknown %s %q (did you mean %s?)", what, name, strings.Join(near, ", "))
}

// editDistance is the Levenshtein distance between a and b.
func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

// matchIDs returns the IDs that match pattern ("*" for any run of
// characters), or an error naming close IDs when none does.
func matchIDs(what, pattern string, ids []string) ([]string, error) {
	pattern = fullID(pattern)
	var out []string
	for _, id := range ids {
		ok, err := path.Match(pattern, id)
		if err != nil {
			return nil, fmt.Errorf("worldgen: %s pattern %q: %w", what, pattern, err)
		}
		if ok {
			out = append(out, id)
		}
	}
	if len(out) == 0 {
		return nil, unknownName(what, pattern, ids)
	}
	return out, nil
}

// quantize turns a climate value into the fixed point of biome.Param.
func climateParam(r Range) biome.Param {
	if r == (Range{}) {
		return biome.Param{Min: -climateScale, Max: climateScale}
	}
	return biome.Param{Min: int64(float64(r.Min) * climateScale), Max: int64(float64(r.Max) * climateScale)}
}

// biomeEntries applies the biome changes to the climate table.
func (c *Config) biomeEntries(entries []biome.Entry, ids map[string]uint32) ([]biome.Entry, error) {
	known := func() []string {
		seen := map[string]bool{}
		var out []string
		for _, e := range entries {
			if !seen[e.Biome] {
				seen[e.Biome] = true
				out = append(out, e.Biome)
			}
		}
		slices.Sort(out)
		return out
	}
	for _, b := range c.Biomes.added {
		name := biomeName(b.Name)
		if _, ok := ids[name]; !ok {
			return nil, fmt.Errorf("worldgen: biome %q has no ID in the biome ID map", name)
		}
		space := [7]biome.Param{
			climateParam(b.Climate.Temperature), climateParam(b.Climate.Humidity),
			climateParam(b.Climate.Continentalness), climateParam(b.Climate.Erosion),
			climateParam(b.Climate.Depth), climateParam(b.Climate.Weirdness),
			{Min: int64(float64(b.Climate.Offset) * climateScale), Max: int64(float64(b.Climate.Offset) * climateScale)},
		}
		if b.Climate.Depth == (Range{}) {
			// A surface biome, as vanilla's: at depth 0 and at depth 1.
			for _, d := range []int64{0, climateScale} {
				space[4] = biome.Param{Min: d, Max: d}
				entries = append(entries, biome.Entry{Space: space, Biome: name})
			}
			continue
		}
		entries = append(entries, biome.Entry{Space: space, Biome: name})
	}
	for _, op := range c.Biomes.ops {
		name := biomeName(op.name)
		has := slices.ContainsFunc(entries, func(e biome.Entry) bool { return e.Biome == name })
		if name != "*" && !has && op.kind != opSetTags {
			return nil, unknownName("biome", name, known())
		}
		switch op.kind {
		case opRemoveBiome:
			entries = slices.DeleteFunc(entries, func(e biome.Entry) bool { return e.Biome == name })
			if len(entries) == 0 {
				return nil, fmt.Errorf("worldgen: removing biome %q leaves no biome", name)
			}
		case opReplaceBiome:
			to := biomeName(op.to)
			if _, ok := ids[to]; !ok {
				return nil, unknownName("biome", to, known())
			}
			for i := range entries {
				if name == "*" || entries[i].Biome == name {
					entries[i].Biome = to
				}
			}
		case opScaleBiome:
			if op.factor <= 0 {
				return nil, fmt.Errorf("worldgen: biome %q scale %v must be above 0", name, op.factor)
			}
			for i := range entries {
				if entries[i].Biome != name {
					continue
				}
				// Every axis but the offset.
				for a := range 6 {
					p := &entries[i].Space[a]
					mid, half := (p.Min+p.Max)/2, float64(p.Max-p.Min)/2*float64(op.factor)
					p.Min, p.Max = mid-int64(half), mid+int64(half)
				}
			}
		}
	}
	return entries, nil
}

// biomeDefs adds the biomes of your own to defs, copied from the biomes
// they are like, and applies SetTags.
func (c *Config) biomeDefs(defs map[string]*biome.Def) error {
	names := func() []string {
		out := make([]string, 0, len(defs))
		for n := range defs {
			out = append(out, n)
		}
		slices.Sort(out)
		return out
	}
	for _, b := range c.Biomes.added {
		like, ok := defs[biomeName(b.Like)]
		if !ok {
			return unknownName("biome", biomeName(b.Like), names())
		}
		d := *like
		d.Name = biomeName(b.Name)
		if b.Tags != nil {
			d.Tags = slices.Clone(b.Tags)
		}
		defs[d.Name] = &d
	}
	for _, op := range c.Biomes.ops {
		if op.kind != opSetTags {
			continue
		}
		d, ok := defs[biomeName(op.name)]
		if !ok {
			return unknownName("biome", biomeName(op.name), names())
		}
		nd := *d
		nd.Tags = slices.Clone(op.tags)
		defs[nd.Name] = &nd
	}
	return nil
}

// applyFeatures applies the feature changes to the loaded features:
// registered Go features, DisableFeature and SetWeight.
func (o *Overworld) applyFeatures(c *Config, set *featureSet) error {
	for id, f := range c.Features.registered {
		if f == nil {
			return fmt.Errorf("worldgen: feature %q registered as nil", id)
		}
		set.byID[id] = userFeature{id: id, f: f, log: c.Logger}
	}
	ids := make([]string, 0, len(set.byID))
	for id := range set.byID {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, op := range c.Features.ops {
		switch op.kind {
		case opDisableFeature:
			m, err := matchIDs("feature", op.pattern, ids)
			if err != nil {
				return err
			}
			for _, id := range m {
				set.byID[id] = noFeature{}
			}
		case opSetWeight:
			mix, ok := set.byID[fullID(op.pattern)]
			if !ok {
				return unknownName("feature", fullID(op.pattern), ids)
			}
			wf, ok := mix.(*weightedFeature)
			if !ok {
				return fmt.Errorf("worldgen: feature %q is not a weighted random feature", fullID(op.pattern))
			}
			if op.value < 0 {
				return fmt.Errorf("worldgen: feature %q weight %v is below 0", fullID(op.feature), op.value)
			}
			feat := fullID(op.feature)
			if _, ok := set.byID[feat]; !ok {
				return unknownName("feature", feat, ids)
			}
			// Copy, so other references to the mix see the change too but
			// the loaded slices are not shared with anything else.
			nw := &weightedFeature{feats: slices.Clone(wf.feats), weights: slices.Clone(wf.weights)}
			i := slices.IndexFunc(nw.feats, func(r featureRef) bool { return r.id == feat })
			if i < 0 {
				nw.feats = append(nw.feats, featureRef{feat, set})
				nw.weights = append(nw.weights, op.value)
			} else {
				nw.weights[i] = op.value
			}
			set.byID[fullID(op.pattern)] = nw
		}
	}
	return nil
}

// applyRules applies the rule changes and adds the Go rules.
func (o *Overworld) applyRules(c *Config, defs map[string]*biome.Def, biomeIDs map[string]uint32) error {
	fr := o.rules
	for _, op := range c.Features.ops {
		var m []string
		switch op.kind {
		case opDisableRule, opDisableRuleIn, opSetChance, opSetIterations, opReplace:
			var err error
			if m, err = matchIDs("feature rule", op.pattern, fr.read); err != nil {
				return err
			}
		default:
			continue
		}
		for _, id := range m {
			r, ok := fr.byID[id]
			if !ok {
				// Read but not loaded: its feature or distribution is not
				// supported, so it places nothing anyway.
				continue
			}
			if err := o.applyRuleOp(op, r, biomeIDs); err != nil {
				return err
			}
		}
	}
	for _, r := range c.Features.rules {
		if r.ID == "" {
			return fmt.Errorf("worldgen: a rule placing %q has no ID", r.Places)
		}
		id := fullID(r.ID)
		if _, ok := fr.byID[id]; ok {
			return fmt.Errorf("worldgen: rule %q already exists; use a pack to replace a rule", id)
		}
		if r.Pass < FirstPass || int(r.Pass) >= passCount {
			return fmt.Errorf("worldgen: rule %q has pass %d, not one of the placement passes", id, r.Pass)
		}
		feat, ok := o.features.get(fullID(r.Places))
		if !ok {
			return unknownName("feature", fullID(r.Places), o.featureIDs())
		}
		rule := &featureRule{id: id, hash: ruleHash(id), pass: int(r.Pass), feature: feat, order: defaultOrder(),
			iterations: max(r.Iterations, 1), percent: 100}
		if r.Percent > 0 {
			rule.percent = r.Percent
		}
		rule.axes = [3]axis{{kind: distUniform, min: 0, max: 16}, {kind: distHeightmap}, {kind: distUniform, min: 0, max: 16}}
		for i, a := range []Axis{r.X, r.Y, r.Z} {
			if a.set {
				rule.axes[i] = a.a
			}
		}
		biomes := map[uint32]bool{}
		for _, n := range r.Biomes {
			bid, ok := biomeIDs[biomeName(n)]
			if !ok {
				return unknownName("biome", biomeName(n), sortedKeys(biomeIDs))
			}
			biomes[bid] = true
		}
		for name, bid := range biomeIDs {
			d, ok := defs[name]
			if !ok {
				continue
			}
			for _, t := range r.Tags {
				if slices.Contains(d.Tags, t) {
					biomes[bid] = true
				}
			}
			if len(r.Biomes) == 0 && len(r.Tags) == 0 {
				biomes[bid] = true
			}
		}
		for bid := range biomes {
			if int(bid) >= len(fr.byBiome) {
				continue
			}
			if fr.byBiome[bid] == nil {
				fr.byBiome[bid] = make([][]*featureRule, passCount)
			}
			fr.byBiome[bid][rule.pass] = append(fr.byBiome[bid][rule.pass], rule)
		}
		fr.byID[id] = rule
		fr.order = append(fr.order, id)
		fr.read = append(fr.read, id)
	}
	return nil
}

// applyRuleOp applies one rule change to rule r.
func (o *Overworld) applyRuleOp(op featureOp, r *featureRule, biomeIDs map[string]uint32) error {
	switch op.kind {
	case opDisableRule:
		r.off, r.drawOnly = true, true
	case opDisableRuleIn:
		// The rule stays as it is elsewhere; these biomes get a disabled
		// copy, which still makes the draws.
		off := *r
		off.off, off.drawOnly = true, true
		for _, n := range op.biomes {
			bid, ok := biomeIDs[biomeName(n)]
			if !ok {
				return unknownName("biome", biomeName(n), sortedKeys(biomeIDs))
			}
			if int(bid) >= len(o.rules.byBiome) || o.rules.byBiome[bid] == nil {
				continue
			}
			list := o.rules.byBiome[bid][r.pass]
			for i, x := range list {
				if x == r {
					list[i] = &off
				}
			}
		}
	case opSetChance:
		if op.value < 0 || op.value > 100 {
			return fmt.Errorf("worldgen: rule %q chance %v is not between 0 and 100", r.id, op.value)
		}
		r.num, r.den, r.percent = 0, 0, op.value
	case opSetIterations:
		if op.value < 0 {
			return fmt.Errorf("worldgen: rule %q iterations %v is below 0", r.id, op.value)
		}
		r.iterations, r.forceN = int32(op.value), true
	case opReplace:
		feat, ok := o.features.get(fullID(op.feature))
		if !ok {
			return unknownName("feature", fullID(op.feature), o.featureIDs())
		}
		r.feature = feat
		// A vanilla rule that only drew (its feature is not built) now
		// places; a disabled one stays disabled.
		r.drawOnly = r.off
	}
	return nil
}

// featureIDs lists the loaded feature IDs, sorted.
func (o *Overworld) featureIDs() []string { return sortedKeys(o.features.byID) }

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// FeatureRuleIDs lists the identifiers of the feature rules this generator
// read, vanilla, from packs and added, in the order they run within a
// pass. Rules the runtime cannot place yet are included: they still make
// their random draws.
func (o *Overworld) FeatureRuleIDs() []string { return slices.Clone(o.rules.read) }

// FeatureIDs lists the identifiers of the features this generator can
// place, sorted.
func (o *Overworld) FeatureIDs() []string { return o.featureIDs() }
