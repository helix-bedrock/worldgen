package nether

import (
	"strings"
	"testing"
)

const configSeed = 236621124415672384

func countIn(n *Nether, cx, cz int32, part string) int {
	var c Chunk
	NewCache(n, 64).Generate(cx, cz, &c)
	k := 0
	for _, b := range c.Blocks {
		st, _ := n.reg.State(uint32(b))
		if strings.Contains(st.Name, part) {
			k++
		}
	}
	return k
}

func TestConfigStructures(t *testing.T) {
	vanilla := testNether(t, configSeed)
	if _, _, _, ok := vanilla.NearestFortress(0, 0, 10); !ok {
		t.Fatal("no vanilla fortress")
	}
	cfg := Vanilla()
	cfg.Structures.Disable(StructureFortress)
	n := testNether(t, configSeed, WithConfig(cfg))
	if _, _, _, ok := n.NearestFortress(0, 0, 10); ok {
		t.Error("found a fortress with fortresses disabled")
	}
	cfg = Vanilla()
	cfg.Structures.PlaceAt(StructureFortress, 5, 7)
	cfg.Structures.PlaceAt(StructureBastion, 70, 70)
	n = testNether(t, configSeed, WithConfig(cfg))
	if x, _, z, ok := n.NearestFortress(5*16, 7*16, 1); !ok || x>>4 < 0 || x>>4 > 10 || z>>4 < 2 || z>>4 > 12 {
		t.Errorf("fortress at %d %d (%v), want near chunk 5 7", x, z, ok)
	}
	if x, _, z, ok := n.NearestBastion(70*16, 70*16, 1, ""); !ok || x>>4 < 64 || x>>4 > 76 || z>>4 < 64 || z>>4 > 76 {
		t.Errorf("bastion at %d %d (%v), want near chunk 70 70", x, z, ok)
	}
	if countIn(n, 5, 7, "nether_brick") == 0 {
		t.Error("no nether bricks in the placed fortress's chunk")
	}
}

func TestConfigBiomesAndRules(t *testing.T) {
	cfg := Vanilla()
	cfg.Biomes.Only("basalt_deltas")
	n := testNether(t, configSeed, WithConfig(cfg))
	_, ids := testData(t)
	var out [256]uint16
	n.ChunkBiomes(3, -9, &out)
	for _, b := range out {
		if uint32(b) != ids["basalt_deltas"] {
			t.Fatalf("biome %d with Only basalt deltas", b)
		}
	}
	cfg = Vanilla()
	cfg.Features.Disable("*fungus*")
	n = testNether(t, configSeed, WithConfig(cfg))
	for _, passes := range n.rules {
		for _, rules := range passes {
			for _, r := range rules {
				if strings.Contains(r.id, "fungus") && !r.off {
					t.Errorf("rule %s still on", r.id)
				}
			}
		}
	}
	for want, set := range map[string]func(c *Config){
		"feature rule": func(c *Config) { c.Features.Disable("no_such_rule") },
		"Nether biome": func(c *Config) { c.Biomes.Remove("plains") },
		"structure":    func(c *Config) { c.Structures.Disable("minecraft:village") },
	} {
		cfg := Vanilla()
		set(cfg)
		reg, ids := testData(t)
		if _, err := New(configSeed, reg, ids, WithConfig(cfg)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("want an error with %q, got %v", want, err)
		}
	}
}

func TestConfigSkip(t *testing.T) {
	vanilla := testNether(t, configSeed)
	if countIn(vanilla, 0, 0, "quartz_ore")+countIn(vanilla, 1, 0, "quartz_ore")+countIn(vanilla, 0, 1, "quartz_ore") == 0 {
		t.Skip("no quartz ore near the origin to compare")
	}
	cfg := Vanilla()
	cfg.Skip.Decoration = true
	n := testNether(t, configSeed, WithConfig(cfg))
	if k := countIn(n, 0, 0, "quartz_ore") + countIn(n, 1, 0, "quartz_ore") + countIn(n, 0, 1, "quartz_ore"); k != 0 {
		t.Errorf("%d quartz ore with decoration skipped", k)
	}
}

func TestConfigJSON(t *testing.T) {
	doc := `{
	  "features": {"disable": ["*fungus*"], "disable_in": [{"biomes": ["hell"], "rules": ["*vines*"]}]},
	  "biomes": {"replace": {"hell": "crimson_forest"}},
	  "structures": {
	    "exclude": [{"area": [-200, -200, 200, 200], "kinds": ["minecraft:bastion_remnant"]}],
	    "place_at": [{"kind": "minecraft:fortress", "chunk": [5, 7]}],
	    "loot": {"loot_tables/chests/nether_bridge.json": "loot_tables/my/fortress.json"},
	    "templates": {"bastion/bridge/starting_pieces/entrance": "bastion/bridge/starting_pieces/entrance_face"},
	    "pools": [{"pool": "minecraft:bastion/hoglin_stable/starting_pieces",
	               "element": "minecraft:bastion_hoglin_stable_starting_pieces_starting_stairs_0", "weight": 0}],
	    "blocks": [{"kind": "minecraft:fortress", "from": "nether_brick", "to": "red_nether_brick"}]
	  },
	  "skip": {"caves": true}
	}`
	cfg, err := ParseConfig([]byte(doc), nil)
	if err != nil {
		t.Fatal(err)
	}
	n := testNether(t, configSeed, WithConfig(cfg))
	if countIn(n, 5, 7, "red_nether_brick") == 0 {
		t.Error("the placed fortress's bricks were not swapped")
	}
	saved, err := cfg.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	again, err := ParseConfig(saved, nil)
	if err != nil {
		t.Fatal(err)
	}
	if saved2, _ := again.MarshalJSON(); string(saved2) != string(saved) {
		t.Errorf("the config does not survive a save and load:\n%s\n%s", saved, saved2)
	}
	if _, err := ParseConfig([]byte(`{"skip": {"cave": true}}`), nil); err == nil {
		t.Error("an unknown field was accepted")
	}
}
