package mixer

import (
	"testing"
)

// TestFinalClipperBypass proves the bypass switch reports correctly and
// the clean path clamps at every boundary.
func TestFinalClipperBypass(t *testing.T) {
	m := New(48000, 2, 64)
	if m.SaturationBypass() {
		t.Fatal("bypass on by default")
	}
	m.SetSaturationBypass(true)
	if !m.SaturationBypass() {
		t.Fatal("bypass not reported")
	}
	c := NewSoftClipper(0.9)
	c.SetClean(true)
	for _, tc := range []struct {
		in, want float32
	}{
		{-3, -1}, {-1, -1}, {-0.999, -0.999}, {0, 0},
		{0.999, 0.999}, {1, 1}, {3, 1},
	} {
		if got := c.ProcessSample(tc.in); got != tc.want {
			t.Fatalf("clean(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
	m.SetSaturationBypass(false)
	if m.SaturationBypass() {
		t.Fatal("bypass stuck on")
	}
}
