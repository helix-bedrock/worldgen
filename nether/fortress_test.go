package nether

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/helix-bedrock/worldgen/biome"
	"github.com/helix-bedrock/worldgen/blocks"
	"github.com/helix-bedrock/worldgen/overworld"
	"github.com/helix-bedrock/worldgen/rand"
)

func testNether(t testing.TB, seed int64) *Nether {
	t.Helper()
	// The overworld's testdata holds the BDS 1.26.52 block and biome lists.
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
	ow, err := overworld.NewOverworld(seed, reg, ids)
	if err != nil {
		t.Fatal(err)
	}
	n, err := New(seed, reg, ids, ow)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// TestFortressLayout pins a fortress whose blocks were compared with a BDS
// world of the same seed: every nether brick, fence, stairs, chest and
// spawner in the 110 chunks around it matched.
func TestFortressLayout(t *testing.T) {
	n := testNether(t, 236621124415672384)
	var r rand.MT
	cx, cz := n.complexStart(0, 0, &r)
	f := n.fortressAt(cx, cz)
	if f == nil {
		t.Fatalf("no fortress at chunk %d %d", cx, cz)
	}
	if want := (box{322, 55, 50, 340, 64, 68}); f.pieces[0].bb != want {
		t.Errorf("start box %v, want %v", f.pieces[0].bb, want)
	}
	if want := (box{244, 48, -42, 423, 80, 172}); f.bb != want {
		t.Errorf("fortress box %v, want %v", f.bb, want)
	}
	if len(f.pieces) != 145 {
		t.Errorf("%d pieces, want 145", len(f.pieces))
	}
	x, y, z, ok := n.NearestFortress(330, 60, 2)
	if !ok || x != 331 || y != 60 || z != 59 {
		t.Errorf("NearestFortress = %d %d %d %v, want 331 60 59", x, y, z, ok)
	}
}
