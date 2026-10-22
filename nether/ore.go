package nether

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/helix-bedrock/worldgen"
	"github.com/helix-bedrock/worldgen/feature"
	"github.com/helix-bedrock/worldgen/rand"
)

// oreFeature is a minecraft:ore_feature file: a vein of count spheres
// (feature.OreVein) whose blocks become the first replace rule's block
// that may replace them.
type oreFeature struct {
	count int32
	rules []oreRule
}

// oreFile is a minecraft:ore_feature file.
type oreFile struct {
	Ore struct {
		Count int32         `json:"count"`
		Rules []oreFileRule `json:"replace_rules"`
	} `json:"minecraft:ore_feature"`
}

// oreFileRule is one replace rule of an ore feature file.
type oreFileRule struct {
	Places     string           `json:"places_block"`
	MayReplace []oreFileReplace `json:"may_replace"`
}

// oreFileReplace is a block an ore rule may replace.
type oreFileReplace struct {
	Name   string         `json:"name"`
	States map[string]any `json:"states"`
}

type oreRule struct {
	place   uint32
	replace []bool // by state
}

// loadOre reads the ore feature id. It supports replace rules without
// states, the only kind the Nether's files use.
func (n *Nether) loadOre(id string) (*oreFeature, error) {
	name := strings.TrimPrefix(id, "minecraft:")
	raw, err := worldgen.Data.ReadFile("data/vanilla/features/" + name + ".json")
	if err != nil {
		return nil, fmt.Errorf("worldgen: read %s: %w", id, err)
	}
	var f oreFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("worldgen: decode %s: %w", id, err)
	}
	o := &oreFeature{count: f.Ore.Count}
	for _, rr := range f.Ore.Rules {
		place, ok := n.reg.Default(rr.Places)
		if !ok {
			return nil, fmt.Errorf("worldgen: %s: block %s missing from the block list", id, rr.Places)
		}
		rule := oreRule{place: place, replace: make([]bool, n.reg.Len())}
		for _, m := range rr.MayReplace {
			if len(m.States) > 0 {
				return nil, fmt.Errorf("worldgen: %s: replace states not supported", id)
			}
			for i := range rule.replace {
				if s, ok := n.reg.State(uint32(i)); ok && s.Name == m.Name {
					rule.replace[i] = true
				}
			}
		}
		o.rules = append(o.rules, rule)
	}
	return o, nil
}

func (o *oreFeature) place(n *Nether, r *region, x, y, z int32, mt *rand.MT) {
	feature.OreVein(o.count, x, y, z, mt, func(bx, by, bz int32) {
		cur := r.at(bx, by, bz)
		for _, rule := range o.rules {
			if rule.replace[cur] && cur != rule.place {
				r.set(bx, by, bz, rule.place)
				return
			}
		}
	})
}
