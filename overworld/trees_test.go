package overworld

import (
	"strings"
	"testing"
)

// TestNoFloatingLeaves decorates a block of taiga chunks and checks that
// every leaf has a log nearby (a tree crossing a chunk border must be placed
// whole or not at all) and that plants stand on soil.
func TestNoFloatingLeaves(t *testing.T) {
	reg, ids := testRegistry(t)
	o, err := NewOverworld(236621124415672384, reg, ids)
	if err != nil {
		t.Fatal(err)
	}
	cache := NewTerrainCache(o, 512)
	chunks := map[[2]int32]*Chunk{}
	for cx := int32(-11); cx < -6; cx++ {
		for cz := int32(29); cz < 34; cz++ {
			c := new(Chunk)
			cache.Generate(cx, cz, c)
			chunks[[2]int32{cx, cz}] = c
		}
	}
	at := func(x, y, z int32) string {
		c, ok := chunks[[2]int32{x >> 4, z >> 4}]
		if !ok || y < MinY || y >= MaxY {
			return ""
		}
		return o.blockName(c.Block(int(x&15), int(y), int(z&15)))
	}
	floating, total := 0, 0
	for cx := int32(-10); cx < -7; cx++ {
		for cz := int32(30); cz < 33; cz++ {
			for x := cx * 16; x < cx*16+16; x++ {
				for z := cz * 16; z < cz*16+16; z++ {
					for y := int32(60); y < 140; y++ {
						name := at(x, y, z)
						if soilPlants[name] && !strings.Contains(name, "_upper") {
							below := at(x, y-1, z)
							// A sand, gravel or clay disc started in another
							// chunk may turn the soil under a plant; BDS
							// decorates chunks in load order and can do the
							// same.
							disc := below == "minecraft:sand" || below == "minecraft:gravel" || below == "minecraft:clay"
							if !(below == name || disc || o.hasBlockTag(chunks[[2]int32{x >> 4, z >> 4}].Block(int(x&15), int(y-1), int(z&15)), "dirt")) {
								t.Errorf("%s at %d %d %d stands on %s", name, x, y, z, below)
							}
						}
						if !strings.HasSuffix(name, "_leaves") {
							continue
						}
						total++
						near := false
						for dx := int32(-7); dx <= 7 && !near; dx++ {
							for dz := int32(-7); dz <= 7 && !near; dz++ {
								for dy := int32(-12); dy <= 3 && !near; dy++ {
									if strings.HasSuffix(at(x+dx, y+dy, z+dz), "_log") {
										near = true
									}
								}
							}
						}
						if !near {
							floating++
							if floating < 5 {
								t.Errorf("floating leaf at %d %d %d", x, y, z)
							}
						}
					}
				}
			}
		}
	}
	if total == 0 {
		t.Error("no leaves generated")
	}
}
