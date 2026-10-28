package nether

import (
	"cmp"
	"slices"

	"github.com/helix-bedrock/worldgen/1.26.60/internal/mathx"

	"github.com/helix-bedrock/worldgen/1.26.60/structure"
)

// Nether ruined portals are BDS's RuinedPortalFeature on the Nether's grid;
// structure.Lib.RuinedPortal builds them with the Nether branches of the
// code the overworld uses. Every grid region has one. BDS builds
// the whole portal from its start chunk into the world as it stands; a
// BDS world shows fortress support columns stopping on a portal's magma,
// so the portal is built first, on the bare terrain, and fortress pieces
// go over it.
// VERIFY: in BDS a neighbour chunk whose fortress pieces were built before
// the portal's start chunk was processed has the portal over the fortress
// instead; here the fortress always wins.

// portalReach is how many chunks from its start chunk a portal's blocks
// can reach: its box spans at most a chunk from the start chunk's corner
// and the netherrack spreads 14 past the box's middle.
const portalReach = 2

// placePortals writes the ruined portals that reach chunk (cx, cz) into c.
func (n *Nether) placePortals(cx, cz int32, c *Chunk) {
	n.portalsReach(cx, cz, func(sx, sz int32) {
		p := n.portalAt(sx, sz)
		if p == nil {
			return
		}
		for _, b := range p.Blocks[[2]int32{cx, cz}] {
			c.set(int(b.X&15), int(b.Y), int(b.Z&15), b.ID)
			c.dropEntity(b.X, b.Y, b.Z)
			if b.Data != nil {
				c.BlockEntities = append(c.BlockEntities, structure.BlockEntity{X: b.X, Y: b.Y, Z: b.Z, Data: b.Data})
			}
		}
	})
}

// portalPick is the start chunk of the portal of placement region (rx, rz).
func (n *Nether) portalPick(rx, rz int32) (int32, int32) {
	if at, ok := n.forcedIn(gridPortal, rx, rz); ok {
		return at.chunk[0], at.chunk[1]
	}
	return structure.PortalPick(n.seed, rx, rz, structure.NetherPortalSpacing, structure.NetherPortalSeparation, structure.PortalSalt)
}

// portalsReach calls f with the start chunk of each portal that may have
// blocks in chunk (cx, cz).
func (n *Nether) portalsReach(cx, cz int32, f func(sx, sz int32)) {
	sp := int32(structure.NetherPortalSpacing)
	for rx := mathx.FloorDiv(cx-portalReach, sp); rx <= mathx.FloorDiv(cx+portalReach, sp); rx++ {
		for rz := mathx.FloorDiv(cz-portalReach, sp); rz <= mathx.FloorDiv(cz+portalReach, sp); rz++ {
			sx, sz := n.portalPick(rx, rz)
			if mathx.Abs(sx-cx) <= portalReach && mathx.Abs(sz-cz) <= portalReach {
				f(sx, sz)
			}
		}
	}
}

// portalAt returns the portal starting in chunk (sx, sz), built once. sx,
// sz must be a start chunk.
func (n *Nether) portalAt(sx, sz int32) *structure.RuinedPortal {
	reg, _ := n.regionOf(skPortal, sx, sz)
	if at, ok := n.forcedIn(gridPortal, reg[0], reg[1]); (!ok || at.chunk != [2]int32{sx, sz}) && !n.gate.OK(skPortal, sx, sz) {
		return nil
	}
	k := [2]int32{sx, sz}
	if v, ok := n.portals.Load(k); ok {
		p, _ := v.(*structure.RuinedPortal)
		return p
	}
	b := baseChunks{n: n, chunks: map[[2]int32]*Chunk{}}
	p := n.st.RuinedPortal(sx, sz, structure.PortalWorld{Block: b.block, MaxY: Height, Sea: SeaLevel, Nether: true, Solid: n.solid})
	v, _ := n.portals.LoadOrStore(k, p)
	p, _ = v.(*structure.RuinedPortal)
	return p
}

// baseChunks generates base chunks on demand for a portal to read.
type baseChunks struct {
	n      *Nether
	chunks map[[2]int32]*Chunk
}

// block returns the base chunk block at (x, y, z).
func (b *baseChunks) block(x, y, z int32) uint32 {
	ck := [2]int32{x >> 4, z >> 4}
	c, ok := b.chunks[ck]
	if !ok {
		c = new(Chunk)
		b.n.baseChunk(ck[0], ck[1], c)
		b.chunks[ck] = c
	}
	return c.Block(int(x&15), int(y), int(z&15))
}

// NearestRuinedPortal finds the Nether ruined portal nearest to block
// (x, z), searching maxRegions placement regions around it, and returns a
// spot on the floor of its box, in its middle. It is safe for concurrent
// use.
func (n *Nether) NearestRuinedPortal(x, z, maxRegions int32) (px, py, pz int32, found bool) {
	var cands []startCand
	sp := int32(structure.NetherPortalSpacing)
	rx0, rz0 := mathx.FloorDiv(x>>4, sp), mathx.FloorDiv(z>>4, sp)
	for rx := rx0 - maxRegions; rx <= rx0+maxRegions; rx++ {
		for rz := rz0 - maxRegions; rz <= rz0+maxRegions; rz++ {
			cx, cz := n.portalPick(rx, rz)
			dx, dz := int64(cx*16+8-x), int64(cz*16+8-z)
			cands = append(cands, startCand{cx, cz, dx*dx + dz*dz})
		}
	}
	slices.SortFunc(cands, func(a, b startCand) int { return cmp.Compare(a.d, b.d) })
	for _, c := range cands {
		if p := n.portalAt(c.cx, c.cz); p != nil {
			return (p.Box.X0 + p.Box.X1) / 2, p.Box.Y0 + 1, (p.Box.Z0 + p.Box.Z1) / 2, true
		}
	}
	return 0, 0, 0, false
}

// dropEntity removes the block entity at a position the chunk's block there
// was replaced at.
func (c *Chunk) dropEntity(x, y, z int32) {
	if len(c.BlockEntities) == 0 {
		return
	}
	c.BlockEntities = slices.DeleteFunc(c.BlockEntities, func(e structure.BlockEntity) bool {
		return e.X == x && e.Y == y && e.Z == z
	})
}
