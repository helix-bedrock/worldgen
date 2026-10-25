package overworld

import (
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"os"
	"slices"
	"strings"
	"testing"
)

// TestStructureFingerprint hashes the chunks around one structure of every
// kind, one hash per kind, so a refactor that must not change the output
// can be checked structure by structure. WORLDGEN_STRUCT_FINGERPRINT names
// the file: it is written when missing and compared otherwise.
func TestStructureFingerprint(t *testing.T) {
	file := os.Getenv("WORLDGEN_STRUCT_FINGERPRINT")
	if file == "" {
		t.Skip("WORLDGEN_STRUCT_FINGERPRINT not set")
	}
	reg, ids := testRegistry(t)
	o, err := NewOverworld(236621124415672384, reg, ids)
	if err != nil {
		t.Fatal(err)
	}
	type spot struct {
		name string
		x, z int32
		ok   bool
	}
	var spots []spot
	add := func(name string, s Scattered, ok bool) { spots = append(spots, spot{name, s.X, s.Z, ok}) }
	for _, k := range []string{"plains", "desert", "savanna", "taiga", "snowy"} {
		x, _, z, ok := o.NearestVillage(0, 0, k, 40)
		spots = append(spots, spot{"village_" + k, x, z, ok})
	}
	if c, ok := o.NearestAncientCity(0, 0, 10); ok {
		spots = append(spots, spot{"ancient_city", c.X, c.Z, true})
	}
	for _, k := range ScatteredKinds {
		s, ok := o.NearestScattered(0, 0, k, 40)
		add(k, s, ok)
	}
	for _, k := range []string{"trail_ruins", "trial_chambers", "abandoned_camp"} {
		s, ok := o.NearestDataJigsaw(k, 0, 0, 20)
		add(k, s, ok)
	}
	s, ok := o.NearestOceanRuin(0, 0, 20)
	add("ocean_ruin", s, ok)
	s, ok = o.NearestShipwreck(0, 0, 20)
	add("shipwreck", s, ok)
	s, ok = o.NearestOutpost(0, 0, 10)
	add("outpost", s, ok)
	s, ok = o.NearestMansion(0, 0, 10)
	add("mansion", s, ok)
	s, ok = o.NearestMonument(0, 0, 20)
	add("monument", s, ok)
	s, ok = o.NearestRuinedPortal(0, 0, 10)
	add("ruined_portal", s, ok)
	s, ok = o.NearestStronghold(0, 0, 3)
	add("stronghold", s, ok)
	s, ok = o.NearestIglooBasement(0, 0, 40)
	add("igloo_basement", s, ok)
	s, ok = o.NearestBuriedTreasure(0, 0, 20)
	add("buried_treasure", s, ok)
	if m, ok := o.NearestMineshaft(0, 0, 60); ok {
		spots = append(spots, spot{"mineshaft", m.X, m.Z, true})
	}

	cache := NewTerrainCache(o, 256)
	var c Chunk
	var lines []string
	for _, sp := range spots {
		if !sp.ok {
			lines = append(lines, sp.name+" none")
			continue
		}
		h := fnv.New64a()
		var buf []byte
		for dx := int32(-1); dx <= 1; dx++ {
			for dz := int32(-1); dz <= 1; dz++ {
				cache.Generate(sp.x>>4+dx, sp.z>>4+dz, &c)
				buf = buf[:0]
				for _, b := range c.Blocks {
					buf = binary.LittleEndian.AppendUint16(buf, b)
				}
				for _, w := range c.Water {
					buf = binary.LittleEndian.AppendUint64(buf, w)
				}
				h.Write(buf)
				for _, be := range c.BlockEntities {
					_, _ = fmt.Fprintf(h, "%d %d %d %s %v;", be.X, be.Y, be.Z, be.LootTable, be.Data)
				}
			}
		}
		lines = append(lines, fmt.Sprintf("%s %d %d %x", sp.name, sp.x, sp.z, h.Sum64()))
	}
	got := strings.Join(lines, "\n") + "\n"
	want, err := os.ReadFile(file)
	switch {
	case os.IsNotExist(err):
		if err := os.WriteFile(file, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("saved structure fingerprints:\n%s", got)
	case err != nil:
		t.Fatal(err)
	case string(want) != got:
		w := strings.Split(string(want), "\n")
		for _, l := range strings.Split(got, "\n") {
			if !slices.Contains(w, l) {
				t.Errorf("changed: %s", l)
			}
		}
	}
}
