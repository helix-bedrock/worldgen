# AGENTS.md

Rules for AI coding agents working in this repo. Also read the org-wide rules in
`../.github/AGENT_RULES.md` (online:
<https://github.com/helix-bedrock/.github/blob/main/AGENT_RULES.md>). Where they
disagree, this file wins for this repo.

## What this repo is

Bedrock world generation that must match BDS block for block. Standard
library only. It never imports the engine.

## Invariants: do not break these

1. Output matches BDS (currently 1.26.52). Check changes with bdsdiff against
   a BDS world of the same seed. Do not guess vanilla behaviour.
2. Optimize for the lowest memory and the highest speed, but an optimization
   that changes the generated world so it no longer matches BDS is not worth
   it. Before and after: `WORLDGEN_FINGERPRINT=<file> go test -run Fingerprint ./overworld`
   must give the same hash.
3. Never paste decompiled Mojang code; use it only to understand behaviour.
4. No package-level mutable state, no panics.
