package overworld

import (
	"fmt"
	"math"
	"strings"

	"github.com/helix-bedrock/worldgen/biome"
	"github.com/helix-bedrock/worldgen/noise"
	"github.com/helix-bedrock/worldgen/rand"
)

// Surface builders paint the top of the terrain (grass, sand, terracotta,
// icebergs...) after the stone shape is filled. Each biome's JSON names a
// builder type and its materials; only the algorithms are code.
//
// Heights inside this file are local column indices, y - MinY, as in
// vanilla: the arithmetic (sea level, water top) is done on them.

const (
	noBlock = ^uint32(0)
	// waterTopNone is the water-top tracker's "no water above" value.
	waterTopNone = math.MinInt32 + 1000
	seaLocal     = SeaLevel - MinY
	// seaLevelMode selects the default builder's sea-level mode; see
	// defaultSurface. New worlds use the water-top mode.
	seaLevelMode = false
)

type builderKind uint8

const (
	builderNone builderKind = iota
	builderOverworld
	builderSwamp
	builderFrozenOcean
	builderMesa
	builderGradient
)

type surfaceMats struct {
	top, mid, seaFloor, foundation, sea uint32
}

type matAdjustment struct {
	freq, min, max float32
	mats           surfaceMats
}

// biomeSurface is a biome's surface builder, resolved to block IDs.
type biomeSurface struct {
	kind          builderKind
	mats          surfaceMats
	seaFloorDepth int32
	adjustments   []matAdjustment
	steep         uint32
	north, south  bool
	west, east    bool
	temperature   float32
	frozenPatches bool
	puddleDepth   int32

	// mesa
	clay, hardClay uint32
	clayOnly       [16]uint32
	pillars        bool
	forest         bool

	// noise_gradient
	gradient       *noise.Normal
	nonReplaceable []uint32
	specifiers     []gradientSpec
}

type gradientSpec struct {
	min, max float32
	block    uint32
}

// surfaceNoises are the per-world noises the builders share.
type surfaceNoises struct {
	depth      *noise.SimplexOctaves
	adjustment *noise.SimplexOctaves
	swamp      *noise.SimplexOctaves
	frozenA    *noise.SimplexOctaves
	frozenB    *noise.SimplexOctaves
	// heightTemp is the per-column noise that varies the height cooling of
	// biome temperatures (seed 1234, one octave).
	heightTemp  *noise.SimplexOctaves
	iceSurface  *noise.Normal
	icePillar   *noise.Normal
	iceRoof     *noise.Normal
	icebergRand rand.Positional
	mesa        mesaNoises
}

// newSurfaceNoises seeds the builders' noises. world is the forked
// positional factory of the world seed.
func newSurfaceNoises(seed int64, world rand.Positional) surfaceNoises {
	var n surfaceNoises
	depthRand := world.FromHash("minecraft:surface")
	n.depth = noise.NewSimplexOctaves(&depthRand, 4, true)

	// The adjustment noise uses a Mersenne Twister on the low 32 bits of the
	// seed, with 20202 draws thrown away first.
	mt := rand.NewMT(uint32(seed))
	for range 20202 {
		mt.Uint32()
	}
	n.adjustment = noise.NewSimplexOctaves(mt, 3, false)

	n.swamp = noise.NewSimplexOctaves(rand.NewMT(uint32(seed)), 2, false)

	// Partially frozen oceans use fixed seeds, the same in every world.
	n.frozenA = noise.NewSimplexOctaves(rand.NewMT(3456), 3, false)
	n.frozenB = noise.NewSimplexOctaves(rand.NewMT(2345), 1, false)
	n.heightTemp = noise.NewSimplexOctaves(rand.NewMT(1234), 1, false)

	// The iceberg and badlands builders are seeded from the seed cut to a
	// signed 32-bit value.
	small := builderFactory(int64(int32(seed)))
	surf, pillar, roof := small.FromHash("minecraft:iceberg_surface"), small.FromHash("minecraft:iceberg_pillar"), small.FromHash("minecraft:iceberg_pillar_roof")
	n.iceSurface = noise.NewNormal(&surf, -6, []float32{1, 1, 1})
	n.icePillar = noise.NewNormal(&pillar, -6, []float32{1, 1, 1, 1})
	n.iceRoof = noise.NewNormal(&roof, -3, []float32{1})
	n.icebergRand = small
	n.mesa = newMesaNoises(uint32(seed), small)
	return n
}

// builderFactory is the forked positional factory for seed, as the terrain
// noises use.
func builderFactory(seed int64) rand.Positional {
	lo, hi := rand.Seed128(seed)
	r := rand.NewXoroshiro(lo, hi)
	return r.Fork()
}

// loadSurfaces resolves every biome's surface builder to block IDs, indexed
// by numeric biome ID.
func (o *Overworld) loadSurfaces(defs map[string]*biome.Def, biomeIDs map[string]uint32, world rand.Positional) error {
	var maxID uint32
	for _, id := range biomeIDs {
		maxID = max(maxID, id)
	}
	o.surfaces = make([]*biomeSurface, maxID+1)
	block := func(b biome.Block) (uint32, error) {
		if b.Name == "" {
			return noBlock, nil
		}
		id, ok := o.reg.Lookup(b.Name, b.States)
		if !ok {
			id, ok = o.reg.Default(flattened(b))
		}
		if !ok {
			return 0, fmt.Errorf("worldgen: surface block %s %v missing from the block list", b.Name, b.States)
		}
		return id, nil
	}
	mats := func(top, mid, seaFloor, foundation, sea biome.Block) (surfaceMats, error) {
		var m surfaceMats
		var err error
		for _, f := range []struct {
			dst  *uint32
			name biome.Block
		}{{&m.top, top}, {&m.mid, mid}, {&m.seaFloor, seaFloor}, {&m.foundation, foundation}, {&m.sea, sea}} {
			if *f.dst, err = block(f.name); err != nil {
				return m, err
			}
		}
		return m, nil
	}
	build := func(d *biome.Def, s *biome.Surface) (*biomeSurface, error) {
		bs := &biomeSurface{temperature: d.Temperature, frozenPatches: d.PartiallyFrozen, steep: noBlock}
		switch s.Type {
		case "minecraft:overworld":
			bs.kind = builderOverworld
		case "minecraft:swamp":
			bs.kind = builderSwamp
			bs.puddleDepth = int32(s.MaxPuddleDepth)
		case "minecraft:frozen_ocean":
			bs.kind = builderFrozenOcean
		case "minecraft:mesa":
			bs.kind = builderMesa
			bs.pillars, bs.forest = s.BrycePillars, s.HasForest
		case "minecraft:noise_gradient":
			bs.kind = builderGradient
		default:
			return nil, nil
		}
		var err error
		if bs.mats, err = mats(s.Top, s.Mid, s.SeaFloor, s.Foundation, s.Sea); err != nil {
			return nil, err
		}
		bs.seaFloorDepth = int32(s.SeaFloorDepth)
		if bs.kind == builderMesa {
			if bs.clay, err = block(s.ClayMaterial); err != nil {
				return nil, err
			}
			if bs.hardClay, err = block(s.HardClayMaterial); err != nil {
				return nil, err
			}
			for i := range bs.clayOnly {
				bs.clayOnly[i] = bs.clay
			}
		}
		if bs.kind == builderGradient {
			r := world.FromHash(s.NoiseName)
			bs.gradient = noise.NewNormal(&r, s.FirstOctave, s.Amplitudes)
			for _, n := range s.NonReplaceable {
				b, err := block(n)
				if err != nil {
					return nil, err
				}
				bs.nonReplaceable = append(bs.nonReplaceable, b)
			}
			for _, sp := range s.Specifiers {
				b, err := block(sp.Block)
				if err != nil {
					return nil, err
				}
				bs.specifiers = append(bs.specifiers, gradientSpec{sp.Min, sp.Max, b})
			}
		}
		for _, a := range d.Adjustments {
			if a.HeightMin != "" || a.HeightMax != "" {
				// Height-ranged adjustments are Nether-only and need Molang.
				continue
			}
			m, err := mats(a.Top, a.Mid, a.SeaFloor, a.Foundation, a.Sea)
			if err != nil {
				return nil, err
			}
			bs.adjustments = append(bs.adjustments, matAdjustment{a.FreqScale, a.Min, a.Max, m})
		}
		if d.Steep != nil {
			if bs.steep, err = block(d.Steep.Material); err != nil {
				return nil, err
			}
			bs.north, bs.south, bs.west, bs.east = d.Steep.North, d.Steep.South, d.Steep.West, d.Steep.East
		}
		return bs, nil
	}
	o.subsurfaces = make([]*biomeSurface, maxID+1)
	for name, id := range biomeIDs {
		d, ok := defs[name]
		if !ok {
			continue
		}
		var err error
		if d.Surface != nil {
			if o.surfaces[id], err = build(d, d.Surface); err != nil {
				return err
			}
		}
		if d.Subsurface != nil {
			if o.subsurfaces[id], err = build(d, d.Subsurface); err != nil {
				return err
			}
		}
	}
	return nil
}

// flattened maps an old block-with-state to the separate block newer
// versions split it into, as BDS upgrades the vanilla data when loading it:
// minecraft:dirt with dirt_type=coarse becomes minecraft:coarse_dirt.
func flattened(b biome.Block) string {
	if len(b.States) != 1 {
		return ""
	}
	for _, v := range b.States {
		if s, ok := v.(string); ok {
			return "minecraft:" + s + "_" + strings.TrimPrefix(b.Name, "minecraft:")
		}
	}
	return ""
}

// columnTop is the local index of the highest non-air block in a column, or
// -1 for an empty column.
func (c *Chunk) columnTop(x, z int, air uint32) int32 {
	for ly := int32(Height - 1); ly >= 0; ly-- {
		if uint32(c.Blocks[int(ly)<<8|z<<4|x]) != air {
			return ly
		}
	}
	return -1
}

func (c *Chunk) at(x int, ly int32, z int) uint32 { return uint32(c.Blocks[int(ly)<<8|z<<4|x]) }
func (c *Chunk) put(x int, ly int32, z int, b uint32) {
	c.Blocks[int(ly)<<8|z<<4|x] = uint16(b)
}

// prelimSurface is BDS's preliminary surface level of a quarter column: the
// highest grid corner, scanning down from y 248 in steps of 8, whose density
// without jaggedness, minus 90, after the top and bottom slides, is above 50.
// It is noSurface when no corner qualifies.
func prelimSurface(col column) int32 {
	y, seg := int32(248), int32(1)
	for j := gridY - 2; j >= 0; j-- {
		d := ((float32(y)*-0.0078125 + 1) + col.offset) * col.factor
		if d > 0 {
			d *= 4
		}
		d += -90
		v := float32(-10)
		if s := float32(seg) * 0.5; s >= 0 {
			if s <= 1 {
				d = (d+10)*s - 10
			}
			v = d
		}
		if t := float32(uint8(j)) / 3; !(1 < t) {
			v = (v-15)*t + 15
		}
		if v > 50 {
			return y &^ 7
		}
		seg++
		y -= 8
	}
	return noSurface
}

// surface runs the biome surface builders, x outer and z inner, continuing
// the chunk's Mersenne Twister after the bedrock floor. Each column gets the
// builder of the biome at its height-map level, walking down from its top to
// the preliminary surface level minus 8 (exclusive). Subsurface builders
// (sulfur_caves) then run over their own biome runs.
func (o *Overworld) surface(chunkX, chunkZ int32, c *Chunk, mt *rand.MT) {
	var depthNoise [256]float32
	x0, z0 := chunkX*16, chunkZ*16
	o.surfNoise.depth.SurfaceDepth(&depthNoise, x0, z0)
	var floors [16]int32
	for qx := range 4 {
		for qz := range 4 {
			y := prelimSurface(o.column(chunkX*4+int32(qx), chunkZ*4+int32(qz)))
			floors[qx*4+qz] = min(max(y-8, MinY), MaxY-1) - MinY
		}
	}
	for x := range 16 {
		for z := range 16 {
			top := c.columnTop(x, z, o.ids.air)
			// The height map is the block above the top block.
			hm := top + 1
			col := surfaceColumn{
				x: x, z: z, wx: x0 + int32(x), wz: z0 + int32(z),
				ySample: surfaceNoiseY, noise: depthNoise[x+16*z], top: top,
				start: floors[(x>>2)*4+z>>2], end: Height - 1,
			}
			b := uint32(c.Biomes[int(min(hm, Height-1))<<8|z<<4|x])
			if int(b) < len(o.surfaces) && o.surfaces[b] != nil {
				o.buildSurface(c, mt, &col, o.surfaces[b])
			}
		}
	}
	o.subsurface(c, x0, z0)
}

// surfaceNoiseY is the y BDS passes for a column's adjustment noise and
// temperature: the chunk's origin, as a local index.
// VERIFY against BDS (stony peaks calcite, dappled forest coarse dirt).
const surfaceNoiseY = 0

// subsurface runs subsurface builders over the runs of their biome in each
// column, bottom up.
func (o *Overworld) subsurface(c *Chunk, x0, z0 int32) {
	if len(o.subsurfaces) == 0 {
		return
	}
	for x := range 16 {
		for z := range 16 {
			top := c.columnTop(x, z, o.ids.air)
			if top < 0 {
				continue
			}
			col := surfaceColumn{x: x, z: z, wx: x0 + int32(x), wz: z0 + int32(z), top: top}
			run := func(b uint32, start, end int32) {
				if int(b) < len(o.subsurfaces) && o.subsurfaces[b] != nil {
					col.start, col.end = start, end
					o.gradientSurface(c, &col, o.subsurfaces[b])
				}
			}
			start, cur := int32(0), uint32(c.Biomes[z<<4|x])
			for y := int32(1); y <= top; y++ {
				b := uint32(c.Biomes[int(y)<<8|z<<4|x])
				if b == cur {
					continue
				}
				run(cur, start, y)
				start, cur = y, b
			}
			run(cur, start, top+1)
		}
	}
}

type surfaceColumn struct {
	x, z    int
	wx, wz  int32
	ySample int32
	noise   float32
	top     int32
	// start and end bound the biome run being built.
	start, end int32
}

func (o *Overworld) buildSurface(c *Chunk, mt *rand.MT, col *surfaceColumn, b *biomeSurface) {
	switch b.kind {
	case builderOverworld:
		o.defaultSurface(c, mt, col, b)
	case builderSwamp:
		o.swampSurface(c, mt, col, b)
	case builderFrozenOcean:
		o.frozenSurface(c, mt, col, b)
	case builderMesa:
		o.mesaSurface(c, mt, col, b)
	case builderGradient:
		o.gradientSurface(c, col, b)
	}
}

// temperatureAt is the biome temperature used for freezing. Frozen oceans
// have noise patches at 0.2 that stay open.
// VERIFY: the height cooling above sea level + 17 is left out; surface ice is
// only placed at sea level, below it.
func (o *Overworld) temperatureAt(b *biomeSurface, wx, wz int32) float32 {
	t := b.temperature
	if !b.frozenPatches {
		return t
	}
	x, z := float32(wx), float32(wz)
	n1 := o.surfNoise.frozenA.Sum2(x*0.05, z*0.05)
	n2 := o.surfNoise.frozenB.Sum2(x*0.2, z*0.2)
	if n1+n2 < 0.3 {
		if o.surfNoise.frozenB.Sum2(x*0.09, z*0.09) < 0.8 {
			t = 0.2
		}
	}
	return t
}

// adjusted returns the materials after the adjustments whose noise range
// holds at this column; later matches override earlier ones field by field.
func (o *Overworld) adjusted(b *biomeSurface, col *surfaceColumn) surfaceMats {
	m := b.mats
	if len(b.adjustments) == 0 {
		return m
	}
	n := o.surfNoise.adjustment
	for _, a := range b.adjustments {
		f := a.freq
		v := n.Sum3(f*float32(col.wx), f*float32(col.ySample), f*float32(col.wz)) * n.Scale
		if v < a.min || v >= a.max {
			continue
		}
		for _, p := range []struct {
			dst *uint32
			v   uint32
		}{
			{&m.top, a.mats.top}, {&m.mid, a.mats.mid}, {&m.seaFloor, a.mats.seaFloor},
			{&m.foundation, a.mats.foundation}, {&m.sea, a.mats.sea},
		} {
			if p.v != noBlock {
				*p.dst = p.v
			}
		}
	}
	return m
}

// steepAt reports whether the column is on a slope facing one of the
// biome's steep directions: the top heights two blocks apart (clamped inside
// the chunk) differ by 4 or more.
func (o *Overworld) steepAt(c *Chunk, b *biomeSurface, x, z int) bool {
	h := func(x, z int) int32 { return c.columnTop(x, z, o.ids.air) }
	if b.north || b.south {
		d := h(x, max(z-1, 0)) - h(x, min(z, 14)+1)
		if (b.south && d >= 4) || (b.north && d <= -4) {
			return true
		}
	}
	if b.west || b.east {
		d := h(max(x-1, 0), z) - h(min(x, 14)+1, z)
		if (b.east && d >= 4) || (b.west && d <= -4) {
			return true
		}
	}
	return false
}

func (o *Overworld) replaceable(b uint32) bool {
	for _, r := range o.ids.replaceable {
		if b == r {
			return true
		}
	}
	return false
}

func (o *Overworld) sandFamily(b uint32) bool {
	return b == o.ids.sand || b == o.ids.redSand || b == o.ids.suspiciousSand
}

func (o *Overworld) liquid(b uint32) bool {
	return b == o.ids.water || b == o.ids.flowingWater || b == o.ids.lava || b == o.ids.flowingLava
}

// defaultSurface is the overworld builder: top and mid materials over the
// first stone below air, the sea floor deep under water, sandstone under
// sand.
//
// The code has two modes behind a world-version flag: measuring from sea
// level, or (new worlds) from the tracked water top plus turning floating
// sand to sandstone. In the water-top mode sandstone runs until the walk ends
// at the preliminary surface level minus 8.
func (o *Overworld) defaultSurface(c *Chunk, mt *rand.MT, col *surfaceColumn, b *biomeSurface) {
	x, z := col.x, col.z
	temp := o.temperatureAt(b, col.wx, col.wz)
	cur := o.adjusted(b, col)
	if b.steep != noBlock && o.steepAt(c, b, x, z) {
		cur.top, cur.mid = b.steep, b.steep
	}
	rnd := mt.Float()
	depth := int32(rnd*0.25 + (float32(float64(col.noise)/3) + 3))
	depthSea := depth + b.seaFloorDepth

	run := int32(-1)
	waterTop := int32(waterTopNone)
	prevTop, prevMid := cur.top, cur.mid
	for ly := min(col.top+1, Height-1); ly > col.start; ly-- {
		blk := c.at(x, ly, z)
		if blk == o.ids.bedrock {
			continue
		}
		// Sand or gravel left over air or liquid is turned to stone of its
		// kind so it does not hang.
		if !seaLevelMode && cur.foundation != o.ids.netherrack && run >= 0 && (blk == o.ids.air || o.liquid(blk)) && ly+1 < Height {
			switch c.at(x, ly+1, z) {
			case o.ids.sand:
				c.put(x, ly+1, z, o.ids.sandstone)
			case o.ids.redSand:
				c.put(x, ly+1, z, o.ids.redSandstone)
			case o.ids.gravel:
				c.put(x, ly+1, z, cur.foundation)
			}
		}
		if blk == o.ids.air {
			run, waterTop = -1, waterTopNone
			continue
		}
		surfaceY := waterTop
		if seaLevelMode {
			surfaceY = seaLocal
		}
		if !o.replaceable(blk) {
			if blk == o.ids.water {
				c.put(x, ly, z, cur.sea)
			}
			if o.liquid(blk) || blk == o.ids.packedIce || blk == o.ids.blueIce {
				waterTop = max(waterTop, ly+1)
			}
			continue
		}
		switch {
		case run == -1:
			topB, fill := o.ids.air, cur.foundation
			if depth > 0 {
				topB, fill = prevTop, prevMid
				if ly >= surfaceY-4 && ly <= surfaceY+1 {
					topB, fill = cur.top, cur.mid
				}
			}
			if ly < surfaceY && topB == o.ids.air {
				topB = cur.sea
				if cur.sea == o.ids.water && temp < 0.15 {
					topB = o.ids.ice
				}
			}
			switch {
			case ly >= surfaceY-1:
				c.put(x, ly, z, topB)
				prevTop, prevMid = topB, fill
			case ly < surfaceY-depthSea || ly == col.start+1:
				c.put(x, ly, z, cur.seaFloor)
				prevTop, prevMid = o.ids.air, cur.foundation
			default:
				c.put(x, ly, z, fill)
				prevTop, prevMid = topB, fill
			}
			run = depth
		case run <= 0:
			c.put(x, ly, z, cur.foundation)
		default:
			c.put(x, ly, z, prevMid)
			run--
			if run == 0 && o.sandFamily(prevMid) {
				n := mt.IntN(4)
				prevMid = o.ids.sandstone
				run = max(ly-surfaceY, 0) + n
			}
		}
	}
}

// swampSurface floods low ground with puddles where the swamp noise is
// positive, then runs the default builder.
func (o *Overworld) swampSurface(c *Chunk, mt *rand.MT, col *surfaceColumn, b *biomeSurface) {
	n := o.surfNoise.swamp.Sum2(float32(col.wx)*0.25, float32(col.wz)*0.25)
	h := col.top + 1
	if n > 0 && h <= seaLocal {
		stop := seaLocal - b.puddleDepth
		for y := h; y > stop; y-- {
			if c.at(col.x, y-1, col.z) != o.ids.water {
				c.put(col.x, y-1, col.z, o.ids.water)
				break
			}
		}
	}
	o.defaultSurface(c, mt, col, b)
}

// frozenSurface raises icebergs of packed ice and snow from the sea, then
// runs the default builder.
func (o *Overworld) frozenSurface(c *Chunk, mt *rand.MT, col *surfaceColumn, b *biomeSurface) {
	sn := &o.surfNoise
	x, z := float32(col.wx), float32(col.wz)
	e := min(float32(math.Abs(float64(sn.iceSurface.Sample(x, 0, z))))*8.25, sn.icePillar.Sample(x*1.28, 0, z*1.28)*15)
	if e > 1.8 {
		roof := float32(math.Ceil(math.Abs(float64(sn.iceRoof.Sample(x*1.17, 0, z*1.17)*1.5))*40)) + 14
		f := min(e*e*1.2, roof)
		if o.temperatureAt(b, col.wx, col.wz) > 0.1 {
			f -= 2
		}
		var bottom, top float32
		if f > 2 {
			bottom = float32(seaLocal) - f - 7
			top = float32(seaLocal) + f
		}
		r := sn.icebergRand.At(col.wx, 0, col.wz)
		maxSnow := r.IntN(4) + 2
		snowY := int32(seaLocal) + 18 + r.IntN(10)
		snow := int32(0)
		for ly := max(col.top+1, int32(top)+1); ly > col.start; ly-- {
			if ly >= Height {
				continue
			}
			blk := c.at(col.x, ly, col.z)
			place := false
			if blk == o.ids.air && float32(ly) < top {
				place = r.Float() > 0.01
			} else if bottom != 0 && blk == o.ids.water && float32(ly) > bottom && ly < seaLocal {
				place = r.Float() > 0.15
			}
			if !place {
				continue
			}
			if snow > maxSnow || ly <= snowY {
				c.put(col.x, ly, col.z, o.ids.packedIce)
			} else {
				c.put(col.x, ly, col.z, o.ids.snow)
				snow++
			}
		}
	}
	o.defaultSurface(c, mt, col, b)
}

// gradientSurface (sulfur_caves) replaces the solid blocks of its run by the
// first specifier whose (min, max] holds the 3D noise.
func (o *Overworld) gradientSurface(c *Chunk, col *surfaceColumn, b *biomeSurface) {
	if len(b.specifiers) == 0 {
		return
	}
	for ly := min(col.end-1, col.top+1); ly >= col.start; ly-- {
		blk := c.at(col.x, ly, col.z)
		if blk == o.ids.air || o.liquid(blk) {
			continue
		}
		skip := false
		for _, n := range b.nonReplaceable {
			if blk == n {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		v := b.gradient.Sample(float32(col.wx), float32(ly+MinY), float32(col.wz))
		v = min(max(v, -1), 1)
		for _, s := range b.specifiers {
			if s.min < v && v <= s.max {
				c.put(col.x, ly, col.z, s.block)
				break
			}
		}
	}
}
