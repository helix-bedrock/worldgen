package worldgen

import (
	"encoding/json"
	"fmt"
	"math"

	"github.com/helix-bedrock/worldgen/biome"
	"github.com/helix-bedrock/worldgen/blocks"
	"github.com/helix-bedrock/worldgen/rand"
)

type oreRule struct {
	place      uint32
	mayReplace []string
}

// oreFeature is minecraft:ore_feature (BDS OreFeature::place, sub_D2F38D0).
type oreFeature struct {
	count   int32
	discard float32
	rules   []oreRule
}

func (o *Overworld) parseOre(raw json.RawMessage) (*oreFeature, string, error) {
	var r struct {
		Description struct {
			Identifier string `json:"identifier"`
		} `json:"description"`
		Count   int32   `json:"count"`
		Discard float32 `json:"discard_chance_on_air_exposure"`
		Rules   []struct {
			Places     biome.Block   `json:"places_block"`
			MayReplace []biome.Block `json:"may_replace"`
		} `json:"replace_rules"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, "", err
	}
	f := &oreFeature{count: r.Count, discard: r.Discard}
	for _, rr := range r.Rules {
		id, ok := o.lookupBlock(rr.Places)
		if !ok {
			return nil, "", fmt.Errorf("ore block %s %v missing from the block list", rr.Places.Name, rr.Places.States)
		}
		rule := oreRule{place: id}
		for _, m := range rr.MayReplace {
			rule.mayReplace = append(rule.mayReplace, upgradedName(m))
		}
		f.rules = append(f.rules, rule)
	}
	if f.count <= 0 || len(f.rules) == 0 {
		return nil, "", nil
	}
	return f, r.Description.Identifier, nil
}

// upgradedBlocks maps old block-with-state names in the vanilla data to the
// split blocks of 1.26, as BDS upgrades them on load.
var upgradedBlocks = map[string]string{
	"minecraft:stone/granite": "minecraft:granite", "minecraft:stone/diorite": "minecraft:diorite",
	"minecraft:stone/andesite": "minecraft:andesite", "minecraft:dirt/coarse": "minecraft:coarse_dirt",
	"minecraft:tallgrass/tall": "minecraft:short_grass", "minecraft:tallgrass/default": "minecraft:short_grass",
	"minecraft:tallgrass/fern": "minecraft:fern",
}

func upgradedName(b biome.Block) string {
	for _, v := range b.States {
		if s, ok := v.(string); ok {
			if n, ok := upgradedBlocks[b.Name+"/"+s]; ok {
				return n
			}
		}
	}
	return b.Name
}

// lookupBlock resolves a data block to a state, upgrading old names.
func (o *Overworld) lookupBlock(b biome.Block) (uint32, bool) {
	if id, ok := o.reg.Lookup(b.Name, b.States); ok {
		return id, true
	}
	if n := upgradedName(b); n != b.Name {
		return o.reg.Default(n)
	}
	return o.reg.Default(flattened(b))
}

// sinTable is the 65536-entry sine table BDS uses for ore angles.
var sinTable = func() (t [65536]float32) {
	for i := range t {
		t[i] = float32(math.Sin(float64(i) * math.Pi * 2 / 65536))
	}
	return
}()

func sinIndex(v float32) float32 { return sinTable[uint16(int32(v))] }

type oreSphere struct{ x, y, z, r2, r float32 }

// place grows a vein of count spheres along a short line from (x+8, y, z+8).
// Draw order: angle, start y, end y, then one radius per sphere; air
// exposure rolls happen while placing.
func (f *oreFeature) place(w *featureWorld, x, y, z int32, r *rand.MT) (blockPos, bool) {
	countF := float32(f.count)
	a := r.Float() * 3.1415927 * 10430.378
	sinA := sinTable[uint16(int32(a))]
	cosA := sinTable[uint16(int32(a+16384))]
	s := countF * 0.125
	sx := sinA * s
	cz := s * cosA
	startZ := (cz + 8) + float32(z)
	startX := (sx + 8) + float32(x)
	startY := (float32(r.IntN(3)) + -2) + float32(y)
	endZ := float32(z) + (8 - cz)
	endX := (8 - sx) + float32(x)
	endY := (float32(r.IntN(3)) + -2) + float32(y)
	dx, dy, dz := endX-startX, endY-startY, endZ-startZ

	inv := 1 / countF
	invPi := inv * 3.1415927
	var sphereBuf [64]oreSphere
	spheres := sphereBuf[:0]
	if int(f.count) > len(sphereBuf) {
		spheres = make([]oreSphere, 0, f.count)
	}
	spheres = spheres[:f.count]
	for i := range spheres {
		fi := float32(i)
		sp := &spheres[i]
		sp.x = inv*(fi*dx) + startX
		sp.y = inv*(fi*dy) + startY
		sp.z = (dz*fi)*inv + startZ
		nf := r.Float()
		sv := sinIndex((fi * invPi) * 10430.378)
		rr := ((sv+1)*((nf*countF)*0.0625) + 1) * 0.5
		sp.r2 = rr * rr
		sp.r = float32(math.Sqrt(float64(sp.r2)))
	}
	// Drop spheres that a later, larger sphere contains. Filtering in place
	// is safe: only spheres after i are read once i is written.
	kept := spheres[:0]
	for i := range spheres {
		si := spheres[i]
		inside := false
		for j := i + 1; j < len(spheres); j++ {
			sj := spheres[j]
			ddz, ddx, ddy := sj.z-si.z, sj.x-si.x, sj.y-si.y
			d := float32(math.Sqrt(float64(ddz*ddz + (ddx*ddx + ddy*ddy))))
			if !(si.r > sj.r) && sj.r > d+si.r {
				inside = true
				break
			}
		}
		if !inside {
			kept = append(kept, si)
		}
	}
	if len(kept) == 0 {
		return blockPos{}, false
	}
	minX, minY, minZ := int32(math.MaxInt32), int32(math.MaxInt32), int32(math.MaxInt32)
	maxX, maxY, maxZ := int32(math.MinInt32), int32(math.MinInt32), int32(math.MinInt32)
	for _, sp := range kept {
		minX = min(minX, int32(math.Floor(float64(sp.x-sp.r))))
		minY = min(minY, int32(math.Floor(float64(sp.y-sp.r))))
		minZ = min(minZ, int32(math.Floor(float64(sp.z-sp.r))))
		maxX = max(maxX, int32(math.Floor(float64(sp.x+sp.r))))
		maxY = max(maxY, int32(math.Floor(float64(sp.y+sp.r))))
		maxZ = max(maxZ, int32(math.Floor(float64(sp.z+sp.r))))
	}
	reg := w.o.reg
	placed := false
	// A sphere can hold a block only if the block's x (then z) distance
	// alone is inside it: float sums of squares never shrink, so this
	// prefilter is exact and the blocks are visited in the same order.
	var slabBuf, colBuf [64]oreSphere
	fx := float32(minX) + 0.5
	for bx := minX; bx <= maxX; bx, fx = bx+1, fx+1 {
		slab := slabBuf[:0]
		for _, sp := range kept {
			if ddx := fx - sp.x; sp.r2 > ddx*ddx {
				slab = append(slab, sp)
			}
		}
		if len(slab) == 0 {
			continue
		}
		fz := float32(minZ) + 0.5
		for bz := minZ; bz <= maxZ; bz, fz = bz+1, fz+1 {
			col := colBuf[:0]
			for _, sp := range slab {
				if ddz := fz - sp.z; sp.r2 > ddz*ddz {
					col = append(col, sp)
				}
			}
			if len(col) == 0 {
				continue
			}
			fy := float32(minY) + 0.5
			for by := minY; by <= maxY; by, fy = by+1, fy+1 {
				in := false
				for _, sp := range col {
					ddz, ddx, ddy := fz-sp.z, fx-sp.x, fy-sp.y
					if sp.r2 > ddz*ddz+(ddx*ddx+ddy*ddy) {
						in = true
						break
					}
				}
				if !in {
					continue
				}
				cur, ok := w.block(bx, by, bz)
				if !ok {
					continue
				}
				if f.replace(w, reg, cur, bx, by, bz, r) {
					placed = true
				}
			}
		}
	}
	return blockPos{x, y, z}, placed
}

// replace applies the first replace rule that matches the block.
func (f *oreFeature) replace(w *featureWorld, reg *blocks.Registry, cur uint32, x, y, z int32, r *rand.MT) bool {
	name := w.o.blockName(cur)
	for _, rule := range f.rules {
		matched := false
		for _, m := range rule.mayReplace {
			if m == name {
				matched = true
				break
			}
		}
		if !matched || cur == rule.place {
			continue
		}
		if c := f.discard; c > 0 {
			check := true
			if c < 1 {
				check = r.Float() < c
			}
			if check && w.airNextTo(x, y, z) {
				continue
			}
		}
		w.set(x, y, z, rule.place)
		return true
	}
	_ = reg
	return false
}

// airNextTo checks the six neighbours in BDS's order for air.
func (w *featureWorld) airNextTo(x, y, z int32) bool {
	air := w.o.ids.air
	for _, d := range [6][3]int32{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}} {
		if b, ok := w.block(x+d[0], y+d[1], z+d[2]); ok && b == air {
			return true
		}
	}
	return false
}
