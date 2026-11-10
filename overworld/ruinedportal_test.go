package overworld

import (
	"fmt"
	"os"
	"testing"
)

// TestListRuinedPortals prints the ruined portals starting within
// WORLDGEN_PORTAL_R chunks of the origin for the BDS comparison seed, with
// the block BDS's "locate structure" reports (chunk*16+8), the template,
// turn, mirror and box.
func TestListRuinedPortals(t *testing.T) {
	r := os.Getenv("WORLDGEN_PORTAL_R")
	if r == "" {
		t.Skip("set WORLDGEN_PORTAL_R")
	}
	var radius int32
	_, _ = fmt.Sscan(r, &radius)
	reg, ids := testRegistry(t)
	o, err := NewOverworld(236621124415672384, reg, ids)
	if err != nil {
		t.Fatal(err)
	}
	tc := NewTerrainCache(o, 256)
	for cx := -radius; cx <= radius; cx++ {
		for cz := -radius; cz <= radius; cz++ {
			p := o.portalAt(cx, cz, tc.Raw)
			if p == nil {
				continue
			}
			n := 0
			for _, bs := range p.Blocks {
				n += len(bs)
			}
			fmt.Printf("portal %d %d %s under=%v rot=%d mirror=%v box=%v blocks=%d\n",
				cx*16+8, cz*16+8, p.Path, p.Under, p.Rot, p.Mirror, p.Box, n)
		}
	}
}
