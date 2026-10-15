package worldgen

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/helix-bedrock/worldgen/biome"
	"github.com/helix-bedrock/worldgen/blocks"
)

// testRegistry loads the block list from WORLDGEN_DATA (a folder holding
// block_palette.nbt and block_types.json). The test is skipped without it.
func testRegistry(t testing.TB) (*blocks.Registry, map[string]uint32) {
	// testdata holds the BDS 1.26.52 block and biome lists (Endstone
	// export); WORLDGEN_DATA points at another export instead.
	dir := os.Getenv("WORLDGEN_DATA")
	if dir == "" {
		dir = "testdata"
	}
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
	if lf, err := os.Open(filepath.Join(dir, "block_light.json")); err == nil {
		err = reg.LoadLight(lf)
		_ = lf.Close()
		if err != nil {
			t.Fatal(err)
		}
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
	return reg, ids
}

func TestGenerate(t *testing.T) {
	reg, ids := testRegistry(t)
	o, err := NewOverworld(12345, reg, ids)
	if err != nil {
		t.Fatal(err)
	}
	var c Chunk
	o.GenerateChunk(0, 0, &c)
	solid := 0
	for _, b := range c.Blocks {
		if uint32(b) != o.ids.air && uint32(b) != o.ids.water {
			solid++
		}
	}
	if solid == 0 {
		t.Fatal("chunk has no solid blocks")
	}
	if c.Block(0, MinY, 0) != o.ids.bedrock {
		t.Fatal("no bedrock at the bottom")
	}
	for x := 0; x < 16; x += 5 {
		top := MinY
		for y := MaxY - 1; y > MinY; y-- {
			if b := c.Block(x, y, 0); b != o.ids.air && b != o.ids.water {
				top = y
				break
			}
		}
		t.Logf("x=%d top=%d biome=%d", x, top, c.Biome(x, top, 0))
	}
}

func BenchmarkGenerate(b *testing.B) {
	reg, ids := testRegistry(b)
	o, err := NewOverworld(12345, reg, ids)
	if err != nil {
		b.Fatal(err)
	}
	var c Chunk
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		o.GenerateChunk(int32(i), 0, &c)
	}
}

// BenchmarkDecorated generates finished chunks in a row, the way a player
// walking loads them, through a terrain cache.
func BenchmarkDecorated(b *testing.B) {
	reg, ids := testRegistry(b)
	o, err := NewOverworld(12345, reg, ids)
	if err != nil {
		b.Fatal(err)
	}
	cache := NewTerrainCache(o, 256)
	var c Chunk
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		cache.Generate(int32(i), 0, &c)
	}
}

// BenchmarkDecorate times the feature step alone, with the neighbours'
// terrain already cached.
func BenchmarkDecorate(b *testing.B) {
	reg, ids := testRegistry(b)
	o, err := NewOverworld(12345, reg, ids)
	if err != nil {
		b.Fatal(err)
	}
	cache := NewTerrainCache(o, 256)
	cache.Generate(0, 0, new(Chunk))
	var c Chunk
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		cache.Get(0, 0).Unpack(&c)
		o.Decorate(0, 0, &c, cache.Get)
	}
}
