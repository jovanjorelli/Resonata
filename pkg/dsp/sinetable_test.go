package dsp

import (
	"math"
	"testing"
)

// TestSineTableAccuracy bounds the lookup error across the circle,
// including negative and multi-cycle phases that must wrap.
func TestSineTableAccuracy(t *testing.T) {
	peak := 0.0
	for i := -4096; i <= 8192; i++ {
		phase := float64(i) / 2048
		got := float64(SineFast(phase))
		want := math.Sin(twoPi * phase)
		if d := math.Abs(got - want); d > peak {
			peak = d
		}
	}
	if peak > 2e-6 {
		t.Fatalf("sine table peak error = %v, want <= 2e-6", peak)
	}
}

// TestSineOscillatorTracksMathSin checks the oscillator still follows a
// true sine after the table conversion.
func TestSineOscillatorTracksMathSin(t *testing.T) {
	osc := NewSine()
	osc.SetFrequency(440, 48000)
	phase := 0.0
	for i := 0; i < 5000; i++ {
		got := float64(osc.Next())
		want := math.Sin(twoPi * phase)
		if math.Abs(got-want) > 2e-6 {
			t.Fatalf("frame %d: %v vs %v", i, got, want)
		}
		phase += 440.0 / 48000
		if phase >= 1 {
			phase -= math.Floor(phase)
		}
	}
}
