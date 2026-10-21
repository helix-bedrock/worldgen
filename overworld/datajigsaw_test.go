package overworld

import (
	"fmt"
	"os"
	"testing"
)

// TestListDataJigsaws prints the trail ruins and trial chambers starting
// within WORLDGEN_DJ_R placement regions of the origin for the BDS
// comparison seed: the start chunk block BDS's "locate structure" reports
// (chunk*16+8), the piece count and the box.
func TestListDataJigsaws(t *testing.T) {
	r := os.Getenv("WORLDGEN_DJ_R")
	if r == "" {
		t.Skip("set WORLDGEN_DJ_R")
	}
	var radius int32
	_, _ = fmt.Sscan(r, &radius)
	reg, ids := testRegistry(t)
	o, err := NewOverworld(236621124415672384, reg, ids)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range o.dataJigsaws {
		for rx := -radius; rx <= radius; rx++ {
			for rz := -radius; rz <= radius; rz++ {
				cx, cz := d.pick(o, rx, rz)
				s := d.at(o, cx, cz)
				if s == nil {
					continue
				}
				for _, p := range s.pieces {
					if o.elementTemplate(p.el) == nil {
						t.Errorf("%s: missing template %q", d.kind, p.el.template)
					}
				}
				fmt.Printf("%s start=%v pieces=%d box=%v\n", d.kind, s.start, len(s.pieces), s.bb)
			}
		}
	}
}

// TestDataJigsawBlocks builds the chunks of the nearest trail ruins and
// trial chambers and counts a few of their blocks.
func TestDataJigsawBlocks(t *testing.T) {
	if os.Getenv("WORLDGEN_DJ_R") == "" {
		t.Skip("set WORLDGEN_DJ_R")
	}
	reg, ids := testRegistry(t)
	o, err := NewOverworld(236621124415672384, reg, ids)
	if err != nil {
		t.Fatal(err)
	}
	tc := NewTerrainCache(o, 512)
	for _, kind := range []string{"trail_ruins", "trial_chambers"} {
		s, ok := o.NearestDataJigsaw(kind, 0, 0, 10)
		if !ok {
			t.Fatalf("no %s", kind)
		}
		var st *djStructure
		for _, d := range o.dataJigsaws {
			if d.kind == kind {
				st = d.at(o, s.X>>4, s.Z>>4)
				if st == nil {
					// The start chunk is the piece's corner chunk.
					d.near(o, s.X>>4, s.Z>>4, func(x *djStructure) { st = x })
				}
			}
		}
		counts := map[string]int{}
		loot := 0
		for cx := st.bb.x0 >> 4; cx <= st.bb.x1>>4; cx++ {
			for cz := st.bb.z0 >> 4; cz <= st.bb.z1>>4; cz++ {
				var c Chunk
				tc.Generate(cx, cz, &c)
				for y := st.bb.y0; y <= st.bb.y1; y++ {
					for x := 0; x < 16; x++ {
						for z := 0; z < 16; z++ {
							switch n := o.blockName(c.Block(x, int(y), z)); n {
							case "minecraft:suspicious_gravel", "minecraft:terracotta", "minecraft:mud_bricks",
								"minecraft:tuff_bricks", "minecraft:trial_spawner", "minecraft:vault", "minecraft:waxed_copper_bulb",
								"minecraft:waxed_oxidized_copper_bulb", "minecraft:waxed_exposed_copper_bulb", "minecraft:waxed_weathered_copper_bulb":
								counts[n]++
							}
						}
					}
				}
				for _, be := range c.BlockEntities {
					if be.LootTable != "" {
						loot++
					}
				}
			}
		}
		fmt.Println(kind, s, "pieces", len(st.pieces), counts, "loot blocks", loot)
	}
}
