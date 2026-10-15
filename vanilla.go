package worldgen

import "embed"

// vanillaData holds Mojang's vanilla world generation data from BDS
// 1.26.52.3; see data/vanilla/README.md.
//
//go:embed data/vanilla/biomes data/vanilla/features data/vanilla/feature_rules
var vanillaData embed.FS
