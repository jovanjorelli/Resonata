package mixer

import (
	"math"
	"testing"
)

func TestPanGainsExtremes(t *testing.T) {
	if l, r := PanGains(-1); l != 1 || r != 0 {
		t.Errorf("hard left = (%v, %v), want exact (1, 0)", l, r)
	}
	if l, r := PanGains(1); l != 0 || r != 1 {
		t.Errorf("hard right = (%v, %v), want exact (0, 1)", l, r)
	}
	center := float32(math.Sqrt(0.5))
	if l, r := PanGains(0); l != center || r != center {
		t.Errorf("center = (%v, %v), want (%v, %v)", l, r, center, center)
	}
	// Out-of-range positions clamp to the extremes.
	if l, r := PanGains(-5); l != 1 || r != 0 {
		t.Errorf("pan -5 = (%v, %v), want (1, 0)", l, r)
	}
	if l, r := PanGains(3); l != 0 || r != 1 {
		t.Errorf("pan 3 = (%v, %v), want (0, 1)", l, r)
	}
}

func TestPanGainsConstantPower(t *testing.T) {
	prevL, prevR := float32(2), float32(-2)
	for k := 0; k <= 200; k++ {
		pan := float32(-1 + 2*float64(k)/200)
		l, r := PanGains(pan)
		power := float64(l)*float64(l) + float64(r)*float64(r)
		if math.Abs(power-1) > 1e-6 {
			t.Fatalf("pan %v: l²+r² = %v, want 1 (constant power)", pan, power)
		}
		if l > prevL+1e-7 || r < prevR-1e-7 {
			t.Fatalf("pan %v: gains not monotonic (%v, %v) after (%v, %v)",
				pan, l, r, prevL, prevR)
		}
		prevL, prevR = l, r
	}
}
