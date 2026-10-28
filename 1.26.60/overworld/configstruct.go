package overworld

import (
	"maps"

	"github.com/helix-bedrock/worldgen/1.26.60/structure"
)

// blockSnapshot is a copy of a chunk's blocks, to find what a structure
// changed.
type blockSnapshot = [16 * 16 * Height]uint16

// applyStructureContents applies the template, pool, loot and block swap
// changes, after the structures are loaded.
func (o *Overworld) applyStructureContents(c *StructureConfig) error {
	if err := structure.CheckTemplates(c.templates, o.st); err != nil {
		return err
	}
	if len(c.pools) > 0 {
		pools := map[string][]*structure.Pool{}
		if o.jigsaw != nil {
			addPools(pools, o.jigsaw.Pools)
		}
		if o.city != nil {
			addPools(pools, o.city.Pools)
		}
		for _, d := range o.dataJigsaws {
			addPools(pools, d.pools)
			for _, v := range d.variants {
				addPools(pools, v.d.pools)
			}
		}
		edits := make([]structure.PoolEdit, len(c.pools))
		for i, e := range c.pools {
			edits[i] = structure.PoolEdit{Pool: e.pool, Element: e.element, Weight: e.weight, Add: e.add}
		}
		if err := structure.EditPools(pools, edits, o.st); err != nil {
			return err
		}
	}
	o.loot = maps.Clone(c.loot)
	for _, s := range c.swaps {
		k, err := structIndex(s.kind)
		if err != nil {
			return err
		}
		if o.swaps[k] == nil {
			o.swaps[k] = map[uint32]uint32{}
		}
		if err := structure.SwapTable(o.reg, s.from, s.to, o.swaps[k]); err != nil {
			return err
		}
	}
	return nil
}

// addPools adds the pools of m to pools, by name.
func addPools(pools map[string][]*structure.Pool, m map[string]*structure.Pool) {
	for name, p := range m {
		pools[name] = append(pools[name], p)
	}
}

// swapped runs place, which writes structure k into c, then swaps the
// blocks it changed by k's block swaps, and reports the step to trace.
func (o *Overworld) swapped(k structKind, c *Chunk, trace func(step string), place func()) {
	if trace != nil {
		defer trace(o.gates.Name(int(k)))
	}
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
	structure.SwapChanged(c.Blocks[:], before[:], m)
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
