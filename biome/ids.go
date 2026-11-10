package biome

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// rawID is one biome entry in biomes.json.
type rawID struct {
	ID *uint32 `json:"id"`
}

// LoadIDs reads the numeric biome IDs from a biomes.json exported from BDS
// (Endstone DevTools): {"minecraft:plains": {"id": 1}, ...}. Names are
// returned without the "minecraft:" prefix.
func LoadIDs(r io.Reader) (map[string]uint32, error) {
	var raw map[string]rawID
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		return nil, fmt.Errorf("biome: read ids: %w", err)
	}
	ids := make(map[string]uint32, len(raw))
	for name, v := range raw {
		if v.ID == nil {
			continue
		}
		ids[strings.TrimPrefix(name, "minecraft:")] = *v.ID
	}
	return ids, nil
}

// Missing returns the biome names used by entries that ids does not have.
func Missing(entries []Entry, ids map[string]uint32) []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range entries {
		if _, ok := ids[e.Biome]; !ok && !seen[e.Biome] {
			seen[e.Biome] = true
			out = append(out, e.Biome)
		}
	}
	return out
}
