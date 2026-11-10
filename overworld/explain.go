package overworld

import "fmt"

// Explanation is one step that set a block while a chunk was generated.
type Explanation struct {
	// Step names what set the block: a terrain step, a structure kind or a
	// feature rule's identifier.
	Step string
	// Block is the block name it set.
	Block string
}

// Explain lists what set the block at (x, y, z), in generation order: the
// terrain steps that changed it, the structures, then every feature write
// there (by rule, from each chunk whose features reach it), and last the
// finished block. It is a debugging tool for a Config: it generates the
// chunk again, slowly, and does not change the cache's results. It may be
// called from many goroutines at once.
func (t *TerrainCache) Explain(x, y, z int32) ([]Explanation, error) {
	if y < MinY || y >= MaxY {
		return nil, fmt.Errorf("worldgen: y %d is outside the world (%d to %d)", y, MinY, MaxY-1)
	}
	o := t.gen
	cx, cz := x>>4, z>>4
	e := &explainer{o: o, c: new(Chunk), x: int(x & 15), y: int(y), z: int(z & 15), last: ^uint32(0)}
	o.generateChunk(cx, cz, e.c, e.record)
	if !o.skip.Structures {
		height := surfaceHeights{o, t.Raw}.at
		o.placeStructures(cx, cz, e.c, height, t.Raw, e.record)
	}
	out := e.out
	for ox := cx - 2; ox <= cx+1; ox++ {
		for oz := cz - 2; oz <= cz+1; oz++ {
			tr := &featureTrace{pos: [3]int32{x, y, z}}
			o.originFeaturesTraced(ox, oz, t.Get, tr)
			for _, w := range tr.writes {
				w.Step = fmt.Sprintf("%s (from chunk %d %d)", w.Step, ox, oz)
				out = append(out, w)
			}
		}
	}
	var final Chunk
	t.Generate(cx, cz, &final)
	out = append(out, Explanation{Step: "finished chunk", Block: o.blockName(final.Block(e.x, e.y, e.z))})
	return out, nil
}

// explainer records the steps that change one block of a chunk.
type explainer struct {
	o       *Overworld
	c       *Chunk
	x, y, z int
	last    uint32
	out     []Explanation
}

// record notes the block after step if the step changed it.
func (e *explainer) record(step string) {
	if b := e.c.Block(e.x, e.y, e.z); b != e.last {
		e.out = append(e.out, Explanation{Step: step, Block: e.o.blockName(b)})
		e.last = b
	}
}
