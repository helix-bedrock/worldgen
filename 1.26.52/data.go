// Package worldgen generates Bedrock worlds that match BDS block for block.
// The dimensions live in their own packages (overworld); this package holds
// the vanilla data they share.
package worldgen

import files "github.com/helix-bedrock/helix-data/data/1.26.52"

// Data holds Mojang's vanilla behaviour pack files from BDS 1.26.52.3 under
// vanilla/ (biomes, features, feature_rules, structures, worldgen and the
// rest, stacked as BDS stacks them) and the tables extracted from the BDS
// code under bds/. Both come from helix-bedrock/helix-data. It is read only.
var Data = files.Files()
