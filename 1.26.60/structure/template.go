package structure

import (
	"compress/gzip"
	"fmt"
	"io"

	"github.com/helix-bedrock/worldgen/1.26.60/blocks"
)

// Template is a structure template in Java Edition's .nbt format, the form
// BDS ships the jigsaw pieces of villages and other structures in
// (behavior_packs/*/structures/**/*.nbt). Block states are Java ones; BDS
// converts them when it loads the file (sub_D2B9E40 in 1.26.52).
type Template struct {
	Size     [3]int32
	Palette  []TemplateState
	Blocks   []TemplateBlock
	Entities []TemplateEntity
	// Bedrock marks an .mcstructure template, whose jigsaws name their
	// final states by Bedrock names.
	Bedrock bool
}

// TemplateState is one Java block state: its name ("minecraft:oak_stairs")
// and properties.
type TemplateState struct {
	Name  string
	Props map[string]string
}

// TemplateBlock is a block of a template: position inside it, palette index
// and block entity data (jigsaw connectors, chests, beds).
type TemplateBlock struct {
	Pos   [3]int32
	State int32
	NBT   map[string]any
}

// TemplateEntity is an entity of a template (villagers, animals).
type TemplateEntity struct {
	Pos      [3]float64
	BlockPos [3]int32
	NBT      map[string]any
}

// LoadTemplate reads a gzip-compressed Java structure template.
func LoadTemplate(r io.Reader) (*Template, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("template: open: %w", err)
	}
	root, err := blocks.ReadNBT(gz)
	if cerr := gz.Close(); err == nil && cerr != nil {
		err = cerr
	}
	if err != nil {
		return nil, fmt.Errorf("template: read: %w", err)
	}
	t := &Template{}
	size, ok := IntList(root["size"], 3)
	if !ok {
		return nil, fmt.Errorf("template: bad size")
	}
	copy(t.Size[:], size)
	pal, _ := root["palette"].([]any)
	for i, p := range pal {
		m, ok := p.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("template: palette entry %d is not a compound", i)
		}
		name, _ := m["Name"].(string)
		st := TemplateState{Name: name}
		if props, ok := m["Properties"].(map[string]any); ok {
			st.Props = make(map[string]string, len(props))
			for k, v := range props {
				if s, ok := v.(string); ok {
					st.Props[k] = s
				}
			}
		}
		t.Palette = append(t.Palette, st)
	}
	bl, _ := root["blocks"].([]any)
	t.Blocks = make([]TemplateBlock, 0, len(bl))
	for i, b := range bl {
		m, ok := b.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("template: block %d is not a compound", i)
		}
		pos, ok := IntList(m["pos"], 3)
		if !ok {
			return nil, fmt.Errorf("template: block %d has a bad pos", i)
		}
		state, ok := m["state"].(int32)
		if !ok || state < 0 || int(state) >= len(t.Palette) {
			return nil, fmt.Errorf("template: block %d has a bad state", i)
		}
		tb := TemplateBlock{State: state}
		copy(tb.Pos[:], pos)
		tb.NBT, _ = m["nbt"].(map[string]any)
		t.Blocks = append(t.Blocks, tb)
	}
	ents, _ := root["entities"].([]any)
	for i, e := range ents {
		m, ok := e.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("template: entity %d is not a compound", i)
		}
		var te TemplateEntity
		if p, ok := m["pos"].([]any); ok && len(p) == 3 {
			for k := range 3 {
				te.Pos[k], _ = p[k].(float64)
			}
		}
		if bp, ok := IntList(m["blockPos"], 3); ok {
			copy(te.BlockPos[:], bp)
		}
		te.NBT, _ = m["nbt"].(map[string]any)
		t.Entities = append(t.Entities, te)
	}
	return t, nil
}

// IntList reads an NBT list of n ints.
func IntList(v any, n int) ([]int32, bool) {
	l, ok := v.([]any)
	if !ok || len(l) != n {
		return nil, false
	}
	out := make([]int32, n)
	for i, x := range l {
		if out[i], ok = x.(int32); !ok {
			return nil, false
		}
	}
	return out, true
}
