// Package biome picks overworld biomes from climate: a list of parameter
// boxes, one per biome placement, and a nearest-box search. The list layout
// and values were read from BDS 1.26.52; it follows the vanilla 1.18+ design
// with Bedrock's own tables (dappled forest, pale garden, sulfur caves).
package biome

// Param is a closed interval of a quantized climate value (x10000).
type Param struct{ Min, Max int64 }

// q quantizes a climate value the way BDS does: x10000 in double precision,
// truncated toward zero (so -0.45 becomes -4499).
func q(f float32) int64 { return int64(float64(f) * 10000) }

func span(a, b float32) Param { return Param{q(a), q(b)} }
func point(a float32) Param   { return Param{q(a), q(a)} }
func join(a, b Param) Param   { return Param{a.Min, b.Max} }

// Entry is one parameter box and the biome it gives.
type Entry struct {
	// Temperature, humidity, continentalness, erosion, depth, weirdness, offset.
	Space [7]Param
	Biome string
}

var (
	full         = span(-1, 1)
	temperatures = [5]Param{span(-1, -0.45), span(-0.45, -0.15), span(-0.15, 0.2), span(0.2, 0.55), span(0.55, 1)}
	humidities   = [5]Param{span(-1, -0.35), span(-0.35, -0.1), span(-0.1, 0.1), span(0.1, 0.3), span(0.3, 1)}
	erosions     = [7]Param{span(-1, -0.78), span(-0.78, -0.375), span(-0.375, -0.2225), span(-0.2225, 0.05),
		span(0.05, 0.45), span(0.45, 0.55), span(0.55, 1)}
	frozen   = temperatures[0]
	unfrozen = join(temperatures[1], temperatures[4])

	mushroomCont  = span(-1.2, -1.05)
	deepOceanCont = span(-1.05, -0.455)
	oceanCont     = span(-0.455, -0.19)
	coastCont     = span(-0.19, -0.11)
	inlandCont    = span(-0.11, 0.55)
	nearCont      = span(-0.11, 0.03)
	midCont       = span(0.03, 0.3)
	farCont       = span(0.3, 1)
)

// Biome tables: rows are temperature index (frozen..hot), columns humidity
// index (dry..wet). Bedrock names; "" means no variant.
var (
	oceans = [2][5]string{
		{"deep_frozen_ocean", "deep_cold_ocean", "deep_ocean", "deep_lukewarm_ocean", "warm_ocean"},
		{"frozen_ocean", "cold_ocean", "ocean", "lukewarm_ocean", "warm_ocean"},
	}
	middle = [5][5]string{
		{"ice_plains", "ice_plains", "ice_plains", "cold_taiga", "taiga"},
		{"plains", "plains", "forest", "taiga", "redwood_taiga_mutated"},
		{"flower_forest", "plains", "forest", "birch_forest", "roofed_forest"},
		{"savanna", "savanna", "forest", "jungle", "jungle"},
		{"desert", "desert", "desert", "desert", "desert"},
	}
	middleVariant = [5][5]string{
		{"ice_plains_spikes", "", "cold_taiga", "", ""},
		{"dappled_forest", "", "", "", "mega_taiga"},
		{"sunflower_plains", "", "", "birch_forest_mutated", ""},
		{"", "", "plains", "jungle_edge", "bamboo_jungle"},
		{"", "", "", "", ""},
	}
	plateau = [5][5]string{
		{"ice_plains", "ice_plains", "ice_plains", "cold_taiga", "cold_taiga"},
		{"meadow", "meadow", "forest", "taiga", "redwood_taiga_mutated"},
		{"meadow", "meadow", "meadow", "meadow", "pale_garden"},
		{"savanna_plateau", "savanna_plateau", "forest", "forest", "jungle"},
		{"mesa", "mesa", "mesa", "mesa_plateau_stone", "mesa_plateau_stone"},
	}
	plateauVariant = [5][5]string{
		{"ice_plains_spikes", "", "", "", ""},
		{"cherry_grove", "", "meadow", "meadow", "mega_taiga"},
		{"cherry_grove", "cherry_grove", "forest", "birch_forest", ""},
		{"", "", "", "", ""},
		{"mesa_bryce", "mesa_bryce", "", "", ""},
	}
	shattered = [5][5]string{
		{"extreme_hills_mutated", "extreme_hills_mutated", "extreme_hills", "extreme_hills_plus_trees", "extreme_hills_plus_trees"},
		{"extreme_hills_mutated", "extreme_hills_mutated", "extreme_hills", "extreme_hills_plus_trees", "extreme_hills_plus_trees"},
		{"extreme_hills", "extreme_hills", "extreme_hills", "extreme_hills_plus_trees", "extreme_hills_plus_trees"},
		{"", "", "", "", ""},
		{"", "", "", "", ""},
	}
)

// Entries builds the full overworld parameter list.
func Entries() []Entry {
	b := &builder{}
	b.offCoast()
	b.inland()
	b.underground()
	return b.out
}

type builder struct{ out []Entry }

func (b *builder) add(t, h, c, e, d, w Param, biome string) {
	b.out = append(b.out, Entry{Space: [7]Param{t, h, c, e, d, w, {}}, Biome: biome})
}

// surface adds a biome at depth 0 and depth 1.
func (b *builder) surface(t, h, c, e, w Param, biome string) {
	b.add(t, h, c, e, point(0), w, biome)
	b.add(t, h, c, e, point(1), w, biome)
}

func (b *builder) offCoast() {
	b.surface(full, full, mushroomCont, full, full, "mushroom_island")
	for i, t := range temperatures {
		b.surface(t, full, deepOceanCont, full, full, oceans[0][i])
		b.surface(t, full, oceanCont, full, full, oceans[1][i])
	}
}

func (b *builder) inland() {
	b.midSlice(span(-1, -0.93333334))
	b.highSlice(span(-0.93333334, -0.7666667))
	b.peaks(span(-0.7666667, -0.56666666))
	b.highSlice(span(-0.56666666, -0.4))
	b.midSlice(span(-0.4, -0.26666668))
	b.lowSlice(span(-0.26666668, -0.05))
	b.valleys(span(-0.05, 0.05))
	b.lowSlice(span(0.05, 0.26666668))
	b.midSlice(span(0.26666668, 0.4))
	b.highSlice(span(0.4, 0.56666666))
	b.peaks(span(0.56666666, 0.7666667))
	b.highSlice(span(0.7666667, 0.93333334))
	b.midSlice(span(0.93333334, 1))
}

func (b *builder) peaks(w Param) {
	for i, t := range temperatures {
		for j, h := range humidities {
			mid := pickMiddle(i, j, w)
			midHot := pickMiddleOrBadlands(i, j, w)
			midHotCold := pickMiddleOrBadlandsOrSlope(i, j, w)
			plat := pickPlateau(i, j, w)
			shat := pickShattered(i, j, w)
			savanna := maybeWindsweptSavanna(i, j, w, shat)
			peak := pickPeak(i, j, w)
			b.surface(t, h, join(coastCont, farCont), erosions[0], w, peak)
			b.surface(t, h, join(coastCont, nearCont), erosions[1], w, midHotCold)
			b.surface(t, h, join(midCont, farCont), erosions[1], w, peak)
			b.surface(t, h, join(coastCont, nearCont), join(erosions[2], erosions[3]), w, mid)
			b.surface(t, h, join(midCont, farCont), erosions[2], w, plat)
			b.surface(t, h, midCont, erosions[3], w, midHot)
			b.surface(t, h, farCont, erosions[3], w, plat)
			b.surface(t, h, join(coastCont, farCont), erosions[4], w, mid)
			b.surface(t, h, join(coastCont, nearCont), erosions[5], w, savanna)
			b.surface(t, h, join(midCont, farCont), erosions[5], w, shat)
			b.surface(t, h, join(coastCont, farCont), erosions[6], w, mid)
		}
	}
}

func (b *builder) highSlice(w Param) {
	for i, t := range temperatures {
		for j, h := range humidities {
			mid := pickMiddle(i, j, w)
			midHot := pickMiddleOrBadlands(i, j, w)
			midHotCold := pickMiddleOrBadlandsOrSlope(i, j, w)
			plat := pickPlateau(i, j, w)
			shat := pickShattered(i, j, w)
			savanna := maybeWindsweptSavanna(i, j, w, mid)
			slope := pickSlope(i, j, w)
			peak := pickPeak(i, j, w)
			b.surface(t, h, coastCont, join(erosions[0], erosions[1]), w, mid)
			b.surface(t, h, nearCont, erosions[0], w, slope)
			b.surface(t, h, join(midCont, farCont), erosions[0], w, peak)
			b.surface(t, h, nearCont, erosions[1], w, midHotCold)
			b.surface(t, h, join(midCont, farCont), erosions[1], w, slope)
			b.surface(t, h, join(coastCont, nearCont), join(erosions[2], erosions[3]), w, mid)
			b.surface(t, h, join(midCont, farCont), erosions[2], w, plat)
			b.surface(t, h, midCont, erosions[3], w, midHot)
			b.surface(t, h, farCont, erosions[3], w, plat)
			b.surface(t, h, join(coastCont, farCont), erosions[4], w, mid)
			b.surface(t, h, join(coastCont, nearCont), erosions[5], w, savanna)
			b.surface(t, h, join(midCont, farCont), erosions[5], w, shat)
			b.surface(t, h, join(coastCont, farCont), erosions[6], w, mid)
		}
	}
}

func (b *builder) midSlice(w Param) {
	b.surface(full, full, coastCont, join(erosions[0], erosions[2]), w, "stone_beach")
	b.surface(join(temperatures[1], temperatures[2]), full, join(nearCont, farCont), erosions[6], w, "swampland")
	b.surface(join(temperatures[3], temperatures[4]), full, join(nearCont, farCont), erosions[6], w, "mangrove_swamp")
	for i, t := range temperatures {
		for j, h := range humidities {
			mid := pickMiddle(i, j, w)
			midHot := pickMiddleOrBadlands(i, j, w)
			midHotCold := pickMiddleOrBadlandsOrSlope(i, j, w)
			shat := pickShattered(i, j, w)
			plat := pickPlateau(i, j, w)
			beach := pickBeach(i)
			savanna := maybeWindsweptSavanna(i, j, w, mid)
			shatCoast := pickShatteredCoast(i, j, w)
			slope := pickSlope(i, j, w)
			b.surface(t, h, join(nearCont, farCont), erosions[0], w, slope)
			b.surface(t, h, join(nearCont, midCont), erosions[1], w, midHotCold)
			if i == 0 {
				b.surface(t, h, farCont, erosions[1], w, slope)
			} else {
				b.surface(t, h, farCont, erosions[1], w, plat)
			}
			b.surface(t, h, nearCont, erosions[2], w, mid)
			b.surface(t, h, midCont, erosions[2], w, midHot)
			b.surface(t, h, farCont, erosions[2], w, plat)
			b.surface(t, h, join(coastCont, nearCont), erosions[3], w, mid)
			b.surface(t, h, join(midCont, farCont), erosions[3], w, midHot)
			if w.Max < 0 {
				b.surface(t, h, coastCont, erosions[4], w, beach)
				b.surface(t, h, join(nearCont, farCont), erosions[4], w, mid)
			} else {
				b.surface(t, h, join(coastCont, farCont), erosions[4], w, mid)
			}
			b.surface(t, h, coastCont, erosions[5], w, shatCoast)
			b.surface(t, h, nearCont, erosions[5], w, savanna)
			b.surface(t, h, join(midCont, farCont), erosions[5], w, shat)
			if w.Max < 0 {
				b.surface(t, h, coastCont, erosions[6], w, beach)
			} else {
				b.surface(t, h, coastCont, erosions[6], w, mid)
			}
			if i == 0 {
				b.surface(t, h, join(nearCont, farCont), erosions[6], w, mid)
			}
		}
	}
}

func (b *builder) lowSlice(w Param) {
	b.surface(full, full, coastCont, join(erosions[0], erosions[2]), w, "stone_beach")
	b.surface(join(temperatures[1], temperatures[2]), full, join(nearCont, farCont), erosions[6], w, "swampland")
	b.surface(join(temperatures[3], temperatures[4]), full, join(nearCont, farCont), erosions[6], w, "mangrove_swamp")
	for i, t := range temperatures {
		for j, h := range humidities {
			mid := pickMiddle(i, j, w)
			midHot := pickMiddleOrBadlands(i, j, w)
			midHotCold := pickMiddleOrBadlandsOrSlope(i, j, w)
			beach := pickBeach(i)
			savanna := maybeWindsweptSavanna(i, j, w, mid)
			shatCoast := pickShatteredCoast(i, j, w)
			b.surface(t, h, nearCont, join(erosions[0], erosions[1]), w, midHot)
			b.surface(t, h, join(midCont, farCont), join(erosions[0], erosions[1]), w, midHotCold)
			b.surface(t, h, nearCont, join(erosions[2], erosions[3]), w, mid)
			b.surface(t, h, join(midCont, farCont), join(erosions[2], erosions[3]), w, midHot)
			b.surface(t, h, coastCont, join(erosions[3], erosions[4]), w, beach)
			b.surface(t, h, join(nearCont, farCont), erosions[4], w, mid)
			b.surface(t, h, coastCont, erosions[5], w, shatCoast)
			b.surface(t, h, nearCont, erosions[5], w, savanna)
			b.surface(t, h, join(midCont, farCont), erosions[5], w, mid)
			b.surface(t, h, coastCont, erosions[6], w, beach)
			if i == 0 {
				b.surface(t, h, join(nearCont, farCont), erosions[6], w, mid)
			}
		}
	}
}

func (b *builder) valleys(w Param) {
	riverOrShore := func(river string) string {
		if w.Max < 0 {
			return "stone_beach"
		}
		return river
	}
	b.surface(frozen, full, coastCont, join(erosions[0], erosions[1]), w, riverOrShore("frozen_river"))
	b.surface(unfrozen, full, coastCont, join(erosions[0], erosions[1]), w, riverOrShore("river"))
	b.surface(frozen, full, nearCont, join(erosions[0], erosions[1]), w, "frozen_river")
	b.surface(unfrozen, full, nearCont, join(erosions[0], erosions[1]), w, "river")
	b.surface(frozen, full, join(coastCont, farCont), join(erosions[2], erosions[5]), w, "frozen_river")
	b.surface(unfrozen, full, join(coastCont, farCont), join(erosions[2], erosions[5]), w, "river")
	b.surface(frozen, full, coastCont, erosions[6], w, "frozen_river")
	b.surface(unfrozen, full, coastCont, erosions[6], w, "river")
	b.surface(join(temperatures[1], temperatures[2]), full, join(inlandCont, farCont), erosions[6], w, "swampland")
	b.surface(join(temperatures[3], temperatures[4]), full, join(inlandCont, farCont), erosions[6], w, "mangrove_swamp")
	b.surface(frozen, full, join(inlandCont, farCont), erosions[6], w, "frozen_river")
	for i, t := range temperatures {
		for j, h := range humidities {
			b.surface(t, h, join(midCont, farCont), join(erosions[0], erosions[1]), w, pickMiddleOrBadlands(i, j, w))
		}
	}
}

func (b *builder) underground() {
	cave := span(0.2, 0.9)
	b.add(full, full, span(0.8, 1), full, cave, full, "dripstone_caves")
	b.add(full, span(0.7, 1), full, full, cave, full, "lush_caves")
	// Bedrock only.
	b.add(full, full, join(coastCont, inlandCont), join(erosions[5], erosions[6]), cave, span(-1.1, -0.85), "sulfur_caves")
	b.add(full, full, full, join(erosions[0], erosions[1]), point(1.1), full, "deep_dark")
}

func pickMiddle(i, j int, w Param) string {
	if w.Max < 0 {
		return middle[i][j]
	}
	if v := middleVariant[i][j]; v != "" {
		return v
	}
	return middle[i][j]
}

func pickMiddleOrBadlands(i, j int, w Param) string {
	if i == 4 {
		return pickBadlands(j, w)
	}
	return pickMiddle(i, j, w)
}

func pickMiddleOrBadlandsOrSlope(i, j int, w Param) string {
	if i == 0 {
		return pickSlope(i, j, w)
	}
	return pickMiddleOrBadlands(i, j, w)
}

func maybeWindsweptSavanna(i, j int, w Param, fallback string) string {
	if i > 1 && j < 4 && w.Max >= 0 {
		return "savanna_mutated"
	}
	return fallback
}

func pickShatteredCoast(i, j int, w Param) string {
	b := pickBeach(i)
	if w.Max >= 0 {
		b = pickMiddle(i, j, w)
	}
	return maybeWindsweptSavanna(i, j, w, b)
}

func pickBeach(i int) string {
	switch i {
	case 0:
		return "cold_beach"
	case 4:
		return "desert"
	}
	return "beach"
}

func pickBadlands(j int, w Param) string {
	if j < 2 {
		if w.Max < 0 {
			return "mesa"
		}
		return "mesa_bryce"
	}
	if j < 3 {
		return "mesa"
	}
	return "mesa_plateau_stone"
}

func pickPlateau(i, j int, w Param) string {
	if w.Max >= 0 {
		if v := plateauVariant[i][j]; v != "" {
			return v
		}
	}
	return plateau[i][j]
}

func pickPeak(i, j int, w Param) string {
	if i <= 2 {
		if w.Max < 0 {
			return "jagged_peaks"
		}
		return "frozen_peaks"
	}
	if i == 3 {
		return "stony_peaks"
	}
	return pickBadlands(j, w)
}

func pickSlope(i, j int, w Param) string {
	if i >= 3 {
		return pickPlateau(i, j, w)
	}
	if j <= 1 {
		return "snowy_slopes"
	}
	return "grove"
}

func pickShattered(i, j int, w Param) string {
	if v := shattered[i][j]; v != "" {
		return v
	}
	return pickMiddle(i, j, w)
}
