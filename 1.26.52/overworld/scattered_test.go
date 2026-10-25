package overworld

import (
	"fmt"
	"os"
	"testing"
)

// TestListScattered prints the scattered features starting within
// WORLDGEN_SCATTER_R chunks of the origin for the BDS comparison seed, with
// the block BDS's "locate structure temple" reports (chunk*16+8).
func TestListScattered(t *testing.T) {
	r := os.Getenv("WORLDGEN_SCATTER_R")
	if r == "" {
		t.Skip("set WORLDGEN_SCATTER_R")
	}
	var radius int32
	_, _ = fmt.Sscan(r, &radius)
	reg, ids := testRegistry(t)
	o, err := NewOverworld(236621124415672384, reg, ids)
	if err != nil {
		t.Fatal(err)
	}
	raw := map[[2]int32]*PackedChunk{}
	get := func(cx, cz int32) *PackedChunk {
		k := [2]int32{cx, cz}
		if p, ok := raw[k]; ok {
			return p
		}
		var c Chunk
		o.GenerateChunk(cx, cz, &c)
		p := Pack(&c)
		raw[k] = p
		return p
	}
	names := [...]string{"", "desert_pyramid", "jungle_temple", "swamp_hut", "igloo"}
	for cx := -radius; cx <= radius; cx++ {
		for cz := -radius; cz <= radius; cz++ {
			if k := o.scatteredAt(cx, cz); k != scatterNone {
				fmt.Printf("%s %d %d y=%d\n", names[k], cx*16+8, cz*16+8, o.scatteredY(k, cx, cz, get(cx, cz)))
			}
		}
	}
}
