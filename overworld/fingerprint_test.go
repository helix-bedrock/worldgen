package overworld

import (
	"encoding/binary"
	"hash/fnv"
	"os"
	"strconv"
	"testing"
)

// TestFingerprint hashes a few finished chunks of the BDS comparison seed,
// to prove a change (an optimization) leaves the output unchanged. Set
// WORLDGEN_FINGERPRINT to a file: the first run saves the hash, later runs
// fail if it differs. Delete the file after an intended output change.
func TestFingerprint(t *testing.T) {
	file := os.Getenv("WORLDGEN_FINGERPRINT")
	if file == "" {
		t.Skip("WORLDGEN_FINGERPRINT not set")
	}
	reg, ids := testRegistry(t)
	o, err := NewOverworld(236621124415672384, reg, ids)
	if err != nil {
		t.Fatal(err)
	}
	cache := NewTerrainCache(o, 256)
	h := fnv.New64a()
	var c Chunk
	var buf []byte
	// Ocean, mountains, jungle, the origin, a forest and a plain.
	for _, p := range [][2]int32{{-41, 31}, {-213, -243}, {162, -180}, {0, 0}, {-110, -78}, {33, 5}} {
		cache.Generate(p[0], p[1], &c)
		buf = buf[:0]
		for _, b := range c.Blocks {
			buf = binary.LittleEndian.AppendUint16(buf, b)
		}
		for _, w := range c.Water {
			buf = binary.LittleEndian.AppendUint64(buf, w)
		}
		h.Write(buf)
	}
	got := strconv.FormatUint(h.Sum64(), 16)
	want, err := os.ReadFile(file)
	switch {
	case os.IsNotExist(err):
		if err := os.WriteFile(file, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("saved fingerprint %s", got)
	case err != nil:
		t.Fatal(err)
	case string(want) != got:
		t.Fatalf("fingerprint %s, saved %s: the output changed", got, want)
	}
}
