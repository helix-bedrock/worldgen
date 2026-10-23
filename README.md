<div align="center">

<img src=".github/helix-mark.svg" alt="Helix" width="120">

<p>
  <b>Vanilla Minecraft: Bedrock Edition world generation</b><br>
  Fast, low memory, standard library only
</p>

[![Minecraft](https://img.shields.io/badge/minecraft-v1.26.52_(Bedrock)-black)](https://feedback.minecraft.net/hc/en-us/sections/360001186971-Release-Changelogs)
[![Go](https://img.shields.io/badge/go-1.26-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![License](https://img.shields.io/badge/license-GPL--3.0-blue)](LICENSE)
[![Status](https://img.shields.io/badge/status-refinement_pass-yellow)](#status)

</div>

## Status

Status as of 2026-10-22: **all three dimensions are generated**, built from BDS 1.26.52.

- **Overworld:** terrain, every biome, surfaces, caves and canyons, aquifers, ores, every tree and
  plant, oceans and reefs, and every structure (villages, mineshafts, dungeons, ancient cities,
  temples, igloos, witch huts, ocean ruins, shipwrecks, buried treasure, monuments, outposts, ruined
  portals, mansions, strongholds, trail ruins, trial chambers, abandoned camps, fossils, wells).
- **Nether:** terrain, the five biomes and their decorations, ores, fossils, fortresses, ruined
  portals and bastion remnants.
- **End:** terrain, the obsidian pillars with their End crystals, small islands, chorus plants, end
  gateways and End cities with their ships. The exit portal is provided for the engine's dragon
  fight, which builds it in BDS.

Most parts match BDS block for block; the rest are close. What is left:

- **A refinement pass** over all three dimensions for the small differences that remain (stair
  corner shapes, some cross-chunk decoration order, a few structures not yet compared).
  [STATUS.md](STATUS.md) lists every part and how well it matches.
- **Mobs in structures:** worldgen hands them to the engine as spawn records; only the End crystals
  so far.
- **Loot:** chests get their loot table and seed; filling them is up to the server.
- **The API is not stable yet.** Names and behaviour may still change between commits. The world
  `Config`s (see below) are the newest part.

## How it is checked

The generation comes from the decompiled Bedrock Dedicated Server (BDS): the same noises, the same
rules and the same random draws in the same order. Each part is compared with chunks BDS itself
generated from the same seed (or checked in game where that is not possible yet). The decompiled
code is **not** in this repo; it was only used to read how the game works.

## Why worldgen?

Custom Bedrock servers usually generate worlds that only look like vanilla: seeds give different
terrain, and ores, trees and caves end up in other places. worldgen aims to be a one-to-one copy of
Bedrock's own generator, so a seed gives the same world as on a vanilla server. It is part of
[Helix](https://github.com/helix-bedrock), an open-source Bedrock server engine in Go, but it is a
plain library that any Go project can use.

## Quick Start

```shell
go get github.com/helix-bedrock/worldgen
```

```go
reg, _ := blocks.Load(paletteFile, typesFile) // block list exported from BDS
gen, _ := overworld.NewOverworld(seed, reg, biomeIDs)
cache := overworld.NewTerrainCache(gen, 1024)

var c overworld.Chunk
cache.Generate(chunkX, chunkZ, &c) // terrain + features, safe from many goroutines
```

The generator returns indices into the block list; your server maps them to its own block IDs.

Each dimension has its own package: [`overworld`](overworld), [`nether`](nether) and [`end`](end). The shared vanilla data and the `biome`, `blocks`, `noise` and `rand` packages stay at the top,
with [`structure`](structure): templates, jigsaw assembly, block rules and the ruined portal builder
shared by every dimension's structures. The dimension packages do not import each other.

## Customizing the world

Everything is vanilla by default. A `Config` changes only what you ask for, and every
change is checked when the generator is built: a misspelled name is an error that lists
the closest real names.

```go
cfg := overworld.Vanilla()

// Biomes
cfg.Biomes.Remove("mushroom_island")         // its neighbours fill the space
cfg.Biomes.Replace("roofed_forest", "forest")
cfg.Biomes.Scale("cherry_grove", 1.5)        // a bigger climate box, so more of it
cfg.Biomes.Add(overworld.Biome{Name: "myserver:crystal_plains", Like: "plains",
	Climate: overworld.Climate{Temperature: overworld.Range{Min: 0.2, Max: 0.55}}})

// Features: trees, plants, ores, rocks...
cfg.Features.Disable("birch_forest_surface_trees_feature")
cfg.Features.DisableIn([]string{"plains"}, "*flower*")
cfg.Features.DisableFeature("*birch*")       // everywhere, also inside tree mixes
cfg.Features.SetChance("plains_surface_trees_feature", 50)
cfg.Features.SetWeight("random_roofed_forest_feature", "minecraft:huge_mushroom_feature", 0)
cfg.Features.Register("myserver:crystal", crystalFeature{}) // your own Go code
cfg.Features.AddRule(overworld.Rule{ID: "myserver:crystals", Places: "myserver:crystal",
	Pass: overworld.SurfacePass, Tags: []string{"plains"}, Iterations: 2})
cfg.AddPack(os.DirFS("my_addon")) // features/ and feature_rules/ in Mojang's add-on format

// Structures: where...
cfg.Structures.Disable(overworld.StructureMansion)
cfg.Structures.Exclude(overworld.Box(-500, -500, 500, 500)) // nothing near spawn
cfg.Structures.SetPlacement(overworld.StructureVillage, overworld.Placement{Spacing: 20, Separation: 6})
cfg.Structures.SetBiomes(overworld.StructureMansion, "plains", "forest")
cfg.Structures.PlaceAt(overworld.StructureVillage, 2, 3)  // a village in chunk (2, 3)
cfg.Structures.PlaceAt(overworld.StructureStronghold, 0, 0)
// ...and what
cfg.Structures.SetPoolWeight("minecraft:village/plains/houses", "village/plains/houses/plains_small_house_1", 0)
cfg.Structures.AddToPool("minecraft:village/plains/houses", "myserver/house", 3) // from a pack
cfg.Structures.ReplaceTemplate("village/plains/houses/plains_big_house_1", "myserver/big_house")
cfg.Structures.ReplaceLoot("loot_tables/chests/village/village_plains_house.json", "loot_tables/my/house.json")
cfg.Structures.ReplaceBlock(overworld.StructureVillage, "cobblestone", "mossy_cobblestone")

// Terrain and whole steps
cfg.Terrain = overworld.Terrain{SeaLevel: 70, SeaBlock: "lava", NoDeepslate: true}
cfg.Skip.Caves = true

gen, err := overworld.NewOverworld(seed, reg, biomeIDs, overworld.WithConfig(cfg))
```

What a change does not touch stays as in vanilla: a disabled rule still makes its random
draws, so the features around it do not move. `FeatureRuleIDs`, `FeatureIDs`,
`StructureKinds` and `Placement` list what there is to change, and `TerrainCache.Explain`
says which step placed any block.

Server owners who do not write Go can put the same changes in a JSON file:
`overworld.ParseConfig` reads it (its doc comment shows the format) and `Config.MarshalJSON`
writes one. The Nether and the End have their own `Config`s (`nether.WithConfig`, `end.WithConfig`, each
with `ParseConfig` and `MarshalJSON` too): biomes, feature rules, structures (with templates,
bastion pools, loot and block swaps) and steps in the Nether; End cities and the pillars,
islands, chorus and gateways in the End.

## Features

- **Same world as vanilla:** seeds give BDS's terrain, biomes, caves, ores and decorations.
- **Data-driven:** biomes, features and feature rules are read from Mojang's own data files in
  [`data/vanilla`](data/vanilla), not hard-coded.
- **Fast:** about 30 to 40 ms per finished chunk, generated on many threads at once.
- **Low memory:** cached chunks are stored packed, at about a fifth of their full size.
- **No dependencies:** only the Go standard library.

## Performance

Measured on the Helix test server while a player flew through 17,000 chunks:

| Step | Average per chunk |
|---|---|
| Generation (terrain, features and structures) | **~30 to 40 ms** |
| Copy into the server's chunk format | ~3.5 ms |

Structures and the full decoration raised it from the earlier 19 ms. It started at 200 to 390 ms
per chunk. Speed and memory matter, but matching BDS comes first: an
optimization that changes the world is not accepted.

## Roadmap

The full list of what is and is not generated is in [STATUS.md](STATUS.md).

| Part | State |
|---|---|
| Terrain, biomes, surface, caves, aquifers, ores | ✅ done |
| Ocean decorations, freezing | 🟡 all placed, close to BDS |
| Cave decorations | 🟡 lush, dripstone, deep dark sculk, glow lichen and sulfur caves close to BDS; geodes exact; springs match |
| Plants and flowers | 🟡 all placed, close to BDS |
| Trees | 🟡 all placed; mangroves too sparse |
| Villages | 🟡 assembled as BDS does (pieces, farms, decorations); one known layout drift left |
| Dungeons and lava lakes | ✅ match BDS (a few differ where neighbouring chunks overlap) |
| Mineshafts | ✅ placement matches BDS; blocks close (refined at the end) |
| Ancient cities | 🟡 placement, layout and cave match BDS near the centre; sculk placed |
| Desert pyramids, jungle temples, witch huts, igloos | ✅ placement matches BDS; jungle temple blocks match BDS; the others close |
| Ocean ruins (warm and cold, large ones with clusters) | 🟡 written from BDS; not yet compared with BDS |
| Shipwrecks (ocean and beached, random woods, chests) | 🟡 written from BDS; not yet compared with BDS |
| Buried treasure, ocean monuments (all room types, sponge rooms, gold core) | 🟡 written from BDS; not yet compared with BDS |
| Pillager outposts (watchtower, overgrown overlay, cages, tents, logs, targets) | 🟡 written from BDS; not yet compared with BDS |
| Ruined portals (buried and surface, netherrack spread, aging) | 🟡 written from BDS; the same code matches BDS in the Nether, overworld portals not yet compared |
| Woodland mansions (room grid, all floors and rooms, roofs, foundation, entrance steps) | 🟡 written from BDS; checked in game, close |
| Strongholds (first three on village chunks, grid placement, all rooms, portal room fallback) | 🟡 placement matches BDS's locate; checked in game, close |
| Trail ruins, trial chambers (placement matches BDS's locate; pools from BDS, buried terrain) | 🟡 written from BDS; trial chambers match BDS for the first 26 pieces, then drift |
| Fossils, desert wells | 🟡 written from BDS; desert wells checked in game |
| Nether (terrain, cave carver, ores, all five biomes' decorations, fortresses, ruined portals, bastions) | 🟡 matches BDS closely; fortresses, ruined portals and bastions match; fungus trees close |
| The End (terrain, pillars and crystals, islands, chorus, gateways, End cities, exit portal) | ✅ matches BDS; a few chorus plants and stair corner shapes differ (refinement) |

**The goal is to match the latest Minecraft version exactly.** After that, every time Minecraft
updates, worldgen will be updated to the new version and gain everything the update adds. The
project is maintained.

## Contributing

- **Found a bug or a difference from vanilla?** Open an [issue](https://github.com/helix-bedrock/worldgen/issues)
- **Want to contribute code?** Read the [contributing guide](https://github.com/helix-bedrock/.github/blob/main/CONTRIBUTING.md)
  and the [AI policy](https://github.com/helix-bedrock/.github/blob/main/AI_POLICY.md)

## License

worldgen is licensed under [GPL-3.0](LICENSE). The files in [`data/vanilla`](data/vanilla) are
Mojang's vanilla data shipped with BDS; [`_provenance.tsv`](data/vanilla/_provenance.tsv) lists
where each one came from.
