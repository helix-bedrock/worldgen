package overworld

import "strings"

// joinFences sets the connections of the fences, panes and iron bars in
// area, which lies in chunk (cx, cz). Pieces built in code place them
// unconnected; BDS joins them to the blocks beside them as it places them
// (the connection rule): fences to fences of their kind, fence gates and
// full faces, panes and bars to panes, bars and full faces. A block in
// another chunk is read from that chunk's terrain (raw) with the
// structure built in by build.
// VERIFY: whether BDS sees the neighbouring chunk's structure blocks or
// only what is there when this chunk is built.
func (o *Overworld) joinFences(c *Chunk, cx, cz int32, area box, raw func(cx, cz int32) *PackedChunk, build func(cx, cz int32, n *Chunk)) {
	cbox := box{cx * 16, MinY, cz * 16, cx*16 + 15, MaxY - 1, cz*16 + 15}
	var others map[[2]int32]*Chunk
	at := func(x, y, z int32) uint32 {
		if cbox.contains([3]int32{x, y, z}) {
			return c.Block(int(x&15), int(y), int(z&15))
		}
		k := [2]int32{x >> 4, z >> 4}
		n, ok := others[k]
		if !ok {
			if others == nil {
				others = map[[2]int32]*Chunk{}
			}
			if pc := raw(k[0], k[1]); pc != nil {
				n = &Chunk{}
				pc.Unpack(n)
				build(k[0], k[1], n)
			}
			others[k] = n
		}
		if n == nil || y < MinY || y >= MaxY {
			return o.ids.air
		}
		return n.Block(int(x&15), int(y), int(z&15))
	}
	for x := area.x0; x <= area.x1; x++ {
		for z := area.z0; z <= area.z1; z++ {
			for y := max(area.y0, MinY); y <= min(area.y1, MaxY-1); y++ {
				id := c.Block(int(x&15), int(y), int(z&15))
				fence, pane := o.isFence(id), o.isPane(id)
				if !fence && !pane {
					continue
				}
				nid := id
				for _, side := range [4]struct {
					name   string
					dx, dz int32
				}{{"north", 0, -1}, {"east", 1, 0}, {"south", 0, 1}, {"west", -1, 0}} {
					nb := at(x+side.dx, y, z+side.dz)
					join := o.sturdyFace(nb, 0)
					if fence {
						join = join || o.gateAccepts(nb, side.dx != 0) || (o.isFence(nb) && o.netherFence(nb) == o.netherFence(id))
					} else {
						join = join || o.isPane(nb)
					}
					nid = o.withProp(nid, "minecraft:connection_"+side.name, join)
				}
				if nid != id {
					c.set(int(x&15), int(y), int(z&15), nid)
				}
			}
		}
	}
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
	d, _ := st.Props["minecraft:cardinal_direction"].(string)
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
