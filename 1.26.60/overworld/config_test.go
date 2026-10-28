package overworld

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/helix-bedrock/worldgen/1.26.60/rand"
)

// configSeed is the BDS comparison seed the other tests use.
const configSeed = 236621124415672384

// forestX and forestZ are a forest chunk of configSeed: its corners and
// centre are all forest.
const forestX, forestZ int32 = 1, -2

func newConfigured(t *testing.T, cfg *Config) *Overworld {
	t.Helper()
	reg, ids := testRegistry(t)
	o, err := NewOverworld(configSeed, reg, ids, WithConfig(cfg))
	if err != nil {
		t.Fatal(err)
	}
	return o
}

// countBlocks counts the blocks of a finished chunk whose name contains
// part.
func countBlocks(o *Overworld, cx, cz int32, part string) int {
	var c Chunk
	NewTerrainCache(o, 256).Generate(cx, cz, &c)
	n := 0
	for _, b := range c.Blocks {
		if strings.Contains(o.blockName(uint32(b)), part) {
			n++
		}
	}
	return n
}

func TestConfigUnknownNames(t *testing.T) {
	reg, ids := testRegistry(t)
	cases := map[string]func(c *Config){
		"forest_surface_trees_feature": func(c *Config) { c.Features.Disable("forest_surface_tress_feature") },
		"plains":                       func(c *Config) { c.Biomes.Remove("plainz") },
		"village":                      func(c *Config) { c.Structures.Disable("minecraft:vilage") },
		"oak_tree_feature":             func(c *Config) { c.Features.DisableFeature("oak_tre_feature") },
	}
	for want, set := range cases {
		cfg := Vanilla()
		set(cfg)
		_, err := NewOverworld(configSeed, reg, ids, WithConfig(cfg))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("want an error suggesting %s, got %v", want, err)
		}
	}
}

func TestConfigDisableTrees(t *testing.T) {
	vanilla := newConfigured(t, Vanilla())
	if countBlocks(vanilla, forestX, forestZ, "_log") == 0 {
		t.Fatal("the forest chunk has no logs in vanilla")
	}
	cfg := Vanilla()
	cfg.Features.DisableFeature("*tree*")
	if n := countBlocks(newConfigured(t, cfg), forestX, forestZ, "_log"); n != 0 {
		t.Errorf("%d logs with every tree feature disabled", n)
	}
}

func TestConfigDisableRule(t *testing.T) {
	cfg := Vanilla()
	cfg.Features.Disable("forest_surface_trees_feature")
	o := newConfigured(t, cfg)
	r := o.rules.byID["minecraft:forest_surface_trees_feature"]
	if r == nil || !r.drawOnly {
		t.Fatal("the rule is not disabled")
	}
	cfg = Vanilla()
	cfg.Features.DisableIn([]string{"forest"}, "forest_surface_trees_feature")
	o = newConfigured(t, cfg)
	if r := o.rules.byID["minecraft:forest_surface_trees_feature"]; r.drawOnly {
		t.Error("DisableIn disabled the rule everywhere")
	}
}

func TestConfigBiomes(t *testing.T) {
	_, ids := testRegistry(t)
	cfg := Vanilla()
	cfg.Biomes.Only("plains")
	o := newConfigured(t, cfg)
	for _, p := range [][2]int32{{0, 0}, {-3400, 3900}, {5000, -200}, {-41 * 16, 31 * 16}} {
		if b := o.BiomeAt(p[0], 70, p[1]); b != ids["plains"] {
			t.Errorf("biome %d at %v, want plains", b, p)
		}
	}
	cfg = Vanilla()
	cfg.Biomes.Replace("forest", "desert")
	o = newConfigured(t, cfg)
	x, z := forestX*16+8, forestZ*16+8
	if b := o.SurfaceBiomeAt(x, z); b != ids["desert"] {
		t.Errorf("biome %d in the forest chunk, want desert", b)
	}
}

func TestConfigStructures(t *testing.T) {
	vanilla := newConfigured(t, Vanilla())
	vx, _, vz, ok := vanilla.NearestVillage(0, 0, "plains", 40)
	if !ok {
		t.Fatal("no vanilla village")
	}
	cfg := Vanilla()
	cfg.Structures.Disable(StructureVillage)
	o := newConfigured(t, cfg)
	if _, _, _, ok := o.NearestVillage(0, 0, "", 40); ok {
		t.Error("found a village with villages disabled")
	}
	if len(o.VillagePieces(vx>>4, vz>>4)) != 0 {
		t.Error("village pieces with villages disabled")
	}
	cfg = Vanilla()
	cfg.Structures.Exclude(Box(vx-64, vz-64, vx+64, vz+64), StructureVillage)
	o = newConfigured(t, cfg)
	if x, _, z, ok := o.NearestVillage(vx, vz, "plains", 40); ok && x == vx && z == vz {
		t.Error("the excluded village is still there")
	}
	var logs bytes.Buffer
	cfg = Vanilla()
	cfg.Logger = slog.New(slog.NewTextHandler(&logs, nil))
	cfg.Structures.Filter(func(kind StructureKind, cx, cz int32) bool {
		if kind == StructureMansion {
			panic("filter failure")
		}
		return true
	})
	o = newConfigured(t, cfg)
	if _, ok := o.NearestMansion(0, 0, 10); ok {
		t.Error("found a mansion that a panicking filter denied")
	}
	if !strings.Contains(logs.String(), "filter failure") {
		t.Error("the filter's panic was not logged")
	}
}

// goldPillar is a Go feature: a pillar of three gold blocks.
type goldPillar struct{}

func (goldPillar) Place(w World, x, y, z int32, _ *rand.MT) bool {
	gold, ok := w.Registry().Default("minecraft:gold_block")
	if !ok {
		return false
	}
	for i := range int32(3) {
		w.SetBlock(x, y+i, z, gold)
	}
	return true
}

// panicky is a Go feature that panics.
type panicky struct{}

func (panicky) Place(World, int32, int32, int32, *rand.MT) bool { panic("feature failure") }

func TestConfigGoFeature(t *testing.T) {
	var logs bytes.Buffer
	cfg := Vanilla()
	cfg.Logger = slog.New(slog.NewTextHandler(&logs, nil))
	cfg.Features.Register("test:gold_pillar", goldPillar{})
	cfg.Features.Register("test:panicky", panicky{})
	cfg.Features.AddRule(Rule{ID: "test:gold_pillars", Places: "test:gold_pillar", Pass: SurfacePass,
		Biomes: []string{"forest"}, Iterations: 2})
	cfg.Features.AddRule(Rule{ID: "test:panics", Places: "test:panicky", Pass: SurfacePass, Biomes: []string{"forest"}})
	o := newConfigured(t, cfg)
	if n := countBlocks(o, forestX, forestZ, "gold_block"); n == 0 {
		t.Error("the Go feature placed no gold")
	}
	if !strings.Contains(logs.String(), "feature failure") {
		t.Error("the feature's panic was not logged")
	}
}

func TestConfigPack(t *testing.T) {
	pack := fstest.MapFS{
		// Replaces the forest's trees with a pack feature: one diamond block.
		"feature_rules/forest_surface_trees_feature.json": {Data: []byte(`{"format_version":"1.13.0",
			"minecraft:feature_rules":{"description":{"identifier":"minecraft:forest_surface_trees_feature",
			"places_feature":"test:diamond"},"conditions":{"placement_pass":"surface_pass",
			"minecraft:biome_filter":[{"test":"has_biome_tag","value":"forest"}]},
			"distribution":{"iterations":4,"x":{"distribution":"uniform","extent":[0,16]},
			"y":"query.heightmap(variable.worldx, variable.worldz)",
			"z":{"distribution":"uniform","extent":[0,16]}}}}`)},
		"features/diamond.json": {Data: []byte(`{"format_version":"1.13.0","minecraft:single_block_feature":{
			"description":{"identifier":"test:diamond"},"places_block":"minecraft:diamond_block",
			"enforce_survivability_rules":false,"enforce_placement_rules":false}}`)},
	}
	cfg := Vanilla()
	cfg.AddPack(pack)
	o := newConfigured(t, cfg)
	if n := countBlocks(o, forestX, forestZ, "diamond_block"); n == 0 {
		t.Error("the pack's rule placed no diamond")
	}
}

func TestConfigSkip(t *testing.T) {
	cfg := Vanilla()
	cfg.Skip = Steps{Decoration: true, Structures: true}
	o := newConfigured(t, cfg)
	if n := countBlocks(o, forestX, forestZ, "_log"); n != 0 {
		t.Errorf("%d logs with decoration skipped", n)
	}
	if _, _, _, ok := o.NearestVillage(0, 0, "", 40); ok {
		t.Error("found a village with structures skipped")
	}
}

func TestConfigPlaceAt(t *testing.T) {
	cfg := Vanilla()
	cfg.Structures.PlaceAt(StructureVillage, 0, 0)
	cfg.Structures.PlaceAt(StructureDesertPyramid, 40, 40)
	cfg.Structures.PlaceAt(StructureMansion, -20, 30)
	o := newConfigured(t, cfg)
	if !o.IsVillageChunk(0, 0) || len(o.VillagePieces(0, 0)) == 0 {
		t.Error("no village at the placed chunk")
	}
	if s, ok := o.NearestScattered(40*16, 40*16, "desert_pyramid", 1); !ok || s.X>>4 != 40 || s.Z>>4 != 40 {
		t.Errorf("desert pyramid at %+v (%v), want chunk 40 40", s, ok)
	}
	if s, ok := o.NearestMansion(-20*16, 30*16, 1); !ok || s.X>>4 < -22 || s.X>>4 > -18 || s.Z>>4 < 28 || s.Z>>4 > 32 {
		t.Errorf("mansion at %+v (%v), want near chunk -20 30", s, ok)
	}
	// The placed chunks generate.
	var c Chunk
	cache := NewTerrainCache(o, 256)
	for _, p := range [][2]int32{{0, 0}, {40, 40}, {-20, 30}} {
		cache.Generate(p[0], p[1], &c)
	}
}

func TestConfigPlacementErrors(t *testing.T) {
	reg, ids := testRegistry(t)
	cases := map[string]func(c *Config){
		"above separation": func(c *Config) {
			c.Structures.SetPlacement(StructureVillage, Placement{Spacing: 8, Separation: 8})
		},
		"disabled": func(c *Config) {
			c.Structures.Disable(StructureIgloo)
			c.Structures.PlaceAt(StructureIgloo, 1, 1)
		},
		"shares its placement region": func(c *Config) {
			c.Structures.PlaceAt(StructureIgloo, 1, 1)
			c.Structures.PlaceAt(StructureSwampHut, 2, 2)
		},
		"rings": func(c *Config) { c.Structures.SetPlacement(StructureStronghold, Placement{Spacing: 10}) },
	}
	for want, set := range cases {
		cfg := Vanilla()
		set(cfg)
		_, err := NewOverworld(configSeed, reg, ids, WithConfig(cfg))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("want an error with %q, got %v", want, err)
		}
	}
}

func TestConfigPlacement(t *testing.T) {
	vanilla := newConfigured(t, Vanilla())
	cfg := Vanilla()
	cfg.Structures.SetPlacement(StructureVillage, Placement{Spacing: 12, Separation: 4})
	o := newConfigured(t, cfg)
	count := func(o *Overworld) int {
		n := 0
		for cx := int32(-96); cx < 96; cx++ {
			for cz := int32(-96); cz < 96; cz++ {
				if o.IsVillageChunk(cx, cz) {
					n++
				}
			}
		}
		return n
	}
	if a, b := count(vanilla), count(o); b <= a {
		t.Errorf("%d villages with spacing 12, %d in vanilla", b, a)
	}
	cfg = Vanilla()
	cfg.Structures.SetPlacement(StructureMineshaft, Placement{Chance: 1e-9})
	o = newConfigured(t, cfg)
	if _, ok := o.NearestMineshaft(0, 0, 30); ok {
		t.Error("found a mineshaft with a near-zero chance")
	}
}

// TestConfigReadme builds the Config of the README example.
func TestConfigReadme(t *testing.T) {
	reg, ids := testRegistry(t)
	ids["myserver:crystal_plains"] = 10000
	cfg := Vanilla()
	cfg.Biomes.Remove("mushroom_island")
	cfg.Biomes.Replace("roofed_forest", "forest")
	cfg.Biomes.Scale("cherry_grove", 1.5)
	cfg.Biomes.Add(Biome{Name: "myserver:crystal_plains", Like: "plains",
		Climate: Climate{Temperature: Range{Min: 0.2, Max: 0.55}}})
	cfg.Features.Disable("birch_forest_surface_trees_feature")
	cfg.Features.DisableIn([]string{"plains"}, "*flower*")
	cfg.Features.DisableFeature("*birch*")
	cfg.Features.SetChance("plains_surface_trees_feature", 50)
	cfg.Features.SetWeight("random_roofed_forest_feature", "minecraft:huge_mushroom_feature", 0)
	cfg.Features.Register("myserver:crystal", goldPillar{})
	cfg.Features.AddRule(Rule{ID: "myserver:crystals", Places: "myserver:crystal", Pass: SurfacePass,
		Tags: []string{"plains"}, Iterations: 2})
	cfg.Structures.Disable(StructureMansion)
	cfg.Structures.Exclude(Box(-500, -500, 500, 500))
	cfg.Structures.SetPlacement(StructureVillage, Placement{Spacing: 20, Separation: 6})
	cfg.Structures.PlaceAt(StructureVillage, 2, 3)
	cfg.Skip.Caves = true
	o, err := NewOverworld(configSeed, reg, ids, WithConfig(cfg))
	if err != nil {
		t.Fatal(err)
	}
	var c Chunk
	NewTerrainCache(o, 256).Generate(2, 3, &c)
}

func TestConfigStructureBiomes(t *testing.T) {
	cfg := Vanilla()
	cfg.Structures.SetBiomes(StructureMansion, "plains")
	o := newConfigured(t, cfg)
	_, ids := testRegistry(t)
	s, ok := o.NearestMansion(0, 0, 20)
	if !ok {
		t.Fatal("no mansion in plains")
	}
	if b := o.SurfaceBiomeAt(s.X, s.Z); b != ids["plains"] {
		t.Errorf("mansion in biome %d, want plains", b)
	}
	cfg = Vanilla()
	cfg.Structures.SetBiomes(StructurePillagerOutpost, "deep_ocean")
	if _, err := NewOverworld(configSeed, o.reg, ids, WithConfig(cfg)); err != nil {
		t.Error(err)
	}
	cfg = Vanilla()
	cfg.Structures.SetBiomes(StructureRuinedPortal, "plains")
	if _, err := NewOverworld(configSeed, o.reg, ids, WithConfig(cfg)); err == nil {
		t.Error("ruined portals took a biome list")
	}
}

func TestConfigStrongholds(t *testing.T) {
	vanilla := newConfigured(t, Vanilla())
	want, ok := vanilla.NearestStronghold(0, 0, 3)
	if !ok {
		t.Fatal("no vanilla stronghold")
	}
	cfg := Vanilla()
	cfg.Structures.Disable(StructureVillage)
	cfg.Structures.PlaceAt(StructureStronghold, 3, 4)
	o := newConfigured(t, cfg)
	if got, ok := o.NearestStronghold(want.X, want.Z, 3); !ok || got != want {
		t.Errorf("stronghold at %+v with villages off, want %+v", got, want)
	}
	if got, ok := o.NearestStronghold(3*16, 4*16, 0); !ok || got.X>>4 < -5 || got.X>>4 > 11 || got.Z>>4 < -4 || got.Z>>4 > 12 {
		t.Errorf("placed stronghold at %+v (%v), want near chunk 3 4", got, ok)
	}
}

func TestConfigStructureContents(t *testing.T) {
	vanilla := newConfigured(t, Vanilla())
	vx, _, vz, ok := vanilla.NearestVillage(0, 0, "plains", 40)
	if !ok {
		t.Fatal("no vanilla village")
	}
	cx, cz := vx>>4, vz>>4
	// The vanilla village's pieces and a chunk of it with loot.
	pieces := vanilla.VillagePieces(cx, cz)
	if len(pieces) < 2 {
		t.Fatal("a village of one piece")
	}
	cache := NewTerrainCache(vanilla, 256)
	var c Chunk
	loot := map[string]bool{}
	var lootChunk [2]int32
	for dx := int32(-4); dx <= 4 && len(loot) == 0; dx++ {
		for dz := int32(-4); dz <= 4 && len(loot) == 0; dz++ {
			cache.Generate(cx+dx, cz+dz, &c)
			for _, be := range c.BlockEntities {
				if be.LootTable != "" {
					loot[be.LootTable] = true
					lootChunk = [2]int32{cx + dx, cz + dz}
				}
			}
		}
	}
	if len(loot) == 0 {
		t.Fatal("no village chest")
	}
	cobble := countBlocks(vanilla, cx, cz, "cobblestone")

	cfg := Vanilla()
	for lt := range loot {
		cfg.Structures.ReplaceLoot(lt, "loot_tables/test/custom.json")
	}
	cfg.Structures.ReplaceBlock(StructureVillage, "cobblestone", "diamond_block")
	o := newConfigured(t, cfg)
	NewTerrainCache(o, 256).Generate(lootChunk[0], lootChunk[1], &c)
	for _, be := range c.BlockEntities {
		if be.LootTable != "" && be.LootTable != "loot_tables/test/custom.json" {
			t.Errorf("chest with loot %s", be.LootTable)
		}
	}
	if cobble > 0 && countBlocks(o, cx, cz, "diamond_block") == 0 {
		t.Error("the village's cobblestone was not swapped")
	}
	cfg = Vanilla()
	cfg.Structures.SetPoolWeight("minecraft:village/plains/houses", "village/plains/houses/plains_small_house_1", 0)
	o = newConfigured(t, cfg)
	for _, p := range o.VillagePieces(cx, cz) {
		if strings.HasSuffix(p.Template, "plains_small_house_1") {
			t.Error("a house of weight 0 was built")
		}
	}

	reg, ids := testRegistry(t)
	for want, set := range map[string]func(c *Config){
		"jigsaw pool": func(c *Config) { c.Structures.SetPoolWeight("minecraft:village/plains/housez", "x", 1) },
		"element":     func(c *Config) { c.Structures.SetPoolWeight("minecraft:village/plains/houses", "nope", 1) },
		"vanilla templ": func(c *Config) {
			c.Structures.ReplaceTemplate("no/such/template", "village/plains/houses/plains_small_house_1")
		},
		"unknown block":  func(c *Config) { c.Structures.ReplaceBlock(StructureVillage, "cobblestone", "no_such_block") },
		"not in the pac": func(c *Config) { c.Structures.AddToPool("minecraft:village/plains/houses", "my/missing", 1) },
	} {
		cfg := Vanilla()
		set(cfg)
		if _, err := NewOverworld(configSeed, reg, ids, WithConfig(cfg)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("want an error with %q, got %v", want, err)
		}
	}
}

func TestConfigTerrain(t *testing.T) {
	cfg := Vanilla()
	cfg.Terrain = Terrain{SeaBlock: "lava", NoDeepslate: true, NoOreVeins: true}
	o := newConfigured(t, cfg)
	var c Chunk
	o.GenerateChunk(-41, 31, &c) // an ocean chunk
	water, lava, deep := 0, 0, 0
	for _, b := range c.Blocks {
		switch o.blockName(uint32(b)) {
		case "minecraft:water":
			water++
		case "minecraft:lava":
			lava++
		case "minecraft:deepslate":
			deep++
		}
	}
	if water != 0 || lava == 0 || deep != 0 {
		t.Errorf("water %d, lava %d, deepslate %d; want a lava sea and no deepslate", water, lava, deep)
	}
	cfg = Vanilla()
	cfg.Terrain.SeaLevel = 100
	o = newConfigured(t, cfg)
	o.GenerateChunk(-41, 31, &c)
	if b := o.blockName(c.Block(8, 90, 8)); b != "minecraft:water" {
		t.Errorf("block %s at y 90 with sea level 100", b)
	}
}

func TestConfigExplain(t *testing.T) {
	o := newConfigured(t, Vanilla())
	cache := NewTerrainCache(o, 256)
	// A log in the forest chunk.
	var c Chunk
	cache.Generate(forestX, forestZ, &c)
	var pos [3]int32
	found := false
	for i, b := range c.Blocks {
		if strings.HasSuffix(o.blockName(uint32(b)), "_log") {
			pos = [3]int32{forestX*16 + int32(i&15), int32(i>>8) + MinY, forestZ*16 + int32(i>>4&15)}
			found = true
			break
		}
	}
	if !found {
		t.Fatal("no log in the forest chunk")
	}
	steps, err := cache.Explain(pos[0], pos[1], pos[2])
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) < 2 || steps[0].Step != "terrain (noise, aquifers, ore veins)" || !strings.HasSuffix(steps[len(steps)-1].Block, "_log") {
		t.Fatalf("steps %+v", steps)
	}
	tree := false
	for _, s := range steps {
		tree = tree || strings.Contains(s.Step, "tree") && strings.HasSuffix(s.Block, "_log")
	}
	if !tree {
		t.Errorf("no tree rule placed the log: %+v", steps)
	}
	var again Chunk
	cache.Generate(forestX, forestZ, &again)
	if again.Blocks != c.Blocks {
		t.Error("Explain changed the cache's output")
	}
}

func TestConfigJSON(t *testing.T) {
	doc := `{
	  "packs": ["pack"],
	  "features": {
	    "disable": ["birch_forest_surface_trees_feature"],
	    "disable_in": [{"biomes": ["plains"], "rules": ["*flower*"]}],
	    "disable_features": ["*birch*"],
	    "chance": {"plains_surface_trees_feature": 50},
	    "iterations": {"plains_surface_trees_feature": 3},
	    "replace": {"forest_surface_trees_feature": "minecraft:oak_tree_feature"},
	    "weights": [{"mix": "random_roofed_forest_feature", "feature": "minecraft:huge_mushroom_feature", "weight": 0}],
	    "rules": [{"id": "myserver:oaks", "places": "minecraft:oak_tree_feature", "pass": "surface_pass",
	               "tags": ["plains"], "iterations": 1, "percent": 10, "x": [0, 16], "y": "heightmap", "z": 4}]
	  },
	  "biomes": {
	    "remove": ["mushroom_island"], "replace": {"roofed_forest": "forest"},
	    "scale": {"cherry_grove": 1.5}, "tags": {"plains": ["overworld", "plains"]},
	    "add": [{"name": "myserver:crystal_plains", "like": "plains", "climate": {"temperature": [0.2, 0.55]}}]
	  },
	  "structures": {
	    "disable": ["minecraft:mansion"],
	    "exclude": [{"area": [-500, -500, 500, 500], "kinds": ["minecraft:village"]}],
	    "placement": {"minecraft:village": {"spacing": 20, "separation": 6}},
	    "place_at": [{"kind": "minecraft:village", "chunk": [2, 3]}],
	    "biomes": {"minecraft:igloo": ["plains"]},
	    "templates": {"village/plains/houses/plains_small_house_1": "village/plains/houses/plains_small_house_2"},
	    "pools": [{"pool": "minecraft:village/plains/houses", "element": "village/plains/houses/plains_small_house_3", "weight": 3}],
	    "loot": {"loot_tables/chests/village/village_plains_house.json": "loot_tables/my/house.json"},
	    "blocks": [{"kind": "minecraft:village", "from": "cobblestone", "to": "mossy_cobblestone"}]
	  },
	  "skip": {"caves": true},
	  "terrain": {"sea_level": 70, "sea_block": "lava", "no_deepslate": true, "no_ore_veins": true}
	}`
	root := fstest.MapFS{"pack/features/.keep": {Data: nil}}
	cfg, err := ParseConfig([]byte(doc), root)
	if err != nil {
		t.Fatal(err)
	}
	reg, ids := testRegistry(t)
	ids["myserver:crystal_plains"] = 10000
	if _, err := NewOverworld(configSeed, reg, ids, WithConfig(cfg)); err != nil {
		t.Fatal(err)
	}
	saved, err := cfg.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	again, err := ParseConfig(saved, root)
	if err != nil {
		t.Fatal(err)
	}
	saved2, err := again.MarshalJSON()
	if err != nil || string(saved2) != string(saved) {
		t.Errorf("the config does not survive a save and load:\n%s\n%s", saved, saved2)
	}
	if _, err := ParseConfig([]byte(`{"structures": {"disabel": []}}`), nil); err == nil {
		t.Error("an unknown field was accepted")
	}
}
