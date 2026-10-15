package worldgen

import (
	"math"

	"github.com/helix-bedrock/worldgen/rand"
)

// The frozen ocean's ice, written from BDS 1.26.52: legacy:ice_feature
// (sub_51D2800) places an iceberg in 1 of 8 chunks and up to 19 blue ice
// patches. The iceberg is IcebergFeature (place: sub_51CC190, with
// sub_51CD4A0 placing one block and sub_51CD890 carving).

// tableSin and tableCos are BDS's sine table lookups (mce::Math, 65536
// steps per turn).
func tableSin(a float32) float32 {
	return float32(math.Sin(float64(uint16(int32(a*10430.378))) * (2 * math.Pi / 65536)))
}

func tableCos(a float32) float32 {
	return float32(math.Sin(float64(uint16(int32(a*10430.378+16384))) * (2 * math.Pi / 65536)))
}

// ceilf is BDS's inlined ceiling of a positive float: the truncation, plus
// one when it was below the value.
func ceilf(f float32) int32 {
	t := int32(f)
	if float32(t) < f {
		t++
	}
	return t
}

type legacyIceFeature struct {
	iceberg *icebergFeature
	blueIce placer
}

func (f *legacyIceFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	if r.IntN(8) == 0 {
		dx := r.IntN(8)
		dz := r.IntN(8)
		bx, bz := x+dx+12, z+dz+12
		f.iceberg.place(w, bx, w.heightmap(bx, bz), bz, r)
	}
	for n := r.IntN(20); n > 0; n-- {
		dx := r.IntN(16)
		by := r.IntN(32) + 30
		dz := r.IntN(16)
		if f.blueIce != nil {
			f.blueIce.place(w, x+dx+8, by, z+dz+8, r)
		}
	}
	return blockPos{x, y, z}, true
}

type icebergFeature struct {
	air, water, snow, packedIce, blueIce, ice, flowingWater, snowLayer uint32
}

func (f *icebergFeature) iceLike(b uint32) bool {
	return b == f.packedIce || b == f.snow || b == f.blueIce
}

// icebergShape holds the values the block helper needs.
type icebergShape struct {
	ox, oy, oz int32
	angle      float32
	snowy      bool
	minor      int32 // the second ellipse axis (v149)
	block      uint32
}

func (f *icebergFeature) place(w *featureWorld, x, _, z int32, r *rand.MT) (blockPos, bool) {
	v6 := r.IntN(5)
	maxR := 11 - v6
	minor := r.IntN(3)
	v8 := r.IntN(6)
	v9 := r.IntN(15)
	shapeF := r.Float()
	var height int32
	if shapeF <= 0.8 {
		if r.Float() <= 0.9 {
			height = v9 + 3
		} else {
			height = v9 + r.IntN(19) + 10
		}
		maxR = 11
	} else {
		height = v8 + 6
	}
	minor += 3
	snowyF := r.Float()
	a := r.Float()
	angle := (a + a) * 3.1415927
	v12 := r.IntN(7)
	width := min(height+v12-r.IntN(5), 11)
	block := f.packedIce
	if r.Float() <= 0.01 {
		block = f.blueIce
	}
	round := shapeF <= 0.8
	s := icebergShape{ox: x, oy: SeaLevel, oz: z, angle: angle, snowy: snowyF > 0.7, minor: minor, block: block}

	if maxR > 0 && height > 0 {
		if round {
			fh := float32(height)
			for bx := -maxR; bx < maxR; bx++ {
				for bz := -maxR; bz < maxR; bz++ {
					for by := int32(0); by < height; by++ {
						rad := f.roundRadius(r, by, height, fh, width)
						if bx < rad {
							f.block(w, r, &s, height, bx, by, bz, rad, maxR, false)
						}
					}
				}
			}
		} else {
			for bx := -maxR; bx < maxR; bx++ {
				for bz := -maxR; bz < maxR; bz++ {
					for by := int32(0); by < height; by++ {
						v := float32((1 - float64(by)*float64(by)/float64(float32(height))) * float64(width))
						rad := ceilf(v * 0.5)
						f.block(w, r, &s, height, bx, by, bz, rad, maxR, true)
					}
				}
			}
		}
	}

	// Smooth: drop ice over air and ice with three open sides.
	sr := width / 2
	if !round {
		sr = 11 - v6
	}
	if sr >= 0 && height >= 0 {
		for dx := -sr; dx <= sr; dx++ {
			for dz := -sr; dz <= sr; dz++ {
				for dy := int32(0); dy <= height; dy++ {
					px, py, pz := x+dx, SeaLevel+dy, z+dz
					b, _ := w.block(px, py, pz)
					if !f.iceLike(b) && b != f.snowLayer {
						continue
					}
					// BDS skips blocks inside a village here; icebergs do not
					// meet villages.
					if below, _ := w.block(px, py-1, pz); below == f.air {
						w.set(px, py, pz, f.air)
						w.set(px, py+1, pz, f.air)
						continue
					}
					if !f.iceLike(b) {
						continue
					}
					open := 0
					for _, d := range [4][2]int32{{0, 1}, {-1, 0}, {0, -1}, {1, 0}} {
						if n, _ := w.block(px+d[0], py, pz+d[1]); !f.iceLike(n) {
							open++
						}
					}
					if open >= 3 {
						w.set(px, py, pz, f.air)
					}
				}
			}
		}
	}

	// The part under water.
	v65 := height + r.IntN(11)
	depth := min(v65, 18)
	if maxR > 0 && v65 >= 2 {
		fd := float32(depth)
		fw := float32(width)
		for bx := -maxR; bx < maxR; bx++ {
			for bz := -maxR; bz < maxR; bz++ {
				for by, k := int32(-1), int32(1); by > -depth; by, k = by-1, k+1 {
					q := float32(k) / ((r.Float()*0.5 + 1) * fd)
					rad := ceilf((1 - q) * fw * 0.5)
					if bx >= rad {
						continue
					}
					if round {
						f.block(w, r, &s, depth, bx, by, bz, rad, maxR, false)
					} else {
						e := float32((1 - float64(by)*float64(by)/float64(8*fd)) * float64(maxR))
						f.block(w, r, &s, depth, bx, by, bz, rad, ceilf(e), true)
					}
				}
			}
		}
	}

	// A channel of water or air cut through the berg.
	first := r.Float()
	half := width / 2
	if round {
		if r.Float() <= 0.7 {
			return blockPos{x, SeaLevel, z}, true
		}
	} else if first <= 0.1 {
		return blockPos{x, SeaLevel, z}, true
	}
	n := max(half, 3) - 2
	offset := func() int32 {
		v := r.IntN(n)
		if r.Uint32()>>27&1 != 0 {
			v = half - r.IntN(max(width-half-1, 1)) + 1
		}
		return v
	}
	ox := offset()
	oz := offset()
	if !round {
		ox = r.IntN(max(6-v6, 1))
		oz = ox
	}
	if r.Uint32()>>27&1 != 0 {
		ox = -ox
	}
	if r.Uint32()>>27&1 != 0 {
		oz = -oz
	}
	var carveAngle float32
	if round {
		c := r.Float()
		carveAngle = (c + c) * 3.1415927
	} else {
		carveAngle = angle + 1.5707964
	}
	if height >= 4 {
		fh := float32(height)
		for by := int32(0); by != height-3; by++ {
			rad := f.roundRadius(r, by, height, fh, width)
			f.carve(w, &s, rad, by, false, carveAngle, ox, oz, 11-v6, minor)
		}
	}
	if r.IntN(5)-height <= -2 {
		fh := float32(height)
		fw := float32(width)
		for by, k := int32(-1), int32(1); ; by, k = by-1, k+1 {
			q := float32(k) / ((r.Float()*0.5 + 1) * fh)
			rad := ceilf((1 - q) * fw * 0.5)
			f.carve(w, &s, rad, by, true, carveAngle, ox, oz, 11-v6, minor)
			if !(by-1 > r.IntN(5)-height) {
				break
			}
		}
	}
	return blockPos{x, SeaLevel, z}, true
}

// roundRadius is the round iceberg's radius at height y (inlined in BDS).
func (f *icebergFeature) roundRadius(r *rand.MT, y, height int32, fh float32, width int32) int32 {
	g := (3.5 - r.Float()) * fh
	var v float32
	if height <= r.IntN(5)+15 {
		v = float32((1 - float64(y)*float64(y)/float64(g)) * float64(width))
	} else {
		k := r.IntN(6)
		j := y
		if y < k+3 {
			j = y >> 1
		}
		v = (float32(j)/(g*-0.4) + 1) * float32(width)
	}
	return ceilf(v * 0.5)
}

// block places one iceberg block (sub_51CD4A0).
func (f *icebergFeature) block(w *featureWorld, r *rand.MT, s *icebergShape, height, x, y, z, radius, maxR int32, ellipse bool) {
	var d float32
	if ellipse {
		sn, cs := tableSin(s.angle), tableCos(s.angle)
		fx, fz := float32(x), float32(z)
		a := float64((cs*fx + -fz*sn) / float32(maxR))
		b := float64((sn*fx + fz*cs) / float32(s.minor))
		d = float32(b*b + a*a + -1)
	} else {
		v := r.Float()
		g := max(v, 0.2) * 10
		if 0.8 < v {
			g = 8
		}
		d = float32(float64(z)*float64(z)+float64(x)*float64(x)-float64(radius)*float64(radius)) + g/float32(radius)
	}
	if d >= 0 {
		return
	}
	px, py, pz := s.ox+x, s.oy+y, s.oz+z
	deep := false
	if ellipse {
		deep = d <= -0.5
	} else {
		deep = d <= float32(-6-float32(r.IntN(3)))
	}
	if !deep && r.Float() > 0.9 {
		return
	}
	at, ok := w.block(px, py, pz)
	if !ok || !(at == f.air || at == f.snow || at == f.ice || at == f.water || at == f.flowingWater) {
		return
	}
	div := int32(2)
	cap := false
	if ellipse {
		cap = r.Float() <= 0.05
		div = 3
	}
	b := s.block
	if s.snowy && at != f.water {
		n := max(height/div, 1)
		if !(float64(height)*0.6+float64(r.IntN(n)) < float64(height-y) || cap) {
			b = f.snow
		}
	}
	w.set(px, py, pz, b)
}

// carve cuts one layer of the channel (sub_51CD890): water below sea level,
// air above.
func (f *icebergFeature) carve(w *featureWorld, s *icebergShape, radius, y int32, underwater bool, angle float32, offX, offZ, v160, minor int32) {
	n := radius + v160/3 + 1
	if n <= 0 {
		return
	}
	axisA := float32(min(radius, 6) + minor/2 - 4)
	axisB := float32(n)
	sn, cs := tableSin(angle), tableCos(angle)
	for x := -n; x < n; x++ {
		fx := float32(x - offX)
		for z := -n; z < n; z++ {
			fz := float32(z - offZ)
			a := float64((sn*fx + fz*cs) / axisA)
			b := float64((cs*fx + -fz*sn) / axisB)
			if !(float32(b*b+a*a+-1) < 0) {
				continue
			}
			px, py, pz := s.ox+x, s.oy+y, s.oz+z
			at, _ := w.block(px, py, pz)
			if !f.iceLike(at) && at != f.snowLayer {
				continue
			}
			if underwater {
				w.set(px, py, pz, f.water)
				continue
			}
			w.set(px, py, pz, f.air)
			if above, _ := w.block(px, py+1, pz); above == f.snowLayer {
				w.set(px, py+1, pz, f.air)
			}
		}
	}
}

// blueIceFeature is BDS's BlueIceFeature (place: sub_4988230): a patch of
// blue ice grown from a block of water next to packed ice, below sea level.
type blueIceFeature struct {
	ice *icebergFeature
}

func (f *blueIceFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	ib := f.ice
	if y >= SeaLevel {
		return blockPos{}, false
	}
	at, _ := w.block(x, y, z)
	below, _ := w.block(x, y-1, z)
	if at != ib.water && below != ib.water {
		return blockPos{}, false
	}
	touch := false
	for _, d := range [5][3]int32{{0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}} {
		if b, _ := w.block(x+d[0], y+d[1], z+d[2]); b == ib.packedIce {
			touch = true
			break
		}
	}
	if !touch {
		return blockPos{}, false
	}
	w.set(x, y, z, ib.blueIce)
	for range 200 {
		dy := r.IntN(5) - r.IntN(6)
		n := int32(3)
		if dy <= 1 {
			if dy < -5 {
				continue
			}
			n = dy/2 + 3
		}
		dx := r.IntN(n)
		dx -= r.IntN(n)
		dz := r.IntN(n)
		dz -= r.IntN(n)
		px, py, pz := x+dx, y+dy, z+dz
		b, _ := w.block(px, py, pz)
		if b != ib.air && b != ib.water && b != ib.packedIce && b != ib.ice {
			continue
		}
		count := 0
		for _, d := range [6][3]int32{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}} {
			if nb, _ := w.block(px+d[0], py+d[1], pz+d[2]); nb == ib.blueIce {
				count++
			}
		}
		if count >= 1 && count <= 4 {
			w.set(px, py, pz, ib.blueIce)
		}
	}
	return blockPos{x, y, z}, true
}
