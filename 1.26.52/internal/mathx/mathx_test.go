package mathx

import "testing"

func TestFloorDiv(t *testing.T) {
	for _, c := range [][3]int32{{7, 2, 3}, {-7, 2, -4}, {-8, 2, -4}, {0, 5, 0}, {-1, 20, -1}, {-20, 20, -1}, {-21, 20, -2}, {7, -2, -4}} {
		if got := FloorDiv(c[0], c[1]); got != c[2] {
			t.Errorf("FloorDiv(%d, %d) = %d, want %d", c[0], c[1], got, c[2])
		}
	}
}

func TestAbs(t *testing.T) {
	if Abs(-3) != 3 || Abs(4) != 4 || AbsFloat(-1.5) != 1.5 {
		t.Error("Abs")
	}
}
