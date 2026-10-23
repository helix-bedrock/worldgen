package overworld

import (
	"fmt"
	"log/slog"
	"slices"
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
	kinds := StructureKinds()
	index := func(k StructureKind) (structKind, error) {
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
	g := structGates{log: log, filters: slices.Clone(c.filters)}
	copy(g.names[:], kinds)
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
