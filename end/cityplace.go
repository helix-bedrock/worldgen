package end

import (
	"slices"

	"github.com/helix-bedrock/worldgen/internal/mathx"

	"github.com/helix-bedrock/worldgen/rand"
	"github.com/helix-bedrock/worldgen/structure"
)

// cityReach is how many chunks from its start chunk a city's pieces are
// looked for.
// VERIFY: BDS's own limit was not read; the farthest piece of the cities
// compared stays well inside it.
const cityReach = 12

// cityAt returns the city starting in chunk (cx, cz), nil if none does. It
// is laid out once per start.
func (e *End) cityAt(cx, cz int32) *city {
	k := [2]int32{cx, cz}
	if v, ok := e.cities.Load(k); ok {
		c, _ := v.(*city)
		return c
	}
	var c *city
	if rot, y, ok := e.isCityChunk(cx, cz); ok {
		c = e.layCity(cx, cz, rot, y)
	}
	v, _ := e.cities.LoadOrStore(k, c)
	c, _ = v.(*city)
	return c
}

// placeCities writes the pieces of the cities that reach chunk (cx, cz)
// into c, each city's pieces in order.
// VERIFY: the chest loot seeds draw from the chunk's population random,
// shared by every piece in the chunk, as in the woodland mansion.
func (e *End) placeCities(cx, cz int32, c *Chunk) {
	box := structure.NewBox(cx*16, 0, cz*16, cx*16+15, Height-1, cz*16+15)
	var r *rand.MT
	for rx := structure.Region(cx-cityReach, citySpacing); rx <= structure.Region(cx+cityReach, citySpacing); rx++ {
		for rz := structure.Region(cz-cityReach, citySpacing); rz <= structure.Region(cz+cityReach, citySpacing); rz++ {
			sx, sz := e.cityPick(rx, rz)
			if mathx.Abs(sx-cx) > cityReach || mathx.Abs(sz-cz) > cityReach {
				continue
			}
			ct := e.cityAt(sx, sz)
			if ct == nil || !ct.box.Intersects(box) {
				continue
			}
			if r == nil {
				r = rand.NewMT(populationSeed(e.seed, cx, cz))
			}
			for _, p := range ct.pieces {
				if p.box.Intersects(box) {
					e.placeCityPiece(c, box, p, r)
				}
			}
		}
	}
}

// cityMarker is a data structure block of a template: its world position
// and name.
type cityMarker struct {
	pos  [3]int32
	name string
}

// elytraFacing is the item frame's facing_direction by the piece's turn
// (dword_15411B4): south, west, north, east.
var elytraFacing = [4]int32{3, 4, 2, 5}

// placeCityPiece places the part of one piece inside box, then handles its
// markers (sub_4E492D0): a chest below a Chest marker gets the End city
// loot table, an Elytra marker becomes an item frame holding an elytra.
// Sentry markers spawn shulkers, which worldgen does not place.
func (e *End) placeCityPiece(c *Chunk, box structure.Box, p cityPiece, r *rand.MT) {
	jt := e.st.Template("endcity/" + p.name)
	if jt == nil {
		return
	}
	conv := e.st.TemplateStates(jt, p.rot)
	var markers []cityMarker
	for _, tb := range jt.Template.Blocks {
		st := jt.Template.Palette[tb.State]
		rp := structure.RotatePos(tb.Pos, p.rot)
		pos := [3]int32{p.pos[0] + rp[0], p.pos[1] + rp[1], p.pos[2] + rp[2]}
		switch st.Name {
		case "minecraft:structure_block":
			if m, _ := tb.NBT["metadata"].(string); m != "" {
				markers = append(markers, cityMarker{pos, m})
			}
			continue
		case "minecraft:jigsaw", "minecraft:structure_void":
			continue
		}
		if st.Name == "minecraft:skull" {
			// The 1.12 templates' skull is a head of its block entity's type.
			if box.Contains(pos) {
				e.placeSkull(c, pos, st, tb.NBT, p.rot)
			}
			continue
		}
		cs := conv[tb.State]
		if !cs.OK || (!p.overwrite && cs.ID == e.ids.air) || !box.Contains(pos) {
			continue
		}
		be := e.st.BlockEntityFor(cs.ID, st.Name, pos[0], pos[1], pos[2])
		switch be["id"] {
		case "Chest":
			// Like every legacy template placement, a chest without a loot
			// seed draws one.
			be["LootTableSeed"] = int32(r.Uint32() >> 1)
		case "Banner":
			for _, k := range []string{"Base", "Patterns"} {
				if v, ok := tb.NBT[k]; ok {
					be[k] = v
				}
			}
			if _, ok := be["Type"]; !ok {
				be["Type"] = int32(0)
			}
		case "BrewingStand":
			be["Items"] = brewingItems(tb.NBT["Items"])
			be["CookTime"], be["FuelAmount"], be["FuelTotal"] = int16(0), int16(0), int16(0)
		}
		e.setStructureBlock(c, pos, cs.ID, be)
	}
	for _, m := range markers {
		switch m.name {
		case "Chest":
			below := [3]int32{m.pos[0], m.pos[1] - 1, m.pos[2]}
			if !box.Contains(below) {
				continue
			}
			for i := range c.BlockEntities {
				be := &c.BlockEntities[i]
				if be.X != below[0] || be.Y != below[1] || be.Z != below[2] || be.Data["id"] != "Chest" {
					continue
				}
				be.LootTable = "loot_tables/chests/end_city_treasure.json"
				be.Data["LootTable"] = be.LootTable
				be.Data["LootTableSeed"] = int32(r.Uint32() >> 1)
			}
		case "Elytra":
			if !box.Contains(m.pos) {
				continue
			}
			e.setStructureBlock(c, m.pos, e.deco.frames[elytraFacing[p.rot&3]], map[string]any{
				"id": "ItemFrame", "x": m.pos[0], "y": m.pos[1], "z": m.pos[2],
				"Item":           map[string]any{"Name": "minecraft:elytra", "Count": uint8(1), "Damage": int16(0), "WasPickedUp": uint8(0)},
				"ItemRotation":   float32(0),
				"ItemDropChance": float32(1),
			})
		}
	}
}

// skullTypes are the heads of the 1.12 templates' SkullType values.
var skullTypes = [7]string{"skeleton_skull", "wither_skeleton_skull", "zombie_head", "player_head", "creeper_head",
	"dragon_head", "piglin_head"}

// placeSkull places a 1.12 template skull: the head its SkullType names,
// on the wall its facing names, turned with the piece.
func (e *End) placeSkull(c *Chunk, pos [3]int32, st structure.TemplateState, nbt map[string]any, rot int) {
	t, _ := nbt["SkullType"].(uint8)
	if int(t) >= len(skullTypes) {
		return
	}
	name := "minecraft:" + skullTypes[t]
	f := structure.RotateFacing(st.Props["facing"], rot)
	dir := map[string]int32{"down": 0, "up": 1, "north": 2, "south": 3, "west": 4, "east": 5}[f]
	id, ok := e.reg.Lookup(name, map[string]any{"facing_direction": dir})
	if !ok {
		return
	}
	e.setStructureBlock(c, pos, id, map[string]any{
		"id": "Skull", "x": pos[0], "y": pos[1], "z": pos[2], "SkullType": t,
		"Rotation": float32(0), "DoingAnimation": uint8(0), "MouthTickCount": int32(0),
	})
}

// potionIDs are Bedrock's potion damage values by Java potion name.
var potionIDs = map[string]int16{"minecraft:strong_healing": 22}

// brewingItems converts a 1.12 template brewing stand's items: Java's
// bottle slots 0 to 2 are Bedrock's 1 to 3, and a potion's kind is its
// damage value.
func brewingItems(v any) []any {
	list, _ := v.([]any)
	out := make([]any, 0, len(list))
	for _, it := range list {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		slot, _ := m["Slot"].(uint8)
		count, _ := m["Count"].(uint8)
		name, _ := m["id"].(string)
		item := map[string]any{"Name": name, "Count": count, "Slot": slot + 1, "Damage": int16(0), "WasPickedUp": uint8(0)}
		if tag, ok := m["tag"].(map[string]any); ok {
			item["tag"] = tag
			if p, _ := tag["Potion"].(string); p != "" {
				item["Damage"] = potionIDs[p]
			}
		}
		out = append(out, item)
	}
	return out
}

// setStructureBlock places a structure's block in c, replacing the block
// entity there with be (none if nil).
func (e *End) setStructureBlock(c *Chunk, pos [3]int32, id uint32, be map[string]any) {
	c.set(int(pos[0]&15), int(pos[1]), int(pos[2]&15), id)
	c.BlockEntities = slices.DeleteFunc(c.BlockEntities, func(b structure.BlockEntity) bool {
		return b.X == pos[0] && b.Y == pos[1] && b.Z == pos[2]
	})
	if be != nil {
		c.BlockEntities = append(c.BlockEntities, structure.BlockEntity{X: pos[0], Y: pos[1], Z: pos[2], Data: be})
	}
}

// CityPiece is one template of an End city, for inspection.
type CityPiece struct {
	// Name is the template, under endcity/.
	Name string
	// Pos is where the template's origin is, Rot its quarter turns.
	Pos [3]int32
	Rot int
	// Box is the template's box, inclusive: x0, y0, z0, x1, y1, z1.
	Box [6]int32
}

// CityPieces lists the pieces of the city starting in chunk (cx, cz), in
// placement order; nil if no city starts there. It is safe for concurrent
// use.
func (e *End) CityPieces(cx, cz int32) []CityPiece {
	ct := e.cityAt(cx, cz)
	if ct == nil {
		return nil
	}
	out := make([]CityPiece, len(ct.pieces))
	for i, p := range ct.pieces {
		b := p.box
		out[i] = CityPiece{Name: p.name, Pos: p.pos, Rot: p.rot, Box: [6]int32{b.X0, b.Y0, b.Z0, b.X1, b.Y1, b.Z1}}
	}
	return out
}
