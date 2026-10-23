package overworld

import (
	"fmt"
	"log/slog"
	"slices"

	"github.com/helix-bedrock/worldgen/structure"
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

// structGates are the Config's structure changes, resolved.
type structGates struct {
	// any is false for a vanilla Config, which skips every check.
	any      bool
	disabled [structKindCount]bool
	excludes []gateExclude
	filters  []StructureFilter
	log      *slog.Logger
	// names are the kinds' StructureKind names, by index.
	names [structKindCount]StructureKind
}

type gateExclude struct {
	area  Area
	kinds [structKindCount]bool
}

// newStructGates resolves the structure changes; skipAll (Steps.Structures)
// disables every kind, so locating finds none either.
func newStructGates(c *StructureConfig, skipAll bool, log *slog.Logger) (structGates, error) {
	index := structIndex
	g := structGates{log: log, filters: slices.Clone(c.filters)}
	copy(g.names[:], StructureKinds())
	for _, k := range c.disabled {
		i, err := index(k)
		if err != nil {
			return g, err
		}
		g.disabled[i] = true
	}
	for _, e := range c.excludes {
		ge := gateExclude{area: e.area}
		if len(e.kinds) == 0 {
			for i := range ge.kinds {
				ge.kinds[i] = true
			}
		}
		for _, k := range e.kinds {
			i, err := index(k)
			if err != nil {
				return g, err
			}
			ge.kinds[i] = true
		}
		g.excludes = append(g.excludes, ge)
	}
	for _, f := range g.filters {
		if f == nil {
			return g, fmt.Errorf("worldgen: nil structure filter")
		}
	}
	g.any = len(c.disabled) > 0 || len(g.excludes) > 0 || len(g.filters) > 0
	if skipAll {
		g.any = true
		for i := range g.disabled {
			g.disabled[i] = true
		}
	}
	return g, nil
}

// structOK reports whether a structure of kind k may start in chunk
// (cx, cz).
func (o *Overworld) structOK(k structKind, cx, cz int32) bool {
	g := &o.gates
	if !g.any {
		return true
	}
	if g.disabled[k] {
		return false
	}
	for i := range g.excludes {
		if g.excludes[i].kinds[k] && g.excludes[i].area.holdsChunk(cx, cz) {
			return false
		}
	}
	for _, f := range g.filters {
		if !g.callFilter(f, k, cx, cz) {
			return false
		}
	}
	return true
}

// callFilter runs a user filter; a panic counts as no.
func (g *structGates) callFilter(f StructureFilter, k structKind, cx, cz int32) (ok bool) {
	kind := g.names[k]
	defer func() {
		if p := recover(); p != nil {
			ok = false
			if g.log != nil {
				g.log.Error("worldgen: structure filter panicked", "structure", string(kind), "panic", fmt.Sprint(p),
					"chunkX", cx, "chunkZ", cz)
			}
		}
	}()
	return f(kind, cx, cz)
}

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
		if k == skStronghold {
			return fmt.Errorf("worldgen: strongholds cannot be placed at a position yet")
		}
		if o.gates.disabled[k] {
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
	}
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
