package biome

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"strings"
)

// Def is the part of a vanilla biome definition (behavior pack
// biomes/*.biome.json) that world generation reads.
type Def struct {
	// Name has no "minecraft:" prefix.
	Name        string
	Temperature float32
	Surface     *Surface
	// Subsurface is the second builder some cave biomes have (sulfur_caves).
	Subsurface  *Surface
	Adjustments []Adjustment
	Steep       *Steep
	// PartiallyFrozen marks the frozen oceans, where noise patches keep the
	// sea open.
	PartiallyFrozen bool
	// Tags are the biome's minecraft:tags; feature rules pick biomes by tag.
	Tags []string
	// Village is the minecraft:village_type type ("default", "desert", ...),
	// empty when villages do not generate in the biome.
	Village string
}

// Surface is a minecraft:surface_builder. Which fields are set depends on
// Type: "minecraft:overworld", "minecraft:swamp", "minecraft:frozen_ocean",
// "minecraft:mesa", "minecraft:noise_gradient", "minecraft:capped" or
// "minecraft:the_end".
type Surface struct {
	Type                                string
	Top, Mid, SeaFloor, Foundation, Sea Block
	SeaFloorDepth                       int
	MaxPuddleDepth                      int
	ClayMaterial, HardClayMaterial      Block
	BrycePillars, HasForest             bool
	NoiseName                           string
	FirstOctave                         int
	Amplitudes                          []float32
	NonReplaceable                      []Block
	Specifiers                          []Specifier
	// FloorMaterials, CeilingMaterials and BeachMaterial are the capped
	// (Nether) builder's.
	FloorMaterials, CeilingMaterials []Block
	BeachMaterial                    Block
}

// Specifier picks a block for a noise_gradient builder when the noise is in
// (Min, Max].
type Specifier struct {
	Min, Max float32
	Block    Block
}

// Adjustment is one entry of minecraft:surface_material_adjustments: when the
// adjustment noise at a column is in [Min, Max), the non-empty materials
// replace the builder's. Height ranges are Molang and only used by Nether
// biomes; HeightMin/HeightMax are empty for the full range.
type Adjustment struct {
	FreqScale                           float32
	Min, Max                            float32
	HeightMin, HeightMax                string
	Top, Mid, SeaFloor, Foundation, Sea Block
}

// Steep is minecraft:mountain_parameters' steep_material_adjustment: slopes
// facing the flagged directions get Material as top and mid.
type Steep struct {
	Material                 Block
	North, South, West, East bool
}

// Block is a block name with optional states. In the JSON it is either a
// plain name or {"name": ..., "states": {...}}.
type Block struct {
	Name   string
	States map[string]any
}

// UnmarshalJSON reads either form.
func (b *Block) UnmarshalJSON(data []byte) error {
	if err := json.Unmarshal(data, &b.Name); err == nil {
		return nil
	}
	var v struct {
		Name   string         `json:"name"`
		States map[string]any `json:"states"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return fmt.Errorf("biome: block: %w", err)
	}
	b.Name, b.States = v.Name, v.States
	return nil
}

type rawMaterials struct {
	Top        Block `json:"top_material"`
	Mid        Block `json:"mid_material"`
	SeaFloor   Block `json:"sea_floor_material"`
	Foundation Block `json:"foundation_material"`
	Sea        Block `json:"sea_material"`
}

type rawBuilder struct {
	Type string `json:"type"`
	rawMaterials
	SeaFloorDepth  int   `json:"sea_floor_depth"`
	MaxPuddleDepth int   `json:"max_puddle_depth_below_sea_level"`
	ClayMaterial   Block `json:"clay_material"`
	HardClay       Block `json:"hard_clay_material"`
	BrycePillars   bool  `json:"bryce_pillars"`
	HasForest      bool  `json:"has_forest"`
	Noise          struct {
		Name        string    `json:"name"`
		FirstOctave int       `json:"first_octave"`
		Amplitudes  []float32 `json:"amplitudes"`
	} `json:"noise_descriptor"`
	NonReplaceable []Block `json:"non_replaceable_blocks"`
	FloorMaterials []Block `json:"floor_materials"`
	CeilMaterials  []Block `json:"ceiling_materials"`
	Beach          Block   `json:"beach_material"`
	Specifiers     []struct {
		Range struct {
			Min float32 `json:"min"`
			Max float32 `json:"max"`
		} `json:"range"`
		Block Block `json:"block"`
	} `json:"noise_block_specifiers"`
}

type rawBiome struct {
	Biome struct {
		Description struct {
			Identifier string `json:"identifier"`
		} `json:"description"`
		Components struct {
			Climate *struct {
				Temperature float32 `json:"temperature"`
			} `json:"minecraft:climate"`
			Surface *struct {
				Builder rawBuilder `json:"builder"`
			} `json:"minecraft:surface_builder"`
			Subsurface *struct {
				Builder rawBuilder `json:"builder"`
			} `json:"minecraft:subsurface_builder"`
			Adjustments *struct {
				Adjustments []struct {
					Materials   rawMaterials `json:"materials"`
					FreqScale   *float32     `json:"noise_frequency_scale"`
					NoiseRange  []float32    `json:"noise_range"`
					HeightRange []any        `json:"height_range"`
				} `json:"adjustments"`
			} `json:"minecraft:surface_material_adjustments"`
			Mountain *struct {
				Steep *struct {
					Material Block `json:"material"`
					North    bool  `json:"north_slopes"`
					South    bool  `json:"south_slopes"`
					West     bool  `json:"west_slopes"`
					East     bool  `json:"east_slopes"`
				} `json:"steep_material_adjustment"`
			} `json:"minecraft:mountain_parameters"`
			PartiallyFrozen *struct{} `json:"minecraft:partially_frozen"`
			Village         *struct {
				Type string `json:"type"`
			} `json:"minecraft:village_type"`
			Tags *struct {
				Tags []string `json:"tags"`
			} `json:"minecraft:tags"`
		} `json:"components"`
	} `json:"minecraft:biome"`
}

func (b rawBuilder) surface() *Surface {
	s := &Surface{
		Type: b.Type, Top: b.Top, Mid: b.Mid, SeaFloor: b.SeaFloor, Foundation: b.Foundation, Sea: b.Sea,
		SeaFloorDepth: b.SeaFloorDepth, MaxPuddleDepth: b.MaxPuddleDepth,
		ClayMaterial: b.ClayMaterial, HardClayMaterial: b.HardClay,
		BrycePillars: b.BrycePillars, HasForest: b.HasForest,
		NoiseName: b.Noise.Name, FirstOctave: b.Noise.FirstOctave, Amplitudes: b.Noise.Amplitudes,
		NonReplaceable: b.NonReplaceable,
		FloorMaterials: b.FloorMaterials, CeilingMaterials: b.CeilMaterials, BeachMaterial: b.Beach,
	}
	for _, sp := range b.Specifiers {
		s.Specifiers = append(s.Specifiers, Specifier{Min: sp.Range.Min, Max: sp.Range.Max, Block: sp.Block})
	}
	return s
}

func molang(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return fmt.Sprint(t)
	}
	return ""
}

// LoadDefs reads every *.biome.json under dir in fsys. The result is keyed
// by name without the "minecraft:" prefix.
func LoadDefs(fsys fs.FS, dir string) (map[string]*Def, error) {
	files, err := fs.Glob(fsys, path.Join(dir, "*.biome.json"))
	if err != nil {
		return nil, fmt.Errorf("biome: list definitions: %w", err)
	}
	defs := make(map[string]*Def, len(files))
	for _, f := range files {
		data, err := fs.ReadFile(fsys, f)
		if err != nil {
			return nil, fmt.Errorf("biome: read %s: %w", f, err)
		}
		var raw rawBiome
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, fmt.Errorf("biome: decode %s: %w", f, err)
		}
		name := strings.TrimPrefix(raw.Biome.Description.Identifier, "minecraft:")
		if name == "" {
			name = strings.TrimSuffix(path.Base(f), ".biome.json")
		}
		c := raw.Biome.Components
		d := &Def{Name: name, PartiallyFrozen: c.PartiallyFrozen != nil}
		if c.Village != nil {
			d.Village = c.Village.Type
		}
		if c.Tags != nil {
			d.Tags = c.Tags.Tags
		}
		if c.Climate != nil {
			d.Temperature = c.Climate.Temperature
		}
		if c.Surface != nil {
			d.Surface = c.Surface.Builder.surface()
		}
		if c.Subsurface != nil {
			d.Subsurface = c.Subsurface.Builder.surface()
		}
		if c.Adjustments != nil {
			for _, a := range c.Adjustments.Adjustments {
				adj := Adjustment{
					FreqScale: 1, Top: a.Materials.Top, Mid: a.Materials.Mid, SeaFloor: a.Materials.SeaFloor,
					Foundation: a.Materials.Foundation, Sea: a.Materials.Sea,
				}
				if a.FreqScale != nil {
					adj.FreqScale = *a.FreqScale
				}
				if len(a.NoiseRange) == 2 {
					adj.Min, adj.Max = a.NoiseRange[0], a.NoiseRange[1]
				}
				if len(a.HeightRange) == 2 {
					adj.HeightMin, adj.HeightMax = molang(a.HeightRange[0]), molang(a.HeightRange[1])
				}
				d.Adjustments = append(d.Adjustments, adj)
			}
		}
		if c.Mountain != nil && c.Mountain.Steep != nil {
			s := c.Mountain.Steep
			d.Steep = &Steep{Material: s.Material, North: s.North, South: s.South, West: s.West, East: s.East}
		}
		defs[name] = d
	}
	return defs, nil
}
