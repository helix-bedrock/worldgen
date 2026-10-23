package overworld

import (
	"fmt"
	"maps"
	"slices"

	"github.com/helix-bedrock/worldgen/structure"
)

// blockSnapshot is a copy of a chunk's blocks, to find what a structure
// changed.
type blockSnapshot = [16 * 16 * Height]uint16

// applyStructureContents applies the template, pool, loot and block swap
// changes, after the structures are loaded.
func (o *Overworld) applyStructureContents(c *StructureConfig) error {
	for _, from := range slices.Sorted(maps.Keys(c.templates)) {
		if _, err := structure.LoadAnyTemplate(from); err != nil {
			return fmt.Errorf("worldgen: replace template %q: no such vanilla template", from)
		}
		if o.st.Template(c.templates[from]) == nil {
			return fmt.Errorf("worldgen: replace template %q: %q is not in the packs or the vanilla data", from, c.templates[from])
		}
	}
	if err := o.applyPools(c.pools); err != nil {
		return err
	}
	o.loot = maps.Clone(c.loot)
	for _, s := range c.swaps {
		k, err := structIndex(s.kind)
		if err != nil {
			return err
		}
		from, to := fullID(s.from), fullID(s.to)
		if _, ok := o.reg.Default(from); !ok {
			return fmt.Errorf("worldgen: block swap: unknown block %q", from)
		}
		toDefault, ok := o.reg.Default(to)
		if !ok {
			return fmt.Errorf("worldgen: block swap: unknown block %q", to)
		}
		toState, _ := o.reg.State(toDefault)
		if o.swaps[k] == nil {
			o.swaps[k] = map[uint32]uint32{}
		}
		for id := range uint32(o.reg.Len()) {
			st, _ := o.reg.State(id)
			if st.Name != from {
				continue
			}
			// The target's default state, with the properties both share
			// taken from the source state.
			props := maps.Clone(toState.Props)
			for p := range props {
				if v, ok := st.Props[p]; ok {
					props[p] = v
				}
			}
			nid, ok := o.reg.Lookup(to, props)
			if !ok {
				nid = toDefault
			}
			o.swaps[k][id] = nid
		}
	}
	return nil
}

// applyPools applies SetPoolWeight and AddToPool.
func (o *Overworld) applyPools(edits []poolEdit) error {
	if len(edits) == 0 {
		return nil
	}
	pools := map[string][]*structure.Pool{}
	add := func(m map[string]*structure.Pool) {
		for name, p := range m {
			pools[name] = append(pools[name], p)
		}
	}
	if o.jigsaw != nil {
		add(o.jigsaw.Pools)
	}
	if o.city != nil {
		add(o.city.Pools)
	}
	for _, d := range o.dataJigsaws {
		add(d.pools)
		for _, v := range d.variants {
			add(v.d.pools)
		}
	}
	for _, e := range edits {
		name := fullID(e.pool)
		list, ok := pools[name]
		if !ok {
			return unknownName("jigsaw pool", name, slices.Sorted(maps.Keys(pools)))
		}
		if e.weight < 0 {
			return fmt.Errorf("worldgen: pool %q weight %d is below 0", name, e.weight)
		}
		for _, p := range list {
			if e.add {
				if o.st.Template(e.element) == nil {
					return fmt.Errorf("worldgen: add to pool %q: template %q is not in the packs or the vanilla data", name, e.element)
				}
				el := &structure.Element{ID: e.element, Template: e.element}
				if len(p.Entries) > 0 {
					el.TerrainMatching = p.Entries[0].Element.TerrainMatching
				}
				p.Entries = append(p.Entries, structure.PoolEntry{Element: el, Weight: e.weight})
				continue
			}
			found := false
			for i := range p.Entries {
				el := p.Entries[i].Element
				if el != nil && (el.ID == fullID(e.element) || el.Template == e.element) {
					p.Entries[i].Weight = e.weight
					found = true
				}
			}
			if !found {
				var names []string
				for _, pe := range p.Entries {
					if pe.Element != nil {
						names = append(names, pe.Element.ID)
					}
				}
				return unknownName("element of pool "+name, e.element, names)
			}
		}
	}
	return nil
}

// swapped runs place, which writes structure k into c, then swaps the
// blocks it changed by k's block swaps.
func (o *Overworld) swapped(k structKind, c *Chunk, place func()) {
	m := o.swaps[k]
	if m == nil {
		place()
		return
	}
	before, _ := o.snapshots.Get().(*blockSnapshot)
	if before == nil {
		before = new(blockSnapshot)
	}
	*before = c.Blocks
	place()
	for i, b := range c.Blocks {
		if b == before[i] {
			continue
		}
		if to, ok := m[uint32(b)]; ok {
			c.Blocks[i] = uint16(to)
		}
	}
	o.snapshots.Put(before)
}

// finishChunk applies the Config's changes that act on a finished chunk:
// the loot table swaps.
func (o *Overworld) finishChunk(c *Chunk) {
	if len(o.loot) == 0 {
		return
	}
	for i := range c.BlockEntities {
		be := &c.BlockEntities[i]
		to, ok := o.loot[be.LootTable]
		if !ok {
			continue
		}
		// The data map may be shared with a cached chunk: copy it.
		be.Data = maps.Clone(be.Data)
		be.LootTable = to
		if _, ok := be.Data["LootTable"]; ok {
			be.Data["LootTable"] = to
		}
	}
}
