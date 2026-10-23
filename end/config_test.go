package end

import (
	"strings"
	"testing"

	"github.com/helix-bedrock/worldgen/structure"
)

const configSeed = 236621124415672384

func countIn(e *End, cache *Cache, cx, cz int32, part string) int {
	var c Chunk
	cache.Generate(cx, cz, &c)
	n := 0
	for _, b := range c.Blocks {
		st, _ := e.reg.State(uint32(b))
		if strings.Contains(st.Name, part) {
			n++
		}
	}
	return n
}

func TestConfigCities(t *testing.T) {
	vanilla := testEnd(t, configSeed)
	cx, _, cz, ok := vanilla.NearestCity(1500, 0, 20)
	if !ok {
		t.Fatal("no vanilla End city")
	}
	cfg := Vanilla()
	cfg.Structures.Exclude(structure.NewArea(cx-100, cz-100, cx+100, cz+100))
	e := testEnd(t, configSeed, WithConfig(cfg))
	if x, _, z, ok := e.NearestCity(cx, cz, 20); ok && x == cx && z == cz {
		t.Error("the excluded city is still there")
	}
	cfg = Vanilla()
	cfg.Structures.PlaceAt(110, 3)
	e = testEnd(t, configSeed, WithConfig(cfg))
	if x, _, z, ok := e.NearestCity(110*16, 3*16, 1); !ok || x>>4 != 110 || z>>4 != 3 {
		t.Errorf("city at %d %d (%v), want chunk 110 3", x, z, ok)
	}
	if countIn(e, NewCache(e, 64), 110, 3, "purpur") == 0 {
		t.Error("no purpur in the placed city's chunk")
	}
}

func TestConfigSkip(t *testing.T) {
	vanilla := testEnd(t, configSeed)
	vc := NewCache(vanilla, 256)
	// The chunk of the first pillar.
	s := vanilla.spikes[0]
	px, pz := s.x>>4, s.z>>4
	if countIn(vanilla, vc, px, pz, "obsidian") == 0 {
		t.Fatal("no obsidian at the first pillar")
	}
	cfg := Vanilla()
	cfg.Skip = Steps{Pillars: true, Chorus: true}
	e := testEnd(t, configSeed, WithConfig(cfg))
	ec := NewCache(e, 256)
	if n := countIn(e, ec, px, pz, "obsidian"); n != 0 {
		t.Errorf("%d obsidian with the pillars skipped", n)
	}
	cx, _, cz, ok := vanilla.NearestCity(1500, 0, 20)
	if !ok {
		t.Fatal("no vanilla End city")
	}
	chorus := 0
	for dx := int32(-3); dx <= 3; dx++ {
		chorus += countIn(e, ec, cx>>4+dx, cz>>4, "chorus")
	}
	if chorus != 0 {
		t.Errorf("%d chorus blocks with chorus skipped", chorus)
	}
}

func TestConfigJSON(t *testing.T) {
	doc := `{
	  "structures": {
	    "place_at": [[110, 3]],
	    "loot": {"loot_tables/chests/end_city_treasure.json": "loot_tables/my/city.json"},
	    "templates": {"endcity/base_roof": "endcity/second_roof"},
	    "blocks": [["purpur_block", "diamond_block"]]
	  },
	  "skip": {"chorus": true}
	}`
	cfg, err := ParseConfig([]byte(doc), nil)
	if err != nil {
		t.Fatal(err)
	}
	e := testEnd(t, configSeed, WithConfig(cfg))
	if countIn(e, NewCache(e, 64), 110, 3, "diamond_block") == 0 {
		t.Error("the placed city's purpur was not swapped")
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
}
