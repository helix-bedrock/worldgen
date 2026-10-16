package worldgen

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/helix-bedrock/worldgen/rand"
)

// Jigsaw structures (villages for now), after BDS 1.26.52:
//   start      sub_D2B36D0  start piece, moved onto the ground at its centre
//   loop       sub_D2B3CB0  per piece: its jigsaw blocks shuffled, then each
//                           tried against its target pool, then the fallback
//   fit        sub_D2B4120  candidates from a weighted shuffle of the pool,
//                           4 rotations shuffled, candidate jigsaws shuffled
// Shuffles are BDS's forward one: for i = 1..n-1, j = rand % (i+1), swap.
// VERIFY: the y rules and the free-space test follow the structure of
// sub_D2B4120 and Java's JigsawPlacement; both are to be compared block by
// block with BDS villages.

// jigsawData is the pools and elements of the jigsaw structures.
type jigsawData struct {
	pools    map[string]*jigsawPool
	elements map[string]*jigsawElement
}

type jigsawPool struct {
	name, fallback string
	entries        []jigsawEntry
}

type jigsawEntry struct {
	el     *jigsawElement
	weight int32
}

type jigsawElement struct {
	id, template, feature string
	terrainMatching       bool
}

// jigsawTemplate is a template prepared for placement: its blocks converted
// per rotation lazily, and its jigsaw connectors.
type jigsawTemplate struct {
	tpl     *Template
	jigsaws []jigsawBlock
	once    [4]sync.Once
	conv    [4][]convertedState
}

type convertedState struct {
	id    uint32
	water bool
	ok    bool
}

type jigsawBlock struct {
	pos        [3]int32
	facing     string
	attachment string
	pool       string
	finalState string
}

func loadJigsawData() (*jigsawData, error) {
	raw, err := vanillaData.ReadFile("data/bds/villages.json")
	if err != nil {
		return nil, fmt.Errorf("worldgen: read village data: %w", err)
	}
	var doc struct {
		Pools []struct {
			Name, Fallback string
			Elements       []struct {
				Element string
				Weight  int32
			}
		}
		Elements []struct {
			ID         string `json:"id"`
			Template   string
			Feature    string
			Projection string
		}
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("worldgen: parse village data: %w", err)
	}
	d := &jigsawData{pools: map[string]*jigsawPool{}, elements: map[string]*jigsawElement{}}
	for _, e := range doc.Elements {
		d.elements[e.ID] = &jigsawElement{id: e.ID, template: e.Template, feature: e.Feature, terrainMatching: e.Projection == "terrain_matching"}
	}
	for _, p := range doc.Pools {
		jp := &jigsawPool{name: p.Name, fallback: p.Fallback}
		for _, e := range p.Elements {
			if el, ok := d.elements[e.Element]; ok {
				jp.entries = append(jp.entries, jigsawEntry{el, e.Weight})
			}
		}
		// The abandoned pools share names with nothing else; a name
		// registered twice keeps its first definition.
		if _, dup := d.pools[p.Name]; !dup {
			d.pools[p.Name] = jp
		}
	}
	return d, nil
}

// template returns the prepared template of an element, nil if it has none
// or the file does not exist (BDS references two missing village files).
func (o *Overworld) template(path string) *jigsawTemplate {
	if path == "" {
		return nil
	}
	if v, ok := o.templates.Load(path); ok {
		jt, _ := v.(*jigsawTemplate)
		return jt
	}
	var jt *jigsawTemplate
	if f, err := vanillaData.Open("data/vanilla/structures/" + path + ".nbt"); err == nil {
		tpl, err := LoadTemplate(f)
		_ = f.Close()
		if err == nil {
			jt = &jigsawTemplate{tpl: tpl}
			for _, b := range tpl.Blocks {
				st := tpl.Palette[b.State]
				if st.Name != "minecraft:jigsaw" {
					continue
				}
				jb := jigsawBlock{pos: b.Pos, facing: st.Props["facing"]}
				jb.attachment, _ = b.NBT["attachement_type"].(string)
				if n, ok := b.NBT["name"].(string); ok && jb.attachment == "" {
					jb.attachment = n
				}
				jb.pool, _ = b.NBT["target_pool"].(string)
				if p, ok := b.NBT["pool"].(string); ok && jb.pool == "" {
					jb.pool = p
				}
				jb.finalState, _ = b.NBT["final_state"].(string)
				jt.jigsaws = append(jt.jigsaws, jb)
			}
		}
	}
	v, _ := o.templates.LoadOrStore(path, jt)
	jt, _ = v.(*jigsawTemplate)
	return jt
}

// box is an inclusive block bounding box.
type box struct{ x0, y0, z0, x1, y1, z1 int32 }

func (b box) contains(p [3]int32) bool {
	return p[0] >= b.x0 && p[0] <= b.x1 && p[1] >= b.y0 && p[1] <= b.y1 && p[2] >= b.z0 && p[2] <= b.z1
}

func (b box) intersects(c box) bool {
	return b.x0 <= c.x1 && b.x1 >= c.x0 && b.y0 <= c.y1 && b.y1 >= c.y0 && b.z0 <= c.z1 && b.z1 >= c.z0
}

func (b box) inside(c box) bool {
	return b.x0 >= c.x0 && b.x1 <= c.x1 && b.y0 >= c.y0 && b.y1 <= c.y1 && b.z0 >= c.z0 && b.z1 <= c.z1
}

func (b box) offset(dx, dy, dz int32) box {
	return box{b.x0 + dx, b.y0 + dy, b.z0 + dz, b.x1 + dx, b.y1 + dy, b.z1 + dz}
}

// rotatePos rotates a template position by r quarter turns clockwise
// around the template origin (Java's Rotation.CLOCKWISE_90: x,z -> -z,x).
func rotatePos(p [3]int32, r int) [3]int32 {
	x, z := p[0], p[2]
	switch r & 3 {
	case 1:
		x, z = -z, x
	case 2:
		x, z = -x, -z
	case 3:
		x, z = z, -x
	}
	return [3]int32{x, p[1], z}
}

var horizontal = []string{"north", "east", "south", "west"}

func rotateFacing(f string, r int) string {
	for i, h := range horizontal {
		if h == f {
			return horizontal[(i+r)&3]
		}
	}
	return f
}

func opposite(f string) string {
	switch f {
	case "up":
		return "down"
	case "down":
		return "up"
	}
	return rotateFacing(f, 2)
}

func jigsawDir(f string) [3]int32 {
	switch f {
	case "north":
		return [3]int32{0, 0, -1}
	case "south":
		return [3]int32{0, 0, 1}
	case "west":
		return [3]int32{-1, 0, 0}
	case "east":
		return [3]int32{1, 0, 0}
	case "up":
		return [3]int32{0, 1, 0}
	case "down":
		return [3]int32{0, -1, 0}
	}
	return [3]int32{}
}

// tplBox is the box of a template placed at pos with rotation r.
func tplBox(t *Template, pos [3]int32, r int) box {
	a := rotatePos([3]int32{0, 0, 0}, r)
	b := rotatePos([3]int32{t.Size[0] - 1, t.Size[1] - 1, t.Size[2] - 1}, r)
	return box{min(a[0], b[0]) + pos[0], pos[1], min(a[2], b[2]) + pos[2],
		max(a[0], b[0]) + pos[0], pos[1] + t.Size[1] - 1, max(a[2], b[2]) + pos[2]}
}

// jigsawPiece is one placed element.
type jigsawPiece struct {
	el       *jigsawElement
	tpl      *jigsawTemplate
	pos      [3]int32
	rotation int
	bb       box
	depth    int
}

// placedJigsaws returns a piece's jigsaw blocks in world space.
func (p *jigsawPiece) placedJigsaws() []jigsawBlock {
	out := make([]jigsawBlock, len(p.tpl.jigsaws))
	for i, j := range p.tpl.jigsaws {
		rp := rotatePos(j.pos, p.rotation)
		j.pos = [3]int32{rp[0] + p.pos[0], rp[1] + p.pos[1], rp[2] + p.pos[2]}
		j.facing = rotateFacing(j.facing, p.rotation)
		out[i] = j
	}
	return out
}

func shuffle[T any](s []T, r *rand.MT) {
	for i := 1; i < len(s); i++ {
		j := int(r.Uint32() % uint32(i+1))
		if j != i {
			s[i], s[j] = s[j], s[i]
		}
	}
}

// weightedShuffle is a pool's elements, each repeated weight times, shuffled.
func (p *jigsawPool) weightedShuffle(r *rand.MT) []*jigsawElement {
	var out []*jigsawElement
	for _, e := range p.entries {
		for range e.weight {
			out = append(out, e.el)
		}
	}
	shuffle(out, r)
	return out
}

// jigsawAssembly holds the state of one structure being assembled.
type jigsawAssembly struct {
	o        *Overworld
	r        *rand.MT
	height   func(x, z int32) int32
	maxDepth int
	pieces   []*jigsawPiece
	bounds   box
}

// assembleJigsaw builds a structure from a start pool at pos (y ignored:
// the start piece sits on the ground at its centre).
func (o *Overworld) assembleJigsaw(start *jigsawPool, pos [3]int32, rotation int, maxDepth int, r *rand.MT, height func(x, z int32) int32) []*jigsawPiece {
	if start == nil || len(start.entries) == 0 {
		return nil
	}
	el := start.entries[r.IntN(int32(len(start.entries)))].el
	tpl := o.template(el.template)
	if tpl == nil {
		return nil
	}
	p := &jigsawPiece{el: el, tpl: tpl, pos: pos, rotation: rotation}
	p.bb = tplBox(tpl.tpl, pos, rotation)
	cx, cz := (p.bb.x0+p.bb.x1)/2, (p.bb.z0+p.bb.z1)/2
	// Ground level delta 1: the bottom layer replaces the surface block.
	dy := height(cx, cz) - 1 - p.bb.y0
	p.pos[1] += dy
	p.bb = p.bb.offset(0, dy, 0)
	a := &jigsawAssembly{o: o, r: r, height: height, maxDepth: maxDepth, pieces: []*jigsawPiece{p},
		bounds: box{cx - 80, MinY, cz - 80, cx + 80, MaxY - 1, cz + 80}}
	queue := []*jigsawPiece{p}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, ch := range a.children(cur) {
			if ch.depth <= maxDepth {
				queue = append(queue, ch)
			}
		}
	}
	return a.pieces
}

// children tries each jigsaw block of piece p and returns the pieces added.
func (a *jigsawAssembly) children(p *jigsawPiece) []*jigsawPiece {
	var added []*jigsawPiece
	js := p.placedJigsaws()
	shuffle(js, a.r)
	rigid := !p.el.terrainMatching
	for _, j := range js {
		pool := a.o.jigsaw.pools[j.pool]
		if pool == nil || len(pool.entries) == 0 {
			continue
		}
		fallback := a.o.jigsaw.pools[pool.fallback]
		dir := jigsawDir(j.facing)
		target := [3]int32{j.pos[0] + dir[0], j.pos[1] + dir[1], j.pos[2] + dir[2]}
		insideParent := p.bb.contains(target)
		var cands []*jigsawElement
		if p.depth != a.maxDepth {
			cands = pool.weightedShuffle(a.r)
		}
		if fallback != nil {
			cands = append(cands, fallback.weightedShuffle(a.r)...)
		}
		relY := j.pos[1] - p.bb.y0
		surface := int32(-1 << 30)
		if np := a.fit(p, j, target, relY, rigid, insideParent, cands, &surface); np != nil {
			added = append(added, np)
		}
	}
	return added
}

func (a *jigsawAssembly) fit(p *jigsawPiece, j jigsawBlock, target [3]int32, relY int32, rigid, insideParent bool, cands []*jigsawElement, surface *int32) *jigsawPiece {
	for _, el := range cands {
		tpl := a.o.template(el.template)
		if tpl == nil {
			continue
		}
		rots := []int{0, 1, 2, 3}
		shuffle(rots, a.r)
		for _, rot := range rots {
			cand := &jigsawPiece{el: el, tpl: tpl, rotation: rot}
			cjs := cand.placedJigsaws()
			// A short piece reserves room above it for what its inward
			// jigsaws will attach: its box is raised to the tallest
			// element of their pools (and fallbacks).
			reserve := a.reserveHeight(tpl, cjs, rot)
			shuffle(cjs, a.r)
			for _, cj := range cjs {
				if cj.facing != opposite(j.facing) || cj.attachment != j.attachment {
					continue
				}
				off := [3]int32{target[0] - cj.pos[0], target[1] - cj.pos[1], target[2] - cj.pos[2]}
				bb := tplBox(tpl.tpl, off, rot)
				candRigid := !el.terrainMatching
				cjRelY := cj.pos[1]
				var y int32
				if rigid && candRigid {
					y = p.bb.y0 + relY - cjRelY + jigsawDir(j.facing)[1]
				} else {
					if *surface == -1<<30 {
						*surface = a.height(j.pos[0], j.pos[2])
					}
					y = *surface - cjRelY
				}
				dy := y - bb.y0
				bb = bb.offset(0, dy, 0)
				if reserve > 0 {
					bb.y1 = bb.y0 + max(reserve+1, bb.y1-bb.y0)
				}
				if !a.free(bb, p, insideParent) {
					continue
				}
				cand.pos = [3]int32{off[0], off[1] + dy, off[2]}
				cand.bb = bb
				cand.depth = p.depth + 1
				a.pieces = append(a.pieces, cand)
				return cand
			}
		}
	}
	return nil
}

// reserveHeight is the tallest element any jigsaw of a candidate (placed
// at the origin) can attach inside its own box, 0 for boxes over 16 high.
func (a *jigsawAssembly) reserveHeight(tpl *jigsawTemplate, cjs []jigsawBlock, rot int) int32 {
	bb := tplBox(tpl.tpl, [3]int32{}, rot)
	if bb.y1-bb.y0+1 > 16 {
		return 0
	}
	var h int32
	for _, j := range cjs {
		d := jigsawDir(j.facing)
		if !bb.contains([3]int32{j.pos[0] + d[0], j.pos[1] + d[1], j.pos[2] + d[2]}) {
			continue
		}
		pool := a.o.jigsaw.pools[j.pool]
		if pool == nil {
			continue
		}
		h = max(h, a.o.poolMaxHeight(pool))
		if fb := a.o.jigsaw.pools[pool.fallback]; fb != nil {
			h = max(h, a.o.poolMaxHeight(fb))
		}
	}
	return h
}

// poolMaxHeight is the tallest template of a pool.
func (o *Overworld) poolMaxHeight(p *jigsawPool) int32 {
	var h int32
	for _, e := range p.entries {
		if t := o.template(e.el.template); t != nil {
			h = max(h, t.tpl.Size[1])
		}
	}
	return h
}

// free reports whether bb fits: inside the parent's box for a connector
// inside it, else inside the structure bounds, and clear of every other
// piece (touching allowed).
func (a *jigsawAssembly) free(bb box, parent *jigsawPiece, insideParent bool) bool {
	limit := a.bounds
	if insideParent {
		limit = parent.bb
	}
	if !bb.inside(limit) {
		return false
	}
	for _, q := range a.pieces {
		if q == parent && insideParent {
			continue
		}
		if bb.intersects(q.bb) {
			return false
		}
	}
	return true
}

// village is an assembled village and the chunk range it covers.
type village struct {
	pieces []*jigsawPiece
	bb     box
}

// villageAt assembles the village starting in chunk (cx, cz), if any.
// height must give the surface height of any column.
func (o *Overworld) villageAt(cx, cz int32, height func(x, z int32) int32) *village {
	if v, ok := o.villages.Load([2]int32{cx, cz}); ok {
		vv, _ := v.(*village)
		return vv
	}
	var vil *village
	if o.jigsaw != nil && o.IsVillageChunk(cx, cz) {
		var r rand.MT
		// The start is built with the region's random after the spread
		// draws (VERIFY: sub_4FBBAD0's caller).
		rx, rz := floorDivRegion(cx, villageSpacing), floorDivRegion(cz, villageSpacing)
		structureSeed(&r, o.seed, rx, rz, villageSalt)
		for range 4 {
			r.IntN(villageSpacing - villageSeparation)
		}
		rotation := int(r.IntN(4))
		abandoned := r.Float() < 0.02
		y := prelimSurface(o.column(cx*4, cz*4))
		biome := o.BiomeAt(cx*16+8, y, cz*16+8)
		kind := "plains"
		if int(biome) < len(o.villageType) && o.villageType[biome] != "" && o.villageType[biome] != "default" {
			kind = o.villageType[biome]
		}
		name := "minecraft:village/" + kind + "/town_centers"
		if abandoned {
			name = "minecraft:village/abandoned/" + kind + "/town_centers"
		}
		pieces := o.assembleJigsaw(o.jigsaw.pools[name], [3]int32{cx * 16, y, cz * 16}, rotation, 6, &r, height)
		if len(pieces) > 0 {
			vil = &village{pieces: pieces, bb: pieces[0].bb}
			for _, p := range pieces[1:] {
				vil.bb = box{min(vil.bb.x0, p.bb.x0), min(vil.bb.y0, p.bb.y0), min(vil.bb.z0, p.bb.z0),
					max(vil.bb.x1, p.bb.x1), max(vil.bb.y1, p.bb.y1), max(vil.bb.z1, p.bb.z1)}
			}
		}
	}
	v, _ := o.villages.LoadOrStore([2]int32{cx, cz}, vil)
	vil, _ = v.(*village)
	return vil
}

// placeVillages writes the parts of nearby villages inside chunk (cx, cz)
// into c. height gives surface heights from the undecorated terrain.
func (o *Overworld) placeVillages(cx, cz int32, c *Chunk, height func(x, z int32) int32) {
	if o.jigsaw == nil {
		return
	}
	// Villages reach up to 80 blocks from their start chunk.
	r0x, r1x := floorDivRegion(cx-6, villageSpacing), floorDivRegion(cx+6, villageSpacing)
	r0z, r1z := floorDivRegion(cz-6, villageSpacing), floorDivRegion(cz+6, villageSpacing)
	chunkBox := box{cx * 16, MinY, cz * 16, cx*16 + 15, MaxY - 1, cz*16 + 15}
	for rx := r0x; rx <= r1x; rx++ {
		for rz := r0z; rz <= r1z; rz++ {
			sx, sz, ok := o.villagePick(rx, rz)
			if !ok {
				continue
			}
			v := o.villageAt(sx, sz, height)
			if v == nil || !v.bb.intersects(chunkBox) {
				continue
			}
			for _, p := range v.pieces {
				if p.bb.intersects(chunkBox) {
					o.placePiece(p, c, chunkBox, height)
				}
			}
		}
	}
}

// villagePick is the chunk a region's village would start in.
func (o *Overworld) villagePick(rx, rz int32) (int32, int32, bool) {
	var r rand.MT
	structureSeed(&r, o.seed, rx, rz, villageSalt)
	n := int32(villageSpacing - villageSeparation)
	a := r.IntN(n)
	x := rx*villageSpacing + (a+r.IntN(n))/2
	b := r.IntN(n)
	z := rz*villageSpacing + (b+r.IntN(n))/2
	return x, z, true
}

// placePiece writes a piece's blocks inside the chunk.
func (o *Overworld) placePiece(p *jigsawPiece, c *Chunk, chunkBox box, height func(x, z int32) int32) {
	t := p.tpl.tpl
	rot := p.rotation
	p.tpl.once[rot].Do(func() {
		conv := make([]convertedState, len(t.Palette))
		for i, st := range t.Palette {
			st.Props = rotateProps(st.Props, rot)
			id, w, ok := o.convertJavaState(st)
			conv[i] = convertedState{id, w, ok}
		}
		p.tpl.conv[rot] = conv
	})
	conv := p.tpl.conv[rot]
	for _, b := range t.Blocks {
		rp := rotatePos(b.Pos, rot)
		x, z := rp[0]+p.pos[0], rp[2]+p.pos[2]
		y := rp[1] + p.pos[1]
		if p.el.terrainMatching {
			y = height(x, z) - 1 + b.Pos[1]
		}
		if !chunkBox.contains([3]int32{x, y, z}) || y < MinY || y >= MaxY {
			continue
		}
		st := t.Palette[b.State]
		cs := conv[b.State]
		switch st.Name {
		case "minecraft:jigsaw":
			fs, _ := b.NBT["final_state"].(string)
			cs = o.parseFinalState(fs, rot)
		case "minecraft:structure_block", "minecraft:structure_void":
			// Data markers are handled by the element, not placed.
			if st.Name == "minecraft:structure_void" {
				continue
			}
			cs = convertedState{o.ids.air, false, true}
		}
		if !cs.ok {
			continue
		}
		lx, lz := int(x&15), int(z&15)
		i := int(y-MinY)<<8 | lz<<4 | lx
		c.Blocks[i] = uint16(cs.id)
		if d := o.blockEntityFor(cs.id, st.Name, x, y, z); d != nil {
			c.BlockEntities = append(c.BlockEntities, BlockEntity{x, y, z, d})
		}
		if cs.water {
			c.Water[i>>6] |= 1 << (i & 63)
		} else {
			c.Water[i>>6] &^= 1 << (i & 63)
		}
	}
}

// parseFinalState converts a jigsaw block's final_state ("minecraft:x[a=b]").
func (o *Overworld) parseFinalState(s string, rot int) convertedState {
	if s == "" {
		return convertedState{o.ids.air, false, true}
	}
	st := TemplateState{Name: s}
	if i := strings.IndexByte(s, '['); i >= 0 && strings.HasSuffix(s, "]") {
		st.Name = s[:i]
		st.Props = map[string]string{}
		for _, kv := range strings.Split(s[i+1:len(s)-1], ",") {
			if k, v, ok := strings.Cut(kv, "="); ok {
				st.Props[k] = v
			}
		}
	}
	st.Props = rotateProps(st.Props, rot)
	id, w, ok := o.convertJavaState(st)
	return convertedState{id, w, ok}
}

// rotateProps rotates the direction properties of a Java state.
func rotateProps(p map[string]string, rot int) map[string]string {
	if rot == 0 || len(p) == 0 {
		return p
	}
	out := make(map[string]string, len(p))
	for k, v := range p {
		out[k] = v
	}
	if f, ok := p["facing"]; ok {
		out["facing"] = rotateFacing(f, rot)
	}
	if a, ok := p["axis"]; ok && rot&1 == 1 {
		switch a {
		case "x":
			out["axis"] = "z"
		case "z":
			out["axis"] = "x"
		}
	}
	if r, ok := p["rotation"]; ok {
		var n int
		if _, err := fmt.Sscan(r, &n); err == nil {
			out["rotation"] = fmt.Sprint((n + rot*4) & 15)
		}
	}
	// Fence and pane connections.
	if rot != 0 {
		sides := [4]string{p["north"], p["east"], p["south"], p["west"]}
		if sides != [4]string{} {
			for i, h := range horizontal {
				if v := sides[(i-rot+4)&3]; v != "" {
					out[h] = v
				}
			}
		}
	}
	return out
}

// surfaceHeight is the y above the highest non-air block of a column in the
// undecorated terrain (Java's WORLD_SURFACE_WG).
func (o *Overworld) surfaceHeight(terrain func(cx, cz int32) *PackedChunk, x, z int32) int32 {
	c := terrain(x>>4, z>>4)
	if c == nil {
		return SeaLevel + 1
	}
	lx, lz := int(x&15), int(z&15)
	for y := MaxY - 1; y >= MinY; y-- {
		if c.at((y-MinY)<<8|lz<<4|lx) != o.ids.air {
			return int32(y + 1)
		}
	}
	return MinY
}
