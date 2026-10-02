package mixer

import "testing"

func TestSoftClipperPassthrough(t *testing.T) {
	c := NewSoftClipper(0.9)
	for _, x := range []float32{0, 0.1, -0.1, 0.5, -0.5, 0.8999, 0.9, -0.9} {
		if y := c.ProcessSample(x); y != x {
			t.Errorf("ProcessSample(%v) = %v, want identity below threshold", x, y)
		}
	}
}

func TestSoftClipperContinuity(t *testing.T) {
	c := NewSoftClipper(0.9)
	for _, eps := range []float32{1e-6, 1e-4, 0.01, 0.05} {
		y := c.ProcessSample(0.9 + eps)
		// tanh(u) <= u keeps the curve between the threshold and the
		// linear continuation: no discontinuity at the knee.
		if y < 0.9 || y > 0.9+eps+1e-6 {
			t.Errorf("x = 0.9+%g: y = %v, want within (0.9, 0.9+eps]", eps, y)
		}
		if yn := c.ProcessSample(-0.9 - eps); yn != -y {
			t.Errorf("asymmetric at eps %g: %v vs %v", eps, yn, -y)
		}
	}
}

func TestSoftClipperBounds(t *testing.T) {
	c := NewSoftClipper(0.9)
	prev := float32(0.9)
	for _, x := range []float32{0.95, 1, 1.5, 3, 10, 100, 1e6, 1e9} {
		y := c.ProcessSample(x)
		if y < prev {
			t.Errorf("not monotonic at %g: %v < %v", x, y, prev)
		}
		if y > 1 || y < 0.9 {
			t.Errorf("x = %g: y = %v, want in (0.9, 1]", x, y)
		}
		if yn := c.ProcessSample(-x); yn != -y {
			t.Errorf("odd symmetry broken at %g: %v vs %v", x, yn, -y)
		}
		prev = y
	}
}

func TestSoftClipperThreshold(t *testing.T) {
	if th := NewSoftClipper(5).Threshold(); th != 1 {
		t.Errorf("high clamp: threshold = %v, want 1", th)
	}
	if th := NewSoftClipper(-1).Threshold(); th != 0.05 {
		t.Errorf("low clamp: threshold = %v, want 0.05", th)
	}
	c := NewSoftClipper(0.5)
	if y := c.ProcessSample(0.5); y != 0.5 {
		t.Errorf("at threshold = %v, want identity", y)
	}
	if y := c.ProcessSample(0.6); y <= 0.5 || y >= 0.6 {
		t.Errorf("0.6 = %v, want strictly between 0.5 and 0.6", y)
	}
	c.SetThreshold(0.95)
	if c.Threshold() != 0.95 {
		t.Error("SetThreshold not applied")
	}
}

func TestSoftClipperSlice(t *testing.T) {
	a := NewSoftClipper(0.9)
	b := NewSoftClipper(0.9)
	data := []float32{0.1, -0.2, 0.95, -3, 0, 1e6}
	want := make([]float32, len(data))
	for i, x := range data {
		want[i] = b.ProcessSample(x)
	}
	a.ProcessSlice(data)
	for i := range data {
		if data[i] != want[i] {
			t.Fatalf("slice[%d] = %v, want %v", i, data[i], want[i])
		}
	}
}
