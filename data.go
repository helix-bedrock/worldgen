// Package worldgen generates Bedrock worlds that match BDS block for block.
// The dimensions live in their own packages (overworld); this package holds
// the vanilla data they share.
package worldgen

import (
	"io/fs"
	"path"
	"slices"
	"strings"

	files "github.com/helix-bedrock/helix-data/data/1.26.60"
)

// Data holds Mojang's vanilla behaviour pack files from BDS 1.26.60.6 under
// vanilla/ (biomes, features, feature_rules, structures, worldgen and the
// rest, stacked as BDS stacks them), each experimental pack's own files
// under experiments/<name>/, and the tables extracted from the BDS code
// under bds/. All come from helix-bedrock/helix-data. It is read only.
var Data = files.Files()

// ExperimentDrop4Of2026 is the helix-data folder of the "Drop 4 of 2026"
// experiment (ice caves).
const ExperimentDrop4Of2026 = "y_2026_drop_4"

// WithExperiments returns Data with the files of the named experiments put
// over vanilla/, later names winning, as BDS loads an enabled experiment's
// pack above the vanilla packs. With no names it returns Data.
func WithExperiments(names ...string) fs.FS {
	if len(names) == 0 {
		return Data
	}
	return overlay{names: names}
}

// overlay serves vanilla/<p> from the last experiment that has
// experiments/<name>/<p>, else from vanilla/. Directory listings merge.
type overlay struct{ names []string }

// layers returns the paths that may hold name, the winning one first.
func (o overlay) layers(name string) []string {
	rest, ok := strings.CutPrefix(name, "vanilla/")
	if !ok && name != "vanilla" {
		return []string{name}
	}
	out := make([]string, 0, len(o.names)+1)
	for _, n := range slices.Backward(o.names) {
		out = append(out, path.Join("experiments", n, rest))
	}
	return append(out, name)
}

func (o overlay) Open(name string) (fs.File, error) {
	var err error
	for _, p := range o.layers(name) {
		var f fs.File
		if f, err = Data.Open(p); err == nil {
			return f, nil
		}
	}
	return nil, err
}

func (o overlay) ReadFile(name string) ([]byte, error) {
	var err error
	for _, p := range o.layers(name) {
		var b []byte
		if b, err = Data.ReadFile(p); err == nil {
			return b, nil
		}
	}
	return nil, err
}

func (o overlay) ReadDir(name string) ([]fs.DirEntry, error) {
	seen := map[string]bool{}
	var out []fs.DirEntry
	var err error
	found := false
	for _, p := range o.layers(name) {
		des, e := Data.ReadDir(p)
		if e != nil {
			err = e
			continue
		}
		found = true
		for _, de := range des {
			if !seen[de.Name()] {
				seen[de.Name()] = true
				out = append(out, de)
			}
		}
	}
	if !found {
		return nil, err
	}
	slices.SortFunc(out, func(a, b fs.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	return out, nil
}
