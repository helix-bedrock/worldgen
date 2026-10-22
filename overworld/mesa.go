package overworld

import (
	"math"

	"github.com/helix-bedrock/worldgen/internal/mathx"

	"github.com/helix-bedrock/worldgen/noise"
	"github.com/helix-bedrock/worldgen/rand"
)

// Band colours index the 16 dye colours of terracotta; bandPlain is plain
// terracotta (the biome's hard_clay_material).
const (
	bandWhite     = 0
	bandOrange    = 1
	bandYellow    = 4
	bandLightGray = 8
	bandBrown     = 12
	bandRed       = 14
	bandPlain     = 16
	bandCount     = 192
)

type mesaNoises struct {
	offset, surface, pillar, roof *noise.Normal
	bands                         [bandCount]uint8
}

func newMesaNoises(seed uint32, small rand.Positional) mesaNoises {
	var m mesaNoises
	off, surf := small.FromHash("minecraft:clay_bands_offset"), small.FromHash("minecraft:badlands_surface")
	pil, roof := small.FromHash("minecraft:badlands_pillar"), small.FromHash("minecraft:badlands_pillar_roof")
	m.offset = noise.NewNormal(&off, -8, []float32{1})
	m.surface = noise.NewNormal(&surf, -6, []float32{1, 1, 1})
	m.pillar = noise.NewNormal(&pil, -2, []float32{1, 1, 1, 1})
	m.roof = noise.NewNormal(&roof, -8, []float32{1})
	m.buildBands(rand.NewMT(seed))
	return m
}

// buildBands lays out the terracotta colour bands, drawing in vanilla's
// order: orange, yellow, brown, red, then white with light-gray edges.
func (m *mesaNoises) buildBands(mt *rand.MT) {
	b := &m.bands
	for i := range b {
		b[i] = bandPlain
	}
	for i := int32(0); ; {
		p := i + mt.IntN(5) + 1
		if p > bandCount-1 {
			break
		}
		b[p] = bandOrange
		i = p + 1
	}
	strip := func(colour uint8, extra int32) {
		for n := mt.IntN(10) + 6; n > 0; n-- {
			w := mt.IntN(3) + extra
			s := mt.IntN(bandCount)
			for k := s; k <= s+min(w, bandCount-1-s); k++ {
				b[k] = colour
			}
		}
	}
	strip(bandYellow, 0)
	strip(bandBrown, 1)
	strip(bandRed, 0)
	nextBool := func() bool { return mt.Uint32()>>27&1 == 1 }
	n := mt.IntN(7) + 8
	p := int32(0)
	for k := int32(0); k <= n && p < bandCount; k++ {
		b[p] = bandWhite
		if p >= 2 && nextBool() {
			b[p-1] = bandLightGray
		}
		if p <= bandCount-2 && nextBool() {
			b[p+1] = bandLightGray
		}
		p += mt.IntN(16) + 4
	}
}

// bandAt is the band colour at local height ly (y - MinY): BDS indexes the
// table with the local height plus the rounded offset noise.
func (m *mesaNoises) bandAt(wx, ly, wz int32) uint8 {
	r := int32(float32(math.Round(float64(m.offset.Sample(float32(wx), 0, float32(wz)) * 4))))
	return m.bands[((ly+bandCount+r)%bandCount+bandCount)%bandCount]
}

func (o *Overworld) terracotta(b *biomeSurface, colour uint8) uint32 {
	if colour > 15 {
		return b.hardClay
	}
	return o.clayColours(b)[colour]
}

// clayColours is the 16-colour list: the dye-ordered terracottas when the
// biome's clay material is one of them, otherwise the clay material 16 times.
func (o *Overworld) clayColours(b *biomeSurface) *[16]uint32 {
	for _, t := range o.ids.terracotta {
		if t == b.clay {
			return &o.ids.terracotta
		}
	}
	return &b.clayOnly
}

// mesaSurface paints badlands, copied from BDS's MesaSurfaceBuilder: red
// sand tops, terracotta bands down the cliffs, bryce spikes and wooded
// plateaus. It walks the whole fill volume from the top down to the
// preliminary floor (inclusive) and stops painting after 15 stone blocks.
func (o *Overworld) mesaSurface(c *Chunk, mt *rand.MT, col *surfaceColumn, b *biomeSurface) {
	m := &o.surfNoise.mesa
	x, z := col.x, col.z
	wx, wz := float32(col.wx), float32(col.wz)
	var spike float32
	if b.pillars {
		e := min(mathx.AbsFloat(m.surface.Sample(wx, 0, wz)*8.25), m.pillar.Sample(wx*0.2, 0, 0.2*wz)*15)
		if e > 0 {
			roof := mathx.AbsFloat(m.roof.Sample(wx*0.75, 0, wz*0.75)*1.5) * 50
			ceil := float32(int32(roof))
			if ceil < roof {
				ceil++
			}
			spike = min((e*e)*2.5, ceil+24) + 64
		}
	}
	spikeTop := int32(spike)
	if float32(spikeTop) > spike {
		spikeTop--
	}
	spikeTop -= MinY

	noise3 := float32(float64(col.noise) / 3)
	rnd := mt.Float()
	cs := float32(math.Cos(float64(3.1415927 * noise3)))
	top := int32(fillHeight - 1)
	start := col.start

	for ly := top; ly > start; ly-- {
		blk := c.at(x, ly, z)
		if o.liquid(blk) {
			return
		}
		if blk != o.ids.air {
			break
		}
	}

	depth := int32(rnd*0.25 + (noise3 + 3))
	forestY := seaLocal + 2*depth + 33
	sandTopY := seaLocal + depth + 10
	bandMax := int32(seaLocal + bandCount)
	colours := o.clayColours(b)
	run := int32(-1)
	placedTop, seenSolid := false, false
	filler := b.mats.mid
	painted := 0
	for ly := top; ; ly-- {
		blk := c.at(x, ly, z)
		if blk == o.ids.bedrock {
			return
		}
		counts := true
		if blk != o.ids.air || seenSolid || ly > spikeTop {
			seenSolid = seenSolid || blk != o.ids.air
		} else {
			blk = o.ids.stone
			c.put(x, ly, z, blk)
			seenSolid, counts = false, false
		}
		if painted <= 14 {
			switch blk {
			case o.ids.air:
				run = -1
			case o.ids.stone:
				switch {
				case run > 0:
					run--
					// Under a top-material block the rest of the run is orange.
					if placedTop {
						c.put(x, ly, z, colours[bandOrange])
					} else {
						c.put(x, ly, z, o.terracotta(b, m.bandAt(col.wx, ly, col.wz)))
					}
				case run == -1:
					mid := filler
					if ly >= seaLocal-4 {
						mid = b.mats.mid
						if ly > seaLocal+1 {
							mid = filler
						}
					}
					if depth <= 0 {
						mid = b.mats.foundation
					}
					run = depth + max(ly-seaLocal, 0)
					filler = mid
					placedTop = false
					switch {
					case ly < seaLocal-1:
						if mid != colours[bandWhite] {
							c.put(x, ly, z, mid)
						} else {
							c.put(x, ly, z, colours[bandOrange])
						}
					case b.forest && ly > forestY:
						if cs <= 0 {
							c.put(x, ly, z, o.ids.grass)
						} else {
							c.put(x, ly, z, o.ids.coarseDirt)
						}
					case ly <= sandTopY:
						c.put(x, ly, z, b.mats.top)
						placedTop = true
					default:
						colour := uint8(bandPlain)
						if ly <= seaLocal || ly > bandMax {
							colour = bandOrange
						}
						if cs <= 0 && ly > seaLocal && ly <= bandMax {
							colour = m.bandAt(col.wx, ly, col.wz)
						}
						c.put(x, ly, z, o.terracotta(b, colour))
					}
				}
				if counts {
					painted++
				}
			}
		}
		if ly <= start {
			return
		}
	}
}
