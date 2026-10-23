package overworld

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/helix-bedrock/worldgen/rand"
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
