package structure

import "strconv"

// box is an inclusive block bounding box.
type Box struct{ X0, Y0, Z0, X1, Y1, Z1 int32 }

func (b Box) Contains(p [3]int32) bool {
	return p[0] >= b.X0 && p[0] <= b.X1 && p[1] >= b.Y0 && p[1] <= b.Y1 && p[2] >= b.Z0 && p[2] <= b.Z1
}

func (b Box) Intersects(c Box) bool {
	return b.X0 <= c.X1 && b.X1 >= c.X0 && b.Y0 <= c.Y1 && b.Y1 >= c.Y0 && b.Z0 <= c.Z1 && b.Z1 >= c.Z0
}

func (b Box) Inside(c Box) bool {
	return b.X0 >= c.X0 && b.X1 <= c.X1 && b.Y0 >= c.Y0 && b.Y1 <= c.Y1 && b.Z0 >= c.Z0 && b.Z1 <= c.Z1
}

// raise moves the box up by dy.
func (b Box) Raise(dy int32) Box {
	return Box{b.X0, b.Y0 + dy, b.Z0, b.X1, b.Y1 + dy, b.Z1}
}

// rotatePos rotates a template position by r quarter turns clockwise
// around the template origin (Java's Rotation.CLOCKWISE_90: x,z -> -z,x).
func RotatePos(p [3]int32, r int) [3]int32 {
	x, z := p[0], p[2]
	switch r & 3 {
	case 1:
		x, z = -z, x
	case 2:
		x, z = -x, -z
	case 3:
		x, z = z, -x
	}
	return [3]int32{x, p[1], z}
}

// Template mirrors, numbered as BDS's placement settings: mirrorZ flips z
// (Java's LEFT_RIGHT), mirrorX flips x (Java's FRONT_BACK). A template is
// mirrored about its origin before it is turned.
const (
	MirrorNone uint8 = iota
	MirrorZ
	MirrorX
)

// mirrorPos mirrors a template position about the origin.
func MirrorPos(p [3]int32, m uint8) [3]int32 {
	switch m {
	case MirrorZ:
		p[2] = -p[2]
	case MirrorX:
		p[0] = -p[0]
	}
	return p
}

var Horizontal = []string{"north", "east", "south", "west"}

func RotateFacing(f string, r int) string {
	for i, h := range Horizontal {
		if h == f {
			return Horizontal[(i+r)&3]
		}
	}
	return f
}

func Opposite(f string) string {
	switch f {
	case "up":
		return "down"
	case "down":
		return "up"
	}
	return RotateFacing(f, 2)
}

func JigsawDir(f string) [3]int32 {
	switch f {
	case "north":
		return [3]int32{0, 0, -1}
	case "south":
		return [3]int32{0, 0, 1}
	case "west":
		return [3]int32{-1, 0, 0}
	case "east":
		return [3]int32{1, 0, 0}
	case "up":
		return [3]int32{0, 1, 0}
	case "down":
		return [3]int32{0, -1, 0}
	}
	return [3]int32{}
}

// tplBox is the box of a template placed at pos with rotation r.
func TemplateBox(t *Template, pos [3]int32, r int) Box {
	// BDS's piece box (sub_D2379D0): the size turned, then the box laid out
	// from pos by the turn. A size of 0 (a feature element) gives an empty
	// box, one block past pos, that holds nothing.
	sx, sy, sz := t.Size[0], t.Size[1], t.Size[2]
	if r&1 == 1 {
		sx, sz = sz, sx
	}
	bb := Box{pos[0], pos[1], pos[2], pos[0] + sx - 1, pos[1] + sy - 1, pos[2] + sz - 1}
	switch r & 3 {
	case 1:
		bb.X0, bb.X1 = pos[0]+1-sx, pos[0]
	case 2:
		bb.X0, bb.X1 = pos[0]+1-sx, pos[0]
		bb.Z0, bb.Z1 = pos[2]+1-sz, pos[2]
	case 3:
		bb.Z0, bb.Z1 = pos[2]+1-sz, pos[2]
	}
	return bb
}

// mirrorPropsZ mirrors a Java block state along z (Java's LEFT_RIGHT).
func mirrorPropsZ(p map[string]string) map[string]string {
	return mirrorProps(p, "north", "south", 8)
}

// mirrorPropsX mirrors a Java block state along x (Java's FRONT_BACK).
func mirrorPropsX(p map[string]string) map[string]string {
	return mirrorProps(p, "east", "west", 0)
}

// mirrorProps swaps the facings and sides a and b, flips door hinges and
// mirrors a 16-step rotation (0 faces south) as Java's Mirror.mirror does:
// (half - r) for LEFT_RIGHT, (16 - r) for FRONT_BACK.
func mirrorProps(p map[string]string, a, b string, half int) map[string]string {
	if len(p) == 0 {
		return p
	}
	out := make(map[string]string, len(p))
	for k, v := range p {
		out[k] = v
	}
	switch p["facing"] {
	case a:
		out["facing"] = b
	case b:
		out["facing"] = a
	}
	if v, ok := p[a]; ok {
		out[a], out[b] = p[b], v
	}
	switch p["hinge"] {
	case "left":
		out["hinge"] = "right"
	case "right":
		out["hinge"] = "left"
	}
	if v, ok := p["rotation"]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			out["rotation"] = strconv.Itoa((half - n + 16) & 15)
		}
	}
	return out
}

// NewBox returns the box with corners (x0, y0, z0) and (x1, y1, z1).
func NewBox(x0, y0, z0, x1, y1, z1 int32) Box { return Box{x0, y0, z0, x1, y1, z1} }
