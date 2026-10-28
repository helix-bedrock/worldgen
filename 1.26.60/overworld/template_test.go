package overworld

import (
	"io/fs"
	"strings"
	"testing"

	worldgen "github.com/helix-bedrock/worldgen/1.26.60"
	"github.com/helix-bedrock/worldgen/1.26.60/structure"
)

// TestLoadTemplates loads every Java structure template shipped in the
// vanilla data and checks a known one.
func TestLoadTemplates(t *testing.T) {
	n := 0
	err := fs.WalkDir(worldgen.Data, "vanilla/structures", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".nbt") {
			return err
		}
		f, err := worldgen.Data.Open(p)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		tpl, err := structure.LoadTemplate(f)
		if err != nil {
			t.Errorf("%s: %v", p, err)
			return nil
		}
		n++
		if strings.HasSuffix(p, "village/plains/houses/plains_small_house_1.nbt") {
			if tpl.Size != [3]int32{7, 7, 7} {
				t.Errorf("plains_small_house_1 size %v, want 7x7x7", tpl.Size)
			}
			jig := 0
			for _, b := range tpl.Blocks {
				if tpl.Palette[b.State].Name == "minecraft:jigsaw" {
					jig++
					if pool, _ := b.NBT["target_pool"].(string); pool != "minecraft:village/plains/streets" {
						t.Errorf("jigsaw targets %q", pool)
					}
				}
			}
			if jig != 1 {
				t.Errorf("plains_small_house_1 has %d jigsaw blocks, want 1", jig)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("loaded %d templates", n)
}
