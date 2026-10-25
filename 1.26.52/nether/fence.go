package nether

import (
	"fmt"
	"strings"
)

func errMissing(name string) error {
	return fmt.Errorf("worldgen: block %s missing from the block list", name)
}

// fenceSide is one side a fence joins on.
type fenceSide struct {
	name   string
	dx, dz int32
	facing int32
}

// fenceSides returns the four sides a fence joins on, in the bit order of
// fenceStates, with the weirdo_direction of a stairs whose full back faces
// the fence.
func fenceSides() [4]fenceSide {
	return [4]fenceSide{{"north", 0, -1, 2}, {"east", 1, 0, 1}, {"south", 0, 1, 3}, {"west", -1, 0, 0}}
}

// loadFences reads the nether brick fence's connection states and sorts
// the blocks a fence joins.
func (n *Nether) loadFences() error {
	n.fenceJoin = make([]uint8, n.reg.Len())
	for i := range n.fenceJoin {
		s, ok := n.reg.State(uint32(i))
		if !ok {
			continue
		}
		switch {
		case s.Name == netherBrickFence:
			n.fenceJoin[i] = joinNetherFence
		case strings.HasSuffix(s.Name, "_stairs"):
			n.fenceJoin[i] = joinStairs
		case strings.HasSuffix(s.Name, "_slab") || strings.HasSuffix(s.Name, "_fence") || strings.HasSuffix(s.Name, "_fence_gate"):
		case i < len(n.topSupport) && n.topSupport[i]:
			n.fenceJoin[i] = joinFull
		}
	}
	for mask := range n.nb.fences {
		props := map[string]any{}
		for i, side := range fenceSides() {
			props["minecraft:connection_"+side.name] = mask>>i&1 == 1
		}
		id, ok := n.reg.Lookup(netherBrickFence, props)
		if !ok {
			return errMissing(netherBrickFence)
		}
		n.nb.fences[mask] = id
	}
	return nil
}

// What a block beside a nether brick fence is to it.
const (
	joinNone uint8 = iota
	joinFull
	joinStairs
	joinNetherFence
)

// connectFences sets the connections of the nether brick fences in chunk
// (cx, cz) from the decorated chunk: BDS's fortress fences join glowstone
// the decoration grew beside them. near gives a decorated neighbour (nil:
// air). A fence joins nether brick fences, full blocks and the full back of
// stairs.
// VERIFY: full faces are taken as the blocks with a sturdy top.
func (n *Nether) connectFences(c *Chunk, cx, cz int32, near func(cx, cz int32) *Chunk) {
	v := fenceView{n: n, c: c, cx: cx, cz: cz, near: near}
	for i, b := range c.Blocks {
		if int(b) >= len(n.fenceJoin) || n.fenceJoin[b] != joinNetherFence {
			continue
		}
		x, y, z := cx*16+int32(i&15), int32(i>>8), cz*16+int32(i>>4&15)
		mask := 0
		for s, side := range fenceSides() {
			nb := v.at(x+side.dx, y, z+side.dz)
			join := false
			switch n.fenceJoin[nb] {
			case joinFull, joinNetherFence:
				join = true
			case joinStairs:
				if st, ok := n.reg.State(nb); ok {
					switch d := st.Props["weirdo_direction"].(type) {
					case int32:
						join = d == side.facing
					case uint8:
						join = int32(d) == side.facing
					}
				}
			}
			if join {
				mask |= 1 << s
			}
		}
		c.Blocks[i] = uint16(n.nb.fences[mask])
	}
}

// Block names the Nether loads more than once.
const (
	// netherBrickFence is the nether brick fence.
	netherBrickFence = "minecraft:nether_brick_fence"
	// blockAir is air.
	blockAir = "minecraft:air"
	// blockLava is still lava.
	blockLava = "minecraft:lava"
	// blockFlowingLava is flowing lava.
	blockFlowingLava = "minecraft:flowing_lava"
	// blockWater is still water.
	blockWater = "minecraft:water"
	// blockFlowingWater is flowing water.
	blockFlowingWater = "minecraft:flowing_water"
	// blockFire is fire.
	blockFire = "minecraft:fire"
	// blockSoulFire is soul fire.
	blockSoulFire = "minecraft:soul_fire"
	// blockSoulSand is soul sand.
	blockSoulSand = "minecraft:soul_sand"
	// blockBasalt is basalt.
	blockBasalt = "minecraft:basalt"
	// blockNetherrack is netherrack.
	blockNetherrack = "minecraft:netherrack"
	// blockNetherWart is the nether wart block.
	blockNetherWart = "minecraft:nether_wart_block"
	// blockWarpedWart is the warped wart block.
	blockWarpedWart = "minecraft:warped_wart_block"
	// blockWeepingVines is weeping vines.
	blockWeepingVines = "minecraft:weeping_vines"
	// blockTwistingVines is twisting vines.
	blockTwistingVines = "minecraft:twisting_vines"
	// blockChest is the chest.
	blockChest = "minecraft:chest"
	// blockNetherBrickStairs is the nether brick stairs.
	blockNetherBrickStairs = "minecraft:nether_brick_stairs"
	// blockBoneBlock is the bone block.
	blockBoneBlock = "minecraft:bone_block"
	// blockDirt is dirt.
	blockDirt = "minecraft:dirt"
	// blockCoarseDirt is coarse dirt.
	blockCoarseDirt = "minecraft:coarse_dirt"
	// blockGrass is the grass block.
	blockGrass = "minecraft:grass_block"
	// blockCrimsonFungus is the crimson fungus.
	blockCrimsonFungus = "minecraft:crimson_fungus"
	// blockWarpedFungus is the warped fungus.
	blockWarpedFungus = "minecraft:warped_fungus"
	// blockCrimsonRoots is crimson roots.
	blockCrimsonRoots = "minecraft:crimson_roots"
	// blockWarpedRoots is warped roots.
	blockWarpedRoots = "minecraft:warped_roots"
	// blockNetherSprouts is nether sprouts.
	blockNetherSprouts = "minecraft:nether_sprouts"
	// blockBrownMushroom is the brown mushroom.
	blockBrownMushroom = "minecraft:brown_mushroom"
	// blockRedMushroom is the red mushroom.
	blockRedMushroom = "minecraft:red_mushroom"
)

// fenceView reads the blocks around a chunk being connected, asking for
// each decorated neighbour at most once.
type fenceView struct {
	n      *Nether
	c      *Chunk
	cx, cz int32
	near   func(cx, cz int32) *Chunk
	others [3][3]*Chunk
	asked  [3][3]bool
}

// at returns the block at world (x, y, z), air where no neighbour is given.
func (v *fenceView) at(x, y, z int32) uint32 {
	if x>>4 == v.cx && z>>4 == v.cz {
		return v.c.Block(int(x&15), int(y), int(z&15))
	}
	dx, dz := x>>4-v.cx+1, z>>4-v.cz+1
	if !v.asked[dx][dz] {
		v.asked[dx][dz] = true
		v.others[dx][dz] = v.near(x>>4, z>>4)
	}
	if o := v.others[dx][dz]; o != nil {
		return o.Block(int(x&15), int(y), int(z&15))
	}
	return v.n.ids.air
}
