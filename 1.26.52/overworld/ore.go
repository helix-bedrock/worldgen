package overworld

import (
	"encoding/json"
	"fmt"

	"github.com/helix-bedrock/worldgen/1.26.52/biome"
	"github.com/helix-bedrock/worldgen/1.26.52/blocks"
	"github.com/helix-bedrock/worldgen/1.26.52/feature"
	"github.com/helix-bedrock/worldgen/1.26.52/rand"
)

type oreRule struct {
	place      uint32
	mayReplace []string
}

// oreFeature is minecraft:ore_feature (BDS OreFeature::place, sub_D2F38D0).
type oreFeature struct {
	count   int32
	discard float32
	rules   []oreRule
}

// rawOre is the JSON of a minecraft:ore_feature.
type rawOre struct {
	Description struct {
		Identifier string `json:"identifier"`
	} `json:"description"`
	Count   int32   `json:"count"`
	Discard float32 `json:"discard_chance_on_air_exposure"`
	Rules   []struct {
		Places     biome.Block   `json:"places_block"`
		MayReplace []biome.Block `json:"may_replace"`
	} `json:"replace_rules"`
}

func (o *Overworld) parseOre(raw json.RawMessage) (*oreFeature, string, error) {
	var r rawOre
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, "", err
	}
	f := &oreFeature{count: r.Count, discard: r.Discard}
	for _, rr := range r.Rules {
		id, ok := o.lookupBlock(rr.Places)
		if !ok {
			return nil, "", fmt.Errorf("ore block %s %v missing from the block list", rr.Places.Name, rr.Places.States)
		}
		rule := oreRule{place: id}
		for _, m := range rr.MayReplace {
			rule.mayReplace = append(rule.mayReplace, upgradedName(m))
		}
		f.rules = append(f.rules, rule)
	}
	if f.count <= 0 || len(f.rules) == 0 {
		return nil, "", nil
	}
	return f, r.Description.Identifier, nil
}

// upgradedBlocks maps old block-with-state names in the vanilla data to the
// split blocks of 1.26, as BDS upgrades them on load.
func upgradedBlocks(name string) (string, bool) {
	switch name {
	case "minecraft:stone/granite":
		return "minecraft:granite", true
	case "minecraft:stone/diorite":
		return "minecraft:diorite", true
	case "minecraft:stone/andesite":
		return "minecraft:andesite", true
	case "minecraft:dirt/coarse":
		return "minecraft:coarse_dirt", true
	case "minecraft:tallgrass/tall", "minecraft:tallgrass/default":
		return blockShortGrass, true
	case "minecraft:tallgrass/fern":
		return blockFern, true
	}
	return "", false
}

func upgradedName(b biome.Block) string {
	for _, v := range b.States {
		if s, ok := v.(string); ok {
			if n, ok := upgradedBlocks(b.Name + "/" + s); ok {
				return n
			}
		}
	}
	return b.Name
}

// lookupBlock resolves a data block to a state, upgrading old names.
func (o *Overworld) lookupBlock(b biome.Block) (uint32, bool) {
	if id, ok := o.reg.Lookup(b.Name, b.States); ok {
		return id, true
	}
	if st, ok := upgradedStates(b.States); ok {
		if id, ok := o.reg.Lookup(b.Name, st); ok {
			return id, true
		}
	}
	if n := upgradedName(b); n != b.Name {
		return o.reg.Default(n)
	}
	return o.reg.Default(flattened(b))
}

// sinTable is the 65536-entry sine table BDS uses for angles.
var sinTable = feature.SinTable()

// place grows a vein of count spheres along a short line from (x+8, y, z+8)
// (feature.OreVein).
func (f *oreFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	reg := w.o.reg
	placed := false
	ok := feature.OreVein(f.count, x, y, z, r, func(bx, by, bz int32) {
		cur, ok := w.block(bx, by, bz)
		if ok && f.replace(w, reg, cur, bx, by, bz, r) {
			placed = true
		}
	})
	if !ok {
		return blockPos{}, false
	}
	return blockPos{x, y, z}, placed
}

// replace applies the first replace rule that matches the block.
func (f *oreFeature) replace(w *featureWorld, reg *blocks.Registry, cur uint32, x, y, z int32, r *rand.MT) bool {
	name := w.o.blockName(cur)
	for _, rule := range f.rules {
		matched := false
		for _, m := range rule.mayReplace {
			if m == name {
				matched = true
				break
			}
		}
		if !matched || cur == rule.place {
			continue
		}
		if c := f.discard; c > 0 {
			check := true
			if c < 1 {
				check = r.Float() < c
			}
			if check && w.airNextTo(x, y, z) {
				continue
			}
		}
		w.set(x, y, z, rule.place)
		return true
	}
	_ = reg
	return false
}

// airNextTo checks the six neighbours in BDS's order for air.
func (w *featureWorld) airNextTo(x, y, z int32) bool {
	air := w.o.ids.air
	for _, d := range [6][3]int32{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}} {
		if b, ok := w.block(x+d[0], y+d[1], z+d[2]); ok && b == air {
			return true
		}
	}
	return false
}

// legacyDirections names the old integer direction state (0 south, 1 west,
// 2 north, 3 east) as minecraft:cardinal_direction, which replaced it.
func legacyDirections() [4]string {
	return [4]string{"south", "west", "north", "east"}
}

// upgradedStates rewrites old states data files still use (the integer
// direction of dripleaves) to their current form.
func upgradedStates(st map[string]any) (map[string]any, bool) {
	d, ok := st["direction"].(float64)
	if !ok || d < 0 || d > 3 {
		return nil, false
	}
	out := make(map[string]any, len(st))
	for k, v := range st {
		out[k] = v
	}
	delete(out, "direction")
	out[stateCardinalDirection] = legacyDirections()[int(d)]
	return out, true
}
