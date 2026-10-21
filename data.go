// Package worldgen generates Bedrock worlds that match BDS block for block.
// The dimensions live in their own packages (overworld); this package holds
// the vanilla data they share.
package worldgen

import "embed"

// Data holds Mojang's vanilla world generation data from BDS 1.26.52.3 (see
// data/vanilla/README.md) and the tables extracted from the BDS code in
// data/bds. It is read only.
//
//go:embed data/vanilla/biomes data/vanilla/features data/vanilla/feature_rules data/vanilla/structures data/vanilla/worldgen data/bds
var Data embed.FS
