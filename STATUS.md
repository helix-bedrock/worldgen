# What worldgen generates

Status as of 2026-10-22. This page lists everything worldgen places today and what is still
missing, so you know what to expect in a world. The **Overworld**, the **Nether** and the **End** are
generated; a refinement pass over all three comes next.

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
| Sulfur caves: the biome and its floor | Close |
| Sulfur caves: sulfur pools with potent sulfur | Close (both pools of the BDS compare area found; one sits a block higher where the terrain differs) |
| Sulfur caves: sulfur spikes and spike clusters | Close (positions match BDS) |
| Sulfur caves: sulfur spring trails (a sulfur column up to a sulfur spring on the surface) | Close (rare: none in the BDS compare area, none in ours) |

## Trees

| What | Status |
| --- | --- |
| Oak, birch, spruce, pine, jungle, acacia, dark oak, pale oak, azalea | Close |
| Big (fancy) oak, giant jungle, giant spruce and pine | Close |
| Fallen logs and the dappled forest's poplars | Close |
| Swamp trees | Close |
| Cherry trees (with pink petals underneath) | Close (block counts within a few % of BDS) |
| Mangrove trees (roots, muddy roots, moss carpet, hanging propagules, vines) | Close (about a third as many trees as BDS; fixed in refinement) |

## Plants and flowers

| What | Status |
| --- | --- |
| Short grass, tall grass, ferns and large ferns | Close |
| Flowers in plains, forest, flower forest, swamp, meadow, savanna and jungle | Close |
| Sunflowers and other tall flowers | Close |
| Wildflowers, pink petals, leaf litter, bushes | Close |
| Dry grass in deserts and badlands | Close |
| Eyeblossoms in the pale garden | Close (always placed closed; BDS opens them when the chunk is made at night) |
| Pale moss and pale moss carpets in the pale garden | Close |
| Sweet berry bushes, pumpkins, sugar cane | Close |
| Brown and red mushrooms (the scattered ones), shelf mushrooms | Close |
| Bamboo in bamboo and regular jungles, with podzol patches | Close |
| Lily pads in swamps and mangrove swamps | Close |
| Cactus and cactus flowers | Close (desert count matches BDS) |
| Dead bushes (deserts, badlands, swamps, mangrove swamps, giant taigas) | Close |
| Melons in jungles | Close |
| Vines in jungles | Close (about 85% of BDS's count; follows the jungle tree difference) |
| Huge mushrooms in mushroom fields, dark forests and swamps | Close (mushroom fields and dark forests match BDS block for block) |
| Firefly bushes | Close |
| Lilacs, rose bushes and peonies in forests and flower forests | Close |
| Red shrubs in the dappled forest | Close |
| Azalea trees with roots and hanging roots above lush caves | Close |
| Mangrove swamp grass patches | Close |

## Oceans and water

| What | Status |
| --- | --- |
| Seagrass and kelp | Close |
| Coral reefs, coral fans and sea pickles | Close |
| Coral crust (dead coral crusts and outcroppings with live coral on them) | Close (the dead coral colour is random in BDS, see below) |
| Sea anemones (live coral and fans on shallow warm ocean floors) | Close |
| Magma blocks under water, with bubble columns above them | Close |
| Water and lava springs in cliffs | Close (match BDS where the terrain matches) |

BDS picks the colour of each coral crust template's dead coral from a random seeded with the time
since the server started, so two BDS runs of the same seed disagree. worldgen picks it from the
world seed and the template instead, so the same seed always gives the same colours.

## Other features

| What | Status |
| --- | --- |
| Forest rocks (mossy cobblestone boulders in giant taigas) | Close (counts match BDS) |
| Ice spikes and their ice patches | Close (counts within 2% of BDS) |
| Fossils (desert and swamp; deepslate fossils with diamond ore) | Close (not yet checked in game) |
| Desert wells (with suspicious sand) | Close (checked in game) |

## Structures

| What | Status |
| --- | --- |
| Villages (all biome types and farm kinds, with loot chests; ground raised and cut under the houses as BDS does) | Close (placement matches BDS; the assembly follows BDS and the first pieces of a compared village match, but it drifts at one decoration, where BDS places a lamp and worldgen hay) |
| Mineshafts (normal and badlands; cobwebs, rails, torches, cave spider spawners; fences joined to their walls) | Close |
| Ancient cities, with the cave around them and their sculk | Close (the jigsaw rotation order and the block rules' random now follow BDS, as checked on bastions) |
| Mineshaft minecarts with chests | Not yet |
| Ocean ruins (warm and cold, large ones with clusters of small ones, chests) | Close (not yet compared with BDS; no suspicious sand or gravel yet) |
| Shipwrecks (ocean and beached, random wood pairs, supply, map and treasure chests) | Close (not yet compared with BDS) |
| Buried treasure (chest under beaches) | Close (not yet compared with BDS) |
| Ocean monuments (room graph, all room types, wings, penthouse, sponge rooms, gold core; no guardians) | Close (not yet compared with BDS) |
| Desert pyramids (with the TNT trap and the suspicious sand cellar) | Close |
| Jungle temples (traps, dispensers, lever puzzle) | Exact (tree leaves can poke inside; fixed in refinement) |
| Witch huts (cauldron with a random potion) | Close |
| Igloos (half with a basement: trapdoor, ladder, stone brick cells) | Exact (compared with BDS) |
| Pillager outposts (watchtower, overgrown overlay, feature pieces, chest; no pillagers) | Close (one of two compared outposts exact; the other 1 block high) |
| Ruined portals (buried and on the surface, netherrack spread, aging) | Close: the same code matches BDS block for block in the Nether; overworld portals not yet compared |
| Woodland mansions (all floors and rooms, roofs, foundation, entrance steps, joined windows) | Close (checked in game) |
| Strongholds (all rooms, library, portal room with silverfish spawner, joined fences and bars) | Close (checked in game) |
| Trail ruins (buried under a mound, with suspicious gravel) | Close (placement matches BDS) |
| Abandoned camps (all 18 biome kinds, tents, campsites and their trees; no tree grows into a camp) | Close (two camps match BDS; camp tree heights do not yet) |
| Trial chambers (all chamber kinds, copper bulb aging, buried shell; spawners and vaults have no mob or loot data yet) | Close (placement matches BDS; the first 26 pieces of a compared chamber match, copper bulbs and pots match; BDS then makes 3 random draws worldgen does not, at or before a failed entrance cap, so the rest of the layout differs) |

Structure chests, dispensers and suspicious sand carry their loot table, but the loot is not filled in yet, so chests are empty in
game.

## Other dimensions

| What | Status |
| --- | --- |
| Nether terrain, biomes, bedrock and surfaces | Close: biomes match BDS; BDS caps about a fifth of the roof undersides with nylium, from a height map its Nether generation never fills, and worldgen caps none (see `nether/surface.go`) |
| Nether lava springs, fire, glowstone, mushrooms | Matches BDS |
| Nether ores: gold, quartz, magma, gravel, blackstone, ancient debris | Matches BDS (debris 85%: one unexplained random draw, see the code) |
| Huge crimson and warped fungi, fungi, roots, sprouts, weeping and twisting vines | Close: 94% of crimson stems sit where BDS has them, caps 77%; where two trees overlap, BDS lets the first tree's blocks stop the second, and worldgen keeps each chunk's features apart (fixed in refinement) |
| Basalt deltas (deltas, basalt columns) | Matches BDS (differences only in chunks whose neighbours BDS never generated) |
| Soul sand valley (basalt pillars, underground soul sand, Nether fossils with dried ghasts) | Matches BDS (both fossils in fully generated BDS chunks match block for block, dried ghast included) |
| Nether cave carver | Matches BDS (lava that BDS let flow into the caves while ticking is not generation) |
| Nether fortresses (all bridge and castle pieces, blaze spawners, chests, nether wart) | Matches BDS: every block of a fortress and the 110 chunks around it, fence connections included (fences join after decoration). Chests get their loot table; the test server has no fortress loot yet |
| Nether ruined portals (blackstone aging, chains, netherrack and magma spread) | Matches BDS: two portals in fully generated BDS chunks match block for block except stair corner shapes, which BDS sets in a later pass (refinement). Where ores or a fortress reached the box before BDS built the portal, BDS's portal differs a little; here the portal always goes on the bare terrain first |
| Bastions (all four kinds: bridge, treasure, hoglin stables, housing units; cracked bricks, gilded blackstone, chests) | Matches BDS: the four BDS bastions compared match block for block (99.6 to 99.9% of their chunks; the rest is lava BDS let flow while ticking and Nether decoration). Blackstone wall connections differ in a few spots (refinement). Piglins and hoglins are not placed: worldgen places no mobs |
| End terrain (main island, outer islands, the void) | Matches BDS: every block of 486 BDS chunks (the main island and five outer islands) |
| End pillars (obsidian spikes, iron-bar cages, bedrock and End crystals on top) | Matches BDS block for block, iron bar joins included. The crystals are returned as spawn records (Chunk.Entities) for the engine to spawn, at BDS's positions; BDS turns each by a random yaw, here 0. The fire under a crystal is the crystal's own doing once it ticks |
| Exit portal (bedrock bowl, portal, pillar and torches) | From BDS's EndPodiumFeature. BDS builds it in the dragon fight, not in generation, so worldgen gives it as `End.ExitPortal(open)` for the engine's dragon fight (the test server places it closed on first load; `/endportal` opens it). The dragon and the egg are game logic |
| Small floating islands, chorus plants, end gateways | Matches BDS in the areas compared (99.997% to 100%). A few chorus plants differ where one chunk's chorus grows next to a neighbour's: BDS lets the first one block the second, and worldgen keeps each chunk's features apart (fixed in refinement). Gateways get their block entity with BDS's default exit |
| End cities (towers, fat towers, bridges, houses, the ship with its elytra, banners, brewing stand and dragon head, loot chests) | Matches BDS: four BDS cities (all four turns) match block for block, chest loot seeds and block entities included, except stair corner shapes, which BDS sets in a later pass (refinement), and a few chorus plants at their edges. Shulkers are not placed: worldgen places no mobs. The test server has no End city loot yet |
