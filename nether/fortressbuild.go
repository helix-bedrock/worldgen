package nether

import (
	"github.com/helix-bedrock/worldgen/rand"
	"github.com/helix-bedrock/worldgen/structure"
)

// fortressIDs are the blocks fortress pieces place.
type fortressIDs struct {
	bricks, fence, wart, soulSand, lava, spawner uint32
	chest                                        uint32
	stairs                                       [4]uint32  // by weirdo_direction
	fences                                       [16]uint32 // by joined sides, bit i fenceSides[i]
}

func (n *Nether) loadFortressIDs() error {
	ids := &n.nb
	for _, b := range []blockTarget{
		{&ids.bricks, "minecraft:nether_brick"},
		{&ids.fence, "minecraft:nether_brick_fence"},
		{&ids.wart, "minecraft:nether_wart"},
		{&ids.soulSand, blockSoulSand},
		{&ids.lava, blockLava},
		{&ids.spawner, "minecraft:mob_spawner"},
	} {
		id, ok := n.reg.Default(b.name)
		if !ok {
			return errMissing(b.name)
		}
		*b.dst = id
	}
	var ok bool
	if ids.chest, ok = n.reg.Lookup(blockChest, map[string]any{"minecraft:cardinal_direction": "west"}); !ok {
		return errMissing(blockChest)
	}
	for d := range ids.stairs {
		if ids.stairs[d], ok = n.reg.Lookup(blockNetherBrickStairs, map[string]any{"weirdo_direction": float64(d)}); !ok {
			return errMissing(blockNetherBrickStairs)
		}
	}
	return n.loadFences()
}

// nbWeirdo turns a stairs' weirdo_direction by the piece's orientation
// (sub_D254F10's table for blocks with that state, as for strongholds).
var nbWeirdo = [4][4]int32{{0, 1, 3, 2}, {2, 3, 0, 1}, {0, 1, 2, 3}, {2, 3, 1, 0}}

// placeFortresses builds the pieces of the fortresses that reach chunk
// (cx, cz) into c.
func (n *Nether) placeFortresses(cx, cz int32, c *Chunk) {
	cbox := box{cx * 16, 0, cz * 16, cx*16 + 15, Height - 1, cz*16 + 15}
	var b *nbBuild
	for sx := cx - nbReach; sx <= cx+nbReach; sx++ {
		for sz := cz - nbReach; sz <= cz+nbReach; sz++ {
			f := n.fortressAt(sx, sz)
			if f == nil || !f.bb.intersects(cbox) {
				continue
			}
			if b == nil {
				b = &nbBuild{n: n, c: c, cbox: cbox}
				b.r.Seed(populationSeed(n.seed, cx, cz))
			}
			for _, p := range f.pieces {
				if p.bb.intersects(cbox) {
					b.p = p
					b.build()
				}
			}
		}
	}
}

// nbBuild builds fortress pieces into one chunk.
type nbBuild struct {
	n    *Nether
	c    *Chunk
	cbox box
	r    rand.MT
	p    *nbPiece
}

// put is placeBlock: a piece position, inside the chunk only.
func (b *nbBuild) put(s uint32, x, y, z int32) {
	wx, wy, wz := b.p.worldPos(x, y, z)
	if b.cbox.contains(wx, wy, wz) {
		b.c.set(int(wx&15), int(wy), int(wz&15), s)
		b.c.dropEntity(wx, wy, wz)
	}
}

// box is generateBox (sub_D260890): edge on the box's faces, inner inside,
// in y, x, z order.
func (b *nbBuild) box(x0, y0, z0, x1, y1, z1 int32, edge, inner uint32) {
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			for z := z0; z <= z1; z++ {
				s := inner
				if y == y0 || y == y1 || x == x0 || x == x1 || z == z0 || z == z1 {
					s = edge
				}
				b.put(s, x, y, z)
			}
		}
	}
}

// down is fillColumnDown (sub_D255920) with nether bricks: down through air
// and liquid to the first other block, no lower than y 2.
func (b *nbBuild) down(x, y, z int32) {
	wx, wy, wz := b.p.worldPos(x, y, z)
	if !b.cbox.contains(wx, wy, wz) {
		return
	}
	n := b.n
	for wy > 1 {
		switch b.c.Block(int(wx&15), int(wy), int(wz&15)) {
		case n.ids.air, n.ids.lava, n.ids.flowingLava, n.ids.water, n.ids.flowingWater:
			b.c.set(int(wx&15), int(wy), int(wz&15), n.nb.bricks)
			wy--
			continue
		}
		return
	}
}

// downArea fills columns down at y -1 for x in [x0, x1] and z in [z0, z1].
func (b *nbBuild) downArea(x0, z0, x1, z1 int32) {
	for x := x0; x <= x1; x++ {
		for z := z0; z <= z1; z++ {
			b.down(x, -1, z)
		}
	}
}

// stairs is a nether brick stairs facing weirdo_direction d in the piece.
func (b *nbBuild) stairs(d int32) uint32 {
	return b.n.nb.stairs[nbWeirdo[b.p.orient&3][d&3]]
}

// chest is createChest (sub_D254CA0): a chest facing west (BDS leaves
// chests unturned) with the fortress loot table and a seed from the
// chunk's random, unless a chest is already there.
func (b *nbBuild) chest(x, y, z int32) {
	wx, wy, wz := b.p.worldPos(x, y, z)
	if !b.cbox.contains(wx, wy, wz) || b.c.Block(int(wx&15), int(wy), int(wz&15)) == b.n.nb.chest {
		return
	}
	b.c.set(int(wx&15), int(wy), int(wz&15), b.n.nb.chest)
	b.c.dropEntity(wx, wy, wz)
	table := "loot_tables/chests/nether_bridge.json"
	b.c.BlockEntities = append(b.c.BlockEntities, structure.BlockEntity{X: wx, Y: wy, Z: wz, LootTable: table,
		Data: map[string]any{"id": "Chest", "x": wx, "y": wy, "z": wz, "isMovable": uint8(1),
			"LootTable": table, "LootTableSeed": int32(b.r.Uint32() >> 1)}})
}

func (b *nbBuild) build() {
	switch b.p.kind {
	case nbStart, nbBridgeCrossing:
		b.buildBridgeCrossing()
	case nbBridgeStraight:
		b.buildBridgeStraight()
	case nbEndFiller:
		b.buildEndFiller()
	case nbRoomCrossing:
		b.buildRoomCrossing()
	case nbStairsRoom:
		b.buildStairsRoom()
	case nbMonsterThrone:
		b.buildMonsterThrone()
	case nbCastleEntrance:
		b.buildCastleTop()
		b.buildEntrance()
	case nbStalkRoom:
		b.buildCastleTop()
		b.buildStalkRoom()
	case nbCorridor:
		b.buildCorridor()
	case nbCorridorCrossing:
		b.buildCorridorCrossing()
	case nbRightTurn:
		b.buildTurn(false)
	case nbLeftTurn:
		b.buildTurn(true)
	case nbCorridorStairs:
		b.buildCorridorStairs()
	case nbTBalcony:
		b.buildTBalcony()
	}
}

func (b *nbBuild) buildBridgeStraight() {
	nb, air := b.n.nb.bricks, b.n.ids.air
	b.box(0, 3, 0, 4, 4, 18, nb, nb)
	b.box(1, 5, 0, 3, 7, 18, air, air)
	b.box(0, 5, 0, 0, 5, 18, nb, nb)
	b.box(4, 5, 0, 4, 5, 18, nb, nb)
	b.box(0, 2, 0, 4, 2, 5, nb, nb)
	b.box(0, 2, 13, 4, 2, 18, nb, nb)
	b.box(0, 0, 0, 4, 1, 3, nb, nb)
	b.box(0, 0, 15, 4, 1, 18, nb, nb)
	for x := int32(0); x <= 4; x++ {
		for j := int32(0); j <= 2; j++ {
			b.down(x, -1, j)
			b.down(x, -1, 18-j)
		}
	}
	f := b.n.nb.fence
	b.box(0, 1, 1, 0, 4, 1, f, f)
	b.box(0, 3, 4, 0, 4, 4, f, f)
	b.box(0, 3, 14, 0, 4, 14, f, f)
	b.box(0, 1, 17, 0, 4, 17, f, f)
	b.box(4, 1, 1, 4, 4, 1, f, f)
	b.box(4, 3, 4, 4, 4, 4, f, f)
	b.box(4, 3, 14, 4, 4, 14, f, f)
	b.box(4, 1, 17, 4, 4, 17, f, f)
}

func (b *nbBuild) buildBridgeCrossing() {
	nb, air := b.n.nb.bricks, b.n.ids.air
	b.box(7, 3, 0, 11, 4, 18, nb, nb)
	b.box(0, 3, 7, 18, 4, 11, nb, nb)
	b.box(8, 5, 0, 10, 7, 18, air, air)
	b.box(0, 5, 8, 18, 7, 10, air, air)
	b.box(7, 5, 0, 7, 5, 7, nb, nb)
	b.box(7, 5, 11, 7, 5, 18, nb, nb)
	b.box(11, 5, 0, 11, 5, 7, nb, nb)
	b.box(11, 5, 11, 11, 5, 18, nb, nb)
	b.box(0, 5, 7, 7, 5, 7, nb, nb)
	b.box(11, 5, 7, 18, 5, 7, nb, nb)
	b.box(0, 5, 11, 7, 5, 11, nb, nb)
	b.box(11, 5, 11, 18, 5, 11, nb, nb)
	b.box(7, 2, 0, 11, 2, 5, nb, nb)
	b.box(7, 2, 13, 11, 2, 18, nb, nb)
	b.box(7, 0, 0, 11, 1, 3, nb, nb)
	b.box(7, 0, 15, 11, 1, 18, nb, nb)
	for x := int32(7); x <= 11; x++ {
		for j := int32(0); j <= 2; j++ {
			b.down(x, -1, j)
			b.down(x, -1, 18-j)
		}
	}
	b.box(0, 2, 7, 5, 2, 11, nb, nb)
	b.box(13, 2, 7, 18, 2, 11, nb, nb)
	b.box(0, 0, 7, 3, 1, 11, nb, nb)
	b.box(15, 0, 7, 18, 1, 11, nb, nb)
	for j := int32(0); j <= 2; j++ {
		for z := int32(7); z <= 11; z++ {
			b.down(j, -1, z)
			b.down(18-j, -1, z)
		}
	}
}

// buildEndFiller draws the filler's ragged end from its own random: raw
// values, the lengths of the top rows mod 8, the ledge mod 5 and the base
// mod 3.
func (b *nbBuild) buildEndFiller() {
	var r rand.MT
	r.Seed(b.p.seed)
	for x := int32(0); x <= 4; x++ {
		b.fillerRow(&r, x, 3, 8)
		b.fillerRow(&r, x, 4, 8)
	}
	b.fillerRow(&r, 0, 5, 8)
	b.fillerRow(&r, 4, 5, 8)
	for x := int32(0); x <= 4; x++ {
		b.fillerRow(&r, x, 2, 5)
	}
	for x := int32(0); x <= 4; x++ {
		b.fillerRow(&r, x, 0, 3)
		b.fillerRow(&r, x, 1, 3)
	}
}

// fillerRow lays one row of the filler's end at (x, y), a raw draw mod m
// long.
func (b *nbBuild) fillerRow(r *rand.MT, x, y int32, m uint32) {
	nb := b.n.nb.bricks
	b.box(x, y, 0, x, y, int32(r.Uint32()%m), nb, nb)
}

func (b *nbBuild) buildRoomCrossing() {
	nb, air, f := b.n.nb.bricks, b.n.ids.air, b.n.nb.fence
	b.box(0, 0, 0, 6, 1, 6, nb, nb)
	b.box(0, 2, 0, 6, 7, 6, air, air)
	b.box(0, 2, 0, 1, 6, 0, nb, nb)
	b.box(0, 2, 6, 1, 6, 6, nb, nb)
	b.box(5, 2, 0, 6, 6, 0, nb, nb)
	b.box(5, 2, 6, 6, 6, 6, nb, nb)
	b.box(0, 2, 0, 0, 6, 1, nb, nb)
	b.box(0, 2, 5, 0, 6, 6, nb, nb)
	b.box(6, 2, 0, 6, 6, 1, nb, nb)
	b.box(6, 2, 5, 6, 6, 6, nb, nb)
	b.box(2, 6, 0, 4, 6, 0, nb, nb)
	b.box(2, 5, 0, 4, 5, 0, f, f)
	b.box(2, 6, 6, 4, 6, 6, nb, nb)
	b.box(2, 5, 6, 4, 5, 6, f, f)
	b.box(0, 6, 2, 0, 6, 4, nb, nb)
	b.box(0, 5, 2, 0, 5, 4, f, f)
	b.box(6, 6, 2, 6, 6, 4, nb, nb)
	b.box(6, 5, 2, 6, 5, 4, f, f)
	b.downArea(0, 0, 6, 6)
}

func (b *nbBuild) buildStairsRoom() {
	nb, air, f := b.n.nb.bricks, b.n.ids.air, b.n.nb.fence
	b.box(0, 0, 0, 6, 1, 6, nb, nb)
	b.box(0, 2, 0, 6, 10, 6, air, air)
	b.box(0, 2, 0, 1, 8, 0, nb, nb)
	b.box(5, 2, 0, 6, 8, 0, nb, nb)
	b.box(0, 2, 1, 0, 8, 6, nb, nb)
	b.box(6, 2, 1, 6, 8, 6, nb, nb)
	b.box(1, 2, 6, 5, 8, 6, nb, nb)
	b.box(0, 3, 2, 0, 5, 4, f, f)
	b.box(6, 3, 2, 6, 5, 2, f, f)
	b.box(6, 3, 4, 6, 5, 4, f, f)
	b.put(nb, 5, 2, 5)
	b.box(4, 2, 5, 4, 3, 5, nb, nb)
	b.box(3, 2, 5, 3, 4, 5, nb, nb)
	b.box(2, 2, 5, 2, 5, 5, nb, nb)
	b.box(1, 2, 5, 1, 6, 5, nb, nb)
	b.box(1, 7, 1, 5, 7, 4, nb, nb)
	b.box(6, 8, 2, 6, 8, 4, air, air)
	b.box(2, 6, 0, 4, 8, 0, nb, nb)
	b.box(2, 5, 0, 4, 5, 0, f, f)
	b.downArea(0, 0, 6, 6)
}

// buildMonsterThrone adds the blaze spawner at (3, 5, 5).
func (b *nbBuild) buildMonsterThrone() {
	nb, air, f := b.n.nb.bricks, b.n.ids.air, b.n.nb.fence
	b.box(0, 2, 0, 6, 7, 7, air, air)
	b.box(1, 0, 0, 5, 1, 7, nb, nb)
	b.box(1, 2, 1, 5, 2, 7, nb, nb)
	b.box(1, 3, 2, 5, 3, 7, nb, nb)
	b.box(1, 4, 3, 5, 4, 7, nb, nb)
	b.box(1, 2, 0, 1, 4, 2, nb, nb)
	b.box(5, 2, 0, 5, 4, 2, nb, nb)
	b.box(1, 5, 2, 1, 5, 3, nb, nb)
	b.box(5, 5, 2, 5, 5, 3, nb, nb)
	b.box(0, 5, 3, 0, 5, 8, nb, nb)
	b.box(6, 5, 3, 6, 5, 8, nb, nb)
	b.box(1, 5, 8, 5, 5, 8, nb, nb)
	b.put(f, 1, 6, 3)
	b.put(f, 5, 6, 3)
	b.box(0, 6, 3, 0, 6, 8, f, f)
	b.box(6, 6, 3, 6, 6, 8, f, f)
	b.box(1, 6, 8, 5, 7, 8, f, f)
	b.box(2, 8, 8, 4, 8, 8, f, f)
	if wx, wy, wz := b.p.worldPos(3, 5, 5); b.cbox.contains(wx, wy, wz) {
		b.c.set(int(wx&15), int(wy), int(wz&15), b.n.nb.spawner)
		b.c.BlockEntities = append(b.c.BlockEntities, structure.BlockEntity{X: wx, Y: wy, Z: wz,
			Data: map[string]any{"id": "MobSpawner", "x": wx, "y": wy, "z": wz, "isMovable": uint8(1),
				"EntityIdentifier": "minecraft:blaze"}})
	}
	b.downArea(0, 0, 6, 6)
}

// buildCastleTop is the shell the castle entrance and the stalk room share.
func (b *nbBuild) buildCastleTop() {
	nb, air := b.n.nb.bricks, b.n.ids.air
	b.box(0, 3, 0, 12, 4, 12, nb, nb)
	b.box(0, 5, 0, 12, 13, 12, air, air)
	b.box(0, 5, 0, 1, 12, 12, nb, nb)
	b.box(11, 5, 0, 12, 12, 12, nb, nb)
	b.box(2, 5, 11, 4, 12, 12, nb, nb)
	b.box(8, 5, 11, 10, 12, 12, nb, nb)
	b.box(5, 9, 11, 7, 12, 12, nb, nb)
	b.box(2, 5, 0, 4, 12, 1, nb, nb)
	b.box(8, 5, 0, 10, 12, 1, nb, nb)
	b.box(5, 9, 0, 7, 12, 1, nb, nb)
	b.box(2, 11, 2, 10, 12, 10, nb, nb)
}

// buildCastleSides are the top ring's fences and the side windows.
func (b *nbBuild) buildCastleSides() {
	nb, f := b.n.nb.bricks, b.n.nb.fence
	for i := int32(1); i <= 11; i += 2 {
		b.box(i, 10, 0, i, 11, 0, f, f)
		b.box(i, 10, 12, i, 11, 12, f, f)
		b.box(0, 10, i, 0, 11, i, f, f)
		b.box(12, 10, i, 12, 11, i, f, f)
		b.put(nb, i, 13, 0)
		b.put(nb, i, 13, 12)
		b.put(nb, 0, 13, i)
		b.put(nb, 12, 13, i)
		b.put(f, i+1, 13, 0)
		b.put(f, i+1, 13, 12)
		b.put(f, 0, 13, i+1)
		b.put(f, 12, 13, i+1)
	}
	b.put(f, 0, 13, 0)
	b.put(f, 0, 13, 12)
	b.put(f, 0, 13, 0)
	b.put(f, 12, 13, 0)
	for _, z := range [4]int32{3, 5, 7, 9} {
		b.box(1, 7, z, 1, 8, z, f, f)
		b.box(11, 7, z, 11, 8, z, f, f)
	}
}

// buildCastleBase is the cross of floor below both castle rooms and its
// columns down.
func (b *nbBuild) buildCastleBase() {
	nb := b.n.nb.bricks
	b.box(4, 2, 0, 8, 2, 12, nb, nb)
	b.box(0, 2, 4, 12, 2, 8, nb, nb)
	b.box(4, 0, 0, 8, 1, 3, nb, nb)
	b.box(4, 0, 9, 8, 1, 12, nb, nb)
	b.box(0, 0, 4, 3, 1, 8, nb, nb)
	b.box(9, 0, 4, 12, 1, 8, nb, nb)
	for x := int32(4); x <= 8; x++ {
		for j := int32(0); j <= 2; j++ {
			b.down(x, -1, j)
			b.down(x, -1, 12-j)
		}
	}
	for j := int32(0); j <= 2; j++ {
		for z := int32(4); z <= 8; z++ {
			b.down(j, -1, z)
			b.down(12-j, -1, z)
		}
	}
}

// buildEntrance is the castle entrance with its lava well in the middle.
func (b *nbBuild) buildEntrance() {
	b.box(5, 8, 0, 7, 8, 0, b.n.nb.fence, b.n.nb.fence)
	b.buildCastleSides()
	b.buildCastleBase()
	nb, air := b.n.nb.bricks, b.n.ids.air
	b.box(5, 5, 5, 7, 5, 7, nb, nb)
	b.box(6, 1, 6, 6, 4, 6, air, air)
	b.put(nb, 6, 0, 6)
	b.put(b.n.nb.lava, 6, 5, 6)
}

// buildStalkRoom is the castle room with nether wart on soul sand.
func (b *nbBuild) buildStalkRoom() {
	b.buildCastleSides()
	nb, air, f := b.n.nb.bricks, b.n.ids.air, b.n.nb.fence
	north := b.stairs(3)
	for i := int32(0); i <= 6; i++ {
		j := i + 4
		for x := int32(5); x <= 7; x++ {
			b.put(north, x, i+5, j)
		}
		switch {
		case i >= 1 && i <= 4:
			b.box(5, 5, j, 7, i+4, j, nb, nb)
			b.box(5, i+6, j, 7, i+9, j, air, air)
		case i >= 5:
			b.box(5, 8, j, 7, i+4, j, nb, nb)
			b.box(5, i+6, j, 7, i+9, j, air, air)
		}
	}
	for x := int32(5); x <= 7; x++ {
		b.put(north, x, 12, 11)
	}
	b.box(5, 6, 7, 5, 7, 7, f, f)
	b.box(7, 6, 7, 7, 7, 7, f, f)
	b.box(5, 13, 12, 7, 13, 12, air, air)
	b.box(2, 5, 2, 3, 5, 3, nb, nb)
	b.box(2, 5, 9, 3, 5, 10, nb, nb)
	b.box(2, 5, 4, 2, 5, 8, nb, nb)
	b.box(9, 5, 2, 10, 5, 3, nb, nb)
	b.box(9, 5, 9, 10, 5, 10, nb, nb)
	b.box(10, 5, 4, 10, 5, 8, nb, nb)
	west, east := b.stairs(1), b.stairs(0)
	for _, z := range [4]int32{2, 3, 9, 10} {
		b.put(west, 4, 5, z)
	}
	for _, z := range [4]int32{2, 3, 9, 10} {
		b.put(east, 8, 5, z)
	}
	soul, wart := b.n.nb.soulSand, b.n.nb.wart
	b.box(3, 4, 4, 4, 4, 8, soul, soul)
	b.box(8, 4, 4, 9, 4, 8, soul, soul)
	b.box(3, 5, 4, 4, 5, 8, wart, wart)
	b.box(8, 5, 4, 9, 5, 8, wart, wart)
	b.buildCastleBase()
}

func (b *nbBuild) buildCorridor() {
	nb, air, f := b.n.nb.bricks, b.n.ids.air, b.n.nb.fence
	b.box(0, 0, 0, 4, 1, 4, nb, nb)
	b.box(0, 2, 0, 4, 5, 4, air, air)
	b.box(0, 2, 0, 0, 5, 4, nb, nb)
	b.box(4, 2, 0, 4, 5, 4, nb, nb)
	b.box(0, 3, 1, 0, 4, 1, f, f)
	b.box(0, 3, 3, 0, 4, 3, f, f)
	b.box(4, 3, 1, 4, 4, 1, f, f)
	b.box(4, 3, 3, 4, 4, 3, f, f)
	b.box(0, 6, 0, 4, 6, 4, nb, nb)
	b.downArea(0, 0, 4, 4)
}

func (b *nbBuild) buildCorridorCrossing() {
	nb, air := b.n.nb.bricks, b.n.ids.air
	b.box(0, 0, 0, 4, 1, 4, nb, nb)
	b.box(0, 2, 0, 4, 5, 4, air, air)
	b.box(0, 2, 0, 0, 5, 0, nb, nb)
	b.box(4, 2, 0, 4, 5, 0, nb, nb)
	b.box(0, 2, 4, 0, 5, 4, nb, nb)
	b.box(4, 2, 4, 4, 5, 4, nb, nb)
	b.box(0, 6, 0, 4, 6, 4, nb, nb)
	b.downArea(0, 0, 4, 4)
}

// buildTurn is the right turn, or with left the left turn: the wall it
// turns away from, and a chest by the far wall.
func (b *nbBuild) buildTurn(left bool) {
	nb, air, f := b.n.nb.bricks, b.n.ids.air, b.n.nb.fence
	b.box(0, 0, 0, 4, 1, 4, nb, nb)
	b.box(0, 2, 0, 4, 5, 4, air, air)
	if left {
		b.box(4, 2, 0, 4, 5, 4, nb, nb)
		b.box(4, 3, 1, 4, 4, 1, f, f)
		b.box(4, 3, 3, 4, 4, 3, f, f)
		b.box(0, 2, 0, 0, 5, 0, nb, nb)
		b.box(0, 2, 4, 3, 5, 4, nb, nb)
	} else {
		b.box(0, 2, 0, 0, 5, 4, nb, nb)
		b.box(0, 3, 1, 0, 4, 1, f, f)
		b.box(0, 3, 3, 0, 4, 3, f, f)
		b.box(4, 2, 0, 4, 5, 0, nb, nb)
		b.box(1, 2, 4, 4, 5, 4, nb, nb)
	}
	b.box(1, 3, 4, 1, 4, 4, f, nb)
	b.box(3, 3, 4, 3, 4, 4, f, nb)
	if b.p.chest {
		if left {
			b.chest(3, 2, 3)
		} else {
			b.chest(1, 2, 3)
		}
	}
	b.box(0, 6, 0, 4, 6, 4, nb, nb)
	b.downArea(0, 0, 4, 4)
}

func (b *nbBuild) buildCorridorStairs() {
	nb, air, f := b.n.nb.bricks, b.n.ids.air, b.n.nb.fence
	south := b.stairs(2)
	for i := int32(0); i <= 9; i++ {
		j := max(1, 7-i)
		k := min(max(j+5, 14-i), 13)
		b.box(0, 0, i, 4, j, i, nb, nb)
		b.box(1, j+1, i, 3, k-1, i, air, air)
		if i <= 6 {
			b.put(south, 1, j+1, i)
			b.put(south, 2, j+1, i)
			b.put(south, 3, j+1, i)
		}
		b.box(0, k, i, 4, k, i, nb, nb)
		b.box(0, j+1, i, 0, k-1, i, nb, nb)
		b.box(4, j+1, i, 4, k-1, i, nb, nb)
		if i&1 == 0 {
			b.box(0, j+2, i, 0, j+3, i, f, f)
			b.box(4, j+2, i, 4, j+3, i, f, f)
		}
		for x := int32(0); x <= 4; x++ {
			b.down(x, -1, i)
		}
	}
}

func (b *nbBuild) buildTBalcony() {
	nb, air, f := b.n.nb.bricks, b.n.ids.air, b.n.nb.fence
	b.box(0, 0, 0, 8, 1, 8, nb, nb)
	b.box(0, 2, 0, 8, 5, 8, air, air)
	b.box(0, 6, 0, 8, 6, 5, nb, nb)
	b.box(0, 2, 0, 2, 5, 0, nb, nb)
	b.box(6, 2, 0, 8, 5, 0, nb, nb)
	b.box(1, 3, 0, 1, 4, 0, f, f)
	b.box(7, 3, 0, 7, 4, 0, f, f)
	b.box(0, 2, 4, 8, 2, 8, nb, nb)
	b.box(1, 1, 4, 2, 2, 4, air, air)
	b.box(6, 1, 4, 7, 2, 4, air, air)
	b.box(0, 3, 8, 8, 3, 8, f, f)
	b.box(0, 3, 6, 0, 3, 7, f, f)
	b.box(8, 3, 6, 8, 3, 7, f, f)
	b.box(0, 3, 4, 0, 5, 5, nb, nb)
	b.box(8, 3, 4, 8, 5, 5, nb, nb)
	b.box(1, 3, 5, 2, 5, 5, nb, nb)
	b.box(6, 3, 5, 7, 5, 5, nb, nb)
	b.box(1, 4, 5, 1, 5, 5, f, f)
	b.box(7, 4, 5, 7, 5, 5, f, f)
	for z := int32(0); z <= 5; z++ {
		for x := int32(0); x <= 8; x++ {
			b.down(x, -1, z)
		}
	}
}
