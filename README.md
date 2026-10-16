# worldgen — Bedrock world generation in Go, built from Bedrock's own code

> [!WARNING]
> **This is early and not finished.** It is under active, heavy development, and it will take a
> while before it is complete.
>
> Status as of 2026-10-16:
>
> - **The base world matches BDS:** terrain, biomes, surface, caves, aquifers, ores and most ocean
>   decorations.
> - **Trees:** oak, birch, spruce, pine, jungle, fancy (big) oak, giant jungle and giant spruce/pine,
>   acacia, dark oak, pale oak, azalea, fallen logs and the dappled forest's poplars. Cherry and
>   mangrove trees are not done yet.
> - **Plants:** grass, flowers (plains, forest, flower forest, swamp, meadow), wildflowers, leaf
>   litter, sweet berry bushes, mushrooms, shelf mushrooms and swamp trees.
> - **Structures:** villages are placed and built (with loot chests). Dungeons (spawner, two loot
>   chests, mossy floor), surface lava lakes and mineshafts (normal and badlands, with cave spider
>   spawners, cobwebs, rails, torches and support pillars) are placed where BDS places them, close
>   to block for block. Ancient cities are placed and laid out where BDS puts them, in the cave BDS
>   carves around them (checked in game against a vanilla world), but their sculk (on the city
>   and in the caves around it) is not placed yet. Everything else
>   (ruins, shipwrecks, strongholds, trial chambers...) is not started. Mineshaft minecarts with
>   chests are not placed yet.
> - **A refinement pass comes last:** every biome and system will be compared block by block with
>   BDS once everything is in.
> - **Chest loot:** structure chests carry their loot table, but chests are still empty in game.
> - **Still missing:** lily pads, huge mushrooms, melons, coral crust, magma and bubble columns,
>   double tall grass in swamps and more.
> - **The Nether and the End are not started.**
> - **The API is not stable.** Names and behaviour change between commits.
>
> Bug reports and ideas are welcome, but do not depend on it yet.

> [!IMPORTANT]
> **Verified against Bedrock itself.** The generation comes from the decompiled Bedrock Dedicated
> Server (BDS): the same noises, the same rules, and the same random draws in the same order.
> Every part is checked block by block against chunks that BDS itself generated from the same seed.
> The decompiled code is **not** in this repo; it was only used to read how the game works.

---

<div align="center">

<h3>worldgen</h3>

<p>
  <b>Vanilla Minecraft: Bedrock Edition world generation</b><br>
  Fast, low memory, standard library only
</p>

[![Minecraft](https://img.shields.io/badge/minecraft-v1.26.52_(Bedrock)-black)](https://feedback.minecraft.net/hc/en-us/sections/360001186971-Release-Changelogs)
[![Go](https://img.shields.io/badge/go-1.26-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![License](https://img.shields.io/badge/license-GPL--3.0-blue)](LICENSE)
[![Status](https://img.shields.io/badge/status-early_development-orange)](#roadmap)

</div>

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
gen, _ := worldgen.NewOverworld(seed, reg, biomeIDs)
cache := worldgen.NewTerrainCache(gen, 1024)

var c worldgen.Chunk
cache.Generate(chunkX, chunkZ, &c) // terrain + features, safe from many goroutines
```

The generator returns indices into the block list; your server maps them to its own block IDs.

## Features

- **Same world as vanilla:** seeds give BDS's terrain, biomes, caves, ores and decorations.
- **Data-driven:** biomes, features and feature rules are read from Mojang's own data files in
  [`data/vanilla`](data/vanilla), not hard-coded.
- **Fast:** about 19 ms per finished chunk, generated on many threads at once.
- **Low memory:** cached chunks are stored packed, at about a fifth of their full size.
- **No dependencies:** only the Go standard library.

## Performance

Measured on the Helix test server while a player flew through 21,000 chunks:

| Step | Average per chunk |
|---|---|
| Generation (terrain and features) | **~19 ms** |
| Copy into the server's chunk format | ~2.7 ms |

It started at 200 to 390 ms per chunk. Speed and memory matter, but matching BDS comes first: an
optimization that changes the world is not accepted.

## Roadmap

| Part | State |
|---|---|
| Terrain, biomes, surface, caves, aquifers, ores | ✅ done |
| Ocean decorations, freezing | ✅ done |
| Plants and flowers | 🟡 most done (no lily pads, huge mushrooms, melons) |
| Trees | 🟡 most done (no cherry, mangrove) |
| Villages | 🟡 placed and built, layout close but not block-exact |
| Dungeons and lava lakes | ✅ match BDS (a few differ where neighbouring chunks overlap) |
| Mineshafts | ✅ placement matches BDS; blocks close (refined at the end) |
| Ancient cities | 🟡 placement, layout and cave match BDS; sculk not yet |
| Other structures | ⬜ not started (trial chambers next) |
| Nether and End | ⬜ not started |

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
