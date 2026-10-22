package nether

import (
	"bytes"
	"fmt"

	"github.com/helix-bedrock/worldgen"
	"github.com/helix-bedrock/worldgen/rand"
	"github.com/helix-bedrock/worldgen/structure"
)

// netherFossilCount is the number of templates, nether_fossils/fossil_1 to
// fossil_14, in BDS's order.
const netherFossilCount = 14

// fossilBlock is one bone block of a template: its position and axis.
type fossilBlock struct {
	pos  [3]int32
	axis byte // 'x', 'y' or 'z'
}

type fossilTemplate struct {
	size   [3]int32
	blocks []fossilBlock
}

// fossilIDs are the Nether fossil's templates and blocks.
type fossilIDs struct {
	templates [netherFossilCount]fossilTemplate
	// bone is the bone block by axis: x, y, z.
	bone [3]uint32
	// ghast is the dried ghast by direction (south, west, north, east).
	ghast [4]uint32
}

// loadFossils reads the Nether fossil templates. They hold only bone blocks
// and air, and BDS ignores the air.
func (n *Nether) loadFossils() error {
	f := &n.fossil
	for i := range f.templates {
		path := fmt.Sprintf("data/vanilla/structures/nether_fossils/fossil_%d.nbt", i+1)
		raw, err := worldgen.Data.ReadFile(path)
		if err != nil {
			return fmt.Errorf("worldgen: read %s: %w", path, err)
		}
		tpl, err := structure.LoadTemplate(bytes.NewReader(raw))
		if err != nil {
			return fmt.Errorf("worldgen: %s: %w", path, err)
		}
		t := &f.templates[i]
		t.size = tpl.Size
		for _, b := range tpl.Blocks {
			if b.State < 0 || int(b.State) >= len(tpl.Palette) {
				return fmt.Errorf("worldgen: %s: palette index %d out of range", path, b.State)
			}
			st := tpl.Palette[b.State]
			switch st.Name {
			case blockAir:
				continue
			case blockBoneBlock:
			default:
				return fmt.Errorf("worldgen: %s: block %s not supported", path, st.Name)
			}
			axis := st.Props["axis"]
			if len(axis) != 1 || axis[0] < 'x' || axis[0] > 'z' {
				return fmt.Errorf("worldgen: %s: bone axis %q", path, axis)
			}
			t.blocks = append(t.blocks, fossilBlock{pos: b.Pos, axis: axis[0]})
		}
	}
	for i, a := range []string{"x", "y", "z"} {
		id, ok := n.reg.Lookup(blockBoneBlock, map[string]any{"pillar_axis": a})
		if !ok {
			return fmt.Errorf("worldgen: bone block with axis %s missing from the block list", a)
		}
		f.bone[i] = id
	}
	for i, d := range []string{"south", "west", "north", "east"} {
		id, ok := n.reg.Lookup("minecraft:dried_ghast", map[string]any{"minecraft:cardinal_direction": d})
		if !ok {
			return fmt.Errorf("worldgen: dried ghast facing %s missing from the block list", d)
		}
		f.ghast[i] = id
	}
	return nil
}

// rotate turns a template offset by rot quarter turns, as BDS's placement
// settings do.
func rotate(x, z int32, rot int32) (int32, int32) {
	switch rot & 3 {
	case 1:
		return -z, x
	case 2:
		return -x, -z
	case 3:
		return z, -x
	}
	return x, z
}

// netherFossil is BDS's NetherFossilFeature (sub_49BD240). From the
// position it searches down for a spot where the template's four corners
// stand on a sturdy top or soul sand with air at the template's
// second-highest layer, giving up at lava, and places the template there,
// clipped to the position's chunk. Half the time a dried ghast then sits
// on soul soil above the bones.
func (n *Nether) netherFossil(r *region, x, y, z int32, mt *rand.MT) {
	f := &n.fossil
	rot := mt.IntN(4)
	t := &f.templates[mt.IntN(netherFossilCount)]
	sx, sy, sz := t.size[0], t.size[1], t.size[2]
	// The corners: the origin, then the template's far z, far x and far
	// corner, turned.
	var corners [4][2]int32
	for i, c := range [4][2]int32{{0, 0}, {0, sz - 1}, {sx - 1, 0}, {sx - 1, sz - 1}} {
		dx, dz := rotate(c[0], c[1], rot)
		corners[i] = [2]int32{x + dx, z + dz}
	}
	air := n.ids.air
	for {
		y--
		if y <= 0 || y+sy+1 >= Height {
			return
		}
		lava := false
		for _, c := range corners {
			if n.isLava(r.at(c[0], y, c[1])) {
				lava = true
			}
		}
		if lava {
			return
		}
		fits := true
		for _, c := range corners {
			if r.at(c[0], y+sy-1, c[1]) != air {
				fits = false
				break
			}
			if b := r.at(c[0], y, c[1]); !n.topSupport[b] && b != n.deco.soulSand {
				fits = false
				break
			}
		}
		if fits {
			break
		}
	}
	cx, cz := x>>4, z>>4
	for _, b := range t.blocks {
		dx, dz := rotate(b.pos[0], b.pos[2], rot)
		px, py, pz := x+dx, y+1+b.pos[1], z+dz
		if px>>4 != cx || pz>>4 != cz || py < 0 || py >= Height {
			continue
		}
		axis := b.axis
		if rot&1 == 1 && axis != 'y' {
			axis ^= 'x' ^ 'z'
		}
		r.set(px, py, pz, f.bone[axis-'x'])
	}
	n.driedGhast(r, corners[0], corners[3], y, sy, mt)
}

// driedGhast is BDS's step after a Nether fossil (sub_49BDFF0): half the
// time, at a random column between the corners, it climbs from above the
// fossil's floor past everything but bone to the first air, and puts a
// dried ghast there when soul soil is below.
func (n *Nether) driedGhast(r *region, c0, c3 [2]int32, y, height int32, mt *rand.MT) {
	if mt.Float() > 0.5 {
		return
	}
	ghast := n.fossil.ghast[mt.IntN(4)]
	x, z := c0[0], c0[1]
	if c3[0] > c0[0] {
		x += mt.IntN(c3[0] - c0[0])
	}
	if c3[1] > c0[1] {
		z += mt.IntN(c3[1] - c0[1])
	}
	gy := y + 1
	found := false
	for range height {
		b := r.at(x, gy, z)
		if b == n.ids.air {
			found = true
			break
		}
		if b == n.fossil.bone[0] || b == n.fossil.bone[1] || b == n.fossil.bone[2] {
			return
		}
		gy++
	}
	if found && r.at(x, gy-1, z) == n.deco.soulSoil {
		r.set(x, gy, z, ghast)
	}
}
