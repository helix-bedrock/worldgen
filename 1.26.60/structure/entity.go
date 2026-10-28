package structure

// EntitySpawn is an entity generation places, for the engine to spawn when
// the chunk is first loaded: worldgen itself never makes entities.
type EntitySpawn struct {
	// X, Y and Z are the entity's position (its feet), Yaw and Pitch its
	// rotation in degrees.
	X, Y, Z    float64
	Yaw, Pitch float32
	// Identifier is the entity type ("minecraft:ender_crystal").
	Identifier string
	// NBT holds the saved fields BDS gives the entity beyond the type and
	// position ("ShowBottom" for an End crystal); nil for none.
	NBT map[string]any
	// Source names what placed it ("end_spike").
	Source string
}
