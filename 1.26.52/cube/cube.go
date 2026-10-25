// Package cube holds the directions blocks and structures are placed with:
// the four horizontal directions and the six faces of a block, with their
// block-state names, their steps and BDS's numeric values for them.
package cube

// Direction is a horizontal direction, in clockwise order from north.
type Direction uint8

// The horizontal directions.
const (
	North Direction = iota
	East
	South
	West
)

// Directions returns the four horizontal directions, in clockwise order
// from north.
func Directions() [4]Direction { return [4]Direction{North, East, South, West} }

// LegacyDirections returns the horizontal directions in the order of BDS's
// legacy 2D direction values ("direction", "weirdo_direction" and the old
// structure pieces): south, west, north, east, so a value's index is its
// legacy value.
func LegacyDirections() [4]Direction { return [4]Direction{South, West, North, East} }

// String returns the block-state name of d, as "north".
func (d Direction) String() string {
	switch d {
	case North:
		return "north"
	case East:
		return "east"
	case South:
		return "south"
	case West:
		return "west"
	}
	return "unknown"
}

// Opposite returns the direction facing the other way.
func (d Direction) Opposite() Direction { return (d + 2) & 3 }

// RotateRight returns d turned a quarter clockwise.
func (d Direction) RotateRight() Direction { return (d + 1) & 3 }

// RotateLeft returns d turned a quarter counterclockwise.
func (d Direction) RotateLeft() Direction { return (d + 3) & 3 }

// Step returns the x and z offsets of one block toward d.
func (d Direction) Step() (dx, dz int32) {
	switch d {
	case North:
		return 0, -1
	case East:
		return 1, 0
	case South:
		return 0, 1
	case West:
		return -1, 0
	}
	return 0, 0
}

// Legacy returns BDS's legacy 2D value of d: south 0, west 1, north 2,
// east 3.
func (d Direction) Legacy() int32 { return int32(d+2) & 3 }

// FromLegacy returns the direction of BDS's legacy 2D value v (taken
// modulo 4).
func FromLegacy(v int32) Direction { return Direction(v+2) & 3 }

// Face returns the block face pointing toward d.
func (d Direction) Face() Face {
	switch d {
	case North:
		return FaceNorth
	case East:
		return FaceEast
	case South:
		return FaceSouth
	}
	return FaceWest
}

// Face is a face of a block, in BDS's facing_direction order.
type Face uint8

// The faces of a block.
const (
	FaceDown Face = iota
	FaceUp
	FaceNorth
	FaceSouth
	FaceWest
	FaceEast
)

// Faces returns the six faces of a block, in BDS's facing_direction order,
// so a face's index is its value.
func Faces() [6]Face { return [6]Face{FaceDown, FaceUp, FaceNorth, FaceSouth, FaceWest, FaceEast} }

// String returns the block-state name of f, as "down".
func (f Face) String() string {
	switch f {
	case FaceDown:
		return "down"
	case FaceUp:
		return "up"
	case FaceNorth:
		return "north"
	case FaceSouth:
		return "south"
	case FaceWest:
		return "west"
	case FaceEast:
		return "east"
	}
	return "unknown"
}

// Opposite returns the face on the other side of the block.
func (f Face) Opposite() Face { return f ^ 1 }

// Step returns the offsets of the block beyond f.
func (f Face) Step() (dx, dy, dz int32) {
	switch f {
	case FaceDown:
		return 0, -1, 0
	case FaceUp:
		return 0, 1, 0
	case FaceNorth:
		return 0, 0, -1
	case FaceSouth:
		return 0, 0, 1
	case FaceWest:
		return -1, 0, 0
	case FaceEast:
		return 1, 0, 0
	}
	return 0, 0, 0
}

// Direction returns the horizontal direction of f, and false for up and
// down.
func (f Face) Direction() (Direction, bool) {
	switch f {
	case FaceNorth:
		return North, true
	case FaceSouth:
		return South, true
	case FaceWest:
		return West, true
	case FaceEast:
		return East, true
	}
	return 0, false
}
