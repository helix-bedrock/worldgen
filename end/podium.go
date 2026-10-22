package end

import (
	"github.com/helix-bedrock/worldgen/cube"
	"github.com/helix-bedrock/worldgen/structure"
)

// The exit portal's shape (EndPodiumFeature): every block within
// podiumReach on each axis, from one below the ground to podiumAir above,
// whose squared distance from the middle is at most podiumOuter2. Below
// the ground those within podiumInner2 are bedrock, the rest end stone; on
// the ground those within podiumInner2 are the portal, the rest bedrock.
// The pillar in the middle is podiumPillar tall, with the torches on its
// side at podiumTorchY.
const (
	podiumReach  = 4
	podiumAir    = 4
	podiumOuter2 = 12
	podiumInner2 = 7
	podiumPillar = 4
	podiumTorchY = 2
)

// crystalIdentifier is the entity standing on each pillar.
const crystalIdentifier = "minecraft:ender_crystal"

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
			Identifier: crystalIdentifier,
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
	p := podium{y0: y0}
	for dx := int32(-podiumReach); dx <= podiumReach; dx++ {
		for dy := int32(-1); dy <= podiumAir; dy++ {
			for dz := int32(-podiumReach); dz <= podiumReach; dz++ {
				d := dx*dx + dy*dy + dz*dz
				switch {
				case d > podiumOuter2:
				case dy < 0 && d < podiumInner2:
					p.add(dx, dy, dz, e.deco.bedrock)
				case dy < 0:
					p.add(dx, dy, dz, e.ids.endStone)
				case dy > 0:
					p.add(dx, dy, dz, e.ids.air)
				case d >= podiumInner2:
					p.add(dx, dy, dz, e.deco.bedrock)
				case open:
					p.add(dx, dy, dz, e.deco.endPortal)
				default:
					p.add(dx, dy, dz, e.ids.air)
				}
			}
		}
	}
	for dy := range int32(podiumPillar) {
		p.add(0, dy, 0, e.deco.bedrock)
	}
	// The torches hang on the pillar, each on the side away from it.
	for _, d := range cube.Directions() {
		dx, dz := d.Step()
		p.add(dx, podiumTorchY, dz, e.deco.torches[d])
	}
	return p.out
}

// podium collects the exit portal's blocks, by offsets from the middle of
// the ground at height y0.
type podium struct {
	y0  int32
	out []PodiumBlock
}

func (p *podium) add(dx, dy, dz int32, id uint32) {
	p.out = append(p.out, PodiumBlock{dx, p.y0 + dy, dz, id})
}
