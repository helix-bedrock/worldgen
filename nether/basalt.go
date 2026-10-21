package nether

import "github.com/helix-bedrock/worldgen/rand"

// msolid is the solid flag of a block's material (Material+5), which the
// basalt features use to decide which way to search; solid is the block's
// own flag (Block+113).
// VERIFY: both come from the same name list (see loadFungi).
func (n *Nether) msolid(b uint32) bool { return n.fungi.solid[b] }

// deltaFeature is BDS's DeltaFeature (sub_499D080). From the position it
// searches up (inside solid blocks) or down (inside liquid) for an air
// block over solid ground; there, in a diamond of a random size, every
// enclosed air-topped block gets magma when the delta has a rim, and lava
// goes at the rim offset from it.
func (n *Nether) deltaFeature(r *region, x, y, z int32, mt *rand.MT) {
	f := &n.fungi
	step := int32(-1)
	if n.msolid(r.at(x, y, z)) {
		step = 1
	}
	for y >= 2 && y < Height {
		if r.at(x, y, z) == n.ids.air {
			break
		}
		y += step
		if y <= 1 {
			return
		}
	}
	if y < 2 || y >= Height {
		return
	}
	below := r.at(x, y-1, z)
	if below == n.ids.bedrock || below == n.ids.air || below == n.ids.lava {
		return
	}
	v := mt.Float()
	var rimX, rimZ int32
	if v < 0.9 {
		rimX = mt.IntN(3)
		rimZ = mt.IntN(3)
	}
	rim := v < 0.9 && rimX != 0 && rimZ != 0
	sizeX := mt.IntN(4) + 3
	sizeZ := mt.IntN(4) + 3
	reach := max(sizeX, sizeZ)
	// clear is BDS's enclosure test: not lava, air above, and the five
	// other neighbours not air.
	clear := func(px, py, pz int32) bool {
		if r.at(px, py, pz) == n.ids.lava || r.at(px, py+1, pz) != n.ids.air {
			return false
		}
		for _, d := range [5][3]int32{{0, -1, 0}, {0, 0, -1}, {0, 0, 1}, {1, 0, 0}, {-1, 0, 0}} {
			if r.at(px+d[0], py+d[1], pz+d[2]) == n.ids.air {
				return false
			}
		}
		return true
	}
	_ = f
	for dx := -sizeX; dx <= sizeX; dx++ {
		for dz := -sizeZ; dz <= sizeZ; dz++ {
			if abs32(dx)+abs32(dz) > reach {
				continue
			}
			px, pz := x+dx, z+dz
			if !clear(px, y, pz) {
				continue
			}
			if rim {
				r.set(px, y, pz, n.deco.magma)
			}
			if qx, qz := px+rimX, pz+rimZ; clear(qx, y, qz) {
				r.set(qx, y, qz, n.ids.lava)
			}
		}
	}
}

// basaltColumns is BDS's BasaltColumnsFeature (sub_4985E50). From the
// position it searches down to a non-solid block over a solid one, then
// makes 50 (or, a tenth of the time, 15 more spread out) column clusters
// around it; each cluster raises one basalt column from basalt or
// blackstone ground, shorter further from the cluster centre.
func (n *Nether) basaltColumns(r *region, x, y, z int32, mt *rand.MT) {
	for {
		if y <= 0 || y >= Height {
			return
		}
		if !n.msolid(r.at(x, y, z)) && n.fungi.solid[r.at(x, y-1, z)] {
			break
		}
		y--
	}
	tall := mt.Float() >= 0.3
	reach, height := int32(1), mt.IntN(4)+1
	if tall {
		reach, height = 2, mt.IntN(6)+5
	}
	count, spread, off := int32(15), int32(17), int32(-8)
	if mt.Float() < 0.9 {
		count, spread, off = 50, 11, -5
	}
	for range count {
		cx := x + off + mt.IntN(spread)
		cz := z + off + mt.IntN(spread)
		left := height - (abs32(cx-x) + abs32(cz-z))
		if left <= 0 {
			continue
		}
		rad := reach + mt.IntN(reach)
		n.columnCluster(r, cx, y, cz, rad, left)
	}
}

// columnCluster tries the columns within rad of (cx, cz), each searching
// from y toward a free spot over solid ground for at most its distance from
// the centre in steps, and raises basalt at the first one standing on
// basalt or blackstone.
func (n *Nether) columnCluster(r *region, cx, y0, cz, rad, left int32) {
	for ddx := -rad; ddx <= rad; ddx++ {
		for ddz := -rad; ddz <= rad; ddz++ {
			px, pz := cx+ddx, cz+ddz
			k := abs32(ddx) + abs32(ddz)
			step := int32(-1)
			if n.msolid(r.at(px, y0, pz)) {
				step = 1
			}
			if y0 <= 0 {
				continue
			}
			for y, budget := y0, k; y < Height && budget > 0; y, budget = y+step, budget-1 {
				if !n.msolid(r.at(px, y, pz)) && n.fungi.solid[r.at(px, y-1, pz)] {
					if b := r.at(px, y-1, pz); b != n.deco.blackstone && !n.basaltStates[b] {
						break
					}
					for h := left - k/2; h >= 0 && y < Height && r.at(px, y, pz) != n.ids.bedrock; h, y = h-1, y+1 {
						r.set(px, y, pz, n.deco.basalt)
					}
					// BDS ends the cluster after its first column.
					return
				}
				if y+step <= 0 {
					break
				}
			}
		}
	}
}

// basaltPillar is BDS's BasaltPillarFeature (sub_49864B0). In air under
// soul sand, soul soil or netherrack it hangs a basalt column down to the
// ground, with side pieces wherever a side is air under a solid block, then
// spreads a base of basalt over the ground around the column's foot.
func (n *Nether) basaltPillar(r *region, x, y, z int32, mt *rand.MT) {
	air := n.ids.air
	if r.at(x, y, z) != air {
		return
	}
	switch r.at(x, y+1, z) {
	case n.deco.soulSand, n.deco.soulSoil, n.ids.netherrack:
	default:
		return
	}
	// The four sides' chances: west, east, north, south.
	var side [4]float32
	for i := range side {
		side[i] = mt.Float()
	}
	sides := [4][2]int32{{-1, 0}, {1, 0}, {0, -1}, {0, 1}}
	// BDS draws a float it compares with 100, always below, before the
	// column and after each step.
	mt.Float()
	for i := range side {
		side[i] = side[i]*0.07999998 + 0.87
	}
	basalt := n.deco.basalt
	r.set(x, y, z, basalt)
	for {
		for i, d := range sides {
			sx, sz := x+d[0], z+d[1]
			if side[i] > mt.Float() && r.at(sx, y, sz) == air && r.at(sx, y+1, sz) != air {
				r.set(sx, y, sz, basalt)
			}
		}
		y--
		mt.Float()
		if r.at(x, y, z) != air || r.at(x, y+1, z) == air {
			break
		}
		r.set(x, y, z, basalt)
	}
	// y is now the ground under the column: basalt on each side that stands
	// on something.
	for _, d := range sides {
		if r.at(x+d[0], y, z+d[1]) != air {
			r.set(x+d[0], y+1, z+d[1], basalt)
		}
	}
	for i := int32(-3); i <= 3; i++ {
		for j := int32(-3); j <= 3; j++ {
			if mt.IntN(10) < abs32(i*j) {
				continue
			}
			for py, k := y, 0; k < 3; py, k = py-1, k+1 {
				if r.at(x+i, py-1, z+j) != air {
					r.set(x+i, py, z+j, basalt)
					break
				}
			}
		}
	}
}
