package end

import (
	"github.com/helix-bedrock/worldgen/structure"
)

// placeCrystals is TheEndGenerator's mob pass (sub_5197E50) for chunk
// (cx, cz): each pillar whose middle is in the chunk gets a bedrock block on
// top (infiniburn) and an End crystal standing on it (sub_49C51E0).
// VERIFY: BDS turns each crystal by a draw from the chunk's random (yaws
// seen: 47 to 108 degrees); the crystals here face yaw 0.
func (e *End) placeCrystals(cx, cz int32, c *Chunk) {
	for _, s := range e.spikes {
		if s.x>>4 != cx || s.z>>4 != cz {
			continue
		}
		c.set(int(s.x&15), int(s.height), int(s.z&15), e.deco.bedrock)
		c.Entities = append(c.Entities, structure.EntitySpawn{
			X: float64(s.x) + 0.5, Y: float64(s.height + 1), Z: float64(s.z) + 0.5,
			Identifier: "minecraft:ender_crystal",
			NBT:        map[string]any{"ShowBottom": uint8(1)},
			Source:     "end_spike",
		})
	}
}

// PodiumBlock is one block of the exit portal.
type PodiumBlock struct {
	X, Y, Z int32
	// ID is the registry index of the block.
	ID uint32
}

// ExitPortal returns the blocks of the exit portal on the main island
// (EndPodiumFeature, sub_D2DE740), open (with end portal blocks) or not.
// BDS builds it in the dragon fight (EndDragonFight's spawnExitPortal,
// sub_497D5B0), not in generation: closed when the fight starts, open
// once the dragon dies. It stands on the terrain at the origin: a bedrock
// bowl with an end stone rim below, a bedrock ring around the portal, air
// up to four blocks above, and a bedrock pillar with four torches in the
// middle. It is safe for concurrent use.
func (e *End) ExitPortal(open bool) []PodiumBlock {
	var c Chunk
	e.baseChunk(0, 0, &c)
	y0 := int32(0)
	for y := Height - 1; y >= 0; y-- {
		if c.Block(0, y, 0) != e.ids.air {
			y0 = int32(y) + 1
			break
		}
	}
	var out []PodiumBlock
	add := func(x, y, z int32, id uint32) { out = append(out, PodiumBlock{x, y0 + y, z, id}) }
	for dx := int32(-4); dx <= 4; dx++ {
		for dy := int32(-1); dy <= 4; dy++ {
			for dz := int32(-4); dz <= 4; dz++ {
				d := dx*dx + dy*dy + dz*dz
				switch {
				case d > 12:
				case dy < 0 && d < 7:
					add(dx, dy, dz, e.deco.bedrock)
				case dy < 0:
					add(dx, dy, dz, e.ids.endStone)
				case dy > 0:
					add(dx, dy, dz, e.ids.air)
				case d >= 7:
					add(dx, dy, dz, e.deco.bedrock)
				case open:
					add(dx, dy, dz, e.deco.endPortal)
				default:
					add(dx, dy, dz, e.ids.air)
				}
			}
		}
	}
	for dy := range int32(4) {
		add(0, dy, 0, e.deco.bedrock)
	}
	// The torches hang on the pillar, each on the side away from it.
	for i, d := range chorusDirs {
		add(d[0], 2, d[1], e.deco.torches[i])
	}
	return out
}
