package overworld

import (
	"github.com/helix-bedrock/worldgen/internal/mathx"
	"github.com/helix-bedrock/worldgen/rand"
	"github.com/helix-bedrock/worldgen/structure"
)

// Ancient cities, after BDS 1.26.52:
//   placement  sub_4E3E390  regions of 24 chunks with picks 8 apart (two
//                           draws averaged per axis), deep dark at
//                           (16cx, -27, 16cz)
//   start      sub_D2AAD50  rotation, then a city_center element; its
//                           minecraft:city_anchor jigsaw goes to
//                           (16cx, -28, 16cz)
//   assembly   sub_D2AB0E0  newer than the village one: jigsaws expanded
//                           first in, first out, 7 deep, within 128 blocks
//   expand     sub_D2B1720  jigsaws shuffled; the pool, or at the last
//                           depth only its fallback, tried
//   fit        sub_D2B1D00  free-space boxes kept per nesting level
//   rules      sub_D1BE660  block rules with a Java random per block
// The start draws from the start chunk's population random (VERIFY).

const (
	citySalt       = 20083232
	citySpacing    = 24
	citySeparation = 8
	cityMaxDepth   = 7
	cityMaxDist    = 128
	// cityAnchorY is where the start piece's anchor jigsaw goes; the
	// pieces' fixed height (vfunc 112 of AncientCityPiece) is one above.
	cityAnchorY = -28
	// cityReach is how many chunks a city reaches from its start chunk:
	// the free space is 128 blocks around the start piece's centre.
	cityReach = 12
)

// city is an assembled ancient city.
type city struct {
	pieces []*structure.Piece
	bb     structure.Box
}

// cityPick is the chunk a region's city would start in, and the random
// after the pick, which the start continues.
func (o *Overworld) cityPick(rx, rz int32, r *rand.MT) (int32, int32) {
	structureSeed(r, o.seed, rx, rz, citySalt)
	n := int32(citySpacing - citySeparation)
	a := r.IntN(n)
	x := rx*citySpacing + (a+r.IntN(n))/2
	b := r.IntN(n)
	z := rz*citySpacing + (b+r.IntN(n))/2
	return x, z
}

// IsAncientCityChunk reports whether an ancient city starts in chunk
// (cx, cz).
func (o *Overworld) IsAncientCityChunk(cx, cz int32) bool {
	var r rand.MT
	px, pz := o.cityPick(structure.Region(cx, citySpacing), structure.Region(cz, citySpacing), &r)
	return px == cx && pz == cz && o.BiomeAt(cx*16, cityAnchorY+1, cz*16) == o.deepDark
}

// cityAt assembles the city of region (rx, rz), nil if it has none.
func (o *Overworld) cityAt(rx, rz int32) *city {
	key := [2]int32{rx, rz}
	if v, ok := o.cities.Load(key); ok {
		c, _ := v.(*city)
		return c
	}
	var c *city
	var r rand.MT
	cx, cz := o.cityPick(rx, rz, &r)
	if o.city != nil && o.BiomeAt(cx*16, cityAnchorY+1, cz*16) == o.deepDark {
		// The start is built with the start chunk's population random: of
		// the seedings tried against BDS's city at chunk (-14, -113) it is
		// the only one that gets every depth-1 piece right (VERIFY: the
		// caller of sub_4E3E5B0).
		r.Seed(populationSeed(o.seed, cx, cz))
		c = o.assembleCity(cx, cz, &r)
	}
	v, _ := o.cities.LoadOrStore(key, c)
	c, _ = v.(*city)
	return c
}

// assembleCity builds the city starting in chunk (cx, cz).
func (o *Overworld) assembleCity(cx, cz int32, r *rand.MT) *city {
	rot := int(r.IntN(4))
	pool := o.city.Pools["minecraft:ancient_city/city_center"]
	if pool == nil || len(pool.Entries) == 0 {
		return nil
	}
	el := pool.Entries[r.IntN(int32(len(pool.Entries)))].Element
	tpl := o.st.Template(el.Template)
	if tpl == nil {
		return nil
	}
	at := [3]int32{cx * 16, 0, cz * 16}
	p := &structure.Piece{Element: el, Template: tpl, Pos: at, Rotation: rot}
	js := p.PlacedJigsaws()
	structure.Shuffle(js, r)
	// BDS logs and uses no offset when the anchor is missing.
	var off [3]int32
	for _, j := range js {
		if j.Name == "minecraft:city_anchor" {
			off = [3]int32{j.Pos[0] - at[0], j.Pos[1] - at[1], j.Pos[2] - at[2]}
			break
		}
	}
	p.Pos = [3]int32{at[0] - off[0], 0, at[2] - off[2]}
	p.BB = structure.TemplateBox(tpl.Template, p.Pos, rot)
	// The free space is centred on the start piece before it is lowered.
	mx := p.BB.X0 + (p.BB.X1-p.BB.X0+1)/2
	my := p.BB.Y0 + (p.BB.Y1-p.BB.Y0+1)/2
	mz := p.BB.Z0 + (p.BB.Z1-p.BB.Z0+1)/2
	bounds := structure.NewBox(mx-cityMaxDist, my-cityMaxDist, mz-cityMaxDist, mx+cityMaxDist, my+cityMaxDist, mz+cityMaxDist)
	dy := cityAnchorY - (p.BB.Y0 + off[1])
	p.Pos[1] += dy
	p.BB = p.BB.Raise(dy)
	p.Ground = 1

	a := &structure.Assembly{Lib: o.st, Data: o.city, MaxDepth: cityMaxDepth, R: r, Pieces: []*structure.Piece{p},
		Shapes: []structure.Shape{{In: []structure.Box{bounds}, Out: []structure.Box{p.BB}}},
		Queue:  []structure.Queued{{Piece: p}},
		Feature: &structure.JigsawTemplate{Template: &structure.Template{Size: [3]int32{1, 1, 1}},
			Jigsaws: []structure.JigsawBlock{{Facing: "down", Name: "minecraft:bottom", Target: "minecraft:empty", Pool: "minecraft:empty"}}},
	}
	a.Run()
	return &city{pieces: a.Pieces, bb: structure.PiecesBox(a.Pieces)}
}

// placeCities writes the pieces of ancient cities that reach chunk
// (cx, cz) into c.
// Feature elements are placed during decoration (cityFeatures).
// TODO: the beard_box terrain adaptation around the pieces.
func (o *Overworld) placeCities(cx, cz int32, c *Chunk) {
	chunkBox := structure.NewBox(cx*16, MinY, cz*16, cx*16+15, MaxY-1, cz*16+15)
	o.forCityPieces(cx, cz, func(p *structure.Piece) bool {
		if p.Element.Feature == "" {
			o.placePiece(p, c, chunkBox, nil)
		}
		return true
	})
}

// cityFeatures places the feature elements of city pieces in origin chunk
// (ox, oz) (the sculk patches, FeaturePoolElement place sub_D232280) at
// each piece's position, in piece order. BDS hands them the random of the
// structure post-process, a Mersenne Twister seeded with the chunk's
// population seed (WorldGenerator::postProcessStructureFeatures).
// VERIFY: BDS 1.26.52 runs this step outside sub_517CD10; its exact place
// in the decoration order was not read.
func (o *Overworld) cityFeatures(w *featureWorld, ox, oz int32, pop uint32) {
	var r *rand.MT
	o.forCityPieces(ox, oz, func(p *structure.Piece) bool {
		if p.Element.Feature == "" || p.Pos[0]>>4 != ox || p.Pos[2]>>4 != oz {
			return true
		}
		f, ok := o.features.get(p.Element.Feature)
		if !ok {
			return true
		}
		if r == nil {
			r = rand.NewMT(pop)
		}
		f.place(w, p.Pos[0], p.Pos[1], p.Pos[2], r)
		return true
	})
}

// citiesReach reports whether an ancient city has a piece in chunk
// (cx, cz).
func (o *Overworld) citiesReach(cx, cz int32) bool {
	found := false
	o.forCityPieces(cx, cz, func(*structure.Piece) bool {
		found = true
		return false
	})
	return found
}

// forCityPieces calls f for every city piece in chunk (cx, cz), in
// placement order, until f returns false.
func (o *Overworld) forCityPieces(cx, cz int32, f func(*structure.Piece) bool) {
	if o.city == nil {
		return
	}
	chunkBox := structure.NewBox(cx*16, MinY, cz*16, cx*16+15, MaxY-1, cz*16+15)
	r0x, r1x := structure.Region(cx-cityReach, citySpacing), structure.Region(cx+cityReach, citySpacing)
	r0z, r1z := structure.Region(cz-cityReach, citySpacing), structure.Region(cz+cityReach, citySpacing)
	for rx := r0x; rx <= r1x; rx++ {
		for rz := r0z; rz <= r1z; rz++ {
			c := o.cityAt(rx, rz)
			if c == nil || !c.bb.Intersects(chunkBox) {
				continue
			}
			for _, p := range c.pieces {
				if p.BB.Intersects(chunkBox) && !f(p) {
					return
				}
			}
		}
	}
}

// AncientCity is where an ancient city is.
type AncientCity struct {
	// X, Y, Z is the start piece's anchor, on the floor of the city centre.
	X, Y, Z int32
	// Min and Max are the corners of the box around every piece.
	Min, Max [3]int32
}

// NearestAncientCity finds the ancient city whose start is nearest to block
// (x, z), searching placement regions ring by ring up to maxRings out. It
// is safe for concurrent use.
func (o *Overworld) NearestAncientCity(x, z, maxRings int32) (AncientCity, bool) {
	rx0, rz0 := structure.Region(x>>4, citySpacing), structure.Region(z>>4, citySpacing)
	var r rand.MT
	best, found := int64(-1), [2]int32{}
	for ring := int32(0); ring <= maxRings; ring++ {
		for rx := rx0 - ring; rx <= rx0+ring; rx++ {
			for rz := rz0 - ring; rz <= rz0+ring; rz++ {
				if max(mathx.Abs(rx-rx0), mathx.Abs(rz-rz0)) != ring {
					continue
				}
				cx, cz := o.cityPick(rx, rz, &r)
				if o.BiomeAt(cx*16, cityAnchorY+1, cz*16) != o.deepDark {
					continue
				}
				dx, dz := int64(cx*16-x), int64(cz*16-z)
				if d := dx*dx + dz*dz; best < 0 || d < best {
					best, found = d, [2]int32{rx, rz}
				}
			}
		}
		// A start one ring further out can still be nearer; stop after it.
		if best >= 0 && ring > 0 {
			break
		}
	}
	if best < 0 {
		return AncientCity{}, false
	}
	c := o.cityAt(found[0], found[1])
	if c == nil {
		return AncientCity{}, false
	}
	var r2 rand.MT
	cx, cz := o.cityPick(found[0], found[1], &r2)
	return AncientCity{X: cx * 16, Y: cityAnchorY + 1, Z: cz * 16,
		Min: [3]int32{c.bb.X0, c.bb.Y0, c.bb.Z0}, Max: [3]int32{c.bb.X1, c.bb.Y1, c.bb.Z1}}, true
}
