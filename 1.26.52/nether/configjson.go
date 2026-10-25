package nether

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"slices"

	"github.com/helix-bedrock/worldgen/1.26.52/structure"
)

// The JSON form of a Nether Config; see ParseConfig.
type jsonConfig struct {
	Packs      []string        `json:"packs,omitempty"`
	Features   *jsonFeatures   `json:"features,omitempty"`
	Biomes     *jsonBiomes     `json:"biomes,omitempty"`
	Structures *jsonStructures `json:"structures,omitempty"`
	Skip       *jsonSkip       `json:"skip,omitempty"`
}

type jsonFeatures struct {
	Disable   []string        `json:"disable,omitempty"`
	DisableIn []jsonDisableIn `json:"disable_in,omitempty"`
}

type jsonDisableIn struct {
	Biomes []string `json:"biomes"`
	Rules  []string `json:"rules"`
}

type jsonBiomes struct {
	Remove  []string          `json:"remove,omitempty"`
	Replace map[string]string `json:"replace,omitempty"`
	Only    string            `json:"only,omitempty"`
}

type jsonStructures struct {
	Disable   []StructureKind   `json:"disable,omitempty"`
	Exclude   []jsonExclude     `json:"exclude,omitempty"`
	PlaceAt   []jsonPlaceAt     `json:"place_at,omitempty"`
	Loot      map[string]string `json:"loot,omitempty"`
	Templates map[string]string `json:"templates,omitempty"`
	Pools     []jsonPool        `json:"pools,omitempty"`
	Blocks    []jsonBlockSwap   `json:"blocks,omitempty"`
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
	Surface    bool `json:"surface,omitempty"`
	Caves      bool `json:"caves,omitempty"`
	Structures bool `json:"structures,omitempty"`
	Decoration bool `json:"decoration,omitempty"`
}

// ParseConfig reads a Nether Config from JSON; unknown fields are errors.
// Packs are folder paths inside root (nil when there are none). Structure
// filters and the logger cannot be written in JSON. The format:
//
//	{
//	  "packs": ["my_addon"],
//	  "features": {"disable": ["*fungus*"], "disable_in": [{"biomes": ["hell"], "rules": ["*"]}]},
//	  "biomes": {"remove": ["basalt_deltas"], "replace": {"hell": "crimson_forest"}, "only": ""},
//	  "structures": {
//	    "disable": ["minecraft:bastion_remnant"],
//	    "exclude": [{"area": [-200, -200, 200, 200]}],
//	    "place_at": [{"kind": "minecraft:fortress", "chunk": [5, 7]}],
//	    "loot": {"loot_tables/chests/nether_bridge.json": "loot_tables/my/fortress.json"},
//	    "templates": {"bastion/bridge/starting_pieces/entrance": "my/entrance"},
//	    "pools": [{"pool": "minecraft:bastion/hoglin_stable/starting_pieces", "element": "minecraft:bastion_hoglin_stable_starting_pieces_starting_stairs_0", "weight": 0}],
//	    "blocks": [{"kind": "minecraft:fortress", "from": "nether_brick", "to": "red_nether_brick"}]
//	  },
//	  "skip": {"surface": false, "caves": false, "structures": false, "decoration": false}
//	}
//
// Maps are applied in key order.
func ParseConfig(data []byte, root fs.FS) (*Config, error) {
	var j jsonConfig
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&j); err != nil {
		return nil, fmt.Errorf("worldgen: nether config: %w", err)
	}
	c := Vanilla()
	for _, p := range j.Packs {
		if root == nil {
			return nil, fmt.Errorf("worldgen: nether config: pack %q but no root folder to find it in", p)
		}
		sub, err := fs.Sub(root, p)
		if err != nil {
			return nil, fmt.Errorf("worldgen: nether config: pack %q: %w", p, err)
		}
		c.AddPack(sub)
	}
	c.packPaths = slices.Clone(j.Packs)
	if f := j.Features; f != nil {
		c.Features.Disable(f.Disable...)
		for _, d := range f.DisableIn {
			c.Features.DisableIn(d.Biomes, d.Rules...)
		}
	}
	if b := j.Biomes; b != nil {
		c.Biomes.Remove(b.Remove...)
		for _, k := range slices.Sorted(maps.Keys(b.Replace)) {
			c.Biomes.Replace(k, b.Replace[k])
		}
		if b.Only != "" {
			c.Biomes.Only(b.Only)
		}
	}
	if s := j.Structures; s != nil {
		c.Structures.Disable(s.Disable...)
		for _, e := range s.Exclude {
			c.Structures.Exclude(structure.NewArea(e.Area[0], e.Area[1], e.Area[2], e.Area[3]), e.Kinds...)
		}
		for _, p := range s.PlaceAt {
			c.Structures.PlaceAt(p.Kind, p.Chunk[0], p.Chunk[1])
		}
		for _, k := range slices.Sorted(maps.Keys(s.Loot)) {
			c.Structures.ReplaceLoot(k, s.Loot[k])
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
		for _, b := range s.Blocks {
			c.Structures.ReplaceBlock(b.Kind, b.From, b.To)
		}
	}
	if k := j.Skip; k != nil {
		c.Skip = Steps{Surface: k.Surface, Caves: k.Caves, Structures: k.Structures, Decoration: k.Decoration}
	}
	return c, nil
}

// MarshalJSON writes the Config in ParseConfig's format. It fails for
// structure filters and packs added with AddPack.
func (c *Config) MarshalJSON() ([]byte, error) {
	if len(c.Structures.filters) > 0 {
		return nil, fmt.Errorf("worldgen: nether config: structure filters cannot be written as JSON")
	}
	if len(c.packs) != len(c.packPaths) {
		return nil, fmt.Errorf("worldgen: nether config: packs added with AddPack have no path to write")
	}
	j := jsonConfig{Packs: c.packPaths}
	if len(c.Features.ops) > 0 {
		j.Features = &jsonFeatures{}
		for _, op := range c.Features.ops {
			if op.biomes == nil {
				j.Features.Disable = append(j.Features.Disable, op.pattern)
			} else {
				j.Features.DisableIn = append(j.Features.DisableIn, jsonDisableIn{Biomes: op.biomes, Rules: []string{op.pattern}})
			}
		}
	}
	if len(c.Biomes.remove) > 0 || len(c.Biomes.replace) > 0 {
		j.Biomes = &jsonBiomes{Remove: c.Biomes.remove}
		for _, r := range c.Biomes.replace {
			if r[0] == "*" {
				j.Biomes.Only = r[1]
				continue
			}
			if j.Biomes.Replace == nil {
				j.Biomes.Replace = map[string]string{}
			}
			j.Biomes.Replace[r[0]] = r[1]
		}
	}
	sc := c.Structures
	s := &jsonStructures{Disable: sc.disabled, Loot: sc.loot, Templates: sc.templates}
	for _, e := range sc.excludes {
		s.Exclude = append(s.Exclude, jsonExclude{Area: [4]int32{e.area.MinX, e.area.MinZ, e.area.MaxX, e.area.MaxZ}, Kinds: e.kinds})
	}
	for _, f := range sc.forced {
		s.PlaceAt = append(s.PlaceAt, jsonPlaceAt{Kind: f.kind, Chunk: [2]int32{f.cx, f.cz}})
	}
	for _, p := range sc.pools {
		s.Pools = append(s.Pools, jsonPool{Pool: p.Pool, Element: p.Element, Weight: p.Weight, Add: p.Add})
	}
	for _, w := range sc.swaps {
		s.Blocks = append(s.Blocks, jsonBlockSwap{Kind: w.kind, From: w.from, To: w.to})
	}
	if b, err := json.Marshal(s); err == nil && string(b) != "{}" {
		j.Structures = s
	}
	if k := c.Skip; k != (Steps{}) {
		j.Skip = &jsonSkip{Surface: k.Surface, Caves: k.Caves, Structures: k.Structures, Decoration: k.Decoration}
	}
	return json.MarshalIndent(j, "", "  ")
}
