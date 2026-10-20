package worldgen

import (
	"fmt"
	"os"
	"testing"
)

// TestListStrongholds prints the strongholds within WORLDGEN_SH_R grid
// cells of the origin for the BDS comparison seed: the start chunk block
// BDS's "locate structure" reports (chunk*16+8), the piece count, the
// portal room and the box. Every stronghold must have a portal room.
func TestListStrongholds(t *testing.T) {
	r := os.Getenv("WORLDGEN_SH_R")
	if r == "" {
		t.Skip("set WORLDGEN_SH_R")
	}
	var radius int32
	_, _ = fmt.Sscan(r, &radius)
	reg, ids := testRegistry(t)
	o, err := NewOverworld(236621124415672384, reg, ids)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println("first three:", o.strongholdFirst())
	o.strongholdsNear(0, 0, radius*200+200, func(sx, sz int32) {
		s := o.strongholdAt(sx, sz)
		if s == nil {
			t.Errorf("no stronghold at chunk %d %d", sx, sz)
			return
		}
		kinds := map[uint8]int{}
		for _, p := range s.pieces {
			kinds[p.kind]++
		}
		portal := "none"
		if s.portal != nil {
			x, y, z := s.portal.worldPos(5, 1, 2)
			portal = fmt.Sprintf("%d %d %d", x, y, z)
		} else {
			t.Errorf("stronghold at %d %d has no portal room", sx*16+8, sz*16+8)
		}
		fmt.Printf("stronghold %d %d pieces=%d kinds=%v portal=%s box=%v\n", sx*16+8, sz*16+8, len(s.pieces), kinds, portal, s.bb)
	})
}

// TestStrongholdBlocks builds the chunks of the stronghold nearest the
// origin and counts what it placed.
func TestStrongholdBlocks(t *testing.T) {
	if os.Getenv("WORLDGEN_SH_R") == "" {
		t.Skip("set WORLDGEN_SH_R")
	}
	reg, ids := testRegistry(t)
	o, err := NewOverworld(236621124415672384, reg, ids)
	if err != nil {
		t.Fatal(err)
	}
	f := o.strongholdFirst()[0]
	s := o.strongholdAt(f[0], f[1])
	tc := NewTerrainCache(o, 256)
	counts := map[string]int{}
	chests, spawners := 0, 0
	for cx := s.bb.x0 >> 4; cx <= s.bb.x1>>4; cx++ {
		for cz := s.bb.z0 >> 4; cz <= s.bb.z1>>4; cz++ {
			var c Chunk
			tc.Generate(cx, cz, &c)
			for y := s.bb.y0; y <= s.bb.y1; y++ {
				for x := 0; x < 16; x++ {
					for z := 0; z < 16; z++ {
						id := c.Block(x, int(y), z)
						if o.isFence(id) || o.isPane(id) {
							st, _ := reg.State(id)
							for _, v := range st.Props {
								if v == uint8(1) {
									counts["joined "+o.blockName(id)]++
									break
								}
							}
						}
						switch n := o.blockName(id); n {
						case "minecraft:stone_bricks", "minecraft:mossy_stone_bricks", "minecraft:end_portal_frame",
							"minecraft:end_portal", "minecraft:bookshelf", "minecraft:iron_bars", "minecraft:oak_fence", "minecraft:wooden_door", "minecraft:iron_door":
							counts[n]++
						}
					}
				}
			}
			for _, be := range c.BlockEntities {
				if len(be.LootTable) > 30 && be.LootTable[:30] == "loot_tables/chests/stronghold_" {
					chests++
				}
				if be.Data["EntityIdentifier"] == "minecraft:silverfish" {
					spawners++
				}
			}
		}
	}
	fmt.Println("stronghold", f[0]*16+8, f[1]*16+8, counts, "chests", chests, "spawners", spawners)
	if counts["minecraft:end_portal_frame"] != 12 {
		t.Errorf("want 12 portal frames, got %d", counts["minecraft:end_portal_frame"])
	}
	if counts["joined minecraft:oak_fence"] == 0 || counts["joined minecraft:iron_bars"] == 0 {
		t.Error("fences or bars left unconnected")
	}
	if spawners != 1 {
		t.Errorf("want 1 spawner, got %d", spawners)
	}
}
