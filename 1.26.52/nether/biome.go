package nether

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"sort"
	"strings"

	worldgen "github.com/helix-bedrock/worldgen/1.26.52"
	"github.com/helix-bedrock/worldgen/1.26.52/noise"
	"github.com/helix-bedrock/worldgen/1.26.52/rand"
)

// The Nether's biomes come from a small node graph (BDS's
// NodeGraphBiomeSource, built in sub_515FDF0): four climate noises at
// quarter resolution, a nearest-climate pick among the biomes with
// minecraft:multinoise_generation_rules, then a 4x Voronoi zoom to blocks.
// It is 2D: a column has one biome at every height.

// layer LCG constants (Java's old biome layers).
const (
	lcgMul = 0x5851F42D4C957F2D
	lcgAdd = 0x14057B7EF767814F
	// zoomSalt is the Voronoi zoom node's salt.
	zoomSalt = -0x794549289D90B428 // 0x86BAB6D7626F4BD8 as int64
)

func mix(a, b int64) int64 { return a*(a*lcgMul+lcgAdd) + b }

// climateNoise is one climate channel: two sets of two old-style octaves,
// the second sampled at a slightly larger scale (sub_519BEF0, sub_519C330).
type climateNoise struct {
	a, b       *noise.LegacyOctaves
	scaleA     float32
	scaleB     float32
	valueScale float32
}

// newClimateNoise seeds channel ch: its own Mersenne Twister on the low 32
// bits of the seed plus the channel index.
func newClimateNoise(seed int64, ch int32) climateNoise {
	const octaves = 2
	mt := rand.NewMT(uint32(int32(seed) + ch))
	c := climateNoise{a: noise.NewLegacyOctaves(mt, octaves), b: noise.NewLegacyOctaves(mt, octaves)}
	// Each set is normalised by 1 / sum(2^i) over its octaves.
	c.scaleA = 1 / float32(1+2)
	c.scaleB = c.scaleA
	c.valueScale = 0.16666667 / float32((1/float64(octaves+1)+1)*0.1)
	return c
}

// at is the channel's value at quarter position (qx, qz).
func (c *climateNoise) at(qx, qz int32) float32 {
	x, z := float32(qx), float32(qz)
	a := c.a.Sample(x*0.015625, 0, z*0.015625) * c.scaleA
	b := c.b.Sample(float32(x*1.0181268)*0.015625, 0, float32(z*1.0181268)*0.015625) * c.scaleB
	return (b + a) * c.valueScale
}

// netherBiome is one biome's climate target.
type netherBiome struct {
	id                      uint32
	temp, humid, alt, weird float32
	weight                  float32
}

// biomeSource picks the Nether's biomes. It is immutable after creation and
// safe for concurrent use.
type biomeSource struct {
	climate [4]climateNoise
	biomes  []netherBiome
	// zoomSeed is the zoom node's layer seed.
	zoomSeed int64
	// remap replaces picked biomes (the Config's Replace and Only).
	remap map[uint32]uint32
}

func newBiomeSource(seed int64, biomeIDs map[string]uint32) (*biomeSource, error) {
	s := &biomeSource{}
	for ch := range s.climate {
		s.climate[ch] = newClimateNoise(seed, int32(ch))
	}
	t := mix(seed, zoomSalt)
	t = mix(t, zoomSalt)
	s.zoomSeed = mix(t, zoomSalt)
	err := fs.WalkDir(worldgen.Data, "vanilla/biomes", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, err := fs.ReadFile(worldgen.Data, p)
		if err != nil {
			return err
		}
		var f biomeFile
		if err := json.Unmarshal(raw, &f); err != nil {
			return fmt.Errorf("nether: read %s: %w", p, err)
		}
		// BDS keeps the multinoise rules whose dimension is
		// minecraft:nether (sub_515FDF0); in the vanilla data those are the
		// biomes tagged nether. Some overworld biomes carry old rules too.
		// VERIFY: how BDS gives a biome's rules their dimension.
		r := f.Biome.Components.Rules
		if r == nil || !slices.Contains(f.Biome.Components.Tags.Tags, "nether") {
			return nil
		}
		name := strings.TrimSuffix(path.Base(p), ".biome.json")
		id, ok := biomeIDs[name]
		if !ok {
			return fmt.Errorf("nether: biome %s missing from the biome list", name)
		}
		s.biomes = append(s.biomes, netherBiome{id, r.Temp, r.Humid, r.Alt, r.Weird, r.Weight})
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(s.biomes) == 0 {
		return nil, fmt.Errorf("nether: no biome has minecraft:multinoise_generation_rules")
	}
	// The first biome in the registry wins a tie.
	// VERIFY: registry order taken as biome ID order.
	sort.Slice(s.biomes, func(i, j int) bool { return s.biomes[i].id < s.biomes[j].id })
	return s, nil
}

// pick is the biome nearest the climate at quarter position (qx, qz)
// (sub_519CB70): the weight counts as a fifth, squared distance.
func (s *biomeSource) pick(qx, qz int32) uint32 {
	t, h, a, w := s.climate[0].at(qx, qz), s.climate[1].at(qx, qz), s.climate[2].at(qx, qz), s.climate[3].at(qx, qz)
	best, bestD := s.biomes[0].id, float32(0)
	for i, b := range s.biomes {
		d := (t-b.temp)*(t-b.temp) + (h-b.humid)*(h-b.humid) + (a-b.alt)*(a-b.alt) + (w-b.weird)*(w-b.weird) + b.weight*b.weight
		if i == 0 || d < bestD {
			best, bestD = b.id, d
		}
	}
	if to, ok := s.remap[best]; ok {
		return to
	}
	return best
}

// fill writes the biome of every column of chunk (chunkX, chunkZ), index
// z<<4 | x: Java's old Voronoi zoom, in float, over the quarter grid.
func (s *biomeSource) fill(chunkX, chunkZ int32, out *[256]uint16) {
	c := quarterCache{s: s, picked: map[[2]int32]uint32{}}
	for z := range int32(16) {
		for x := range int32(16) {
			out[z<<4|x] = uint16(s.zoom(chunkX*16+x, chunkZ*16+z, c.pick))
		}
	}
}

// quarterCache remembers the biomes picked on the quarter grid while one
// chunk is filled.
type quarterCache struct {
	s      *biomeSource
	picked map[[2]int32]uint32
}

// pick is biomeSource.pick, asked at most once per quarter position.
func (c *quarterCache) pick(qx, qz int32) uint32 {
	k := [2]int32{qx, qz}
	if b, ok := c.picked[k]; ok {
		return b
	}
	b := c.s.pick(qx, qz)
	c.picked[k] = b
	return b
}

// biomeFile is the part of a vanilla biome file the Nether reads.
type biomeFile struct {
	Biome struct {
		Components struct {
			Tags struct {
				Tags []string `json:"tags"`
			} `json:"minecraft:tags"`
			Rules *multinoiseRules `json:"minecraft:multinoise_generation_rules"`
		} `json:"components"`
	} `json:"minecraft:biome"`
}

// multinoiseRules is a biome's minecraft:multinoise_generation_rules.
type multinoiseRules struct {
	Temp   float32 `json:"target_temperature"`
	Humid  float32 `json:"target_humidity"`
	Alt    float32 `json:"target_altitude"`
	Weird  float32 `json:"target_weirdness"`
	Weight float32 `json:"weight"`
}

// column is the biome of block column (x, z), as fill gives it.
func (s *biomeSource) column(x, z int32) uint32 {
	return s.zoom(x, z, s.pick)
}

// zoom picks the biome of block column (x, z) from the quarter grid at.
func (s *biomeSource) zoom(x, z int32, at func(qx, qz int32) uint32) uint32 {
	i, j := x-2, z-2
	k, l := i>>2, j>>2
	// The four corners' jitter, x then z each.
	var ox, oz [4]float32
	for c := range 4 {
		cx, cz := k<<2, l<<2
		if c&1 != 0 {
			cx += 4
		}
		if c&2 != 0 {
			cz += 4
		}
		r := mix(mix(mix(mix(s.zoomSeed, int64(cx)), int64(cz)), int64(cx)), int64(cz))
		dx := float32(float32(float32((r>>24)&1023)*0.0009765625)+-0.5) * 3.6
		r = mix(r, s.zoomSeed)
		dz := float32(float32(float32((r>>24)&1023)*0.0009765625)+-0.5) * 3.6
		if c&1 != 0 {
			dx += 4
		}
		if c&2 != 0 {
			dz += 4
		}
		ox[c], oz[c] = dx, dz
	}
	fx, fz := float32(i&3), float32(j&3)
	var d [4]float32
	for c := range 4 {
		d[c] = (ox[c]-fx)*(ox[c]-fx) + (oz[c]-fz)*(oz[c]-fz)
	}
	switch {
	case d[0] < d[1] && d[0] < d[2] && d[0] < d[3]:
		return at(k, l)
	case d[1] < d[0] && d[1] < d[2] && d[1] < d[3]:
		return at(k+1, l)
	case d[2] < d[0] && d[2] < d[1] && d[2] < d[3]:
		return at(k, l+1)
	}
	return at(k+1, l+1)
}
