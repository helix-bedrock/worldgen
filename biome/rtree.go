package biome

import (
	"math"
	"sort"
)

// childrenPerNode is the fan-out of the search tree.
const childrenPerNode = 6

// node is a tree node: a leaf holds one entry, a branch holds children. Its
// space is the bounding box of everything under it.
type node struct {
	space    [7]Param
	children []*node
	biome    uint32
	leaf     bool
}

// Tree finds the parameter box nearest to a climate point. It is immutable
// after creation and safe for concurrent use.
type Tree struct{ root *node }

// NewTree builds the tree over entries, with biome IDs from ids. Entries
// whose biome is not in ids are left out.
func NewTree(entries []Entry, ids map[string]uint32) *Tree {
	leaves := make([]*node, 0, len(entries))
	for _, e := range entries {
		id, ok := ids[e.Biome]
		if !ok {
			continue
		}
		leaves = append(leaves, &node{space: e.Space, biome: id, leaf: true})
	}
	if len(leaves) == 0 {
		return &Tree{}
	}
	return &Tree{root: build(leaves)}
}

func center(p Param) int64 { return (p.Min + p.Max) / 2 }

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func build(children []*node) *node {
	if len(children) == 1 {
		return children[0]
	}
	if len(children) <= childrenPerNode {
		sort.SliceStable(children, func(a, b int) bool {
			return centerSum(children[a]) < centerSum(children[b])
		})
		return branch(children)
	}
	best := int64(math.MaxInt64)
	bestAxis := -1
	var bestBuckets []*node
	for axis := range 7 {
		sortNodes(children, axis, false)
		buckets := bucketize(children)
		var cost int64
		for _, b := range buckets {
			cost += boxCost(b.space)
		}
		if best > cost {
			best, bestAxis, bestBuckets = cost, axis, buckets
		}
	}
	sortNodes(bestBuckets, bestAxis, true)
	out := make([]*node, len(bestBuckets))
	for i, b := range bestBuckets {
		out[i] = build(b.children)
	}
	return branch(out)
}

func centerSum(n *node) int64 {
	var s int64
	for _, p := range n.space {
		s += abs64(center(p))
	}
	return s
}

// sortNodes sorts by the centre of axis, then the following axes in turn.
func sortNodes(nodes []*node, axis int, absolute bool) {
	key := func(n *node, a int) int64 {
		c := center(n.space[a])
		if absolute {
			return abs64(c)
		}
		return c
	}
	sort.SliceStable(nodes, func(i, j int) bool {
		for k := range 7 {
			a := (axis + k) % 7
			ki, kj := key(nodes[i], a), key(nodes[j], a)
			if ki != kj {
				return ki < kj
			}
		}
		return false
	})
}

func bucketize(nodes []*node) []*node {
	size := int(math.Pow(childrenPerNode, math.Floor(math.Log(float64(len(nodes))-0.01)/math.Log(childrenPerNode))))
	size = max(size, 1)
	var out []*node
	cur := make([]*node, 0, size)
	for _, n := range nodes {
		cur = append(cur, n)
		if len(cur) >= size {
			out = append(out, branch(cur))
			cur = nil
		}
	}
	if len(cur) > 0 {
		out = append(out, branch(cur))
	}
	return out
}

func boxCost(space [7]Param) int64 {
	var c int64
	for _, p := range space {
		c += abs64(p.Max - p.Min)
	}
	return c
}

func branch(children []*node) *node {
	n := &node{children: children}
	for a := range 7 {
		lo, hi := int64(math.MaxInt64), int64(math.MinInt64)
		for _, c := range children {
			lo = min(lo, c.space[a].Min)
			hi = max(hi, c.space[a].Max)
		}
		n.space[a] = Param{lo, hi}
	}
	return n
}

func dist(space *[7]Param, target *[7]int64) int64 {
	var d int64
	for i := range space {
		p := space[i]
		v := target[i]
		var e int64
		if a := v - p.Max; a > 0 {
			e = a
		} else if b := p.Min - v; b > 0 {
			e = b
		}
		d += e * e
	}
	return d
}

// Search returns the biome ID nearest to the quantized target (temperature,
// humidity, continentalness, erosion, depth, weirdness, 0). The bool is false
// for an empty tree.
func (t *Tree) Search(target [7]int64) (uint32, bool) {
	if t.root == nil {
		return 0, false
	}
	leaf, _ := search(t.root, &target, nil, math.MaxInt64)
	if leaf == nil {
		return 0, false
	}
	return leaf.biome, true
}

func search(n *node, target *[7]int64, best *node, bestDist int64) (*node, int64) {
	if n.leaf {
		return n, dist(&n.space, target)
	}
	for _, c := range n.children {
		d := dist(&c.space, target)
		if bestDist > d {
			l, ld := search(c, target, best, bestDist)
			if c.leaf {
				ld = d
			}
			if bestDist > ld {
				best, bestDist = l, ld
			}
		}
	}
	return best, bestDist
}
