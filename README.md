# worldgen — Bedrock world generation in Go, built from Bedrock's own code

> [!WARNING]
> **This is early and not finished.** It is under active, heavy development, and it will take a
> while before it is complete.
>
> Status as of 2026-10-20:
>
> - **The base Overworld matches BDS:** terrain, all biomes, surface layers, caves and canyons,
>   underground water and lava, ores and stone patches, and an unbroken bedrock floor.
> - **Caves:** lush caves, dripstone caves, the deep dark's sculk (with sensors and shriekers), glow
>   lichen, amethyst geodes, water and lava springs, and the sulfur caves' spikes, pools and
>   surface springs.
> - **Trees and plants:** every tree, including cherry and mangrove, huge mushrooms, grass and
>   ferns, flowers in every flower biome, sunflowers, wildflowers, pink petals, leaf litter, sweet
>   berries, pumpkins, sugar cane, mushrooms, bamboo, lily pads, cactus, dead bushes, melons, vines,
>   firefly bushes, dry grass, red shrubs, lilacs, rose bushes and peonies.
> - **Biome features:** ice spikes, forest rocks in giant taigas and grass patches in mangrove swamps.
> - **Oceans:** seagrass, kelp, coral reefs, coral crusts, sea anemones, icebergs, and magma with
>   bubble columns.
> - **Structures:** villages, mineshafts, dungeons, ancient cities, desert pyramids, jungle temples,
>   witch huts, igloos, ocean ruins, shipwrecks, buried treasure, ocean monuments, pillager outposts,
>   ruined portals, woodland mansions, strongholds, trail ruins, trial chambers, abandoned camps, fossils
>   and desert wells. Chests are still empty in game.
> - **The Nether and the End are not started.**
> - **The API is not stable.** Names and behaviour change between commits.
>
> **[STATUS.md](STATUS.md) lists everything that is generated and everything that is missing.**
> Most parts are close to BDS but not block for block yet; a refinement pass comes last.
>
> Bug reports and ideas are welcome, but do not depend on it yet.

> [!IMPORTANT]
> **Verified against Bedrock itself.** The generation comes from the decompiled Bedrock Dedicated
> Server (BDS): the same noises, the same rules, and the same random draws in the same order.
> Parts are checked against chunks that BDS itself generated from the same seed; the rest is checked
> in game, and a final refinement pass makes everything match block for block.
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

The full list of what is and is not generated is in [STATUS.md](STATUS.md).

| Part | State |
|---|---|
| Terrain, biomes, surface, caves, aquifers, ores | ✅ done |
| Ocean decorations, freezing | 🟡 all placed, close to BDS |
| Cave decorations | 🟡 lush, dripstone, deep dark sculk, glow lichen and sulfur caves close to BDS; geodes exact; springs match |
| Plants and flowers | 🟡 all placed, close to BDS |
| Trees | 🟡 all placed; mangroves too sparse |
| Villages | 🟡 placed and built, layout close but not block-exact |
| Dungeons and lava lakes | ✅ match BDS (a few differ where neighbouring chunks overlap) |
| Mineshafts | ✅ placement matches BDS; blocks close (refined at the end) |
| Ancient cities | 🟡 placement, layout and cave match BDS near the centre; sculk placed |
| Desert pyramids, jungle temples, witch huts, igloos | ✅ placement matches BDS; jungle temple blocks match BDS; the others close |
| Ocean ruins (warm and cold, large ones with clusters) | 🟡 written from BDS; not yet compared with BDS |
| Shipwrecks (ocean and beached, random woods, chests) | 🟡 written from BDS; not yet compared with BDS |
| Buried treasure, ocean monuments (all room types, sponge rooms, gold core) | 🟡 written from BDS; not yet compared with BDS |
| Pillager outposts (watchtower, overgrown overlay, cages, tents, logs, targets) | 🟡 written from BDS; not yet compared with BDS |
| Ruined portals (buried and surface, netherrack spread, aging) | 🟡 written from BDS; not yet compared with BDS |
| Woodland mansions (room grid, all floors and rooms, roofs, foundation, entrance steps) | 🟡 written from BDS; checked in game, close |
| Strongholds (first three on village chunks, grid placement, all rooms, portal room fallback) | 🟡 placement matches BDS's locate; checked in game, close |
| Trail ruins, trial chambers (placement matches BDS's locate; pools from BDS, buried terrain) | 🟡 written from BDS; trial chambers checked in game, close |
| Fossils, desert wells | 🟡 written from BDS; desert wells checked in game |
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
