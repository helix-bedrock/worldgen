package overworld

import (
	"slices"
	"strconv"
	"strings"

	"github.com/helix-bedrock/worldgen/rand"
	"github.com/helix-bedrock/worldgen/structure"
)

// Woodland mansions, written from BDS 1.26.52: WoodlandMansionFeature
// (constructor sub_4FBBDB0, chunk test sub_4FBC500, start sub_4FBC760),
// the room grid (corridors sub_4FCDE80, edge cleanup sub_4FCE3A0, rooms
// sub_4FCE780), the piece placer (sub_4FBFFB0 with outer walls sub_4FC7D20,
// wall pieces sub_4FC82D0 and roofs sub_4FC85E0), the room collections
// (vtables EE9CBC8, EE9CC48, EE9CDB8), WoodlandMansionPiece (box sub_D27F200,
// markers sub_4FC6500) and WoodlandMansionStart's post pass (sub_4FBF5B0).
//
// The start chunk's random drives everything in order: the turn, the grid
// (corridors, room shuffles and doors, the third floor), then the placer
// (door picks and room templates). The layout follows Java's
// WoodlandMansionPieces; Bedrock differs in:
//   - corridor directions are drawn as north, south, west, east (Java:
//     south, west, north, east);
//   - the room shuffle swaps from the end with nextInt(n-1) (Sattolo);
//   - corridor floor pieces sit one block further east (no -1);
//   - template names carry no _1 (corridor_floor, carpet_south, ...);
//   - the start height is never refused: below sea level it is raised to
//     sea level;
//   - the start biome test reaches 32 blocks and takes pale gardens and
//     the cave biomes under dark forests;
//   - after the cobblestone foundation two runs of cobblestone stairs step
//     down from beside the entrance.
// No evokers, vindicators or allays are spawned; the allay marker still
// draws its count.
// VERIFY: data markers are taken in template order (BDS: a hash map); the
// allay marker draws in every chunk the piece touches; heights come from
// the undecorated start chunk.

// Woodland mansion placement (sub_4FBC500): triangular spread.
const (
	mansionSalt       = 10387319
	mansionSpacing    = 80
	mansionSeparation = 20
)

// mansionBiomes are the biomes a mansion starts in (sub_4FBBDB0).
func mansionBiomes() []string {
	return []string{"pale_garden", "roofed_forest", "roofed_forest_mutated", "dripstone_caves",
		"lush_caves", "sulfur_caves"}
}

// Horizontal directions in Bedrock's numbering, as the placer uses them.
const (
	dirUp    int32 = 1
	dirNorth int32 = 2
	dirSouth int32 = 3
	dirWest  int32 = 4
	dirEast  int32 = 5
	dirNone  int32 = 6
)

// dirStep is the x and z step of a horizontal direction.
func dirStep(d int32) (int32, int32) {
	switch d {
	case dirNorth:
		return 0, -1
	case dirSouth:
		return 0, 1
	case dirWest:
		return -1, 0
	case dirEast:
		return 1, 0
	}
	return 0, 0
}

// dirCW and dirCCW turn a horizontal direction a quarter clockwise and
// counterclockwise.
func dirCW(d int32) int32 {
	return [6]int32{0, 0, dirEast, dirWest, dirNorth, dirSouth}[d%6]
}

func dirCCW(d int32) int32 { return dirCW(dirCW(dirCW(d))) }

// rotDir is Rotation.rotate: a direction turned rot quarter turns
// clockwise.
func rotDir(rot int, d int32) int32 {
	for range rot & 3 {
		d = dirCW(d)
	}
	return d
}

// rel is a position moved n blocks along a direction.
func rel(p [3]int32, d, n int32) [3]int32 {
	dx, dz := dirStep(d)
	return [3]int32{p[0] + dx*n, p[1], p[2] + dz*n}
}

// mansionGridSize is the side of BDS's square mansion grid, in cells.
const mansionGridSize = 11

// Base grid cell values.
const (
	// cellEmpty is a cell nothing has claimed yet.
	cellEmpty = 0
	// cellCorridor is a corridor cell.
	cellCorridor = 1
	// cellRoom is a cell that becomes part of a room.
	cellRoom = 2
	// cellEntrance is the entrance hall, or the stairs on the third floor.
	cellEntrance = 3
	// cellHouseMax is the highest value that is still part of the building.
	cellHouseMax = 4
	// cellOutside is blocked space outside the building; reads past the grid
	// edge return it too.
	cellOutside = 5
)

// mansionGrid is BDS's square grid of cell values; outside it reads
// cellOutside.
type mansionGrid [mansionGridSize * mansionGridSize]int32

func (g *mansionGrid) get(x, y int32) int32 {
	if x < 0 || y < 0 || x >= mansionGridSize || y >= mansionGridSize {
		return cellOutside
	}
	return g[x*mansionGridSize+y]
}

func (g *mansionGrid) set(x, y, v int32) {
	if x >= 0 && y >= 0 && x < mansionGridSize && y < mansionGridSize {
		g[x*mansionGridSize+y] = v
	}
}

func (g *mansionGrid) fill(x0, y0, x1, y1, v int32) {
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			g.set(x, y, v)
		}
	}
}

// setIfEmpty sets a cell to v if it is still empty.
func (g *mansionGrid) setIfEmpty(x, y, v int32) {
	if g.get(x, y) == cellEmpty {
		g.set(x, y, v)
	}
}

// house reports whether a cell is part of the building (corridor up to
// cellHouseMax).
func (g *mansionGrid) house(x, y int32) bool {
	v := g.get(x, y)
	return v >= cellCorridor && v <= cellHouseMax
}

// edgesToCorridor reports whether a cell borders a corridor cell.
func (g *mansionGrid) edgesToCorridor(x, y int32) bool {
	return g.get(x-1, y) == cellCorridor || g.get(x+1, y) == cellCorridor ||
		g.get(x, y+1) == cellCorridor || g.get(x, y-1) == cellCorridor
}

// Room cell flags.
const (
	room1x1    = 0x10000
	room1x2    = 0x20000
	room2x2    = 0x40000
	roomStart  = 0x100000
	roomDoor   = 0x200000
	roomStairs = 0x400000
	roomEntry  = 0x800000
)

// Room cell fields.
const (
	// roomIDMask holds the room's id in a room cell.
	roomIDMask = 0xFFFF
	// roomTypeMask holds the room's size flag (room1x1, room1x2, room2x2).
	roomTypeMask = 0xF0000
	// roomTypeDoorMask is the size flag together with the door flag.
	roomTypeDoorMask = roomTypeMask | roomDoor
	// roomFirstID is the id the first room of a floor gets.
	roomFirstID = 10
)

// Corridor walk lengths and tries (sub_4FCDE80's callers).
const (
	// mansionLongCorridor is the length of the corridors from the entrance's
	// far corners.
	mansionLongCorridor = 6
	// mansionShortCorridor is the length of the corridors beside the
	// entrance.
	mansionShortCorridor = 3
	// mansionThirdCorridor is the length of the third floor's corridor.
	mansionThirdCorridor = 4
	// mansionCorridorTries is how many directions a corridor step draws
	// before it gives up branching.
	mansionCorridorTries = 8
)

// cleanEdges neighbour counts.
const (
	// cleanFillSides fills an empty cell with at least this many building
	// sides.
	cleanFillSides = 3
	// cleanCornerSides is the side count that also looks at the corners.
	cleanCornerSides = 2
	// cleanMaxCorners is the most building corners a cleanCornerSides cell
	// may have and still be filled.
	cleanMaxCorners = 1
)

// Mansion floors, as indexes into the per-floor room grids.
const (
	floorGround = 0
	floorSecond = 1
	floorThird  = 2
	// mansionFloors is how many floors a mansion has.
	mansionFloors = 3
)

// Block geometry of the mansion pieces.
const (
	// mansionCellBlocks is the width of one grid cell in blocks.
	mansionCellBlocks = 8
	// mansionFloorHeight is the rise from one floor to the next.
	mansionFloorHeight = 8
	// mansionThirdFloorLift is how much higher the third floor sits than a
	// plain floor step would put it.
	mansionThirdFloorLift = 3
	// mansionEntranceWest is how far west of the origin the entrance piece
	// starts.
	mansionEntranceWest = 9
	// mansionWallSouth is how far south of the origin the outer wall walk
	// starts.
	mansionWallSouth = 16
	// mansionUpperWallRise is the second floor's outer walls' height above
	// the first's.
	mansionUpperWallRise = 8
	// mansionThirdWallRise is the third floor's outer walls' height above
	// the first's.
	mansionThirdWallRise = 19
	// mansionRoofRise is the height of the roof over the first two floors.
	mansionRoofRise = 16
	// mansionTopRoofRise is the height of the roof over the third floor.
	mansionTopRoofRise = 27
	// mansionRoofLift is how far a roof piece sits above its cell's corner.
	mansionRoofLift = 3
	// mansionFoundationFloor is the lowest y the cobblestone foundation
	// fills down to.
	mansionFoundationFloor = 2
	// mansionStepsEast is how far east of the start the entrance steps
	// begin.
	mansionStepsEast = 9
	// mansionStepWidth is how many columns wide each run of steps is.
	mansionStepWidth = 2
	// mansionBiomeReach is the radius of the start biome test, in blocks.
	mansionBiomeReach = 32
	// mansionAllayDraws is the bound the allay marker draws its count with.
	mansionAllayDraws = 3
)

// mansionLayout is the grid of a mansion: the base, the third floor and
// the rooms of each floor.
type mansionLayout struct {
	r     *rand.MT
	base  mansionGrid
	third mansionGrid
	rooms [mansionFloors]mansionGrid
}

// Entrance cell of the base grid.
const mansionEntryX, mansionEntryY = 7, 4

// mansionEdgeRows is how many grid rows at each side edge are blocked
// (rows 0-1 and 9-10).
const mansionEdgeRows = 2

func newMansionLayout(r *rand.MT) *mansionLayout {
	l := &mansionLayout{r: r}
	g := &l.base
	ex, ey := int32(mansionEntryX), int32(mansionEntryY)
	g.fill(ex, ey, ex+1, ey+1, cellEntrance)
	g.fill(ex-1, ey, ex-1, ey+1, cellRoom)
	g.fill(ex+2, ey-2, ex+3, ey+3, cellOutside)
	g.fill(ex+1, ey-2, ex+1, ey-1, cellCorridor)
	g.fill(ex+1, ey+2, ex+1, ey+3, cellCorridor)
	g.set(ex-1, ey-1, cellCorridor)
	g.set(ex-1, ey+2, cellCorridor)
	// The two rows at each side edge stay outside the building.
	g.fill(0, 0, mansionGridSize, mansionEdgeRows-1, cellOutside)
	g.fill(0, mansionGridSize-mansionEdgeRows, mansionGridSize, mansionGridSize, cellOutside)
	l.corridor(g, ex, ey-2, dirWest, mansionLongCorridor)
	l.corridor(g, ex, ey+3, dirWest, mansionLongCorridor)
	l.corridor(g, ex-2, ey-1, dirWest, mansionShortCorridor)
	l.corridor(g, ex-2, ey+2, dirWest, mansionShortCorridor)
	for cleanEdges(g) {
	}
	l.identifyRooms(g, &l.rooms[floorGround])
	l.identifyRooms(g, &l.rooms[floorSecond])
	l.rooms[floorGround].fill(ex+1, ey, ex+1, ey+1, roomEntry)
	l.rooms[floorSecond].fill(ex+1, ey, ex+1, ey+1, roomEntry)
	l.setupThirdFloor()
	l.identifyRooms(&l.third, &l.rooms[floorThird])
	return l
}

// corridor is recursiveCorridor (sub_4FCDE80).
func (l *mansionLayout) corridor(g *mansionGrid, x, y, dir, length int32) {
	if length <= 0 {
		return
	}
	g.set(x, y, cellCorridor)
	dx, dz := dirStep(dir)
	nx, ny := x+dx, y+dz
	g.setIfEmpty(nx, ny, cellCorridor)
	opp := dirCW(dirCW(dir))
	dirs := [...]int32{dirNorth, dirSouth, dirWest, dirEast}
	for range mansionCorridorTries {
		d := dirs[l.r.IntN(int32(len(dirs)))]
		if d == opp || d == dirEast && nextBool(l.r) {
			continue
		}
		sx, sz := dirStep(d)
		if g.get(nx+sx, ny+sz) == cellEmpty && g.get(nx+2*sx, ny+2*sz) == cellEmpty {
			l.corridor(g, nx+sx, ny+sz, d, length-1)
			break
		}
	}
	cx, cz := dirStep(dirCW(dir))
	ax, az := dirStep(dirCCW(dir))
	g.setIfEmpty(x+cx, y+cz, cellRoom)
	g.setIfEmpty(x+ax, y+az, cellRoom)
	g.setIfEmpty(nx+cx, ny+cz, cellRoom)
	g.setIfEmpty(nx+ax, ny+az, cellRoom)
	g.setIfEmpty(x+2*dx, y+2*dz, cellRoom)
	g.setIfEmpty(x+2*cx, y+2*cz, cellRoom)
	g.setIfEmpty(x+2*ax, y+2*az, cellRoom)
}

// cleanEdges turns empty cells mostly surrounded by building into rooms
// (sub_4FCE3A0) and reports whether it changed any.
func cleanEdges(g *mansionGrid) bool {
	touched := false
	for y := range int32(mansionGridSize) {
		for x := range int32(mansionGridSize) {
			if g.get(x, y) != cellEmpty {
				continue
			}
			n := b2i(g.house(x+1, y)) + b2i(g.house(x-1, y)) + b2i(g.house(x, y+1)) + b2i(g.house(x, y-1))
			if n >= cleanFillSides {
				g.set(x, y, cellRoom)
				touched = true
			} else if n == cleanCornerSides {
				d := b2i(g.house(x+1, y+1)) + b2i(g.house(x-1, y+1)) + b2i(g.house(x+1, y-1)) + b2i(g.house(x-1, y-1))
				if d <= cleanMaxCorners {
					g.set(x, y, cellRoom)
					touched = true
				}
			}
		}
	}
	return touched
}

// identifyRooms groups a floor's room cells into 1x1, 1x2 and 2x2 rooms
// and picks their doors (sub_4FCE780).
func (l *mansionLayout) identifyRooms(base, rooms *mansionGrid) {
	var cells [][2]int32
	for y := range int32(mansionGridSize) {
		for x := range int32(mansionGridSize) {
			if base.get(x, y) == cellRoom {
				cells = append(cells, [2]int32{x, y})
			}
		}
	}
	for k := len(cells) - 1; k >= 1; k-- {
		j := l.r.IntN(int32(k))
		cells[j], cells[k] = cells[k], cells[j]
	}
	id := int32(roomFirstID)
	for _, c := range cells {
		x, y := c[0], c[1]
		if !rooms.free(x, y) {
			continue
		}
		x0, x1, y0, y1 := x, x, y, y
		typ := int32(room1x1)
		switch {
		case rooms.free(x+1, y) && rooms.free(x, y+1) && rooms.free(x+1, y+1) &&
			base.get(x+1, y) == cellRoom && base.get(x, y+1) == cellRoom && base.get(x+1, y+1) == cellRoom:
			x1, y1, typ = x+1, y+1, room2x2
		case rooms.free(x-1, y) && rooms.free(x, y+1) && rooms.free(x-1, y+1) &&
			base.get(x-1, y) == cellRoom && base.get(x, y+1) == cellRoom && base.get(x-1, y+1) == cellRoom:
			x0, y1, typ = x-1, y+1, room2x2
		case rooms.free(x-1, y) && rooms.free(x, y-1) && rooms.free(x-1, y-1) &&
			base.get(x-1, y) == cellRoom && base.get(x, y-1) == cellRoom && base.get(x-1, y-1) == cellRoom:
			x0, y0, typ = x-1, y-1, room2x2
		case rooms.free(x+1, y) && base.get(x+1, y) == cellRoom:
			x1, typ = x+1, room1x2
		case rooms.free(x, y+1) && base.get(x, y+1) == cellRoom:
			y1, typ = y+1, room1x2
		case rooms.free(x-1, y) && base.get(x-1, y) == cellRoom:
			x0, typ = x-1, room1x2
		case rooms.free(x, y-1) && base.get(x, y-1) == cellRoom:
			y0, typ = y-1, room1x2
		}
		dx := x1
		if nextBool(l.r) {
			dx = x0
		}
		dy := y1
		if nextBool(l.r) {
			dy = y0
		}
		door := int32(roomStart | roomDoor)
		if !base.edgesToCorridor(dx, dy) {
			dx, dy = flipEnd(dx, x0, x1), flipEnd(dy, y0, y1)
			if !base.edgesToCorridor(dx, dy) {
				dy = flipEnd(dy, y0, y1)
				if !base.edgesToCorridor(dx, dy) {
					dx, dy = flipEnd(dx, x0, x1), flipEnd(dy, y0, y1)
					if !base.edgesToCorridor(dx, dy) {
						door, dx, dy = roomStart, x0, y0
					}
				}
			}
		}
		for yy := y0; yy <= y1; yy++ {
			for xx := x0; xx <= x1; xx++ {
				if xx == dx && yy == dy {
					rooms.set(xx, yy, door|typ|id)
				} else {
					rooms.set(xx, yy, typ|id)
				}
			}
		}
		id++
	}
}

// setupThirdFloor builds the third floor's grid over a 1x2 second floor
// room with a door, which gets the stairs.
func (l *mansionLayout) setupThirdFloor() {
	second := &l.rooms[floorSecond]
	var spots [][2]int32
	for y := range int32(mansionGridSize) {
		for x := range int32(mansionGridSize) {
			if second.get(x, y)&roomTypeDoorMask == room1x2|roomDoor {
				spots = append(spots, [2]int32{x, y})
			}
		}
	}
	if len(spots) == 0 {
		l.third.fill(0, 0, mansionGridSize, mansionGridSize, cellOutside)
		return
	}
	s := spots[l.r.IntN(int32(len(spots)))]
	v := second.get(s[0], s[1])
	second.set(s[0], s[1], v|roomStairs)
	id := v & roomIDMask
	d := roomDirection(second, s[0], s[1], id)
	dx, dz := dirStep(d)
	x2, y2 := s[0]+dx, s[1]+dz
	for y := range int32(mansionGridSize) {
		for x := range int32(mansionGridSize) {
			switch {
			case !l.base.house(x, y):
				l.third.set(x, y, cellOutside)
			case x == s[0] && y == s[1]:
				l.third.set(x, y, cellEntrance)
			case x == x2 && y == y2:
				l.third.set(x, y, cellEntrance)
				l.rooms[floorThird].set(x, y, roomEntry)
			}
		}
	}
	var dirs []int32
	for _, dd := range [4]int32{dirNorth, dirEast, dirSouth, dirWest} {
		sx, sz := dirStep(dd)
		if l.third.get(x2+sx, y2+sz) == cellEmpty {
			dirs = append(dirs, dd)
		}
	}
	if len(dirs) == 0 {
		l.third.fill(0, 0, mansionGridSize, mansionGridSize, cellOutside)
		second.set(s[0], s[1], v)
		return
	}
	dd := dirs[l.r.IntN(int32(len(dirs)))]
	sx, sz := dirStep(dd)
	l.corridor(&l.third, x2+sx, y2+sz, dd, mansionThirdCorridor)
	for cleanEdges(&l.third) {
	}
}

// roomDirection is get1x2RoomDirection: the side of a cell where the rest
// of its room lies, dirNone if none.
func roomDirection(rooms *mansionGrid, x, y, id int32) int32 {
	for _, d := range [4]int32{dirNorth, dirEast, dirSouth, dirWest} {
		dx, dz := dirStep(d)
		if rooms.get(x+dx, y+dz)&roomIDMask == id {
			return d
		}
	}
	return dirNone
}

// mansionPiece is one template of a mansion.
type mansionPiece struct {
	name   string
	pos    [3]int32
	rot    int
	mirror uint8
	bb     structure.Box
}

// mansion is a laid out woodland mansion.
type mansion struct {
	origin [3]int32
	rot    int
	pieces []mansionPiece
	bb     structure.Box
}

// mansionPlacer is MansionPiecePlacer (sub_4FBFFB0).
type mansionPlacer struct {
	o      *Overworld
	r      *rand.MT
	pieces []mansionPiece
}

// add appends a piece, sizing its box from the template (sub_D27F200: the
// box reaches the template's size on x and z, one past its last block).
func (p *mansionPlacer) add(name string, pos [3]int32, rot int, mirror uint8) {
	rot &= 3
	mp := mansionPiece{name: name, pos: pos, rot: rot, mirror: mirror}
	if jt := p.o.st.Template("mansion/" + name); jt != nil {
		s := jt.Template.Size
		e := structure.RotatePos(structure.MirrorPos([3]int32{s[0], 0, s[2]}, mirror), rot)
		mp.bb = structure.NewBox(min(pos[0], pos[0]+e[0]), pos[1], min(pos[2], pos[2]+e[2]),
			max(pos[0], pos[0]+e[0]), pos[1]+s[1]-1, max(pos[2], pos[2]+e[2]))

	}
	p.pieces = append(p.pieces, mp)
}

// placement is PlacementData: where the outer wall walk stands.
type placement struct {
	rot  int
	pos  [3]int32
	wall string
}

// Start cell of the outer wall walk.
const mansionStartX, mansionStartY = mansionEntryX + 1, mansionEntryY + 1

func (p *mansionPlacer) create(origin [3]int32, rot int, l *mansionLayout) {
	d := placement{rot, origin, "wall_flat"}
	p.add("entrance", rel(origin, rotDir(rot, dirWest), mansionEntranceWest), rot, structure.MirrorNone)
	d.pos = rel(d.pos, rotDir(rot, dirSouth), mansionWallSouth)
	d2 := placement{rot, [3]int32{d.pos[0], d.pos[1] + mansionUpperWallRise, d.pos[2]}, "wall_window"}
	p.outerWalls(&d, &l.base, mansionStartX, mansionStartY, mansionStartX, mansionEntryY)
	p.outerWalls(&d2, &l.base, mansionStartX, mansionStartY, mansionStartX, mansionEntryY)
	d3 := placement{rot, [3]int32{d.pos[0], d.pos[1] + mansionThirdWallRise, d.pos[2]}, "wall_window"}
third:
	for y := range int32(mansionGridSize) {
		for x := int32(mansionGridSize - 1); x >= 0; x-- {
			if l.third.house(x, y) {
				d3.pos = rel(d3.pos, rotDir(rot, dirSouth), mansionCellBlocks+(y-mansionStartY)*mansionCellBlocks)
				d3.pos = rel(d3.pos, rotDir(rot, dirEast), (x-mansionStartX)*mansionCellBlocks)
				p.wallPiece(&d3)
				p.outerWalls(&d3, &l.third, x, y, x, y)
				break third
			}
		}
	}
	p.roof([3]int32{origin[0], origin[1] + mansionRoofRise, origin[2]}, rot, &l.base, &l.third)
	p.roof([3]int32{origin[0], origin[1] + mansionTopRoofRise, origin[2]}, rot, &l.third, nil)
	for f := range mansionFloors {
		p.floor(f, origin, rot, l)
	}
}

// outerWalls is traverseOuterWalls (sub_4FC7D20): it walks the building's
// edge from (x, y), heading south, until it is back at (endX, endY).
func (p *mansionPlacer) outerWalls(d *placement, g *mansionGrid, x, y, endX, endY int32) {
	dir := dirSouth
	for {
		dx, dz := dirStep(dir)
		if !g.house(x+dx, y+dz) {
			p.turn(d)
			dir = dirCW(dir)
			if x != endX || y != endY || dir != dirSouth {
				p.wallPiece(d)
			}
		} else if ax, az := dirStep(dirCCW(dir)); g.house(x+dx+ax, y+dz+az) {
			d.pos = rel(rel(d.pos, rotDir(d.rot, dirSouth), 6), rotDir(d.rot, dirEast), mansionCellBlocks)
			d.rot = (d.rot + 3) & 3
			x, y = x+dx, y+dz
			dir = dirCCW(dir)
		} else {
			x, y = x+dx, y+dz
			if x != endX || y != endY || dir != dirSouth {
				p.wallPiece(d)
			}
		}
		if x == endX && y == endY && dir == dirSouth {
			return
		}
	}
}

// turn is traverseTurn: a wall corner, then the walk turns clockwise.
func (p *mansionPlacer) turn(d *placement) {
	d.pos = rel(d.pos, rotDir(d.rot, dirSouth), -1)
	p.add("wall_corner", d.pos, d.rot, structure.MirrorNone)
	d.pos = rel(rel(d.pos, rotDir(d.rot, dirSouth), -7), rotDir(d.rot, dirWest), -6)
	d.rot = (d.rot + 1) & 3
}

// wallPiece is traverseWallPiece (sub_4FC82D0).
func (p *mansionPlacer) wallPiece(d *placement) {
	p.add(d.wall, rel(d.pos, rotDir(d.rot, dirEast), 7), d.rot, structure.MirrorNone)
	d.pos = rel(d.pos, rotDir(d.rot, dirSouth), mansionCellBlocks)
}

// cellPos is a grid cell's corner on a floor, shifted east by dx.
func cellPos(base [3]int32, rot int, x, y, dx int32) [3]int32 {
	return rel(rel(base, rotDir(rot, dirSouth), mansionCellBlocks+(y-mansionStartY)*mansionCellBlocks),
		rotDir(rot, dirEast), dx+(x-mansionStartX)*mansionCellBlocks)
}

// roof is createRoof (sub_4FC85E0): roofs over g where upper has no
// building, the small walls where it has, then the roof corners.
func (p *mansionPlacer) roof(origin [3]int32, rot int, g, upper *mansionGrid) {
	n, e, s, w := rotDir(rot, dirNorth), rotDir(rot, dirEast), rotDir(rot, dirSouth), rotDir(rot, dirWest)
	for y := range int32(mansionGridSize) {
		for x := range int32(mansionGridSize) {
			if !g.house(x, y) || houseAt(upper, x, y) {
				continue
			}
			c := cellPos(origin, rot, x, y, 0)
			p.add("roof", [3]int32{c[0], c[1] + mansionRoofLift, c[2]}, rot, structure.MirrorNone)
			if !g.house(x+1, y) {
				p.add("roof_front", rel(c, e, 6), rot, structure.MirrorNone)
			}
			if !g.house(x-1, y) {
				p.add("roof_front", rel(c, s, 7), rot+2, structure.MirrorNone)
			}
			if !g.house(x, y-1) {
				p.add("roof_front", rel(c, w, 1), rot+3, structure.MirrorNone)
			}
			if !g.house(x, y+1) {
				p.add("roof_front", rel(rel(c, e, 6), s, 6), rot+1, structure.MirrorNone)
			}
		}
	}
	if upper != nil {
		for y := range int32(mansionGridSize) {
			for x := range int32(mansionGridSize) {
				if !g.house(x, y) || !upper.house(x, y) {
					continue
				}
				c := cellPos(origin, rot, x, y, 0)
				if !g.house(x+1, y) {
					p.add("small_wall", rel(c, e, 7), rot, structure.MirrorNone)
				}
				if !g.house(x-1, y) {
					p.add("small_wall", rel(rel(c, w, 1), s, 6), rot+2, structure.MirrorNone)
				}
				if !g.house(x, y-1) {
					p.add("small_wall", rel(c, n, 1), rot+3, structure.MirrorNone)
				}
				if !g.house(x, y+1) {
					p.add("small_wall", rel(rel(c, e, 6), s, 7), rot+1, structure.MirrorNone)
				}
				if !g.house(x+1, y) {
					if !g.house(x, y-1) {
						p.add("small_wall_corner", rel(rel(c, e, 7), n, 2), rot, structure.MirrorNone)
					}
					if !g.house(x, y+1) {
						p.add("small_wall_corner", rel(rel(c, e, 8), s, 7), rot+1, structure.MirrorNone)
					}
				}
				if !g.house(x-1, y) {
					if !g.house(x, y-1) {
						p.add("small_wall_corner", rel(rel(c, w, 2), n, 1), rot+3, structure.MirrorNone)
					}
					if !g.house(x, y+1) {
						p.add("small_wall_corner", rel(rel(c, w, 1), s, 8), rot+2, structure.MirrorNone)
					}
				}
			}
		}
	}
	for y := range int32(mansionGridSize) {
		for x := range int32(mansionGridSize) {
			if !g.house(x, y) || houseAt(upper, x, y) {
				continue
			}
			c := cellPos(origin, rot, x, y, 0)
			if !g.house(x+1, y) {
				q := rel(c, e, 6)
				if !g.house(x, y+1) {
					p.add("roof_corner", rel(q, s, 6), rot, structure.MirrorNone)
				} else if g.house(x+1, y+1) {
					p.add("roof_inner_corner", rel(q, s, 5), rot, structure.MirrorNone)
				}
				if !g.house(x, y-1) {
					p.add("roof_corner", q, rot+3, structure.MirrorNone)
				} else if g.house(x+1, y-1) {
					p.add("roof_inner_corner", rel(rel(c, e, 9), n, 2), rot+1, structure.MirrorNone)
				}
			}
			if !g.house(x-1, y) {
				if !g.house(x, y+1) {
					p.add("roof_corner", rel(c, s, 6), rot+1, structure.MirrorNone)
				} else if g.house(x-1, y+1) {
					p.add("roof_inner_corner", rel(rel(c, s, 8), w, 3), rot+3, structure.MirrorNone)
				}
				if !g.house(x, y-1) {
					p.add("roof_corner", c, rot+2, structure.MirrorNone)
				} else if g.house(x-1, y-1) {
					p.add("roof_inner_corner", rel(c, s, 1), rot+2, structure.MirrorNone)
				}
			}
		}
	}
}

// floor places a floor's corridors, carpets, room walls and rooms.
func (p *mansionPlacer) floor(f int, origin [3]int32, rot int, l *mansionLayout) {
	n, e, s, w := rotDir(rot, dirNorth), rotDir(rot, dirEast), rotDir(rot, dirSouth), rotDir(rot, dirWest)
	fy := origin[1] + mansionFloorHeight*int32(f)
	if f == floorThird {
		fy += 3
	}
	base := [3]int32{origin[0], fy, origin[2]}
	rooms := &l.rooms[f]
	g := &l.base
	if f == floorThird {
		g = &l.third
	}
	corridor, carpetS, carpetW := "corridor_floor_2", "carpet_south_2", "carpet_west_2"
	wall, doorName := "indoors_wall_2", "indoors_door_2"
	if f == floorGround {
		corridor, carpetS, carpetW = "corridor_floor", "carpet_south", "carpet_west"
		wall, doorName = "indoors_wall", "indoors_door"
	}
	for y := range int32(mansionGridSize) {
		for x := range int32(mansionGridSize) {
			if g.get(x, y) != cellCorridor {
				continue
			}
			c := cellPos(base, rot, x, y, 0)
			p.add(corridor, c, rot, structure.MirrorNone)
			if corridorOpen(g, rooms, x, y-1) {
				p.add("carpet_north", oneUp(rel(c, e, 1)), rot, structure.MirrorNone)
			}
			if corridorOpen(g, rooms, x+1, y) {
				p.add("carpet_east", oneUp(rel(rel(c, s, 1), e, 5)), rot, structure.MirrorNone)
			}
			if corridorOpen(g, rooms, x, y+1) {
				p.add(carpetS, rel(rel(c, s, 5), w, 1), rot, structure.MirrorNone)
			}
			if corridorOpen(g, rooms, x-1, y) {
				p.add(carpetW, rel(rel(c, w, 1), n, 1), rot, structure.MirrorNone)
			}
		}
	}
	coll := mansionRooms{second: f > floorGround}
	for y := range int32(mansionGridSize) {
		for x := range int32(mansionGridSize) {
			entry := f == floorThird && g.get(x, y) == cellEntrance
			if g.get(x, y) != cellRoom && !entry {
				continue
			}
			room := rooms.get(x, y)
			typ, id := room&roomTypeMask, room&roomIDMask
			entry = entry && room&roomEntry != 0
			door := dirNone
			if room&roomDoor != 0 {
				var dirs []int32
				for _, d := range [4]int32{dirNorth, dirEast, dirSouth, dirWest} {
					dx, dz := dirStep(d)
					if g.get(x+dx, y+dz) == cellCorridor {
						dirs = append(dirs, d)
					}
				}
				if len(dirs) > 0 {
					door = dirs[p.r.IntN(int32(len(dirs)))]
				}
			}
			if door == dirNone && room&roomStart != 0 {
				door = dirUp
			}
			c := cellPos(base, rot, x, y, -1)
			if g.house(x-1, y) && rooms.get(x-1, y)&roomIDMask != id {
				p.add(wallOrDoor(door, dirWest, doorName, wall), c, rot, structure.MirrorNone)
			}
			if g.get(x+1, y) == cellCorridor && !entry {
				p.add(wallOrDoor(door, dirEast, doorName, wall), rel(c, e, 8), rot, structure.MirrorNone)
			}
			if g.house(x, y+1) && rooms.get(x, y+1)&roomIDMask != id {
				p.add(wallOrDoor(door, dirSouth, doorName, wall), rel(rel(c, s, 7), e, 7), rot+1, structure.MirrorNone)
			}
			if g.get(x, y-1) == cellCorridor && !entry {
				p.add(wallOrDoor(door, dirNorth, doorName, wall), rel(rel(c, n, 1), e, 7), rot+1, structure.MirrorNone)
			}
			switch {
			case typ == room1x1:
				p.room1x1(c, rot, door, coll)
			case typ == room1x2 && door != dirNone:
				p.room1x2(c, rot, roomDirection(rooms, x, y, id), door, coll, room&roomStairs != 0)
			case typ == room2x2 && door == dirUp:
				p.add(coll.secret2x2(), rel(c, e, 1), rot, structure.MirrorNone)
			case typ == room2x2 && door != dirNone:
				dir := dirCW(door)
				if dx, dz := dirStep(dir); rooms.get(x+dx, y+dz)&roomIDMask != id {
					dir = dirCW(dirCW(dir))
				}
				p.room2x2(c, rot, dir, door, coll)
			}
		}
	}
}

// room1x1 is addRoom1x1: the room turned toward its door, a secret room
// when it has none to the side.
func (p *mansionPlacer) room1x1(c [3]int32, rot int, door int32, coll mansionRooms) {
	name := coll.room1x1(p.r)
	turn, off := 0, [3]int32{1, 0, 0}
	switch door {
	case dirNorth:
		turn, off = 3, [3]int32{1, 0, 6}
	case dirSouth:
		turn, off = 1, [3]int32{7, 0, 0}
	case dirWest:
		turn, off = 2, [3]int32{7, 0, 6}
	case dirEast:
	default:
		name = coll.secret1x1(p.r)
	}
	o := structure.RotatePos(off, rot)
	p.add(name, [3]int32{c[0] + o[0], c[1], c[2] + o[2]}, rot+turn, structure.MirrorNone)
}

// room1x2 is addRoom1x2: the table of door side and room direction
// (sub_4FBFFB0's 1x2 branch).
func (p *mansionPlacer) room1x2(c [3]int32, rot int, dir, door int32, coll mansionRooms, stairs bool) {
	n, e, s, w := rotDir(rot, dirNorth), rotDir(rot, dirEast), rotDir(rot, dirSouth), rotDir(rot, dirWest)
	switch {
	case door == dirEast && dir == dirSouth:
		p.add(coll.side1x2(p.r, stairs), rel(c, e, 1), rot, structure.MirrorNone)
	case door == dirEast && dir == dirNorth:
		p.add(coll.side1x2(p.r, stairs), rel(rel(c, e, 1), s, 6), rot, structure.MirrorZ)
	case door == dirWest && dir == dirNorth:
		p.add(coll.side1x2(p.r, stairs), rel(rel(c, e, 7), s, 6), rot+2, structure.MirrorNone)
	case door == dirWest && dir == dirSouth:
		p.add(coll.side1x2(p.r, stairs), rel(c, e, 7), rot, structure.MirrorX)
	case door == dirSouth && dir == dirEast:
		p.add(coll.side1x2(p.r, stairs), rel(c, e, 1), rot+1, structure.MirrorZ)
	case door == dirSouth && dir == dirWest:
		p.add(coll.side1x2(p.r, stairs), rel(c, e, 7), rot+1, structure.MirrorNone)
	case door == dirNorth && dir == dirWest:
		p.add(coll.side1x2(p.r, stairs), rel(rel(c, e, 7), s, 6), rot+1, structure.MirrorX)
	case door == dirNorth && dir == dirEast:
		p.add(coll.side1x2(p.r, stairs), rel(rel(c, e, 1), s, 6), rot+3, structure.MirrorNone)
	case door == dirSouth && dir == dirNorth:
		p.add(coll.front1x2(p.r, stairs), rel(rel(c, e, 1), n, 8), rot, structure.MirrorNone)
	case door == dirNorth && dir == dirSouth:
		p.add(coll.front1x2(p.r, stairs), rel(rel(c, e, 7), s, 14), rot+2, structure.MirrorNone)
	case door == dirWest && dir == dirEast:
		p.add(coll.front1x2(p.r, stairs), rel(c, e, 15), rot+1, structure.MirrorNone)
	case door == dirEast && dir == dirWest:
		p.add(coll.front1x2(p.r, stairs), rel(rel(c, w, 7), s, 6), rot+3, structure.MirrorNone)
	case door == dirUp && dir == dirEast:
		p.add(coll.secret1x2(p.r), rel(c, e, 15), rot+1, structure.MirrorNone)
	case door == dirUp && dir == dirSouth:
		p.add(coll.secret1x2(p.r), rel(c, e, 1), rot, structure.MirrorNone)
	}
}

// room2x2 is addRoom2x2.
func (p *mansionPlacer) room2x2(c [3]int32, rot int, dir, door int32, coll mansionRooms) {
	var x, z int32
	turn, mirror := 0, structure.MirrorNone
	switch {
	case door == dirEast && dir == dirSouth:
		x = -7
	case door == dirEast && dir == dirNorth:
		x, z, mirror = -7, 6, structure.MirrorZ
	case door == dirNorth && dir == dirEast:
		x, z, turn = 1, 14, 3
	case door == dirNorth && dir == dirWest:
		x, z, turn, mirror = 7, 14, 3, structure.MirrorZ
	case door == dirSouth && dir == dirWest:
		x, z, turn = 7, -8, 1
	case door == dirSouth && dir == dirEast:
		x, z, turn, mirror = 1, -8, 1, structure.MirrorZ
	case door == dirWest && dir == dirNorth:
		x, z, turn = 15, 6, 2
	case door == dirWest && dir == dirSouth:
		x, mirror = 15, structure.MirrorX
	}
	o := structure.RotatePos([3]int32{x, 0, z}, rot)
	p.add(coll.room2x2(p.r), [3]int32{c[0] + o[0], c[1], c[2] + o[2]}, rot+turn, mirror)
}

// mansionRooms is a floor's room collection: the first floor's, or the
// second floor's, which the third floor shares.
type mansionRooms struct{ second bool }

// Template variant counts of each room kind: a room draws a number from
// 1 to the count and appends it to the template name.
const (
	mansion1x1AVariants       = 5
	mansion1x1BVariants       = 5
	mansionSecret1x1Variants  = 4
	mansion1x2AVariants       = 9
	mansion1x2BVariants       = 5
	mansion1x2CVariants       = 4
	mansion1x2DVariants       = 5
	mansionSecret1x2AVariants = 2
	mansionSecret1x2BVariants = 1
	mansion2x2AVariants       = 4
	mansion2x2BVariants       = 5
)

func numbered(prefix string, r *rand.MT, n int32) string {
	return prefix + strconv.Itoa(int(r.IntN(n))+1)
}

func (c mansionRooms) room1x1(r *rand.MT) string {
	if c.second {
		return numbered("1x1_b", r, mansion1x1BVariants)
	}
	return numbered("1x1_a", r, mansion1x1AVariants)
}

func (c mansionRooms) secret1x1(r *rand.MT) string {
	return numbered("1x1_as", r, mansionSecret1x1Variants)
}

func (c mansionRooms) side1x2(r *rand.MT, stairs bool) string {
	if !c.second {
		return numbered("1x2_a", r, mansion1x2AVariants)
	}
	if stairs {
		return "1x2_c_stairs"
	}
	return numbered("1x2_c", r, mansion1x2CVariants)
}

func (c mansionRooms) front1x2(r *rand.MT, stairs bool) string {
	if !c.second {
		return numbered("1x2_b", r, mansion1x2BVariants)
	}
	if stairs {
		return "1x2_d_stairs"
	}
	return numbered("1x2_d", r, mansion1x2DVariants)
}

func (c mansionRooms) secret1x2(r *rand.MT) string {
	if c.second {
		return numbered("1x2_se", r, mansionSecret1x2BVariants)
	}
	return numbered("1x2_s", r, mansionSecret1x2AVariants)
}

func (c mansionRooms) room2x2(r *rand.MT) string {
	if c.second {
		return numbered("2x2_b", r, mansion2x2BVariants)
	}
	return numbered("2x2_a", r, mansion2x2AVariants)
}

func (c mansionRooms) secret2x2() string { return "2x2_s1" }

// mansionPick is the chunk a mansion region picks.
func (o *Overworld) mansionPick(rx, rz int32) (int32, int32) {
	var r rand.MT
	structureSeed(&r, o.seed, rx, rz, mansionSalt)
	n := int32(mansionSpacing - mansionSeparation)
	a := r.IntN(n)
	x := rx*mansionSpacing + (a+r.IntN(n))/2
	b := r.IntN(n)
	return x, rz*mansionSpacing + (b+r.IntN(n))/2
}

// isMansionChunk reports whether a woodland mansion starts in chunk
// (cx, cz).
func (o *Overworld) isMansionChunk(cx, cz int32) bool {
	x, z := o.mansionPick(structure.Region(cx, mansionSpacing), structure.Region(cz, mansionSpacing))
	if x != cx || z != cz {
		return false
	}
	return o.biomesOnly(cx*16+8, o.structureY(cx, cz, noSurfaceY), cz*16+8, mansionBiomeReach, o.mansionBiome)
}

// mansionAt returns the mansion starting in chunk (cx, cz), nil if none
// does. It is laid out once per start.
func (o *Overworld) mansionAt(cx, cz int32, raw func(cx, cz int32) *PackedChunk) *mansion {
	k := [2]int32{cx, cz}
	if v, ok := o.mansions.Load(k); ok {
		m, _ := v.(*mansion)
		return m
	}
	var m *mansion
	if o.isMansionChunk(cx, cz) {
		m = o.layMansion(cx, cz, raw(cx, cz))
	}
	o.mansions.Store(k, m)
	return m
}

// Start chunk columns sampled for the mansion's height.
const (
	// mansionNearCorner is the near corner's column on x and z.
	mansionNearCorner = 7
	// mansionFarHigh and mansionFarLow are the far corner's column on an
	// axis the turn points along or against.
	mansionFarHigh = 12
	mansionFarLow  = 2
)

// layMansion is the start (sub_4FBC760): the turn, the height from the
// start chunk's terrain, the grid and the pieces.
func (o *Overworld) layMansion(cx, cz int32, start *PackedChunk) *mansion {
	r := new(rand.MT)
	r.Seed(populationSeed(o.seed, cx, cz))
	rot := int(r.IntN(structRotations))
	// The far corner columns by turn (qword_1541748, dword_1541754).
	fx := [structRotations]int32{mansionFarHigh, mansionFarLow, mansionFarLow, mansionFarHigh}[rot]
	fz := [structRotations]int32{mansionFarHigh, mansionFarHigh, mansionFarLow, mansionFarLow}[rot]
	low := int32(MaxY)
	for _, c := range [4][2]int32{{mansionNearCorner, mansionNearCorner}, {mansionNearCorner, fz}, {fx, mansionNearCorner}, {fx, fz}} {
		low = min(low, o.topSolid(start, cx*16+c[0], cz*16+c[1], false))
	}
	m := &mansion{origin: [3]int32{cx*16 + 8, max(low, SeaLevel-1) + 1, cz*16 + 8}, rot: rot}
	p := &mansionPlacer{o: o, r: r}
	p.create(m.origin, rot, newMansionLayout(r))
	m.pieces = p.pieces
	m.bb = structure.Box{X0: boxUnset, Y0: boxUnset, Z0: boxUnset, X1: -boxUnset, Y1: -boxUnset, Z1: -boxUnset}
	for _, pc := range m.pieces {
		m.bb = structure.NewBox(min(m.bb.X0, pc.bb.X0), min(m.bb.Y0, pc.bb.Y0), min(m.bb.Z0, pc.bb.Z0),
			max(m.bb.X1, pc.bb.X1), max(m.bb.Y1, pc.bb.Y1), max(m.bb.Z1, pc.bb.Z1))

	}
	return m
}

// mansionReach is how many chunks a mansion's pieces reach from its start
// chunk.
const mansionReach = 8

// mansionsReach calls f for each mansion with a piece in chunk (cx, cz).
func (o *Overworld) mansionsReach(cx, cz int32, raw func(cx, cz int32) *PackedChunk, f func(m *mansion)) {
	chunk := structure.NewBox(cx*16, MinY, cz*16, cx*16+15, MaxY-1, cz*16+15)
	for rx := structure.Region(cx-mansionReach, mansionSpacing); rx <= structure.Region(cx+mansionReach, mansionSpacing); rx++ {
		for rz := structure.Region(cz-mansionReach, mansionSpacing); rz <= structure.Region(cz+mansionReach, mansionSpacing); rz++ {
			sx, sz := o.mansionPick(rx, rz)
			if sx < cx-mansionReach || sx > cx+mansionReach || sz < cz-mansionReach || sz > cz+mansionReach {
				continue
			}
			if m := o.mansionAt(sx, sz, raw); m != nil && m.bb.Intersects(chunk) {
				f(m)
			}
		}
	}
}

// mansionReaches reports whether a mansion has a piece in chunk (cx, cz).
func (o *Overworld) mansionReaches(cx, cz int32, raw func(cx, cz int32) *PackedChunk) bool {
	found := false
	o.mansionsReach(cx, cz, raw, func(*mansion) { found = true })
	return found
}

// placeMansions writes the mansions that reach chunk (cx, cz) into c: the
// pieces in the chunk, then the foundation and the entrance steps
// (WoodlandMansionStart's post pass). raw gives undecorated terrain.
func (o *Overworld) placeMansions(cx, cz int32, c *Chunk, raw func(cx, cz int32) *PackedChunk) {
	o.mansionsReach(cx, cz, raw, func(m *mansion) {
		b := &scBuild{o: o, c: c, cbox: structure.NewBox(cx*16, MinY, cz*16, cx*16+15, MaxY-1, cz*16+15)}
		b.ids()
		// The chunk's random rolls the allays and the chest seeds.
		b.r.Seed(populationSeed(o.seed, cx, cz))
		for _, p := range m.pieces {
			if p.bb.Intersects(b.cbox) {
				b.mansionPiece(p)
			}
		}
		b.mansionFoundation(m)
		b.mansionSteps(m)
	})
}

// isPane reports whether a block is a glass pane or iron bars.
func (o *Overworld) isPane(id uint32) bool {
	n := o.blockName(id)
	return strings.HasSuffix(n, "glass_pane") || n == "minecraft:iron_bars"
}

// Chest marker sides, as quarter turns before the piece's own turn.
const (
	chestNone  = -1
	chestNorth = 0
	chestEast  = 1
	chestSouth = 2
	chestWest  = 3
)

// mansionPiece places one template into the chunk, then handles its
// markers (sub_4FC6500).
func (b *scBuild) mansionPiece(p mansionPiece) {
	o := b.o
	jt := o.st.Template("mansion/" + p.name)
	if jt == nil {
		return
	}
	conv := o.st.TemplateStatesMirror(jt, p.rot, p.mirror)
	var markers []templateMarker
	for _, tb := range jt.Template.Blocks {
		st := jt.Template.Palette[tb.State]
		rp := structure.RotatePos(structure.MirrorPos(tb.Pos, p.mirror), p.rot)
		pos := [3]int32{p.pos[0] + rp[0], p.pos[1] + rp[1], p.pos[2] + rp[2]}
		switch st.Name {
		case blockStructureBlock:
			if m, _ := tb.NBT["metadata"].(string); m != "" {
				markers = append(markers, templateMarker{pos, m})
			}
			continue
		case blockJigsaw, blockStructureVoid:
			continue
		}
		cs := conv[tb.State]
		if !cs.OK || !b.setWorld(pos[0], pos[1], pos[2], cs.ID) {
			continue
		}
		b.dropEntity(pos)
		if cs.Water && o.holdsLiquid[o.blockName(cs.ID)] {
			i := (pos[1]-MinY)<<8 | (pos[2]&15)<<4 | pos[0]&15
			b.c.Water[i>>waterWordShift] |= 1 << (i & waterBitMask)
		}
		if d := o.st.BlockEntityFor(cs.ID, st.Name, pos[0], pos[1], pos[2]); d != nil {
			b.c.BlockEntities = append(b.c.BlockEntities, structure.BlockEntity{X: pos[0], Y: pos[1], Z: pos[2], Data: d})
		}
	}
	for _, m := range markers {
		// The chest's turn before the piece's: 0 north, 1 east, 2 south,
		// 3 west; a mirror swaps the sides it flips.
		// Any other name starting with Chest (the templates use ChestNorth)
		// faces north.
		side := chestNone
		switch {
		case m.name == "ChestEast":
			side = chestEast
			if p.mirror == structure.MirrorX {
				side = chestWest
			}
		case m.name == "ChestSouth":
			side = chestSouth
			if p.mirror == structure.MirrorZ {
				side = chestNorth
			}
		case m.name == "ChestWest":
			side = chestWest
			if p.mirror == structure.MirrorX {
				side = chestEast
			}
		case strings.HasPrefix(m.name, "Chest"):
			side = chestNorth
			if p.mirror == structure.MirrorZ {
				side = chestSouth
			}
		case m.name == "Group of Allays":
			if b.setWorld(m.pos[0], m.pos[1], m.pos[2], b.air) {
				b.dropEntity(m.pos)
			}
			b.r.IntN(mansionAllayDraws)
		}
		if side != chestNone {
			facing := [4]int32{dirNorth, dirEast, dirSouth, dirWest}[(p.rot+side)&3]
			b.chest(m.pos[0], m.pos[1], m.pos[2], facing, "woodland_mansion")
		}
	}
}

// isLiquid reports whether a block is water or lava.
func (o *Overworld) isLiquid(id uint32) bool {
	return id == o.ids.water || id == o.ids.flowingWater || id == o.ids.lava || id == o.ids.flowingLava
}

// fillDownCobble fills cobblestone below a position through air and
// liquid, down to y 2.
func (b *scBuild) fillDownCobble(x, y, z int32) {
	if y <= mansionFoundationFloor {
		return
	}
	for yy := y - 1; yy >= mansionFoundationFloor; yy-- {
		if id := b.get(x, yy, z); id != b.air && !b.o.isLiquid(id) {
			return
		}
		b.put(b.cobble, x, yy, z)
	}
}

// mansionFoundation fills cobblestone under the mansion's floor where a
// piece's box stands over air or liquid.
func (b *scBuild) mansionFoundation(m *mansion) {
	y0 := m.bb.Y0
	for x := b.cbox.X0; x <= b.cbox.X1; x++ {
		for z := b.cbox.Z0; z <= b.cbox.Z1; z++ {
			p := [3]int32{x, y0, z}
			if b.get(x, y0, z) == b.air || !m.bb.Contains(p) {
				continue
			}
			for _, pc := range m.pieces {
				if pc.bb.Contains(p) {
					b.fillDownCobble(x, y0, z)
					break
				}
			}
		}
	}
}

// mansionStepRun is one run of entrance stairs: its distance south of the
// start and the direction it steps down.
type mansionStepRun struct {
	south int32
	dir   int32
}

// mansionSteps lays the two runs of cobblestone stairs beside the entrance
// (sub_4FBF5B0 with sub_4FBFD00): each starts 9 east of the start, 2 and
// 12 south, two columns wide, and steps down outward (north for the first,
// south for the second) while it finds air.
func (b *scBuild) mansionSteps(m *mansion) {
	e, s, n := rotDir(m.rot, dirEast), rotDir(m.rot, dirSouth), rotDir(m.rot, dirNorth)
	origin := [3]int32{m.origin[0], m.origin[1] - 1, m.origin[2]}
	// weirdo_direction of a stair rising toward the mansion, by the run's
	// direction (east, west, south, north rise from west, east, north, south).
	rise := map[int32]int32{dirWest: 0, dirEast: 1, dirNorth: 2, dirSouth: 3}
	for _, run := range [2]mansionStepRun{{2, n}, {12, s}} {
		st := b.o.state("minecraft:stone_stairs", map[string]any{"weirdo_direction": rise[run.dir], "upside_down_bit": false})
		start := rel(rel(origin, e, mansionStepsEast), s, run.south)
		for col := range int32(mansionStepWidth) {
			b.stepRun(rel(start, e, col), run.dir, st)
		}
	}
}

// stepRun is sub_4FBFD00: stairs going down along dir from pos while the
// spot is air, each with cobblestone under it.
func (b *scBuild) stepRun(pos [3]int32, dir int32, st uint32) {
	dx, dz := dirStep(dir)
	for b.cbox.Contains(pos) && b.get(pos[0], pos[1], pos[2]) == b.air {
		b.put(st, pos[0], pos[1], pos[2])
		b.fillDownCobble(pos[0], pos[1], pos[2])
		pos = [3]int32{pos[0] + dx, pos[1] - 1, pos[2] + dz}
	}
}

// NearestMansion finds the woodland mansion nearest to block (x, z),
// searching maxRegions placement regions (80 chunks each) around it, and
// returns its start column at the top of its box (the roof). It is safe
// for concurrent use.
func (o *Overworld) NearestMansion(x, z int32, maxRegions int32) (Scattered, bool) {
	var cands []locateCand
	rx0, rz0 := structure.Region(x>>4, mansionSpacing), structure.Region(z>>4, mansionSpacing)
	for rx := rx0 - maxRegions; rx <= rx0+maxRegions; rx++ {
		for rz := rz0 - maxRegions; rz <= rz0+maxRegions; rz++ {
			cx, cz := o.mansionPick(rx, rz)
			dx, dz := int64(cx*16+8-x), int64(cz*16+8-z)
			cands = append(cands, locateCand{cx, cz, dx*dx + dz*dz})
		}
	}
	slices.SortFunc(cands, compareLocateCand)
	raw := newRawChunks(o)
	for _, c := range cands {
		if !o.isMansionChunk(c.cx, c.cz) {
			continue
		}
		m := o.mansionAt(c.cx, c.cz, raw.get)
		if m == nil {
			continue
		}
		return Scattered{Kind: "mansion", X: m.origin[0], Y: m.bb.Y1, Z: m.origin[2]}, true
	}
	return Scattered{}, false
}

// free reports whether cell (x, y) is empty.
func (g *mansionGrid) free(x, y int32) bool { return g.get(x, y) == cellEmpty }

// houseAt reports whether g, which may be nil, has building at (x, y).
func houseAt(g *mansionGrid, x, y int32) bool { return g != nil && g.house(x, y) }

// flipEnd is the other end of span [a, b] from v.
func flipEnd(v, a, b int32) int32 {
	if v == a {
		return b
	}
	return a
}

// oneUp is q one block higher.
func oneUp(q [3]int32) [3]int32 { return [3]int32{q[0], q[1] + 1, q[2]} }

// corridorOpen reports whether cell (x, y) is corridor or a room entrance.
func corridorOpen(g, rooms *mansionGrid, x, y int32) bool {
	return g.get(x, y) == cellCorridor || rooms.get(x, y)&roomEntry != 0
}

// wallOrDoor is the door piece on the room's door side d, the wall piece
// elsewhere.
func wallOrDoor(door, d int32, doorName, wall string) string {
	if door == d {
		return doorName
	}
	return wall
}
