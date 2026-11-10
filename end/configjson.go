package end

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"slices"

	"github.com/helix-bedrock/worldgen/structure"
)

// The JSON form of an End Config; see ParseConfig.
type jsonConfig struct {
	Packs      []string        `json:"packs,omitempty"`
	Structures *jsonStructures `json:"structures,omitempty"`
	Skip       *jsonSkip       `json:"skip,omitempty"`
}

type jsonStructures struct {
	Exclude   [][4]int32        `json:"exclude,omitempty"`
	PlaceAt   [][2]int32        `json:"place_at,omitempty"`
	Loot      map[string]string `json:"loot,omitempty"`
	Templates map[string]string `json:"templates,omitempty"`
	Blocks    [][2]string       `json:"blocks,omitempty"`
}

type jsonSkip struct {
	Pillars      bool `json:"pillars,omitempty"`
	SmallIslands bool `json:"small_islands,omitempty"`
	Chorus       bool `json:"chorus,omitempty"`
	Gateways     bool `json:"gateways,omitempty"`
	Cities       bool `json:"cities,omitempty"`
}

// ParseConfig reads an End Config from JSON; unknown fields are errors.
// Packs are folder paths inside root (nil when there are none). Structure
// filters and the logger cannot be written in JSON. The format:
//
//	{
//	  "packs": ["my_addon"],
//	  "structures": {
//	    "exclude": [[-2000, -2000, 2000, 2000]],
//	    "place_at": [[110, 3]],
//	    "loot": {"loot_tables/chests/end_city_treasure.json": "loot_tables/my/city.json"},
//	    "templates": {"endcity/ship": "my/ship"},
//	    "blocks": [["purpur_block", "end_stone_bricks"]]
//	  },
//	  "skip": {"pillars": false, "small_islands": false, "chorus": false, "gateways": false, "cities": false}
//	}
//
// Maps are applied in key order.
func ParseConfig(data []byte, root fs.FS) (*Config, error) {
	var j jsonConfig
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&j); err != nil {
		return nil, fmt.Errorf("worldgen: end config: %w", err)
	}
	c := Vanilla()
	for _, p := range j.Packs {
		if root == nil {
			return nil, fmt.Errorf("worldgen: end config: pack %q but no root folder to find it in", p)
		}
		sub, err := fs.Sub(root, p)
		if err != nil {
			return nil, fmt.Errorf("worldgen: end config: pack %q: %w", p, err)
		}
		c.AddPack(sub)
	}
	c.packPaths = slices.Clone(j.Packs)
	if s := j.Structures; s != nil {
		for _, a := range s.Exclude {
			c.Structures.Exclude(structure.NewArea(a[0], a[1], a[2], a[3]))
		}
		for _, p := range s.PlaceAt {
			c.Structures.PlaceAt(p[0], p[1])
		}
		for _, k := range slices.Sorted(maps.Keys(s.Loot)) {
			c.Structures.ReplaceLoot(k, s.Loot[k])
		}
		for _, k := range slices.Sorted(maps.Keys(s.Templates)) {
			c.Structures.ReplaceTemplate(k, s.Templates[k])
		}
		for _, b := range s.Blocks {
			c.Structures.ReplaceBlock(b[0], b[1])
		}
	}
	if k := j.Skip; k != nil {
		c.Skip = Steps{Pillars: k.Pillars, SmallIslands: k.SmallIslands, Chorus: k.Chorus, Gateways: k.Gateways, Cities: k.Cities}
	}
	return c, nil
}

// MarshalJSON writes the Config in ParseConfig's format. It fails for
// structure filters and packs added with AddPack.
func (c *Config) MarshalJSON() ([]byte, error) {
	if len(c.Structures.filters) > 0 {
		return nil, fmt.Errorf("worldgen: end config: structure filters cannot be written as JSON")
	}
	if len(c.packs) != len(c.packPaths) {
		return nil, fmt.Errorf("worldgen: end config: packs added with AddPack have no path to write")
	}
	j := jsonConfig{Packs: c.packPaths}
	sc := c.Structures
	s := &jsonStructures{PlaceAt: sc.forced, Loot: sc.loot, Templates: sc.templates, Blocks: sc.swaps}
	for _, a := range sc.excludes {
		s.Exclude = append(s.Exclude, [4]int32{a.MinX, a.MinZ, a.MaxX, a.MaxZ})
	}
	if b, err := json.Marshal(s); err == nil && string(b) != "{}" {
		j.Structures = s
	}
	if k := c.Skip; k != (Steps{}) {
		j.Skip = &jsonSkip{Pillars: k.Pillars, SmallIslands: k.SmallIslands, Chorus: k.Chorus, Gateways: k.Gateways, Cities: k.Cities}
	}
	return json.MarshalIndent(j, "", "  ")
}
