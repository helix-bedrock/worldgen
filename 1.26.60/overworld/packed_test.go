package overworld

import "testing"

// TestPackRoundTrip checks that a packed chunk reads and unpacks to the
// chunk it was made from, including a sub-chunk with more than 256 states.
func TestPackRoundTrip(t *testing.T) {
	reg, ids := testRegistry(t)
	o, err := NewOverworld(12345, reg, ids)
	if err != nil {
		t.Fatal(err)
	}
	c := new(Chunk)
	o.GenerateChunk(3, -7, c)
	for i := range 300 {
		c.Blocks[5<<12+i*7] = uint16(i + 1)
	}
	p := Pack(c)
	if p.subs[5].wide == nil {
		t.Fatal("sub-chunk with 300 states is not stored wide")
	}
	var u Chunk
	p.Unpack(&u)
	for i := range c.Blocks {
		if got := p.at(i); got != uint32(c.Blocks[i]) {
			t.Fatalf("at(%d) = %d, want %d", i, got, c.Blocks[i])
		}
		if u.Blocks[i] != c.Blocks[i] {
			t.Fatalf("unpacked block %d = %d, want %d", i, u.Blocks[i], c.Blocks[i])
		}
	}
	if u.Biomes != c.Biomes {
		t.Fatal("unpacked biomes differ")
	}
}
