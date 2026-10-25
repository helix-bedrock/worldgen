package overworld

import (
	"github.com/helix-bedrock/worldgen/1.26.52/rand"
)

// Canyons (ravines), copied from BDS's CanyonFeature (sub_4989150 and slot 0,
// new-world config at 0x148FC54). They run after the cave carvers. Unlike the
// caves, each target chunk replays the canyons of every source chunk within 8
// chunks from scratch.
const (
	canyonBase   = 64  // local y offset of the start height
	canyonMaxY   = 312 // highest local y carved
	canyonChance = 100
	canyonTable  = 320
)

func (o *Overworld) canyons(c *Chunk, aq *aquifer, cx, cz int32) {
	seed := uint32(o.seed)
	r := rand.NewMT(seed)
	a := int32(r.Uint32()>>1) | 1
	b := int32(r.Uint32()>>1) | 1
	centreX, centreZ := float32(16*cx|8), float32(16*cz|8)
	for sx := cx - 8; sx <= cx+8; sx++ {
		for sz := cz - 8; sz <= cz+8; sz++ {
			r.Seed(seed ^ uint32(a*sx+b*sz))
			if r.IntN(canyonChance) != 0 {
				continue
			}
			x := float32(16*sx + r.IntN(16))
			y := float32(r.IntN(58) + canyonBase + 10)
			r.Uint32() // two skipped draws
			r.Uint32()
			z := float32(16*sz + r.IntN(16))
			yaw := r.Float() * pi32
			pitch := (r.Float() + -0.5) * 0.25
			w1 := r.Float()
			width := w1*3 + r.Float()*3
			yScale := float32(4)
			_ = r.Float() < 0.05 // the 40x y scale option is off in this config
			yaw += yaw
			seedT := r.Uint32() >> 1
			o.canyon(c, aq, cx, cz, centreX, centreZ, seedT, x, y, z, yaw, pitch, width, yScale)
		}
	}
}

func (o *Overworld) canyon(c *Chunk, aq *aquifer, cx, cz int32, centreX, centreZ float32, seed uint32, x, y, z, yaw, pitch, width, yScale float32) {
	t := rand.NewMT(seed)
	l := int32(120 - t.Uint32()%30)
	var rs [canyonTable]float32
	t2 := rand.NewMT(seed)
	f := t.Float()
	g := t.Float()*f + 1
	rs[0] = g * g
	for m := 1; m < 128; m++ {
		if t.IntN(3) == 0 {
			f := t.Float()
			g = t.Float()*f + 1
		}
		rs[m] = g * g
	}
	for n := 128; n < canyonTable; n++ {
		if t2.IntN(3) == 0 {
			f := t2.Float()
			g = t2.Float()*f + 1
		}
		rs[n] = g * g
	}
	reach := (width + 2) + 16
	reach2 := reach * reach
	fl := float32(l)
	var yawMod, pitchMod float32
	remaining := l
	for step := int32(1); ; step++ {
		radius := sin32((float32(step-1)*pi32)/fl)*width + 1.5
		hrDraw := t.Uint32()
		vrDraw := t.Uint32()
		cp := cos32(pitch)
		sp := sin32(pitch)
		x += cos32(yaw) * cp
		y += sp
		z += sin32(yaw) * cp
		pitch *= 0.7
		decayYaw, decayPitch := yawMod*0.5, pitchMod*0.8
		u1, u2, u3 := u(t), u(t), u(t)
		u4, u5, u6 := u(t), u(t), u(t)
		addYaw := (u6 * (u4 - u5)) * 4
		addPitch := (u3 * (u1 - u2)) * 2
		if t.Uint32()&3 != 0 {
			dx, dz := x-centreX, z-centreZ
			if (dx*dx+dz*dz)-float32(remaining)*float32(remaining) > reach2 {
				return
			}
			hr := (float32(float64(hrDraw)*2.3283064365386963e-10)*0.25 + 0.75) * radius
			vr := (float32(float64(vrDraw)*2.3283064365386963e-10*0.25) + 0.75) * (radius * yScale)
			if !(centreX+16+2*hr < x || x < centreX-16-2*hr || centreZ+16+2*hr < z || z < centreZ-16-2*hr) {
				o.carveCanyon(c, aq, cx, cz, x, y, z, hr, vr, &rs)
			}
		}
		if step >= l {
			return
		}
		pitch += pitchMod * 0.05
		yaw += yawMod * 0.05
		remaining--
		yawMod = addYaw + decayYaw
		pitchMod = addPitch + decayPitch
	}
}

// carveCanyon carves one canyon step (slot 0, new-world path).
func (o *Overworld) carveCanyon(c *Chunk, aq *aquifer, cx, cz int32, x, y, z, hr, vr float32, rs *[canyonTable]float32) {
	bx, bz := 16*cx, 16*cz
	minX := max(int32(float32(floorI(x-hr))-float32(bx)+-1), 0)
	maxX := min(int32(float32(floorI(x+hr))-float32(bx)+1), 16)
	yLo := int32(float32(floorI(y-vr)) + -1)
	yHi := int32(float32(floorI(y+vr)) + 1)
	top := int32(1)
	if yHi >= 2 {
		top = yHi
	}
	if canyonMaxY < yHi {
		top = canyonMaxY
	}
	bottom := int32(1)
	if yLo >= 2 {
		bottom = yLo
	}
	if top < yLo {
		bottom = top
	}
	minZ := max(int32(float32(floorI(z-hr))-float32(bz)+-1), 0)
	maxZ := min(int32(float32(floorI(z+hr))-float32(bz)+1), 16)
	if !(minX < maxX && minZ < maxZ && top > bottom) {
		return
	}
	for lx := minX; lx < maxX; lx++ {
		ex := ((float32(lx+bx) + 0.5) - x) / hr
		ex2 := ex * ex
		for lz := minZ; lz < maxZ; lz++ {
			ez := ((float32(lz+bz) + 0.5) - z) / hr
			d2 := ez*ez + ex2
			if !(d2 < 1) {
				continue
			}
			grass := false
			for ly := top; ly > bottom; ly-- {
				if ly-2 < 0 || ly-2 >= canyonTable {
					continue
				}
				ey := ((float32(ly-1) + 0.5) - y) / vr
				if !(rs[ly-2]*d2+(ey*ey)/6 < 1) {
					continue
				}
				blk := c.at(int(lx), ly, int(lz))
				grass = grass || blk == o.ids.grass
				if _, ok := o.ids.canyonCarvable[blk]; !ok {
					continue
				}
				wx, wy, wz := bx+lx, ly+MinY, bz+lz
				aq.sample(wx, wy, wz)
				if !(aq.density > 0) {
					fill := o.ids.air
					if aq.level > wy {
						fill = o.ids.water
						if aq.isLava {
							fill = o.ids.lava
						}
					}
					c.put(int(lx), ly, int(lz), fill)
				}
				if grass && ly > 0 {
					if below := c.at(int(lx), ly-1, int(lz)); below == o.ids.dirt || below == o.ids.coarseDirt {
						c.put(int(lx), ly-1, int(lz), o.ids.grass)
					}
				}
			}
		}
	}
}
