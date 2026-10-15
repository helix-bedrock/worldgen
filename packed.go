package worldgen

// PackedChunk is an undecorated chunk stored compactly for TerrainCache and
// Decorate's neighbour reads. A sub-chunk (16x16x16) of one block, such as
// the air above the terrain, is just that block; any other holds a palette
// and one byte per block, or two bytes in the rare case of more than 256
// states. A typical chunk takes about a fifth of a Chunk's memory, and a
// read is still two array lookups. It is immutable once made, so it may be
// read from many goroutines.
type PackedChunk struct {
	subs   [Height / 16]packedSub
	biomes [4 * 4 * Height / 4]uint16
	// biomeList holds the distinct biomes in BDS's order (see chunkBiomes).
	biomeList []uint32
}

type packedSub struct {
	one     uint16 // the block, when the sub-chunk is a single block
	palette []uint16
	idx     []uint8  // palette index per block; nil if single or wide
	wide    []uint16 // states per block, when the palette is over 256
}

// at returns the state at Chunk index i.
func (p *PackedChunk) at(i int) uint32 {
	s := &p.subs[i>>12]
	if s.idx != nil {
		return uint32(s.palette[s.idx[i&4095]])
	}
	if s.wide != nil {
		return uint32(s.wide[i&4095])
	}
	return uint32(s.one)
}

// Block returns the state index at local (x, z) and world y.
func (p *PackedChunk) Block(x, y, z int) uint32 {
	return p.at((y-MinY)<<8 | z<<4 | x)
}

// Biome returns the biome ID at local (x, z) and world y.
func (p *PackedChunk) Biome(x, y, z int) uint32 {
	return uint32(p.biomes[((y-MinY)>>2)<<4|(z>>2)<<2|x>>2])
}

// Pack returns a compact copy of c. Water bits are not kept: undecorated
// terrain has none.
func Pack(c *Chunk) *PackedChunk {
	p := &PackedChunk{biomeList: chunkBiomes(c)}
	for i, b := range c.Biomes {
		p.biomes[i] = uint16(b)
	}
	var single [Height / 16]bool
	mixed := 0
	for s := range p.subs {
		blocks := c.Blocks[s<<12 : (s+1)<<12]
		single[s] = true
		for _, b := range blocks {
			if b != blocks[0] {
				single[s] = false
				mixed++
				break
			}
		}
	}
	// The index arrays of all sub-chunks share one allocation.
	slab := make([]uint8, mixed<<12)
	var pal [256]uint16
	for s := range p.subs {
		blocks := c.Blocks[s<<12 : (s+1)<<12]
		if single[s] {
			p.subs[s].one = blocks[0]
			continue
		}
		idx := slab[:4096:4096]
		pal[0] = blocks[0]
		n, last, lastIdx := 1, blocks[0], uint8(0)
		ok := true
		for j, b := range blocks {
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
			p.subs[s].wide = append([]uint16(nil), blocks...)
			continue
		}
		slab = slab[4096:]
		p.subs[s].palette = append([]uint16(nil), pal[:n]...)
		p.subs[s].idx = idx
	}
	return p
}

// Unpack writes the chunk back into c.
func (p *PackedChunk) Unpack(c *Chunk) {
	clear(c.Water[:])
	for i, b := range p.biomes {
		c.Biomes[i] = uint32(b)
	}
	for s := range p.subs {
		sub := &p.subs[s]
		dst := c.Blocks[s<<12 : (s+1)<<12]
		switch {
		case sub.idx != nil:
			for j, k := range sub.idx {
				dst[j] = sub.palette[k]
			}
		case sub.wide != nil:
			copy(dst, sub.wide)
		default:
			b := sub.one
			for j := range dst {
				dst[j] = b
			}
		}
	}
}
