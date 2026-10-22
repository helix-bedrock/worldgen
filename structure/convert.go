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

var javaBlockNames = map[string]string{
	"torch":                        "torch",
	"wall_torch":                   "torch",
	"bricks":                       "brick_block",
	"terracotta":                   "hardened_clay",
	"oak_fence_gate":               "fence_gate",
	"light_gray_glazed_terracotta": "silver_glazed_terracotta",
	"melon":                        "melon_block",
	"oak_door":                     "wooden_door",
	"dirt_path":                    "grass_path",
	"chain":                        "iron_chain",
	"tripwire":                     "trip_wire",
	"magma_block":                  "magma",
	"attached_melon_stem":          "melon_stem",
	"oak_pressure_plate":           "wooden_pressure_plate",
	"oak_trapdoor":                 "trapdoor",
	"cobblestone_stairs":           "stone_stairs",
	"skeleton_skull":               "skeleton_skull",
	"grass":                        "short_grass",
	"dead_bush":                    "deadbush",
	"snow_block":                   "snow",
	"snow":                         "snow_layer",
	"stonecutter":                  "stonecutter_block",
	"oak_sign":                     "standing_sign",
	"blackstone_bricks":            "polished_blackstone_bricks",
	"spawner":                      "mob_spawner",
	"note_block":                   "noteblock",
	"repeater":                     "unpowered_repeater",
	"comparator":                   "unpowered_comparator",
	"redstone_torch":               "unlit_redstone_torch",
	"redstone_wall_torch":          "unlit_redstone_torch",
	"oak_button":                   "wooden_button",
	"cobweb":                       "web",
	"waxed_copper_block":           "waxed_copper",
	"cave_air":                     "air",
	"piston_head":                  "air",
	"water_cauldron":               "cauldron",
	"sugar_cane":                   "reeds",
	"lily_pad":                     "waterlily",
}

// javaBlockContains are checked after the exact names, in this order.
var javaBlockContains = [][2]string{
	{"straw_bed", "straw_bed"},
	{"bed", "bed"},
	{"wall_banner", "wall_banner"},
	{"potted", "flower_pot"},
}

// blockEntityIDs are the Bedrock block entity ids of the blocks that have
// one, by Bedrock block name (suffix match for the families).
var blockEntityIDs = map[string]string{
	"minecraft:bed": "Bed", "minecraft:bell": "Bell", "minecraft:chest": "Chest",
	"minecraft:trapped_chest": "Chest", "minecraft:barrel": "Barrel", "minecraft:furnace": "Furnace",
	"minecraft:lit_furnace": "Furnace", "minecraft:blast_furnace": "BlastFurnace", "minecraft:smoker": "Smoker",
	"minecraft:flower_pot": "FlowerPot", "minecraft:lectern": "Lectern", "minecraft:brewing_stand": "BrewingStand",
	"minecraft:cauldron": "Cauldron", "minecraft:jukebox": "Jukebox", "minecraft:hopper": "Hopper",
	"minecraft:dispenser": "Dispenser", "minecraft:dropper": "Dropper", "minecraft:campfire": "Campfire",
	"minecraft:soul_campfire": "Campfire", "minecraft:beehive": "Beehive", "minecraft:bee_nest": "Beehive",
	"minecraft:mob_spawner": "MobSpawner", "minecraft:noteblock": "Music", "minecraft:enchanting_table": "EnchantTable",
	"minecraft:standing_sign": "Sign", "minecraft:wall_sign": "Sign", "minecraft:standing_banner": "Banner",
	"minecraft:wall_banner": "Banner", "minecraft:skeleton_skull": "Skull", "minecraft:decorated_pot": "DecoratedPot",
	"minecraft:chiseled_bookshelf": "ChiseledBookshelf", "minecraft:unpowered_comparator": "Comparator",
	"minecraft:suspicious_sand": "BrushableBlock", "minecraft:suspicious_gravel": "BrushableBlock",
	"minecraft:trial_spawner": "TrialSpawner", "minecraft:vault": "Vault",
}

// bedColors are Bedrock's colour indexes for bed block entities.
var bedColors = map[string]uint8{"white": 0, "orange": 1, "magenta": 2, "light_blue": 3, "yellow": 4, "lime": 5,
	"pink": 6, "gray": 7, "light_gray": 8, "cyan": 9, "purple": 10, "blue": 11, "brown": 12, "green": 13, "red": 14, "black": 15}

// blockEntityFor returns the block entity data of a converted block placed
// at (x, y, z), nil if it has none. javaName gives the bed colour.
func (l *Lib) BlockEntityFor(id uint32, javaName string, x, y, z int32) map[string]any {
	name := l.BlockName(id)
	beID, ok := blockEntityIDs[name]
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
		d["color"] = bedColors[color]
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
var javaFacing = map[string]int32{"down": 0, "up": 1, "north": 2, "south": 3, "west": 4, "east": 5}

// Bedrock's horizontal "direction" encodings differ per block.
var (
	dirSWNE = map[string]int32{"south": 0, "west": 1, "north": 2, "east": 3} // beds, fence gates, repeaters
	dirESWN = map[string]int32{"east": 0, "south": 1, "west": 2, "north": 3} // doors
	dirEWSN = map[string]int32{"east": 0, "west": 1, "south": 2, "north": 3} // stairs, trapdoors
)

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

// convertJavaState returns the registry index of the Bedrock block for a
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
	target, ok := javaBlockNames[name]
	if !ok {
		target = name
		for _, c := range javaBlockContains {
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
	set := func(key string, v any) {
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
	water := false
	for k, v := range st.Props {
		switch k {
		case "waterlogged":
			water = v == "true"
		case "facing":
			if strings.HasSuffix(target, "_door") && !strings.Contains(target, "trapdoor") {
				// Doors: the legacy direction 0 was Java east and became
				// "south" (block upgrade schema 1.21.60), a quarter turn.
				set("minecraft:cardinal_direction", RotateFacing(v, 1))
			} else {
				set("minecraft:cardinal_direction", v)
			}
			set("minecraft:facing_direction", v)
			if f, ok := javaFacing[v]; ok {
				set("facing_direction", f)
			}
			// A wall torch's Java facing points away from its wall; Bedrock
			// names the side it hangs on.
			set("torch_facing_direction", Opposite(v))
			switch {
			case strings.HasSuffix(target, "_door"):
				set("direction", dirESWN[v])
			case strings.HasSuffix(target, "stairs"):
				set("weirdo_direction", dirEWSN[v])
			case strings.Contains(target, "trapdoor"):
				set("direction", dirEWSN[v])
			default:
				set("direction", dirSWNE[v])
			}
		case "half":
			top := v == "top" || v == "upper"
			set("upside_down_bit", top)
			set("upper_block_bit", top)
			if v == "top" || v == "bottom" {
				set("minecraft:vertical_half", v)
			}
		case "type":
			if v == "top" || v == "bottom" {
				set("minecraft:vertical_half", v)
			}
		case "north", "east", "south", "west":
			// Panes, bars and fences (1.26: minecraft:connection_*); walls
			// (low/tall -> short/tall).
			set("minecraft:connection_"+k, v == "true")
			switch v {
			case "low":
				set("wall_connection_type_"+k, "short")
			case "tall":
				set("wall_connection_type_"+k, "tall")
			case "none":
				set("wall_connection_type_"+k, "none")
			}
		case "persistent":
			set("persistent_bit", v == "true")
		case "attachment":
			// Bells.
			switch v {
			case "floor":
				set("attachment", "standing")
			case "ceiling":
				set("attachment", "hanging")
			case "single_wall":
				set("attachment", "side")
			case "double_wall":
				set("attachment", "multiple")
			}
		case "axis":
			set("pillar_axis", v)
		case "part":
			set("head_piece_bit", v == "head")
		case "open":
			set("open_bit", v == "true")
		case "hinge":
			set("door_hinge_bit", v == "right")
		case "occupied":
			set("occupied_bit", v == "true")
		case "powered":
			set("powered_bit", v == "true")
		case "lit":
			set("lit", v == "true")
		case "hanging":
			// Lanterns and chains hung from a ceiling.
			set("hanging", v == "true")
		case "attached":
			set("attached_bit", v == "true")
		case "age", "level", "layers", "rotation", "pickles", "bites", "moisture", "candles":
			if n64, err := strconv.ParseInt(v, 10, 32); err == nil {
				n := int32(n64)
				switch k {
				case "age":
					set("age", n)
					set("growth", n)
				case "level":
					set("fill_level", n)
				case "layers":
					set("height", n-1)
				case "rotation":
					set("ground_sign_direction", n)
				case "pickles":
					set("cluster_count", n-1)
				case "bites":
					set("bite_counter", n)
				case "moisture":
					set("moisturized_amount", n)
				case "candles":
					set("candles", n-1)
				}
			}
		}
	}
	if f, ok := st.Props["facing"]; !ok || f == "up" {
		set("torch_facing_direction", "top")
	}
	// Floor and ceiling buttons point up and down; facing only turns them.
	if strings.HasSuffix(target, "button") {
		switch st.Props["face"] {
		case "floor":
			set("facing_direction", int32(1))
		case "ceiling":
			set("facing_direction", int32(0))
		}
	}
	if target == "lever" {
		set("lever_direction", leverDirection(st.Props))
		set("open_bit", st.Props["powered"] == "true")
	}
	id, ok := l.reg.Lookup(full, props)
	if !ok {
		id = def
	}
	return id, water, true
}

func b2i(b bool) int32 {
	if b {
		return 1
	}
	return 0
}
