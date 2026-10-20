# What worldgen generates

Status as of 2026-10-19. This page lists everything worldgen places today and what is still
missing, so you know what to expect in a world. Only the **Overworld** is generated so far.

Every part is built from the Bedrock Dedicated Server (BDS 1.26.52) and compared with chunks that
BDS generated from the same seed. The marks mean:

- **Exact**: compared block for block with BDS and matches.
- **Close**: in place and looks right, but not block for block yet. It is fixed in the final
  refinement pass.
- **Not yet**: not generated at all.

## Terrain and the world

| What | Status |
| --- | --- |
| Terrain shape (hills, mountains, plateaus, valleys) | Exact |
| All Overworld biomes, stored per block with BDS's jagged edges | Exact |
| Surface layers (grass, sand, sandstone, snow, terracotta bands in badlands, mud, podzol...) | Exact |
| Caves (cheese, spaghetti and noodle caves), cave carvers and canyons | Exact |
| Underground water and lava (aquifers), and lava below y -55 | Exact |
| Bedrock floor (never replaced by anything generated later) | Exact |
| Freezing: ice on water and snow layers in cold biomes | Close |
| Icebergs in frozen oceans | Close |

## Ores and underground blocks

| What | Status |
| --- | --- |
| Coal, iron, copper, gold, redstone, lapis, diamond and emerald ore (with their deepslate versions) | Exact |
| Badlands extra gold, dripstone caves extra copper | Exact |
| Granite, diorite, andesite, tuff, deepslate, dirt and gravel patches | Exact |
| Copper and iron ore veins (with raw ore blocks) | Exact |
| Infested (silverfish) stone in mountains | Exact |
| Clay, sand and gravel discs under water | Close |
| Dungeons (spawner, two loot chests, mossy floor) and lava lakes | Close |

## Caves and cave biomes

| What | Status |
| --- | --- |
| Lush caves: moss, clay pools, cave vines with glow berries, big and small dripleaves, azaleas, spore blossoms, vines, azalea roots | Close |
| Dripstone caves: dripstone blocks, pointed dripstone, large dripstone | Close |
| Deep dark: sculk, sculk veins, sculk sensors, sculk shriekers, sculk catalysts | Close |
| Glow lichen in caves everywhere | Close |
| Amethyst geodes (smooth basalt, calcite, amethyst, budding amethyst, buds and clusters) | Exact |
| Sulfur caves: the biome, its floor and sulfur pools | Close |
| Sulfur caves: sulfur spikes and spike clusters | Not yet |

## Trees

| What | Status |
| --- | --- |
| Oak, birch, spruce, pine, jungle, acacia, dark oak, pale oak, azalea | Close |
| Big (fancy) oak, giant jungle, giant spruce and pine | Close |
| Fallen logs and the dappled forest's poplars | Close |
| Swamp trees | Close |
| Cherry trees | Not yet |
| Mangrove trees (and their roots) | Not yet |

## Plants and flowers

| What | Status |
| --- | --- |
| Short grass, tall grass, ferns and large ferns | Close |
| Flowers in plains, forest, flower forest, swamp, meadow, savanna and jungle | Close |
| Sunflowers and other tall flowers | Close |
| Wildflowers, pink petals, leaf litter, eyeblossoms, bushes, dry grass | Close |
| Pale moss in the pale garden | Close |
| Sweet berry bushes, pumpkins, sugar cane | Close |
| Brown and red mushrooms (the scattered ones), shelf mushrooms | Close |
| Bamboo in bamboo jungles | Close |
| Lily pads in swamps | Close |
| Cactus | Not yet |
| Dead bushes | Not yet |
| Melons | Not yet |
| Bamboo in regular jungles | Not yet |
| Huge mushrooms, and the extra mushroom patches in taiga, mega taiga and swamp | Not yet |
| Firefly bushes | Not yet |
| Mangrove swamp grass and dead bushes | Not yet |

## Oceans and water

| What | Status |
| --- | --- |
| Seagrass and kelp | Close |
| Coral reefs, coral fans and sea pickles | Close |
| Coral crust and sea anemones | Not yet |
| Magma blocks and bubble columns on the ocean floor | Not yet |
| Water and lava springs in cliffs | Not yet |

## Other features

| What | Status |
| --- | --- |
| Forest rocks (mossy cobblestone boulders in giant taigas) | Not yet |
| Ice spikes and their ice patches | Not yet |
| Fossils (desert and swamp; deepslate fossils with diamond ore) | Close (not yet checked in game) |
| Desert wells (with suspicious sand) | Close (checked in game) |
| Emerald ore in mountain surfaces | Not yet |

## Structures

| What | Status |
| --- | --- |
| Villages (all biome types, with loot chests) | Close |
| Mineshafts (normal and badlands; cobwebs, rails, torches, cave spider spawners; fences joined to their walls) | Close |
| Ancient cities, with the cave around them and their sculk | Close |
| Mineshaft minecarts with chests | Not yet |
| Ocean ruins (warm and cold, large ones with clusters of small ones, chests) | Close (not yet compared with BDS; no suspicious sand or gravel yet) |
| Shipwrecks (ocean and beached, random wood pairs, supply, map and treasure chests) | Close (not yet compared with BDS) |
| Buried treasure (chest under beaches) | Close (not yet compared with BDS) |
| Ocean monuments (room graph, all room types, wings, penthouse, sponge rooms, gold core; no guardians) | Close (not yet compared with BDS) |
| Desert pyramids (with the TNT trap and the suspicious sand cellar) | Close |
| Jungle temples (traps, dispensers, lever puzzle) | Exact (tree leaves can poke inside; fixed in refinement) |
| Witch huts (cauldron with a random potion) | Close |
| Igloos (half with a basement) | Close |
| Pillager outposts (watchtower, overgrown overlay, feature pieces, chest; no pillagers) | Close (not yet compared with BDS) |
| Ruined portals (buried and on the surface, netherrack spread, aging) | Close (not yet compared with BDS) |
| Woodland mansions (all floors and rooms, roofs, foundation, entrance steps, joined windows) | Close (checked in game) |
| Strongholds (all rooms, library, portal room with silverfish spawner, joined fences and bars) | Close (checked in game) |
| Trail ruins (buried under a mound, with suspicious gravel) | Close (placement matches BDS) |
| Trial chambers (all chamber kinds, copper bulb aging, buried shell; spawners and vaults have no mob or loot data yet) | Close (placement matches BDS; checked in game) |

Structure chests, dispensers and suspicious sand carry their loot table, but the loot is not filled in yet, so chests are empty in
game.

## Other dimensions

| What | Status |
| --- | --- |
| The Nether | Not yet |
| The End | Not yet |
