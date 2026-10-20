package worldgen

import (
	"fmt"
	"os"
	"testing"
)

// TestListMansions prints the woodland mansions starting within
// WORLDGEN_MANSION_R regions of the origin for the BDS comparison seed:
// the start BDS's "locate structure" reports, the turn, the piece count and
// the box. Every piece must name a template that exists.
func TestListMansions(t *testing.T) {
	r := os.Getenv("WORLDGEN_MANSION_R")
	if r == "" {
		t.Skip("set WORLDGEN_MANSION_R")
	}
	var radius int32
	_, _ = fmt.Sscan(r, &radius)
	reg, ids := testRegistry(t)
	o, err := NewOverworld(236621124415672384, reg, ids)
	if err != nil {
		t.Fatal(err)
	}
	tc := NewTerrainCache(o, 64)
	for rx := -radius; rx <= radius; rx++ {
		for rz := -radius; rz <= radius; rz++ {
			cx, cz := o.mansionPick(rx, rz)
			m := o.mansionAt(cx, cz, tc.Raw)
			if m == nil {
				continue
			}
			for _, p := range m.pieces {
				if o.template("mansion/"+p.name) == nil {
					t.Errorf("mansion at chunk %d %d: missing template %q", cx, cz, p.name)
				}
			}
			fmt.Printf("mansion %d %d rot=%d start=%v pieces=%d box=%v\n", cx*16+8, cz*16+8, m.rot, m.origin, len(m.pieces), m.bb)
		}
	}
}

// TestMansionBlocks builds the chunks of the mansion nearest the origin (or
// WORLDGEN_MANSION_AT, "x z")
// and counts what it placed: chests, cobblestone and a few of its blocks.
func TestMansionBlocks(t *testing.T) {
	if os.Getenv("WORLDGEN_MANSION_R") == "" {
		t.Skip("set WORLDGEN_MANSION_R")
	}
	reg, ids := testRegistry(t)
	o, err := NewOverworld(236621124415672384, reg, ids)
	if err != nil {
		t.Fatal(err)
	}
	x, z := int32(0), int32(0)
	if v := os.Getenv("WORLDGEN_MANSION_AT"); v != "" {
		_, _ = fmt.Sscan(v, &x, &z)
	}
	s, ok := o.NearestMansion(x, z, 15)
	if !ok {
		t.Fatal("no mansion")
	}
	tc := NewTerrainCache(o, 256)
	m := o.mansionAt(s.X>>4, s.Z>>4, tc.Raw)
	counts := map[string]int{}
	chests := 0
	for cx := m.bb.x0 >> 4; cx <= m.bb.x1>>4; cx++ {
		for cz := m.bb.z0 >> 4; cz <= m.bb.z1>>4; cz++ {
			var c Chunk
			tc.Generate(cx, cz, &c)
			for y := m.bb.y0 - 4; y <= m.bb.y1; y++ {
				for x := 0; x < 16; x++ {
					for z := 0; z < 16; z++ {
						n := o.blockName(c.Block(x, int(y), z))
						switch n {
						case "minecraft:dark_oak_planks", "minecraft:cobblestone", "minecraft:chest", "minecraft:stone_stairs",
							"minecraft:birch_planks", "minecraft:dark_oak_log", "minecraft:glass_pane":
							counts[n]++
						}
					}
				}
			}
			for _, be := range c.BlockEntities {
				if be.LootTable == "loot_tables/chests/woodland_mansion.json" {
					chests++
				}
			}
		}
	}
	fmt.Println("mansion", s, counts, "loot chests", chests)
	if counts["minecraft:dark_oak_planks"] == 0 {
		t.Fatal("no mansion blocks placed")
	}
}
