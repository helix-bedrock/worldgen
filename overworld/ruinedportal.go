package overworld

import (
	"cmp"
	"slices"

	"github.com/helix-bedrock/worldgen/rand"
	"github.com/helix-bedrock/worldgen/structure"
)

// Ruined portals in the overworld, written from BDS 1.26.52:
// RuinedPortalFeature (constructor sub_4E87930, chunk test sub_4E87A80,
// start sub_4E87BE0) and RuinedPortalPiece (postProcess sub_4E88070 with
// its helpers sub_4E8A680, sub_4E8A710, sub_4E8A7C0, sub_4E8A9F0 and
// sub_4E8AC30). Bedrock builds the whole portal from its start chunk, in
// this order, all from that chunk's random:
//   - the underground roll, the turn, the z mirror, then the template
//     (giant_portal_1..3 one time in 20, else portal_1..10);
//   - the height: buried in the ground on the underground roll (no lower
//     than 70 in extreme_hills and mountain biomes, else 15 above the
//     bottom), partly buried in deserts, else on the surface; then lowered
//     until three of the box's four bottom corners stand on something;
//   - a spread of netherrack (magma one time in 20) around the box, each
//     spot dripping a short netherrack column down;
//   - the template, keeping its air (an air pocket) on the underground roll
//     and half of the other times outside deserts, waterlogged when a box
//     corner is in water; its chest draws a loot seed;
//   - more drips under the netherrack of the template's bottom layer;
//   - an aging pass over every block in the box, the terrain included:
//     stone bricks (and plain stone) crack or turn to stairs, stairs to
//     slabs, slabs and walls go mossy (more so in jungles, swamps and under
//     water, never in deserts), netherrack turns to magma outside ice
//     biomes, obsidian cries, lava hardens and gold blocks vanish.
// Bedrock's box is one block larger than the template on each axis, so the
// aging pass also reaches a row of terrain past the template.
// Bedrock differs from Java: there is no biome filter or portal type; a
// desert portal can still be buried, and Bedrock adds no vines or leaves.
// In the Nether (the same code on its own grid) the portal sits 15 to 99
// above the bottom before it is lowered, keeps no air below sea level and
// gets no moss, and the aging pass turns stone bricks and stone into
// polished blackstone blocks, slabs, stairs and walls, and iron bars into
// chains.
// VERIFY: heights and corners in BDS read the live world of the start
// chunk and its neighbours; here the undecorated terrain. The underwater
// test of the box corners runs only for worlds of a recent base game
// version (sub_4EA7A10); it is assumed to run. A final pass that refreshes
// blocks with the minecraft:corner state is not done. Java's stone_slab has
// no Bedrock name in BDS's table and places nothing.

// Ruined portal placement (sub_4E87A80): linear spread.
const (
	portalSalt       = 40552231
	portalSpacing    = 40
	portalSeparation = 15
)

// Nether ruined portals use the same salt on a smaller grid.
const (
	NetherPortalSpacing    = 25
	NetherPortalSeparation = 10
)

// portalTemplates are BDS's template tables (off_F456D50, off_F456E40).
var (
	portalSmall = []string{"portal_1", "portal_2", "portal_3", "portal_4", "portal_5", "portal_6", "portal_7",
		"portal_8", "portal_9", "portal_10"}
	portalGiant = []string{"giant_portal_1", "giant_portal_2", "giant_portal_3"}
)

// portalSpread is the netherrack chance by distance (qword_1523CB8).
var portalSpread = [14]float32{1, 1, 1, 1, 1, 1, 1, 0.9, 0.9, 0.8, 0.7, 0.6, 0.4, 0.2}

// Block families the aging pass looks at (xmmword_F60D4B0, F60D480,
// F60D450, F60D528, plus the blocks it names one by one).
var (
	portalFullStone = map[string]bool{
		"minecraft:stone_bricks": true, "minecraft:mossy_stone_bricks": true, "minecraft:cracked_stone_bricks": true,
		"minecraft:chiseled_stone_bricks": true, "minecraft:smooth_stone_double_slab": true,
		"minecraft:sandstone_double_slab": true, "minecraft:petrified_oak_double_slab": true,
		"minecraft:cobblestone_double_slab": true, "minecraft:brick_double_slab": true,
		"minecraft:stone_brick_double_slab": true, "minecraft:quartz_double_slab": true,
		"minecraft:nether_brick_double_slab": true, "minecraft:oak_double_slab": true, "minecraft:stone": true,
	}
	portalSlabs = map[string]bool{
		"minecraft:smooth_stone_slab": true, "minecraft:sandstone_slab": true, "minecraft:petrified_oak_slab": true,
		"minecraft:cobblestone_slab": true, "minecraft:brick_slab": true, "minecraft:stone_brick_slab": true,
		"minecraft:quartz_slab": true, "minecraft:nether_brick_slab": true, "minecraft:oak_slab": true,
	}
	portalWalls = map[string]bool{
		"minecraft:cobblestone_wall": true, "minecraft:mossy_cobblestone_wall": true, "minecraft:granite_wall": true,
		"minecraft:diorite_wall": true, "minecraft:andesite_wall": true, "minecraft:sandstone_wall": true,
		"minecraft:brick_wall": true, "minecraft:stone_brick_wall": true, "minecraft:mossy_stone_brick_wall": true,
		"minecraft:nether_brick_wall": true, "minecraft:end_stone_brick_wall": true, "minecraft:prismarine_wall": true,
		"minecraft:red_sandstone_wall": true, "minecraft:red_nether_brick_wall": true,
	}
)

// portalWrite is one block a ruined portal leaves in the world.
type portalWrite struct {
	x, y, z int32
	id      uint32
	water   bool
	be      map[string]any
}

// ruinedPortal is a built portal: its box and its blocks by chunk.
type ruinedPortal struct {
	path   string
	rot    int
	mirror bool
	under  bool
	bb     structure.Box
	writes map[[2]int32][]portalWrite
}

// portalPick is the chunk a ruined portal region picks.
func (o *Overworld) portalPick(rx, rz int32) (int32, int32) {
	return pickPortal(o.seed, rx, rz, portalSpacing, portalSeparation)
}

func pickPortal(seed int64, rx, rz, spacing, separation int32) (int32, int32) {
	var r rand.MT
	structureSeed(&r, seed, rx, rz, portalSalt)
	n := spacing - separation
	x := rx*spacing + r.IntN(n)
	return x, rz*spacing + r.IntN(n)
}

// NetherPortalPick is the start chunk of the Nether ruined portal of
// placement region (rx, rz), NetherPortalSpacing chunks a side. Every
// region has one.
func NetherPortalPick(seed int64, rx, rz int32) (cx, cz int32) {
	return pickPortal(seed, rx, rz, NetherPortalSpacing, NetherPortalSeparation)
}

// isPortalChunk reports whether a ruined portal starts in chunk (cx, cz).
// Every region has one.
func (o *Overworld) isPortalChunk(cx, cz int32) bool {
	x, z := o.portalPick(floorDivRegion(cx, portalSpacing), floorDivRegion(cz, portalSpacing))
	return x == cx && z == cz
}

// portalsReach calls f for each portal start whose blocks may reach chunk
// (cx, cz): the box is within 16 blocks of the start chunk's corner and
// the netherrack spreads 14 past its middle.
func (o *Overworld) portalsReach(cx, cz int32, f func(sx, sz int32)) {
	for rx := floorDivRegion(cx-2, portalSpacing); rx <= floorDivRegion(cx+2, portalSpacing); rx++ {
		for rz := floorDivRegion(cz-2, portalSpacing); rz <= floorDivRegion(cz+2, portalSpacing); rz++ {
			sx, sz := o.portalPick(rx, rz)
			if sx >= cx-2 && sx <= cx+2 && sz >= cz-2 && sz <= cz+2 {
				f(sx, sz)
			}
		}
	}
}

// portalReaches reports whether a ruined portal may have blocks in chunk
// (cx, cz).
func (o *Overworld) portalReaches(cx, cz int32) bool {
	found := false
	o.portalsReach(cx, cz, func(int32, int32) { found = true })
	return found
}

// placePortals writes the ruined portals that reach chunk (cx, cz) into c.
// raw gives undecorated terrain.
func (o *Overworld) placePortals(cx, cz int32, c *Chunk, raw func(cx, cz int32) *PackedChunk) {
	var b *scBuild
	o.portalsReach(cx, cz, func(sx, sz int32) {
		p := o.portalAt(sx, sz, raw)
		if p == nil {
			return
		}
		ws := p.writes[[2]int32{cx, cz}]
		if len(ws) == 0 {
			return
		}
		if b == nil {
			b = &scBuild{o: o, c: c, cbox: structure.NewBox(cx*16, MinY, cz*16, cx*16+15, MaxY-1, cz*16+15)}
		}
		for _, w := range ws {
			if !b.setWorld(w.x, w.y, w.z, w.id) {
				continue
			}
			b.dropEntity([3]int32{w.x, w.y, w.z})
			if w.water {
				i := (w.y-MinY)<<8 | (w.z&15)<<4 | w.x&15
				c.Water[i>>6] |= 1 << (i & 63)
			}
			if w.be != nil {
				c.BlockEntities = append(c.BlockEntities, structure.BlockEntity{X: w.x, Y: w.y, Z: w.z, Data: w.be})
			}
		}
	})
}

// portalAt returns the ruined portal starting in chunk (cx, cz), nil if
// none does. It is built once per start.
func (o *Overworld) portalAt(cx, cz int32, raw func(cx, cz int32) *PackedChunk) *ruinedPortal {
	k := [2]int32{cx, cz}
	if v, ok := o.portals.Load(k); ok {
		p, _ := v.(*ruinedPortal)
		return p
	}
	var p *ruinedPortal
	if o.isPortalChunk(cx, cz) {
		p = o.buildPortal(cx, cz, o.portalSim(raw))
	}
	o.portals.Store(k, p)
	return p
}

// portalSim is the world a portal is built in: the world before it (in the
// overworld, undecorated terrain) with the portal's own blocks on top.
type portalSim struct {
	o          *Overworld
	read       func(x, y, z int32) uint32
	minY, maxY int32
	sea        int32
	nether     bool
	set        map[[3]int32]portalWrite
}

// portalSim is the overworld's: raw gives undecorated terrain.
func (o *Overworld) portalSim(raw func(cx, cz int32) *PackedChunk) *portalSim {
	chunks := map[[2]int32]*PackedChunk{}
	read := func(x, y, z int32) uint32 {
		k := [2]int32{x >> 4, z >> 4}
		c, ok := chunks[k]
		if !ok {
			c = raw(k[0], k[1])
			chunks[k] = c
		}
		if c == nil {
			return o.ids.air
		}
		return c.at(int(y-MinY)<<8 | int(z&15)<<4 | int(x&15))
	}
	return &portalSim{o: o, read: read, minY: MinY, maxY: MaxY, sea: SeaLevel, set: map[[3]int32]portalWrite{}}
}

func (s *portalSim) get(x, y, z int32) uint32 {
	if y < s.minY || y >= s.maxY {
		return s.o.ids.air
	}
	if w, ok := s.set[[3]int32{x, y, z}]; ok {
		return w.id
	}
	return s.read(x, y, z)
}

// put is setBlock; the bottom bedrock is never replaced.
func (s *portalSim) put(x, y, z int32, id uint32, water bool, be map[string]any) {
	if y < s.minY || y >= s.maxY || (y < s.minY+5 && s.get(x, y, z) == s.o.ids.bedrock) {
		return
	}
	s.set[[3]int32{x, y, z}] = portalWrite{x, y, z, id, water, be}
}

func (s *portalSim) isWater(b uint32) bool { return b == s.o.ids.water || b == s.o.ids.flowingWater }

func (s *portalSim) isLava(b uint32) bool { return b == s.o.ids.lava || b == s.o.ids.flowingLava }

// height is one above the top solid block of a column, or of water when
// water is set, looking through leaves (sub_C9F7970).
func (s *portalSim) height(x, z int32, water bool) int32 {
	y := s.maxY - 1
	if s.nether {
		// Under a roof (the Nether's dimension flag, sub_C8D5D50) BDS first
		// goes down through the solid roof, so it finds the floor under it.
		for ; y >= s.minY; y-- {
			b := s.get(x, y, z)
			if b == s.o.ids.air || !(water && s.isWater(b)) && !s.o.solidMaterial(b) {
				break
			}
		}
	}
	for ; y >= s.minY; y-- {
		b := s.get(x, y, z)
		if b == s.o.ids.air || s.o.hasBlockTag(b, "leaves") {
			continue
		}
		if (water && s.isWater(b)) || s.o.solidMaterial(b) {
			return y + 1
		}
	}
	return s.minY
}

// biomeHas reports whether a biome has a tag.
func (o *Overworld) biomeHas(b uint32, tag string) bool {
	if int(b) >= len(o.biomeTags) {
		return false
	}
	return slices.Contains(o.biomeTags[b], tag)
}

// buildPortal is RuinedPortalPiece's postProcess in the start chunk.
func (o *Overworld) buildPortal(cx, cz int32, s *portalSim) *ruinedPortal {
	// No Nether biome has the tags the portal looks for.
	var biome uint32
	var ocean, desert, cold bool
	if !s.nether {
		biome = o.BiomeAt(cx*16+8, o.structureY(cx, cz, 1), cz*16+8)
		ocean = o.biomeHas(biome, "ocean") || o.biomeHas(biome, "swamp")
		desert = o.biomeHas(biome, "desert")
		cold = o.biomeHas(biome, "ice")
	}

	var r rand.MT
	r.Seed(populationSeed(o.seed, cx, cz))
	under := r.Float() < 0.5
	rot := int(r.IntN(4))
	mirror := r.Float() >= 0.5
	ox, oz := cx*16, cz*16
	h := s.height(ox, oz, !ocean)
	var path string
	if r.Float() >= 0.05 {
		path = portalSmall[r.IntN(10)]
	} else {
		path = portalGiant[r.IntN(3)]
	}
	jt := o.st.Template("ruined_portal/" + path)
	if jt == nil {
		return nil
	}

	// The box spans the template's size, not size-1, from the origin.
	sz := jt.Template.Size[2]
	if mirror {
		sz = -sz
	}
	e := structure.RotatePos([3]int32{jt.Template.Size[0], 0, sz}, rot)
	y := h - 1
	bb := structure.NewBox(min(ox, ox+e[0]), y, min(oz, oz+e[2]), max(ox, ox+e[0]), y+jt.Template.Size[1], max(oz, oz+e[2]))
	mid := func(a0, a1 int32) int32 { return a0 + (a1-a0+1)/2 }
	surface := s.height(mid(bb.X0, bb.X1), mid(bb.Z0, bb.Z1), !ocean)
	y = surface - 1
	switch {
	case s.nether:
		y = s.minY + 15 + r.IntN(85)
	case !under:
		if desert {
			y = surface - (bb.Y1 - bb.Y0) + r.IntN(6)
		}
	default:
		b := o.BiomeAt(mid(bb.X0, bb.X1), mid(bb.Y0, bb.Y1), mid(bb.Z0, bb.Z1))
		low := s.minY + 15
		if o.biomeHas(b, "extreme_hills") || o.biomeHas(b, "mountain") {
			low = SeaLevel + 7
		}
		y += bb.Y0 - bb.Y1 - 1
		if y > low {
			y = low + r.IntN(y-low+1)
		}
	}
	bb = bb.Raise(y - bb.Y0)

	// Lower the box until three of its bottom corners stand on something.
	standing := func(x, z int32) bool {
		b := s.get(x, bb.Y0-1, z)
		if o.hasBlockTag(b, "leaves") {
			return false
		}
		return (!ocean && s.isWater(b)) || o.solidMaterial(b)
	}
	for bb.Y0 > s.minY+15 {
		n := 0
		for _, c := range [4][2]int32{{bb.X0, bb.Z0}, {bb.X0, bb.Z1}, {bb.X1, bb.Z0}, {bb.X1, bb.Z1}} {
			if standing(c[0], c[1]) {
				n++
			}
		}
		if n >= 3 {
			break
		}
		bb = bb.Raise(-1)
	}

	ids := o.portalIDs()
	buried := desert || under
	netherrack := func(x, y, z int32) {
		id := ids.netherrack
		if r.Float() < 0.05 {
			id = ids.magma
		}
		s.put(x, y, z, id, false, nil)
	}
	drip := func(x, y, z int32) {
		netherrack(x, y, z)
		for i := int32(1); i <= 8 && r.Float() < 0.5; i++ {
			netherrack(x, y-i, z)
		}
	}
	spread := r.IntN(max(8-((bb.X1-bb.X0)+(bb.Z1-bb.Z0)+2)/4, 1))
	mx, mz := mid(bb.X0, bb.X1), mid(bb.Z0, bb.Z1)
	for x := mx - 14; x <= mx+14; x++ {
		for dz := int32(-14); dz <= 14; dz++ {
			d := spread + iabs(mx-x) + iabs(dz)
			if d > 13 || float64(portalSpread[d]) <= r.Float64() {
				continue
			}
			z := mz + dz
			hy := s.height(x, z, !ocean) - 1
			if buried {
				hy = min(hy, bb.Y0)
			}
			if iabs(bb.Y0-hy) <= 3 {
				netherrack(x, hy, z)
				drip(x, hy-1, z)
			}
		}
	}

	keepAir := under
	switch {
	case s.nether && bb.Y0 < s.sea:
		keepAir = false
	case !under && !desert:
		keepAir = r.Float() < 0.5
	}
	wet := false
	for _, c := range [8][3]int32{
		{bb.X0, bb.Y1, bb.Z0}, {bb.X0, bb.Y1, bb.Z1}, {bb.X1, bb.Y1, bb.Z0}, {bb.X1, bb.Y1, bb.Z1},
		{bb.X0, bb.Y0, bb.Z0}, {bb.X0, bb.Y0, bb.Z1}, {bb.X1, bb.Y0, bb.Z0}, {bb.X1, bb.Y0, bb.Z1},
	} {
		if s.get(c[0], c[1], c[2]) == o.ids.water {
			wet = true
			break
		}
	}
	o.portalTemplate(s, &r, jt, ox, bb.Y0, oz, rot, mirror, keepAir, wet)

	for x := bb.X0 + 1; x < bb.X1; x++ {
		for z := bb.Z0 + 1; z < bb.Z1; z++ {
			if s.get(x, bb.Y0, z) == ids.netherrack {
				drip(x, bb.Y0-1, z)
			}
		}
	}

	moss := float32(0.8)
	if s.nether {
		moss = 0
	} else if !wet {
		switch {
		case desert:
			moss = 0
		case o.biomeHas(biome, "jungle"):
			moss = 0.8
		case o.biomeHas(biome, "swamp"):
			moss = 0.5
		default:
			moss = 0.2
		}
	}
	o.agePortal(s, &r, bb, ids, moss, ocean, cold)

	p := &ruinedPortal{path: path, rot: rot, mirror: mirror, under: under, bb: bb, writes: map[[2]int32][]portalWrite{}}
	for _, w := range s.set {
		k := [2]int32{w.x >> 4, w.z >> 4}
		p.writes[k] = append(p.writes[k], w)
	}
	return p
}

// portalTemplate places the template at the origin, mirrored along z then
// turned. Air is skipped unless keepAir; wet waterlogs below sea level.
// Like every legacy template placement, a chest without a loot seed draws
// one from r.
func (o *Overworld) portalTemplate(s *portalSim, r *rand.MT, jt *structure.JigsawTemplate, ox, oy, oz int32, rot int, mirror, keepAir, wet bool) {
	m := structure.MirrorNone
	if mirror {
		m = structure.MirrorZ
	}
	conv := o.st.TemplateStatesMirror(jt, rot, m)
	for _, tb := range jt.Template.Blocks {
		st := jt.Template.Palette[tb.State]
		switch st.Name {
		case "minecraft:structure_block", "minecraft:jigsaw", "minecraft:structure_void":
			continue
		}
		cs := conv[tb.State]
		if !cs.OK || (!keepAir && cs.ID == o.ids.air) {
			continue
		}
		p := tb.Pos
		if mirror {
			p[2] = -p[2]
		}
		p = structure.RotatePos(p, rot)
		x, y, z := ox+p[0], oy+p[1], oz+p[2]
		id, name := cs.ID, o.blockName(cs.ID)
		below := wet && y < s.sea
		if id == o.ids.air && below {
			id, name = o.ids.water, "minecraft:water"
		}
		water := o.holdsLiquid[name] && (below || cs.Water)
		be := o.st.BlockEntityFor(id, st.Name, x, y, z)
		if be != nil && be["id"] == "Chest" {
			be["LootTableSeed"] = int32(r.Uint32() >> 1)
		}
		s.put(x, y, z, id, water, be)
	}
}

// portalIDs are the blocks the portal places.
type portalIDs struct {
	netherrack, magma, cracked, mossyBricks, crying, lava, slabMossy, wallMossy, stoneSlab, brickSlab uint32
	// The Nether's aging blocks.
	blackstone, chiseledBlackstone, blackBricks, crackedBlackBricks, blackSlab, blackWall, chain uint32
}

func (o *Overworld) portalIDs() portalIDs {
	id := func(name string) uint32 {
		s, _ := o.reg.Default("minecraft:" + name)
		return s
	}
	return portalIDs{
		netherrack: id("netherrack"), magma: id("magma"), cracked: id("cracked_stone_bricks"),
		mossyBricks: id("mossy_stone_bricks"), crying: id("crying_obsidian"), lava: id("lava"),
		slabMossy: id("mossy_stone_brick_slab"), wallMossy: id("mossy_stone_brick_wall"),
		stoneSlab: id("normal_stone_slab"), brickSlab: id("stone_brick_slab"),
		blackstone: id("polished_blackstone"), chiseledBlackstone: id("chiseled_polished_blackstone"),
		blackBricks: id("polished_blackstone_bricks"), crackedBlackBricks: id("cracked_polished_blackstone_bricks"),
		blackSlab: id("polished_blackstone_brick_slab"), blackWall: id("polished_blackstone_brick_wall"),
		chain: id("iron_chain"),
	}
}

// agePortal is the aging pass over the box: x, then y, then z.
func (o *Overworld) agePortal(s *portalSim, r *rand.MT, bb structure.Box, ids portalIDs, moss float32, ocean, cold bool) {
	// stairs draws a facing and a half (sub_4E8A9F0).
	stairs := func(name string) uint32 {
		d := r.IntN(4)
		up := r.Float() < 0.5
		return o.state(name, map[string]any{"weirdo_direction": d, "upside_down_bit": up})
	}
	pick := func(normal, mossy [2]uint32) uint32 {
		f := r.Float()
		i := r.IntN(2)
		if moss <= f {
			return normal[i]
		}
		return mossy[i]
	}
	for x := bb.X0; x <= bb.X1; x++ {
		for y := bb.Y0; y <= bb.Y1; y++ {
			for z := bb.Z0; z <= bb.Z1; z++ {
				b := s.get(x, y, z)
				name := o.blockName(b)
				set := func(id uint32) { s.put(x, y, z, id, false, nil) }
				switch {
				case s.nether && portalFullStone[name]:
					switch name {
					case "minecraft:stone":
						st := stairs("minecraft:polished_blackstone_stairs")
						set([2]uint32{ids.blackstone, st}[r.IntN(2)])
					case "minecraft:chiseled_stone_bricks":
						set(ids.chiseledBlackstone)
					default:
						st := stairs("minecraft:polished_blackstone_brick_stairs")
						set([3]uint32{ids.blackBricks, ids.crackedBlackBricks, st}[r.IntN(3)])
					}
				case portalFullStone[name]:
					if r.Float() >= 0.5 {
						normal := [2]uint32{ids.cracked, stairs("minecraft:stone_brick_stairs")}
						mossy := [2]uint32{ids.mossyBricks, stairs("minecraft:mossy_stone_brick_stairs")}
						set(pick(normal, mossy))
					}
				case name == "minecraft:stone_brick_stairs":
					st, _ := o.reg.State(b)
					f := r.Float()
					switch {
					case s.nether && f < 0.5:
						set(o.state("minecraft:polished_blackstone_brick_stairs", map[string]any{
							"weirdo_direction": st.Props["weirdo_direction"], "upside_down_bit": st.Props["upside_down_bit"]}))
					case s.nether:
						set(ids.blackSlab)
					case f >= 0.5:
						ms := o.state("minecraft:mossy_stone_brick_stairs", map[string]any{
							"weirdo_direction": st.Props["weirdo_direction"], "upside_down_bit": st.Props["upside_down_bit"]})
						set(pick([2]uint32{ids.stoneSlab, ids.brickSlab}, [2]uint32{ms, ids.slabMossy}))
					}
				case portalSlabs[name]:
					switch {
					case s.nether:
						set(ids.blackSlab)
					case moss > r.Float():
						set(ids.slabMossy)
					}
				case portalWalls[name]:
					switch {
					case s.nether:
						set(ids.blackWall)
					case moss > r.Float():
						set(ids.wallMossy)
					}
				case name == "minecraft:iron_bars":
					if s.nether {
						set(ids.chain)
					}
				case b == ids.netherrack:
					if !cold && r.Float() < 0.1 {
						set(ids.magma)
					}
				case name == "minecraft:obsidian":
					if r.Float() < 0.2 {
						set(ids.crying)
					}
				case s.isLava(b):
					switch {
					case ocean:
						set(ids.magma)
					case cold:
						set(ids.netherrack)
					case r.Float() < 0.2:
						set(ids.magma)
					}
				case name == "minecraft:gold_block":
					if r.Float() < 0.3 {
						id := o.ids.air
						for _, n := range [5][3]int32{{x - 1, y, z}, {x + 1, y, z}, {x, y, z - 1}, {x, y, z + 1}, {x, y + 1, z}} {
							if s.isLava(s.get(n[0], n[1], n[2])) {
								id = ids.lava
								break
							}
						}
						set(id)
					}
				}
			}
		}
	}
}

func iabs(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

// NearestRuinedPortal finds the ruined portal nearest to block (x, z),
// searching maxRegions placement regions (40 chunks each) around it, and
// returns the middle of its box: above it, or inside it for a buried one
// (Kind "buried ruined_portal"). It is safe for concurrent use.
func (o *Overworld) NearestRuinedPortal(x, z int32, maxRegions int32) (Scattered, bool) {
	type cand struct {
		cx, cz int32
		d      int64
	}
	var cands []cand
	rx0, rz0 := floorDivRegion(x>>4, portalSpacing), floorDivRegion(z>>4, portalSpacing)
	for rx := rx0 - maxRegions; rx <= rx0+maxRegions; rx++ {
		for rz := rz0 - maxRegions; rz <= rz0+maxRegions; rz++ {
			cx, cz := o.portalPick(rx, rz)
			dx, dz := int64(cx*16+8-x), int64(cz*16+8-z)
			cands = append(cands, cand{cx, cz, dx*dx + dz*dz})
		}
	}
	slices.SortFunc(cands, func(a, b cand) int { return cmp.Compare(a.d, b.d) })
	raw := map[[2]int32]*PackedChunk{}
	get := func(cx, cz int32) *PackedChunk {
		k := [2]int32{cx, cz}
		if pc, ok := raw[k]; ok {
			return pc
		}
		var ch Chunk
		o.GenerateChunk(cx, cz, &ch)
		pc := Pack(&ch)
		raw[k] = pc
		return pc
	}
	for _, c := range cands {
		p := o.portalAt(c.cx, c.cz, get)
		if p == nil {
			continue
		}
		s := Scattered{Kind: "ruined_portal", X: (p.bb.X0 + p.bb.X1) / 2, Y: p.bb.Y1 + 1, Z: (p.bb.Z0 + p.bb.Z1) / 2}
		if p.under {
			// A buried portal keeps its air: its middle is open.
			s.Kind, s.Y = "buried ruined_portal", p.bb.Y0+1
		}
		return s, true
	}
	return Scattered{}, false
}

// NetherPortal is a built Nether ruined portal.
type NetherPortal struct {
	// X0 to Z1 is the portal's box, inclusive.
	X0, Y0, Z0, X1, Y1, Z1 int32
	// Blocks are the blocks it leaves, by chunk.
	Blocks map[[2]int32][]StructureBlock
}

// StructureBlock is one block a structure leaves in the world.
type StructureBlock struct {
	X, Y, Z int32
	ID      uint32
	// Data is the block entity, nil for none.
	Data map[string]any
}

// NetherRuinedPortal builds the Nether ruined portal starting in chunk
// (cx, cz) (see NetherPortalPick) into a Nether of the given height and sea
// level. block reads the Nether as it is before the portal. The Nether must
// share o's seed and block registry. It returns nil if the template is
// missing. It is safe for concurrent use.
func (o *Overworld) NetherRuinedPortal(cx, cz, height, sea int32, block func(x, y, z int32) uint32) *NetherPortal {
	s := &portalSim{o: o, read: block, maxY: height, sea: sea, nether: true, set: map[[3]int32]portalWrite{}}
	p := o.buildPortal(cx, cz, s)
	if p == nil {
		return nil
	}
	np := &NetherPortal{X0: p.bb.X0, Y0: p.bb.Y0, Z0: p.bb.Z0, X1: p.bb.X1, Y1: p.bb.Y1, Z1: p.bb.Z1,
		Blocks: make(map[[2]int32][]StructureBlock, len(p.writes))}
	for k, ws := range p.writes {
		bs := make([]StructureBlock, len(ws))
		for i, w := range ws {
			bs[i] = StructureBlock{w.x, w.y, w.z, w.id, w.be}
		}
		np.Blocks[k] = bs
	}
	return np
}
