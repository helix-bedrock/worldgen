package overworld

import (
	"strings"

	"github.com/helix-bedrock/worldgen/1.26.52/rand"
)

// Features BDS registers in code rather than in features/*.json (the
// built-in feature table, sub_4E2BB50): clay, sand and gravel discs under
// water, sugar cane and pumpkin patches. Written from BDS 1.26.52.

// builtinFeatures adds the code-registered features the rules name. A data
// file with the same identifier wins, as in BDS, which only registers a
// built-in when the name is still free.
func (o *Overworld) builtinFeatures(set *featureSet) {
	dirt := []uint32{o.ids.dirt, o.ids.coarseDirt}
	set.addBuiltin("minecraft:clay_feature", &clayFeature{clay: o.blockID("minecraft:clay"), mud: o.blockID("minecraft:mud"), replace: dirt, radius: 4})
	set.addBuiltin("minecraft:sand_feature", &discFeature{block: o.ids.sand, replace: dirt, radius: 7})
	set.addBuiltin("minecraft:gravel_feature", &discFeature{block: o.ids.gravel, replace: dirt, radius: 6})
	set.addBuiltin("minecraft:reeds_feature", &reedsFeature{reeds: o.blockID("minecraft:reeds"), farmland: o.blockID("minecraft:farmland"), frostedIce: o.blockID("minecraft:frosted_ice")})
	set.addBuiltin("minecraft:pumpkin_feature", &pumpkinFeature{pumpkin: o.blockID("minecraft:pumpkin"), count: 96})
	// Block piles of village streets (vtable off_EE54AC0).
	set.addBuiltin("minecraft:hay_pile_feature", &pileFeature{block: o.blockID("minecraft:hay_block"), kind: pileHay})
	set.addBuiltin("minecraft:melon_pile_feature", &pileFeature{block: o.blockID("minecraft:melon_block")})
	set.addBuiltin("minecraft:snow_pile_feature", &pileFeature{block: o.blockID("minecraft:snow")})
	set.addBuiltin("minecraft:ice_pile_feature", &pileFeature{block: o.blockID("minecraft:packed_ice"), kind: pileIce, other: o.blockID("minecraft:blue_ice")})
	set.addBuiltin("minecraft:pumpkin_pile_feature", &pileFeature{block: o.blockID("minecraft:pumpkin"), kind: pilePumpkin, other: o.blockID("minecraft:carved_pumpkin")})
	// Mushroom tries: 1, or 64 in old worlds (sub_4E2BB50 picks by a base
	// game version check, as for the seagrass count).
	soil := []uint32{o.blockID("minecraft:mycelium"), o.blockID("minecraft:podzol")}
	set.addBuiltin("minecraft:brown_mushroom_feature", &mushroomFeature{block: o.blockID("minecraft:brown_mushroom"), count: 1, soil: soil})
	set.addBuiltin("minecraft:red_mushroom_feature", &mushroomFeature{block: o.blockID("minecraft:red_mushroom"), count: 1, soil: soil})
	set.addBuiltin("minecraft:moss_vegetation_feature", o.newMossVegetation(o.blockID))
	set.addBuiltin("minecraft:pale_moss_vegetation_feature", o.newPaleMossVegetation(o.blockID))
	drip := speleothemBlocks{base: o.blockID("minecraft:dripstone_block"), tip: o.blockID("minecraft:pointed_dripstone"),
		replace: []uint32{o.ids.dirt, o.ids.coarseDirt, o.ids.stone, o.blockID("minecraft:tuff"), o.ids.deepslate},
		tips:    []uint32{o.blockID("minecraft:pointed_dripstone"), o.blockID("minecraft:sulfur_spike")}}
	set.addBuiltin("minecraft:pointed_dripstone_feature", &pointedFeature{drip})
	set.addBuiltin("minecraft:dripstone_cluster_feature", &clusterFeature{speleothemBlocks: drip,
		stone: []uint32{o.ids.stone, o.ids.deepslate, o.blockID("minecraft:tuff")}, minHeight: 3, maxHeight: 5,
		wetBase: 0.1, wetScale: 0.3, wetMin: 0.1, wetMax: 1})
	// Sulfur caves use the same classes (sulfur, cinnabar list xmmword_F60D660).
	sulfur := speleothemBlocks{base: o.blockID("minecraft:sulfur"), tip: o.blockID("minecraft:sulfur_spike"),
		replace: []uint32{o.blockID("minecraft:sulfur"), o.blockID("minecraft:cinnabar")}}
	set.addBuiltin("minecraft:sulfur_spike_feature", &pointedFeature{sulfur})
	set.addBuiltin("minecraft:sulfur_pool", &lakeFeature{liquid: o.ids.water, border: sulfur.base, avoid: []string{"minecraft:sulfur_spike"}})
	set.addBuiltin("minecraft:sulfur_spike_cluster_feature", &clusterFeature{speleothemBlocks: sulfur,
		stone: []uint32{o.ids.stone, o.ids.deepslate, o.blockID("minecraft:tuff")}, minHeight: 1, maxHeight: 4})
	set.addBuiltin("minecraft:large_dripstone_feature", &largeFeature{drip})
	set.addBuiltin("minecraft:dripleaf_feature", o.newDripleaf(o.blockID, set))
	singleVine := &vinesSingleFaceFeature{vine: o.blockID(blockVine), invisibleBedrock: o.blockID("minecraft:invisible_bedrock")}
	set.addBuiltin("minecraft:vines_single_face_feature", singleVine)
	set.addBuiltin("minecraft:vines_feature", &vinesFeature{single: singleVine})
	set.addBuiltin("minecraft:wildflowers_feature", o.newFlowerBed("minecraft:wildflowers"))
	set.addBuiltin("minecraft:leaf_litter_feature", o.newFlowerBed("minecraft:leaf_litter"))
	set.addBuiltin("minecraft:pink_petals_feature", o.newFlowerBed("minecraft:pink_petals"))
	for name, kind := range map[string]flowerKind{"flower_forest": flowersFlowerForest, "forest": flowersForest,
		"overworld": flowersOverworld, "plains": flowersPlains, "swamp": flowersSwamp} {
		set.addBuiltin("minecraft:legacy:"+name+"_flower_feature", o.newLegacyFlower(kind, o.blockID))
	}
	set.addBuiltin("minecraft:meadow_flower_feature", &meadowFlowerFeature{blocks: [8]uint32{o.blockID("minecraft:allium"), o.blockID("minecraft:poppy"),
		o.blockID("minecraft:cornflower"), o.blockID("minecraft:oxeye_daisy"), o.blockID("minecraft:azure_bluet"), o.blockID("minecraft:dandelion"),
		o.blockID(blockShortGrass), o.blockID(blockTallGrass)}})
	set.addBuiltin("minecraft:sweet_berry_bush_feature", o.newSweetBerry())
	set.addBuiltin("minecraft:desert_well_feature", o.newDesertWell())
	set.addBuiltin("minecraft:huge_mushroom_feature", o.newHugeMushroom())
	set.addBuiltin("minecraft:forest_rock_feature", &blockBlobFeature{block: o.blockID("minecraft:mossy_cobblestone")})
	set.addBuiltin("minecraft:legacy:forest_rock_feature", &legacyForestRockFeature{rock: featureRef{"minecraft:forest_rock_feature", set}})
	spike := o.newIceSpike()
	set.addBuiltin("minecraft:ice_spike_feature", spike)
	set.addBuiltin("minecraft:ice_patch_feature", &icePatchFeature{spike: spike, radius: 4})
	set.addBuiltin("minecraft:grass_disc_feature", &grassDiscFeature{grass: o.ids.grass, dirt: o.ids.dirt, mud: o.blockID("minecraft:mud"), radius: 5})
	set.addBuiltin("minecraft:waterlily_feature", &waterlilyFeature{lily: o.blockID("minecraft:waterlily")})
	set.addBuiltin("minecraft:melon_feature", &melonFeature{melon: o.blockID("minecraft:melon_block")})
	set.addBuiltin("minecraft:cactus_feature", &cactusFeature{cactus: o.blockID("minecraft:cactus"), flower: o.blockID("minecraft:cactus_flower"), flowers: true})
	set.addBuiltin("minecraft:bamboo_feature", o.newBamboo())
	set.addBuiltin("minecraft:podzol_area_feature", &podzolAreaFeature{podzol: o.blockID("minecraft:podzol"), dirt: o.ids.dirt})
	// Registered only with the new-world check that also turns on cactus
	// flowers.
	set.addBuiltin("minecraft:firefly_bush_cluster_feature", &fireflyBushFeature{bush: o.blockID("minecraft:firefly_bush")})
	set.addBuiltin("minecraft:firefly_bush_water_cluster_feature", &fireflyBushFeature{bush: o.blockID("minecraft:firefly_bush"), water: true})
	foliage := [3]uint32{o.blockID("minecraft:lilac"), o.blockID("minecraft:rose_bush"), o.blockID("minecraft:peony")}
	set.addBuiltin("minecraft:legacy:flower_forest_foliage_feature", &forestFoliageFeature{fewer: 1, plants: foliage})
	set.addBuiltin("minecraft:legacy:forest_foliage_feature", &forestFoliageFeature{fewer: 3, plants: foliage})
	set.addBuiltin("minecraft:eyeblossom_feature", &eyeblossomFeature{flower: o.blockID("minecraft:closed_eyeblossom")})
	roots := &rootSystemFeature{tree: featureRef{"minecraft:azalea_tree_feature", set}, hanging: o.blockID("minecraft:hanging_roots"),
		roots: o.blockID("minecraft:dirt_with_roots"), replace: map[string]bool{}}
	// The root list (off_F329F10).
	for _, n := range []string{"clay", "moss_block", "sand", "gravel", "dirt", "podzol", "dirt_with_roots", "grass_block", "mycelium",
		"stone", "deepslate", "tuff", "cave_vines", "cave_vines_head_with_berries", "moss_carpet", "short_grass", "fern"} {
		roots.replace["minecraft:"+n] = true
	}
	set.addBuiltin("minecraft:azalea_root_system_feature", roots)
	// The same class leads sulfur caves to a spring on the surface, the
	// trail of sulfur in the column below (xmmword_F60D648).
	sulfurTrail := &rootSystemFeature{tree: featureRef{"minecraft:random_sulfur_spring_with_tuff_feature", set},
		hanging: o.blockID("minecraft:sulfur"), roots: o.blockID("minecraft:sulfur"), replace: map[string]bool{}, space: sulfurSpringSpace}
	for _, n := range []string{"powder_snow", "snow", "sand", "gravel", "clay", "red_sand", "dirt", "mud", "moss_block",
		"grass_block", "stone", "granite", "diorite", "andesite", "tuff", "deepslate", "white_terracotta",
		"orange_terracotta", "magenta_terracotta", "light_blue_terracotta", "yellow_terracotta", "lime_terracotta",
		"pink_terracotta", "gray_terracotta", "light_gray_terracotta", "cyan_terracotta", "purple_terracotta",
		"blue_terracotta", "brown_terracotta", "green_terracotta", "red_terracotta", "black_terracotta"} {
		sulfurTrail.replace["minecraft:"+n] = true
	}
	set.addBuiltin("minecraft:sulfur_spring_trail_to_surface_feature", sulfurTrail)
	set.addBuiltin("minecraft:dead_bush_feature", &deadBushFeature{bush: o.blockID(blockDeadBush)})

	short, top, bottom := o.seagrass("default"), o.seagrass("double_top"), o.seagrass("double_bot")
	// BDS sets the seagrass and kelp counts to 1, or 10 in a mode this
	// world does not use (sub_4E2BB50, v147).
	for name, tall := range map[string]float32{"minecraft:seagrass_feature": 0.6, "minecraft:taller_seagrass_feature": 0.8,
		"minecraft:shorter_seagrass_feature": 0.4, "minecraft:shortest_seagrass_feature": 0.3} {
		set.addBuiltin(name, &seagrassFeature{short: short, top: top, bottom: bottom, count: 1, tall: tall})
	}
	var ages []uint32
	for a := 0; ; a++ {
		b, ok := o.reg.Lookup("minecraft:kelp", map[string]any{"kelp_age": int32(a)})
		if !ok {
			break
		}
		ages = append(ages, b)
	}
	set.addBuiltin("minecraft:kelp_feature", &kelpFeature{ages: ages, count: 1})
	var pf pickleFeature
	for i := range pf.pickles {
		pf.pickles[i], _ = o.reg.Lookup("minecraft:sea_pickle", map[string]any{"cluster_count": int32(i)})
	}
	set.addBuiltin("minecraft:pickle_feature", &pf)

	ib := &icebergFeature{air: o.ids.air, water: o.ids.water, snow: o.blockID("minecraft:snow"), packedIce: o.ids.packedIce,
		blueIce: o.ids.blueIce, ice: o.ids.ice, flowingWater: o.ids.flowingWater, snowLayer: o.blockID(blockSnowLayer)}
	bi := &blueIceFeature{ice: ib}
	set.addBuiltin("minecraft:iceberg_feature", ib)
	set.addBuiltin("minecraft:blue_ice_feature", bi)
	set.addBuiltin("minecraft:legacy:ice_feature", &legacyIceFeature{iceberg: ib, blueIce: bi})

	cf := &coralFeature{stone: o.ids.stone}
	for i, c := range []string{"tube", "brain", "bubble", "fire", "horn"} {
		cf.blocks[i] = o.blockID("minecraft:" + c + "_coral_block")
		cf.blocks[i+5] = o.blockID("minecraft:dead_" + c + "_coral_block")
		for d := range 2 {
			cf.fans[i][d], _ = o.reg.Lookup("minecraft:"+c+"_coral_fan", map[string]any{"coral_fan_direction": int32(d)})
		}
		for d := range 4 {
			cf.wallFans[i][d], _ = o.reg.Lookup("minecraft:"+c+"_coral_wall_fan", map[string]any{"coral_direction": int32(d)})
		}
	}
	set.addBuiltin("minecraft:coral_feature", cf)
	set.addBuiltin("minecraft:coral_hang_feature", &coralHangFeature{coral: cf})
	set.addBuiltin("minecraft:coral_crust_feature", newCoralCrust(cf))
	sa := &seaAnemoneFeature{coral: cf}
	for i, c := range []string{"tube", "brain", "bubble", "fire", "horn"} {
		sa.plants[i] = o.blockID("minecraft:" + c + "_coral")
	}
	set.addBuiltin("minecraft:sea_anemone_feature", sa)
	rock := map[string]bool{}
	for _, n := range []string{"stone", "deepslate", "tuff", "calcite", "snow", "powder_snow", "packed_ice", "dirt", "coarse_dirt"} {
		rock["minecraft:"+n] = true
	}
	waterSpring := &springFeature{liquid: o.ids.water, rock: rock}
	lavaSpring := &springFeature{liquid: o.ids.lava, rock: rock}
	set.addBuiltin("minecraft:water_spring_feature", waterSpring)
	set.addBuiltin("minecraft:lava_spring_feature", lavaSpring)
	set.addBuiltin("minecraft:legacy:springs_feature", &legacySpringsFeature{water: waterSpring, lava: lavaSpring})
}

// isWater reports whether a block is water.
func (o *Overworld) isWater(b uint32) bool {
	return b == o.ids.water || b == o.ids.flowingWater
}

// airOrWater reports whether a block is air or water.
func (o *Overworld) airOrWater(b uint32) bool { return b == o.ids.air || o.isWater(b) }

// waterMaterial reports whether a block's material is water (BDS material
// type 5): water, and the underwater plants kelp and seagrass.
func (o *Overworld) waterMaterial(b uint32) bool {
	if o.isWater(b) {
		return true
	}
	switch o.blockName(b) {
	case "minecraft:kelp", blockSeagrass:
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
// below (SeagrassBlock::mayPlace, sub_CF4DAB0): not magma or soul sand, and
// a top face that can hold it, so not a fence, a bottom slab or lower
// stairs (shipwrecks put those under water).
// VERIFY: BDS asks each block class for a sturdy top face; this takes the
// states that are solid or whose collision covers the top face.
func (o *Overworld) supportsWaterPlant(b uint32) bool {
	name := o.blockName(b)
	switch name {
	case "minecraft:magma", "minecraft:soul_sand":
		return false
	}
	// No trapdoor holds it, not even one in the top half (lead, in game).
	// VERIFY: against BDS's exclusion list xmmword_F613C98.
	if strings.HasSuffix(name, "trapdoor") {
		return false
	}
	return o.solidMaterial(b) && (int(b) >= len(o.topSupport) || o.topSupport[b])
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
				if w.waterAt(px, py+1, pz) && (!cold || w.waterAt(px, py+2, pz)) {
					w.set(px, py, pz, f.bottom)
					w.set(px, py+1, pz, f.top)
				}
			} else if !cold || w.waterAt(px, py+1, pz) {
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

// grassDiscFeature is BDS's DiscFeature (place sub_49A0E50), registered as
// minecraft:grass_disc_feature: a disc of grass blocks, radius 2 to
// radius-1, through the dirt-tagged blocks two above to two below the
// origin; grass with something on top becomes dirt.
// VERIFY: the third accepted block (qword_F619630) is taken as mud.
type grassDiscFeature struct {
	grass, dirt, mud uint32
	radius           int32
}

func (f *grassDiscFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	n := r.IntN(f.radius-2) + 2
	for bx := x - n; bx <= x+n; bx++ {
		for bz := z - n; bz <= z+n; bz++ {
			if (bx-x)*(bx-x)+(bz-z)*(bz-z) > n*n {
				continue
			}
			for by := y - 2; by <= y+2; by++ {
				b, ok := w.block(bx, by, bz)
				if !ok || !(b == f.grass || b == f.mud || w.o.hasBlockTag(b, "dirt")) {
					continue
				}
				put := f.grass
				if above, ok := w.block(bx, by+1, bz); !ok || above != w.o.ids.air {
					put = f.dirt
				}
				w.set(bx, by, bz, put)
			}
		}
	}
	return blockPos{x, y, z}, true
}

// Block pile kinds: the block a pile places varies for hay (its axis),
// packed ice (sometimes blue ice) and pumpkins (sometimes carved).
const (
	pilePlain = iota
	pileHay
	pileIce
	pilePumpkin
)

// pileFeature is BDS's block pile feature (sub_4987920, block choice
// sub_4988060): two layers around the position, each spot within a ragged
// radius (nextFloat*10 - nextFloat*6 against its squared distance), or
// else 3.1% of the time, takes the block where it is air and stands on a
// sturdy top face or a grass path. Nothing is placed within 5 blocks of
// the world bottom.
type pileFeature struct {
	block, other uint32
	kind         int
}

func (f *pileFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	if y < MinY+5 {
		return blockPos{}, false
	}
	a := r.IntN(2)
	b := r.IntN(2)
	for bx := x - a - 2; bx != x+a+3; bx++ {
		for by := y; by <= y+1; by++ {
			for bz := z - b - 2; bz != z+b+3; bz++ {
				dx, dz := x-bx, z-bz
				far := r.Float()*10 - r.Float()*6
				if far < float32(dx*dx+dz*dz) && r.Float() >= 0.031 {
					continue
				}
				f.tryPlace(w, bx, by, bz, r)
			}
		}
	}
	return blockPos{x, y, z}, true
}

// tryPlace puts the pile's block at a spot that is air with a sturdy top
// face or a grass path under it.
func (f *pileFeature) tryPlace(w *featureWorld, x, y, z int32, r *rand.MT) {
	below, ok := w.block(x, y-1, z)
	if !ok || !w.o.sturdyFace(below, 1) && w.o.blockName(below) != "minecraft:grass_path" {
		return
	}
	if at, ok := w.block(x, y, z); !ok || at != w.o.ids.air {
		return
	}
	w.set(x, y, z, f.pick(w.o, r))
}

// pick is the block for one spot (sub_4988060).
// VERIFY: hay's nextInt(2) indexes pillar_axis taken as y, x, z.
func (f *pileFeature) pick(o *Overworld, r *rand.MT) uint32 {
	switch f.kind {
	case pileHay:
		return o.withProp(f.block, "pillar_axis", [2]string{"y", "x"}[r.IntN(2)])
	case pileIce:
		if r.IntN(7) == 0 {
			return f.other
		}
	case pilePumpkin:
		if r.Float() >= 0.95 {
			return f.other
		}
	}
	return f.block
}

// addBuiltin registers a code-defined feature unless a data file already
// took its identifier.
func (s *featureSet) addBuiltin(name string, p placer) {
	if _, ok := s.byID[name]; !ok {
		s.byID[name] = p
	}
}

// seagrass is the seagrass state of the given sea_grass_type.
func (o *Overworld) seagrass(typ string) uint32 {
	b, _ := o.reg.Lookup(blockSeagrass, map[string]any{"sea_grass_type": typ})
	return b
}

// waterAt reports whether the block at (x, y, z) has water material.
func (w *featureWorld) waterAt(x, y, z int32) bool {
	b, ok := w.block(x, y, z)
	return ok && w.o.waterMaterial(b)
}
