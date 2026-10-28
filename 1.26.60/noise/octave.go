package noise

import (
	"strconv"

	"github.com/helix-bedrock/worldgen/1.26.60/rand"
)

// Octaves is a sum of Perlin octaves. Octave i has frequency 2^(first+i) and
// weight amp[i] * 2^(n-1-i) / (2^n - 1). An octave with amplitude 0 is
// skipped and has no random state.
type Octaves struct {
	octaves  []*Perlin
	amps     []float32
	inFirst  float32
	valFirst float32
}

// NewOctaves builds the octaves from a positional factory: each octave is
// seeded from the name "octave_<first+i>".
func NewOctaves(pf rand.Positional, first int, amps []float32) *Octaves {
	o := &Octaves{octaves: make([]*Perlin, len(amps)), amps: amps}
	for i, a := range amps {
		if a == 0 {
			continue
		}
		r := pf.FromHash("octave_" + strconv.Itoa(first+i))
		o.octaves[i] = NewPerlin(&r)
	}
	o.inFirst = pow2(first)
	n := len(amps)
	o.valFirst = pow2(n-1) / (pow2(n) - 1)
	return o
}

func pow2(e int) float32 {
	v := float32(1)
	for ; e > 0; e-- {
		v *= 2
	}
	for ; e < 0; e++ {
		v /= 2
	}
	return v
}

// Sample returns the octave sum at (x, y, z).
func (o *Octaves) Sample(x, y, z float32) float32 {
	var sum float32
	in, val := o.inFirst, o.valFirst
	for i, n := range o.octaves {
		if n != nil {
			sum += o.amps[i] * n.Sample(x*in, y*in, z*in) * val
		}
		in *= 2
		val /= 2
	}
	return sum
}

// secondInputFactor scales the input of the second octave set of a Normal
// noise so the two sets do not line up.
const secondInputFactor = 1.0181268

// Normal is two octave sets with the same settings, sampled at slightly
// different scales and summed. Each named climate and terrain noise is one.
// It is immutable after creation and safe for concurrent use.
type Normal struct {
	a, b        *Octaves
	valueFactor float32
}

// NewNormal builds a Normal noise from the named random r: the two octave
// sets each take a positional factory forked from r, A first.
func NewNormal(r *rand.Xoroshiro, first int, amps []float32) *Normal {
	a := NewOctaves(r.Fork(), first, amps)
	b := NewOctaves(r.Fork(), first, amps)
	lo, hi := len(amps), -1
	for i, v := range amps {
		if v != 0 {
			lo = min(lo, i)
			hi = max(hi, i)
		}
	}
	span := 2
	if hi >= lo {
		span = hi - lo + 1
	}
	vf := float32(1.0/6.0) / (0.1 * (1 + 1/float32(span)))
	return &Normal{a: a, b: b, valueFactor: vf}
}

// Sample returns the noise at (x, y, z).
func (n *Normal) Sample(x, y, z float32) float32 {
	const k = secondInputFactor
	return (n.a.Sample(x, y, z) + n.b.Sample(x*k, y*k, z*k)) * n.valueFactor
}
