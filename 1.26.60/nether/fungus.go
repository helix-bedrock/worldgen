package nether

import (
	"fmt"

	"github.com/helix-bedrock/worldgen/1.26.60/internal/mathx"

	"github.com/helix-bedrock/worldgen/1.26.60/rand"
)

// fungusKind is one HugeFungusFeature registration (crimson or warped).
type fungusKind struct {
	stem, hat, decor uint32
	// vines: the crimson fungus hangs weeping vines from its hat.
	vines bool
}

// fungusIDs are the blocks the huge fungi look for or place.
type fungusIDs struct {
	crimsonNylium, warpedNylium, crimsonFungus, warpedFungus uint32
	// weepingVines and twistingVines hold the vines by age.
	weepingVines, twistingVines [26]uint32
	netherWart, warpedWart      uint32
	// dirt are the other blocks a fungus may grow on (BDS's list at
	// xmmword_F60E180).
	dirt []uint32
	// solid is BDS's Block+113 flag, replaceable its can-be-replaced
	// test (BlockLegacy vtable +672), by state.
	solid, replaceable []bool
}

// look is the state of block name with props.
func (n *Nether) look(name string, props map[string]any) (uint32, error) {
	id, ok := n.reg.Lookup(name, props)
	if !ok {
		return 0, fmt.Errorf("worldgen: block %s %v missing from the block list", name, props)
	}
	return id, nil
}

// fungusKind loads the blocks of a huge fungus with the stem and hat
// blocks.
func (n *Nether) fungusKind(stem, hat string, vines bool) (k fungusKind, err error) {
	if k.stem, err = n.look(stem, map[string]any{"pillar_axis": "y"}); err != nil {
		return k, err
	}
	if k.hat, err = n.look(hat, nil); err != nil {
		return k, err
	}
	k.decor, err = n.look("minecraft:shroomlight", nil)
	k.vines = vines
	return k, err
}

// replaceableBlocks returns the blocks a huge fungus may replace; none of
// them is solid.
func replaceableBlocks() []string {
	return []string{
		blockAir, blockLava, blockFlowingLava, blockWater, blockFlowingWater, blockFire, blockSoulFire,
		blockCrimsonRoots, blockWarpedRoots, blockNetherSprouts,
	}
}

func (n *Nether) loadFungi() error {
	var err error
	if n.crimson, err = n.fungusKind("minecraft:crimson_stem", blockNetherWart, true); err != nil {
		return err
	}
	if n.warped, err = n.fungusKind("minecraft:warped_stem", blockWarpedWart, false); err != nil {
		return err
	}
	f := &n.fungi
	for _, b := range []blockTarget{
		{&f.crimsonNylium, "minecraft:crimson_nylium"}, {&f.warpedNylium, "minecraft:warped_nylium"},
		{&f.crimsonFungus, blockCrimsonFungus}, {&f.warpedFungus, blockWarpedFungus},
	} {
		if *b.dst, err = n.look(b.name, nil); err != nil {
			return err
		}
	}
	for age := range int32(26) {
		if f.weepingVines[age], err = n.look(blockWeepingVines, map[string]any{"weeping_vines_age": float64(age)}); err != nil {
			return err
		}
		if f.twistingVines[age], err = n.look(blockTwistingVines, map[string]any{"twisting_vines_age": float64(age)}); err != nil {
			return err
		}
	}
	if f.netherWart, err = n.look(blockNetherWart, nil); err != nil {
		return err
	}
	if f.warpedWart, err = n.look(blockWarpedWart, nil); err != nil {
		return err
	}
	for _, name := range []string{blockDirt, blockGrass, "minecraft:podzol", blockCoarseDirt,
		"minecraft:mycelium", "minecraft:dirt_with_roots", "minecraft:moss_block", "minecraft:pale_moss_block",
		"minecraft:mud", "minecraft:muddy_mangrove_roots", "minecraft:farmland"} {
		if id, ok := n.reg.Default(name); ok {
			f.dirt = append(f.dirt, id)
		}
	}
	// VERIFY: both flags are read from BDS per block; these name lists
	// cover the blocks the Nether generates.
	replaceable := map[string]bool{}
	for _, name := range replaceableBlocks() {
		replaceable[name] = true
	}
	nonSolid := map[string]bool{
		blockCrimsonFungus: true, blockWarpedFungus: true, blockWeepingVines: true,
		blockTwistingVines: true, blockBrownMushroom: true, blockRedMushroom: true,
	}
	for name := range replaceable {
		nonSolid[name] = true
	}
	f.solid = make([]bool, n.reg.Len())
	f.replaceable = make([]bool, n.reg.Len())
	for i := range f.solid {
		s, ok := n.reg.State(uint32(i))
		if !ok {
			continue
		}
		f.solid[i] = !nonSolid[s.Name]
		f.replaceable[i] = replaceable[s.Name]
	}
	return nil
}

func (n *Nether) isLava(b uint32) bool { return b == n.ids.lava || b == n.ids.flowingLava }

// growsFungus reports whether a huge fungus may stand on b.
func (n *Nether) growsFungus(b uint32) bool {
	f := &n.fungi
	if b == n.deco.soulSoil || b == f.warpedNylium || b == f.crimsonNylium {
		return true
	}
	for _, d := range f.dirt {
		if b == d {
			return true
		}
	}
	return false
}

// hugeFungus is BDS's HugeFungusFeature (place: sub_D2E7040). From the
// rule's position it walks down (up to 100 blocks) to a block it can grow
// on, then builds a stem, one block or (6%) three wide, and a hat whose
// lower three layers only fill the rim.
func (n *Nether) hugeFungus(r *region, x, y, z int32, mt *rand.MT, k *fungusKind) {
	f := &n.fungi
	height := mt.IntN(10) + 4
	if mt.IntN(12) == 0 {
		height <<= 1
	}
	if y <= 0 || height+y+1 >= Height {
		return
	}
	for tries := 0; ; tries++ {
		if n.isLava(r.at(x, y, z)) || n.isLava(r.at(x, y-1, z)) {
			return
		}
		if n.growsFungus(r.at(x, y-1, z)) {
			break
		}
		if tries == 100 {
			return
		}
		y--
		if y <= 0 || height+y+1 >= Height {
			return
		}
	}
	thick := mt.Float() < 0.06
	t := int32(0)
	if thick {
		t = 1
	}
	for dx := -t; dx <= t; dx++ {
		for dz := -t; dz <= t; dz++ {
			corner := thick && mathx.Abs(dx) == t && mathx.Abs(dz) == t
			for i := range height {
				px, py, pz := x+dx, y+i, z+dz
				b := r.at(px, py, pz)
				if !f.replaceable[b] && b != f.crimsonFungus && b != f.warpedFungus {
					continue
				}
				if !corner || mt.Float() < 0.1 {
					r.set(px, py, pz, k.stem)
				}
			}
		}
	}
	hatHeight := min(mt.IntN(height/3+1)+5, height)
	start := height - hatHeight
	for yy := start; yy <= height; yy++ {
		rad := int32(1)
		if yy < height-mt.IntN(3) {
			rad = 2
		}
		if hatHeight >= 9 && yy < start+4 {
			rad = 3
		}
		R := rad + t
		lower := yy < start+3
		for dx := -R; dx <= R; dx++ {
			for dz := -R; dz <= R; dz++ {
				px, py, pz := x+dx, y+yy, z+dz
				b := r.at(px, py, pz)
				if f.solid[b] || !f.replaceable[b] {
					continue
				}
				edgeX, edgeZ, top := mathx.Abs(dx) == R, mathx.Abs(dz) == R, yy == height
				if lower {
					if !edgeX && !edgeZ && !top {
						continue
					}
					if r.at(px, py-1, pz) == k.hat {
						r.set(px, py, pz, k.hat)
					} else if mt.Float() < 0.15 {
						r.set(px, py, pz, k.hat)
						if k.vines && mt.IntN(11) == 0 {
							n.hangVines(r, px, py-1, pz, mt)
						}
					}
					continue
				}
				v := mt.Float()
				switch {
				case !edgeX && !edgeZ && !top:
					if v < 0.1 {
						r.set(px, py, pz, k.decor)
					} else if mt.Float() < 0.2 {
						r.set(px, py, pz, k.hat)
						if k.vines && mt.IntN(10) == 0 {
							n.hangVines(r, px, py-1, pz, mt)
						}
					}
				case edgeX && edgeZ:
					if v < 0.7 {
						r.set(px, py, pz, k.hat)
						if k.vines && mt.IntN(12) == 0 {
							n.hangVines(r, px, py-1, pz, mt)
						}
					} else if mt.Float() < 0.02 {
						r.set(px, py, pz, k.decor)
					}
				default:
					if v < 0.98 {
						r.set(px, py, pz, k.hat)
						if k.vines && mt.IntN(14) == 0 {
							n.hangVines(r, px, py-1, pz, mt)
						}
					} else if mt.Float() < 0.02 {
						r.set(px, py, pz, k.decor)
					}
				}
			}
		}
	}
}

// hangVines hangs weeping vines from under a hat block at (x, y, z) if it
// is air: usually up to four more below it, now and then twice that, a
// fifth of the time just one.
func (n *Nether) hangVines(r *region, x, y, z int32, mt *rand.MT) {
	if r.at(x, y, z) != n.ids.air {
		return
	}
	length := mt.IntN(5)
	if mt.IntN(6) == 0 {
		length <<= 1
	}
	if mt.IntN(5) == 0 {
		length = 1
	}
	n.weepingColumn(r, x, y, z, length, 23, 25, mt)
}

// weepingColumn places a weeping vine column (sub_CE61DF0) from (x, y, z)
// down: length+1 blocks at most, each of an age in [minAge, maxAge),
// stopping above the first block that is not air.
func (n *Nether) weepingColumn(r *region, x, y, z, length, minAge, maxAge int32, mt *rand.MT) {
	for i := int32(0); i <= length; i, y = i+1, y-1 {
		if r.at(x, y, z) != n.ids.air {
			continue
		}
		age := minAge
		if maxAge > minAge {
			age += mt.IntN(maxAge - minAge)
		}
		r.set(x, y, z, n.fungi.weepingVines[age])
		if i == length || r.at(x, y-1, z) != n.ids.air {
			return
		}
	}
}
