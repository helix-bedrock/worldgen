package worldgen

import "embed"

// vanillaData holds Mojang's vanilla world generation data from BDS
// 1.26.52.3 (see data/vanilla/README.md) and the tables extracted from the
// BDS code in data/bds.
//
//go:embed data/vanilla/biomes data/vanilla/features data/vanilla/feature_rules data/vanilla/structures data/vanilla/worldgen data/bds
var vanillaData embed.FS
