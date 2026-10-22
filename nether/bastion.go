package nether

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/helix-bedrock/worldgen/internal/mathx"

	"github.com/helix-bedrock/worldgen/rand"
	"github.com/helix-bedrock/worldgen/structure"
)

// Bastion remnants, written from BDS 1.26.52:
//   placement  sub_4FD0B70  the fortress grid: a start not in a basalt delta
//                           (the feature's biomes, sub_4E3E770: nether
//                           wastes, soul sand valley, crimson and warped
//                           forest) whose draw from six is not a fortress's
//   start      sub_D2AC6C0  from the placement random: a turn
//                           (nextInt(4)), one of the four start pools
//                           (nextInt(4)) and one of its elements, at the
//                           start chunk's corner with no start anchor
//   height     BastionPiece's slot 14: the sea level plus one; the start
//                           piece's bottom goes one below it
//   assembly   sub_D2AB0E0  the assembler ancient cities use, 15 deep,
//                           within 80 blocks of the start piece's centre
//   pieces     sub_5071920  all rigid; their terrain adaptation does
//                           nothing here, as the Nether's terrain is not
//                           adjusted around structures
//   rules      sub_506A230  data/bds/bastion.json
// Pieces go into the chunks they reach on the terrain before decoration,
// after the ruined portals and before the fortresses (VERIFY: the order
// where they overlap). The piglins and hoglins of the mob pieces are not
// placed: worldgen places no mobs.

const (
	bastionMaxDepth = 15
	bastionMaxDist  = 80
	// bastionReach is how many chunks from its start chunk a bastion can
	// reach: its pieces stay within 80 blocks of the start piece's centre.
	bastionReach = 6
)

// BastionKinds name the four kinds of bastion, in the order of
// bastionStarts.
var BastionKinds = [4]string{"bridge", "treasure", "hoglin_stable", "housing_units"}

// bastionStarts are the start pools, by the start's second draw.
var bastionStarts = [4]string{
	"minecraft:bastion/bridge/start",
	"minecraft:bastion/treasure/starters",
	"minecraft:bastion/hoglin_stable/origin",
	"minecraft:bastion/units/air_base",
}

// bastion is an assembled bastion remnant.
type bastion struct {
	pieces []*structure.Piece
	bb     structure.Box
}

// loadBastions loads the bastion pools and the biomes bastions stand in.
func (n *Nether) loadBastions(biomeIDs map[string]uint32) error {
	var err error
	if n.bastionData, err = structure.LoadJigsawData("bastion.json", n.reg); err != nil {
		return err
	}
	n.bastionBiomes = map[uint32]bool{}
	for _, name := range []string{"hell", "soulsand_valley", "crimson_forest", "warped_forest"} {
		id, ok := biomeIDs[name]
		if !ok {
			return fmt.Errorf("worldgen: biome %s missing from the biome list", name)
		}
		n.bastionBiomes[id] = true
	}
	return nil
}

// bastionChunk reports whether a bastion starts in chunk (cx, cz) and
// leaves r where the start goes on.
func (n *Nether) bastionChunk(cx, cz int32, r *rand.MT) bool {
	sx, sz := n.complexStart(mathx.FloorDiv(cx, complexSpacing), mathx.FloorDiv(cz, complexSpacing), r)
	if sx != cx || sz != cz {
		return false
	}
	if !n.complexBiomesOK(cx, cz, func(b uint32) bool { return n.bastionBiomes[b] }) {
		return false
	}
	return r.IntN(6) >= 2
}

// bastionAt assembles the bastion starting in chunk (cx, cz), if any.
func (n *Nether) bastionAt(cx, cz int32) *bastion {
	var r rand.MT
	if !n.bastionChunk(cx, cz, &r) {
		return nil
	}
	k := [2]int32{cx, cz}
	if v, ok := n.bastions.Load(k); ok {
		b, _ := v.(*bastion)
		return b
	}
	b := n.assembleBastion(cx, cz, &r)
	v, _ := n.bastions.LoadOrStore(k, b)
	b, _ = v.(*bastion)
	return b
}

// assembleBastion is the start and the assembly.
func (n *Nether) assembleBastion(cx, cz int32, r *rand.MT) *bastion {
	rot := int(r.IntN(4))
	pool := n.bastionData.Pools[bastionStarts[r.IntN(4)]]
	if pool == nil || len(pool.Entries) == 0 {
		return nil
	}
	el := pool.Entries[r.IntN(int32(len(pool.Entries)))].Element
	tpl := n.st.Template(el.Template)
	if tpl == nil {
		return nil
	}
	p := &structure.Piece{Element: el, Template: tpl, Pos: [3]int32{cx * 16, 0, cz * 16}, Rotation: rot}
	p.BB = structure.TemplateBox(tpl.Template, p.Pos, rot)
	// The free space is centred on the start piece before it is raised.
	mx := p.BB.X0 + (p.BB.X1-p.BB.X0+1)/2
	my := p.BB.Y0 + (p.BB.Y1-p.BB.Y0+1)/2
	mz := p.BB.Z0 + (p.BB.Z1-p.BB.Z0+1)/2
	bounds := structure.NewBox(mx-bastionMaxDist, my-bastionMaxDist, mz-bastionMaxDist,
		mx+bastionMaxDist, my+bastionMaxDist, mz+bastionMaxDist)
	dy := SeaLevel - p.BB.Y0
	p.Pos[1] += dy
	p.BB = p.BB.Raise(dy)
	a := &structure.Assembly{Lib: n.st, Data: n.bastionData, MaxDepth: bastionMaxDepth, R: r,
		Pieces: []*structure.Piece{p},
		Shapes: []structure.Shape{{In: []structure.Box{bounds}, Out: []structure.Box{p.BB}}},
		Queue:  []structure.Queued{{Piece: p}},
	}
	a.Run()
	return &bastion{pieces: a.Pieces, bb: structure.PiecesBox(a.Pieces)}
}

// bastionsReach calls f with each bastion that may have blocks in chunk
// (cx, cz).
func (n *Nether) bastionsReach(cx, cz int32, f func(b *bastion)) {
	for rx := mathx.FloorDiv(cx-bastionReach, complexSpacing); rx <= mathx.FloorDiv(cx+bastionReach, complexSpacing); rx++ {
		for rz := mathx.FloorDiv(cz-bastionReach, complexSpacing); rz <= mathx.FloorDiv(cz+bastionReach, complexSpacing); rz++ {
			var r rand.MT
			sx, sz := n.complexStart(rx, rz, &r)
			if mathx.Abs(sx-cx) > bastionReach || mathx.Abs(sz-cz) > bastionReach {
				continue
			}
			if b := n.bastionAt(sx, sz); b != nil {
				f(b)
			}
		}
	}
}

// placeBastions writes the bastion pieces that reach chunk (cx, cz) into c.
func (n *Nether) placeBastions(cx, cz int32, c *Chunk) {
	chunkBox := structure.NewBox(cx*16, 0, cz*16, cx*16+15, Height-1, cz*16+15)
	n.bastionsReach(cx, cz, func(b *bastion) {
		if !b.bb.Intersects(chunkBox) {
			return
		}
		for _, p := range b.pieces {
			if p.BB.Intersects(chunkBox) {
				n.st.PlacePiece(p, chunkWorld{c}, chunkBox, nil)
			}
		}
	})
}

// chunkWorld places structure pieces into a Nether chunk.
type chunkWorld struct{ c *Chunk }

func (w chunkWorld) Bounds() (int32, int32) { return 0, Height }

func (w chunkWorld) Block(x, y, z int32) uint32 { return w.c.Block(int(x&15), int(y), int(z&15)) }

func (w chunkWorld) Set(x, y, z int32, id uint32, _ bool) bool {
	w.c.set(int(x&15), int(y), int(z&15), id)
	w.c.dropEntity(x, y, z)
	return true
}

func (w chunkWorld) Entity(be structure.BlockEntity) {
	w.c.BlockEntities = append(w.c.BlockEntities, be)
}

// bastionKind is the index in BastionKinds of a bastion, given the random
// bastionChunk left: the start's second draw picks it.
func bastionKind(r *rand.MT) int {
	r.IntN(4)
	return int(r.IntN(4))
}

// NearestBastion finds the bastion remnant whose start is nearest to block
// (x, z), searching maxRings rings of placement regions around it, and
// returns the centre of its start piece's floor. kind is one of
// BastionKinds, or "" for any. It is safe for concurrent use.
func (n *Nether) NearestBastion(x, z, maxRings int32, kind string) (bx, by, bz int32, found bool) {
	type cand struct {
		cx, cz int32
		d      int64
	}
	var cands []cand
	rx0, rz0 := mathx.FloorDiv(x>>4, complexSpacing), mathx.FloorDiv(z>>4, complexSpacing)
	for rx := rx0 - maxRings; rx <= rx0+maxRings; rx++ {
		for rz := rz0 - maxRings; rz <= rz0+maxRings; rz++ {
			var r rand.MT
			cx, cz := n.complexStart(rx, rz, &r)
			if !n.bastionChunk(cx, cz, &r) {
				continue
			}
			if kind != "" && BastionKinds[bastionKind(&r)] != kind {
				continue
			}
			dx, dz := int64(cx*16-x), int64(cz*16-z)
			cands = append(cands, cand{cx, cz, dx*dx + dz*dz})
		}
	}
	slices.SortFunc(cands, func(a, b cand) int { return cmp.Compare(a.d, b.d) })
	for _, c := range cands {
		if b := n.bastionAt(c.cx, c.cz); b != nil {
			s := b.pieces[0].BB
			return s.X0 + (s.X1-s.X0+1)/2, s.Y0 + 1, s.Z0 + (s.Z1-s.Z0+1)/2, true
		}
	}
	return 0, 0, 0, false
}
