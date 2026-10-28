package nether

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"

	worldgen "github.com/helix-bedrock/worldgen/1.26.60"
	"github.com/helix-bedrock/worldgen/1.26.60/biome"
	"github.com/helix-bedrock/worldgen/1.26.60/rand"
)

// plantScatter is a minecraft:scatter_feature whose feature is a
// minecraft:single_block_feature placing a Nether plant in air (fungi,
// roots, sprouts), as the overworld's scatter and single block features
// place them.
type plantScatter struct {
	iterations int32
	order      [3]int
	// axes are triangle extents around the input position.
	axes  [3][2]int32
	block uint32
}

// readFeature decodes the vanilla feature file id into v.
func readFeature(id string, v any) error {
	name := strings.TrimPrefix(id, "minecraft:")
	raw, err := worldgen.Data.ReadFile(path.Join("vanilla/features", name+".json"))
	if err != nil {
		return fmt.Errorf("worldgen: read feature %s: %w", id, err)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("worldgen: decode feature %s: %w", id, err)
	}
	return nil
}

// triangle is a feature axis with a distribution over an extent.
type triangle struct {
	Distribution string   `json:"distribution"`
	Extent       [2]int32 `json:"extent"`
}

// scatterFile is a minecraft:scatter_feature file.
type scatterFile struct {
	F struct {
		Iterations int32    `json:"iterations"`
		Order      string   `json:"coordinate_eval_order"`
		X          triangle `json:"x"`
		Y          triangle `json:"y"`
		Z          triangle `json:"z"`
		ToFloor    bool     `json:"project_input_to_floor"`
		Places     string   `json:"places_feature"`
	} `json:"minecraft:scatter_feature"`
}

// singleBlockFile is a minecraft:single_block_feature file.
type singleBlockFile struct {
	F struct {
		Places     biome.Block `json:"places_block"`
		MayReplace []string    `json:"may_replace"`
	} `json:"minecraft:single_block_feature"`
}

// loadPlantScatter reads a scatter feature and the single block feature it
// places from the vanilla data.
func (n *Nether) loadPlantScatter(id string) (*plantScatter, error) {
	var sc scatterFile
	if err := readFeature(id, &sc); err != nil {
		return nil, err
	}
	f := sc.F
	p := &plantScatter{iterations: f.Iterations}
	var ok bool
	if p.order, ok = coordinateOrders[f.Order]; !ok || f.ToFloor {
		return nil, fmt.Errorf("worldgen: feature %s: scatter form not supported", id)
	}
	for i, a := range []triangle{f.X, f.Y, f.Z} {
		if a.Distribution != "triangle" {
			return nil, fmt.Errorf("worldgen: feature %s: distribution %q not supported", id, a.Distribution)
		}
		p.axes[i] = a.Extent
	}
	var sb singleBlockFile
	if err := readFeature(f.Places, &sb); err != nil {
		return nil, err
	}
	if len(sb.F.MayReplace) != 1 || sb.F.MayReplace[0] != blockAir {
		return nil, fmt.Errorf("worldgen: feature %s: may_replace %v not supported", f.Places, sb.F.MayReplace)
	}
	if p.block, ok = n.reg.Lookup(sb.F.Places.Name, sb.F.Places.States); !ok {
		return nil, fmt.Errorf("worldgen: feature %s: block %s missing from the block list", f.Places, sb.F.Places.Name)
	}
	return p, nil
}

// place scatters the plant: iterations tries at triangle offsets, each one
// draw for the block choice (BDS draws it even for a single block), then
// the plant goes in air on a block it can stand on.
// VERIFY: the plants' placement rule is taken as the huge fungus' ground
// (nylium, soul soil or a dirt block), not read from BDS's blocks.
func (p *plantScatter) place(n *Nether, r *region, x, y, z int32, mt *rand.MT) {
	base := [3]int32{x, y, z}
	for range p.iterations {
		var pos [3]int32
		for _, a := range p.order {
			lo, hi := p.axes[a][0], p.axes[a][1]
			d := hi - lo
			pos[a] = base[a] + lo + mt.IntN(d/2+1) + mt.IntN(d-d/2+1)
		}
		mt.Uint32()
		if r.at(pos[0], pos[1], pos[2]) == n.ids.air && n.growsFungus(r.at(pos[0], pos[1]-1, pos[2])) {
			r.set(pos[0], pos[1], pos[2], p.block)
		}
	}
}
