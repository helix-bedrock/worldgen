package overworld

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// molang is a parsed Molang expression, as far as vanilla feature data
// uses it: numbers, + - * /, comparisons, && || !, ?:, parentheses,
// query.has_biome_tag('...'), query.any_tag('...') (block tag lists) and
// variable.is_legacy. Anything else fails to parse, and the feature that
// uses it is not loaded.
type molang interface {
	eval(env *molangEnv) float32
}

// molangEnv is what an expression can ask about.
type molangEnv struct {
	biomeTags        []string
	originX, originZ float32
	// noise is query.noise(x, z).
	noise func(x, z float32) float32
	// random is math.random's source: BDS's rule context draws nextFloat
	// from the chunk's population random (the lambda sub_C839AE0 set up in
	// sub_C81D6D0), which scatter features share through their copied
	// context.
	random func() float32
}

type molangVar uint8

const (
	varOriginX molangVar = iota
	varOriginZ
)

func (v molangVar) eval(env *molangEnv) float32 {
	if v == varOriginX {
		return env.originX
	}
	return env.originZ
}

type molangCall struct {
	fn   string
	args []molang
}

func (c molangCall) eval(env *molangEnv) float32 {
	switch c.fn {
	case "math.ceil":
		return float32(math.Ceil(float64(c.args[0].eval(env))))
	case "math.floor":
		return float32(math.Floor(float64(c.args[0].eval(env))))
	case "math.random":
		lo, hi := c.args[0].eval(env), c.args[1].eval(env)
		var f float32
		if env.random != nil {
			f = env.random()
		}
		// VERIFY: assumed low + (high - low) * f; the op 33 handler was not
		// read.
		return lo + (hi-lo)*f
	case "query.noise":
		if env.noise == nil {
			return 0
		}
		return env.noise(c.args[0].eval(env), c.args[1].eval(env))
	}
	return 0
}

type molangNum float32
type molangTag string
type molangUnary struct {
	op string
	x  molang
}
type molangBin struct {
	op   string
	l, r molang
}
type molangCond struct{ c, a, b molang }

func (n molangNum) eval(*molangEnv) float32 { return float32(n) }

func (t molangTag) eval(env *molangEnv) float32 {
	for _, s := range env.biomeTags {
		if s == string(t) {
			return 1
		}
	}
	return 0
}

func (u molangUnary) eval(env *molangEnv) float32 {
	v := u.x.eval(env)
	if u.op == "!" {
		return b2f(v == 0)
	}
	return -v
}

func b2f(b bool) float32 {
	if b {
		return 1
	}
	return 0
}

func (b molangBin) eval(env *molangEnv) float32 {
	l := b.l.eval(env)
	switch b.op {
	case "&&":
		return b2f(l != 0 && b.r.eval(env) != 0)
	case "||":
		return b2f(l != 0 || b.r.eval(env) != 0)
	}
	r := b.r.eval(env)
	switch b.op {
	case "+":
		return l + r
	case "-":
		return l - r
	case "*":
		return l * r
	case "/":
		if r == 0 {
			return 0
		}
		return l / r
	case "==":
		return b2f(l == r)
	case "!=":
		return b2f(l != r)
	case "<":
		return b2f(l < r)
	case "<=":
		return b2f(l <= r)
	case ">":
		return b2f(l > r)
	case ">=":
		return b2f(l >= r)
	}
	return 0
}

func (c molangCond) eval(env *molangEnv) float32 {
	if c.c.eval(env) != 0 {
		return c.a.eval(env)
	}
	return c.b.eval(env)
}

// parseMolang parses an expression. variable.is_legacy reads as 0, the value
// BDS gives it in new worlds.
func parseMolang(src string) (molang, error) {
	p := &molangParser{s: strings.ToLower(src)}
	e, err := p.ternary()
	if err != nil {
		return nil, err
	}
	p.space()
	if p.i != len(p.s) {
		return nil, fmt.Errorf("molang %q: unexpected %q", src, p.s[p.i:])
	}
	return e, nil
}

type molangParser struct {
	s string
	i int
}

func (p *molangParser) space() {
	for p.i < len(p.s) && (p.s[p.i] == ' ' || p.s[p.i] == '\t' || p.s[p.i] == '\n') {
		p.i++
	}
}

func (p *molangParser) eat(tok string) bool {
	p.space()
	if strings.HasPrefix(p.s[p.i:], tok) {
		p.i += len(tok)
		return true
	}
	return false
}

func (p *molangParser) ternary() (molang, error) {
	c, err := p.binary(0)
	if err != nil {
		return nil, err
	}
	if !p.eat("?") {
		return c, nil
	}
	a, err := p.ternary()
	if err != nil {
		return nil, err
	}
	if !p.eat(":") {
		return nil, fmt.Errorf("molang: missing ':'")
	}
	b, err := p.ternary()
	if err != nil {
		return nil, err
	}
	return molangCond{c, a, b}, nil
}

var molangLevels = [][]string{{"||"}, {"&&"}, {"==", "!="}, {"<=", ">=", "<", ">"}, {"+", "-"}, {"*", "/"}}

func (p *molangParser) binary(level int) (molang, error) {
	if level == len(molangLevels) {
		return p.unary()
	}
	l, err := p.binary(level + 1)
	if err != nil {
		return nil, err
	}
	for {
		op := ""
		for _, o := range molangLevels[level] {
			if p.eat(o) {
				op = o
				break
			}
		}
		if op == "" {
			return l, nil
		}
		r, err := p.binary(level + 1)
		if err != nil {
			return nil, err
		}
		l = molangBin{op, l, r}
	}
}

func (p *molangParser) unary() (molang, error) {
	if p.eat("!") {
		x, err := p.unary()
		return molangUnary{"!", x}, err
	}
	if p.eat("-") {
		x, err := p.unary()
		return molangUnary{"-", x}, err
	}
	if p.eat("(") {
		e, err := p.ternary()
		if err != nil {
			return nil, err
		}
		if !p.eat(")") {
			return nil, fmt.Errorf("molang: missing ')'")
		}
		return e, nil
	}
	if p.eat("query.has_biome_tag(") {
		p.space()
		if p.i >= len(p.s) || (p.s[p.i] != '\'' && p.s[p.i] != '"') {
			return nil, fmt.Errorf("molang: has_biome_tag wants a string")
		}
		q := p.s[p.i]
		end := strings.IndexByte(p.s[p.i+1:], q)
		if end < 0 {
			return nil, fmt.Errorf("molang: unterminated string")
		}
		tag := p.s[p.i+1 : p.i+1+end]
		p.i += end + 2
		if !p.eat(")") {
			return nil, fmt.Errorf("molang: missing ')'")
		}
		return molangTag(tag), nil
	}
	if p.eat("variable.is_legacy") || p.eat("v.is_legacy") {
		return molangNum(0), nil
	}
	if p.eat("variable.originx") {
		return varOriginX, nil
	}
	if p.eat("variable.originz") {
		return varOriginZ, nil
	}
	for fn, n := range map[string]int{"math.ceil": 1, "math.floor": 1, "math.random": 2, "query.noise": 2} {
		if !p.eat(fn + "(") {
			continue
		}
		var args []molang
		for i := range n {
			if i > 0 && !p.eat(",") {
				return nil, fmt.Errorf("molang: %s wants %d arguments", fn, n)
			}
			a, err := p.ternary()
			if err != nil {
				return nil, err
			}
			args = append(args, a)
		}
		if !p.eat(")") {
			return nil, fmt.Errorf("molang: missing ')' after %s", fn)
		}
		return molangCall{fn, args}, nil
	}
	p.space()
	j := p.i
	for j < len(p.s) && (p.s[j] >= '0' && p.s[j] <= '9' || p.s[j] == '.') {
		j++
	}
	if j == p.i {
		return nil, fmt.Errorf("molang: cannot read %q", p.s[p.i:])
	}
	f, err := strconv.ParseFloat(p.s[p.i:j], 32)
	if err != nil {
		return nil, fmt.Errorf("molang: %w", err)
	}
	p.i = j
	return molangNum(f), nil
}
