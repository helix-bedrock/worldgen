// Package rand holds the random number generators vanilla Bedrock world
// generation draws from: the Mersenne Twister used for per-chunk randomness,
// and Xoroshiro128++ used to seed noise and for positional randomness.
package rand

const (
	mtN       = 624
	mtM       = 397
	mtUpper   = 0x80000000
	mtLower   = 0x7fffffff
	mtMatrixA = 0x9908b0df
)

// MT is a 32-bit Mersenne Twister (MT19937). Bedrock seeds one per chunk for
// things such as the bedrock floor. An MT must not be shared between
// goroutines; the zero value acts as seeded with 0.
//
// World generation reseeds an MT for every feature rule and usually draws
// only a few numbers, so the first round is computed lazily: a seed word is
// filled in and a word twisted only when a draw needs it. The outputs are
// the same as an eager MT19937's.
type MT struct {
	state   [mtN]uint32
	index   int // next output
	inited  int // seed words filled in
	twisted int // words twisted in the current round
}

// NewMT returns a Mersenne Twister seeded with seed.
func NewMT(seed uint32) *MT {
	m := &MT{}
	m.Seed(seed)
	return m
}

// Seed resets the generator to the state for seed.
func (m *MT) Seed(seed uint32) {
	m.state[0] = seed
	m.index, m.inited, m.twisted = 0, 1, 0
}

// Uint32 returns the next raw 32-bit output.
func (m *MT) Uint32() uint32 {
	if m.index >= m.twisted {
		if m.index >= mtN {
			m.twist()
		} else {
			m.twistNext()
		}
	}
	y := m.state[m.index]
	m.index++
	y ^= y >> 11
	y ^= (y << 7) & 0x9d2c5680
	y ^= (y << 15) & 0xefc60000
	y ^= y >> 18
	return y
}

// twistNext twists the next word of the first round, filling in the seed
// words it reads first. Twisting in place in order reads the already
// twisted words exactly as the eager loop does.
func (m *MT) twistNext() {
	i := m.twisted
	need := min(i+mtM, mtN-1)
	if i >= mtN-mtM {
		need = mtN - 1
	}
	if m.inited == 0 {
		m.inited = 1
	}
	for m.inited <= need {
		prev := m.state[m.inited-1]
		m.state[m.inited] = 1812433253*(prev^(prev>>30)) + uint32(m.inited)
		m.inited++
	}
	y := m.state[i]&mtUpper | m.state[(i+1)%mtN]&mtLower
	v := m.state[(i+mtM)%mtN] ^ y>>1
	if y&1 != 0 {
		v ^= mtMatrixA
	}
	m.state[i] = v
	m.twisted++
}

func (m *MT) twist() {
	for i := range mtN {
		y := m.state[i]&mtUpper | m.state[(i+1)%mtN]&mtLower
		v := m.state[(i+mtM)%mtN] ^ y>>1
		if y&1 != 0 {
			v ^= mtMatrixA
		}
		m.state[i] = v
	}
	m.index, m.twisted = 0, mtN
}

// IntN returns a value in [0, n) the way Bedrock does: the raw output modulo
// n, which is slightly biased but must be kept to match vanilla. It returns 0
// when n <= 0.
func (m *MT) IntN(n int32) int32 {
	if n <= 0 {
		return 0
	}
	return int32(m.Uint32() % uint32(n))
}

// Float returns a value in [0, 1): the raw output scaled by 2^-32.
func (m *MT) Float() float32 {
	return float32(float64(m.Uint32()) * (1.0 / 4294967296.0))
}

// ChunkSeed is the seed Bedrock gives the per-chunk Mersenne Twister. The
// world seed is not part of it, so the bedrock floor pattern is the same in
// every world.
func ChunkSeed(chunkX, chunkZ int32) uint32 {
	return uint32(341872712*chunkX + 132899541*chunkZ)
}
