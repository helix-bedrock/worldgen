package structure

import (
	"fmt"
	"log/slog"
)

// Area is a box of block columns, corners included.
type Area struct{ MinX, MinZ, MaxX, MaxZ int32 }

// NewArea returns the area between two corners, in any order.
func NewArea(x0, z0, x1, z1 int32) Area {
	return Area{min(x0, x1), min(z0, z1), max(x0, x1), max(z0, z1)}
}

// HoldsChunk reports whether the centre of chunk (cx, cz) is in the area.
func (a Area) HoldsChunk(cx, cz int32) bool {
	x, z := cx*16+8, cz*16+8
	return x >= a.MinX && x <= a.MaxX && z >= a.MinZ && z <= a.MaxZ
}

// Filter decides whether a structure of a kind may start in a chunk. It
// must give the same answer every time for the same arguments and may be
// called from many goroutines at once.
type Filter func(kind string, chunkX, chunkZ int32) bool

// Gate holds a dimension Config's structure changes: kinds turned off,
// areas kept clear and filters. Kinds are indices into the names given to
// NewGate. The zero Gate lets everything start.
type Gate struct {
	any      bool
	names    []string
	disabled []bool
	excludes []gateExclude
	filters  []Filter
	log      *slog.Logger
}

type gateExclude struct {
	area  Area
	kinds []bool
}

// NewGate returns a Gate over the kind names; log receives filter panics.
func NewGate(names []string, log *slog.Logger) *Gate {
	return &Gate{names: names, disabled: make([]bool, len(names)), log: log}
}

// Disable turns kind off.
func (g *Gate) Disable(kind int) {
	g.disabled[kind], g.any = true, true
}

// Disabled reports whether kind is off.
func (g *Gate) Disabled(kind int) bool { return g.disabled[kind] }

// Exclude keeps the kinds (all when none are given) from starting in area.
func (g *Gate) Exclude(area Area, kinds ...int) {
	e := gateExclude{area: area, kinds: make([]bool, len(g.names))}
	for i := range e.kinds {
		e.kinds[i] = len(kinds) == 0
	}
	for _, k := range kinds {
		e.kinds[k] = true
	}
	g.excludes, g.any = append(g.excludes, e), true
}

// AddFilter adds a filter every start must pass.
func (g *Gate) AddFilter(f Filter) error {
	if f == nil {
		return fmt.Errorf("worldgen: nil structure filter")
	}
	g.filters, g.any = append(g.filters, f), true
	return nil
}

// Name is kind's name.
func (g *Gate) Name(kind int) string { return g.names[kind] }

// OK reports whether kind may start in chunk (cx, cz). A filter that
// panics counts as no, and is logged.
func (g *Gate) OK(kind int, cx, cz int32) bool {
	if g == nil || !g.any {
		return true
	}
	if g.disabled[kind] {
		return false
	}
	for i := range g.excludes {
		if g.excludes[i].kinds[kind] && g.excludes[i].area.HoldsChunk(cx, cz) {
			return false
		}
	}
	for _, f := range g.filters {
		if !g.call(f, kind, cx, cz) {
			return false
		}
	}
	return true
}

func (g *Gate) call(f Filter, kind int, cx, cz int32) (ok bool) {
	defer func() {
		if p := recover(); p != nil {
			ok = false
			if g.log != nil {
				g.log.Error("worldgen: structure filter panicked", "structure", g.names[kind], "panic", fmt.Sprint(p),
					"chunkX", cx, "chunkZ", cz)
			}
		}
	}()
	return f(g.names[kind], cx, cz)
}
