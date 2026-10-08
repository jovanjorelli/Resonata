package midi

import (
	"math"
	"testing"
)

// TestFinalClampF64 proves below/above/NaN/exact-limit behavior.
func TestFinalClampF64(t *testing.T) {
	if got := clampF64(-5, 0, 10); got != 0 {
		t.Fatalf("low = %v", got)
	}
	if got := clampF64(50, 0, 10); got != 10 {
		t.Fatalf("high = %v", got)
	}
	if got := clampF64(math.NaN(), 0, 10); got != 0 {
		t.Fatalf("NaN = %v", got)
	}
	if got := clampF64(0, 0, 10); got != 0 {
		t.Fatalf("lo edge = %v", got)
	}
	if got := clampF64(10, 0, 10); got != 10 {
		t.Fatalf("hi edge = %v", got)
	}
	if got := clampF64(4.5, 0, 10); got != 4.5 {
		t.Fatalf("mid = %v", got)
	}
}

// TestFinalClampInt proves below/above/exact-limit behavior.
func TestFinalClampInt(t *testing.T) {
	if got := clampInt(-1, 0, 127); got != 0 {
		t.Fatalf("low = %d", got)
	}
	if got := clampInt(200, 0, 127); got != 127 {
		t.Fatalf("high = %d", got)
	}
	if got := clampInt(0, 0, 127); got != 0 {
		t.Fatalf("lo edge = %d", got)
	}
	if got := clampInt(127, 0, 127); got != 127 {
		t.Fatalf("hi edge = %d", got)
	}
	if got := clampInt(64, 0, 127); got != 64 {
		t.Fatalf("mid = %d", got)
	}
}

// TestFinalRoundF proves negative, positive, and halfway rounding.
func TestFinalRoundF(t *testing.T) {
	if got := roundF(2.3); got != 2 {
		t.Fatalf("pos = %v", got)
	}
	if got := roundF(-2.3); got != -2 {
		t.Fatalf("neg = %v", got)
	}
	if got := roundF(2.5); got != 3 {
		t.Fatalf("half-up = %v", got)
	}
	if got := roundF(-2.5); got != -3 {
		t.Fatalf("half-down = %v", got)
	}
	if got := roundF(0); got != 0 {
		t.Fatalf("zero = %v", got)
	}
}
