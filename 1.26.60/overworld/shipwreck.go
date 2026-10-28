package overworld

import (
	"slices"
	"strings"

	"github.com/helix-bedrock/worldgen/1.26.60/rand"
	"github.com/helix-bedrock/worldgen/1.26.60/structure"
)

// Shipwrecks, written from BDS 1.26.52: ShipwreckFeature (constructor
// sub_4E9C480, chunk test sub_4E9D1F0, start sub_4E9D4A0) and ShipwreckPiece
// (postProcess sub_4E9D850). The start chunk builds the whole ship into its
// neighbours as well; here every chunk the ship reaches lays out the same
// ship from the start chunk's random and writes its own part.
//
// Bedrock differs from Java here, and the differences are kept:
//   - the biome all around the start (10 blocks on beaches, 20 elsewhere)
//     must be the start's own biome, and no ship starts within 5 chunks of
//     an ocean monument; deep warm oceans have none;
//   - each ship draws a pair of woods: its oak and dark oak stairs, doors,
//     trapdoors and stripped logs turn into them (qword_1523D20);
//   - the ship sits at the average floor height over the middle half of
//     its footprint, 2 lower (4 for upside-down ships); a beached ship is
//     never below sea level;
//   - a supply or treasure chest is kept only half the time (nextInt(10)
//     >= 5); the map chest always is.
//
// VERIFY: the wood families (BDS checks block types by list: the stairs,
// doors and trapdoors read cleanly; the fourth list is taken as stripped
// logs). Planks, slabs and fences keep their wood.
// VERIFY: markers handled in template order.

// Shipwreck placement (sub_4E9D1F0): linear spread. Worlds with the legacy
// flag use 10 and 5.
const (
	shipSalt       = 165745295
	shipSpacing    = 24
	shipSeparation = 4
)

// shipBiomes are the biomes a shipwreck starts in (sub_4E9C480).
func shipBiomes() []string {
	return []string{"beach", "cold_beach", "mushroom_island_shore", "ocean", "deep_ocean", "cold_ocean",
		"deep_cold_ocean", "lukewarm_ocean", "deep_lukewarm_ocean", "frozen_ocean", "deep_frozen_ocean", "warm_ocean"}
}

// shipBeaches are the biomes that make a beached ship (mask 0x4018000).
func shipBeaches() []string { return []string{"beach", "cold_beach", "mushroom_island_shore"} }

// shipTemplates in BDS's order (sub_4E9B6C0); beached ships draw from the
// first shipUprightTemplates, which leaves out the upside-down ones.
func shipTemplates() [20]string {
	return [...]string{"swwithmast", "swwithmastdegraded",
		"swsidewaysfull", "swsidewaysfronthalf", "swsidewaysbackhalf",
		"swsidewaysfulldegraded", "swsidewaysfronthalfdegraded", "swsidewaysbackhalfdegraded",
		"swrightsideupfull", "swrightsideupfronthalf", "swrightsideupbackhalf",
		"swrightsideupfulldegraded", "swrightsideupfronthalfdegraded", "swrightsideupbackhalfdegraded",
		"swupsidedownfull", "swupsidedownfronthalf", "swupsidedownbackhalf",
		"swupsidedownfulldegraded", "swupsidedownfronthalfdegraded", "swupsidedownbackhalfdegraded"}
}

// Bedrock's legacy wood types by index.
const (
	woodOak     = 0
	woodSpruce  = 1
	woodBirch   = 2
	woodJungle  = 3
	woodAcacia  = 4
	woodDarkOak = 5
)

// woodNames are Bedrock's legacy wood types by index.
func woodNames() [6]string {
	return [...]string{woodOak: "oak", woodSpruce: "spruce", woodBirch: "birch", woodJungle: "jungle",
		woodAcacia: "acacia", woodDarkOak: "dark_oak"}
}

// shipWoods are the wood pairs a ship draws from: what its oak and its dark
// oak become (qword_1523D20).
func shipWoods() [9][2]uint8 {
	return [...][2]uint8{{woodOak, woodSpruce}, {woodJungle, woodSpruce}, {woodDarkOak, woodJungle},
		{woodDarkOak, woodSpruce}, {woodSpruce, woodOak}, {woodSpruce, woodJungle}, {woodSpruce, woodDarkOak},
		{woodOak, woodBirch}, {woodSpruce, woodAcacia}}
}

// Shipwreck layout (sub_4E9D1F0, sub_4E9D4A0, sub_4E9D850).
const (
	// shipBeachReach and shipOceanReach are the radius, in blocks, of the
	// same-biome test around a beached and an ocean start.
	shipBeachReach = 10
	shipOceanReach = 20
	// shipMonumentGap is how many chunks around a start must hold no
	// monument start (from -shipMonumentGap to shipMonumentGap-1).
	shipMonumentGap = 5
	// shipOceanBoxDrop is how far below sea level the start puts an ocean
	// ship's piece box.
	shipOceanBoxDrop = 33
	// shipUprightTemplates is how many templates come before the
	// upside-down ones.
	shipUprightTemplates = 14
	// shipUprightDrop and shipUpsideDownDrop are how far the ship sits
	// below its footprint's average floor.
	shipUprightDrop    = 2
	shipUpsideDownDrop = 4
	// shipMidQuarters is how many quarters of the footprint the middle
	// part ends at (it starts at one quarter).
	shipMidQuarters = 3
	// shipChestRolls and shipChestKeep: a supply or treasure chest stays
	// when nextInt(shipChestRolls) is at least shipChestKeep.
	shipChestRolls = 10
	shipChestKeep  = 5
	// shipReachChunks is how far, in chunks, a ship reaches from its start.
	shipReachChunks = 2
)

// shipwreck is a laid-out ship.
type shipwreck struct {
	path    string
	x, z    int32
	rot     int
	beached bool
	woods   [2]uint8
	// keep says, per chest marker in template order, whether its chest
	// stays; seeds are the kept chests' loot seeds.
	keep  []bool
	seeds []int32
	// area is the ship's footprint (all heights); mid is the part of it
	// whose floor sets the height, and deep the extra drop.
	area structure.Box
	mid  structure.Box
	deep int32
}

// shipPick is the chunk a shipwreck region picks.
func (o *Overworld) shipPick(rx, rz int32) (int32, int32) {
	if x, z, ok := o.forcedPick(skShipwreck, rx, rz); ok {
		return x, z
	}
	var r rand.MT
	structureSeed(&r, o.seed, rx, rz, o.place[skShipwreck].salt)
	n := o.place[skShipwreck].spacing - o.place[skShipwreck].separation
	x := rx*o.place[skShipwreck].spacing + r.IntN(n)
	return x, rz*o.place[skShipwreck].spacing + r.IntN(n)
}

// isShipChunk reports whether a shipwreck starts in chunk (cx, cz).
func (o *Overworld) isShipChunk(cx, cz int32) bool {
	x, z := o.shipPick(structure.Region(cx, o.place[skShipwreck].spacing), structure.Region(cz, o.place[skShipwreck].spacing))
	if x != cx || z != cz {
		return false
	}
	if o.isForced(skShipwreck, cx, cz) {
		return true
	}
	if !o.structOK(skShipwreck, cx, cz) {
		return false
	}
	y := o.structureY(cx, cz, noSurfaceY)
	b := o.BiomeAt(cx*16+8, y, cz*16+8)
	if int(b) >= len(o.shipBiome) || !o.shipBiome[b] {
		return false
	}
	r := int32(shipOceanReach)
	if o.shipBeach[b] {
		r = shipBeachReach
	}
	same := make([]bool, len(o.shipBiome))
	same[b] = true
	if !o.biomesOnly(cx*16+8, y, cz*16+8, r, same) {
		return false
	}
	return !o.monumentNear(cx-shipMonumentGap, cz-shipMonumentGap, cx+shipMonumentGap-1, cz+shipMonumentGap-1)
}

// shipAt lays out the shipwreck starting in chunk (sx, sz), nil if none
// does, from the start chunk's random (sub_4E9D850). It is computed once
// per start; the height is not part of it (shipY).
func (o *Overworld) shipAt(sx, sz int32) *shipwreck {
	k := [2]int32{sx, sz}
	if v, ok := o.ships.Load(k); ok {
		s, _ := v.(*shipwreck)
		return s
	}
	s := o.layShip(sx, sz)
	o.ships.Store(k, s)
	return s
}

func (o *Overworld) layShip(sx, sz int32) *shipwreck {
	if !o.isShipChunk(sx, sz) {
		return nil
	}
	x0, z0 := sx*16, sz*16
	// The start puts the piece box at sea level - 33 (sea level - 1 when
	// the middle is a beach); the piece reads the biome at its corner one
	// above that.
	by := int32(SeaLevel - shipOceanBoxDrop)
	if b := o.BiomeAt(x0+8, SeaLevel, z0+8); int(b) < len(o.shipBeach) && o.shipBeach[b] {
		by = SeaLevel - 1
	}
	b := o.BiomeAt(x0, by+1, z0)
	s := &shipwreck{beached: int(b) < len(o.shipBeach) && o.shipBeach[b]}
	var r rand.MT
	r.Seed(populationSeed(o.seed, sx, sz))
	s.rot = int(r.IntN(structRotations))
	templates, woods := shipTemplates(), shipWoods()
	idx := r.IntN(int32(len(templates)))
	if s.beached {
		idx = r.IntN(shipUprightTemplates)
	}
	s.path = "shipwreck/" + templates[idx]
	s.woods = woods[r.IntN(int32(len(woods)))]
	jt := o.st.Template(s.path)
	if jt == nil {
		return nil
	}
	// The middle half of the rotated footprint, measured from the corner
	// the template turns about (C division, as BDS's).
	w, d := jt.Template.Size[0], jt.Template.Size[2]
	if s.rot == rot90 || s.rot == rot270 {
		w, d = d, w
	}
	var ox, oz int32
	xa, xb := w/4, shipMidQuarters*w/4
	za, zb := d/4, shipMidQuarters*d/4
	if s.rot == rot90 || s.rot == rot180 {
		ox, xa, xb = chunkLastBlock, -shipMidQuarters*w/4, w/-4
	}
	if s.rot == rot180 || s.rot == rot270 {
		oz, za, zb = chunkLastBlock, -shipMidQuarters*d/4, d/-4
	}
	s.x, s.z = x0+ox, z0+oz
	s.mid = structure.NewBox(s.x+min(xa, xb), 0, s.z+min(za, zb), s.x+max(xa, xb), 0, s.z+max(za, zb))
	s.deep = shipUprightDrop
	if idx >= shipUprightTemplates {
		s.deep = shipUpsideDownDrop
	}
	e := structure.RotatePos([3]int32{jt.Template.Size[0] - 1, 0, jt.Template.Size[2] - 1}, s.rot)
	s.area = structure.NewBox(min(s.x, s.x+e[0]), MinY, min(s.z, s.z+e[2]), max(s.x, s.x+e[0]), MaxY-1, max(s.z, s.z+e[2]))
	// The chest rolls and loot seeds follow the placement.
	for _, tb := range jt.Template.Blocks {
		m, _ := tb.NBT["metadata"].(string)
		switch m {
		case "treasureChest", "supplyChest":
			keep := r.IntN(shipChestRolls) >= shipChestKeep
			s.keep = append(s.keep, keep)
			if keep {
				s.seeds = append(s.seeds, int32(r.Uint32()>>1))
			}
		case "mapChest":
			s.keep = append(s.keep, true)
			s.seeds = append(s.seeds, int32(r.Uint32()>>1))
		}
	}
	return s
}

// shipY is a ship's bottom: the average top solid block (through water)
// over the middle of its footprint, less 2 (4 upside down); a beached ship
// stays at sea level or above.
func (o *Overworld) shipY(s *shipwreck, raw func(cx, cz int32) *PackedChunk) int32 {
	var sum, n int32
	for x := s.mid.X0; x <= s.mid.X1; x++ {
		for z := s.mid.Z0; z <= s.mid.Z1; z++ {
			sum += o.topSolid(raw(x>>4, z>>4), x, z, false)
			n++
		}
	}
	y := sum/n - s.deep
	if s.beached && y < SeaLevel {
		y = SeaLevel
	}
	return y
}

// shipsReach calls f for each shipwreck with a block in chunk (cx, cz).
// Ships reach at most 2 chunks from their start.
func (o *Overworld) shipsReach(cx, cz int32, f func(s *shipwreck)) {
	chunk := structure.NewBox(cx*16, MinY, cz*16, cx*16+15, MaxY-1, cz*16+15)
	for rx := structure.Region(cx-shipReachChunks, o.place[skShipwreck].spacing); rx <= structure.Region(cx+shipReachChunks, o.place[skShipwreck].spacing); rx++ {
		for rz := structure.Region(cz-shipReachChunks, o.place[skShipwreck].spacing); rz <= structure.Region(cz+shipReachChunks, o.place[skShipwreck].spacing); rz++ {
			sx, sz := o.shipPick(rx, rz)
			if sx < cx-shipReachChunks || sx > cx+shipReachChunks || sz < cz-shipReachChunks || sz > cz+shipReachChunks {
				continue
			}
			if s := o.shipAt(sx, sz); s != nil && s.area.Intersects(chunk) {
				f(s)
			}
		}
	}
}

// shipReaches reports whether a shipwreck has a block in chunk (cx, cz).
func (o *Overworld) shipReaches(cx, cz int32) bool {
	found := false
	o.shipsReach(cx, cz, func(*shipwreck) { found = true })
	return found
}

// placeShips writes the shipwrecks that reach chunk (cx, cz) into c. raw
// gives undecorated terrain.
func (o *Overworld) placeShips(cx, cz int32, c *Chunk, raw func(cx, cz int32) *PackedChunk) {
	o.shipsReach(cx, cz, func(s *shipwreck) {
		b := &scBuild{o: o, c: c, cbox: structure.NewBox(cx*16, MinY, cz*16, cx*16+15, MaxY-1, cz*16+15)}
		b.ids()
		b.ship(s, o.shipY(s, raw))
	})
}

// shipMarker is a structure block data marker of a shipwreck template.
type shipMarker struct {
	pos  [3]int32
	name string
}

// ship places a shipwreck's template (sub_D2B7A10: integrity 1, below sea
// level air turns to water and liquid holders are waterlogged unless the
// ship is beached), then its chest markers.
func (b *scBuild) ship(s *shipwreck, y int32) {
	o := b.o
	jt := o.st.Template(s.path)
	conv := o.st.TemplateStates(jt, s.rot)
	var markers []shipMarker
	for _, tb := range jt.Template.Blocks {
		st := jt.Template.Palette[tb.State]
		rp := structure.RotatePos(tb.Pos, s.rot)
		pos := [3]int32{s.x + rp[0], y + rp[1], s.z + rp[2]}
		switch st.Name {
		case blockStructureBlock:
			if m, _ := tb.NBT["metadata"].(string); m == "treasureChest" || m == "supplyChest" || m == "mapChest" {
				markers = append(markers, shipMarker{pos, m})
			}
			continue
		case blockJigsaw, blockStructureVoid:
			continue
		}
		cs := conv[tb.State]
		if !cs.OK || !b.cbox.Contains(pos) {
			continue
		}
		id := o.shipWood(cs.ID, s.woods)
		name := o.blockName(id)
		below := !s.beached && pos[1] < SeaLevel
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
	seed := 0
	for i, m := range markers {
		keep := i < len(s.keep) && s.keep[i]
		var ls int32
		if keep && seed < len(s.seeds) {
			ls = s.seeds[seed]
			seed++
		}
		b.shipChest(m.pos, m.name, keep, ls)
	}
}

// shipChest handles a chest marker: the marker turns to water below sea
// level and air above; the chest under it gets its loot, or is removed when
// its roll failed.
func (b *scBuild) shipChest(pos [3]int32, name string, keep bool, seed int32) {
	o := b.o
	fill := o.ids.air
	if pos[1] < SeaLevel {
		fill = o.ids.water
	}
	if b.cbox.Contains(pos) && b.setWorld(pos[0], pos[1], pos[2], fill) {
		b.dropEntity(pos)
	}
	cp := [3]int32{pos[0], pos[1] - 1, pos[2]}
	if !b.cbox.Contains(cp) {
		return
	}
	if !keep {
		fill = o.ids.air
		if pos[1] <= SeaLevel {
			fill = o.ids.water
		}
		if b.setWorld(cp[0], cp[1], cp[2], fill) {
			b.dropEntity(cp)
		}
		return
	}
	table := map[string]string{"treasureChest": "shipwrecktreasure", "supplyChest": "shipwrecksupply",
		"mapChest": "shipwreck"}[name]
	for i := range b.c.BlockEntities {
		be := &b.c.BlockEntities[i]
		if be.X == cp[0] && be.Y == cp[1] && be.Z == cp[2] && o.blockName(b.c.Block(int(cp[0]&15), int(cp[1]), int(cp[2]&15))) == blockChest {
			be.LootTable = "loot_tables/chests/" + table + ".json"
			be.Data["LootTable"] = be.LootTable
			be.Data["LootTableSeed"] = seed
		}
	}
}

// shipWood turns an oak or dark oak stairs, door, trapdoor or stripped log
// into the ship's woods, keeping its states.
func (o *Overworld) shipWood(id uint32, woods [2]uint8) uint32 {
	name := strings.TrimPrefix(o.blockName(id), "minecraft:")
	family := ""
	switch name {
	case "oak_stairs", "dark_oak_stairs":
		family = "stairs"
	case "wooden_door", "dark_oak_door":
		family = "door"
	case "trapdoor", "dark_oak_trapdoor":
		family = "trapdoor"
	case "stripped_oak_log", "stripped_dark_oak_log":
		family = "stripped"
	default:
		return id
	}
	wood := int(woods[0])
	if strings.Contains(name, "dark_oak") {
		wood = int(woods[1])
	}
	w := woodNames()[wood]
	var to string
	switch family {
	case "stairs":
		to = w + "_stairs"
	case "door":
		to = w + "_door"
		if wood == woodOak {
			to = "wooden_door"
		}
	case "trapdoor":
		to = w + "_trapdoor"
		if wood == woodOak {
			to = "trapdoor"
		}
	case "stripped":
		to = "stripped_" + w + "_log"
	}
	st, ok := o.reg.State(id)
	if !ok {
		return id
	}
	props := make(map[string]any, len(st.Props))
	for k, v := range st.Props {
		props[k] = v
	}
	if nid, ok := o.reg.Lookup("minecraft:"+to, props); ok {
		return nid
	}
	return id
}

// NearestShipwreck finds the shipwreck nearest to block (x, z), searching
// maxRegions placement regions around it. Kind is "shipwreck" or
// "shipwreck_beached"; X, Y, Z are the template corner. It is safe for
// concurrent use.
func (o *Overworld) NearestShipwreck(x, z int32, maxRegions int32) (Scattered, bool) {
	var cands []locateCand
	rx0, rz0 := structure.Region(x>>4, o.place[skShipwreck].spacing), structure.Region(z>>4, o.place[skShipwreck].spacing)
	for rx := rx0 - maxRegions; rx <= rx0+maxRegions; rx++ {
		for rz := rz0 - maxRegions; rz <= rz0+maxRegions; rz++ {
			cx, cz := o.shipPick(rx, rz)
			dx, dz := int64(cx*16+8-x), int64(cz*16+8-z)
			cands = append(cands, locateCand{cx, cz, dx*dx + dz*dz})
		}
	}
	slices.SortFunc(cands, compareLocateCand)
	raw := newRawChunks(o)
	for _, c := range cands {
		s := o.shipAt(c.cx, c.cz)
		if s == nil {
			continue
		}
		kind := "shipwreck"
		if s.beached {
			kind = "shipwreck_beached"
		}
		return Scattered{Kind: kind, X: s.x, Y: o.shipY(s, raw.get), Z: s.z}, true
	}
	return Scattered{}, false
}
