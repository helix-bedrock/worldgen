package rand

// Java is the 48-bit linear congruential generator of java.util.Random,
// which Bedrock also uses where it was ported from Java Edition, such as the
// block rules of structure templates. The zero value acts as seeded with 0.
type Java struct {
	seed uint64
}

const (
	javaMul  = 0x5DEECE66D
	javaMask = 1<<48 - 1
)

// Seed sets the seed as java.util.Random.setSeed does.
func (j *Java) Seed(seed int64) {
	j.seed = (uint64(seed) ^ javaMul) & javaMask
}

func (j *Java) next(bits uint) int32 {
	j.seed = (j.seed*javaMul + 0xB) & javaMask
	return int32(j.seed >> (48 - bits))
}

// Float returns a float in [0, 1) with 24 random bits (nextFloat).
func (j *Java) Float() float32 {
	return float32(j.next(24)) / (1 << 24)
}

// BlockSeed is Java's Mth.getSeed for a block position, the seed of the
// random a structure block rule draws from at that block.
func BlockSeed(x, y, z int32) int64 {
	// x*3129871 wraps as a 32-bit product, as in Java and BDS.
	l := int64(x*3129871) ^ int64(z)*116129781 ^ int64(y)
	return (l*l*42317861 + l*11) >> 16
}
