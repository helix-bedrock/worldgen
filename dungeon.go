package worldgen

import (
	"github.com/helix-bedrock/worldgen/rand"
)

// The code-placed start of BDS's decoration (postProcess sub_517CD10): with
// the population random, before any feature rule, a chance of a lava lake
// and then the dungeons (MonsterRoomFeature, sub_51D8680), 4 below y 0 and
// 10 anywhere. Written from BDS 1.26.52. New worlds skip the water lake
// that old (legacy) worlds roll first.

// lakesAndDungeons runs the lava lake and dungeon attempts of origin chunk
// (ox, oz), whose population seed is pop.
func (o *Overworld) lakesAndDungeons(w *featureWorld, ox, oz int32, pop uint32) {
	var r rand.MT
	r.Seed(pop)
	x0, z0 := ox*16, oz*16
	if r.IntN(8) == 0 {
		x := r.IntN(9)
		y := r.IntN(r.IntN(120) + 8)
		z := r.IntN(9)
		// Lava lakes only from three below sea level, and above sea level
		// on one try in ten.
		if y >= SeaLevel-3 && (y <= SeaLevel || r.IntN(10) == 0) {
			o.lake(w, x0+x+3, y, z0+z+3, &r, o.ids.lava, o.ids.stone)
		}
	}
	// Four deep dungeons between y -58 and -1, then ten from y 0 up.
	for range 4 {
		x := r.IntN(16)
		y := r.IntN(-(MinY + 6)) + MinY + 6
		z := r.IntN(16)
		o.monsterRoom(w, x0+8+x, y, z0+8+z, &r)
	}
	for range 10 {
		x := r.IntN(16)
		y := r.IntN(MaxY)
		z := r.IntN(16)
		o.monsterRoom(w, x0+8+x, y, z0+8+z, &r)
	}
}

// liquidMaterial reports whether a block is water or lava.
func (o *Overworld) liquidMaterial(b uint32) bool {
	switch b {
	case o.ids.water, o.ids.flowingWater, o.ids.lava, o.ids.flowingLava:
		return true
	}
	return false
}

// lake is LakeFeature (sub_51CE810): a blob of up to 7 ellipsoids in a
// 16x8x16 box whose corner is 8 blocks west and north of (x, z), its
// bottom 4 layers liquid, the rest air, ringed by border below the
// waterline and on half the spots above it. The box sits 4 below the first
// non-air block at or under y. It fails when the edge would let liquid out
// or touch other liquid above the waterline.
func (o *Overworld) lake(w *featureWorld, x, y, z int32, r *rand.MT, liquid, border uint32) bool {
	if y <= MinY+4 {
		return false
	}
	x, z = x-8, z-8
	for {
		b, ok := w.block(x, y, z)
		if !ok || b != o.ids.air || y <= 0 {
			break
		}
		y--
		if y <= MinY {
			break
		}
	}
	y -= 4

	var cells [16 * 16 * 8]bool
	at := func(i, j, k int32) bool { return cells[(i*16+k)*8+j] }
	for n := r.IntN(4) + 4; n > 0; n-- {
		sx := r.Float()*6 + 3
		sy := r.Float()*4 + 2
		sz := r.Float()*6 + 3
		cx := (16-sx-2)*r.Float() + 1
		hx := sx * 0.5
		cx += hx
		cy := (8-sy-4)*r.Float() + 2
		hy := sy * 0.5
		cy += hy
		cz := (16-sz-2)*r.Float() + 1
		hz := sz * 0.5
		cz += hz
		for i := int32(1); i < 15; i++ {
			dx := (float32(i) - cx) / hx
			for k := int32(1); k < 15; k++ {
				dz := (float32(k) - cz) / hz
				for j := int32(1); j < 7; j++ {
					dy := (float32(j) - cy) / hy
					if dz*dz+(dx*dx+dy*dy) < 1 {
						cells[(i*16+k)*8+j] = true
					}
				}
			}
		}
	}
	edge := func(i, j, k int32) bool {
		if at(i, j, k) {
			return false
		}
		return i < 15 && at(i+1, j, k) || i > 0 && at(i-1, j, k) ||
			k < 15 && at(i, j, k+1) || k > 0 && at(i, j, k-1) ||
			j < 7 && at(i, j+1, k) || j > 0 && at(i, j-1, k)
	}
	for i := int32(0); i < 16; i++ {
		for k := int32(0); k < 16; k++ {
			for j := int32(0); j < 8; j++ {
				if !edge(i, j, k) {
					continue
				}
				b, ok := w.block(x+i, y+j, z+k)
				if !ok {
					return false
				}
				if j >= 4 && o.liquidMaterial(b) {
					return false
				}
				if j < 4 && !o.solidMaterial(b) && b != liquid {
					return false
				}
			}
		}
	}
	for i := int32(0); i < 16; i++ {
		for k := int32(0); k < 16; k++ {
			for j := int32(0); j < 8; j++ {
				if !at(i, j, k) {
					continue
				}
				if j < 4 {
					w.set(x+i, y+j, z+k, liquid)
				} else {
					w.set(x+i, y+j, z+k, o.ids.air)
				}
			}
		}
	}
	// Dirt under the air part that sees the sky turns to grass.
	// VERIFY: BDS tests a block list (xmmword_F613CE0) that was not read;
	// the dirt tag is assumed.
	for i := int32(0); i < 16; i++ {
		for k := int32(0); k < 16; k++ {
			for j := int32(4); j < 8; j++ {
				if !at(i, j, k) {
					continue
				}
				if b, ok := w.block(x+i, y+j-1, z+k); ok && o.hasBlockTag(b, "dirt") && w.seesSky(x+i, y+j, z+k) {
					w.set(x+i, y+j-1, z+k, o.ids.grass)
				}
			}
		}
	}
	if border == o.ids.air {
		return true
	}
	for i := int32(0); i < 16; i++ {
		for k := int32(0); k < 16; k++ {
			for j := int32(0); j < 8; j++ {
				if !edge(i, j, k) || j >= 4 && r.IntN(2) == 0 {
					continue
				}
				if b, ok := w.block(x+i, y+j, z+k); ok && o.solidMaterial(b) {
					w.set(x+i, y+j, z+k, border)
				}
			}
		}
	}
	return true
}

// seesSky reports whether no fully opaque block is above (x, y, z).
func (w *featureWorld) seesSky(x, y, z int32) bool {
	for yy := int32(MaxY - 1); yy >= y; yy-- {
		if b, ok := w.block(x, yy, z); ok && b != w.o.ids.air && w.o.reg.LightDampening(b) >= 15 {
			return false
		}
	}
	return true
}

// dungeonMobs are the spawner mobs a dungeon picks from with nextInt(4).
var dungeonMobs = [4]string{"minecraft:skeleton", "minecraft:zombie", "minecraft:zombie", "minecraft:spider"}

// monsterRoom is MonsterRoomFeature: a cobblestone room 5 or 7 blocks wide
// inside (walls included 7 or 9) and 4 high around (x, y, z), with a mossy
// floor, up to two chests against a wall and a spawner in the middle. It
// needs a solid floor and ceiling and 1 to 5 doorways in its walls.
// VERIFY: "solid" is BDS's material solid flag; solidMaterial lists it by
// name.
func (o *Overworld) monsterRoom(w *featureWorld, x, y, z int32, r *rand.MT) {
	rx := r.IntN(2) + 2
	rz := r.IntN(2) + 2
	x0, x1, z0, z1 := x-rx-1, x+rx+1, z-rz-1, z+rz+1
	solid := func(px, py, pz int32) bool {
		b, ok := w.block(px, py, pz)
		return ok && o.solidMaterial(b)
	}
	air := func(px, py, pz int32) bool {
		b, ok := w.block(px, py, pz)
		return ok && b == o.ids.air
	}
	doors := 0
	for px := x0; px <= x1; px++ {
		for py := y - 1; py <= y+4; py++ {
			for pz := z0; pz <= z1; pz++ {
				if (py == y-1 || py == y+4) && !solid(px, py, pz) {
					return
				}
				if (px == x0 || px == x1 || pz == z0 || pz == z1) && py == y && air(px, py, pz) && air(px, py+1, pz) {
					doors++
				}
			}
		}
	}
	if doors < 1 || doors > 5 {
		return
	}
	cobble, _ := o.reg.Default("minecraft:cobblestone")
	mossy, _ := o.reg.Default("minecraft:mossy_cobblestone")
	for px := x0; px <= x1; px++ {
		for py := y + 3; py >= y-1; py-- {
			for pz := z0; pz <= z1; pz++ {
				wall := px == x0 || px == x1 || pz == z0 || pz == z1 || py == y-1
				if !wall || py >= MinY && !solid(px, py-1, pz) {
					w.set(px, py, pz, o.ids.air)
					continue
				}
				if !solid(px, py, pz) {
					continue
				}
				if py == y-1 && r.IntN(4) != 0 {
					w.set(px, py, pz, mossy)
				} else {
					w.set(px, py, pz, cobble)
				}
			}
		}
	}
	for range 2 {
		for range 3 {
			dz := r.IntN(2*rz+1) - rz
			dx := r.IntN(2*rx+1) - rx
			cx, cz := x+dx, z+dz
			if !air(cx, y, cz) {
				continue
			}
			n := 0
			for _, d := range [4][2]int32{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
				if solid(cx+d[0], y, cz+d[1]) {
					n++
				}
			}
			if n != 1 {
				continue
			}
			o.dungeonChest(w, cx, y, cz, dx, dz, r)
			break
		}
	}
	if sp, ok := o.reg.Default("minecraft:mob_spawner"); ok {
		w.set(x, y, z, sp)
		mob := dungeonMobs[r.IntN(4)]
		if d := o.blockEntityFor(sp, "", x, y, z); d != nil {
			d["EntityIdentifier"] = mob
			w.setEntity(BlockEntity{X: x, Y: y, Z: z, Data: d})
		}
	}
}

// dungeonChest places a dungeon chest at (x, y, z), offset (dx, dz) from
// the room's centre, facing away from the wall it stands against. Its loot
// seed is the next number of the room's random, as BDS draws it.
func (o *Overworld) dungeonChest(w *featureWorld, x, y, z, dx, dz int32, r *rand.MT) {
	notAir := func(px, pz int32) bool {
		b, ok := w.block(px, y, pz)
		return ok && b != o.ids.air
	}
	// BDS's facing choice, as minecraft:cardinal_direction indexes.
	facing := func() int {
		if dz > 0 && notAir(x, z+1) {
			return 2
		}
		if dz < 0 && notAir(x, z-1) {
			return 0
		}
		if dx < 0 && notAir(x-1, z) {
			return 3
		}
		if dx > 0 && notAir(x+1, z) {
			return 1
		}
		switch {
		case dz != 0:
			return 2 * int(b2i(dz > 0))
		case dx < 0:
			return 3
		case dx > 0:
			return 1
		}
		return 2
	}()
	id, ok := o.reg.Lookup("minecraft:chest", map[string]any{"minecraft:cardinal_direction": cardinalNames[facing]})
	if !ok {
		return
	}
	w.set(x, y, z, id)
	seed := int32(r.Uint32() >> 1)
	if d := o.blockEntityFor(id, "", x, y, z); d != nil {
		be := BlockEntity{X: x, Y: y, Z: z, Data: d, LootTable: "loot_tables/chests/simple_dungeon.json"}
		d["LootTable"] = be.LootTable
		d["LootTableSeed"] = seed
		w.setEntity(be)
	}
}
