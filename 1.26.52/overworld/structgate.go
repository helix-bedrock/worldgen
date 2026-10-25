package overworld

import (
	"fmt"
	"log/slog"
	"slices"

	"github.com/helix-bedrock/worldgen/1.26.52/structure"
)

// structKind is a structure kind's index in StructureKinds.
type structKind uint8

// The structure kinds, in StructureKinds order.
const (
	skVillage structKind = iota
	skMineshaft
	skStronghold
	skAncientCity
	skDesertPyramid
	skJungleTemple
	skSwampHut
	skIgloo
	skOutpost
	skMansion
	skMonument
	skOceanRuin
	skShipwreck
	skBuriedTreasure
	skRuinedPortal
	skTrailRuins
	skTrialChambers
	skAbandonedCamp
	structKindCount
)

// newStructGates resolves the structure changes; skipAll (Steps.Structures)
// disables every kind, so locating finds none either.
func newStructGates(c *StructureConfig, skipAll bool, log *slog.Logger) (*structure.Gate, error) {
	kinds := StructureKinds()
	names := make([]string, len(kinds))
	for i, k := range kinds {
		names[i] = string(k)
	}
	g := structure.NewGate(names, log)
	for _, k := range c.disabled {
		i, err := structIndex(k)
		if err != nil {
			return nil, err
		}
		g.Disable(int(i))
	}
	for _, e := range c.excludes {
		idx := make([]int, 0, len(e.kinds))
		for _, k := range e.kinds {
			i, err := structIndex(k)
			if err != nil {
				return nil, err
			}
			idx = append(idx, int(i))
		}
		g.Exclude(e.area, idx...)
	}
	for _, f := range c.filters {
		if f == nil {
			return nil, fmt.Errorf("worldgen: nil structure filter")
		}
		if err := g.AddFilter(f.forGate); err != nil {
			return nil, err
		}
	}
	if skipAll {
		for i := range names {
			g.Disable(i)
		}
	}
	return g, nil
}

// forGate is the filter as the shared structure gate calls it.
func (f StructureFilter) forGate(kind string, cx, cz int32) bool {
	return f(StructureKind(kind), cx, cz)
}

// structOK reports whether a structure of kind k may start in chunk
// (cx, cz).
func (o *Overworld) structOK(k structKind, cx, cz int32) bool { return o.gates.OK(int(k), cx, cz) }

// startGrid is a structure kind's placement grid: one start per spacing x
// spacing chunk region, at least separation chunks from the region's far
// edges, drawn from a random salted with salt. chance is the mineshafts'
// per-chunk chance instead.
type startGrid struct {
	spacing, separation, salt int32
	chance                    float32
}

// mineshaftChance is the chance of a mineshaft in a chunk (sub_4E8BA90).
const mineshaftChance = 0.004

// vanillaPlacements are the placement grids of BDS 1.26.52. The four
// scattered kinds share one grid, kept in the desert pyramid's slot. The
// data-driven jigsaws take theirs from their structure sets when loaded;
// strongholds are placed in rings, not on a grid.
func vanillaPlacements() [structKindCount]startGrid {
	var p [structKindCount]startGrid
	p[skVillage] = startGrid{spacing: villageSpacing, separation: villageSeparation, salt: villageSalt}
	p[skAncientCity] = startGrid{spacing: citySpacing, separation: citySeparation, salt: citySalt}
	p[skBuriedTreasure] = startGrid{spacing: treasureSpacing, separation: treasureSeparation, salt: treasureSalt}
	p[skMansion] = startGrid{spacing: mansionSpacing, separation: mansionSeparation, salt: mansionSalt}
	p[skOceanRuin] = startGrid{spacing: ruinSpacing, separation: ruinSeparation, salt: ruinSalt}
	p[skMonument] = startGrid{spacing: monumentSpacing, separation: monumentSeparation, salt: monumentSalt}
	p[skOutpost] = startGrid{spacing: outpostSpacing, separation: outpostSeparation, salt: outpostSalt}
	p[skDesertPyramid] = startGrid{spacing: scatterSpacing, separation: scatterSeparation, salt: scatterSalt}
	p[skShipwreck] = startGrid{spacing: shipSpacing, separation: shipSeparation, salt: shipSalt}
	p[skRuinedPortal] = startGrid{spacing: structure.PortalSpacing, separation: structure.PortalSeparation, salt: structure.PortalSalt}
	p[skMineshaft] = startGrid{chance: mineshaftChance}
	return p
}

// placementSlot is the slot holding a kind's grid: the scattered kinds
// share the desert pyramid's.
func placementSlot(k structKind) structKind {
	switch k {
	case skJungleTemple, skSwampHut, skIgloo:
		return skDesertPyramid
	}
	return k
}

// structIndex is the index of a StructureKind.
func structIndex(k StructureKind) (structKind, error) {
	kinds := StructureKinds()
	i := slices.Index(kinds, StructureKind(fullID(string(k))))
	if i < 0 {
		names := make([]string, len(kinds))
		for j, k := range kinds {
			names[j] = string(k)
		}
		return 0, unknownName("structure", fullID(string(k)), names)
	}
	return structKind(i), nil
}

// regionOf is the placement region of chunk (cx, cz) for kind k: the
// chunk itself for kinds without a grid.
func (o *Overworld) regionOf(k structKind, cx, cz int32) [2]int32 {
	sp := o.place[placementSlot(k)].spacing
	if sp <= 0 {
		return [2]int32{cx, cz}
	}
	return [2]int32{structure.Region(cx, sp), structure.Region(cz, sp)}
}

// applyPlacements applies the placement changes and resolves the forced
// starts. It runs after the data-driven jigsaws are loaded, whose grids
// come from their structure sets.
func (o *Overworld) applyPlacements(c *StructureConfig) error {
	for _, d := range o.dataJigsaws {
		o.place[d.sk] = startGrid{spacing: d.spacing, separation: d.separation, salt: d.salt}
	}
	for _, kp := range c.placements {
		k, err := structIndex(kp.kind)
		if err != nil {
			return err
		}
		if k == skStronghold {
			return fmt.Errorf("worldgen: strongholds are placed in rings and have no placement grid")
		}
		g := &o.place[placementSlot(k)]
		if kp.p.Chance != 0 {
			if k != skMineshaft {
				return fmt.Errorf("worldgen: %s has a placement grid, not a chance", kp.kind)
			}
			if kp.p.Chance < 0 || kp.p.Chance > 1 {
				return fmt.Errorf("worldgen: mineshaft chance %v is not between 0 and 1", kp.p.Chance)
			}
			g.chance = kp.p.Chance
		}
		if k == skMineshaft {
			if kp.p.Spacing != 0 || kp.p.Separation != 0 || kp.p.Salt != 0 {
				return fmt.Errorf("worldgen: mineshafts have a chance per chunk, not a placement grid")
			}
			continue
		}
		if kp.p.Spacing != 0 {
			g.spacing = kp.p.Spacing
		}
		if kp.p.Separation != 0 {
			g.separation = kp.p.Separation
		}
		if kp.p.Salt != 0 {
			g.salt = kp.p.Salt
		}
		if g.spacing <= 0 || g.separation < 0 || g.spacing-g.separation < 1 {
			return fmt.Errorf("worldgen: %s spacing %d must be above 0 and above separation %d", kp.kind, g.spacing, g.separation)
		}
	}
	for _, d := range o.dataJigsaws {
		g := o.place[d.sk]
		d.spacing, d.separation, d.salt = g.spacing, g.separation, g.salt
	}
	for _, f := range c.forced {
		k, err := structIndex(f.kind)
		if err != nil {
			return err
		}
		if o.gates.Disabled(int(k)) {
			return fmt.Errorf("worldgen: %s is placed at chunk %d %d but disabled", f.kind, f.cx, f.cz)
		}
		if o.forced[k] == nil {
			o.forced[k] = map[[2]int32][2]int32{}
		}
		reg := o.regionOf(k, f.cx, f.cz)
		// The scattered kinds share one grid, so one start per region
		// among all four.
		for _, other := range []structKind{skDesertPyramid, skJungleTemple, skSwampHut, skIgloo, k} {
			if placementSlot(other) != placementSlot(k) {
				continue
			}
			if at, ok := o.forced[other][reg]; ok && (other != k || at != [2]int32{f.cx, f.cz}) {
				return fmt.Errorf("worldgen: %s at chunk %d %d shares its placement region with another placed start at chunk %d %d",
					f.kind, f.cx, f.cz, at[0], at[1])
			}
		}
		o.forced[k][reg] = [2]int32{f.cx, f.cz}
		o.anyForced = true
		if k == skStronghold && !slices.Contains(o.forcedStrongholds, [2]int32{f.cx, f.cz}) {
			o.forcedStrongholds = append(o.forcedStrongholds, [2]int32{f.cx, f.cz})
		}
	}
	slices.SortFunc(o.forcedStrongholds, func(a, b [2]int32) int {
		if a[0] != b[0] {
			return int(a[0] - b[0])
		}
		return int(a[1] - b[1])
	})
	return nil
}

// forcedPick returns the placed start of region (rx, rz) of kind k.
func (o *Overworld) forcedPick(k structKind, rx, rz int32) (int32, int32, bool) {
	if !o.anyForced || o.forced[k] == nil {
		return 0, 0, false
	}
	at, ok := o.forced[k][[2]int32{rx, rz}]
	return at[0], at[1], ok
}

// isForced reports whether chunk (cx, cz) holds a placed start of kind k.
func (o *Overworld) isForced(k structKind, cx, cz int32) bool {
	if !o.anyForced || o.forced[k] == nil {
		return false
	}
	at, ok := o.forced[k][o.regionOf(k, cx, cz)]
	return ok && at == [2]int32{cx, cz}
}

// Placement returns the placement grid kind uses in this generator, with
// the Config's changes; ok is false for strongholds, which are placed in
// rings.
func (o *Overworld) Placement(kind StructureKind) (p Placement, ok bool) {
	k, err := structIndex(kind)
	if err != nil || k == skStronghold {
		return Placement{}, false
	}
	g := o.place[placementSlot(k)]
	return Placement{Spacing: g.spacing, Separation: g.separation, Salt: g.salt, Chance: g.chance}, true
}

// applyStructureBiomes applies SetBiomes, after the vanilla biome sets are
// built.
func (o *Overworld) applyStructureBiomes(c *StructureConfig, biomeIDs map[string]uint32) error {
	n := uint32(len(o.biomeTags))
	for _, kb := range c.biomes {
		k, err := structIndex(kb.kind)
		if err != nil {
			return err
		}
		mask := make([]bool, n)
		for _, name := range kb.biomes {
			id, ok := biomeIDs[biomeName(name)]
			if !ok {
				return unknownName("biome", biomeName(name), sortedKeys(biomeIDs))
			}
			mask[id] = true
		}
		switch k {
		case skVillage:
			o.villageBiome = mask
		case skAncientCity:
			o.cityBiome = mask
		case skDesertPyramid, skJungleTemple, skSwampHut, skIgloo:
			sk := scatterKindOf(k)
			for b := range o.scatterKind {
				if o.scatterKind[b] == sk {
					o.scatterKind[b] = scatterNone
				}
				if mask[b] {
					o.scatterKind[b] = sk
				}
			}
		case skOutpost:
			o.outpostBiome = mask
		case skMansion:
			o.mansionBiome = mask
		case skBuriedTreasure:
			o.treasureBiome = mask
		case skShipwreck:
			o.shipBiome = mask
		case skMonument:
			o.monumentDeep, o.monumentWater = mask, mask
		case skOceanRuin:
			table := make([]ruinBiome, n)
			for b := range table {
				if !mask[b] {
					continue
				}
				table[b] = o.ruinOcean
				if b < len(o.ruinBiome) && o.ruinBiome[b].ok {
					table[b] = o.ruinBiome[b]
				}
			}
			o.ruinBiome = table
		case skTrailRuins, skTrialChambers, skAbandonedCamp:
			for _, d := range o.dataJigsaws {
				if d.sk != k {
					continue
				}
				d.biomes, d.biomeTags = mask, nil
				for _, v := range d.variants {
					v.d.biomes, v.d.biomeTags = mask, nil
				}
			}
		default:
			return fmt.Errorf("worldgen: %s has no biome list", kb.kind)
		}
	}
	return nil
}
