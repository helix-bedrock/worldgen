package overworld

import (
	"math"

	"github.com/helix-bedrock/worldgen/internal/mathx"

	"github.com/helix-bedrock/worldgen/noise"
	"github.com/helix-bedrock/worldgen/rand"
)

// caveNoises are the noises of the cave router: cave entrances, spaghetti
// tunnels (2D and 3D), cheese caves and pillars. BDS runs it on every density
// grid corner, before the slides.
type caveNoises struct {
	pillar, pillarRareness, pillarThickness             *noise.Normal
	spaghetti2D, spaghetti2DElevation                   *noise.Normal
	spaghetti2DModulator, spaghetti2DThickness          *noise.Normal
	spaghetti3D1, spaghetti3D2, spaghetti3DRarity       *noise.Normal
	spaghetti3DThickness, roughness, roughnessModulator *noise.Normal
	entrance, layer, cheese                             *noise.Normal
}

func newCaveNoises(world rand.Positional) caveNoises {
	n := func(name string, first int, amps ...float32) *noise.Normal {
		r := world.FromHash("minecraft:" + name)
		return noise.NewNormal(&r, first, amps)
	}
	return caveNoises{
		pillar:               n("pillar", -7, 1, 1),
		pillarRareness:       n("pillar_rareness", -8, 1),
		pillarThickness:      n("pillar_thickness", -8, 1),
		spaghetti2D:          n("spaghetti_2d", -7, 1),
		spaghetti2DElevation: n("spaghetti_2d_elevation", -8, 1),
		spaghetti2DModulator: n("spaghetti_2d_modulator", -11, 1),
		spaghetti2DThickness: n("spaghetti_2d_thickness", -11, 1),
		spaghetti3D1:         n("spaghetti_3d_1", -7, 1),
		spaghetti3D2:         n("spaghetti_3d_2", -7, 1),
		spaghetti3DRarity:    n("spaghetti_3d_rarity", -11, 1),
		spaghetti3DThickness: n("spaghetti_3d_thickness", -8, 1),
		roughness:            n("spaghetti_roughness", -5, 1),
		roughnessModulator:   n("spaghetti_roughness_modulator", -8, 1),
		entrance:             n("cave_entrance", -7, 0.4, 0.5, 1),
		layer:                n("cave_layer", -8, 1),
		cheese:               n("cave_cheese", -8, 0.5, 1, 2, 1, 2, 1, 0, 2, 0),
	}
}

// clamp1 limits v to [-1, 1] the way BDS does: min(1, v), then -1 if v < -1.
func clamp1(v float32) float32 {
	if v < -1 {
		return -1
	}
	return min(1, v)
}

// caveDensityScale is BDS's density scale (128 times Java's).
const caveDensityScale = 128

// caveMinElevation is the dimension's minimum height times 1/8.
const caveMinElevation = float32(MinY) * 0.125

// route applies the cave router to the density d at block (x, y, z), a grid
// corner. Near and above the surface (d < 200) only entrances and 3D
// spaghetti cut in; deeper, cheese caves, 2D spaghetti and pillars too.
func (c *caveNoises) route(x, y, z, d float32) float32 {
	if d < -150 {
		return d
	}
	rough := ((c.roughnessModulator.Sample(x, y, z)+1)*0.5)*0.1 + 0
	rough *= 0.4 - mathx.AbsFloat(c.roughness.Sample(x, y, z))

	rarity := float32(0.75)
	if v := c.spaghetti3DRarity.Sample(x+x, y, z+z); v >= -0.5 {
		switch {
		case v < 0:
			rarity = 1
		case v < 0.5:
			rarity = 1.5
		default:
			rarity = 2
		}
	}
	thick := ((c.spaghetti3DThickness.Sample(x, y, z)+1)*0.5)*0.023000002 + 0.065
	inv := 1 / rarity
	a := mathx.AbsFloat(c.spaghetti3D1.Sample(x*inv, y*inv, inv*z)*rarity) - thick
	b := mathx.AbsFloat(c.spaghetti3D2.Sample(x*inv, y*inv, inv*z)*rarity) - thick
	spaghetti3D := clamp1(max(b, a))

	ent := c.entrance.Sample(x*0.75, y*0.5, 0.75*z) + 0.37
	var grad float32
	if g := float32(float64(y+10) / 40); g < 0 {
		grad = 0.3
	} else if g <= 1 {
		grad = g*-0.3 + 0.3
	}
	entrances := min(spaghetti3D+rough, ent+grad)
	if d < 200 {
		return min((entrances*caveDensityScale)*5, d)
	}

	cheese := clamp1(c.cheese.Sample(x, 0.6666667*y, z) + 0.27)
	var fade float32 = 0.5
	if t := (d - 200) * 1.28; t >= 0 {
		fade = 0
		if t <= 1 {
			fade = t*-0.5 + 0.5
		}
	}
	l := c.layer.Sample(x, 8*y, z)
	caves := cheese + fade + (l*l)*4

	scale2D := float32(0.5)
	if v := c.spaghetti2DModulator.Sample(x+x, y, z+z); v >= -0.75 {
		switch {
		case v < -0.5:
			scale2D = 0.75
		case v < 0.5:
			scale2D = 1
		case v < 0.75:
			scale2D = 2
		default:
			scale2D = 3
		}
	}
	thick2D := c.spaghetti2DThickness.Sample(x+x, y, z+z)
	inv2D := 1 / scale2D
	sp2D := c.spaghetti2D.Sample(x*inv2D, y*inv2D, inv2D*z)
	elev := float32(caveMinElevation)
	if e := c.spaghetti2DElevation.Sample(x, 0, z); mathx.AbsFloat(caveMinElevation-8) > 0.00000011920929 {
		elev = caveMinElevation + (8-caveMinElevation)*((e+1)*0.5)
	}
	t2 := ((thick2D+1)*0.5)*0.6999999 + 0.6
	h := mathx.AbsFloat(-0.125*y+elev) - t2
	spaghetti2D := clamp1(max((h*h)*h, -0.083*t2+mathx.AbsFloat(scale2D*sp2D)))
	caves = min(caves, min(entrances, rough+spaghetti2D))

	rare := c.pillarRareness.Sample(x, y, z)
	rare = ((rare+1)*0.5 + (rare+1)*0.5) + 0
	pt := c.pillarThickness.Sample(x, y, z)
	pthick := float32(math.Pow(float64(((pt+1)*0.5)*1.1+0), 3))
	p := c.pillar.Sample(x*25, y*0.3, z*25)
	pv := ((p + p) - rare) * pthick
	if !(0.03 < pv) {
		pv = -math.MaxFloat32
	}
	return max(caves, pv) * caveDensityScale
}
