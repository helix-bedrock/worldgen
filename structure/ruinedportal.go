package structure

import (
	"github.com/helix-bedrock/worldgen/internal/mathx"
	"github.com/helix-bedrock/worldgen/rand"
)

// Ruined portals, written from BDS 1.26.52:
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

// Ruined portal placement (sub_4E87A80): linear spread. The Nether uses
// the same salt on a smaller grid.
const (
	portalSalt             = 40552231
	PortalSpacing          = 40
	PortalSeparation       = 15
	NetherPortalSpacing    = 25
	NetherPortalSeparation = 10
)

// PortalPick is the start chunk of the ruined portal of placement region
// (rx, rz) on a grid of the given spacing and separation, in chunks. Every
// region has one.
func PortalPick(seed int64, rx, rz, spacing, separation int32) (cx, cz int32) {
	var r rand.MT
	r.Seed(RegionSeed(seed, portalSalt, rx, rz))
	n := spacing - separation
	cx = rx*spacing + r.IntN(n)
	return cx, rz*spacing + r.IntN(n)
}

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

// PortalWorld is the world a ruined portal is built into.
type PortalWorld struct {
	// Block reads the world as it is before the portal.
	Block           func(x, y, z int32) uint32
	MinY, MaxY, Sea int32
	Nether          bool
	// Solid is the solid material test of the height and corner checks;
	// Leaves the leaves tag, nil when no block has it.
	Solid, Leaves func(b uint32) bool
	// BiomeHas reports whether the biome at a position has a tag, and
	// StartY is the height the start chunk's biome is read at. BiomeHas is
	// nil in the Nether: no Nether biome has the tags the portal looks for.
	BiomeHas func(x, y, z int32, tag string) bool
	StartY   int32
}

// RuinedPortal is a built ruined portal.
type RuinedPortal struct {
	// Path is the template, under ruined_portal/.
	Path   string
	Rot    int
	Mirror bool
	// Under is the underground roll: a buried portal keeps its air.
	Under bool
	Box   Box
	// Blocks are the blocks it leaves, by chunk.
	Blocks map[[2]int32][]PortalBlock
}

// PortalBlock is one block a ruined portal leaves in the world.
type PortalBlock struct {
	X, Y, Z int32
	ID      uint32
	// Water is set when the block is waterlogged.
	Water bool
	// Data is the block entity, nil for none.
	Data map[string]any
}

// portalSim is the world a portal is built in: the world before it with
// the portal's own blocks on top.
type portalSim struct {
	l   *Lib
	w   *PortalWorld
	set map[[3]int32]PortalBlock
}

func (s *portalSim) get(x, y, z int32) uint32 {
	if y < s.w.MinY || y >= s.w.MaxY {
		return s.l.air
	}
	if b, ok := s.set[[3]int32{x, y, z}]; ok {
		return b.ID
	}
	return s.w.Block(x, y, z)
}

// put is setBlock; the bottom bedrock is never replaced.
func (s *portalSim) put(x, y, z int32, id uint32, water bool, be map[string]any) {
	if y < s.w.MinY || y >= s.w.MaxY || (y < s.w.MinY+5 && s.get(x, y, z) == s.l.bedrock) {
		return
	}
	s.set[[3]int32{x, y, z}] = PortalBlock{x, y, z, id, water, be}
}

func (s *portalSim) isLava(b uint32) bool { return b == s.l.lava || b == s.l.flowingLava }

func (s *portalSim) leaves(b uint32) bool { return s.w.Leaves != nil && s.w.Leaves(b) }

// height is one above the top solid block of a column, or of water when
// water is set, looking through leaves (sub_C9F7970).
func (s *portalSim) height(x, z int32, water bool) int32 {
	y := s.w.MaxY - 1
	if s.w.Nether {
		// Under a roof (the Nether's dimension flag, sub_C8D5D50) BDS first
		// goes down through the solid roof, so it finds the floor under it.
		for ; y >= s.w.MinY; y-- {
			b := s.get(x, y, z)
			if b == s.l.air || !(water && s.l.IsWater(b)) && !s.w.Solid(b) {
				break
			}
		}
	}
	for ; y >= s.w.MinY; y-- {
		b := s.get(x, y, z)
		if b == s.l.air || s.leaves(b) {
			continue
		}
		if (water && s.l.IsWater(b)) || s.w.Solid(b) {
			return y + 1
		}
	}
	return s.w.MinY
}

// biomeHas reports whether the biome at a position has a tag.
func (s *portalSim) biomeHas(x, y, z int32, tag string) bool {
	return s.w.BiomeHas != nil && s.w.BiomeHas(x, y, z, tag)
}

// RuinedPortal builds the ruined portal starting in chunk (cx, cz) into w:
// RuinedPortalPiece's postProcess in the start chunk. It returns nil if the
// template is missing. It is safe for concurrent use.
func (l *Lib) RuinedPortal(cx, cz int32, w PortalWorld) *RuinedPortal {
	s := &portalSim{l: l, w: &w, set: map[[3]int32]PortalBlock{}}
	// No Nether biome has the tags the portal looks for.
	startHas := func(tag string) bool { return s.biomeHas(cx*16+8, w.StartY, cz*16+8, tag) }
	ocean := startHas("ocean") || startHas("swamp")
	desert := startHas("desert")
	cold := startHas("ice")

	var r rand.MT
	r.Seed(l.populationSeed(cx, cz))
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
	jt := l.Template("ruined_portal/" + path)
	if jt == nil {
		return nil
	}

	// The box spans the template's size, not size-1, from the origin.
	sz := jt.Template.Size[2]
	if mirror {
		sz = -sz
	}
	e := RotatePos([3]int32{jt.Template.Size[0], 0, sz}, rot)
	y := h - 1
	bb := NewBox(min(ox, ox+e[0]), y, min(oz, oz+e[2]), max(ox, ox+e[0]), y+jt.Template.Size[1], max(oz, oz+e[2]))
	mid := func(a0, a1 int32) int32 { return a0 + (a1-a0+1)/2 }
	surface := s.height(mid(bb.X0, bb.X1), mid(bb.Z0, bb.Z1), !ocean)
	y = surface - 1
	switch {
	case w.Nether:
		y = w.MinY + 15 + r.IntN(85)
	case !under:
		if desert {
			y = surface - (bb.Y1 - bb.Y0) + r.IntN(6)
		}
	default:
		bx, by, bz := mid(bb.X0, bb.X1), mid(bb.Y0, bb.Y1), mid(bb.Z0, bb.Z1)
		low := w.MinY + 15
		if s.biomeHas(bx, by, bz, "extreme_hills") || s.biomeHas(bx, by, bz, "mountain") {
			low = w.Sea + 7
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
		if s.leaves(b) {
			return false
		}
		return (!ocean && l.IsWater(b)) || w.Solid(b)
	}
	for bb.Y0 > w.MinY+15 {
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

	ids := l.portalIDs()
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
			d := spread + mathx.Abs(mx-x) + mathx.Abs(dz)
			if d > 13 || float64(portalSpread[d]) <= r.Float64() {
				continue
			}
			z := mz + dz
			hy := s.height(x, z, !ocean) - 1
			if buried {
				hy = min(hy, bb.Y0)
			}
			if mathx.Abs(bb.Y0-hy) <= 3 {
				netherrack(x, hy, z)
				drip(x, hy-1, z)
			}
		}
	}

	keepAir := under
	switch {
	case w.Nether && bb.Y0 < w.Sea:
		keepAir = false
	case !under && !desert:
		keepAir = r.Float() < 0.5
	}
	wet := false
	for _, c := range [8][3]int32{
		{bb.X0, bb.Y1, bb.Z0}, {bb.X0, bb.Y1, bb.Z1}, {bb.X1, bb.Y1, bb.Z0}, {bb.X1, bb.Y1, bb.Z1},
		{bb.X0, bb.Y0, bb.Z0}, {bb.X0, bb.Y0, bb.Z1}, {bb.X1, bb.Y0, bb.Z0}, {bb.X1, bb.Y0, bb.Z1},
	} {
		if s.get(c[0], c[1], c[2]) == l.water {
			wet = true
			break
		}
	}
	l.portalTemplate(s, &r, jt, ox, bb.Y0, oz, rot, mirror, keepAir, wet)

	for x := bb.X0 + 1; x < bb.X1; x++ {
		for z := bb.Z0 + 1; z < bb.Z1; z++ {
			if s.get(x, bb.Y0, z) == ids.netherrack {
				drip(x, bb.Y0-1, z)
			}
		}
	}

	moss := float32(0.8)
	if w.Nether {
		moss = 0
	} else if !wet {
		switch {
		case desert:
			moss = 0
		case startHas("jungle"):
			moss = 0.8
		case startHas("swamp"):
			moss = 0.5
		default:
			moss = 0.2
		}
	}
	l.agePortal(s, &r, bb, ids, moss, ocean, cold)

	p := &RuinedPortal{Path: path, Rot: rot, Mirror: mirror, Under: under, Box: bb, Blocks: map[[2]int32][]PortalBlock{}}
	for _, b := range s.set {
		k := [2]int32{b.X >> 4, b.Z >> 4}
		p.Blocks[k] = append(p.Blocks[k], b)
	}
	return p
}

// portalTemplate places the template at the origin, mirrored along z then
// turned. Air is skipped unless keepAir; wet waterlogs below sea level.
// Like every legacy template placement, a chest without a loot seed draws
// one from r.
func (l *Lib) portalTemplate(s *portalSim, r *rand.MT, jt *JigsawTemplate, ox, oy, oz int32, rot int, mirror, keepAir, wet bool) {
	m := MirrorNone
	if mirror {
		m = MirrorZ
	}
	conv := l.TemplateStatesMirror(jt, rot, m)
	for _, tb := range jt.Template.Blocks {
		st := jt.Template.Palette[tb.State]
		switch st.Name {
		case "minecraft:structure_block", "minecraft:jigsaw", "minecraft:structure_void":
			continue
		}
		cs := conv[tb.State]
		if !cs.OK || (!keepAir && cs.ID == l.air) {
			continue
		}
		p := tb.Pos
		if mirror {
			p[2] = -p[2]
		}
		p = RotatePos(p, rot)
		x, y, z := ox+p[0], oy+p[1], oz+p[2]
		id, name := cs.ID, l.BlockName(cs.ID)
		below := wet && y < s.w.Sea
		if id == l.air && below {
			id, name = l.water, "minecraft:water"
		}
		water := l.holdsLiquid[name] && (below || cs.Water)
		be := l.BlockEntityFor(id, st.Name, x, y, z)
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

func (l *Lib) portalIDs() portalIDs {
	id := func(name string) uint32 {
		s, _ := l.reg.Default("minecraft:" + name)
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
func (l *Lib) agePortal(s *portalSim, r *rand.MT, bb Box, ids portalIDs, moss float32, ocean, cold bool) {
	// stairs draws a facing and a half (sub_4E8A9F0).
	stairs := func(name string) uint32 {
		d := r.IntN(4)
		up := r.Float() < 0.5
		return l.state(name, map[string]any{"weirdo_direction": d, "upside_down_bit": up})
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
				name := l.BlockName(b)
				set := func(id uint32) { s.put(x, y, z, id, false, nil) }
				switch {
				case s.w.Nether && portalFullStone[name]:
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
					st, _ := l.reg.State(b)
					f := r.Float()
					switch {
					case s.w.Nether && f < 0.5:
						set(l.state("minecraft:polished_blackstone_brick_stairs", map[string]any{
							"weirdo_direction": st.Props["weirdo_direction"], "upside_down_bit": st.Props["upside_down_bit"]}))
					case s.w.Nether:
						set(ids.blackSlab)
					case f >= 0.5:
						ms := l.state("minecraft:mossy_stone_brick_stairs", map[string]any{
							"weirdo_direction": st.Props["weirdo_direction"], "upside_down_bit": st.Props["upside_down_bit"]})
						set(pick([2]uint32{ids.stoneSlab, ids.brickSlab}, [2]uint32{ms, ids.slabMossy}))
					}
				case portalSlabs[name]:
					switch {
					case s.w.Nether:
						set(ids.blackSlab)
					case moss > r.Float():
						set(ids.slabMossy)
					}
				case portalWalls[name]:
					switch {
					case s.w.Nether:
						set(ids.blackWall)
					case moss > r.Float():
						set(ids.wallMossy)
					}
				case name == "minecraft:iron_bars":
					if s.w.Nether {
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
						id := l.air
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
