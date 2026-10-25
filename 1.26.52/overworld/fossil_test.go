package overworld

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestWellsAndFossils builds the desert and swamp chunks within
// WORLDGEN_FOSSIL_R chunks of the origin (or WORLDGEN_FOSSIL_AT) of
// the BDS comparison seed and lists the desert wells (by their suspicious
// sand) and the chunks with fossil bone blocks.
func TestWellsAndFossils(t *testing.T) {
	r := os.Getenv("WORLDGEN_FOSSIL_R")
	if r == "" {
		t.Skip("set WORLDGEN_FOSSIL_R")
	}
	var radius int32
	_, _ = fmt.Sscan(r, &radius)
	reg, ids := testRegistry(t)
	o, err := NewOverworld(236621124415672384, reg, ids)
	if err != nil {
		t.Fatal(err)
	}
	// WORLDGEN_FOSSIL_AT moves the centre ("x z" in blocks).
	var mx, mz int32
	if v := os.Getenv("WORLDGEN_FOSSIL_AT"); v != "" {
		_, _ = fmt.Sscan(v, &mx, &mz)
	}
	mx, mz = mx>>4, mz>>4
	tc := NewTerrainCache(o, 512)
	wells, fossils, ores := 0, 0, 0
	for cx := mx - radius; cx <= mx+radius; cx++ {
		for cz := mz - radius; cz <= mz+radius; cz++ {
			b := o.SurfaceBiomeAt(cx*16+8, cz*16+8)
			tags := ""
			if int(b) < len(o.biomeTags) {
				tags = strings.Join(o.biomeTags[b], ",")
			}
			if !strings.Contains(tags, "desert") && !strings.Contains(tags, "swamp") {
				continue
			}
			var c Chunk
			tc.Generate(cx, cz, &c)
			bones, ore := 0, 0
			minY := int32(MaxY)
			for y := int32(MinY); y < MaxY; y++ {
				for x := 0; x < 16; x++ {
					for z := 0; z < 16; z++ {
						switch o.blockName(c.Block(x, int(y), z)) {
						case "minecraft:bone_block":
							bones++
							minY = min(minY, y)
						case "minecraft:coal_ore":
							if c.Block(x, int(y)+1, z) == o.ids.air {
								continue
							}
						}
					}
				}
			}
			for _, be := range c.BlockEntities {
				if strings.Contains(be.LootTable, "desert_well") {
					wells++
					fmt.Println("well sus sand at", be.X, be.Y, be.Z)
				}
			}
			if bones > 0 {
				fossils++
				fmt.Printf("fossil chunk %d %d bones=%d lowest y=%d\n", cx*16, cz*16, bones, minY)
			}
			ores += ore
		}
	}
	fmt.Println("well sand blocks", wells, "fossil chunks", fossils)
}
