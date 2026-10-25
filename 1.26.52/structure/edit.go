package structure

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/helix-bedrock/worldgen/1.26.52/blocks"
)

// fullID adds "minecraft:" to an identifier without a namespace.
func fullID(id string) string {
	if strings.Contains(id, ":") {
		return id
	}
	return "minecraft:" + id
}

// SwapTable maps every state of block from to the state of block to with
// the properties both share (stairs keep their facing), for the Configs'
// block swaps. Names may leave out "minecraft:".
func SwapTable(reg *blocks.Registry, from, to string, into map[uint32]uint32) error {
	from, to = fullID(from), fullID(to)
	if _, ok := reg.Default(from); !ok {
		return fmt.Errorf("worldgen: block swap: unknown block %q", from)
	}
	toDefault, ok := reg.Default(to)
	if !ok {
		return fmt.Errorf("worldgen: block swap: unknown block %q", to)
	}
	toState, _ := reg.State(toDefault)
	for id := range uint32(reg.Len()) {
		st, _ := reg.State(id)
		if st.Name != from {
			continue
		}
		props := maps.Clone(toState.Props)
		for p := range props {
			if v, ok := st.Props[p]; ok {
				props[p] = v
			}
		}
		nid, ok := reg.Lookup(to, props)
		if !ok {
			nid = toDefault
		}
		into[id] = nid
	}
	return nil
}

// SwapChanged applies swap to the blocks that differ from before: the
// blocks one structure wrote.
func SwapChanged(blocks []uint16, before []uint16, swap map[uint32]uint32) {
	for i, b := range blocks {
		if i >= len(before) || b == before[i] {
			continue
		}
		if to, ok := swap[uint32(b)]; ok {
			blocks[i] = uint16(to)
		}
	}
}

// PoolEdit is a Config's change to a jigsaw pool: the weight of an element
// (by ID or template path), or, with Add, a template added with a weight.
type PoolEdit struct {
	Pool, Element string
	Weight        int32
	Add           bool
}

// EditPools applies edits to pools (each name may stand for several pool
// copies). Added templates must load from lib.
func EditPools(pools map[string][]*Pool, edits []PoolEdit, lib *Lib) error {
	for _, e := range edits {
		name := fullID(e.Pool)
		list, ok := pools[name]
		if !ok {
			return fmt.Errorf("worldgen: unknown jigsaw pool %q (pools: %s)", name, strings.Join(slices.Sorted(maps.Keys(pools)), ", "))
		}
		if e.Weight < 0 {
			return fmt.Errorf("worldgen: pool %q weight %d is below 0", name, e.Weight)
		}
		for _, p := range list {
			if e.Add {
				if lib.Template(e.Element) == nil {
					return fmt.Errorf("worldgen: add to pool %q: template %q is not in the packs or the vanilla data", name, e.Element)
				}
				el := &Element{ID: e.Element, Template: e.Element}
				if len(p.Entries) > 0 && p.Entries[0].Element != nil {
					el.TerrainMatching = p.Entries[0].Element.TerrainMatching
				}
				p.Entries = append(p.Entries, PoolEntry{Element: el, Weight: e.Weight})
				continue
			}
			found := false
			var names []string
			for i := range p.Entries {
				el := p.Entries[i].Element
				if el == nil {
					continue
				}
				names = append(names, el.ID)
				if el.ID == fullID(e.Element) || el.Template == e.Element {
					p.Entries[i].Weight = e.Weight
					found = true
				}
			}
			if !found {
				return fmt.Errorf("worldgen: unknown element %q of pool %s (elements: %s)", e.Element, name, strings.Join(names, ", "))
			}
		}
	}
	return nil
}

// CheckTemplates reports an error for a template replacement whose source
// is not a vanilla template or whose target does not load from lib.
func CheckTemplates(alias map[string]string, lib *Lib) error {
	for _, from := range slices.Sorted(maps.Keys(alias)) {
		if _, err := LoadAnyTemplate(from); err != nil {
			return fmt.Errorf("worldgen: replace template %q: no such vanilla template", from)
		}
		if lib.Template(alias[from]) == nil {
			return fmt.Errorf("worldgen: replace template %q: %q is not in the packs or the vanilla data", from, alias[from])
		}
	}
	return nil
}
