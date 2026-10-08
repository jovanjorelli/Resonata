package encoder

import (
	"testing"
)

// abs32 returns the magnitude of v.
func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// TestDeepDownmixClamps proves short buffers truncate the frame count
// instead of reading out of range.
func TestDeepDownmixClamps(t *testing.T) {
	left := []float32{0.6, 0.8, 1.0, 1.0}
	right := []float32{-0.2, 0.2, 0.4, 0.6}
	dst := make([]float32, 4)
	if n := DownmixMono(dst, left, right, 4); n != 4 {
		t.Fatalf("n = %d, want 4", n)
	}
	if abs32(dst[0]-0.2) > 1e-6 || abs32(dst[1]-0.5) > 1e-6 {
		t.Fatalf("mix = %v", dst[:2])
	}
	short := make([]float32, 2)
	if n := DownmixMono(short, left, right, 4); n != 2 {
		t.Fatalf("short dst n = %d, want 2", n)
	}
	if n := DownmixMono(dst, left[:1], right, 4); n != 1 {
		t.Fatalf("short left n = %d, want 1", n)
	}
	if n := DownmixMono(dst, left, right[:3], 4); n != 3 {
		t.Fatalf("short right n = %d, want 3", n)
	}
	if n := DownmixMono(dst, left, right, 0); n != 0 {
		t.Fatalf("zero n = %d", n)
	}
}

// TestDeepResolveErrors proves every invalid encoding fails loudly.
func TestDeepResolveErrors(t *testing.T) {
	for _, tc := range []struct {
		depth    string
		channels int
		rate     int
	}{
		{"64", 2, 48000},
		{"16", 3, 48000},
		{"16", -1, 48000},
		{"16", 2, 22050},
		{"32f", 2, 96000 + 1},
	} {
		if _, err := Resolve(tc.depth, tc.channels, tc.rate); err == nil {
			t.Errorf("%+v accepted", tc)
		}
	}
}

// TestDeepResolveMatrix proves every documented combination resolves
// with the expected container parameters.
func TestDeepResolveMatrix(t *testing.T) {
	for _, tc := range []struct {
		depth        string
		channels     int
		rate         int
		bits, ch, sr int
	}{
		{"16", 1, 44100, 16, 1, 44100},
		{"24", 0, 48000, 24, 2, 48000},
		{"32", 2, 96000, 32, 2, 96000},
		{"32f", 2, 48000, 32, 2, 48000},
		{"float", 1, 44100, 32, 1, 44100},
	} {
		s, err := Resolve(tc.depth, tc.channels, tc.rate)
		if err != nil {
			t.Fatalf("%+v: %v", tc, err)
		}
		if s.Bits != tc.bits || s.Channels != tc.ch || s.SampleRate != tc.sr {
			t.Fatalf("%+v = %+v", tc, s)
		}
	}
}
