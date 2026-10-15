// Package blocks holds the block states the generator can place, loaded from
// a block list exported from BDS (Endstone DevTools: block_palette.nbt and
// block_types.json).
package blocks

import (
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"slices"
	"strconv"
)

// State is one block state: a block name and its properties. Property values
// are uint8 (byte and bool properties), int32 or string.
type State struct {
	Name  string
	Props map[string]any
}

// Registry is the list of block states and each block's default state. It is
// immutable after Load and safe for concurrent use.
type Registry struct {
	states   []State
	byHash   map[uint32]uint32
	defaults map[string]uint32
	tags     map[string][]string
	// light is each state's light dampening; nil until LoadLight.
	light []uint8
}

// Load reads the gzipped big-endian NBT palette and the block types JSON.
func Load(palette io.Reader, types io.Reader) (*Registry, error) {
	gz, err := gzip.NewReader(palette)
	if err != nil {
		return nil, fmt.Errorf("blocks: open palette: %w", err)
	}
	root, err := (&nbtReader{r: gz}).root()
	if cerr := gz.Close(); err == nil && cerr != nil {
		err = cerr
	}
	if err != nil {
		return nil, fmt.Errorf("blocks: read palette: %w", err)
	}
	list, ok := root["blocks"].([]any)
	if !ok {
		return nil, fmt.Errorf("blocks: palette has no blocks list")
	}
	reg := &Registry{byHash: map[uint32]uint32{}, defaults: map[string]uint32{}, tags: map[string][]string{}}
	for i, e := range list {
		m, ok := e.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("blocks: palette entry %d is not a compound", i)
		}
		name, ok := m["name"].(string)
		if !ok {
			return nil, fmt.Errorf("blocks: palette entry %d has no name", i)
		}
		props, _ := m["states"].(map[string]any)
		if props == nil {
			props = map[string]any{}
		}
		s := State{Name: name, Props: props}
		idx := uint32(len(reg.states))
		reg.states = append(reg.states, s)
		reg.byHash[Hash(s)] = idx
	}

	var typ map[string]struct {
		Default uint32   `json:"defaultBlockStateHash"`
		Tags    []string `json:"tags"`
	}
	if err := json.NewDecoder(types).Decode(&typ); err != nil {
		return nil, fmt.Errorf("blocks: read block types: %w", err)
	}
	missing := 0
	for name, t := range typ {
		if len(t.Tags) > 0 {
			reg.tags[name] = t.Tags
		}
		idx, ok := reg.byHash[t.Default]
		if !ok {
			// minecraft:unknown has a reserved hash and no palette entry.
			missing++
			continue
		}
		reg.defaults[name] = idx
	}
	// More than a handful missing means the hash or the files do not match.
	if missing > 4 {
		return nil, fmt.Errorf("blocks: %d of %d default states not found in the palette", missing, len(typ))
	}
	return reg, nil
}

// LoadLight reads light dampening values: a JSON object from state hash
// (decimal) to dampening, listing every state that is not 15 (fully
// opaque). It is exported from BDS block states. Call it before using the
// registry from several goroutines.
func (r *Registry) LoadLight(rd io.Reader) error {
	var m map[string]uint8
	if err := json.NewDecoder(rd).Decode(&m); err != nil {
		return fmt.Errorf("blocks: read light dampening: %w", err)
	}
	r.light = make([]uint8, len(r.states))
	for i := range r.light {
		r.light[i] = 15
	}
	for h, d := range m {
		v, err := strconv.ParseUint(h, 10, 32)
		if err != nil {
			return fmt.Errorf("blocks: light dampening key %q: %w", h, err)
		}
		if i, ok := r.byHash[uint32(v)]; ok {
			r.light[i] = d
		}
	}
	return nil
}

// LightDampening returns how much a state dampens light, 0 to 15. Without
// LoadLight, air is 0 and everything else 15.
func (r *Registry) LightDampening(i uint32) uint8 {
	if int(i) < len(r.light) {
		return r.light[i]
	}
	if s, ok := r.State(i); ok && s.Name == "minecraft:air" {
		return 0
	}
	return 15
}

// Tags returns a block's tags as BDS reports them ("minecraft:dirt", "wood").
func (r *Registry) Tags(name string) []string { return r.tags[name] }

// Len returns the number of states.
func (r *Registry) Len() int { return len(r.states) }

// State returns state i. The bool is false if i is out of range.
func (r *Registry) State(i uint32) (State, bool) {
	if int(i) >= len(r.states) {
		return State{}, false
	}
	return r.states[i], true
}

// Default returns the index of a block's default state.
func (r *Registry) Default(name string) (uint32, bool) {
	i, ok := r.defaults[name]
	return i, ok
}

// Hash returns the network hash of a state: FNV-1a (32-bit) over the state
// written as little-endian NBT {name, states} with the state keys sorted.
func Hash(s State) uint32 {
	var b []byte
	str := func(v string) {
		b = binary.LittleEndian.AppendUint16(b, uint16(len(v)))
		b = append(b, v...)
	}
	b = append(b, tagCompound, 0, 0)
	b = append(b, tagString)
	str("name")
	str(s.Name)
	b = append(b, tagCompound)
	str("states")
	keys := make([]string, 0, len(s.Props))
	for k := range s.Props {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		switch v := s.Props[k].(type) {
		case string:
			b = append(b, tagString)
			str(k)
			str(v)
		case uint8:
			b = append(b, tagByte)
			str(k)
			b = append(b, v)
		case int32:
			b = append(b, tagInt)
			str(k)
			b = binary.LittleEndian.AppendUint32(b, uint32(v))
		}
	}
	b = append(b, tagEnd, tagEnd)
	h := fnv.New32a()
	_, _ = h.Write(b)
	return h.Sum32()
}

// Lookup returns the state of name with props applied over its default
// state. JSON-style values are accepted: bool for byte properties and
// float64 for int properties.
func (r *Registry) Lookup(name string, props map[string]any) (uint32, bool) {
	def, ok := r.defaults[name]
	if !ok {
		return 0, false
	}
	if len(props) == 0 {
		return def, true
	}
	base := r.states[def]
	s := State{Name: name, Props: make(map[string]any, len(base.Props))}
	for k, v := range base.Props {
		s.Props[k] = v
	}
	for k, v := range props {
		old, ok := base.Props[k]
		if !ok {
			return 0, false
		}
		switch old.(type) {
		case uint8:
			switch t := v.(type) {
			case bool:
				if t {
					v = uint8(1)
				} else {
					v = uint8(0)
				}
			case float64:
				v = uint8(t)
			}
		case int32:
			if t, ok := v.(float64); ok {
				v = int32(t)
			}
		}
		s.Props[k] = v
	}
	i, ok := r.byHash[Hash(s)]
	return i, ok
}
