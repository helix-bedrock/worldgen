package end

import (
	"cmp"
	"slices"

	"github.com/helix-bedrock/worldgen/structure"

	"github.com/helix-bedrock/worldgen/rand"
)

// End cities, written from BDS 1.26.52: EndCityFeature (chunk test
// sub_4E400E0, height sub_4E402F0, start sub_4E40A30) and EndCityPieces
// (recursion sub_4E41EA0, the tower, fat tower, bridge and house tower
// generators sub_4E42570, sub_4E43EF0, sub_4E45340 and sub_4E46930, the
// piece sub_4E48460 with its markers sub_4E492D0). The layout is Java's
// EndCityPieces with Bedrock's template names; Bedrock never refuses a
// generator's pieces for overlapping older ones.

// End city placement (sub_D251A10 with these values): a triangular spread.
const (
	citySalt       = 10387313
	citySpacing    = 20
	citySeparation = 11
)

// cityRandom is city region (rx, rz)'s placement random.
func (e *End) cityRandom(rx, rz int32) *rand.MT {
	return rand.NewMT(structure.RegionSeed(e.seed, citySalt, rx, rz))
}

// cityPick is the chunk city region (rx, rz) picks.
func (e *End) cityPick(rx, rz int32) (int32, int32) {
	r := e.cityRandom(rx, rz)
	n := int32(citySpacing - citySeparation)
	x := (r.IntN(n)+r.IntN(n))/2 + rx*citySpacing
	z := (r.IntN(n)+r.IntN(n))/2 + rz*citySpacing
	return x, z
}

// cityRot is the start's turn (sub_4E402F0 and sub_4E40A30 both draw it).
func cityRot(cx, cz int32) int {
	return int(rand.NewMT(uint32(cx+citySalt*cz)).Uint32() & 3)
}

// cityHeight is the lowest of the terrain heights at four columns of the
// chunk, picked by the turn (sub_4E402F0): one above the top end stone,
// 0 for none.
func (e *End) cityHeight(cx, cz int32, rot int) int32 {
	var c Chunk
	e.baseChunk(cx, cz, &c)
	fz, fx := [4]int{12, 12, 2, 2}[rot], [4]int{12, 2, 2, 12}[rot]
	low := int32(Height)
	for _, p := range [4][2]int{{7, 7}, {7, fz}, {fx, 7}, {fx, fz}} {
		h := int32(0)
		for y := Height - 1; y >= 0; y-- {
			if c.Block(p[0], y, p[1]) != e.ids.air {
				h = int32(y) + 1
				break
			}
		}
		low = min(low, h)
	}
	return low
}

// isCityChunk reports whether an End city starts in chunk (cx, cz), and
// its turn and height: the chunk is its region's pick, more than 64
// chunks from the origin, on ground at least 60 high.
func (e *End) isCityChunk(cx, cz int32) (rot int, y int32, ok bool) {
	px, pz := e.cityPick(structure.Region(cx, citySpacing), structure.Region(cz, citySpacing))
	if px != cx || pz != cz || int64(cx)*int64(cx)+int64(cz)*int64(cz) <= 4096 {
		return 0, 0, false
	}
	rot = cityRot(cx, cz)
	y = e.cityHeight(cx, cz, rot)
	return rot, y, y >= 60
}

// NearestCity finds the End city nearest to block (x, z), searching
// maxRegions placement regions (20 chunks each) around it, and returns the
// middle of its start chunk at the start's height. It is safe for
// concurrent use.
func (e *End) NearestCity(x, z, maxRegions int32) (cx, cy, cz int32, found bool) {
	var cands []cityCandidate
	rx0, rz0 := structure.Region(x>>4, citySpacing), structure.Region(z>>4, citySpacing)
	for rx := rx0 - maxRegions; rx <= rx0+maxRegions; rx++ {
		for rz := rz0 - maxRegions; rz <= rz0+maxRegions; rz++ {
			px, pz := e.cityPick(rx, rz)
			dx, dz := int64(px*16+8-x), int64(pz*16+8-z)
			cands = append(cands, cityCandidate{px, pz, dx*dx + dz*dz})
		}
	}
	slices.SortFunc(cands, func(a, b cityCandidate) int { return cmp.Compare(a.d, b.d) })
	for _, c := range cands {
		if _, y, ok := e.isCityChunk(c.cx, c.cz); ok {
			return c.cx*16 + 8, y, c.cz*16 + 8, true
		}
	}
	return 0, 0, 0, false
}

// cityCandidate is a region's picked chunk and its squared distance from
// where NearestCity searches.
type cityCandidate struct {
	cx, cz int32
	d      int64
}
