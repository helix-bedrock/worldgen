package overworld

import "strings"

// Connection kinds of joinKind.
const (
	joinFence = 1 + iota
	joinPane
)

// joinKindOf sorts a block name for connectBlocks.
func joinKindOf(name string) uint8 {
	switch {
	case strings.HasSuffix(name, "_fence"):
		return joinFence
	case strings.HasSuffix(name, "glass_pane"), name == "minecraft:iron_bars":
		return joinPane
	}
	return 0
}

// connectBlocks sets the connections of every fence, pane and iron bar in
// chunk (cx, cz). BDS sets them when a structure template is placed: it
// recomputes the connections in the template's box and the ring of blocks
// around it (sub_D262990), so blocks from later pieces join earlier ones.
// Features never trigger it, so a fence does not join a tree or plant
// decorated next to it. connectBlocks reads every neighbour from near, the
// chunk's terrain with its structures and without features (nil: air).
// Fences join fences of their kind, fence gates along their wall and
// full faces; panes and bars join panes, bars and full faces.
// VERIFY: blocks code-built pieces (strongholds, mineshafts) set without a
// template are joined the same way here; their placement path was not read.
func (o *Overworld) connectBlocks(c *Chunk, cx, cz int32, near func(cx, cz int32) *PackedChunk) {
	n := neighbourChunks{cx: cx, cz: cz, near: near, air: o.ids.air}
	for i, b := range c.Blocks {
		if int(b) >= len(o.joinKind) || o.joinKind[b] == 0 {
			continue
		}
		id := uint32(b)
		kind := o.joinKind[b]
		x, y, z := cx*16+int32(i&15), int32(i>>8)+MinY, cz*16+int32(i>>4&15)
		nid := id
		for _, side := range joinSides {
			nb := n.at(x+side.dx, y, z+side.dz)
			join := o.faceSturdy(nb, side.facing)
			if kind == joinFence {
				join = join || o.gateAccepts(nb, side.dx != 0) || (o.isFence(nb) && o.netherFence(nb) == o.netherFence(id))
			} else {
				join = join || int(nb) < len(o.joinKind) && o.joinKind[nb] == joinPane
			}
			nid = o.withProp(nid, "minecraft:connection_"+side.name, join)
		}
		if nid != id {
			c.Blocks[i] = uint16(nid)
		}
	}
}

// neighbourChunks reads blocks of chunk (cx, cz) and the eight chunks
// around it, asking near for each chunk once.
type neighbourChunks struct {
	cx, cz int32
	near   func(cx, cz int32) *PackedChunk
	air    uint32
	others [3][3]*PackedChunk
	asked  [3][3]bool
}

// at is the block at (x, y, z), air where near has no chunk.
func (n *neighbourChunks) at(x, y, z int32) uint32 {
	dx, dz := x>>4-n.cx+1, z>>4-n.cz+1
	if !n.asked[dx][dz] {
		n.asked[dx][dz] = true
		n.others[dx][dz] = n.near(x>>4, z>>4)
	}
	if p := n.others[dx][dz]; p != nil {
		return p.at(int((y-MinY)<<8 | (z&15)<<4 | x&15))
	}
	return n.air
}

// joinSides are the four sides a block joins on, with the face of the
// neighbour that touches it (the stairs' weirdo_direction of a full back
// face on that side).
var joinSides = [4]struct {
	name   string
	dx, dz int32
	facing int32
}{{"north", 0, -1, 2}, {"east", 1, 0, 1}, {"south", 0, 1, 3}, {"west", -1, 0, 0}}

// faceSturdy reports whether the face of nb that touches a fence or pane is
// a full face. Stairs have one: their back, on the side weirdo_direction
// names (0 east, 1 west, 2 south, 3 north). A bell has none.
func (o *Overworld) faceSturdy(nb uint32, facing int32) bool {
	name := o.blockName(nb)
	switch {
	case strings.HasSuffix(name, "_stairs"):
		st, ok := o.reg.State(nb)
		if !ok {
			return false
		}
		switch d := st.Props["weirdo_direction"].(type) {
		case int32:
			return d == facing
		case uint8:
			return int32(d) == facing
		}
		return false
	case name == "minecraft:bell":
		return false
	}
	return o.sturdyFace(nb, 0)
}

// gateAccepts reports whether a fence gate joins a fence beside it: only
// along the gate's wall, east and west of a gate facing north or south
// (BDS's gate connection rule, sub_C566C20).
func (o *Overworld) gateAccepts(gate uint32, eastWest bool) bool {
	if !o.isFenceGate(gate) {
		return false
	}
	st, ok := o.reg.State(gate)
	if !ok {
		return false
	}
	d, _ := st.Props[stateCardinalDirection].(string)
	return eastWest == (d == "north" || d == "south")
}

// isFence, isFenceGate and netherFence sort fences for the connection
// rule: wooden fences join each other, the nether brick fence only its own
// kind.
func (o *Overworld) isFence(id uint32) bool {
	return strings.HasSuffix(o.blockName(id), "_fence")
}

func (o *Overworld) isFenceGate(id uint32) bool {
	return strings.HasSuffix(o.blockName(id), "fence_gate")
}

func (o *Overworld) netherFence(id uint32) bool {
	return o.blockName(id) == "minecraft:nether_brick_fence"
}
