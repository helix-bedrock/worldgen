package worldgen

import (
	"github.com/helix-bedrock/worldgen/rand"
)

// Features BDS registers in code rather than in features/*.json (the
// built-in feature table, sub_4E2BB50): clay, sand and gravel discs under
// water, sugar cane and pumpkin patches. Written from BDS 1.26.52.

// builtinFeatures adds the code-registered features the rules name. A data
// file with the same identifier wins, as in BDS, which only registers a
// built-in when the name is still free.
func (o *Overworld) builtinFeatures(set *featureSet) {
	id := func(name string) uint32 {
		b, _ := o.reg.Default(name)
		return b
	}
	dirt := []uint32{o.ids.dirt, o.ids.coarseDirt}
	add := func(name string, p placer) {
		if _, ok := set.byID[name]; !ok {
			set.byID[name] = p
		}
	}
	add("minecraft:clay_feature", &clayFeature{clay: id("minecraft:clay"), mud: id("minecraft:mud"), replace: dirt, radius: 4})
	add("minecraft:sand_feature", &discFeature{block: o.ids.sand, replace: dirt, radius: 7})
	add("minecraft:gravel_feature", &discFeature{block: o.ids.gravel, replace: dirt, radius: 6})
	add("minecraft:reeds_feature", &reedsFeature{reeds: id("minecraft:reeds"), farmland: id("minecraft:farmland"), frostedIce: id("minecraft:frosted_ice")})
	add("minecraft:pumpkin_feature", &pumpkinFeature{pumpkin: id("minecraft:pumpkin"), count: 96})

	grass := func(typ string) uint32 {
		b, _ := o.reg.Lookup("minecraft:seagrass", map[string]any{"sea_grass_type": typ})
		return b
	}
	short, top, bottom := grass("default"), grass("double_top"), grass("double_bot")
	// BDS sets the seagrass and kelp counts to 1, or 10 in a mode this
	// world does not use (sub_4E2BB50, v147).
	for name, tall := range map[string]float32{"minecraft:seagrass_feature": 0.6, "minecraft:taller_seagrass_feature": 0.8,
		"minecraft:shorter_seagrass_feature": 0.4, "minecraft:shortest_seagrass_feature": 0.3} {
		add(name, &seagrassFeature{short: short, top: top, bottom: bottom, count: 1, tall: tall})
	}
	var ages []uint32
	for a := 0; ; a++ {
		b, ok := o.reg.Lookup("minecraft:kelp", map[string]any{"kelp_age": int32(a)})
		if !ok {
			break
		}
		ages = append(ages, b)
	}
	add("minecraft:kelp_feature", &kelpFeature{ages: ages, count: 1})
	var pf pickleFeature
	for i := range pf.pickles {
		pf.pickles[i], _ = o.reg.Lookup("minecraft:sea_pickle", map[string]any{"cluster_count": int32(i)})
	}
	add("minecraft:pickle_feature", &pf)

	ib := &icebergFeature{air: o.ids.air, water: o.ids.water, snow: id("minecraft:snow"), packedIce: o.ids.packedIce,
		blueIce: o.ids.blueIce, ice: o.ids.ice, flowingWater: o.ids.flowingWater, snowLayer: id("minecraft:snow_layer")}
	bi := &blueIceFeature{ice: ib}
	add("minecraft:iceberg_feature", ib)
	add("minecraft:blue_ice_feature", bi)
	add("minecraft:legacy:ice_feature", &legacyIceFeature{iceberg: ib, blueIce: bi})

	cf := &coralFeature{stone: o.ids.stone}
	for i, c := range []string{"tube", "brain", "bubble", "fire", "horn"} {
		cf.blocks[i] = id("minecraft:" + c + "_coral_block")
		cf.blocks[i+5] = id("minecraft:dead_" + c + "_coral_block")
		for d := range 2 {
			cf.fans[i][d], _ = o.reg.Lookup("minecraft:"+c+"_coral_fan", map[string]any{"coral_fan_direction": int32(d)})
		}
		for d := range 4 {
			cf.wallFans[i][d], _ = o.reg.Lookup("minecraft:"+c+"_coral_wall_fan", map[string]any{"coral_direction": int32(d)})
		}
	}
	add("minecraft:coral_feature", cf)
	add("minecraft:coral_hang_feature", &coralHangFeature{coral: cf})
}

// isWater reports whether a block is water.
func (o *Overworld) isWater(b uint32) bool {
	return b == o.ids.water || b == o.ids.flowingWater
}

// waterMaterial reports whether a block's material is water (BDS material
// type 5): water, and the underwater plants kelp and seagrass.
func (o *Overworld) waterMaterial(b uint32) bool {
	if o.isWater(b) {
		return true
	}
	switch o.blockName(b) {
	case "minecraft:kelp", "minecraft:seagrass":
		return true
	}
	return false
}

// clayFeature is BDS's ClayFeature (place: sub_498FD80): under water, a disc
// of clay three blocks thick replacing dirt, coarse dirt and mud.
type clayFeature struct {
	clay, mud uint32
	replace   []uint32
	radius    int32
}

func (f *clayFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	at, ok := w.block(x, y, z)
	if !ok || !w.o.isWater(at) && at != f.clay {
		return blockPos{}, false
	}
	n := r.IntN(f.radius-2) + 2
	for bx := x - n; bx <= x+n; bx++ {
		for bz := z - n; bz <= z+n; bz++ {
			if (bx-x)*(bx-x)+(bz-z)*(bz-z) > n*n {
				continue
			}
			for by := y - 1; by <= y+1; by++ {
				if b, ok := w.block(bx, by, bz); ok && (inList(f.replace, b) || b == f.mud) {
					w.set(bx, by, bz, f.clay)
				}
			}
		}
	}
	return blockPos{x, y, z}, true
}

// discFeature is BDS's SandFeature (place: sub_49C1620), used for both the
// sand and the gravel discs: under water, a disc five blocks thick
// replacing dirt, coarse dirt and grass.
type discFeature struct {
	block   uint32
	replace []uint32
	radius  int32
}

func (f *discFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	// VERIFY: BDS compares the material at the position with one entry of
	// its material table; taken to be water as for clay.
	at, ok := w.block(x, y, z)
	if !ok || !w.o.isWater(at) {
		return blockPos{}, false
	}
	n := r.IntN(f.radius-2) + 2
	for bx := x - n; bx <= x+n; bx++ {
		for bz := z - n; bz <= z+n; bz++ {
			if (bx-x)*(bx-x)+(bz-z)*(bz-z) > n*n {
				continue
			}
			for by := y - 2; by <= y+2; by++ {
				if b, ok := w.block(bx, by, bz); ok && (inList(f.replace, b) || b == w.o.ids.grass) {
					w.set(bx, by, bz, f.block)
				}
			}
		}
	}
	return blockPos{x, y, z}, true
}

func inList(l []uint32, b uint32) bool {
	for _, v := range l {
		if v == b {
			return true
		}
	}
	return false
}

// reedsFeature is BDS's ReedsFeature (place: sub_49C10D0): 20 tries around
// the position for sugar cane 2 to 4 tall next to water.
type reedsFeature struct {
	reeds, farmland, frostedIce uint32
}

func (f *reedsFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	for range 20 {
		dx := drawDiff(r, 7)
		dz := drawDiff(r, 7)
		px, pz := x+dx, z+dz
		if b, ok := w.block(px, y, pz); !ok || b != w.o.ids.air {
			continue
		}
		wet := false
		for _, d := range [4][2]int32{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
			if b, ok := w.block(px+d[0], y-1, pz+d[1]); ok && w.o.isWater(b) {
				wet = true
				break
			}
		}
		if !wet {
			continue
		}
		h := r.IntN(r.IntN(3)+1) + 2
		for i := range h {
			py := y + i
			if b, ok := w.block(px, py, pz); !ok || b != w.o.ids.air || !f.mayPlace(w, px, py, pz) {
				break
			}
			w.set(px, py, pz, f.reeds)
		}
	}
	return blockPos{x, y, z}, true
}

// mayPlace is sugar cane's placement rule (SugarCaneBlock::mayPlace,
// sub_CF0D200): on sugar cane, or on a dirt or sand block (or farmland)
// with water or frosted ice beside it.
func (f *reedsFeature) mayPlace(w *featureWorld, x, y, z int32) bool {
	below, ok := w.block(x, y-1, z)
	if !ok {
		return false
	}
	if below == f.reeds {
		return true
	}
	o := w.o
	if !o.hasBlockTag(below, "dirt") && below != f.farmland && !o.hasBlockTag(below, "sand") {
		return false
	}
	for _, d := range [4][2]int32{{0, -1}, {1, 0}, {0, 1}, {-1, 0}} {
		if b, ok := w.block(x+d[0], y-1, z+d[1]); ok && (o.hasBlockTag(b, "water") || b == f.frostedIce) {
			return true
		}
	}
	return false
}

// pumpkinFeature is BDS's PumpkinFeature (place: sub_498F7D0): tries at the
// position and around it for pumpkins on grass. BDS rolls a direction and
// a growth stage only for blocks with those states; the pumpkin has
// neither (it uses cardinal_direction), so it is placed in its default
// state with no draws.
type pumpkinFeature struct {
	pumpkin uint32
	count   int32
}

func (f *pumpkinFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	px, py, pz := x, y, z
	for range f.count {
		if b, ok := w.block(px, py, pz); ok && b == w.o.ids.air {
			if below, ok := w.block(px, py-1, pz); ok && below == w.o.ids.grass {
				w.set(px, py, pz, f.pumpkin)
			}
		}
		dx := drawDiff(r, 7)
		dz := drawDiff(r, 7)
		dy := drawDiff(r, 3)
		px, py, pz = x+dx, y+dy, z+dz
	}
	return blockPos{x, y, z}, true
}

// seaFloor walks down from y to the first solid block (BDS's
// isSolidBlockingBlock, sub_C8D6250), stopping at the bottom of the world.
func (w *featureWorld) seaFloor(x, y, z int32) int32 {
	for ; y > MinY; y-- {
		if b, ok := w.block(x, y, z); ok && w.o.solidMaterial(b) {
			break
		}
	}
	return y
}

// supportsWaterPlant is seagrass' and kelp's placement rule for the block
// below (SeagrassBlock::mayPlace, sub_CF4DAB0): a solid block that is not
// magma or soul sand. Fences, also excluded in BDS, never generate under
// water.
func (o *Overworld) supportsWaterPlant(b uint32) bool {
	switch o.blockName(b) {
	case "minecraft:magma", "minecraft:soul_sand":
		return false
	}
	return o.solidMaterial(b)
}

// temperatureAtBlock is the biome temperature at a block, as features see it
// (Biome::getTemperature, sub_C7B8B70).
func (w *featureWorld) temperatureAtBlock(x, y, z int32) float32 {
	c := w.chunk(x, z)
	if c == nil || y < MinY || y >= MaxY {
		return 1
	}
	return w.o.temperature(c.Biome(int(x&15), int(y), int(z&15)), x, y, z)
}

// seagrassFeature is BDS's SeagrassFeature (place: sub_49C2550). The four
// built-in seagrass features differ in count and in the chance of tall
// seagrass.
type seagrassFeature struct {
	short, top, bottom uint32
	count              int32
	tall               float32
}

func (f *seagrassFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	y = w.seaFloor(x, y, z)
	if b, ok := w.block(x, y, z); !ok || !w.o.solidMaterial(b) {
		return blockPos{}, false
	}
	water := func(x, y, z int32) bool {
		b, ok := w.block(x, y, z)
		return ok && w.o.waterMaterial(b)
	}
	cold := w.temperatureAtBlock(x, y, z) < 0.15
	px, pz := x, z
	for range f.count {
		py := y
		for {
			b, ok := w.block(px, py, pz)
			if !ok || w.o.waterMaterial(b) || b == w.o.ids.air {
				break
			}
			py++
		}
		// The climb stops at any water material, but seagrass survives only
		// in real water (SeagrassBlock::mayPlace).
		below, _ := w.block(px, py-1, pz)
		if at, ok := w.block(px, py, pz); ok && w.o.isWater(at) && w.o.supportsWaterPlant(below) {
			if f.tall > r.Float() {
				if water(px, py+1, pz) && (!cold || water(px, py+2, pz)) {
					w.set(px, py, pz, f.bottom)
					w.set(px, py+1, pz, f.top)
				}
			} else if !cold || water(px, py+1, pz) {
				w.set(px, py, pz, f.short)
			}
		}
		dz := drawDiff(r, 7)
		dx := drawDiff(r, 7)
		px, pz = x+dx, z+dz
	}
	return blockPos{x, y, z}, true
}

// kelpFeature is BDS's KelpFeature (place: sub_51CDDE0): kelp columns 3 or
// more blocks tall, ending a few blocks under the water surface.
type kelpFeature struct {
	ages  []uint32 // kelp by kelp_age
	count int32
}

func (f *kelpFeature) isKelp(b uint32) bool {
	return len(f.ages) > 0 && b >= f.ages[0] && b <= f.ages[len(f.ages)-1] && inList(f.ages, b)
}

func (f *kelpFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	o := w.o
	for y > MinY {
		if b, ok := w.block(x, y-1, z); ok && o.solidMaterial(b) {
			break
		}
		if b, _ := w.block(x, y, z); f.isKelp(b) {
			break
		}
		y--
	}
	if b, ok := w.block(x, y-1, z); !ok || !o.solidMaterial(b) {
		return blockPos{}, false
	}
	maxAge := int32(len(f.ages))
	// The checks below ask for water material (BDS reads material type
	// 5), which kelp and seagrass have too.
	wet := o.waterMaterial
	px, pz := x, z
	for range f.count {
		py := y
		for {
			b, ok := w.block(px, py, pz)
			if !ok || wet(b) || b == o.ids.air {
				break
			}
			py++
		}
		if b, _ := w.block(px, py, pz); b == o.ids.air || f.isKelp(b) {
			continue
		}
		if b, _ := w.block(px, py-1, pz); wet(b) || maxAge == 0 {
			continue
		}
		depth := int32(0)
		for ; depth < maxAge; depth++ {
			if b, ok := w.block(px, py+depth, pz); !ok || !wet(b) {
				break
			}
		}
		if depth < 3 {
			continue
		}
		h := int32(3)
		if depth >= 4 {
			h = r.IntN(depth-3) + 3
		}
		age := maxAge - h - 1
		below, _ := w.block(px, py-1, pz)
		if !f.isKelp(below) && o.supportsWaterPlant(below) {
			w.set(px, py, pz, f.ages[max(age, 0)])
			for i := int32(1); i < h; i++ {
				if b, ok := w.block(px, py+i, pz); !ok || !o.isWater(b) {
					break
				}
				w.set(px, py+i, pz, f.ages[min(max(age+i, 0), maxAge-1)])
			}
		}
		dz := drawDiff(r, 7)
		dx := drawDiff(r, 7)
		px, pz = x+dx, z+dz
	}
	return blockPos{x, y, z}, true
}

// pickleFeature is BDS's SeaPickleFeature (place: sub_49C20D0): four tries
// for a sea pickle cluster of 1 to 4 within 6 blocks east and south.
type pickleFeature struct {
	pickles [4]uint32 // by cluster_count
}

func (f *pickleFeature) isPickle(b uint32) bool { return inList(f.pickles[:], b) }

func (f *pickleFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	o := w.o
	for y > MinY {
		b, ok := w.block(x, y, z)
		if !ok || o.solidMaterial(b) || f.isPickle(b) {
			break
		}
		y--
	}
	if b, ok := w.block(x, y, z); !ok || !o.solidMaterial(b) {
		return blockPos{}, false
	}
	for range 4 {
		dz := r.IntN(6)
		dx := r.IntN(6)
		n := r.IntN(4)
		px, py, pz := x+dx, y, z+dz
		for {
			b, ok := w.block(px, py, pz)
			if !ok || o.waterMaterial(b) || b == o.ids.air {
				break
			}
			py++
		}
		b, _ := w.block(px, py, pz)
		if b == o.ids.air || f.isPickle(b) {
			continue
		}
		// VERIFY: the sea pickle's survival rule was not read; it is taken
		// to need a solid block below, like seagrass.
		if below, ok := w.block(px, py-1, pz); !ok || !o.supportsWaterPlant(below) {
			continue
		}
		w.set(px, py, pz, f.pickles[n])
	}
	return blockPos{x, y, z}, true
}

// drawDiff is two raw draws masked with mask, the first minus the second:
// the spread offsets BDS's code features use.
func drawDiff(r *rand.MT, mask uint32) int32 {
	a := int32(r.Uint32() & mask)
	return a - int32(r.Uint32()&mask)
}
