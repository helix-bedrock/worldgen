package nether

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/helix-bedrock/worldgen"
	"github.com/helix-bedrock/worldgen/biome"
	"github.com/helix-bedrock/worldgen/noise"
	"github.com/helix-bedrock/worldgen/rand"
)

// noBlock marks an unset material.
const noBlock = ^uint32(0)

type builderKind uint8

const (
	builderNone builderKind = iota
	// builderOverworld is the overworld builder, which hell and the two
	// forests use with netherrack as their stone.
	builderOverworld
	// builderCapped is the Nether's capped builder (soul sand valley,
	// basalt deltas).
	builderCapped
)

type surfaceMats struct {
	top, mid, seaFloor, foundation, sea uint32
}

// matAdjustment is one surface_material_adjustments entry: inside its
// height range, where the adjustment noise is in [min, max), its set
// materials replace the builder's.
type matAdjustment struct {
	freq, min, max float32
	yMin, yMax     int32
	mats           surfaceMats
}

// cappedMat is a capped builder material with its own noise.
type cappedMat struct {
	block uint32
	noise *noise.LegacyOctaves
}

type biomeSurface struct {
	kind          builderKind
	mats          surfaceMats
	seaFloorDepth int32
	adjustments   []matAdjustment
	// The capped builder's materials.
	floors, ceilings []cappedMat
	beach            uint32
}

// surfaceNoises are the per-world noises the builders share.
type surfaceNoises struct {
	// adjustment is the overworld builder's material adjustment noise,
	// drawn last from the terrain noises' Mersenne Twister (sub_515FDF0).
	adjustment *noise.SimplexOctaves
	// beach is the capped builder's beach noise.
	beach *noise.LegacyOctaves
}

// surfaceBlock is the state of a surface material, noBlock for none.
func (n *Nether) surfaceBlock(b biome.Block) (uint32, error) {
	if b.Name == "" {
		return noBlock, nil
	}
	id, ok := n.reg.Lookup(b.Name, b.States)
	if !ok {
		id, ok = n.reg.Default(b.Name)
	}
	if !ok {
		return 0, fmt.Errorf("worldgen: surface block %s missing from the block list", b.Name)
	}
	return id, nil
}

// surfaceMats resolves a builder's five materials.
func (n *Nether) surfaceMats(top, mid, seaFloor, foundation, sea biome.Block) (surfaceMats, error) {
	var m surfaceMats
	dst := [5]*uint32{&m.top, &m.mid, &m.seaFloor, &m.foundation, &m.sea}
	for i, b := range [5]biome.Block{top, mid, seaFloor, foundation, sea} {
		id, err := n.surfaceBlock(b)
		if err != nil {
			return m, err
		}
		*dst[i] = id
	}
	return m, nil
}

// list returns the materials in the order top, mid, sea floor, foundation,
// sea.
func (m surfaceMats) list() [5]uint32 {
	return [5]uint32{m.top, m.mid, m.seaFloor, m.foundation, m.sea}
}

// cappedMats appends the capped materials src to dst, each with a
// one-octave noise seeded from *ms, which counts up.
func (n *Nether) cappedMats(src []biome.Block, dst *[]cappedMat, ms *uint32) error {
	for _, m := range src {
		id, err := n.surfaceBlock(m)
		if err != nil {
			return err
		}
		*dst = append(*dst, cappedMat{id, noise.NewLegacyOctaves(rand.NewMT(*ms), 1)})
		*ms++
	}
	return nil
}

// loadSurfaces resolves the Nether biomes' surface builders to block IDs,
// indexed by biome ID.
func (n *Nether) loadSurfaces(biomeIDs map[string]uint32) error {
	defs, err := biome.LoadDefs(worldgen.Data, "data/vanilla/biomes")
	if err != nil {
		return err
	}
	seed := uint32(n.seed)
	// The capped builder's beach noise: one octave from the seed
	// (sub_C752060).
	n.surfNoise.beach = noise.NewLegacyOctaves(rand.NewMT(seed), 1)

	var maxID uint32
	for _, id := range biomeIDs {
		maxID = max(maxID, id)
	}
	n.surfaces = make([]*biomeSurface, maxID+1)
	for _, b := range n.biomes.biomes {
		var d *biome.Def
		for name, id := range biomeIDs {
			if id == b.id {
				d = defs[name]
			}
		}
		if d == nil || d.Surface == nil {
			continue
		}
		s := d.Surface
		bs := &biomeSurface{beach: noBlock}
		switch s.Type {
		case "minecraft:overworld":
			bs.kind = builderOverworld
		case "minecraft:capped":
			bs.kind = builderCapped
		default:
			continue
		}
		if bs.mats, err = n.surfaceMats(s.Top, s.Mid, s.SeaFloor, s.Foundation, s.Sea); err != nil {
			return err
		}
		bs.seaFloorDepth = int32(s.SeaFloorDepth)
		if bs.beach, err = n.surfaceBlock(s.BeachMaterial); err != nil {
			return err
		}
		// Each capped material has a one-octave noise; the floors take
		// seeds seed, seed+1, ... and the ceilings carry on (sub_C7BFB60).
		ms := seed
		if err := n.cappedMats(s.FloorMaterials, &bs.floors, &ms); err != nil {
			return err
		}
		if err := n.cappedMats(s.CeilingMaterials, &bs.ceilings, &ms); err != nil {
			return err
		}
		for _, a := range d.Adjustments {
			m, err := n.surfaceMats(a.Top, a.Mid, a.SeaFloor, a.Foundation, a.Sea)
			if err != nil {
				return err
			}
			adj := matAdjustment{freq: a.FreqScale, min: a.Min, max: a.Max, yMin: 0, yMax: Height - 1, mats: m}
			if a.HeightMin != "" {
				if adj.yMin, err = seaLevelExpr(a.HeightMin); err != nil {
					return err
				}
			}
			if a.HeightMax != "" {
				if adj.yMax, err = seaLevelExpr(a.HeightMax); err != nil {
					return err
				}
			}
			bs.adjustments = append(bs.adjustments, adj)
		}
		n.surfaces[b.id] = bs
	}
	return nil
}

// seaLevelExpr evaluates the Molang height bounds the Nether biomes use:
// "variable.sea_level", optionally plus or minus a whole number.
func seaLevelExpr(e string) (int32, error) {
	e = strings.ReplaceAll(e, " ", "")
	rest, ok := strings.CutPrefix(e, "variable.sea_level")
	if !ok {
		v, err := strconv.Atoi(e)
		if err != nil {
			return 0, fmt.Errorf("nether: height bound %q: %w", e, err)
		}
		return int32(v), nil
	}
	if rest == "" {
		return SeaLevel, nil
	}
	v, err := strconv.Atoi(rest)
	if err != nil {
		return 0, fmt.Errorf("nether: height bound %q: %w", e, err)
	}
	return SeaLevel + int32(v), nil
}

// surface runs the biome surface builders, x outer and z inner, continuing
// the chunk's Mersenne Twister after the bedrock (sub_5163F50).
func (n *Nether) surface(chunkX, chunkZ int32, c *Chunk, mt *rand.MT) {
	var depth [256]float32
	x0, z0 := chunkX*16, chunkZ*16
	n.surface4.SurfaceNoise(&depth, x0, z0, 0.0625)
	for x := range 16 {
		for z := range 16 {
			b := c.Biomes[z<<4|x]
			if int(b) >= len(n.surfaces) || n.surfaces[b] == nil {
				continue
			}
			bs := n.surfaces[b]
			col := column{x: x, z: z, wx: x0 + int32(x), wz: z0 + int32(z), noise: depth[x+16*z]}
			switch bs.kind {
			case builderOverworld:
				n.overworldSurface(c, mt, &col, bs)
			case builderCapped:
				n.cappedSurface(c, mt, &col, bs)
			}
		}
	}
}

type column struct {
	x, z   int
	wx, wz int32
	noise  float32
}

// adjusted is the materials at height y after the adjustments.
func (n *Nether) adjusted(b *biomeSurface, col *column, y int32) surfaceMats {
	m := b.mats
	for _, a := range b.adjustments {
		if y < a.yMin || y > a.yMax {
			continue
		}
		f := a.freq
		v := n.surfNoise.adjustment.Sum3(f*float32(col.wx), 0, f*float32(col.wz)) * n.surfNoise.adjustment.Scale
		if v < a.min || v >= a.max {
			continue
		}
		dst := [5]*uint32{&m.top, &m.mid, &m.seaFloor, &m.foundation, &m.sea}
		for i, v := range a.mats.list() {
			if v != noBlock {
				*dst[i] = v
			}
		}
	}
	return m
}

func (n *Nether) liquid(b uint32) bool { return b == n.ids.lava }

// overworldSurface is the overworld builder over a Nether column: top and
// mid materials over the first netherrack below air, walking the column
// from the top down.
// VERIFY: the builder's replaceable set (netherrack here) and its sea-level
// mode in the Nether.
func (n *Nether) overworldSurface(c *Chunk, mt *rand.MT, col *column, b *biomeSurface) {
	x, z := col.x, col.z
	depth := int32(mt.Float()*0.25 + (float32(float64(col.noise)/3) + 3))
	depthSea := depth + b.seaFloorDepth
	run := int32(-1)
	// The materials carried down start as the adjusted ones at the top of
	// the world.
	cur := n.adjusted(b, col, Height-1)
	prevTop, prevMid := cur.top, cur.mid
	surfaceY := int32(SeaLevel)
	// BDS starts the walk at the chunk's height map, which the Nether
	// generator never fills: what it holds decides whether the roof's
	// underside is capped, and on the comparison world about a fifth of
	// the columns are, in no pattern the terrain explains. The walk here
	// starts at the first air from the top, so the roof is never capped.
	// VERIFY: what that height map holds when a Nether chunk is generated.
	top := int32(Height - 1)
	for top > 0 && c.Block(x, int(top), z) != n.ids.air {
		top--
	}
	for y := top; y > 0; y-- {
		blk := c.Block(x, int(y), z)
		if blk == n.ids.bedrock {
			continue
		}
		if blk == n.ids.air {
			run = -1
			continue
		}
		if blk != n.ids.netherrack {
			continue
		}
		cur = n.adjusted(b, col, y)
		switch {
		case run == -1:
			topB, fill := n.ids.air, cur.foundation
			if depth > 0 {
				topB, fill = prevTop, prevMid
				if y >= surfaceY-4 && y <= surfaceY+1 {
					topB, fill = cur.top, cur.mid
				}
			}
			if y < surfaceY && topB == n.ids.air {
				topB = cur.sea
			}
			switch {
			case y >= surfaceY-1:
				c.set(x, int(y), z, topB)
				prevTop, prevMid = topB, fill
			case y < surfaceY-depthSea || y == 1:
				c.set(x, int(y), z, cur.seaFloor)
				prevTop, prevMid = n.ids.air, cur.foundation
			default:
				c.set(x, int(y), z, fill)
				prevTop, prevMid = topB, fill
			}
			run = depth
		case run <= 0:
			c.set(x, int(y), z, cur.foundation)
		default:
			c.set(x, int(y), z, prevMid)
			run--
		}
	}
}

// pickMat is the capped material whose noise is highest at the column
// (the first on a tie; a single material needs no noise).
func pickMat(ms []cappedMat, col *column) uint32 {
	if len(ms) == 0 {
		return noBlock
	}
	best, bestV := ms[0].block, float32(-1)
	if len(ms) == 1 {
		return best
	}
	for _, m := range ms {
		v := m.noise.Sample(float32(col.wx)*0.0625, SeaLevel, float32(col.wz)*0.0625) * m.noise.Scale()
		if v > bestV {
			best, bestV = m.block, v
		}
	}
	return best
}

// solid is the capped builder's solid test: anything but air, water and
// lava.
func (n *Nether) solid(b uint32) bool {
	return b != n.ids.air && b != n.ids.lava && b != n.ids.water && b != n.ids.flowingLava && b != n.ids.flowingWater
}

// cappedSurface is the Nether's capped builder (sub_C752370): walking up
// the column, it caps each ceiling's underside with a ceiling material and
// each floor's top with a floor material (or the beach material near the
// lava sea), then turns lava into the sea material and netherrack into the
// foundation.
func (n *Nether) cappedSurface(c *Chunk, mt *rand.MT, col *column, b *biomeSurface) {
	floorDepth := int32(mt.Float()*0.25 + (float32(float64(col.noise)/3) + 3))
	ceilDepth := int32(mt.Float()*0.25 + (float32(float64(col.noise)/3) + 3))
	floor, ceil := pickMat(b.floors, col), pickMat(b.ceilings, col)
	beach := false
	if b.beach != noBlock {
		nb := n.surfNoise.beach
		beach = nb.Sample(float32(col.wx)*0.03125, 109, float32(col.wz)*0.03125)*nb.Scale() > 0
	}
	x, z := col.x, col.z
	prev := n.solid(c.Block(x, 0, z))
	for y := int32(0); y < Height; y++ {
		s := n.solid(c.Block(x, int(y), z))
		if s != prev {
			if s {
				// The underside of a ceiling: cap upward.
				end := min(max(y+ceilDepth, -1), Height)
				for yy := y; yy != end; yy++ {
					if !n.solid(c.Block(x, int(yy), z)) {
						break
					}
					if ceil != noBlock {
						c.set(x, int(yy), z, ceil)
					}
				}
				prev = true
			} else {
				// The top of a floor at y-1: cap downward.
				end := min(max(y-1-floorDepth, -1), Height)
				for yy := y - 1; yy != end; yy-- {
					if !n.solid(c.Block(x, int(yy), z)) {
						break
					}
					m := floor
					if beach && yy >= SeaLevel-5 && yy <= SeaLevel {
						m = b.beach
					}
					if m != noBlock {
						c.set(x, int(yy), z, m)
					}
				}
				prev = false
			}
		}
		blk := c.Block(x, int(y), z)
		if blk != n.ids.netherrack {
			if n.liquid(blk) && b.mats.sea != noBlock {
				c.set(x, int(y), z, b.mats.sea)
			}
		} else if b.mats.foundation != noBlock {
			c.set(x, int(y), z, b.mats.foundation)
		}
	}
}
