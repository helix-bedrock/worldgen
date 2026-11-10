package blocks

import "fmt"

// Source is a server's own block table, for building a Registry from it
// instead of from an exported palette. Index i is state i of the Registry.
type Source struct {
	// States are every block state, in the server's own order.
	States []State
	// Defaults gives the index of each block's default state, by block name.
	Defaults map[string]uint32
	// Tags are each block's tags as BDS holds them ("minecraft:leaves",
	// "dirt"), by block name.
	Tags map[string][]string
	// Light is each state's light dampening, 0 to 15. Nil means air 0 and
	// everything else 15.
	Light []uint8
	// Translucency is each state's translucency, 0 (opaque) to 1. Nil means
	// 1 for states that do not dampen light and 0 for the rest.
	Translucency []float32
}

// New builds a Registry from a server's block table, so the generator places
// the server's own state indices. Property values must be uint8, int32 or
// string, as in Load.
func New(src Source) (*Registry, error) {
	n := len(src.States)
	if src.Light != nil && len(src.Light) != n || src.Translucency != nil && len(src.Translucency) != n {
		return nil, fmt.Errorf("blocks: %d states but %d light and %d translucency values", n, len(src.Light), len(src.Translucency))
	}
	reg := &Registry{
		states:       make([]State, n),
		byHash:       make(map[uint32]uint32, n),
		defaults:     make(map[string]uint32, len(src.Defaults)),
		tags:         make(map[string][]string, len(src.Tags)),
		light:        src.Light,
		translucency: src.Translucency,
	}
	for i, s := range src.States {
		if s.Props == nil {
			s.Props = map[string]any{}
		}
		reg.states[i] = s
		reg.byHash[Hash(s)] = uint32(i) //nolint:gosec // state counts fit uint32
	}
	for name, d := range src.Defaults {
		if int(d) >= n {
			return nil, fmt.Errorf("blocks: default state %d of %s is out of range", d, name)
		}
		reg.defaults[name] = d
	}
	for name, t := range src.Tags {
		if len(t) > 0 {
			reg.tags[name] = t
		}
	}
	return reg, nil
}
