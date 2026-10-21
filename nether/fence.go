package nether

import (
	"fmt"
	"strings"
)

func errMissing(name string) error {
	return fmt.Errorf("worldgen: block %s missing from the block list", name)
}

// fenceSides are the four sides a fence joins on, in the bit order of
// fenceStates, with the weirdo_direction of a stairs whose full back faces
// the fence.
var fenceSides = [4]struct {
	name   string
	dx, dz int32
	facing int32
}{{"north", 0, -1, 2}, {"east", 1, 0, 1}, {"south", 0, 1, 3}, {"west", -1, 0, 0}}

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
		case s.Name == "minecraft:nether_brick_fence":
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
		for i, side := range fenceSides {
			props["minecraft:connection_"+side.name] = mask>>i&1 == 1
		}
		id, ok := n.reg.Lookup("minecraft:nether_brick_fence", props)
		if !ok {
			return errMissing("minecraft:nether_brick_fence")
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
	var others [3][3]*Chunk
	var asked [3][3]bool
	at := func(x, y, z int32) uint32 {
		if x>>4 == cx && z>>4 == cz {
			return c.Block(int(x&15), int(y), int(z&15))
		}
		dx, dz := x>>4-cx+1, z>>4-cz+1
		if !asked[dx][dz] {
			asked[dx][dz] = true
			others[dx][dz] = near(x>>4, z>>4)
		}
		if o := others[dx][dz]; o != nil {
			return o.Block(int(x&15), int(y), int(z&15))
		}
		return n.ids.air
	}
	for i, b := range c.Blocks {
		if int(b) >= len(n.fenceJoin) || n.fenceJoin[b] != joinNetherFence {
			continue
		}
		x, y, z := cx*16+int32(i&15), int32(i>>8), cz*16+int32(i>>4&15)
		mask := 0
		for s, side := range fenceSides {
			nb := at(x+side.dx, y, z+side.dz)
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
