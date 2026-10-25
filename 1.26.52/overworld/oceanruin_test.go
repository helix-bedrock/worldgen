package overworld

import (
	"fmt"
	"os"
	"testing"
)

// TestListOceanRuins prints the ocean ruins and monuments starting within
// WORLDGEN_RUIN_R chunks of the origin for the BDS comparison seed, with
// the block BDS's "locate structure" reports (chunk*16+8), and each ruin's
// pieces.
func TestListOceanRuins(t *testing.T) {
	r := os.Getenv("WORLDGEN_RUIN_R")
	if r == "" {
		t.Skip("set WORLDGEN_RUIN_R")
	}
	var radius int32
	_, _ = fmt.Sscan(r, &radius)
	reg, ids := testRegistry(t)
	o, err := NewOverworld(236621124415672384, reg, ids)
	if err != nil {
		t.Fatal(err)
	}
	for cx := -radius; cx <= radius; cx++ {
		for cz := -radius; cz <= radius; cz++ {
			if o.isMonumentChunk(cx, cz) {
				fmt.Printf("monument %d %d\n", cx*16+8, cz*16+8)
			}
			cfg := o.ruinAt(cx, cz)
			if !cfg.ok {
				continue
			}
			fmt.Printf("ruin %d %d warm=%v\n", cx*16+8, cz*16+8, cfg.warm)
			for _, p := range o.ruinPieces(cx, cz, cfg) {
				fmt.Printf("  %s %d %d rot=%d integrity=%.1f\n", p.path, p.x, p.z, p.rot, p.integrity)
			}
		}
	}
}
