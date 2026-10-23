package overworld

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/helix-bedrock/worldgen"

	"github.com/helix-bedrock/worldgen/rand"
	"github.com/helix-bedrock/worldgen/structure"
)

// Ocean ruins, written from BDS 1.26.52: OceanRuinFeature (constructor
// sub_4E7CB90, chunk test sub_4E7D710, start sub_4E7D890, piece choice
// sub_4E7ECF0) and OceanRuinPiece (postProcess sub_4E80060, chest marker
// sub_4E80ED0). The pieces are LegacyStructureTemplate placements
// (sub_D27F3A0 -> sub_D2B7A10).
//
// Bedrock differs from Java here, and the differences are kept:
//   - a ruin never starts within 5 chunks of an ocean monument start;
//   - the chance of a large ruin and of a cluster comes from the start
//     biome (sub_4951300): 0.3/0.25 for ocean, cold and frozen oceans,
//     0.5/0.4 for their deep forms, 0.3/0.5 for warm and lukewarm ones;
//   - cold groups have a plain "ruin4" as their first small template;
//   - a piece's height is the height map at its corner, lowered to the
//     lowest floor under it when most of its columns are more than 3 blocks
//     deeper;
//   - air in a template turns to water below sea level, and blocks that
//     can hold water there are waterlogged;
//   - a chest marker puts the chest one block under it and fills the marker
//     with water (warm: sandstone; a few templates: gravel or stone bricks).
//
// BDS computes a piece's height again in every chunk it builds, from that
// chunk's world, so a later chunk can see the parts placed before it. Here
// every chunk uses the undecorated terrain.
// VERIFY: heights against BDS ruins that cross chunks.
// REFINE: the archaeology processor (up to 5 sand or gravel blocks turned
// suspicious, StructurePoolBlockPredicateCappedArcheologyBlockReplacement,
// Xoroshiro seeded from the piece corner) is not applied.

// Ocean ruin placement (sub_4E7D710): linear spread. Worlds with the legacy
// flag use 12 and 7.
const (
	ruinSalt       = 14357621
	ruinSpacing    = 20
	ruinSeparation = 8
)

// Ocean monument placement (sub_4E5E7B0): triangular spread.
const (
	monumentSalt       = 10387313
	monumentSpacing    = 32
	monumentSeparation = 5
)

// ruinBiome is a biome's ocean ruin settings (Biome +0x44..+0x4C).
type ruinBiome struct {
	ok             bool
	warm           bool
	large, cluster float32
}

// Ocean ruin start chances by biome kind (sub_4951300).
const (
	// ruinLargeShallow is a shallow or warm ocean's large ruin chance.
	ruinLargeShallow = 0.3
	// ruinLargeDeep is a deep cold or plain ocean's large ruin chance.
	ruinLargeDeep = 0.5
	// ruinClusterShallow is a shallow cold or plain ocean's cluster chance.
	ruinClusterShallow = 0.25
	// ruinClusterDeep is a deep cold or plain ocean's cluster chance.
	ruinClusterDeep = 0.4
	// ruinClusterWarm is a warm or lukewarm ocean's cluster chance.
	ruinClusterWarm = 0.5
)

// ruinBiomes are the biomes ocean ruins start in (sub_4E7CB90) with their
// settings (sub_4951300). It is only read when the generator is built.
func ruinBiomes() map[string]ruinBiome {
	shallow := ruinBiome{true, false, ruinLargeShallow, ruinClusterShallow}
	deep := ruinBiome{true, false, ruinLargeDeep, ruinClusterDeep}
	warm := ruinBiome{true, true, ruinLargeShallow, ruinClusterWarm}
	return map[string]ruinBiome{
		"ocean":               shallow,
		"deep_ocean":          deep,
		"cold_ocean":          shallow,
		"deep_cold_ocean":     deep,
		"frozen_ocean":        shallow,
		"deep_frozen_ocean":   deep,
		"warm_ocean":          warm,
		"deep_warm_ocean":     warm,
		"lukewarm_ocean":      warm,
		"deep_lukewarm_ocean": warm,
	}
}

// monumentDeepBiomes are the biomes a monument's middle must be in
// (sub_4E5D570).
func monumentDeepBiomes() []string {
	return []string{"deep_ocean", "deep_cold_ocean", "deep_lukewarm_ocean", "deep_warm_ocean", "deep_frozen_ocean"}
}

// monumentWaterBiomes are the biomes all around a monument (sub_4E5D570).
func monumentWaterBiomes() []string {
	return []string{"ocean", "deep_ocean", "cold_ocean", "deep_cold_ocean", "frozen_ocean", "deep_frozen_ocean",
		"lukewarm_ocean", "deep_lukewarm_ocean", "warm_ocean", "deep_warm_ocean", "frozen_river", "river"}
}

// Monument biome test radii in blocks (sub_4E5D570).
const (
	monumentDeepReach  = 16
	monumentWaterReach = 29
)

// ruinWarmSmall, ruinWarmBig, ruinColdSmall and ruinColdBig are the ruin
// templates in BDS's table order (0xEE8E160); the index is drawn.
func ruinWarmSmall() [8]string {
	return [...]string{"ruin_warm1", "ruin_warm2", "ruin_warm3", "ruin_warm4", "ruin_warm5", "ruin_warm6",
		"ruin_warm7", "ruin_warm8"}
}

func ruinWarmBig() [4]string {
	return [...]string{"big_ruin_warm4", "big_ruin_warm5", "big_ruin_warm6", "big_ruin_warm7"}
}

func ruinColdSmall() []string {
	return []string{"ruin4", "ruin1_%s", "ruin2_%s", "ruin3_%s", "ruin4_%s", "ruin5_%s", "ruin6_%s",
		"ruin7_%s", "ruin8_%s"}
}

func ruinColdBig() []string {
	return []string{"big_ruin1_%s", "big_ruin2_%s", "big_ruin3_%s", "big_ruin8_%s"}
}

// Ruin piece sizes: the main piece's box reaches ruinMainSize blocks on
// each axis; a cluster piece's box reaches ruinClusterLong by
// ruinClusterShort.
const (
	ruinMainSize     = 15
	ruinClusterLong  = 6
	ruinClusterShort = 5
)

// Rotated offsets by rotation 0..3: the main piece's corner inside the start
// chunk (dword_13ED8BC, dword_15411CC), its box (dword_15411FC,
// qword_15411F0) and a cluster piece's box (dword_15411E4, qword_15411D8).
func ruinCornerX() [structRotations]int32 { return [...]int32{0, ruinMainSize, ruinMainSize, 0} }

func ruinCornerZ() [structRotations]int32 { return [...]int32{0, 0, ruinMainSize, ruinMainSize} }

func ruinMainX() [structRotations]int32 {
	return [...]int32{ruinMainSize, -ruinMainSize, -ruinMainSize, ruinMainSize}
}

func ruinMainZ() [structRotations]int32 {
	return [...]int32{ruinMainSize, ruinMainSize, -ruinMainSize, -ruinMainSize}
}

func ruinClusterX() [structRotations]int32 {
	return [...]int32{ruinClusterShort, -ruinClusterLong, -ruinClusterShort, ruinClusterLong}
}

func ruinClusterZ() [structRotations]int32 {
	return [...]int32{ruinClusterLong, ruinClusterShort, -ruinClusterLong, -ruinClusterShort}
}

// Ocean ruin layout (sub_4E7D890, sub_4E7ECF0, sub_4E80060).
const (
	// ruinMonumentGap is how many chunks around a ruin start must hold no
	// monument start (the box runs from -ruinMonumentGap to
	// ruinMonumentGap-1).
	ruinMonumentGap = 5
	// ruinSmallIntegrity and ruinBigIntegrity are the main piece's
	// integrity; cluster pieces are small.
	ruinSmallIntegrity = 0.8
	ruinBigIntegrity   = 0.9
	// ruinCrackedIntegrity and ruinMossyIntegrity are a cold ruin's upper
	// layers' integrity.
	ruinCrackedIntegrity = 0.7
	ruinMossyIntegrity   = 0.5
	// ruinClusterMin is the fewest cluster pieces tried; ruinClusterExtra
	// bounds how many more are drawn.
	ruinClusterMin   = 4
	ruinClusterExtra = 4
	// ruinReachChunks is how far, in chunks, a ruin's pieces reach from its
	// start chunk.
	ruinReachChunks = 3
	// ruinDeepDrop is how much deeper than the corner height a column's
	// floor must be to count as deep.
	ruinDeepDrop = 3
	// ruinDeepMargin is how many fewer deep columns than the box's width
	// still lower the piece.
	ruinDeepMargin = 2
	// ruinNoFloor starts the lowest floor search above any world height.
	ruinNoFloor = 512
	// ruinChestFacing is the chest's minecraft:cardinal_direction index
	// (east).
	ruinChestFacing = 3
)

// ruinClusterSpots are the cluster spots (Java's addClusterRuins): x
// range, x offset, z range and z offset from the main piece's low corner.
func ruinClusterSpots() [8][4]int32 {
	return [...][4]int32{
		{7, -15, 6, 17}, {7, -15, 6, 1}, {7, -15, 4, -12}, {6, 1, 6, 17},
		{6, 1, 2, -12}, {6, 17, 5, 19}, {6, 17, 6, 1}, {6, 17, 4, -12},
	}
}

// ruinPiece is one template of a ruin.
type ruinPiece struct {
	path      string
	x, z      int32
	rot       int
	integrity float32
	big, warm bool
}

// initOceanStructures fills the per-biome ocean structure tables.
func (o *Overworld) initOceanStructures(biomeIDs map[string]uint32, n uint32) error {
	o.ruinBiome = make([]ruinBiome, n)
	o.ruinOcean = ruinBiomes()["ocean"]
	for name, rb := range ruinBiomes() {
		if id, ok := biomeIDs[name]; ok {
			o.ruinBiome[id] = rb
		}
	}
	o.monumentDeep, o.monumentWater = biomeMask(biomeIDs, n, monumentDeepBiomes()), biomeMask(biomeIDs, n, monumentWaterBiomes())
	o.shipBiome, o.shipBeach = biomeMask(biomeIDs, n, shipBiomes()), biomeMask(biomeIDs, n, shipBeaches())
	o.treasureBiome = biomeMask(biomeIDs, n, treasureBiomes())
	o.outpostBiome = biomeMask(biomeIDs, n, outpostBiomes())
	o.mansionBiome = biomeMask(biomeIDs, n, mansionBiomes())
	h, err := structure.LoadLiquidHolders()
	if err != nil {
		return err
	}
	o.holdsLiquid = h
	return o.loadTopSupport()
}

// loadTopSupport reads the block states whose top face can hold a plant
// (BDS 1.26.52 states that are solid or whose collision covers the whole
// top face).
func (o *Overworld) loadTopSupport() error {
	raw, err := worldgen.Data.ReadFile("data/bds/top_support.json")
	if err != nil {
		return fmt.Errorf("worldgen: read top support: %w", err)
	}
	var hashes []uint32
	if err := json.Unmarshal(raw, &hashes); err != nil {
		return fmt.Errorf("worldgen: parse top support: %w", err)
	}
	o.topSupport = make([]bool, o.reg.Len())
	for _, h := range hashes {
		if i, ok := o.reg.ByHash(h); ok {
			o.topSupport[i] = true
		}
	}
	return nil
}

// structureY is the y a structure test reads the biome at: the preliminary
// surface of the chunk's corner column, or none if there is none.
func (o *Overworld) structureY(cx, cz, none int32) int32 {
	if y := prelimSurface(o.column(cx*quartsPerChunk, cz*quartsPerChunk)); y != noSurface {
		return y
	}
	return none
}

// monumentPick is the chunk a monument region picks.
func (o *Overworld) monumentPick(rx, rz int32) (int32, int32) {
	if x, z, ok := o.forcedPick(skMonument, rx, rz); ok {
		return x, z
	}
	var r rand.MT
	structureSeed(&r, o.seed, rx, rz, o.place[skMonument].salt)
	n := o.place[skMonument].spacing - o.place[skMonument].separation
	a := r.IntN(n)
	x := rx*o.place[skMonument].spacing + (a+r.IntN(n))/2
	b := r.IntN(n)
	return x, rz*o.place[skMonument].spacing + (b+r.IntN(n))/2
}

// isMonumentChunk reports whether an ocean monument starts in chunk
// (cx, cz).
func (o *Overworld) isMonumentChunk(cx, cz int32) bool {
	x, z := o.monumentPick(structure.Region(cx, o.place[skMonument].spacing), structure.Region(cz, o.place[skMonument].spacing))
	if x != cx || z != cz {
		return false
	}
	if o.isForced(skMonument, cx, cz) {
		return true
	}
	y := o.structureY(cx, cz, noSurfaceY)
	return o.biomesOnly(cx*16+8, y, cz*16+8, monumentDeepReach, o.monumentDeep) &&
		o.biomesOnly(cx*16+8, y, cz*16+8, monumentWaterReach, o.monumentWater)
}

// monumentNear reports whether a monument starts in chunks cx0..cx1,
// cz0..cz1 (the ruin test asks each chunk; only region picks can answer).
func (o *Overworld) monumentNear(cx0, cz0, cx1, cz1 int32) bool {
	for rx := structure.Region(cx0, o.place[skMonument].spacing); rx <= structure.Region(cx1, o.place[skMonument].spacing); rx++ {
		for rz := structure.Region(cz0, o.place[skMonument].spacing); rz <= structure.Region(cz1, o.place[skMonument].spacing); rz++ {
			x, z := o.monumentPick(rx, rz)
			if x >= cx0 && x <= cx1 && z >= cz0 && z <= cz1 && o.isMonumentChunk(x, z) {
				return true
			}
		}
	}
	return false
}

// ruinPick is the chunk a ruin region picks.
func (o *Overworld) ruinPick(rx, rz int32) (int32, int32) {
	if x, z, ok := o.forcedPick(skOceanRuin, rx, rz); ok {
		return x, z
	}
	var r rand.MT
	structureSeed(&r, o.seed, rx, rz, o.place[skOceanRuin].salt)
	n := o.place[skOceanRuin].spacing - o.place[skOceanRuin].separation
	x := rx*o.place[skOceanRuin].spacing + r.IntN(n)
	return x, rz*o.place[skOceanRuin].spacing + r.IntN(n)
}

// ruinAt returns the settings of the ocean ruin starting in chunk (cx, cz);
// ok is false if none does.
func (o *Overworld) ruinAt(cx, cz int32) ruinBiome {
	x, z := o.ruinPick(structure.Region(cx, o.place[skOceanRuin].spacing), structure.Region(cz, o.place[skOceanRuin].spacing))
	if x != cx || z != cz {
		return ruinBiome{}
	}
	if o.isForced(skOceanRuin, cx, cz) {
		return o.forcedRuin(cx, cz)
	}
	if !o.structOK(skOceanRuin, cx, cz) {
		return ruinBiome{}
	}
	b := o.BiomeAt(cx*16+8, o.structureY(cx, cz, noSurfaceY), cz*16+8)
	if int(b) >= len(o.ruinBiome) || !o.ruinBiome[b].ok {
		return ruinBiome{}
	}
	if o.monumentNear(cx-ruinMonumentGap, cz-ruinMonumentGap, cx+ruinMonumentGap-1, cz+ruinMonumentGap-1) {
		return ruinBiome{}
	}
	// The start reads its settings at the same spot, at 0 when the column
	// has no surface.
	b = o.BiomeAt(cx*16+8, o.structureY(cx, cz, 0), cz*16+8)
	if int(b) >= len(o.ruinBiome) || !o.ruinBiome[b].ok {
		return ruinBiome{}
	}
	return o.ruinBiome[b]
}

// ruinPieces lays out the ruin starting in chunk (cx, cz) (sub_4E7D890):
// the main ruin and, for a large one, a cluster of small ones around it.
func (o *Overworld) ruinPieces(cx, cz int32, cfg ruinBiome) []ruinPiece {
	var r rand.MT
	r.Seed(populationSeed(o.seed, cx, cz))
	rot := int(r.IntN(structRotations))
	x0, z0 := cx*16+ruinCornerX()[rot], cz*16+ruinCornerZ()[rot]
	f := r.Float()
	big := cfg.large >= f
	integrity := float32(ruinSmallIntegrity)
	if big {
		integrity = ruinBigIntegrity
	}
	pieces := addRuin(nil, &r, x0, z0, rot, integrity, big, cfg.warm)
	if !big || cfg.cluster < r.Float() {
		return pieces
	}
	minX, maxX := min(x0, x0+ruinMainX()[rot]), max(x0, x0+ruinMainX()[rot])
	minZ, maxZ := min(z0, z0+ruinMainZ()[rot]), max(z0, z0+ruinMainZ()[rot])
	// The cluster spots (Java's addClusterRuins), each drawn x then z.
	table := ruinClusterSpots()
	spots := make([][2]int32, 0, len(table))
	for _, s := range table {
		dx := r.IntN(s[0])
		dz := r.IntN(s[2])
		spots = append(spots, [2]int32{minX + s[1] + dx, minZ + s[3] + dz})
	}
	for range r.IntN(ruinClusterExtra) + ruinClusterMin {
		i := r.IntN(int32(len(spots)))
		p := spots[i]
		spots = slices.Delete(spots, int(i), int(i)+1)
		crot := int(r.IntN(structRotations))
		ex, ez := p[0]+ruinClusterX()[crot], p[1]+ruinClusterZ()[crot]
		if min(p[1], ez) > maxZ || max(p[0], ex) < minX || min(p[0], ex) > maxX || max(p[1], ez) < minZ {
			pieces = addRuin(pieces, &r, p[0], p[1], crot, ruinSmallIntegrity, false, cfg.warm)
		}
	}
	return pieces
}

// addRuin adds one ruin's pieces (sub_4E7ECF0): a warm template, or a cold
// one in three layers (brick, cracked at 0.7, mossy at 0.5).
func addRuin(pieces []ruinPiece, r *rand.MT, x, z int32, rot int, integrity float32, big, warm bool) []ruinPiece {
	if warm {
		small, large := ruinWarmSmall(), ruinWarmBig()
		path := small[r.IntN(int32(len(small)))]
		if big {
			path = large[r.IntN(int32(len(large)))]
		}
		return append(pieces, ruinPiece{"ruin/" + path, x, z, rot, integrity, big, warm})
	}
	group := ruinColdSmall()
	if big {
		group = ruinColdBig()
	}
	name := group[r.IntN(int32(len(group)))]
	return append(pieces,
		ruinLayer(name, "brick", x, z, rot, integrity, big, warm),
		ruinLayer(name, "cracked", x, z, rot, ruinCrackedIntegrity, big, warm),
		ruinLayer(name, "mossy", x, z, rot, ruinMossyIntegrity, big, warm))
}

// ruinStart returns the pieces of the ruin starting in chunk (sx, sz), nil
// if none does, computed once per start.
func (o *Overworld) ruinStart(sx, sz int32) []ruinPiece {
	k := [2]int32{sx, sz}
	if v, ok := o.ruins.Load(k); ok {
		pieces, _ := v.([]ruinPiece)
		return pieces
	}
	var pieces []ruinPiece
	if cfg := o.ruinAt(sx, sz); cfg.ok {
		pieces = o.ruinPieces(sx, sz, cfg)
	}
	o.ruins.Store(k, pieces)
	return pieces
}

// ruinBox is the area a piece covers.
func (o *Overworld) ruinBox(p ruinPiece) (structure.Box, *structure.JigsawTemplate) {
	jt := o.st.Template(p.path)
	if jt == nil {
		return structure.Box{}, nil
	}
	e := structure.RotatePos([3]int32{jt.Template.Size[0] - 1, 0, jt.Template.Size[2] - 1}, p.rot)
	return structure.NewBox(min(p.x, p.x+e[0]), MinY, min(p.z, p.z+e[2]), max(p.x, p.x+e[0]), MaxY-1, max(p.z, p.z+e[2])), jt
}

// ruinsReach calls f for each ruin start with a piece in chunk (cx, cz),
// with its pieces. Pieces reach at most 3 chunks from the start.
func (o *Overworld) ruinsReach(cx, cz int32, f func(sx, sz int32, pieces []ruinPiece)) {
	chunk := structure.NewBox(cx*16, MinY, cz*16, cx*16+15, MaxY-1, cz*16+15)
	for rx := structure.Region(cx-ruinReachChunks, o.place[skOceanRuin].spacing); rx <= structure.Region(cx+ruinReachChunks, o.place[skOceanRuin].spacing); rx++ {
		for rz := structure.Region(cz-ruinReachChunks, o.place[skOceanRuin].spacing); rz <= structure.Region(cz+ruinReachChunks, o.place[skOceanRuin].spacing); rz++ {
			sx, sz := o.ruinPick(rx, rz)
			if sx < cx-ruinReachChunks || sx > cx+ruinReachChunks || sz < cz-ruinReachChunks || sz > cz+ruinReachChunks {
				continue
			}
			pieces := o.ruinStart(sx, sz)
			for _, p := range pieces {
				if b, jt := o.ruinBox(p); jt != nil && b.Intersects(chunk) {
					f(sx, sz, pieces)
					break
				}
			}
		}
	}
}

// ruinReaches reports whether an ocean ruin has a piece in chunk (cx, cz).
func (o *Overworld) ruinReaches(cx, cz int32) bool {
	found := false
	o.ruinsReach(cx, cz, func(int32, int32, []ruinPiece) { found = true })
	return found
}

// placeRuins writes the ocean ruins that reach chunk (cx, cz) into c. raw
// gives undecorated terrain.
func (o *Overworld) placeRuins(cx, cz int32, c *Chunk, raw func(cx, cz int32) *PackedChunk) {
	var b *scBuild
	o.ruinsReach(cx, cz, func(_, _ int32, pieces []ruinPiece) {
		if b == nil {
			b = &scBuild{o: o, c: c, cbox: structure.NewBox(cx*16, MinY, cz*16, cx*16+15, MaxY-1, cz*16+15)}
			b.ids()
			// The chunk's random gives the chests their loot seeds.
			b.r.Seed(populationSeed(o.seed, cx, cz))
		}
		for _, p := range pieces {
			b.ruinPiece(p, raw)
		}
	})
}

// ruinY is a piece's bottom y (sub_4E80060): the height map at its corner,
// moved down to the lowest floor under the piece when that floor is more
// than 3 below and most columns are that deep, then one lower.
func (o *Overworld) ruinY(p ruinPiece, area structure.Box, raw func(cx, cz int32) *PackedChunk) int32 {
	h := int32(MinY)
	for y := MaxY - 1; y >= MinY; y-- {
		if s := o.rawBlock(raw, p.x, int32(y), p.z); s != o.ids.air && o.reg.LightDampening(s) >= fullLightDampening {
			h = int32(y + 1)
			break
		}
	}
	lowest, deep := int32(ruinNoFloor), int32(0)
	for x := area.X0; x <= area.X1; x++ {
		for z := area.Z0; z <= area.Z1; z++ {
			y := h - 1
			for y > MinY+1 {
				s := o.rawBlock(raw, x, y, z)
				if s != o.ids.air && s != o.ids.water && s != o.ids.flowingWater {
					break
				}
				y--
			}
			lowest = min(lowest, y)
			if y < h-ruinDeepDrop {
				deep++
			}
		}
	}
	if lowest-h < -ruinDeepDrop && deep > area.X1-area.X0-ruinDeepMargin {
		h = lowest + 1
	}
	return h - 1
}

// ruinPiece places one ruin template into the chunk (sub_D2B7A10 with the
// piece's settings, then its chest markers).
func (b *scBuild) ruinPiece(p ruinPiece, raw func(cx, cz int32) *PackedChunk) {
	o := b.o
	area, jt := o.ruinBox(p)
	if jt == nil || !area.Intersects(b.cbox) {
		return
	}
	y0 := o.ruinY(p, area, raw)
	conv := o.st.TemplateStates(jt, p.rot)
	// Integrity draws one float per placed template block, inside the chunk
	// or not, from a random seeded with the chunk's population seed.
	var ir rand.MT
	ir.Seed(populationSeed(o.seed, b.cbox.X0>>4, b.cbox.Z0>>4))
	var markers []templateMarker
	for _, tb := range jt.Template.Blocks {
		st := jt.Template.Palette[tb.State]
		rp := structure.RotatePos(tb.Pos, p.rot)
		pos := [3]int32{p.x + rp[0], y0 + rp[1], p.z + rp[2]}
		switch st.Name {
		case blockStructureBlock:
			// Structure blocks and jigsaws are ignored before the draw.
			if m, _ := tb.NBT["metadata"].(string); m == "chest" {
				markers = append(markers, templateMarker{pos, m})
			}
			continue
		case blockJigsaw, blockStructureVoid:
			continue
		}
		if p.integrity < 1 && float64(p.integrity) < float64(ir.Uint32())*0x1p-32 {
			continue
		}
		cs := conv[tb.State]
		if !cs.OK || !b.cbox.Contains(pos) {
			continue
		}
		id, name := cs.ID, o.blockName(cs.ID)
		below := pos[1] < SeaLevel
		if id == o.ids.air && below {
			id, name = o.ids.water, blockWater
		}
		if !b.setWorld(pos[0], pos[1], pos[2], id) {
			continue
		}
		b.dropEntity(pos)
		if o.holdsLiquid[name] && (below || cs.Water) {
			i := (pos[1]-MinY)<<8 | (pos[2]&15)<<4 | pos[0]&15
			b.c.Water[i>>waterWordShift] |= 1 << (i & waterBitMask)
		}
		if d := o.st.BlockEntityFor(id, st.Name, pos[0], pos[1], pos[2]); d != nil {
			b.c.BlockEntities = append(b.c.BlockEntities, structure.BlockEntity{X: pos[0], Y: pos[1], Z: pos[2], Data: d})
		}
	}
	for _, m := range markers {
		b.ruinChest(p, m.pos)
	}
}

// ruinChest handles a chest marker (sub_4E80ED0): the marker spot is
// filled, the chest goes one block under it, waterlogged at or below sea
// level, and gets the big or small ruin loot.
func (b *scBuild) ruinChest(p ruinPiece, pos [3]int32) {
	o := b.o
	if !b.cbox.Contains(pos) {
		return
	}
	fill := blockWater
	if p.warm {
		fill = "minecraft:sandstone"
	}
	switch p.path {
	case "ruin/big_ruin1_brick", "ruin/big_ruin8_brick", "ruin/ruin2_brick":
		fill = "minecraft:gravel"
	case "ruin/ruin3_brick":
		fill = "minecraft:stone_bricks"
	}
	fillID, _ := o.reg.Default(fill)
	if b.setWorld(pos[0], pos[1], pos[2], fillID) {
		b.dropEntity(pos)
	}
	cy := pos[1] - 1
	chest := o.state(blockChest, map[string]any{stateCardinalDirection: cardinalNames[ruinChestFacing]})
	if !b.setWorld(pos[0], cy, pos[2], chest) {
		return
	}
	b.dropEntity([3]int32{pos[0], cy, pos[2]})
	if pos[1] <= SeaLevel {
		i := (cy-MinY)<<8 | (pos[2]&15)<<4 | pos[0]&15
		b.c.Water[i>>waterWordShift] |= 1 << (i & waterBitMask)
	}
	table := "loot_tables/chests/underwater_ruin_small.json"
	if p.big {
		table = "loot_tables/chests/underwater_ruin_big.json"
	}
	if d := o.st.BlockEntityFor(chest, "", pos[0], cy, pos[2]); d != nil {
		d["LootTable"] = table
		d["LootTableSeed"] = int32(b.r.Uint32() >> 1)
		b.c.BlockEntities = append(b.c.BlockEntities, structure.BlockEntity{X: pos[0], Y: cy, Z: pos[2], Data: d, LootTable: table})
	}
}

// NearestOceanRuin finds the ocean ruin nearest to block (x, z), searching
// maxRegions placement regions around it. Kind is "ocean_ruin_warm" or
// "ocean_ruin_cold"; X, Z are the main piece's corner and Y its bottom. It
// is safe for concurrent use.
func (o *Overworld) NearestOceanRuin(x, z int32, maxRegions int32) (Scattered, bool) {
	var cands []locateCand
	rx0, rz0 := structure.Region(x>>4, o.place[skOceanRuin].spacing), structure.Region(z>>4, o.place[skOceanRuin].spacing)
	for rx := rx0 - maxRegions; rx <= rx0+maxRegions; rx++ {
		for rz := rz0 - maxRegions; rz <= rz0+maxRegions; rz++ {
			cx, cz := o.ruinPick(rx, rz)
			dx, dz := int64(cx*16+8-x), int64(cz*16+8-z)
			cands = append(cands, locateCand{cx, cz, dx*dx + dz*dz})
		}
	}
	slices.SortFunc(cands, compareLocateCand)
	for _, c := range cands {
		cfg := o.ruinAt(c.cx, c.cz)
		if !cfg.ok {
			continue
		}
		p := o.ruinPieces(c.cx, c.cz, cfg)[0]
		area, jt := o.ruinBox(p)
		if jt == nil {
			continue
		}
		raw := newRawChunks(o)
		kind := "ocean_ruin_cold"
		if cfg.warm {
			kind = "ocean_ruin_warm"
		}
		return Scattered{Kind: kind, X: p.x, Y: o.ruinY(p, area, raw.get), Z: p.z}, true
	}
	return Scattered{}, false
}

// dropEntity removes the block entity at a position, as replacing its
// block does in BDS (a later layer's chest replaces an earlier one's).
func (b *scBuild) dropEntity(p [3]int32) {
	b.c.BlockEntities = slices.DeleteFunc(b.c.BlockEntities, func(be structure.BlockEntity) bool {
		return be.X == p[0] && be.Y == p[1] && be.Z == p[2]
	})
}

// biomeMask marks the biomes of names in a slice of n biome IDs.
func biomeMask(biomeIDs map[string]uint32, n uint32, names []string) []bool {
	s := make([]bool, n)
	for _, name := range names {
		if id, ok := biomeIDs[name]; ok {
			s[id] = true
		}
	}
	return s
}

// ruinLayer is one layer of a cold ruin: template name with kind filled
// in (ruin4 has none).
func ruinLayer(name, kind string, x, z int32, rot int, integrity float32, big, warm bool) ruinPiece {
	path := name
	if path != "ruin4" {
		path = fmt.Sprintf(name, kind)
	}
	return ruinPiece{"ruin/" + path, x, z, rot, integrity, big, warm}
}

// forcedRuin is the settings of a ruin the Config placed: its biome's, or a
// plain ocean's when its biome has no ruins.
func (o *Overworld) forcedRuin(cx, cz int32) ruinBiome {
	b := o.BiomeAt(cx*16+8, o.structureY(cx, cz, 0), cz*16+8)
	if int(b) < len(o.ruinBiome) && o.ruinBiome[b].ok {
		return o.ruinBiome[b]
	}
	return o.ruinOcean
}
