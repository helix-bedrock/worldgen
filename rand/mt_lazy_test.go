package rand

import "testing"

// eagerMT is the textbook MT19937, the reference for the lazy MT.
type eagerMT struct {
	s [mtN]uint32
	i int
}

func (m *eagerMT) seed(v uint32) {
	m.s[0] = v
	for i := 1; i < mtN; i++ {
		m.s[i] = 1812433253*(m.s[i-1]^(m.s[i-1]>>30)) + uint32(i)
	}
	m.i = mtN
}

func (m *eagerMT) next() uint32 {
	if m.i >= mtN {
		for i := range mtN {
			y := m.s[i]&mtUpper | m.s[(i+1)%mtN]&mtLower
			v := m.s[(i+mtM)%mtN] ^ y>>1
			if y&1 != 0 {
				v ^= mtMatrixA
			}
			m.s[i] = v
		}
		m.i = 0
	}
	y := m.s[m.i]
	m.i++
	y ^= y >> 11
	y ^= (y << 7) & 0x9d2c5680
	y ^= (y << 15) & 0xefc60000
	y ^= y >> 18
	return y
}

// TestLazyMTMatchesEager checks the lazy first round against the eager
// generator over three rounds, reseeding the same MT between seeds.
func TestLazyMTMatchesEager(t *testing.T) {
	var lazy MT
	for _, seed := range []uint32{0, 1, 5489, 0xdeadbeef, 0xffffffff} {
		var ref eagerMT
		ref.seed(seed)
		lazy.Seed(seed)
		for n := range 3 * mtN {
			if got, want := lazy.Uint32(), ref.next(); got != want {
				t.Fatalf("seed %d draw %d: got %d, want %d", seed, n, got, want)
			}
		}
	}
	// The first output of the default seed, from the MT19937 reference.
	if got := NewMT(5489).Uint32(); got != 3499211612 {
		t.Fatalf("first output of seed 5489 = %d, want 3499211612", got)
	}
}
