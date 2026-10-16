package worldgen

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"testing"
)

// TestVillagesMatchBDS checks village starts against BDS 1.26.52 for seed
// 236621124415672384. testdata/bds_villages.txt lists the blocks
// (chunk*16+8) BDS's "locate structure village" reported, from a grid of
// spots and from each start this generator found near them; BDS reported
// every one of the latter at 0 blocks away.
func TestVillagesMatchBDS(t *testing.T) {
	f, err := os.Open("testdata/bds_villages.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	reg, ids := testRegistry(t)
	o, err := NewOverworld(236621124415672384, reg, ids)
	if err != nil {
		t.Fatal(err)
	}
	n, bad := 0, 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		p := strings.Fields(sc.Text())
		if len(p) != 2 {
			continue
		}
		x, _ := strconv.Atoi(p[0])
		z, _ := strconv.Atoi(p[1])
		n++
		if !o.IsVillageChunk(int32(x)>>4, int32(z)>>4) {
			bad++
			t.Errorf("BDS has a village in chunk (%d, %d), we do not", x>>4, z>>4)
		}
	}
	t.Logf("%d of %d BDS villages match", n-bad, n)
}

// TestNearestVillage finds the BDS village at (184, 4008) from next to it,
// and a village of every type from the origin.
func TestNearestVillage(t *testing.T) {
	reg, ids := testRegistry(t)
	o, err := NewOverworld(236621124415672384, reg, ids)
	if err != nil {
		t.Fatal(err)
	}
	if x, _, z, ok := o.NearestVillage(190, 4000, "", 4); !ok || x != 184 || z != 4008 {
		t.Errorf("nearest village to (190, 4000): (%d, %d) %v, want (184, 4008)", x, z, ok)
	}
	for _, k := range []string{"plains", "desert", "savanna", "snowy", "taiga"} {
		x, y, z, ok := o.NearestVillage(0, 0, k, 40)
		if !ok {
			t.Errorf("no %s village", k)
			continue
		}
		t.Logf("%s village at %d %d %d", k, x, y, z)
	}
}
