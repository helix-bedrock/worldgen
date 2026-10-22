package structure

import (
	"bytes"
	"fmt"
	"strconv"

	"github.com/helix-bedrock/worldgen"

	"github.com/helix-bedrock/worldgen/blocks"
)

// MCStructure is a Bedrock .mcstructure template (the format BDS's own
// structure templates use, such as the sulfur springs), resolved to
// registry states.
type MCStructure struct {
	Size [3]int32
	// blocks holds the layer 0 block of every cell, x outermost, then y,
	// then z (index z + sizeZ*(y + sizeY*x)); voidCell marks a cell the
	// template leaves alone.
	Blocks []uint32
}

// VoidCell marks a template cell that places no block.
const VoidCell = ^uint32(0)

// Cell returns the block at a template position, or voidCell.
func (t *MCStructure) Cell(x, y, z int32) uint32 {
	if x < 0 || y < 0 || z < 0 || x >= t.Size[0] || y >= t.Size[1] || z >= t.Size[2] {
		return VoidCell
	}
	return t.Blocks[z+t.Size[2]*(y+t.Size[1]*x)]
}

// LoadMCStructure reads an .mcstructure file from the embedded data. Palette
// states the registry does not know become void.
func (l *Lib) LoadMCStructure(path string) (*MCStructure, error) {
	raw, err := worldgen.Data.ReadFile("data/vanilla/structures/" + path + ".mcstructure")
	if err != nil {
		return nil, fmt.Errorf("mcstructure %s: %w", path, err)
	}
	root, err := blocks.ReadNBTLittle(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("mcstructure %s: %w", path, err)
	}
	size, ok := IntList(root["size"], 3)
	if !ok {
		return nil, fmt.Errorf("mcstructure %s: bad size", path)
	}
	t := &MCStructure{}
	copy(t.Size[:], size)
	st, _ := root["structure"].(map[string]any)
	layers, _ := st["block_indices"].([]any)
	if len(layers) == 0 {
		return nil, fmt.Errorf("mcstructure %s: no block indices", path)
	}
	idx, _ := layers[0].([]any)
	n := int(t.Size[0]) * int(t.Size[1]) * int(t.Size[2])
	if len(idx) != n {
		return nil, fmt.Errorf("mcstructure %s: %d block indices for %d cells", path, len(idx), n)
	}
	pal, _ := st["palette"].(map[string]any)
	def, _ := pal["default"].(map[string]any)
	list, _ := def["block_palette"].([]any)
	states := make([]uint32, len(list))
	for i, e := range list {
		states[i] = VoidCell
		m, _ := e.(map[string]any)
		name, _ := m["name"].(string)
		props, _ := m["states"].(map[string]any)
		if props == nil {
			props = map[string]any{}
		}
		if id, ok := l.reg.ByHash(blocks.Hash(blocks.State{Name: name, Props: props})); ok {
			states[i] = id
		}
	}
	t.Blocks = make([]uint32, n)
	for i, v := range idx {
		t.Blocks[i] = VoidCell
		if k, ok := v.(int32); ok && k >= 0 && int(k) < len(states) {
			t.Blocks[i] = states[k]
		}
	}
	return t, nil
}

// mcJigsawFacing names a Bedrock jigsaw's facing_direction.
func mcJigsawFacing() [6]string { return [6]string{"down", "up", "north", "south", "west", "east"} }

// LoadMCTemplate reads an .mcstructure file as a jigsaw template, with its
// block names and its jigsaws' block entity data, for pool elements BDS
// keeps in that format (the bastion's mobs). The template's entities are
// not read: worldgen places no mobs.
func LoadMCTemplate(path string) (*Template, error) {
	raw, err := worldgen.Data.ReadFile("data/vanilla/structures/" + path + ".mcstructure")
	if err != nil {
		return nil, fmt.Errorf("mcstructure %s: %w", path, err)
	}
	root, err := blocks.ReadNBTLittle(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("mcstructure %s: %w", path, err)
	}
	size, ok := IntList(root["size"], 3)
	if !ok {
		return nil, fmt.Errorf("mcstructure %s: bad size", path)
	}
	t := &Template{Bedrock: true}
	copy(t.Size[:], size)
	st, _ := root["structure"].(map[string]any)
	layers, _ := st["block_indices"].([]any)
	if len(layers) == 0 {
		return nil, fmt.Errorf("mcstructure %s: no block indices", path)
	}
	idx, _ := layers[0].([]any)
	if len(idx) != int(t.Size[0]*t.Size[1]*t.Size[2]) {
		return nil, fmt.Errorf("mcstructure %s: %d block indices for size %v", path, len(idx), t.Size)
	}
	pal, _ := st["palette"].(map[string]any)
	def, _ := pal["default"].(map[string]any)
	list, _ := def["block_palette"].([]any)
	for _, e := range list {
		m, _ := e.(map[string]any)
		name, _ := m["name"].(string)
		props, _ := m["states"].(map[string]any)
		ts := TemplateState{Name: name, Props: map[string]string{}}
		if name == "minecraft:jigsaw" {
			// VERIFY: the rotation state read as the top, north first;
			// the bastion's mob jigsaws are rollable, so it is unused.
			f, r := NBTInt(props["facing_direction"]), NBTInt(props["rotation"])
			if facings := mcJigsawFacing(); f >= 0 && f < int32(len(facings)) {
				ts.Props["orientation"] = facings[f] + "_" + Horizontal()[r&3]
			}
		}
		t.Palette = append(t.Palette, ts)
	}
	pos, _ := def["block_position_data"].(map[string]any)
	for i, v := range idx {
		k, ok := v.(int32)
		if !ok || k < 0 || int(k) >= len(t.Palette) {
			continue
		}
		x := int32(i) / (t.Size[1] * t.Size[2])
		y := int32(i) / t.Size[2] % t.Size[1]
		z := int32(i) % t.Size[2]
		b := TemplateBlock{Pos: [3]int32{x, y, z}, State: k}
		if d, ok := pos[strconv.Itoa(i)].(map[string]any); ok {
			b.NBT, _ = d["block_entity_data"].(map[string]any)
		}
		t.Blocks = append(t.Blocks, b)
	}
	return t, nil
}
