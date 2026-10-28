package overworld

import (
	"encoding/json"
	"io/fs"
	"path"
	"strings"
	"testing"

	worldgen "github.com/helix-bedrock/worldgen/1.26.60"
)

// TestDrop4Loads checks that every feature and feature rule of the Drop 4
// of 2026 experiment is built: vanilla features of a type the generator
// does not know are left out without an error.
func TestDrop4Loads(t *testing.T) {
	reg, ids := testRegistry(t)
	o, err := NewOverworld(1, reg, ids, WithConfig(&Config{Experiments: Experiments{Drop4Of2026: true}}))
	if err != nil {
		t.Fatal(err)
	}
	root := path.Join("experiments", worldgen.ExperimentDrop4Of2026)
	for _, dir := range []string{"features", "feature_rules"} {
		files, err := fs.Glob(worldgen.Data, path.Join(root, dir, "*.json"))
		if err != nil || len(files) == 0 {
			t.Fatalf("%s: %d files, %v", dir, len(files), err)
		}
		for _, f := range files {
			raw, err := fs.ReadFile(worldgen.Data, f)
			if err != nil {
				t.Fatal(err)
			}
			var top map[string]json.RawMessage
			if err := json.Unmarshal(raw, &top); err != nil {
				t.Fatal(err)
			}
			for typ, body := range top {
				if !strings.HasPrefix(typ, "minecraft:") {
					continue
				}
				var v struct {
					Description struct {
						Identifier string `json:"identifier"`
					} `json:"description"`
				}
				if err := json.Unmarshal(body, &v); err != nil {
					t.Fatal(err)
				}
				id := v.Description.Identifier
				if dir == "features" {
					if _, ok := o.features.get(id); !ok {
						t.Errorf("feature %s (%s) not built", id, typ)
					}
				} else if _, ok := o.rules.byID[id]; !ok {
					t.Errorf("feature rule %s not built", id)
				}
			}
		}
	}
}

// TestIceCaves finds an ice cave with the experiment on and checks that the
// chunks around it have packed ice, calcite and attached icicles after
// decoration, and that the same
// place is no ice cave without the experiment.
func TestIceCaves(t *testing.T) {
	if testing.Short() {
		t.Skip("searches for an ice cave")
	}
	reg, ids := testRegistry(t)
	const seed = 12345
	o, err := NewOverworld(seed, reg, ids, WithConfig(&Config{Experiments: Experiments{Drop4Of2026: true}}))
	if err != nil {
		t.Fatal(err)
	}
	ice := ids["ice_caves"]
	var cx, cz int32
	found := false
	for r := int32(0); r < 12000 && !found; r += 48 {
		for _, p := range [][2]int32{{r, 0}, {-r, 0}, {0, r}, {0, -r}, {r, r}, {-r, -r}, {r, -r}, {-r, r}} {
			if o.CaveBiomeAt(p[0], p[1]) == ice {
				cx, cz, found = p[0]>>4, p[1]>>4, true
				break
			}
		}
	}
	if !found {
		t.Fatal("no ice caves within 12000 blocks")
	}
	cache := map[[2]int32]*PackedChunk{}
	neighbour := func(x, z int32) *PackedChunk {
		if p, ok := cache[[2]int32{x, z}]; ok {
			return p
		}
		var t Chunk
		o.GenerateChunk(x, z, &t)
		p := Pack(&t)
		cache[[2]int32{x, z}] = p
		return p
	}
	// One chunk may have no icicle; the 3 by 3 chunks around it have.
	count := map[string]int{}
	attached := 0
	for x := cx - 1; x <= cx+1; x++ {
		for z := cz - 1; z <= cz+1; z++ {
			var c Chunk
			o.GenerateChunk(x, z, &c)
			o.Decorate(x, z, &c, neighbour)
			for _, b := range c.Blocks {
				s, ok := reg.State(uint32(b))
				if !ok {
					continue
				}
				count[s.Name]++
				if s.Name == blockIcicle && s.Props["attached_bit"] == uint8(1) {
					attached++
				}
			}
		}
	}
	t.Logf("chunks around %d %d: packed ice %d, calcite %d, icicle %d (attached %d), ice crystal %d", cx, cz,
		count["minecraft:packed_ice"], count["minecraft:calcite"], count[blockIcicle], attached, count["minecraft:ice_crystal"])
	for _, n := range []string{"minecraft:packed_ice", "minecraft:calcite", blockIcicle} {
		if count[n] == 0 {
			t.Errorf("no %s around an ice cave", n)
		}
	}
	if attached == 0 {
		t.Error("no icicle with attached_bit")
	}
	vanilla, err := NewOverworld(seed, reg, ids)
	if err != nil {
		t.Fatal(err)
	}
	if vanilla.CaveBiomeAt(cx<<4, cz<<4) == ice {
		t.Error("ice caves without the experiment")
	}
}
