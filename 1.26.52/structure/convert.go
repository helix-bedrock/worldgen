package structure

import (
	"strconv"
	"strings"
)

// Java to Bedrock block conversion for structure templates, as BDS does it
// when it loads a Java .nbt template (sub_D2B9E40 in 1.26.52).
//
// Names: sub_D279E60 strips "minecraft:" and maps the special cases in
// javaBlockNames (exact) and javaBlockContains (substring, in order); every
// other name is looked up as is, and an unknown one places nothing.
// Properties: sub_D276010 applies each Java property to the Bedrock block.
// VERIFY: the property rules here are written per Bedrock state name and
// checked against BDS villages with bdsdiff, not ported line by line from
// sub_D276010.

// javaBlockName is the Bedrock name of a Java block with a special case.
func javaBlockName(name string) (string, bool) {
	switch name {
	case "torch":
		return "torch", true
	case "wall_torch":
		return "torch", true
	case "bricks":
		return "brick_block", true
	case "terracotta":
		return "hardened_clay", true
	case "oak_fence_gate":
		return "fence_gate", true
	case "light_gray_glazed_terracotta":
		return "silver_glazed_terracotta", true
	case "melon":
		return "melon_block", true
	case "oak_door":
		return "wooden_door", true
	case "dirt_path":
		return "grass_path", true
	case "chain":
		return "iron_chain", true
	case "tripwire":
		return "trip_wire", true
	case "magma_block":
		return "magma", true
	case "attached_melon_stem":
		return "melon_stem", true
	case "oak_pressure_plate":
		return "wooden_pressure_plate", true
	case "oak_trapdoor":
		return "trapdoor", true
	case "cobblestone_stairs":
		return "stone_stairs", true
	case "skeleton_skull":
		return "skeleton_skull", true
	case "grass":
		return "short_grass", true
	case "dead_bush":
		return "deadbush", true
	case "snow_block":
		return "snow", true
	case "snow":
		return "snow_layer", true
	case "stonecutter":
		return "stonecutter_block", true
	case "oak_sign":
		return "standing_sign", true
	case "blackstone_bricks":
		return "polished_blackstone_bricks", true
	case "spawner":
		return "mob_spawner", true
	case "note_block":
		return "noteblock", true
	case "repeater":
		return "unpowered_repeater", true
	case "comparator":
		return "unpowered_comparator", true
	case "redstone_torch":
		return "unlit_redstone_torch", true
	case "redstone_wall_torch":
		return "unlit_redstone_torch", true
	case "oak_button":
		return "wooden_button", true
	case "cobweb":
		return "web", true
	case "waxed_copper_block":
		return "waxed_copper", true
	case "cave_air":
		return "air", true
	case "piston_head":
		return "air", true
	case "water_cauldron":
		return "cauldron", true
	case "sugar_cane":
		return "reeds", true
	case "lily_pad":
		return "waterlily", true
	}
	return "", false
}

// javaBlockContains are checked after the exact names, in this order.
func javaBlockContains() [4][2]string {
	return [4][2]string{
		{"straw_bed", "straw_bed"},
		{"bed", "bed"},
		{"wall_banner", "wall_banner"},
		{"potted", "flower_pot"},
	}
}

// blockEntityID is the Bedrock block entity id of a block that has one, by
// Bedrock block name (suffix match for the families).
func blockEntityID(name string) (string, bool) {
	switch name {
	case "minecraft:bed":
		return "Bed", true
	case "minecraft:bell":
		return "Bell", true
	case "minecraft:chest", "minecraft:trapped_chest":
		return "Chest", true
	case "minecraft:barrel":
		return "Barrel", true
	case "minecraft:furnace", "minecraft:lit_furnace":
		return "Furnace", true
	case "minecraft:blast_furnace":
		return "BlastFurnace", true
	case "minecraft:smoker":
		return "Smoker", true
	case "minecraft:flower_pot":
		return "FlowerPot", true
	case "minecraft:lectern":
		return "Lectern", true
	case "minecraft:brewing_stand":
		return "BrewingStand", true
	case "minecraft:cauldron":
		return "Cauldron", true
	case "minecraft:jukebox":
		return "Jukebox", true
	case "minecraft:hopper":
		return "Hopper", true
	case "minecraft:dispenser":
		return "Dispenser", true
	case "minecraft:dropper":
		return "Dropper", true
	case "minecraft:campfire", "minecraft:soul_campfire":
		return "Campfire", true
	case "minecraft:beehive", "minecraft:bee_nest":
		return "Beehive", true
	case "minecraft:mob_spawner":
		return "MobSpawner", true
	case "minecraft:noteblock":
		return "Music", true
	case "minecraft:enchanting_table":
		return "EnchantTable", true
	case "minecraft:standing_sign", "minecraft:wall_sign":
		return "Sign", true
	case "minecraft:standing_banner", "minecraft:wall_banner":
		return "Banner", true
	case "minecraft:skeleton_skull":
		return "Skull", true
	case "minecraft:decorated_pot":
		return "DecoratedPot", true
	case "minecraft:chiseled_bookshelf":
		return "ChiseledBookshelf", true
	case "minecraft:unpowered_comparator":
		return "Comparator", true
	case "minecraft:suspicious_sand", "minecraft:suspicious_gravel":
		return "BrushableBlock", true
	case "minecraft:trial_spawner":
		return "TrialSpawner", true
	case "minecraft:vault":
		return "Vault", true
	case "minecraft:ender_chest":
		return "EnderChest", true
	}
	return "", false
}

// bedColor is Bedrock's colour index for a bed block entity; an unknown
// colour is white.
func bedColor(c string) uint8 {
	colors := [...]string{"white", "orange", "magenta", "light_blue", "yellow", "lime", "pink", "gray",
		"light_gray", "cyan", "purple", "blue", "brown", "green", "red", "black"}
	for i, n := range colors {
		if n == c {
			return uint8(i)
		}
	}
	return 0
}

// BlockEntityFor returns the block entity data of a converted block placed
// at (x, y, z), nil if it has none. javaName gives the bed colour.
func (l *Lib) BlockEntityFor(id uint32, javaName string, x, y, z int32) map[string]any {
	name := l.BlockName(id)
	beID, ok := blockEntityID(name)
	if !ok {
		switch {
		case strings.HasSuffix(name, "_sign") && strings.Contains(name, "hanging"):
			beID = "HangingSign"
		case strings.HasSuffix(name, "_sign"):
			beID = "Sign"
		case strings.HasSuffix(name, "_banner"):
			beID = "Banner"
		case strings.HasSuffix(name, "_skull") || strings.HasSuffix(name, "_head"):
			beID = "Skull"
		case strings.HasSuffix(name, "shulker_box"):
			beID = "ShulkerBox"
		default:
			return nil
		}
	}
	d := map[string]any{"id": beID, "x": x, "y": y, "z": z, "isMovable": uint8(1)}
	if beID == "Bed" {
		color := strings.TrimSuffix(strings.TrimPrefix(javaName, "minecraft:"), "_bed")
		d["color"] = bedColor(color)
	}
	// Outpost templates name the ominous banner illager_captain_wall_banner;
	// Bedrock keeps it as a banner of type 1 (illager_captain).
	// VERIFY: against a BDS outpost's banner block entity.
	if beID == "Banner" && strings.Contains(javaName, "illager_captain") {
		d["Type"] = int32(1)
	}
	return d
}

// javaFacing maps a Java facing to Bedrock's facing_direction.
func javaFacing(f string) (int32, bool) {
	return dirIndex([]string{"down", "up", "north", "south", "west", "east"}, f)
}

// dirIndex is the index of f in order, and whether it is there.
func dirIndex(order []string, f string) (int32, bool) {
	for i, n := range order {
		if n == f {
			return int32(i), true
		}
	}
	return 0, false
}

// Bedrock's horizontal "direction" encodings differ per block; an unknown
// facing is 0.

// dirSWNE is the encoding of beds, fence gates and repeaters.
func dirSWNE(f string) int32 {
	d, _ := dirIndex([]string{"south", "west", "north", "east"}, f)
	return d
}

// dirESWN is the encoding of doors.
func dirESWN(f string) int32 {
	d, _ := dirIndex([]string{"east", "south", "west", "north"}, f)
	return d
}

// dirEWSN is the encoding of stairs and trapdoors.
func dirEWSN(f string) int32 {
	d, _ := dirIndex([]string{"east", "west", "south", "north"}, f)
	return d
}

// leverDirection is Bedrock's lever_direction for a Java lever: on a wall
// the side it faces, on the floor ("up") or ceiling ("down") the axis it
// lies along. Older templates name floor and ceiling levers up_x, down_z...
func leverDirection(p map[string]string) string {
	f := p["facing"]
	switch f {
	case "up_x":
		return "up_east_west"
	case "up_z":
		return "up_north_south"
	case "down_x":
		return "down_east_west"
	case "down_z":
		return "down_north_south"
	}
	axis := "north_south"
	if f == "east" || f == "west" {
		axis = "east_west"
	}
	switch p["face"] {
	case "floor":
		return "up_" + axis
	case "ceiling":
		return "down_" + axis
	}
	return f
}

// withJavaProp returns a copy of p with one property set.
func withJavaProp(p map[string]string, k, v string) map[string]string {
	out := make(map[string]string, len(p)+1)
	for pk, pv := range p {
		out[pk] = pv
	}
	out[k] = v
	return out
}

// ConvertJavaState returns the registry index of the Bedrock block for a
// Java template state and whether it is waterlogged.
func (l *Lib) ConvertJavaState(st TemplateState) (uint32, bool, bool) {
	name := strings.TrimPrefix(st.Name, "minecraft:")
	// Older templates (the shipwrecks, the mansion) still use Java's
	// pre-1.13 names, with the wood, colour or stone in a property.
	if v := st.Props["variant"]; v != "" {
		switch name {
		case "planks":
			name = v + "_planks"
		case "wooden_slab":
			name = v + "_slab"
		case "double_wooden_slab":
			name = v + "_double_slab"
		case "log", "log2":
			name = v + "_log"
		case "leaves", "leaves2":
			name = v + "_leaves"
			if st.Props["decayable"] == "false" {
				st.Props = withJavaProp(st.Props, "persistent", "true")
			}
		case "stone_slab", "double_stone_slab":
			stone := map[string]string{"smoothstone": "smooth_stone", "stone": "smooth_stone", "cobblestone": "cobblestone",
				"sandstone": "sandstone", "brick": "brick", "stone_brick": "stone_brick", "nether_brick": "nether_brick",
				"quartz": "quartz", "wood_old": "petrified_oak"}[v]
			if stone != "" {
				name = stone + "_slab"
				if st.Name == "minecraft:double_stone_slab" {
					name = stone + "_double_slab"
				}
			}
		case "stone":
			// BDS keeps these and leaves other variants plain stone
			// (sub_D276010).
			switch v {
			case "granite", "diorite", "andesite":
				name = v
			case "smooth_andesite":
				name = "polished_andesite"
			}
		case "stonebrick":
			name = map[string]string{"stonebrick": "stone_bricks", "mossy_stonebrick": "mossy_stone_bricks",
				"cracked_stonebrick": "cracked_stone_bricks", "chiseled_stonebrick": "chiseled_stone_bricks"}[v]
		case "monster_egg":
			name = map[string]string{"stone": "infested_stone", "cobblestone": "infested_cobblestone",
				"stone_brick": "infested_stone_bricks", "mossy_brick": "infested_mossy_stone_bricks",
				"cracked_brick": "infested_cracked_stone_bricks", "chiseled_brick": "infested_chiseled_stone_bricks"}[v]
		}
	}
	if c := st.Props["color"]; c != "" && (name == "wool" || name == "carpet" || name == "stained_glass" ||
		name == "stained_glass_pane" || name == "stained_hardened_clay" || name == "concrete") {
		if c == "silver" {
			c = "light_gray"
		}
		if name == "stained_hardened_clay" {
			name = "terracotta"
		}
		name = c + "_" + name
	}
	if t := st.Props["type"]; name == "sapling" && t != "" {
		name = t + "_sapling"
	}
	if name == "fence" {
		name = "oak_fence"
	}
	target, ok := javaBlockName(name)
	if !ok {
		target = name
		for _, c := range javaBlockContains() {
			if strings.Contains(name, c[0]) {
				target = c[1]
				break
			}
		}
	}
	full := "minecraft:" + target
	def, ok := l.reg.Default(full)
	if !ok {
		return 0, false, false
	}
	ds, _ := l.reg.State(def)
	props := make(map[string]any, len(ds.Props))
	for k, v := range ds.Props {
		props[k] = v
	}
	water := false
	for k, v := range st.Props {
		switch k {
		case "waterlogged":
			water = v == "true"
		case "facing":
			if strings.HasSuffix(target, "_door") && !strings.Contains(target, "trapdoor") {
				// Doors: the legacy direction 0 was Java east and became
				// "south" (block upgrade schema 1.21.60), a quarter turn.
				setProp(props, "minecraft:cardinal_direction", RotateFacing(v, 1))
			} else {
				setProp(props, "minecraft:cardinal_direction", v)
			}
			setProp(props, "minecraft:facing_direction", v)
			if f, ok := javaFacing(v); ok {
				// A sideways end rod names the side it points away from.
				if target == "end_rod" && f >= 2 {
					f, _ = javaFacing(Opposite(v))
				}
				setProp(props, "facing_direction", f)
			}
			// A wall torch's Java facing points away from its wall; Bedrock
			// names the side it hangs on.
			setProp(props, "torch_facing_direction", Opposite(v))
			switch {
			case strings.HasSuffix(target, "_door"):
				setProp(props, "direction", dirESWN(v))
			case strings.HasSuffix(target, "stairs"):
				setProp(props, "weirdo_direction", dirEWSN(v))
			case strings.Contains(target, "trapdoor"):
				setProp(props, "direction", dirEWSN(v))
			default:
				setProp(props, "direction", dirSWNE(v))
			}
		case "half":
			top := v == "top" || v == "upper"
			setProp(props, "upside_down_bit", top)
			setProp(props, "upper_block_bit", top)
			if v == "top" || v == "bottom" {
				setProp(props, "minecraft:vertical_half", v)
			}
		case "type":
			if v == "top" || v == "bottom" {
				setProp(props, "minecraft:vertical_half", v)
			}
		case "north", "east", "south", "west":
			// Panes, bars and fences (1.26: minecraft:connection_*); walls
			// (low/tall -> short/tall).
			setProp(props, "minecraft:connection_"+k, v == "true")
			switch v {
			case "low":
				setProp(props, "wall_connection_type_"+k, "short")
			case "tall":
				setProp(props, "wall_connection_type_"+k, "tall")
			case "none":
				setProp(props, "wall_connection_type_"+k, "none")
			}
		case "persistent":
			setProp(props, "persistent_bit", v == "true")
		case "attachment":
			// Bells.
			switch v {
			case "floor":
				setProp(props, "attachment", "standing")
			case "ceiling":
				setProp(props, "attachment", "hanging")
			case "single_wall":
				setProp(props, "attachment", "side")
			case "double_wall":
				setProp(props, "attachment", "multiple")
			}
		case "axis":
			setProp(props, "pillar_axis", v)
		case "part":
			setProp(props, "head_piece_bit", v == "head")
		case "open":
			setProp(props, "open_bit", v == "true")
		case "hinge":
			setProp(props, "door_hinge_bit", v == "right")
		case "occupied":
			setProp(props, "occupied_bit", v == "true")
		case "powered":
			setProp(props, "powered_bit", v == "true")
		case "lit":
			setProp(props, "lit", v == "true")
		case "hanging":
			// Lanterns and chains hung from a ceiling.
			setProp(props, "hanging", v == "true")
		case "attached":
			setProp(props, "attached_bit", v == "true")
		case "has_bottle_0":
			setProp(props, "brewing_stand_slot_a_bit", v == "true")
		case "has_bottle_1":
			setProp(props, "brewing_stand_slot_b_bit", v == "true")
		case "has_bottle_2":
			setProp(props, "brewing_stand_slot_c_bit", v == "true")
		case "age", "level", "layers", "rotation", "pickles", "bites", "moisture", "candles":
			if n64, err := strconv.ParseInt(v, 10, 32); err == nil {
				n := int32(n64)
				switch k {
				case "age":
					setProp(props, "age", n)
					setProp(props, "growth", n)
				case "level":
					setProp(props, "fill_level", n)
				case "layers":
					setProp(props, "height", n-1)
				case "rotation":
					setProp(props, "ground_sign_direction", n)
				case "pickles":
					setProp(props, "cluster_count", n-1)
				case "bites":
					setProp(props, "bite_counter", n)
				case "moisture":
					setProp(props, "moisturized_amount", n)
				case "candles":
					setProp(props, "candles", n-1)
				}
			}
		}
	}
	if f, ok := st.Props["facing"]; !ok || f == "up" {
		setProp(props, "torch_facing_direction", "top")
	}
	// Floor and ceiling buttons point up and down; facing only turns them.
	if strings.HasSuffix(target, "button") {
		switch st.Props["face"] {
		case "floor":
			setProp(props, "facing_direction", int32(1))
		case "ceiling":
			setProp(props, "facing_direction", int32(0))
		}
	}
	if target == "lever" {
		setProp(props, "lever_direction", leverDirection(st.Props))
		setProp(props, "open_bit", st.Props["powered"] == "true")
	}
	id, ok := l.reg.Lookup(full, props)
	if !ok {
		id = def
	}
	return id, water, true
}

// setProp sets key in props to v, converted to the type key already has;
// a key the block does not have is left out.
func setProp(props map[string]any, key string, v any) {
	cur, ok := props[key]
	if !ok {
		return
	}
	switch cur.(type) {
	case int32:
		switch x := v.(type) {
		case int32:
			props[key] = x
		case bool:
			props[key] = b2i(x)
		}
	case uint8:
		switch x := v.(type) {
		case bool:
			props[key] = uint8(b2i(x))
		case int32:
			props[key] = uint8(x)
		}
	case string:
		if s, ok := v.(string); ok {
			props[key] = s
		}
	}
}

func b2i(b bool) int32 {
	if b {
		return 1
	}
	return 0
}
