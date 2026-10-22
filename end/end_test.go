package end

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/helix-bedrock/worldgen/biome"
	"github.com/helix-bedrock/worldgen/blocks"
)

func testEnd(t testing.TB, seed int64) *End {
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
	e, err := New(seed, reg, ids)
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
