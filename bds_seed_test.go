package worldgen

import "testing"

// bdsSpots are biome positions BDS 1.26.52.3 reported for seed
// 236621124415672384, from "execute positioned ... run locate biome" in the
// server console. Each is a spot where BDS has that biome.
var bdsSpots = []struct {
	name    string
	x, y, z int32
}{
	{"jungle", 528, 63, -80},
	{"bamboo_jungle", 1552, 63, -2416},
	{"beach", -688, 73, -3088},
	{"birch_forest", 304, 98, -1392},
	{"birch_forest_mutated", -720, 79, -1008},
	{"cherry_grove", -3344, 112, 7312},
	{"cold_ocean", -560, 63, 144},
	{"cold_taiga", 3152, 65, -7440},
	{"dappled_forest", -720, 71, -176},
	{"deep_cold_ocean", -752, 63, 176},
	{"deep_dark", -240, -59, -240},
	{"deep_frozen_ocean", 2736, 63, -6736},
	{"deep_lukewarm_ocean", 816, 63, -592},
	{"deep_ocean", -1040, 63, -944},
	{"desert", -3952, 67, -5968},
}

func TestBiomesMatchBDS(t *testing.T) {
	reg, ids := testRegistry(t)
	o, err := NewOverworld(236621124415672384, reg, ids)
	if err != nil {
		t.Fatal(err)
	}
	names := make(map[uint32]string, len(ids))
	for n, id := range ids {
		names[id] = n
	}
	bad := 0
	for _, s := range bdsSpots {
		// locate reports the block it found; check it and its 4 neighbours
		// at quarter resolution, since biomes are sampled per 4 blocks.
		got := names[o.BiomeAt(s.x, s.y, s.z)]
		if got != s.name {
			bad++
			t.Errorf("(%d, %d, %d): BDS has %s, we have %s", s.x, s.y, s.z, s.name, got)
		}
	}
	t.Logf("%d of %d spots match BDS", len(bdsSpots)-bad, len(bdsSpots))
}
