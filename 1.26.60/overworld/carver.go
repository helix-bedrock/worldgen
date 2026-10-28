package overworld

import (
	"fmt"
	"math"
	"sync"

	"github.com/helix-bedrock/worldgen/1.26.60/rand"
)

// Cave carvers, copied from BDS's CaveFeature (new-world parameters). They
// run after the surface builders. Each carver feature reseeds the chunk's
// Mersenne Twister with the world seed, draws two odd multipliers, and for
// each source chunk within 8 chunks seeds a Mersenne Twister with
// seed ^ (a*sx + b*sz) to lay out its tunnels as nodes. The nodes of a source
// chunk are cached and carved into every target chunk they reach.

// caveCarver is one cave_carver_feature (from its JSON).
type caveCarver struct {
	skipChance   uint32
	heightLimit  int32
	yScale       [2]float32
	hMult, vMult [2]float32
	floor        [2]float32
	width        float32
	fill         uint32

	mu    sync.Mutex
	cache map[[2]int32][]carveNode
}

// carveNode is one ellipsoid a tunnel or room carves: BDS's 56-byte node.
type carveNode struct {
	x, y, z   float32
	hr, vr    float32
	params    [6]float32 // h mult, v mult, floor level, start x, y, z
	step, len int32
	thickness float32
}

// Constants of BDS's CaveFeature constructor.
const (
	caveStartBound  = 40
	caveRoomChance  = 4
	caveRoomTunnels = 4
	caveSteepChance = 6
	caveSkipStep    = 4
	caveLegacyLen   = 112
)

// Tunnel turn damping of the CaveFeature walk: the pitch and yaw turn rates
// decay each step, and the pitch is damped harder on a steep tunnel.
const (
	cavePitchDecay float32 = 0.9
	caveYawDecay   float32 = 0.75
	caveDampSteep  float32 = 0.92
	caveDamp       float32 = 0.7
)

func newCaveCarver(o *Overworld, skip uint32, height int32, yScale, h, v, floor [2]float32, fill string) (*caveCarver, error) {
	id, ok := o.reg.Default(fill)
	if !ok {
		return nil, fmt.Errorf("worldgen: carver fill %s missing from the block list", fill)
	}
	return &caveCarver{
		skipChance: skip, heightLimit: height, yScale: yScale,
		hMult: h, vMult: v, floor: floor, fill: id,
		cache: map[[2]int32][]carveNode{},
	}, nil
}

// lerpDraw draws a value in [min, max] the way BDS does: only when the range
// is not empty.
func lerpDraw(r *rand.MT, v [2]float32) float32 {
	if v[0] == v[1] {
		return v[0]
	}
	return v[0] + (v[1]-v[0])*r.Float()
}

// nodes returns the carve nodes of source chunk (sx, sz), generating them
// once.
func (cv *caveCarver) nodes(seed uint32, a, b int32, sx, sz int32) []carveNode {
	k := [2]int32{sx, sz}
	cv.mu.Lock()
	defer cv.mu.Unlock()
	if n, ok := cv.cache[k]; ok {
		return n
	}
	r := rand.NewMT(seed ^ uint32(a*sx+b*sz))
	var out []carveNode
	cv.start(r, sx, sz, &out)
	cv.cache[k] = out
	return out
}

// start lays out the tunnels starting in source chunk (sx, sz) (slot 8).
func (cv *caveCarver) start(r *rand.MT, sx, sz int32, out *[]carveNode) {
	n := r.IntN(r.IntN(r.IntN(caveStartBound)+1) + 1)
	skip := r.IntN(int32(cv.skipChance))
	if !(n > 0 && skip == 0) {
		return
	}
	for range n {
		z := float32(16*sz + r.IntN(16))
		y := float32(r.IntN(cv.heightLimit) + 8)
		x := float32(16*sx + r.IntN(16))
		var p [6]float32
		p[0] = lerpDraw(r, cv.hMult)
		p[1] = lerpDraw(r, cv.vMult)
		p[2] = lerpDraw(r, cv.floor)
		p[3], p[4], p[5] = x, y, z
		tunnels := int32(1)
		if r.IntN(caveRoomChance) == 0 {
			cv.room(r, x, y, z, p, out)
			tunnels = r.IntN(caveRoomTunnels) + 1
		}
		for range tunnels {
			f := r.Float()
			yaw := f*pi32 + f*pi32
			g := r.Float()
			pitch := ((g + -0.5) + (g + -0.5)) * 0.125
			thickness := caveThickness(r)
			length := caveLegacyLen - r.IntN(28)
			cv.tunnel(r, x, y, z, thickness, yaw, pitch, 1, 0, length, p, out)
		}
	}
}

// caveThickness is BDS's new-world tunnel thickness.
func caveThickness(r *rand.MT) float32 {
	a := r.Float()
	t := (a + a) + r.Float()
	if r.IntN(10) != 0 {
		return t
	}
	b := r.Float()
	c := r.Float()
	return t * ((c*b)*3 + 1)
}

// room is the large round room some starts open with (slot 5).
func (cv *caveCarver) room(r *rand.MT, x, y, z float32, p [6]float32, out *[]carveNode) {
	f := r.Float()
	m := rand.NewMT(r.Uint32() >> 1)
	thickness := f*6 + 1
	ys := cv.yScale[0]
	if cv.yScale[0] != cv.yScale[1] {
		ys = cv.yScale[0] + (cv.yScale[1]-cv.yScale[0])*m.Float()
	}
	l := int32(caveLegacyLen - m.Uint32()%28)
	s := sin32((float32(l>>1) * pi32) / float32(l))
	radius := s*(cv.width+thickness) + 1.5
	vr := ys * radius
	*out = append(*out, carveNode{
		x: x, y: y, z: z, hr: radius * p[0], vr: vr * p[1], params: p,
		step: l >> 1, len: l, thickness: thickness,
	})
}

func sin32(v float32) float32 { return float32(math.Sin(float64(v))) }
func cos32(v float32) float32 { return float32(math.Cos(float64(v))) }

// u is a raw Mersenne Twister draw scaled to [0, 1), via double as in BDS.
func u(m *rand.MT) float32 { return float32(float64(m.Uint32()) * 2.3283064365386963e-10) }

// tunnel walks a tunnel and records its nodes (slot 6).
func (cv *caveCarver) tunnel(parent *rand.MT, x, y, z, thickness, yaw, pitch, yScale float32, startStep, length int32, p [6]float32, out *[]carveNode) {
	m := rand.NewMT(parent.Uint32() >> 1)
	var split int32
	l := length
	if length <= 0 {
		l = int32(caveLegacyLen - m.Uint32()%28)
	} else if length == 1 {
		l = 1
	}
	if length != 1 {
		split = int32(m.Uint32() % uint32(l>>1))
	}
	damp := caveDamp
	if m.Uint32()%caveSteepChance == 0 {
		damp = caveDampSteep
	}
	if l <= startStep {
		return
	}
	split += l >> 2
	var pitchMod, yawMod float32
	for i := startStep; i < l; i++ {
		s := sin32(float32(i) * pi32 / float32(l))
		cp := cos32(pitch)
		sp := sin32(pitch)
		x += cos32(yaw) * cp
		y += sp
		z += sin32(yaw) * cp
		pitch *= damp
		u1, u2, u3 := u(m), u(m), u(m)
		u4, u5, u6 := u(m), u(m), u(m)
		radius := s*(cv.width+thickness) + 1.5
		pitch += pitchMod * 0.1
		yaw += yawMod * 0.1
		addPitch := (u3 * (u1 - u2)) * 2
		addYaw := (u6 * (u4 - u5)) * 4
		if thickness > 1 && split == i {
			t := float32(float64(float32(float64(m.Uint32())*2.3283064365386963e-10*0.5)) + 0.5)
			cv.tunnel(m, x, y, z, t, yaw+-1.5707964, pitch/3, 1, split, l, p, out)
			t = float32(float64(float32(float64(m.Uint32())*2.3283064365386963e-10*0.5)) + 0.5)
			cv.tunnel(m, x, y, z, t, yaw+1.5707964, pitch/3, 1, split, l, p, out)
			return
		}
		if m.Uint32()%caveSkipStep != 0 {
			*out = append(*out, carveNode{
				x: x, y: y, z: z, hr: radius, vr: yScale * radius, params: p,
				step: i, len: l, thickness: thickness,
			})
		}
		pitchMod = pitchMod*cavePitchDecay + addPitch
		yawMod = yawMod*caveYawDecay + addYaw
	}
}

// caveCarvable reports whether a carver may cut block b (BDS
// sub_498AB80).
func (o *Overworld) caveCarvable(b uint32) bool {
	_, ok := o.ids.carvable[b]
	return ok
}

// carve runs the carver over target chunk (cx, cz) (slot 2): every node of
// the source chunks within 8 chunks whose ellipsoid can reach the chunk.
func (cv *caveCarver) carve(o *Overworld, c *Chunk, aq *aquifer, cx, cz int32) {
	r := rand.NewMT(uint32(o.seed))
	a := int32(r.Uint32()>>1) | 1
	b := int32(r.Uint32()>>1) | 1
	centreX, centreZ := float32(16*cx+8), float32(16*cz+8)
	for sx := cx - 8; sx <= cx+8; sx++ {
		for sz := cz - 8; sz <= cz+8; sz++ {
			nodes := cv.nodes(uint32(o.seed), a, b, sx, sz)
			for i := range nodes {
				n := &nodes[i]
				reach := (n.thickness + 2) + 16
				rem := n.len - n.step
				dx, dz := n.x-centreX, n.z-centreZ
				if !(reach*reach >= (dx*dx+dz*dz)-float32(rem*rem)) {
					continue
				}
				span := (n.hr + n.hr) + 16
				fx := float32(int32(math.Floor(float64(n.x))))
				if !(centreX-span <= fx && fx <= centreX+span) {
					continue
				}
				fz := float32(int32(math.Floor(float64(n.z))))
				if !(centreZ-span <= fz && fz <= centreZ+span) {
					continue
				}
				cv.carveNode(o, c, aq, cx, cz, n)
			}
		}
	}
}

func floorI(v float32) int32 { return int32(math.Floor(float64(v))) }

// carveNode carves one node's ellipsoid inside the chunk (slot 7, new-world
// path: the aquifer decides what fills the space).
func (cv *caveCarver) carveNode(o *Overworld, c *Chunk, aq *aquifer, cx, cz int32, n *carveNode) {
	bx, bz := 16*cx, 16*cz
	minX := max(floorI(n.x-n.hr)-bx-1, 0)
	maxX := min(floorI(n.x+n.hr)-bx, 15) + 1
	minZ := max(floorI(n.z-n.hr)-bz-1, 0)
	maxZ := min(floorI(n.z+n.hr)-bz, 15) + 1
	top := min(int32(fillHeight-2), cv.heightLimit)
	yHi := floorI(n.vr + n.y)
	maxY := top
	if !(top <= yHi) {
		maxY = max(yHi, 0) + 1
	}
	yLo := floorI(n.y-n.vr) - 1
	minY := int32(1)
	if yLo >= 2 {
		minY = yLo
	}
	if yLo > maxY {
		minY = maxY
	}
	floorLevel := n.params[2]
	for x := minX; x < maxX; x++ {
		ex := ((float32(x+bx) + 0.5) - n.x) / n.hr
		ex2 := ex * ex
		for z := minZ; z < maxZ; z++ {
			ez := ((float32(z+bz) + 0.5) - n.z) / n.hr
			ez2 := ez * ez
			if !(ex2+ez2 < 1) {
				continue
			}
			for y := maxY; y > minY; y-- {
				ey := ((float32(y-1) + 0.5) - n.y) / n.vr
				if floorLevel >= ey || (ey*ey+ex2)+ez2 >= 1 {
					continue
				}
				cv.carveBlock(o, c, aq, int(x), y, int(z), bx, bz)
			}
		}
	}
}

func (cv *caveCarver) carveBlock(o *Overworld, c *Chunk, aq *aquifer, x int, ly int32, z int, bx, bz int32) {
	blk := c.at(x, ly, z)
	if !o.caveCarvable(blk) {
		return
	}
	if ly <= fillHeight-3 && o.sandFamily(c.at(x, ly+1, z)) && o.sandFamily(c.at(x, ly+2, z)) && o.sandFamily(c.at(x, ly+3, z)) {
		c.put(x, ly+1, z, o.ids.sandstone)
	}
	wx, wy, wz := bx+int32(x), ly+MinY, bz+int32(z)
	aq.sample(wx, wy, wz)
	if !(aq.density > 0) {
		fill := cv.fill
		if wy < aq.level {
			fill = o.ids.water
			if aq.isLava {
				fill = o.ids.lava
			}
		}
		c.put(x, ly, z, fill)
	}
	if (blk == o.ids.grass || blk == o.ids.mycelium) && ly > 0 {
		if below := c.at(x, ly-1, z); below == o.ids.dirt || below == o.ids.coarseDirt {
			c.put(x, ly-1, z, blk)
		}
	}
}
