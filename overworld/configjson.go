package overworld

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"slices"
)

// The JSON form of a Config. Every field is optional; see ParseConfig.
type jsonConfig struct {
	Packs      []string        `json:"packs,omitempty"`
	Features   *jsonFeatures   `json:"features,omitempty"`
	Biomes     *jsonBiomes     `json:"biomes,omitempty"`
	Structures *jsonStructures `json:"structures,omitempty"`
	Skip       *jsonSkip       `json:"skip,omitempty"`
	Terrain    *jsonTerrain    `json:"terrain,omitempty"`
}

type jsonFeatures struct {
	Disable         []string           `json:"disable,omitempty"`
	DisableIn       []jsonDisableIn    `json:"disable_in,omitempty"`
	DisableFeatures []string           `json:"disable_features,omitempty"`
	Chance          map[string]float32 `json:"chance,omitempty"`
	Iterations      map[string]int32   `json:"iterations,omitempty"`
	Replace         map[string]string  `json:"replace,omitempty"`
	Weights         []jsonWeight       `json:"weights,omitempty"`
	Rules           []jsonRule         `json:"rules,omitempty"`
}

type jsonDisableIn struct {
	Biomes []string `json:"biomes"`
	Rules  []string `json:"rules"`
}

type jsonWeight struct {
	Mix     string  `json:"mix"`
	Feature string  `json:"feature"`
	Weight  float32 `json:"weight"`
}

type jsonRule struct {
	ID         string    `json:"id"`
	Places     string    `json:"places"`
	Pass       string    `json:"pass"`
	Biomes     []string  `json:"biomes,omitempty"`
	Tags       []string  `json:"tags,omitempty"`
	Iterations int32     `json:"iterations,omitempty"`
	Percent    float32   `json:"percent,omitempty"`
	X          *jsonAxis `json:"x,omitempty"`
	Y          *jsonAxis `json:"y,omitempty"`
	Z          *jsonAxis `json:"z,omitempty"`
}

// jsonAxis is "heightmap", "above_top_solid", a number, or [min, max) as
// a two-number list.
type jsonAxis struct{ a Axis }

func (j *jsonAxis) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		switch s {
		case "heightmap":
			j.a = Heightmap()
		case "above_top_solid":
			j.a = AboveTopSolid()
		default:
			return fmt.Errorf("axis %q is not heightmap or above_top_solid", s)
		}
		return nil
	}
	var n int32
	if json.Unmarshal(b, &n) == nil {
		j.a = Fixed(n)
		return nil
	}
	var r [2]int32
	if err := json.Unmarshal(b, &r); err != nil {
		return fmt.Errorf("axis %s is not a name, a number or [min, max]", b)
	}
	j.a = Uniform(r[0], r[1])
	return nil
}

func (j jsonAxis) MarshalJSON() ([]byte, error) {
	switch j.a.a.kind {
	case distHeightmap:
		return json.Marshal("heightmap")
	case distAboveTopSolid:
		return json.Marshal("above_top_solid")
	case distConstant:
		return json.Marshal(j.a.a.min)
	}
	return json.Marshal([2]int32{j.a.a.min, j.a.a.max})
}

type jsonBiomes struct {
	Remove  []string            `json:"remove,omitempty"`
	Replace map[string]string   `json:"replace,omitempty"`
	Only    string              `json:"only,omitempty"`
	Scale   map[string]float32  `json:"scale,omitempty"`
	Tags    map[string][]string `json:"tags,omitempty"`
	Add     []jsonBiome         `json:"add,omitempty"`
}

type jsonBiome struct {
	Name    string      `json:"name"`
	Like    string      `json:"like"`
	Tags    []string    `json:"tags,omitempty"`
	Climate jsonClimate `json:"climate"`
}

type jsonClimate struct {
	Temperature     *[2]float32 `json:"temperature,omitempty"`
	Humidity        *[2]float32 `json:"humidity,omitempty"`
	Continentalness *[2]float32 `json:"continentalness,omitempty"`
	Erosion         *[2]float32 `json:"erosion,omitempty"`
	Depth           *[2]float32 `json:"depth,omitempty"`
	Weirdness       *[2]float32 `json:"weirdness,omitempty"`
	Offset          float32     `json:"offset,omitempty"`
}

type jsonStructures struct {
	Disable   []StructureKind             `json:"disable,omitempty"`
	Exclude   []jsonExclude               `json:"exclude,omitempty"`
	Placement map[StructureKind]Placement `json:"placement,omitempty"`
	PlaceAt   []jsonPlaceAt               `json:"place_at,omitempty"`
	Biomes    map[StructureKind][]string  `json:"biomes,omitempty"`
	Templates map[string]string           `json:"templates,omitempty"`
	Pools     []jsonPool                  `json:"pools,omitempty"`
	Loot      map[string]string           `json:"loot,omitempty"`
	Blocks    []jsonBlockSwap             `json:"blocks,omitempty"`
}

type jsonExclude struct {
	Area  [4]int32        `json:"area"`
	Kinds []StructureKind `json:"kinds,omitempty"`
}

type jsonPlaceAt struct {
	Kind  StructureKind `json:"kind"`
	Chunk [2]int32      `json:"chunk"`
}

type jsonPool struct {
	Pool    string `json:"pool"`
	Element string `json:"element"`
	Weight  int32  `json:"weight"`
	Add     bool   `json:"add,omitempty"`
}

type jsonBlockSwap struct {
	Kind StructureKind `json:"kind"`
	From string        `json:"from"`
	To   string        `json:"to"`
}

type jsonSkip struct {
	Caves        bool `json:"caves,omitempty"`
	Canyons      bool `json:"canyons,omitempty"`
	BedrockFloor bool `json:"bedrock_floor,omitempty"`
	Surface      bool `json:"surface,omitempty"`
	Structures   bool `json:"structures,omitempty"`
	Decoration   bool `json:"decoration,omitempty"`
	Freeze       bool `json:"freeze,omitempty"`
}

type jsonTerrain struct {
	SeaLevel    int32  `json:"sea_level,omitempty"`
	SeaBlock    string `json:"sea_block,omitempty"`
	NoDeepslate bool   `json:"no_deepslate,omitempty"`
	NoOreVeins  bool   `json:"no_ore_veins,omitempty"`
}

// passNames are the JSON names of the passes, in Pass order.
func passNames() [passCount]string { return passOrder() }

// ParseConfig reads a Config from JSON. Unknown fields are errors, so a
// typo does not silently do nothing. Packs are folder paths inside root
// (which may be nil when there are none). Go features, rules placing them,
// structure filters and the logger cannot be written in JSON; add them to
// the returned Config. The format:
//
//	{
//	  "packs": ["my_addon"],
//	  "features": {
//	    "disable": ["birch_forest_surface_trees_feature"],
//	    "disable_in": [{"biomes": ["plains"], "rules": ["*flower*"]}],
//	    "disable_features": ["*birch*"],
//	    "chance": {"plains_surface_trees_feature": 50},
//	    "iterations": {"plains_surface_trees_feature": 3},
//	    "replace": {"forest_surface_trees_feature": "minecraft:oak_tree_feature"},
//	    "weights": [{"mix": "random_roofed_forest_feature", "feature": "minecraft:huge_mushroom_feature", "weight": 0}],
//	    "rules": [{"id": "myserver:oaks", "places": "minecraft:oak_tree_feature", "pass": "surface_pass",
//	               "tags": ["plains"], "iterations": 1, "percent": 10, "x": [0, 16], "y": "heightmap", "z": [0, 16]}]
//	  },
//	  "biomes": {
//	    "remove": ["mushroom_island"], "replace": {"roofed_forest": "forest"}, "only": "",
//	    "scale": {"cherry_grove": 1.5}, "tags": {"plains": ["overworld", "plains"]},
//	    "add": [{"name": "myserver:crystal_plains", "like": "plains", "climate": {"temperature": [0.2, 0.55]}}]
//	  },
//	  "structures": {
//	    "disable": ["minecraft:mansion"],
//	    "exclude": [{"area": [-500, -500, 500, 500], "kinds": ["minecraft:village"]}],
//	    "placement": {"minecraft:village": {"spacing": 20, "separation": 6}},
//	    "place_at": [{"kind": "minecraft:village", "chunk": [2, 3]}],
//	    "biomes": {"minecraft:mansion": ["plains"]},
//	    "templates": {"village/plains/houses/plains_small_house_1": "my/house"},
//	    "pools": [{"pool": "minecraft:village/plains/houses", "element": "my/house", "weight": 3, "add": true}],
//	    "loot": {"loot_tables/chests/village/village_plains_house.json": "loot_tables/my/house.json"},
//	    "blocks": [{"kind": "minecraft:village", "from": "cobblestone", "to": "mossy_cobblestone"}]
//	  },
//	  "skip": {"caves": true},
//	  "terrain": {"sea_level": 70, "sea_block": "lava", "no_deepslate": true, "no_ore_veins": true}
//	}
//
// Maps are applied in key order.
func ParseConfig(data []byte, root fs.FS) (*Config, error) {
	var j jsonConfig
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&j); err != nil {
		return nil, fmt.Errorf("worldgen: config: %w", err)
	}
	c := Vanilla()
	for _, p := range j.Packs {
		if root == nil {
			return nil, fmt.Errorf("worldgen: config: pack %q but no root folder to find it in", p)
		}
		sub, err := fs.Sub(root, p)
		if err != nil {
			return nil, fmt.Errorf("worldgen: config: pack %q: %w", p, err)
		}
		c.AddPack(sub)
	}
	c.packPaths = slices.Clone(j.Packs)
	if f := j.Features; f != nil {
		c.Features.Disable(f.Disable...)
		for _, d := range f.DisableIn {
			c.Features.DisableIn(d.Biomes, d.Rules...)
		}
		c.Features.DisableFeature(f.DisableFeatures...)
		for _, k := range slices.Sorted(maps.Keys(f.Chance)) {
			c.Features.SetChance(k, f.Chance[k])
		}
		for _, k := range slices.Sorted(maps.Keys(f.Iterations)) {
			c.Features.SetIterations(k, f.Iterations[k])
		}
		for _, k := range slices.Sorted(maps.Keys(f.Replace)) {
			c.Features.Replace(k, f.Replace[k])
		}
		for _, w := range f.Weights {
			c.Features.SetWeight(w.Mix, w.Feature, w.Weight)
		}
		names := passNames()
		for _, r := range f.Rules {
			pass := slices.Index(names[:], r.Pass)
			if pass < 0 {
				return nil, fmt.Errorf("worldgen: config: rule %q: pass %q is not one of %v", r.ID, r.Pass, names)
			}
			rule := Rule{ID: r.ID, Places: r.Places, Pass: Pass(pass), Biomes: r.Biomes, Tags: r.Tags,
				Iterations: r.Iterations, Percent: r.Percent}
			if r.X != nil {
				rule.X = r.X.a
			}
			if r.Y != nil {
				rule.Y = r.Y.a
			}
			if r.Z != nil {
				rule.Z = r.Z.a
			}
			c.Features.AddRule(rule)
		}
	}
	if b := j.Biomes; b != nil {
		for _, a := range b.Add {
			c.Biomes.Add(Biome{Name: a.Name, Like: a.Like, Tags: a.Tags, Climate: a.Climate.climate()})
		}
		c.Biomes.Remove(b.Remove...)
		for _, k := range slices.Sorted(maps.Keys(b.Replace)) {
			c.Biomes.Replace(k, b.Replace[k])
		}
		if b.Only != "" {
			c.Biomes.Only(b.Only)
		}
		for _, k := range slices.Sorted(maps.Keys(b.Scale)) {
			c.Biomes.Scale(k, b.Scale[k])
		}
		for _, k := range slices.Sorted(maps.Keys(b.Tags)) {
			c.Biomes.SetTags(k, b.Tags[k]...)
		}
	}
	if s := j.Structures; s != nil {
		c.Structures.Disable(s.Disable...)
		for _, e := range s.Exclude {
			c.Structures.Exclude(Box(e.Area[0], e.Area[1], e.Area[2], e.Area[3]), e.Kinds...)
		}
		for _, k := range slices.Sorted(maps.Keys(s.Placement)) {
			c.Structures.SetPlacement(k, s.Placement[k])
		}
		for _, p := range s.PlaceAt {
			c.Structures.PlaceAt(p.Kind, p.Chunk[0], p.Chunk[1])
		}
		for _, k := range slices.Sorted(maps.Keys(s.Biomes)) {
			c.Structures.SetBiomes(k, s.Biomes[k]...)
		}
		for _, k := range slices.Sorted(maps.Keys(s.Templates)) {
			c.Structures.ReplaceTemplate(k, s.Templates[k])
		}
		for _, p := range s.Pools {
			if p.Add {
				c.Structures.AddToPool(p.Pool, p.Element, p.Weight)
			} else {
				c.Structures.SetPoolWeight(p.Pool, p.Element, p.Weight)
			}
		}
		for _, k := range slices.Sorted(maps.Keys(s.Loot)) {
			c.Structures.ReplaceLoot(k, s.Loot[k])
		}
		for _, b := range s.Blocks {
			c.Structures.ReplaceBlock(b.Kind, b.From, b.To)
		}
	}
	if s := j.Skip; s != nil {
		c.Skip = Steps{Caves: s.Caves, Canyons: s.Canyons, BedrockFloor: s.BedrockFloor, Surface: s.Surface,
			Structures: s.Structures, Decoration: s.Decoration, Freeze: s.Freeze}
	}
	if t := j.Terrain; t != nil {
		c.Terrain = Terrain{SeaLevel: t.SeaLevel, SeaBlock: t.SeaBlock, NoDeepslate: t.NoDeepslate, NoOreVeins: t.NoOreVeins}
	}
	return c, nil
}

func (j jsonClimate) climate() Climate {
	return Climate{Temperature: jsonRange(j.Temperature), Humidity: jsonRange(j.Humidity),
		Continentalness: jsonRange(j.Continentalness), Erosion: jsonRange(j.Erosion), Depth: jsonRange(j.Depth),
		Weirdness: jsonRange(j.Weirdness), Offset: j.Offset}
}

// jsonRange is a JSON [min, max] as a Range; absent is the whole axis.
func jsonRange(p *[2]float32) Range {
	if p == nil {
		return Range{}
	}
	return Range{Min: p[0], Max: p[1]}
}

// rangeJSON is a Range as JSON [min, max]; the whole axis is absent.
func rangeJSON(r Range) *[2]float32 {
	if r == (Range{}) {
		return nil
	}
	return &[2]float32{r.Min, r.Max}
}

// MarshalJSON writes the Config in ParseConfig's format, so a Config built
// in Go can be saved and loaded back. It fails for what JSON cannot hold:
// Go features, structure filters, and packs added with AddPack rather than
// read from a config file.
func (c *Config) MarshalJSON() ([]byte, error) {
	if len(c.Features.registered) > 0 {
		return nil, fmt.Errorf("worldgen: config: Go features cannot be written as JSON")
	}
	if len(c.Structures.filters) > 0 {
		return nil, fmt.Errorf("worldgen: config: structure filters cannot be written as JSON")
	}
	if len(c.packs) != len(c.packPaths) {
		return nil, fmt.Errorf("worldgen: config: packs added with AddPack have no path to write")
	}
	j := jsonConfig{Packs: c.packPaths}
	f := &jsonFeatures{}
	for _, op := range c.Features.ops {
		switch op.kind {
		case opDisableRule:
			f.Disable = append(f.Disable, op.pattern)
		case opDisableRuleIn:
			f.DisableIn = append(f.DisableIn, jsonDisableIn{Biomes: op.biomes, Rules: []string{op.pattern}})
		case opDisableFeature:
			f.DisableFeatures = append(f.DisableFeatures, op.pattern)
		case opSetChance:
			if f.Chance == nil {
				f.Chance = map[string]float32{}
			}
			f.Chance[op.pattern] = op.value
		case opSetIterations:
			if f.Iterations == nil {
				f.Iterations = map[string]int32{}
			}
			f.Iterations[op.pattern] = int32(op.value)
		case opReplace:
			if f.Replace == nil {
				f.Replace = map[string]string{}
			}
			f.Replace[op.pattern] = op.feature
		case opSetWeight:
			f.Weights = append(f.Weights, jsonWeight{Mix: op.pattern, Feature: op.feature, Weight: op.value})
		}
	}
	names := passNames()
	for _, r := range c.Features.rules {
		jr := jsonRule{ID: r.ID, Places: r.Places, Biomes: r.Biomes, Tags: r.Tags, Iterations: r.Iterations, Percent: r.Percent}
		if r.Pass >= FirstPass && int(r.Pass) < len(names) {
			jr.Pass = names[r.Pass]
		}
		if r.X.set {
			jr.X = &jsonAxis{r.X}
		}
		if r.Y.set {
			jr.Y = &jsonAxis{r.Y}
		}
		if r.Z.set {
			jr.Z = &jsonAxis{r.Z}
		}
		f.Rules = append(f.Rules, jr)
	}
	if !emptyJSON(f) {
		j.Features = f
	}
	b := &jsonBiomes{}
	for _, op := range c.Biomes.ops {
		switch op.kind {
		case opRemoveBiome:
			b.Remove = append(b.Remove, op.name)
		case opReplaceBiome:
			if op.name == "*" {
				b.Only = op.to
				continue
			}
			if b.Replace == nil {
				b.Replace = map[string]string{}
			}
			b.Replace[op.name] = op.to
		case opScaleBiome:
			if b.Scale == nil {
				b.Scale = map[string]float32{}
			}
			b.Scale[op.name] = op.factor
		case opSetTags:
			if b.Tags == nil {
				b.Tags = map[string][]string{}
			}
			b.Tags[op.name] = op.tags
		}
	}
	for _, a := range c.Biomes.added {
		cl := a.Climate
		b.Add = append(b.Add, jsonBiome{Name: a.Name, Like: a.Like, Tags: a.Tags, Climate: jsonClimate{
			Temperature: rangeJSON(cl.Temperature), Humidity: rangeJSON(cl.Humidity),
			Continentalness: rangeJSON(cl.Continentalness), Erosion: rangeJSON(cl.Erosion), Depth: rangeJSON(cl.Depth),
			Weirdness: rangeJSON(cl.Weirdness), Offset: cl.Offset}})
	}
	if !emptyJSON(b) {
		j.Biomes = b
	}
	sc := c.Structures
	s := &jsonStructures{Disable: sc.disabled, Templates: sc.templates, Loot: sc.loot}
	for _, e := range sc.excludes {
		s.Exclude = append(s.Exclude, jsonExclude{Area: [4]int32{e.area.MinX, e.area.MinZ, e.area.MaxX, e.area.MaxZ}, Kinds: e.kinds})
	}
	for _, p := range sc.placements {
		if s.Placement == nil {
			s.Placement = map[StructureKind]Placement{}
		}
		s.Placement[p.kind] = p.p
	}
	for _, f := range sc.forced {
		s.PlaceAt = append(s.PlaceAt, jsonPlaceAt{Kind: f.kind, Chunk: [2]int32{f.cx, f.cz}})
	}
	for _, kb := range sc.biomes {
		if s.Biomes == nil {
			s.Biomes = map[StructureKind][]string{}
		}
		s.Biomes[kb.kind] = kb.biomes
	}
	for _, p := range sc.pools {
		s.Pools = append(s.Pools, jsonPool{Pool: p.pool, Element: p.element, Weight: p.weight, Add: p.add})
	}
	for _, w := range sc.swaps {
		s.Blocks = append(s.Blocks, jsonBlockSwap{Kind: w.kind, From: w.from, To: w.to})
	}
	if !emptyJSON(s) {
		j.Structures = s
	}
	if c.Skip != (Steps{}) {
		k := c.Skip
		j.Skip = &jsonSkip{Caves: k.Caves, Canyons: k.Canyons, BedrockFloor: k.BedrockFloor, Surface: k.Surface,
			Structures: k.Structures, Decoration: k.Decoration, Freeze: k.Freeze}
	}
	if c.Terrain != (Terrain{}) {
		t := c.Terrain
		j.Terrain = &jsonTerrain{SeaLevel: t.SeaLevel, SeaBlock: t.SeaBlock, NoDeepslate: t.NoDeepslate, NoOreVeins: t.NoOreVeins}
	}
	return json.MarshalIndent(j, "", "  ")
}

// emptyJSON reports whether v marshals to an empty JSON object.
func emptyJSON(v any) bool {
	b, err := json.Marshal(v)
	return err == nil && string(b) == "{}"
}
