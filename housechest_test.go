package worldgen

import (
	"strings"
	"testing"
)

// TestHouseChests checks that WithHouseChests adds chests to the houses of
// the plains village at (184, 4008) and that its pick limits them.
func TestHouseChests(t *testing.T) {
	reg, ids := testRegistry(t)
	count := func(opts ...Option) int {
		o, err := NewOverworld(236621124415672384, reg, ids, opts...)
		if err != nil {
			t.Fatal(err)
		}
		cache := NewTerrainCache(o, 1024)
		n := 0
		var c Chunk
		for cx := int32(184>>4) - 6; cx <= 184>>4+6; cx++ {
			for cz := int32(4008>>4) - 6; cz <= 4008>>4+6; cz++ {
				cache.Generate(cx, cz, &c)
				for _, be := range c.BlockEntities {
					if be.Data["id"] == "Chest" {
						n++
					}
				}
			}
		}
		return n
	}
	vanilla := count()
	all := count(WithHouseChests(nil))
	small := count(WithHouseChests(func(h string) bool { return strings.Contains(h, "small_house") }))
	t.Logf("chests: vanilla %d, every house %d, small houses only %d", vanilla, all, small)
	if all <= vanilla {
		t.Errorf("WithHouseChests added no chests (%d vs %d)", all, vanilla)
	}
	if small < vanilla || small > all {
		t.Errorf("picking small houses gave %d chests, want between %d and %d", small, vanilla, all)
	}
}
