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
