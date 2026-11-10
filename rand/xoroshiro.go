package rand

import (
	"crypto/md5" //nolint:gosec // vanilla seeds noises from the MD5 of their names; not security
	"encoding/binary"
	"math/bits"
)

const (
	goldenGamma = 0x9E3779B97F4A7C15
	silverRatio = 0x6A09E667F3BCC909
)

// Xoroshiro is a Xoroshiro128++ generator. A zero state is replaced by a
// fixed non-zero one, because an all-zero state would only ever return 0.
// An Xoroshiro must not be shared between goroutines.
type Xoroshiro struct {
	lo, hi uint64
}

// NewXoroshiro returns a generator with the 128-bit state (lo, hi).
func NewXoroshiro(lo, hi uint64) Xoroshiro {
	if lo|hi == 0 {
		lo, hi = goldenGamma, silverRatio
	}
	return Xoroshiro{lo: lo, hi: hi}
}

// Seed128 expands a 64-bit world seed into the 128-bit state the noise
// generators start from.
func Seed128(seed int64) (lo, hi uint64) {
	l := uint64(seed) ^ silverRatio
	h := l + goldenGamma
	return mix13(l), mix13(h)
}

func mix13(z uint64) uint64 {
	z = (z ^ z>>30) * 0xBF58476D1CE4E5B9
	z = (z ^ z>>27) * 0x94D049BB133111EB
	return z ^ z>>31
}

// Uint64 returns the next 64-bit output.
func (r *Xoroshiro) Uint64() uint64 {
	l, h := r.lo, r.hi
	out := bits.RotateLeft64(l+h, 17) + l
	h ^= l
	r.lo = bits.RotateLeft64(l, 49) ^ h ^ h<<21
	r.hi = bits.RotateLeft64(h, 28)
	return out
}

// Float returns a value in [0, 1) from the top 24 bits.
func (r *Xoroshiro) Float() float32 {
	return float32(r.Uint64()>>40) * (1.0 / (1 << 24))
}

// Double returns a value in [0, 1) from the top 53 bits.
func (r *Xoroshiro) Double() float64 {
	return float64(r.Uint64()>>11) * (1.0 / (1 << 53))
}

// IntN returns an unbiased value in [0, n) using the multiply-and-reject
// method. It returns 0 when n <= 0.
// VERIFY: the bounded method Bedrock uses for permutation shuffles.
func (r *Xoroshiro) IntN(n int32) int32 {
	if n <= 0 {
		return 0
	}
	bound := uint64(n)
	l := uint64(uint32(r.Uint64()))
	m := l * bound
	low := m & 0xFFFFFFFF
	if low < bound {
		threshold := uint64(uint32(-n) % uint32(n))
		for low < threshold {
			l = uint64(uint32(r.Uint64()))
			m = l * bound
			low = m & 0xFFFFFFFF
		}
	}
	return int32(m >> 32)
}

// Fork returns a positional factory seeded from the next two outputs.
func (r *Xoroshiro) Fork() Positional {
	lo := r.Uint64()
	hi := r.Uint64()
	return Positional{lo: lo, hi: hi}
}

// Positional derives independent generators from a 128-bit seed, either by
// name or by block position.
type Positional struct {
	lo, hi uint64
}

// NewPositional returns a factory with the 128-bit state (lo, hi).
func NewPositional(lo, hi uint64) Positional {
	return Positional{lo: lo, hi: hi}
}

// FromHash returns the generator for a name: the MD5 of the name, read as two
// big-endian 64-bit halves, XORed into the factory state.
func (p Positional) FromHash(name string) Xoroshiro {
	sum := md5.Sum([]byte(name)) //nolint:gosec // see the import
	return NewXoroshiro(binary.BigEndian.Uint64(sum[:8])^p.lo, binary.BigEndian.Uint64(sum[8:])^p.hi)
}

// At returns the generator for a block position.
func (p Positional) At(x, y, z int32) Xoroshiro {
	return NewXoroshiro(uint64(PositionSeed(x, y, z))^p.lo, p.hi)
}

// PositionSeed is the classic position hash used for per-block randomness.
func PositionSeed(x, y, z int32) int64 {
	h := int64(x*3129871) ^ int64(z)*116129781 ^ int64(y)
	h = h*h*42317861 + h*11
	return h >> 16
}
