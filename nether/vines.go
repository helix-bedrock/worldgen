package nether

import "github.com/helix-bedrock/worldgen/rand"

// weepingVinesCluster is BDS's WeepingVinesClusterFeature (sub_4DE7B20):
// nether wart under netherrack or wart, 200 tries to grow the wart along
// the ceiling (an air block next to exactly one of them), then 100 tries to
// hang vines from netherrack or wart.
func (n *Nether) weepingVinesCluster(r *region, x, y, z int32, mt *rand.MT) {
	f := &n.fungi
	nr := n.ids.netherrack
	ceiling := func(b uint32) bool { return b == nr || b == f.netherWart }
	if r.at(x, y, z) != n.ids.air || !ceiling(r.at(x, y+1, z)) {
		return
	}
	r.set(x, y, z, f.netherWart)
	for range 200 {
		px := x + mt.IntN(12) - 6
		py := y + mt.IntN(7) - 5
		pz := z + mt.IntN(12) - 6
		if r.at(px, py, pz) != n.ids.air {
			continue
		}
		count := 0
		for _, d := range [6][3]int32{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}} {
			if ceiling(r.at(px+d[0], py+d[1], pz+d[2])) {
				count++
			}
		}
		if count == 1 {
			r.set(px, py, pz, f.netherWart)
		}
	}
	for range 100 {
		px := x - 8 + mt.IntN(16)
		py := y - 8 + mt.IntN(9)
		pz := z - 8 + mt.IntN(16)
		if r.at(px, py, pz) != n.ids.air || !ceiling(r.at(px, py+1, pz)) {
			continue
		}
		n.weepingColumn(r, px, py, pz, vineLength(mt), 17, 25, mt)
	}
}

// vineLength draws a vine cluster's column length: one to seven, now and
// then doubled, a fifth of the time just one.
func vineLength(mt *rand.MT) int32 {
	l := mt.IntN(7) + 1
	if mt.IntN(6) == 0 {
		l <<= 1
	}
	if mt.IntN(5) == 0 {
		l = 1
	}
	return l
}

// twistingVinesCluster is BDS's TwistingVinesClusterFeature (sub_49C6C50):
// 100 tries around the position, each growing a column of twisting vines up
// from netherrack, warped nylium or warped wart.
func (n *Nether) twistingVinesCluster(r *region, x, y, z int32, mt *rand.MT) {
	f := &n.fungi
	for range 100 {
		// Each horizontal offset is the difference of two raw draws mod 10,
		// shifted by -5 (x first, then z), then y.
		dx := int32(mt.Uint32()%10) - int32(mt.Uint32()%10)
		dz := int32(mt.Uint32()%10) - int32(mt.Uint32()%10)
		dy := mt.IntN(5)
		px, py, pz := x+dx-5, y+dy-4, z+dz-5
		if r.at(px, py, pz) != n.ids.air {
			continue
		}
		if b := r.at(px, py-1, pz); b != n.ids.netherrack && b != f.warpedNylium && b != f.warpedWart {
			continue
		}
		length := vineLength(mt)
		for i := int32(0); i <= length; i, py = i+1, py+1 {
			if r.at(px, py, pz) != n.ids.air {
				continue
			}
			r.set(px, py, pz, f.twistingVines[mt.IntN(25)])
			if i == length || r.at(px, py+1, pz) != n.ids.air {
				break
			}
		}
	}
}
