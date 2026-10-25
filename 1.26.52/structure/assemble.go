// Package structure loads structure templates and places them: jigsaw
// assembly, Java template conversion and the structures the overworld and
// the Nether share.
package structure

import "github.com/helix-bedrock/worldgen/1.26.52/rand"

// Shape is the free space of one nesting level: a piece must lie inside
// one of in and touch none of out.
type Shape struct {
	In, Out []Box
}

func (s *Shape) fits(bb Box) bool {
	inside := false
	for _, b := range s.In {
		if bb.Inside(b) {
			inside = true
			break
		}
	}
	if !inside {
		return false
	}
	for _, b := range s.Out {
		if bb.Intersects(b) {
			return false
		}
	}
	return true
}

// Queued is a placed piece waiting to have its jigsaws expanded, with
// the nesting level it was placed in.
type Queued struct {
	Piece *Piece
	Shape int
}

// Assembly is BDS's newer jigsaw assembler (sub_D2AB0E0), which ancient
// cities and bastions share: d holds the structure's pools and maxDepth is
// how deep it expands.
type Assembly struct {
	Lib      *Lib
	Data     *JigsawData
	MaxDepth int
	R        *rand.MT
	Shapes   []Shape
	Pieces   []*Piece
	Queue    []Queued
	Feature  *JigsawTemplate
}

// Run expands the queued pieces first in, first out.
func (a *Assembly) Run() {
	for i := 0; i < len(a.Queue); i++ {
		a.expand(a.Queue[i])
	}
}

// PiecesBox is the box around every piece.
func PiecesBox(ps []*Piece) Box {
	bb := ps[0].BB
	for _, q := range ps[1:] {
		bb = Box{min(bb.X0, q.BB.X0), min(bb.Y0, q.BB.Y0), min(bb.Z0, q.BB.Z0),
			max(bb.X1, q.BB.X1), max(bb.Y1, q.BB.Y1), max(bb.Z1, q.BB.Z1)}
	}
	return bb
}

// expand tries every jigsaw of a queued piece.
func (a *Assembly) expand(q Queued) {
	if q.Piece.Depth > a.MaxDepth {
		return
	}
	js := q.Piece.PlacedJigsaws()
	Shuffle(js, a.R)
	// BDS then sorts by selection priority; city jigsaws have none.
	for _, j := range js {
		pool := a.Data.Pools[j.Pool]
		if pool == nil || len(pool.Entries) == 0 {
			continue
		}
		fallback := a.Data.Pools[pool.Fallback]
		if fallback != nil && len(fallback.Entries) == 0 {
			fallback = nil
		}
		if q.Piece.Depth != a.MaxDepth && a.fit(q, j, pool) {
			continue
		}
		if fallback != nil {
			a.fit(q, j, fallback)
		}
	}
}

// template is an element's prepared template; feature elements are a
// single block with one jigsaw facing down.
func (a *Assembly) template(el *Element) *JigsawTemplate {
	if el.Feature != "" {
		return a.Feature
	}
	return a.Lib.Template(el.Template)
}

// fit attaches the first element of pool that fits to jigsaw j of the
// queued piece, and reports whether one did.
func (a *Assembly) fit(q Queued, j JigsawBlock, pool *Pool) bool {
	cands := pool.WeightedShuffle(a.R)
	dir := JigsawDir(j.Facing)
	target := [3]int32{j.Pos[0] + dir[0], j.Pos[1] + dir[1], j.Pos[2] + dir[2]}
	inside := q.Piece.BB.Contains(target)
	want := Opposite(j.Facing)
	for _, el := range cands {
		// BDS stops at an empty element.
		if el.Template == "" && el.Feature == "" {
			return true
		}
		tpl := a.template(el)
		rots := []int{1, 2, 3, 0}
		Shuffle(rots, a.R)
		// A template missing from BDS's files (two bastion stables) has
		// no jigsaws: its turns are shuffled and nothing fits.
		if tpl == nil {
			continue
		}
		for _, rot := range rots {
			cand := &Piece{Element: el, Template: tpl, Rotation: rot}
			cjs := cand.PlacedJigsaws()
			Shuffle(cjs, a.R)
			for _, cj := range cjs {
				// The fronts must face; an aligned vertical joint must also
				// keep its top (sub_C556580).
				if cj.Facing != want || cj.Name != j.Target {
					continue
				}
				if (j.Facing == "up" || j.Facing == "down") && j.Aligned && cj.Top != j.Top {
					continue
				}
				// A piece joined inside its parent goes into the next
				// nesting level, whose space grows by the parent's box.
				si := q.Shape
				if inside {
					si++
					if si < len(a.Shapes) {
						a.Shapes[si].In = append(a.Shapes[si].In, q.Piece.BB)
					} else {
						a.Shapes = append(a.Shapes, Shape{In: []Box{q.Piece.BB}})
					}
				}
				pos := [3]int32{target[0] - cj.Pos[0], target[1] - cj.Pos[1], target[2] - cj.Pos[2]}
				bb := TemplateBox(tpl.Template, pos, rot)
				if !a.Shapes[si].fits(bb) {
					continue
				}
				a.Shapes[si].Out = append(a.Shapes[si].Out, bb)
				cand.Pos, cand.BB, cand.Depth = pos, bb, q.Piece.Depth+1
				// The joining jigsaw's height in the piece is its ground.
				cand.Ground = cj.Pos[1]
				a.Pieces = append(a.Pieces, cand)
				a.Queue = append(a.Queue, Queued{cand, si})
				return true
			}
		}
	}
	return false
}
