package structure

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"testing"
)

// leCompound writes a little-endian NBT root compound holding one int
// list "block_indices" entry, enough to tell the structure compound apart.
func leCompound(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	str := func(s string) {
		_ = binary.Write(&b, binary.LittleEndian, uint16(len(s)))
		b.WriteString(s)
	}
	b.WriteByte(10) // compound
	str("")
	b.WriteByte(3) // int
	str("marker")
	_ = binary.Write(&b, binary.LittleEndian, int32(42))
	b.WriteByte(0)
	return b.Bytes()
}

func TestMCStructureCompression(t *testing.T) {
	plain := map[string]any{"structure": map[string]any{"marker": int32(42)}}
	if st, err := mcStructureBody(plain); err != nil || st["marker"] != int32(42) {
		t.Fatalf("plain: %v %v", st, err)
	}
	var z bytes.Buffer
	w := zlib.NewWriter(&z)
	_, _ = w.Write(leCompound(t))
	_ = w.Close()
	packed := map[string]any{"compression": int32(1), "structure": z.Bytes()}
	if st, err := mcStructureBody(packed); err != nil || st["marker"] != int32(42) {
		t.Fatalf("zlib: %v %v", st, err)
	}
	for name, root := range map[string]map[string]any{
		"bad level":    {"compression": int32(2), "structure": map[string]any{}},
		"not an int":   {"compression": "1", "structure": map[string]any{}},
		"not zlib":     {"compression": int32(1), "structure": []byte{1, 2, 3}},
		"no bytes":     {"compression": int32(1), "structure": map[string]any{}},
		"no structure": {},
	} {
		if _, err := mcStructureBody(root); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

// FuzzMCStructureCompressed feeds arbitrary bytes as a compressed
// structure: it must return an error or a compound, never panic.
func FuzzMCStructureCompressed(f *testing.F) {
	var z bytes.Buffer
	w := zlib.NewWriter(&z)
	_, _ = w.Write([]byte{10, 0, 0, 0})
	_ = w.Close()
	f.Add(z.Bytes())
	f.Add([]byte{0x78, 0x9c})
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = mcStructureBody(map[string]any{"compression": int32(1), "structure": b})
	})
}
