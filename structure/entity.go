package structure

import "strings"

// EntitySpawn is an entity generation places, for the engine to spawn when
// the chunk is first loaded: worldgen itself never makes entities.
type EntitySpawn struct {
	// X, Y and Z are the entity's position (its feet), Yaw and Pitch its
	// rotation in degrees.
	X, Y, Z    float64
	Yaw, Pitch float32
	// Identifier is the entity type ("minecraft:ender_crystal").
	Identifier string
	// Event is the spawn event that picks the entity's component groups
	// ("minecraft:spawn_from_village"); empty for the definition's default.
	Event string
	// Persistent marks a mob that never despawns, as BDS sets on every mob a
	// structure places (the Persistent flag, type hash 0xFAD8D24F).
	Persistent bool
	// NBT holds the saved fields BDS gives the entity beyond the type and
	// position ("ShowBottom" for an End crystal); nil for none.
	NBT map[string]any
	// Source names what placed it ("end_spike", "village").
	Source string
}

// MobAt is the mob a structure places at block (x, y, z): BDS spawns it at
// the block's corner, unturned, and marks it persistent (the structure
// pieces' postProcessMobsAt, 1.26.60 sub_53704F0).
func MobAt(x, y, z int32, identifier, event, source string) EntitySpawn {
	return EntitySpawn{
		X: float64(x), Y: float64(y), Z: float64(z),
		Identifier: identifier, Event: event, Persistent: true, Source: source,
	}
}

// MarkerMob reads a jigsaw data marker's metadata as BDS does
// (1.26.60 sub_D80D140): "namespace:id[:event]", split on the first colon
// and then the next one, as in "minecraft:villager_v2:minecraft:spawn_from_village".
// A name with no colon has no namespace. ok is false for empty metadata.
func MarkerMob(metadata string) (identifier, event string, ok bool) {
	if metadata == "" {
		return "", "", false
	}
	ns, rest, found := strings.Cut(metadata, ":")
	if !found {
		return metadata, "", true
	}
	id, event, _ := strings.Cut(rest, ":")
	return ns + ":" + id, event, true
}
