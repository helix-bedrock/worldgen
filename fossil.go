package worldgen

import (
	"encoding/json"

	"github.com/helix-bedrock/worldgen/biome"
	"github.com/helix-bedrock/worldgen/rand"
)

// Desert wells and fossils, the two template-like features, written from
// BDS 1.26.52.

// desertWellFeature is DesertWellFeature (sub_499D900): Java 1.20's well,
// with Bedrock's order for the suspicious sand spots.
type desertWellFeature struct {
	sand, redSand, sandstone, slab, water, sus uint32
}

func (o *Overworld) newDesertWell() *desertWellFeature {
	id := func(name string) uint32 {
		b, _ := o.reg.Default("minecraft:" + name)
		return b
	}
	return &desertWellFeature{sand: o.ids.sand, redSand: o.ids.redSand, sandstone: o.ids.sandstone,
		slab: id("sandstone_slab"), water: o.ids.water, sus: o.ids.suspiciousSand}
}

func (f *desertWellFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	air := w.o.ids.air
	isAir := func(x, y, z int32) bool {
		b, ok := w.block(x, y, z)
		return ok && b == air
	}
	for isAir(x, y, z) && y > MinY+2 {
		y--
	}
	// VERIFY: BDS tests a block family list (xmmword_F613D10); taken to
	// be sand and red sand.
	if b, ok := w.block(x, y, z); !ok || (b != f.sand && b != f.redSand) {
		return blockPos{}, false
	}
	for dx := int32(-2); dx <= 2; dx++ {
		for dz := int32(-2); dz <= 2; dz++ {
			if isAir(x+dx, y-1, z+dz) && isAir(x+dx, y-2, z+dz) {
				return blockPos{}, false
			}
		}
	}
	for dy := int32(-2); dy <= 0; dy++ {
		for dx := int32(-2); dx <= 2; dx++ {
			for dz := int32(-2); dz <= 2; dz++ {
				w.set(x+dx, y+dy, z+dz, f.sandstone)
			}
		}
	}
	cross := [5][2]int32{{0, 0}, {-1, 0}, {1, 0}, {0, -1}, {0, 1}}
	for _, c := range cross {
		w.set(x+c[0], y, z+c[1], f.water)
	}
	for _, c := range cross {
		w.set(x+c[0], y-1, z+c[1], f.sand)
	}
	for dx := int32(-2); dx <= 2; dx++ {
		for dz := int32(-2); dz <= 2; dz++ {
			if dx == -2 || dx == 2 || dz == -2 || dz == 2 {
				w.set(x+dx, y+1, z+dz, f.sandstone)
			}
		}
	}
	w.set(x+2, y+1, z, f.slab)
	w.set(x-2, y+1, z, f.slab)
	w.set(x, y+1, z+2, f.slab)
	w.set(x, y+1, z-2, f.slab)
	for dx := int32(-1); dx <= 1; dx++ {
		for dz := int32(-1); dz <= 1; dz++ {
			if dx == 0 && dz == 0 {
				w.set(x, y+4, z, f.sandstone)
			} else {
				w.set(x+dx, y+4, z+dz, f.slab)
			}
		}
	}
	for dy := int32(1); dy <= 3; dy++ {
		w.set(x-1, y+dy, z-1, f.sandstone)
		w.set(x-1, y+dy, z+1, f.sandstone)
		w.set(x+1, y+dy, z-1, f.sandstone)
		w.set(x+1, y+dy, z+1, f.sandstone)
	}
	// Two suspicious sand blocks under the water: the centre or one of its
	// sides (BDS's order: centre, west, east, north, south), one and two
	// blocks down. Both spots are drawn before either is placed.
	// VERIFY: BDS places them only when a base game version check passes
	// (byte_F4A2978); taken to pass for new worlds.
	a := cross[r.IntN(5)]
	b := cross[r.IntN(5)]
	for _, p := range [2][3]int32{{x + a[0], y - 1, z + a[1]}, {x + b[0], y - 2, z + b[1]}} {
		w.set(p[0], p[1], p[2], f.sus)
		if d := w.o.blockEntityFor(f.sus, "", p[0], p[1], p[2]); d != nil {
			table := "loot_tables/entities/desert_well_brushable_block.json"
			d["LootTable"] = table
			d["LootTableSeed"] = int32(r.Uint32() >> 1)
			w.setEntity(BlockEntity{X: p[0], Y: p[1], Z: p[2], Data: d, LootTable: table})
		}
	}
	return blockPos{x, y, z}, true
}

// fossilTemplates are BDS's fossil templates in its order (sub_51C05A0).
var fossilTemplates = [8]string{
	"fossils/fossil_spine_01", "fossils/fossil_spine_02", "fossils/fossil_spine_03", "fossils/fossil_spine_04",
	"fossils/fossil_skull_01", "fossils/fossil_skull_02", "fossils/fossil_skull_03", "fossils/fossil_skull_04",
}

// fossilFeature is FossilFeature (sub_51C0A20): a bone template buried
// 15 to 24 blocks under the lowest surface over its footprint, inside the
// chunk, placed twice from the same integrity seed: bone at 0.9, then at
// 0.1 with bone blocks turned to the ore.
type fossilFeature struct {
	ore      uint32
	maxEmpty int32
}

func (o *Overworld) parseFossil(raw json.RawMessage) (placer, error) {
	var d struct {
		Ore      json.RawMessage `json:"ore_block"`
		MaxEmpty int32           `json:"max_empty_corners"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, err
	}
	var b biome.Block
	if err := json.Unmarshal(d.Ore, &b); err != nil {
		return nil, err
	}
	ore, ok := o.lookupBlock(b)
	if !ok {
		return nil, nil
	}
	return &fossilFeature{ore: ore, maxEmpty: d.MaxEmpty}, nil
}

func (f *fossilFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	o := w.o
	// VERIFY: BDS first asks the world generator for a structure at the
	// position and skips when there is one (vtable 336 of the generator);
	// not done here.
	if w.chunk(x, z) == nil {
		return blockPos{}, false
	}
	rot := int(r.IntN(4))
	jt := o.template(fossilTemplates[r.IntN(8)])
	if jt == nil {
		return blockPos{}, false
	}
	size := jt.tpl.Size
	sx, sz := size[0], size[2]
	if rot&1 == 1 {
		sx, sz = sz, sx
	}
	ox := r.IntN(16 - sx)
	oz := r.IntN(16 - sz)
	for i := int32(0); i < sx; i++ {
		for j := int32(0); j < sz; j++ {
			y = min(y, w.heightmap(x+ox+i, z+oz+j))
		}
	}
	y = max(y-r.IntN(10)-15, MinY+10)
	x0, z0 := x+ox, z+oz
	far := rotatePos([3]int32{size[0] - 1, size[1] - 1, size[2] - 1}, rot)
	x1, yTop, z1 := x0+far[0], y+far[1], z0+far[2]
	empty := int32(0)
	for _, c := range [8][3]int32{{x0, yTop, z0}, {x0, yTop, z1}, {x1, yTop, z0}, {x1, yTop, z1},
		{x0, y, z0}, {x0, y, z1}, {x1, y, z0}, {x1, y, z1}} {
		if b, ok := w.block(c[0], c[1], c[2]); ok && (b == o.ids.air || o.liquidMaterial(b)) {
			empty++
		}
	}
	if empty > f.maxEmpty {
		return blockPos{}, false
	}
	// BDS clips the template to the position's chunk, from y 0 to 319.
	// VERIFY: the fixed heights look like old-world leftovers; read
	// literally, no fossil block is placed below y 0.
	cx, cz := x>>4, z>>4
	clip := box{cx * 16, 0, cz * 16, cx*16 + 15, 319, cz*16 + 15}
	conv := o.templateStates(jt, rot)
	seed := w.featSeed
	for pass := range 2 {
		integrity := float32(0.9)
		if pass == 1 {
			integrity = 0.1
		}
		var ir rand.MT
		ir.Seed(seed)
		for _, tb := range jt.tpl.Blocks {
			st := jt.tpl.Palette[tb.State]
			switch st.Name {
			case "minecraft:air", "minecraft:structure_block", "minecraft:structure_void", "minecraft:jigsaw":
				// Air is the settings' ignored block.
				// VERIFY: ignored before the integrity draw.
				continue
			}
			if float64(integrity) < float64(ir.Uint32())*0x1p-32 {
				continue
			}
			cs := conv[tb.State]
			rp := rotatePos(tb.Pos, rot)
			p := [3]int32{x0 + rp[0], y + rp[1], z0 + rp[2]}
			if !cs.ok || !clip.contains(p) {
				continue
			}
			id := cs.id
			if pass == 1 && st.Name == "minecraft:bone_block" {
				id = f.ore
			}
			w.set(p[0], p[1], p[2], id)
		}
	}
	return blockPos{x0, y, z0}, true
}

// NearestFossil finds the nearest desert or swamp fossil to block (x, z):
// it builds the chunks where the fossil rule fires, nearest first (at most
// tries of them), and returns the first bone block found. It is safe for
// concurrent use but slow: each try builds a decorated chunk.
func (o *Overworld) NearestFossil(x, z, maxChunks int32, tries int) (Scattered, bool) {
	tc := NewTerrainCache(o, 64)
	var out Scattered
	found := false
	o.ruleChunks("minecraft:desert_or_swamp_after_surface_fossil_feature", x, z, maxChunks, func(px, pz int32) bool {
		if tries--; tries < 0 {
			return false
		}
		cx, cz := px>>4, pz>>4
		var c Chunk
		tc.Generate(cx, cz, &c)
		for y := MinY; y < MaxY; y++ {
			for i := range 256 {
				if o.blockName(c.Block(i&15, y, i>>4)) == "minecraft:bone_block" {
					out, found = Scattered{Kind: "fossil", X: cx*16 + int32(i&15), Y: int32(y), Z: cz*16 + int32(i>>4)}, true
					return false
				}
			}
		}
		return true
	})
	return out, found
}

// NearestFeature finds the chunk nearest to block (x, z), within maxChunks
// chunks, where feature rule ruleID passes its scatter chance and the
// chunk's surface biome has the rule. It returns the rule's x and z for
// that chunk (the well or fossil may still fail its own checks there). It
// is safe for concurrent use.
func (o *Overworld) NearestFeature(ruleID string, x, z, maxChunks int32) (Scattered, bool) {
	var out Scattered
	found := false
	o.ruleChunks(ruleID, x, z, maxChunks, func(px, pz int32) bool {
		out, found = Scattered{Kind: ruleID, X: px, Y: SeaLevel, Z: pz}, true
		return false
	})
	return out, found
}

// ruleChunks calls f, nearest ring first, with the rule's x and z in each
// chunk where feature rule ruleID passes its scatter chance and the
// surface biome has the rule, until f returns false.
func (o *Overworld) ruleChunks(ruleID string, x, z, maxChunks int32, f func(x, z int32) bool) {
	var rule *featureRule
	ok := map[uint32]bool{}
	for b, passes := range o.rules.byBiome {
		for _, rs := range passes {
			for _, r := range rs {
				if r.id == ruleID {
					rule = r
					ok[uint32(b)] = true
				}
			}
		}
	}
	if rule == nil || rule.den <= 0 {
		return
	}
	cx0, cz0 := x>>4, z>>4
	var r rand.MT
	for ring := int32(0); ring <= maxChunks; ring++ {
		for cx := cx0 - ring; cx <= cx0+ring; cx++ {
			for cz := cz0 - ring; cz <= cz0+ring; cz++ {
				if max(abs32i(cx-cx0), abs32i(cz-cz0)) != ring {
					continue
				}
				r.Seed(ruleSeed(populationSeed(o.seed, cx, cz), rule.hash))
				if rule.num != rule.den && r.IntN(rule.den) >= rule.num {
					continue
				}
				pos := [3]int32{cx * 16, 0, cz * 16}
				for _, a := range rule.order {
					if rule.axes[a].kind == distUniform {
						pos[a] += rule.axes[a].sample(&r)
					}
				}
				if !ok[o.SurfaceBiomeAt(pos[0], pos[2])] {
					continue
				}
				if !f(pos[0], pos[2]) {
					return
				}
			}
		}
	}
}
