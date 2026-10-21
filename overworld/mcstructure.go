package overworld

import (
	"bytes"
	"fmt"
	"github.com/helix-bedrock/worldgen"

	"github.com/helix-bedrock/worldgen/blocks"
)

// mcStructure is a Bedrock .mcstructure template (the format BDS's own
// structure templates use, such as the sulfur springs), resolved to
// registry states.
type mcStructure struct {
	size [3]int32
	// blocks holds the layer 0 block of every cell, x outermost, then y,
	// then z (index z + sizeZ*(y + sizeY*x)); voidCell marks a cell the
	// template leaves alone.
	blocks []uint32
}

const voidCell = ^uint32(0)

// cell returns the block at a template position, or voidCell.
func (t *mcStructure) cell(x, y, z int32) uint32 {
	if x < 0 || y < 0 || z < 0 || x >= t.size[0] || y >= t.size[1] || z >= t.size[2] {
		return voidCell
	}
	return t.blocks[z+t.size[2]*(y+t.size[1]*x)]
}

// loadMCStructure reads an .mcstructure file from the embedded data. Palette
// states the registry does not know become void.
func (o *Overworld) loadMCStructure(path string) (*mcStructure, error) {
	raw, err := worldgen.Data.ReadFile("data/vanilla/structures/" + path + ".mcstructure")
	if err != nil {
		return nil, fmt.Errorf("mcstructure %s: %w", path, err)
	}
	root, err := blocks.ReadNBTLittle(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("mcstructure %s: %w", path, err)
	}
	size, ok := intList(root["size"], 3)
	if !ok {
		return nil, fmt.Errorf("mcstructure %s: bad size", path)
	}
	t := &mcStructure{}
	copy(t.size[:], size)
	st, _ := root["structure"].(map[string]any)
	layers, _ := st["block_indices"].([]any)
	if len(layers) == 0 {
		return nil, fmt.Errorf("mcstructure %s: no block indices", path)
	}
	idx, _ := layers[0].([]any)
	n := int(t.size[0]) * int(t.size[1]) * int(t.size[2])
	if len(idx) != n {
		return nil, fmt.Errorf("mcstructure %s: %d block indices for %d cells", path, len(idx), n)
	}
	pal, _ := st["palette"].(map[string]any)
	def, _ := pal["default"].(map[string]any)
	list, _ := def["block_palette"].([]any)
	states := make([]uint32, len(list))
	for i, e := range list {
		states[i] = voidCell
		m, _ := e.(map[string]any)
		name, _ := m["name"].(string)
		props, _ := m["states"].(map[string]any)
		if props == nil {
			props = map[string]any{}
		}
		if id, ok := o.reg.ByHash(blocks.Hash(blocks.State{Name: name, Props: props})); ok {
			states[i] = id
		}
	}
	t.blocks = make([]uint32, n)
	for i, v := range idx {
		t.blocks[i] = voidCell
		if k, ok := v.(int32); ok && k >= 0 && int(k) < len(states) {
			t.blocks[i] = states[k]
		}
	}
	return t, nil
}
