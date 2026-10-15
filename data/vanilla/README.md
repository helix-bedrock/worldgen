# Vanilla world generation data

Mojang's vanilla data files shipped inside Bedrock Dedicated Server 1.26.52.3:
`biomes`, `features`, `feature_rules`, `worldgen` (jigsaw structures, pools,
processors) and `structures`.

They are the behaviour packs from `behavior_packs/vanilla*` plus the server's
base `definitions/` layer, stacked in version order: a later pack replaces a
file with the same identifier. `_provenance.tsv` lists which pack each file
came from.

These are data files, not code. The generator reads them instead of
hard-coding features, rules and structures.
