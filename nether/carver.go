package nether

import (
	"encoding/json"
	"fmt"
	"math"

	"github.com/helix-bedrock/worldgen"
	"github.com/helix-bedrock/worldgen/feature"
	"github.com/helix-bedrock/worldgen/rand"
)

// The Nether's cave carver, as BDS's NetherCaveFeature runs it: in
// loadChunk, after the surface, as the pregeneration pass. It shares the
// overworld CaveFeature's seeding (a Mersenne Twister per source chunk
// within 8 chunks, seeded with seed ^ (a*sx + b*sz)) but walks its tunnels
// with the sine table, carves each step as it goes, refuses steps whose
// box touches lava, and cuts only netherrack, dirt, coarse dirt and grass.

// Constants of BDS's CaveFeature constructor the Nether carver uses, and
// the ones NetherCaveFeature overrides (start bound 10).
const (
	ncStartBound  = 10
	ncRoomChance  = 4
	ncRoomTunnels = 4
	ncSteepChance = 6
	ncSkipStep    = 4
	ncLegacyLen   = 112
	// ncYScale is the vertical scale starts and rooms pass to the tunnel;
	// branches pass 1.
	ncYScale = 0.5
	// ncTop is the highest y a step's box reaches (BDS's constant 119).
	ncTop = 119
)

var (
	ncDecay     = [2]float32{0.9, 0.75}
	ncDampSteep = float32(0.92)
	ncDamp      = float32(0.7)
)

// netherCarver holds the carver's settings from its feature file and the
// two odd multipliers its source chunk seeds use.
type netherCarver struct {
	fill       uint32
	width      float32
	skipChance int32
	floor      float32
	a, b       int32
	sin        *[65536]float32
	// lava and cuts are by state: any lava, and the blocks it may cut.
	lava, cuts []bool
}

// loadCarver reads nether_cave_carver_feature.
func (n *Nether) loadCarver() error {
	raw, err := worldgen.Data.ReadFile("data/vanilla/features/nether_cave_carver_feature.json")
	if err != nil {
		return fmt.Errorf("worldgen: read nether carver: %w", err)
	}
	var f struct {
		F struct {
			Fill  string     `json:"fill_with"`
			Width float32    `json:"width_modifier"`
			Skip  int32      `json:"skip_carve_chance"`
			Floor [2]float32 `json:"floor_level"`
		} `json:"minecraft:nether_cave_carver_feature"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		return fmt.Errorf("worldgen: decode nether carver: %w", err)
	}
	// The carver draws a floor level only from a range; the vanilla one is
	// a constant, and that is all this port supports.
	if f.F.Floor[0] != f.F.Floor[1] || f.F.Skip <= 0 {
		return fmt.Errorf("worldgen: nether carver: floor %v, skip %d not supported", f.F.Floor, f.F.Skip)
	}
	cv := &n.carver
	var ok bool
	if cv.fill, ok = n.reg.Default(f.F.Fill); !ok {
		return fmt.Errorf("worldgen: nether carver fill %s missing from the block list", f.F.Fill)
	}
	cv.width, cv.skipChance, cv.floor = f.F.Width, f.F.Skip, f.F.Floor[0]
	r := rand.NewMT(uint32(n.seed))
	cv.a = int32(r.Uint32()>>1) | 1
	cv.b = int32(r.Uint32()>>1) | 1
	cv.sin = feature.SinTable()
	// BDS compares block types: lava (its liquid states included) stops a
	// step; netherrack, dirt, coarse dirt and grass blocks are cut.
	cuts := map[string]bool{
		"minecraft:netherrack": true, "minecraft:dirt": true,
		"minecraft:coarse_dirt": true, "minecraft:grass_block": true,
	}
	cv.lava = make([]bool, n.reg.Len())
	cv.cuts = make([]bool, n.reg.Len())
	for i := range cv.lava {
		s, ok := n.reg.State(uint32(i))
		if !ok {
			continue
		}
		cv.lava[i] = s.Name == "minecraft:lava" || s.Name == "minecraft:flowing_lava"
		cv.cuts[i] = cuts[s.Name]
	}
	return nil
}

// carve runs the carver over chunk (cx, cz): the tunnels of every source
// chunk within 8 chunks, x outer.
func (cv *netherCarver) carve(seed int64, cx, cz int32, c *Chunk) {
	var r rand.MT
	for sx := cx - 8; sx <= cx+8; sx++ {
		for sz := cz - 8; sz <= cz+8; sz++ {
			r.Seed(uint32(seed) ^ uint32(cv.a*sx+cv.b*sz))
			cv.start(&r, sx, sz, cx, cz, c)
		}
	}
}

// start lays out the tunnels that begin in source chunk (sx, sz).
func (cv *netherCarver) start(r *rand.MT, sx, sz, cx, cz int32, c *Chunk) {
	n := r.IntN(r.IntN(r.IntN(ncStartBound)+1) + 1)
	skip := r.IntN(cv.skipChance)
	if !(n > 0 && skip == 0) {
		return
	}
	for range n {
		z := float32(16*sz + r.IntN(16))
		y := float32(r.IntN(128))
		x := float32(16*sx + r.IntN(16))
		tunnels := int32(1)
		if r.IntN(ncRoomChance) == 0 {
			t := r.Float()*6 + 1
			cv.tunnel(r, cx, cz, x, y, z, t, 0, 0, ncYScale, -1, -1, c)
			tunnels = r.IntN(ncRoomTunnels) + 1
		}
		for range tunnels {
			f := r.Float()
			yaw := f*3.1415927 + f*3.1415927
			g := r.Float()
			pitch := ((g + -0.5) + (g + -0.5)) * 0.125
			a := r.Float()
			b := r.Float()
			t := (b + (a + a)) + (b + (a + a))
			cv.tunnel(r, cx, cz, x, y, z, t, yaw, pitch, ncYScale, 0, 0, c)
		}
	}
}

// unit is a raw draw scaled to [0, 1) through a double, as BDS does.
func unit(m *rand.MT) float32 { return float32(float64(m.Uint32()) * 2.3283064365386963e-10) }

func floorF(v float32) int32 { return int32(math.Floor(float64(v))) }

// tunnel walks one tunnel from (x, y, z), carving chunk (cx, cz) as it
// goes. A start of -1 makes a room: it starts halfway, never branches or
// skips, and ends after its first carve.
func (cv *netherCarver) tunnel(parent *rand.MT, cx, cz int32, x, y, z, thickness, yaw, pitch, yScale float32, start, length int32, c *Chunk) {
	m := rand.NewMT(parent.Uint32() >> 1)
	room := start == -1
	l := length
	if length <= 0 {
		l = int32(ncLegacyLen - m.Uint32()%28)
	}
	if room {
		start = l >> 1
	}
	var split int32
	if l != 1 || length <= 0 {
		split = int32(m.Uint32() % uint32(l>>1))
	}
	damp := ncDamp
	if m.Uint32()%ncSteepChance == 0 {
		damp = ncDampSteep
	}
	if start >= l {
		return
	}
	split += l >> 2
	fl := float32(l)
	centreX, centreZ := float32(16*cx+8), float32(16*cz+8)
	reach := (thickness + 2) + 16
	reach *= reach
	var pitchMod, yawMod float32
	for i := start; i < l; i++ {
		radius := cv.sinAt(float32(i)*3.1415927/fl)*(cv.width+thickness) + 1.5
		cp, sp := cv.cosAt(pitch), cv.sinAt(pitch)
		x += cv.cosAt(yaw) * cp
		y += sp
		z += cv.sinAt(yaw) * cp
		pitch = pitch*damp + pitchMod*0.1
		yaw += yawMod * 0.1
		u1, u2, u3 := unit(m), unit(m), unit(m)
		u4, u5, u6 := unit(m), unit(m), unit(m)
		pitchMod = (u3*(u1-u2))*2 + ncDecay[0]*pitchMod
		yawMod = (u6*(u4-u5))*4 + ncDecay[1]*yawMod
		if !room {
			if thickness > 1 && i == split {
				t := float32(float64(m.Uint32())*2.3283064365386963e-10*0.5) + 0.5
				cv.tunnel(m, cx, cz, x, y, z, t, yaw+-1.5707964, pitch/3, 1, split, l, c)
				t = float32(float64(m.Uint32())*2.3283064365386963e-10*0.5) + 0.5
				cv.tunnel(m, cx, cz, x, y, z, t, yaw+1.5707964, pitch/3, 1, split, l, c)
				return
			}
			if m.Uint32()%ncSkipStep == 0 {
				continue
			}
		}
		dx, dz := x-centreX, z-centreZ
		rem := float32(l - i)
		if (dx*dx+dz*dz)-rem*rem > reach {
			return
		}
		span := radius + radius
		if x < centreX-16-span || z < centreZ-16-span || x > centreX+16+span || z > centreZ+16+span {
			continue
		}
		if cv.step(cx, cz, x, y, z, radius, yScale*radius, c) && room {
			return
		}
	}
}

// step carves one step's ellipsoid into the chunk. It reports false when
// lava in the box's shell stopped it, true otherwise (carved or not).
func (cv *netherCarver) step(cx, cz int32, x, y, z, hr, vr float32, c *Chunk) bool {
	bx, bz := 16*cx, 16*cz
	minX := max(floorF(x-hr)-bx-1, 0)
	maxX := min(floorF(x+hr)-bx, 15)
	minY := max(floorF(y-vr), 2)
	maxY := min(floorF(y+vr), ncTop)
	minZ := max(floorF(z-hr)-bz-1, 0)
	maxZ := min(floorF(z+hr)-bz, 15)
	if minX > maxX || minZ > maxZ {
		return true
	}
	if cv.lavaShell(minX, maxX, minY-2, maxY+2, minZ, maxZ, c) {
		return false
	}
	for lx := minX; lx <= maxX; lx++ {
		ex := ((float32(lx+bx) + 0.5) - x) / hr
		ex2 := ex * ex
		for lz := minZ; lz <= maxZ; lz++ {
			ez := ((float32(lz+bz) + 0.5) - z) / hr
			ez2 := ez * ez
			for ly := maxY; ly >= minY-1; ly-- {
				ey := ((float32(ly) + 0.5) - y) / vr
				if !(cv.floor < ey) || !((ey*ey+ex2)+ez2 < 1) {
					continue
				}
				if cv.cuts[c.Block(int(lx), int(ly+1), int(lz))] {
					c.set(int(lx), int(ly+1), int(lz), cv.fill)
				}
			}
		}
	}
	return true
}

// lavaShell reports whether lava lies on the shell of the box: every
// height on its x and z faces, the top and bottom inside.
func (cv *netherCarver) lavaShell(minX, maxX, lo, hi, minZ, maxZ int32, c *Chunk) bool {
	if hi < lo {
		return false
	}
	at := func(x, y, z int32) bool {
		return y >= 0 && y < Height && cv.lava[c.Block(int(x), int(y), int(z))]
	}
	for x := minX; x <= maxX; x++ {
		for z := minZ; z <= maxZ; z++ {
			if x == minX || x == maxX || z == minZ || z == maxZ {
				for y := hi; y >= lo; y-- {
					if at(x, y, z) {
						return true
					}
				}
				continue
			}
			if at(x, hi, z) || at(x, lo, z) {
				return true
			}
		}
	}
	return false
}

func (cv *netherCarver) sinAt(a float32) float32 { return cv.sin[uint16(int32(a*10430.378))] }
func (cv *netherCarver) cosAt(a float32) float32 { return cv.sin[uint16(int32(a*10430.378+16384))] }
