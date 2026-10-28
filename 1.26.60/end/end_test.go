package end

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/helix-bedrock/worldgen/1.26.60/biome"
	"github.com/helix-bedrock/worldgen/1.26.60/blocks"
)

func testEnd(t testing.TB, seed int64, opts ...Option) *End {
	t.Helper()
	dir := filepath.Join("..", "overworld", "testdata")
	p, err := os.Open(filepath.Join(dir, "block_palette.nbt"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()
	ty, err := os.Open(filepath.Join(dir, "block_types.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ty.Close() }()
	reg, err := blocks.Load(p, ty)
	if err != nil {
		t.Fatal(err)
	}
	bf, err := os.Open(filepath.Join(dir, "biomes.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = bf.Close() }()
	ids, err := biome.LoadIDs(bf)
	if err != nil {
		t.Fatal(err)
	}
	e, err := New(seed, reg, ids, opts...)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

// TestMainIsland pins the main island of a seed whose End was compared with
// a BDS world: the 81 chunks around the origin matched block for block,
// iron bar joins included.
func TestMainIsland(t *testing.T) {
	e := testEnd(t, 236621124415672384)
	c := NewCache(e, 256)
	var ch Chunk
	count := map[string]int{}
	for cx := int32(-4); cx <= 4; cx++ {
		for cz := int32(-4); cz <= 4; cz++ {
			c.Generate(cx, cz, &ch)
			for i := range ch.Blocks {
				st, _ := e.reg.State(uint32(ch.Blocks[i]))
				count[st.Name]++
			}
		}
	}
	for name, want := range map[string]int{
		"minecraft:end_stone": 538479, "minecraft:obsidian": 40499, "minecraft:iron_bars": 146,
	} {
		if count[name] != want {
			t.Errorf("%s: %d, BDS has %d", name, count[name], want)
		}
	}
}

// TestGateway pins an end gateway BDS placed on an outer island of the same
// seed, at (1109, 68, 8).
func TestGateway(t *testing.T) {
	e := testEnd(t, 236621124415672384)
	c := NewCache(e, 64)
	var ch Chunk
	c.Generate(69, 0, &ch)
	st, _ := e.reg.State(ch.Block(1109&15, 68, 8))
	if st.Name != "minecraft:end_gateway" {
		t.Fatalf("block at 1109 68 8: %s, BDS has an end gateway", st.Name)
	}
	if len(ch.BlockEntities) != 1 || ch.BlockEntities[0].Data["id"] != "EndGateway" {
		t.Errorf("block entities: %v", ch.BlockEntities)
	}
}

// TestCities pins the layouts of End cities that matched a BDS world of
// the same seed block for block: the piece counts, where the start and the
// ship are, and the first chest's loot seed.
func TestCities(t *testing.T) {
	e := testEnd(t, 236621124415672384)
	for _, c := range []struct {
		cx, cz int32
		pieces int
		ship   bool
	}{{-36, -55, 91, true}, {65, -19, 18, false}} {
		ps := e.CityPieces(c.cx, c.cz)
		if len(ps) != c.pieces {
			t.Errorf("city %d %d: %d pieces, BDS has %d", c.cx, c.cz, len(ps), c.pieces)
			continue
		}
		if ps[0].Name != "base_floor" || ps[0].Pos[0] != c.cx*16+8 || ps[0].Pos[2] != c.cz*16+8 {
			t.Errorf("city %d %d starts with %s at %v", c.cx, c.cz, ps[0].Name, ps[0].Pos)
		}
		ship := false
		for _, p := range ps {
			if p.Name == "ship" {
				ship = true
				if p.Pos != [3]int32{-526, 119, -784} {
					t.Errorf("ship at %v, BDS has it at -526 119 -784", p.Pos)
				}
			}
		}
		if ship != c.ship {
			t.Errorf("city %d %d: ship %v, BDS %v", c.cx, c.cz, ship, c.ship)
		}
	}
	cache := NewCache(e, 64)
	var ch Chunk
	cache.Generate(-34, -55, &ch)
	for _, be := range ch.BlockEntities {
		if be.X == -530 && be.Y == 101 && be.Z == -877 {
			if s, _ := be.Data["LootTableSeed"].(int32); s != 1019559790 {
				t.Errorf("chest loot seed %d, BDS has 1019559790", s)
			}
			return
		}
	}
	t.Errorf("no chest at -530 101 -877")
}
