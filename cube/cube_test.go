package cube

import "testing"

func TestDirection(t *testing.T) {
	for i, d := range LegacyDirections() {
		if d.Legacy() != int32(i) || FromLegacy(int32(i)) != d {
			t.Errorf("%v: legacy %d, want %d", d, d.Legacy(), i)
		}
	}
	for _, d := range Directions() {
		dx, dz := d.Step()
		ox, oz := d.Opposite().Step()
		if dx != -ox || dz != -oz {
			t.Errorf("%v: opposite step (%d, %d) for (%d, %d)", d, ox, oz, dx, dz)
		}
		if d.RotateRight().RotateLeft() != d {
			t.Errorf("%v: rotations do not undo", d)
		}
		if d.Face().String() != d.String() {
			t.Errorf("%v: face %v", d, d.Face())
		}
		if back, ok := d.Face().Direction(); !ok || back != d {
			t.Errorf("%v: face direction %v", d, back)
		}
	}
	// Clockwise: north turns to east, whose step is +x.
	if dx, _ := North.RotateRight().Step(); dx != 1 {
		t.Errorf("north turned right steps x %d", dx)
	}
}

func TestFace(t *testing.T) {
	for i, f := range Faces() {
		if int(f) != i {
			t.Errorf("%v at %d", f, i)
		}
		dx, dy, dz := f.Step()
		ox, oy, oz := f.Opposite().Step()
		if dx != -ox || dy != -oy || dz != -oz {
			t.Errorf("%v: opposite %v does not step back", f, f.Opposite())
		}
	}
}
