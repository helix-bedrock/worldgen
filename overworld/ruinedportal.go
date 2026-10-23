package overworld

import (
	"slices"

	"github.com/helix-bedrock/worldgen/structure"
)

// Ruined portals: structure.Lib.RuinedPortal builds them (the same code as
// the Nether's); this file places them on the overworld's grid and builds
// each into the undecorated terrain.

// portalPick is the chunk a ruined portal region picks.
func (o *Overworld) portalPick(rx, rz int32) (int32, int32) {
	if x, z, ok := o.forcedPick(skRuinedPortal, rx, rz); ok {
		return x, z
	}
	return structure.PortalPick(o.seed, rx, rz, o.place[skRuinedPortal].spacing, o.place[skRuinedPortal].separation, o.place[skRuinedPortal].salt)
}

// isPortalChunk reports whether a ruined portal starts in chunk (cx, cz).
// Every region has one.
func (o *Overworld) isPortalChunk(cx, cz int32) bool {
	x, z := o.portalPick(structure.Region(cx, o.place[skRuinedPortal].spacing), structure.Region(cz, o.place[skRuinedPortal].spacing))
	return x == cx && z == cz && (o.isForced(skRuinedPortal, cx, cz) || o.structOK(skRuinedPortal, cx, cz))
}

// portalsReach calls f for each portal start whose blocks may reach chunk
// (cx, cz): the box is within 16 blocks of the start chunk's corner and
// the netherrack spreads 14 past its middle.
func (o *Overworld) portalsReach(cx, cz int32, f func(sx, sz int32)) {
	for rx := structure.Region(cx-2, o.place[skRuinedPortal].spacing); rx <= structure.Region(cx+2, o.place[skRuinedPortal].spacing); rx++ {
		for rz := structure.Region(cz-2, o.place[skRuinedPortal].spacing); rz <= structure.Region(cz+2, o.place[skRuinedPortal].spacing); rz++ {
			sx, sz := o.portalPick(rx, rz)
			if sx >= cx-2 && sx <= cx+2 && sz >= cz-2 && sz <= cz+2 {
				f(sx, sz)
			}
		}
	}
}

// portalReaches reports whether a ruined portal may have blocks in chunk
// (cx, cz).
func (o *Overworld) portalReaches(cx, cz int32) bool {
	found := false
	o.portalsReach(cx, cz, func(int32, int32) { found = true })
	return found
}

// placePortals writes the ruined portals that reach chunk (cx, cz) into c.
// raw gives undecorated terrain.
func (o *Overworld) placePortals(cx, cz int32, c *Chunk, raw func(cx, cz int32) *PackedChunk) {
	var b *scBuild
	o.portalsReach(cx, cz, func(sx, sz int32) {
		p := o.portalAt(sx, sz, raw)
		if p == nil {
			return
		}
		ws := p.Blocks[[2]int32{cx, cz}]
		if len(ws) == 0 {
			return
		}
		if b == nil {
			b = &scBuild{o: o, c: c, cbox: structure.NewBox(cx*16, MinY, cz*16, cx*16+15, MaxY-1, cz*16+15)}
		}
		for _, w := range ws {
			if !b.setWorld(w.X, w.Y, w.Z, w.ID) {
				continue
			}
			b.dropEntity([3]int32{w.X, w.Y, w.Z})
			if w.Water {
				i := (w.Y-MinY)<<8 | (w.Z&15)<<4 | w.X&15
				c.Water[i>>waterWordShift] |= 1 << (i & waterBitMask)
			}
			if w.Data != nil {
				c.BlockEntities = append(c.BlockEntities, structure.BlockEntity{X: w.X, Y: w.Y, Z: w.Z, Data: w.Data})
			}
		}
	})
}

// portalAt returns the ruined portal starting in chunk (cx, cz), nil if
// none does. It is built once per start.
func (o *Overworld) portalAt(cx, cz int32, raw func(cx, cz int32) *PackedChunk) *structure.RuinedPortal {
	k := [2]int32{cx, cz}
	if v, ok := o.portals.Load(k); ok {
		p, _ := v.(*structure.RuinedPortal)
		return p
	}
	var p *structure.RuinedPortal
	if o.isPortalChunk(cx, cz) {
		p = o.st.RuinedPortal(cx, cz, o.portalWorld(cx, cz, raw))
	}
	o.portals.Store(k, p)
	return p
}

// biomeHas reports whether a biome has a tag.
func (o *Overworld) biomeHas(b uint32, tag string) bool {
	if int(b) >= len(o.biomeTags) {
		return false
	}
	return slices.Contains(o.biomeTags[b], tag)
}

// portalWorld is the overworld a portal is built in: raw gives the
// undecorated terrain.
func (o *Overworld) portalWorld(cx, cz int32, raw func(cx, cz int32) *PackedChunk) structure.PortalWorld {
	terrain := &cachedTerrain{o: o, raw: raw, chunks: map[[2]int32]*PackedChunk{}}
	return structure.PortalWorld{
		Block: terrain.block, MinY: MinY, MaxY: MaxY, Sea: SeaLevel,
		Solid:    o.solidMaterial,
		Leaves:   func(b uint32) bool { return o.hasBlockTag(b, "leaves") },
		BiomeHas: func(x, y, z int32, tag string) bool { return o.biomeHas(o.BiomeAt(x, y, z), tag) },
		StartY:   o.structureY(cx, cz, noSurfaceY),
	}
}

// NearestRuinedPortal finds the ruined portal nearest to block (x, z),
// searching maxRegions placement regions (40 chunks each) around it, and
// returns the middle of its box: above it, or inside it for a buried one
// (Kind "buried ruined_portal"). It is safe for concurrent use.
func (o *Overworld) NearestRuinedPortal(x, z int32, maxRegions int32) (Scattered, bool) {
	var cands []locateCand
	rx0, rz0 := structure.Region(x>>4, o.place[skRuinedPortal].spacing), structure.Region(z>>4, o.place[skRuinedPortal].spacing)
	for rx := rx0 - maxRegions; rx <= rx0+maxRegions; rx++ {
		for rz := rz0 - maxRegions; rz <= rz0+maxRegions; rz++ {
			cx, cz := o.portalPick(rx, rz)
			dx, dz := int64(cx*16+8-x), int64(cz*16+8-z)
			cands = append(cands, locateCand{cx, cz, dx*dx + dz*dz})
		}
	}
	slices.SortFunc(cands, compareLocateCand)
	raw := newRawChunks(o)
	for _, c := range cands {
		p := o.portalAt(c.cx, c.cz, raw.get)
		if p == nil {
			continue
		}
		s := Scattered{Kind: "ruined_portal", X: (p.Box.X0 + p.Box.X1) / 2, Y: p.Box.Y1 + 1, Z: (p.Box.Z0 + p.Box.Z1) / 2}
		if p.Under {
			// A buried portal keeps its air: its middle is open.
			s.Kind, s.Y = "buried ruined_portal", p.Box.Y0+1
		}
		return s, true
	}
	return Scattered{}, false
}

// cachedTerrain reads blocks from a raw terrain source, asking it for each
// chunk once.
type cachedTerrain struct {
	o      *Overworld
	raw    func(cx, cz int32) *PackedChunk
	chunks map[[2]int32]*PackedChunk
}

// block is the block at (x, y, z); air where raw has no chunk.
func (t *cachedTerrain) block(x, y, z int32) uint32 {
	k := [2]int32{x >> 4, z >> 4}
	c, ok := t.chunks[k]
	if !ok {
		c = t.raw(k[0], k[1])
		t.chunks[k] = c
	}
	if c == nil {
		return t.o.ids.air
	}
	return c.at(int(y-MinY)<<8 | int(z&15)<<4 | int(x&15))
}
