package noise

import "github.com/helix-bedrock/worldgen/1.26.52/rand"

// newPerlinMT draws a Perlin octave from a Mersenne Twister the way BDS's
// legacy-random octave builder does (sub_D23FD60, mode 0): three nextDouble
// origins, then a forward shuffle with nextInt(256 - i).
func newPerlinMT(r *rand.MT) *Perlin {
	n := &Perlin{}
	n.ox = float32(r.Float64() * 256)
	n.oy = float32(r.Float64() * 256)
	n.oz = float32(r.Float64() * 256)
	for i := range 256 {
		n.p[i] = uint8(i)
	}
	for i := range 256 {
		j := i + int(r.IntN(int32(256-i)))
		n.p[i], n.p[j] = n.p[j], n.p[i]
	}
	return n
}

// newOctavesMT builds octaves from a Mersenne Twister: the octave at
// -first is drawn first, then octaves -first-1 down to 0, each drawn when
// it has an amplitude and skipped (262 raw draws) otherwise.
func newOctavesMT(r *rand.MT, first int, amps []float32) *Octaves {
	n := len(amps)
	o := &Octaves{octaves: make([]*Perlin, n), amps: amps}
	k := -first
	p0 := newPerlinMT(r)
	for l := k - 1; l >= 0; l-- {
		if l < n && amps[l] != 0 {
			o.octaves[l] = newPerlinMT(r)
			continue
		}
		for range 262 {
			r.Uint32()
		}
	}
	if k >= 0 && k < n && amps[k] != 0 {
		o.octaves[k] = p0
	}
	o.inFirst = pow2(first)
	o.valFirst = pow2(n-1) / (pow2(n) - 1)
	return o
}

// NewNormalMT builds a Normal noise from a Mersenne Twister, as BDS does for
// features that take the feature random (sub_D2411F0, used by geodes): both
// octave sets come from r in turn.
func NewNormalMT(r *rand.MT, first int, amps []float32) *Normal {
	a := newOctavesMT(r, first, amps)
	b := newOctavesMT(r, first, amps)
	lo, hi := len(amps), -1
	for i, v := range amps {
		if v != 0 {
			lo = min(lo, i)
			hi = max(hi, i)
		}
	}
	span := float32(2)
	if hi >= lo {
		span = float32(hi-lo) + 1
	}
	return &Normal{a: a, b: b, valueFactor: 0.16666667 / float32((1/span+1)*0.1)}
}
