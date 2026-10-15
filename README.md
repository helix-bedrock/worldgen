# worldgen

Minecraft: Bedrock Edition world generation in Go, built from Bedrock's own
code. We decompiled the Bedrock Dedicated Server (BDS) and took the chunk
generation from it: noises, terrain shape, biomes, surface, caves, aquifers and
features, with the same random draws in the same order. Every part is checked
block by block against chunks that BDS itself generated from the same seed.
The decompiled code is not in this repo. It was only used to read how the game
works.

**Status: early and under heavy development.** The base works: terrain, biomes,
surface, caves, ores, trees and most plants and ocean decorations generate
like BDS. Structures and many smaller details are still missing, and it will
take a while before generation is complete. This is an active project, and it
will be maintained.

**Goal:** match the latest Minecraft version exactly (currently BDS 1.26.52).
After that, every time Minecraft updates we will update this generator to the
new version and add everything the update adds.

Part of [Helix](https://github.com/helix-bedrock), an open-source Minecraft:
Bedrock Edition server engine in Go. Standard library only. GPL-3.0.

## What works

| Part | State |
|---|---|
| Random sources (chunk Mersenne Twister, Xoroshiro128++, named and positional seeding) | done |
| Climate noises, terrain splines, density, interpolation | done |
| Biomes (multi-noise search) | done |
| Surface (grass, sand, snow, badlands bands, steep slopes, powder snow, frozen patches) | done, small differences left |
| Aquifers, caves (cheese, spaghetti, noodle), carvers, canyons | done, canyon differences left |
| Ore veins and ore features | done |
| Feature rules and data-driven features (scatter, aggregate, sequence, single block, ores, ...) | mostly done |
| Trees | oak, birch, spruce, pine, jungle bush, azalea done; fancy oak, acacia, dark oak, big spruce/jungle, fallen logs missing |
| Plants: grass, flowers, ferns, sugar cane, pumpkins, bee nests | done |
| Ocean: seagrass, kelp, sea pickles, coral reefs, icebergs, blue ice, clay/sand/gravel discs | done (coral crust missing) |
| Freezing (ice on cold water, snow layers) | done |
| Water layer for kelp, seagrass and coral fans | done |
| Structures (villages, ruins, shipwrecks, mineshafts, strongholds, ...) | not started |
| Coral crust, magma and bubble columns, other small decorations | not started |
| Nether and End | not started |

## Performance

Average load time per finished chunk (terrain and features), measured on the
Helix test server while a player flew around for 21,000 chunks:

| Step | Average |
|---|---|
| Generation | about 19 ms (18.6 to 20.7 ms) |
| Copy into the server's chunk format | about 2.7 ms |

Before the cache and feature work it was 200 to 390 ms per chunk. Chunks are
generated on many threads at once, so the server loads far more than one
chunk per 19 ms.

## How it is checked

- `bdsdiff` (separate tool, not in this repo yet) generates the same chunks
  as a BDS world made with the same seed and lists every block that differs.
- Optimizations must not change the output: `TestFingerprint` hashes finished
  chunks before and after a change. Speed and memory matter, but matching BDS
  comes first.

## Data

`data/vanilla` holds Mojang's vanilla data that BDS ships and the generator
reads: biomes, features, feature rules, jigsaw structures and structure
files. `_provenance.tsv` says which pack each file came from. `testdata` holds
the BDS block and biome lists (exported with Endstone) used by the tests.

## Use

```go
reg, _ := blocks.Load(paletteFile, typesFile) // Endstone block export
gen, _ := worldgen.NewOverworld(seed, reg, biomeIDs)
cache := worldgen.NewTerrainCache(gen, 1024)
var c worldgen.Chunk
cache.Generate(chunkX, chunkZ, &c) // terrain + features; safe from many goroutines
```

The generator returns indices into the block list; the server maps them to its
own block IDs. `Chunk.Waterlogged` reports blocks that also hold water.

```bash
go test ./...
go test -run=^$ -bench=LoadRadius .   # chunks/s and memory
```

## Contributing

See the [contributing guide](https://github.com/helix-bedrock/.github/blob/main/CONTRIBUTING.md)
and the [AI policy](https://github.com/helix-bedrock/.github/blob/main/AI_POLICY.md).

## License

GPL-3.0. See [`LICENSE`](LICENSE).
