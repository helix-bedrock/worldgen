package overworld

import "maps"

// PackedChunk is an undecorated chunk stored compactly for TerrainCache and
// Decorate's neighbour reads. A sub-chunk (16x16x16) of one block, such as
// the air above the terrain, is just that block; any other holds a palette
// and one byte per block, or two bytes in the rare case of more than 256
// states. A typical chunk takes about a fifth of a Chunk's memory, and a
// read is still two array lookups. It is immutable once made, so it may be
// read from many goroutines.
type PackedChunk struct {
	subs   [Height / 16]packedSub
	biomes [Height / 16]packedSub
	// biomeList holds the distinct biomes in BDS's order (see chunkBiomes).
	biomeList []uint32
	// water and blockEntities are those of structures placed in the
	// terrain; nil for plain terrain.
	water         []uint64
	blockEntities []BlockEntity
}

type packedSub struct {
	one     uint16 // the block, when the sub-chunk is a single block
	palette []uint16
	idx     []uint8  // palette index per block; nil if single or wide
	wide    []uint16 // states per block, when the palette is over 256
}

// at returns the state at Chunk index i.
func (p *PackedChunk) at(i int) uint32 {
	return uint32(p.subs[i>>12].at(i))
}

// Block returns the state index at local (x, z) and world y.
func (p *PackedChunk) Block(x, y, z int) uint32 {
	return p.at((y-MinY)<<8 | z<<4 | x)
}

// Biome returns the biome ID at local (x, z) and world y.
func (p *PackedChunk) Biome(x, y, z int) uint32 {
	i := (y-MinY)<<8 | z<<4 | x
	return uint32(p.biomes[i>>12].at(i))
}

// at returns the value at Chunk index i of a sub-chunk.
func (s *packedSub) at(i int) uint16 {
	if s.idx != nil {
		return s.palette[s.idx[i&4095]]
	}
	if s.wide != nil {
		return s.wide[i&4095]
	}
	return s.one
}

// Pack returns a compact copy of c, with its water bits and block entities
// when it has any.
func Pack(c *Chunk) *PackedChunk {
	p := &PackedChunk{biomeList: chunkBiomes(c)}
	for _, w := range c.Water {
		if w != 0 {
			p.water = append([]uint64(nil), c.Water[:]...)
			break
		}
	}
	if len(c.BlockEntities) > 0 {
		p.blockEntities = append([]BlockEntity(nil), c.BlockEntities...)
	}
	packSubs(&p.subs, c.Blocks[:])
	packSubs(&p.biomes, c.Biomes[:])
	return p
}

// packSubs packs src (Chunk index order) into one packedSub per sub-chunk.
func packSubs(subs *[Height / 16]packedSub, src []uint16) {
	var single [Height / 16]bool
	mixed := 0
	for s := range subs {
		vals := src[s<<12 : (s+1)<<12]
		single[s] = true
		for _, b := range vals {
			if b != vals[0] {
				single[s] = false
				mixed++
				break
			}
		}
	}
	// The index arrays of all sub-chunks share one allocation.
	slab := make([]uint8, mixed<<12)
	var pal [256]uint16
	for s := range subs {
		vals := src[s<<12 : (s+1)<<12]
		if single[s] {
			subs[s].one = vals[0]
			continue
		}
		idx := slab[:4096:4096]
		pal[0] = vals[0]
		n, last, lastIdx := 1, vals[0], uint8(0)
		ok := true
		for j, b := range vals {
			if b != last {
				k := 0
				for k < n && pal[k] != b {
					k++
				}
				if k == n {
					if n == len(pal) {
						ok = false
						break
					}
					pal[n] = b
					n++
				}
				last, lastIdx = b, uint8(k)
			}
			idx[j] = lastIdx
		}
		if !ok {
			subs[s].wide = append([]uint16(nil), vals...)
			continue
		}
		slab = slab[4096:]
		subs[s].palette = append([]uint16(nil), pal[:n]...)
		subs[s].idx = idx
	}
}

// unpackSubs writes packed sub-chunks back into dst (Chunk index order).
func unpackSubs(subs *[Height / 16]packedSub, dst []uint16) {
	for s := range subs {
		sub := &subs[s]
		d := dst[s<<12 : (s+1)<<12]
		switch {
		case sub.idx != nil:
			for j, k := range sub.idx {
				d[j] = sub.palette[k]
			}
		case sub.wide != nil:
			copy(d, sub.wide)
		default:
			b := sub.one
			for j := range d {
				d[j] = b
			}
		}
	}
}

// Unpack writes the chunk back into c.
func (p *PackedChunk) Unpack(c *Chunk) {
	clear(c.Water[:])
	copy(c.Water[:], p.water)
	c.BlockEntities = c.BlockEntities[:0]
	for _, be := range p.blockEntities {
		// Each chunk gets its own data: servers fill chests in it.
		be.Data = maps.Clone(be.Data)
		c.BlockEntities = append(c.BlockEntities, be)
	}
	unpackSubs(&p.subs, c.Blocks[:])
	unpackSubs(&p.biomes, c.Biomes[:])
}
